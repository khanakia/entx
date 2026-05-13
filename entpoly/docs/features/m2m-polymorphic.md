# Polymorphic many-to-many — `MorphedByMany` + pivot

A single `Tag` can attach to many `Post`s or many `Video`s through the same `Taggable` pivot table. The pivot is the polymorphic child (it carries the discriminator pair); the holder (`Tag`) declares one `MorphedByMany(...)` per concrete parent type. entpoly auto-emits both directions — `tag.QueryPosts(ctx)` AND `post.QueryTags(ctx)` — from a single declaration on the holder. Pair with `helper.Sync` / `Toggle` / `SyncWithoutDetach` for Laravel's set-diff verbs.

## When to use

- One taxonomy / labelling table attaches to many concrete entity types
- The pivot has its own columns (added_by, sort_order, weight) — Laravel's `withPivot('added_by')`
- You want Laravel `attach` / `detach` / `sync` / `toggle` semantics with typed Go builders

## Setup

Three schemas — see [`entpoly/examples/basic/schema/`](../../examples/basic/schema/) for the runnable version.

Pivot (the polymorphic child):

```go
// ent/schema/taggable.go
type Taggable struct{ ent.Schema }

func (Taggable) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("taggable",
            entpoly.MixinAllowed(Post.Type, Video.Type),
        ),
    }
}

func (Taggable) Fields() []ent.Field {
    return []ent.Field{
        field.Int("tag_id"),
        field.String("added_by").Optional(),
        field.Int("sort_order").Default(0),
    }
}

func (Taggable) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("taggable", Post.Type, Video.Type),
    }
}
```

Holder (declares the M2M back-refs):

```go
// ent/schema/tag.go
type Tag struct{ ent.Schema }

func (Tag) Fields() []ent.Field {
    return []ent.Field{field.String("name").Unique()}
}

func (Tag) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphedByMany("posts", Post.Type).
            Through("taggables", Taggable.Type),
        entpoly.MorphedByMany("videos", Video.Type).
            Through("taggables", Taggable.Type),
    }
}
```

Parents (Post / Video) need no special declaration — entpoly auto-emits the inverse on each AllowedType.

## Wiring

Standard extension registration; nothing M2M-specific:

```go
entc.Extensions(entpoly.NewExtension())
```

Install the helper if you want the set-diff functions:

```bash
go get github.com/khanakia/entx/entpoly/helper
```

## Usage

Attach — write a pivot row:

```go
tag := client.Tag.Create().SetName("golang").SaveX(ctx)
post := client.Post.Create().SetTitle("p").SaveX(ctx)

client.Taggable.Create().
    SetTagID(tag.ID).
    SetTaggable(post).      // typed Morphable setter
    SetAddedBy("aman").
    SaveX(ctx)
```

Forward queries — both directions auto-emitted:

```go
posts, _ := tag.QueryPosts(ctx)   // []*Post — every post tagged with this tag
tags,  _ := post.QueryTags(ctx)   // []*Tag — every tag on this post (auto-inverse)
```

Sync / Toggle / SyncWithoutDetach via the helper:

```go
import "github.com/khanakia/entx/entpoly/helper"

// Current pivots attached to this post:
cur := attachedTagIDs(ctx, client, post)   // []int — your helper

// Target set:
target := []int{a.ID, c.ID}

// Sync — replace whole set.
toAdd, toDel := helper.Sync(cur, target)
for _, id := range toAdd {
    client.Taggable.Create().SetTagID(id).SetTaggable(post).SaveX(ctx)
}
client.Taggable.Delete().Where(/* tag_id IN toDel AND taggable_is(post) */).ExecX(ctx)

// SyncWithoutDetach — attach missing, keep existing.
toAdd = helper.SyncWithoutDetach(cur, target)

// Toggle — flip presence.
toAdd, toDel = helper.Toggle(cur, target)
```

The helpers are pure set-diff — they do not touch the database. You drive the typed pivot builders with the diffs they return.

## Verification

```go
// from testentpoly/m2m_test.go — TestM2M_AutoInverseFromHolder
aPosts, _ := tag.QueryPosts(ctx)        // forward
postTags, _ := post.QueryTags(ctx)      // auto-inverse (no declaration on Post)
```

## Gotchas

1. **`Through(tableName, pivot)` defaults the morph name to the singular of the table name.** `"taggables"` → `"taggable"`. If your pivot table name is irregular (`categories` → `category`? `feet`?), call `.MorphName("...")` explicitly. The handler covers `ies → y` and trailing `s`; anything else passes through unchanged.
2. **The pivot must declare its own `MorphTo`.** Without `entpoly.MorphTo("taggable", Post.Type, Video.Type)` in `Taggable.Edges()`, the M2M back-ref has nowhere to read the discriminator from. The `MorphedByMany(...).Through(...)` declaration on Tag is wired to the pivot's MorphTo by name.
3. **Auto-inverse plural defaults to `<HolderType>s`.** `MorphedByMany("posts", Post.Type)` with Tag as the holder emits `post.QueryTags(ctx)`. For irregular plurals (Category → Categories), use `.InverseName("categories")`. See the builder docs in [`edge.go`](../../edge.go) for the full option set.
4. **Attaching uses the typed pivot builder, not a method on the holder.** Laravel's `$tag->posts()->attach($post)` has no direct equivalent on `*Tag` — write a `Taggable` row instead. This is the same pattern as ent's own M2M edges with extras: the pivot is a first-class entity.

## See also

- [`entpoly/examples/basic/`](../../examples/basic/) — full runnable example with Tag + Taggable
- [`testentpoly/m2m_test.go`](../../../testentpoly/m2m_test.go) — auto-inverse + helper round trips
- [Mutations reference § many-to-many](../mutations.md)
- [Relationships reference § shape 3](../relationships.md)
- [Laravel parity](../laravel-parity.md)
