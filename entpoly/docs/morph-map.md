# The morph map

Every polymorphic row stores a **morph key** in the `*_type` column — a short, stable string that identifies which parent table the `*_id` column points at. The morph map controls what that string is.

This page covers: what the morph key is, why you should not store fully-qualified Go type names, how the default fallback works, and how to register stable aliases.

## What the morph key is

When you call

```go
client.Comment.Create().SetBody("hi").SetCommentable(post).Save(ctx)
```

`entpoly`'s generated `Set<Morph>` builder method writes two columns:

| Column | Value |
|---|---|
| `commentable_id` | `post.MorphID()` — the parent row's PK, stringified |
| `commentable_type` | `post.MorphKey()` — the **morph key** for `*Post` |

On read, `entpoly` uses the morph key to determine which ent client to query for the parent. The key is the index into the morph map; the map's values are the ent schema names.

## Default behaviour — snake_case from the schema name

If you register **no** morph map at all, every parent type's morph key defaults to the snake_case form of its ent type name:

| Ent type | Default morph key |
|---|---|
| `Post` | `post` |
| `Video` | `video` |
| `FeaturedPost` | `featured_post` |

For small projects this is fine, but it ties your column data to your Go-side type names. Renaming `Post` → `Article` in Go means every existing `commentable_type='post'` row is now orphaned — the morph key changed but the data did not.

## Stable aliases via `WithMorphMap` (optional)

`WithMorphMap` is **optional**. Every parent type that appears in a `MorphTo` edge is auto-registered with its snake_case alias at preprocess time — for most projects you do not need this option at all. Register an explicit alias only when you want to decouple the persisted column value from the Go identifier (typically before a rename):

```go
// ent/entc.go
entc.Extensions(
    entpoly.NewExtension(
        entpoly.WithMorphMap(map[string]string{
            "post":  "Post",
            "video": "Video",
            "image": "Image",
        }),
    ),
)
```

Now `(*Post).MorphKey()` returns `"post"` regardless of what the Go-side ent type is named today. You can rename the schema to `Article` tomorrow as long as you update the map's right-hand side to match — the persisted column data never moves.

The morph map flows through the codegen pipeline into the runtime: every generated `MorphKey()` method returns the literal string from the map.

## Renaming workflow

The whole point of having an alias map is to make renames a Go-only change. The workflow:

1. **Decide on the new Go name.** Say you want to rename `Post` to `Article`.
2. **Update the morph map** — change the right-hand side, leave the key alone:
   ```go
   entpoly.WithMorphMap(map[string]string{
       "post":  "Article", // was "Post"
       ...
   })
   ```
3. **Run codegen** — `go generate ./ent`.
4. **Rename the schema file** and update the Go-side struct name.
5. **No data migration required.** Every `commentable_type = 'post'` row remains valid; the morph key never changed.

If you skip step 2 and rename the schema directly, codegen falls back to the snake_case default (`article`) and your existing rows become unrecoverable from the typed read path (`comment.QueryCommentable(...)` will hit the `default:` arm and return an "unknown morph type" error).

## What **not** to store

Some ORMs (notably Eloquent prior to morph maps) default to storing the fully-qualified class name in the type column — `App\Models\Post`, `Acme\Domain\Video`. We recommend against this for ent the same way Laravel recommends `Relation::morphMap` for Eloquent:

- **Refactor-unsafe**: any rename, namespace change, or package move requires a data migration.
- **Verbose**: doubles the column's width for no read-side benefit.
- **Cross-language hostile**: external consumers (job queues, analytics, message brokers, other services in different languages) cannot reason about Go import paths.

Short stable aliases beat type-path strings on every axis.

## Inspecting the map at runtime

The generated code exposes two lookup helpers in the ent package:

```go
ent.MorphTypeFor("Post")     // → "post"   — schema name → morph key
ent.MorphTypeName("post")    // → "Post"   — morph key → schema name
ent.MorphTypeFor("Unknown")  // → ""       — empty string for unknown types
```

These are handy for code that operates against the morph key without knowing the parent type at compile time — exporters, ETL jobs, audit log writers, and so on.

## Multiple relations, one morph map

The morph map is **global to the extension**. The same map serves every relationship in the project — `commentable`, `imageable`, `taggable`, and so on. There is no per-relation override.

If two relations need different alias schemes for the same parent type (rare), you have two options:

1. **Use the schema-name default for one of them.** Drop the entry for that parent from the map; the snake_case fallback kicks in.
2. **Disambiguate with a prefix in the morph key.** `commentable_type = 'post'`, `imageable_type = 'image_post'`. The morph map is just `string → string`; nothing forces the keys to be unique types.

In practice, a single project-wide map is what you want — the morph key identifies the *parent table*, not the *relation*, so the same `Post` plays the same role in every relation.

## Per-feature how-to

`WithMorphMap` is the only extension option that controls the persisted `*_type` value. Other features (custom column names, GraphQL union name, M2M pivot table name) operate on different layers and compose independently:

| Concern | Knob | Where |
|---|---|---|
| Persisted `*_type` value | `entpoly.WithMorphMap(...)` (extension option) | This page |
| Column name on the child | `MixinTypeColumn(...)` + `.TypeColumn(...)` | [Custom columns](./features/custom-columns.md) |
| GraphQL union name | `.GQL("CustomName")` on the edge | [GraphQL](./features/graphql.md) |
| Pivot table name (M2M) | `.Through("custom_pivots", Pivot.Type)` | [M2M](./features/m2m-polymorphic.md) |

## Backward-compatible renames

Want to rename the morph key itself (not the Go type)? You have to migrate data. There is no in-place rename — `commentable_type = 'post'` is the literal value in the column. Two approaches:

1. **Live migration**: update both the map and rows in a single transaction. Works for small tables.
   ```sql
   UPDATE comments SET commentable_type = 'article' WHERE commentable_type = 'post';
   ```
   Update the map's key in the same deploy.
2. **Dual-write window**: temporarily write the new key on creates, but read both. After backfill, switch the map and remove the old key handler. Better for large tables where a single `UPDATE` would lock the table too long.

Either way: the morph map is a code-side description of what is *currently* in the column. Keep them in sync.
