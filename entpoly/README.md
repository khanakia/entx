# entpoly — polymorphic relationships for [ent](https://entgo.io) (Go ORM)

> **Laravel-style polymorphic relationships for the ent ORM in Go.** Adds `MorphTo`, `MorphOne`, `MorphMany`, and `MorphedByMany` to your ent schemas, with compile-time type safety, a DB-enforced type enum, runtime hooks (`Required` / `Touch` / `Cascade` / `SoftDelete`), and an optional GraphQL union surface for [gqlgen](https://gqlgen.com).

`go get github.com/khanakia/entx/entpoly`

[Quickstart](#quickstart) · [Install](#install) · [How do I…](#how-do-i) · [Docs](#documentation-map) · [Laravel parity](./docs/laravel-parity.md) · [FAQ](#faq)

---

## What is entpoly?

`entpoly` is a [code-generation extension](https://entgo.io/docs/extensions) for the [ent ORM](https://entgo.io) that brings **polymorphic relationships** — the kind you get from Laravel Eloquent's `morphTo` / `morphMany` — to Go. A single child schema (`Comment`, `Image`, `Like`, `Tag`) can belong to any one of several parent types (`Post`, `Video`, `Article`) without a join table per parent and without giving up Go's type system.

Out of the box, ent only supports homogeneous edges (one edge → one target type). `entpoly` closes that gap with:

- **Four relation shapes:** `MorphTo`, `MorphOne`, `MorphMany`, `MorphedByMany` (polymorphic many-to-many with a pivot).
- **Compile-time type safety:** the forward resolver returns a sealed Go interface; passing a non-allowed parent fails to compile.
- **Database-level integrity:** the `<rel>_type` column is a real `field.Enum` with a CHECK constraint — raw SQL can't write an invalid morph key.
- **Laravel-parity ergonomics:** chain `.Required()`, `.Touch()`, `.Cascade()`, `.SoftDelete()` on the edge builder. One `RegisterPolyHooks(client)` call wires every hook.
- **GraphQL union codegen:** `.GQL()` emits Go-side `Is<Union>()` markers plus a `.graphql` fragment — drop straight into gqlgen.

Built for production: UUID-parent PKs, eager-loading with 1+N batching (not N+1), polymorphic many-to-many helpers (`Toggle` / `Sync` / `SyncWithoutDetach`), soft-delete-aware reverse resolves, and a drift linter that catches AllowedTypes mismatches at codegen time.

## Quickstart

```go
// ent/schema/post.go, video.go — regular ent schemas with .Type fields.
// ent/schema/comment.go — the child carrying the discriminator:

func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable", entpoly.MixinAllowed(Post.Type, Video.Type)),
    }
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type).
            Required().     // hook rejects unset / cleared writes
            Touch().        // bumps parent.updated_at on save
            Cascade().      // deletes children when parent dies
            SoftDelete().   // reverse resolves skip soft-deleted parents
            GQL(),          // emit GraphQL union surface
    }
}
```

```bash
go get github.com/khanakia/entx/entpoly
go generate ./ent
```

```go
client := ent.NewClient(...)
ent.RegisterPolyHooks(client)   // wires Required + Touch + Cascade hooks

// Forward resolve — sealed interface, no `any` escape hatch:
switch p := comment.QueryCommentable(ctx).(type) {
case *ent.Post:   /* typed *Post  */
case *ent.Video:  /* typed *Video */
case nil:         /* unset        */
}

// Reverse — typed back-ref, composable:
post.QueryComments().Where(comment.ApprovedEQ(true)).All(ctx)
```

## Install

```bash
go get github.com/khanakia/entx/entpoly
```

Register in `ent/entc.go`:

```go
opts := []entc.Option{
    entc.Extensions(entpoly.NewExtension()),
}
entc.Generate("./schema", config, opts...)
```

`WithMorphMap(...)` is optional — every parent gets a snake_case morph key by default. Pass an explicit map to lock aliases across renames. See [docs/morph-map.md](./docs/morph-map.md).

Run `go generate ./ent`. A sidecar `ent/polymorphic.go` is emitted alongside ent's normal output containing the `Morphable` interface, per-parent constants, sealed parent interfaces, typed setters, typed predicates, typed resolver, typed back-refs, eager-load helpers, and the runtime hooks.

## How do I…

| Task | Doc |
|---|---|
| Add a polymorphic relation (Comment → Post / Video) | [getting-started](./docs/getting-started.md) |
| Reject saves when the parent is unset | [features/required.md](./docs/features/required.md) |
| Bump the parent's `updated_at` on child save | [features/touch.md](./docs/features/touch.md) |
| Delete children when the parent is deleted | [features/cascade.md](./docs/features/cascade.md) |
| Hide soft-deleted parents from reverse resolves | [features/soft-delete.md](./docs/features/soft-delete.md) |
| Expose the relation as a GraphQL union (gqlgen) | [features/graphql.md](./docs/features/graphql.md) |
| Use UUID-PK parents | [features/uuid-parents.md](./docs/features/uuid-parents.md) |
| Build a polymorphic many-to-many (tags) | [features/m2m-polymorphic.md](./docs/features/m2m-polymorphic.md) |
| Eager-load the parent without N+1 | [features/eager-loading.md](./docs/features/eager-loading.md) |
| Rename the `<rel>_id` / `<rel>_type` columns | [features/custom-columns.md](./docs/features/custom-columns.md) |
| Avoid composite-index name collisions across ent modules sharing a DB | [getting-started.md § cross-module index-name override](./docs/getting-started.md#optional-cross-module-index-name-override) |
| List the host type in its own `AllowedTypes` | [features/self-referential.md](./docs/features/self-referential.md) |
| Filter children by typed parent predicates | [features/predicates.md](./docs/features/predicates.md) |
| Model exactly-one back-reference (MorphOne) | [features/morph-one.md](./docs/features/morph-one.md) |
| Lock morph-key aliases across renames | [morph-map.md](./docs/morph-map.md) |
| Translate a Laravel verb to entpoly | [laravel-parity.md](./docs/laravel-parity.md) |

## Core concepts

**Schema-level edges.** Polymorphic relations are declared in `Edges()` the same way as regular ent edges. No annotations, no field-spread API. The mixin contributes the discriminator columns; the edge builder carries the options.

**Sealed parent interface.** The forward resolver returns a generated Go interface (`CommentCommentableParent`) sealed to the types listed in `AllowedTypes`. Type-switches on that interface reject parents not in the set at compile time.

**Three-layer type safety.** Sealed Go interface (compile time) + ent runtime enum validator (typo'd morph key) + DB CHECK / native ENUM (raw SQL bypass). See [ADR-001](./docs/internals/adr-001-type-safety.md).

**No foreign keys.** Polymorphic columns can't carry SQL FKs — the type column references multiple tables. `Cascade()` + DB-enforced enum compensate. See [internals/architecture.md](./docs/internals/architecture.md).

## Laravel → entpoly cheat-sheet

| Laravel | entpoly |
|---|---|
| `$this->morphTo()` | `entpoly.MorphTo("commentable", Post.Type, Video.Type)` |
| `$this->morphMany(Comment::class, 'commentable')` | `entpoly.MorphMany("comments", Comment.Type, "commentable")` |
| `$this->morphOne(Image::class, 'imageable')` | `entpoly.MorphOne("featured_image", Image.Type, "imageable")` |
| `$this->morphedByMany(Post::class, 'taggable')` | `entpoly.MorphedByMany("posts", Post.Type)` on the `Tag` schema |
| `$comment->commentable` | `comment.QueryCommentable(ctx)` (sealed Go interface) |
| `$post->comments` | `post.QueryComments().All(ctx)` |
| `$post->touches = ['commentable']` | `MorphTo(...).Touch()` |
| `protected $morphMap` | `entpoly.WithMorphMap(map[string]string{...})` |

Full mapping: [docs/laravel-parity.md](./docs/laravel-parity.md).

## Documentation map

### Using entpoly

| Doc | Use when |
|---|---|
| [Getting started](./docs/getting-started.md) | First-time setup |
| [Per-feature how-tos](./docs/features/) | Recipe per feature (`.Required()`, `.Touch()`, `.GQL()`, …) |
| [Relationships reference](./docs/relationships.md) | Choosing a shape |
| [Mutations reference](./docs/mutations.md) | Translating Laravel verbs |
| [Laravel parity](./docs/laravel-parity.md) | Full Laravel → entpoly map |
| [Morph map](./docs/morph-map.md) | Stable aliases for type-column values |
| [FAQ](./docs/faq.md) | Common questions |
| [Feature matrix](./docs/feature-matrix.md) | Dense reference of every surface |

### Examples + tests

| Path | What it is |
|---|---|
| [examples/basic/](./examples/basic/) | Int-PK runnable example with `MixinAllowed` enum mode + every feature flag |
| [examples/morphstring/](./examples/morphstring/) | Bare `MorphMixin` example — plain `field.String` discriminator (no `MixinAllowed`); covers stem-matching and divergent-name child schemas |
| [examples/uuid/](./examples/uuid/) | UUID-PK runnable example |
| [../testentpoly/](../testentpoly/) | Integration harness — every feature, real GraphQL HTTP |

### Internals / contributing

| Doc | What it covers |
|---|---|
| [Architecture](./docs/internals/architecture.md) | Codegen pipeline, edge cases, v2 roadmap |
| [ADR-001: type safety](./docs/internals/adr-001-type-safety.md) | Sealed iface + enum design |
| [ADR-002: whereMorphRelation](./docs/internals/adr-002-where-morph-relation.md) | Per-parent sub-query helper design |

## Status

| State | Items |
|---|---|
| Shipped | 13 of 13 roadmap items — see [feature-matrix.md](./docs/feature-matrix.md) |
| Backlog | 2 follow-up gaps surfaced by testentpoly — see [internals/architecture.md § v2 roadmap](./docs/internals/architecture.md#v2-roadmap) |
| Tests | 28 PASS / 2 SKIP / 0 FAIL across 5 phases in [testentpoly/](../testentpoly/) |

## FAQ

**Does the ent ORM support polymorphic relationships natively?** No. ent's edges are homogeneous — one edge points at one target type. `entpoly` is a codegen extension that adds polymorphic edges (`MorphTo` / `MorphOne` / `MorphMany` / `MorphedByMany`) on top.

**How is this different from a regular ent edge?** A regular edge stores a foreign key to a single target table. A polymorphic edge stores a discriminator pair (`<rel>_id` + `<rel>_type`) that can reference rows in any table in `AllowedTypes`.

**Does this work with PostgreSQL / MySQL / SQLite?** Yes — all three. The type column is a `field.Enum`, which lands as a native ENUM on MySQL, a `text` + CHECK on PostgreSQL/SQLite, with the same runtime validator regardless.

**Does it support UUID parent IDs?** Yes — `field.UUID("id", uuid.UUID{})` on parents is auto-detected per parent type. Mixed PK types across the parent set are supported too (`int` + `int64` + `string` + `uuid.UUID` all in the same `AllowedTypes`) — the template emits the right `strconv` / `uuid.Parse` branch per parent. Render-matrix tests in `entpoly/integration_test.go` cover the combinations.

**Does it integrate with gqlgen / entgql for GraphQL?** Yes — `.GQL()` emits the Go-side markers gqlgen needs (`Is<Union>()` on each parent + a type alias), plus an optional `.graphql` schema fragment via `entpoly.WithGQLSchemaFile(...)`. See [features/graphql.md](./docs/features/graphql.md) and [testentpoly/QUERIES.md](../testentpoly/QUERIES.md) for end-to-end examples.

**What about foreign keys?** Polymorphic columns can't carry SQL FKs by definition (one column → multiple target tables). `entpoly` compensates with the `.Cascade()` hook (application-level cascade delete) and the DB-enforced enum on the type column.

**Is there a Laravel-style `morphMap`?** Yes — `entpoly.WithMorphMap(map[string]string{...})`. Defaults to the snake_case of the schema name, so most projects never need it. See [docs/morph-map.md](./docs/morph-map.md).

## License

MIT
