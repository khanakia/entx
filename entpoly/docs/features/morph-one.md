# `MorphOne` — exactly-one polymorphic back-reference

`MorphOne(name, child, morphName)` declares a parent-side back-reference that returns a single child (or nil) rather than a query. The canonical case is a featured image — `post.QueryFeaturedImage(ctx) (*Image, error)` rather than `(*ImageQuery)`. Reach for it when the polymorphic relationship is one-to-one in your domain semantics.

## When to use

- A parent has at most one of the polymorphic child (featured image, avatar, primary contact)
- You want the read API to return `(*Child, error)` directly
- The "at most one" constraint should be enforced by an application-level unique index

## Setup

Child schema — same as `MorphMany` (the discriminator pair):

```go
// ent/schema/image.go
type Image struct{ ent.Schema }

func (Image) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("imageable",
            entpoly.MixinAllowed(Post.Type, User.Type),
        ),
    }
}

func (Image) Fields() []ent.Field {
    return []ent.Field{field.String("url")}
}

func (Image) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("imageable", Post.Type, User.Type),
    }
}
```

Parent schema — `MorphOne` instead of `MorphMany`:

```go
// ent/schema/post.go
func (Post) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphOne("featured_image", Image.Type, "imageable"),
    }
}
```

For database-enforced uniqueness, add a unique composite index on the child:

```go
func (Image) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("imageable_type", "imageable_id").Unique(),
    }
}
```

## Wiring

None — `MorphOne` is read-side only.

## Usage

```go
img, _ := client.Image.Create().
    SetURL("https://example.com/cover.png").
    SetImageable(post).
    Save(ctx)

// Laravel: $post->image
featured, err := post.QueryFeaturedImage(ctx)
// (nil, nil) when the post has no featured image; (*Image, nil) when it does.
if err != nil { /* ... */ }
if featured == nil {
    // no featured image yet
}
```

## Verification

For the storage-layer assertion that `MorphOne` and `MorphMany` produce **the same schema** (they differ only at query time), inspect the SQL — both shapes emit the same `imageable_id` / `imageable_type` columns with the same composite index:

```sql
SELECT sql FROM sqlite_master WHERE name = 'images';
-- same DDL regardless of MorphOne vs MorphMany on the parent side
```

For uniqueness enforcement, the unique index turns a duplicate insert into a constraint violation at the database level.

## Gotchas

1. **`MorphOne` is a query-time constraint, not a storage-layer one.** The discriminator columns are identical to `MorphMany`. The parent's `MorphOne` back-ref simply limits the result to one row; nothing stops the application (or another writer) from inserting two children with the same `(imageable_type, imageable_id)` pair. Add the unique composite index when you need that.
2. **The relation name must match between `MorphOne` and the child's `MorphTo`.** `MorphOne("featured_image", Image.Type, "imageable")` references the child's `MorphTo("imageable", ...)` by the third arg. A typo there produces an `entpoly:` diagnostic at codegen time.
3. **`(nil, nil)` is the empty case, not an error.** When no child references the parent, `post.QueryFeaturedImage(ctx)` returns `(nil, nil)`. Treat the nil pointer as "no featured image", not "failed to query."
4. **Updating which child is the featured one is a child-side mutation.** Setting a new featured image is `client.Image.UpdateOneID(newImg.ID).SetImageable(post).Save(ctx)` plus (typically) clearing or deleting the previous one. The parent has no `SetFeaturedImage(child)` builder — by design, since the discriminator lives on the child.

## See also

- [Relationships reference § shape 1](../relationships.md)
- [`entpoly/examples/basic/schema/post.go`](../../examples/basic/schema/post.go) — featured image declaration
- [Eager loading](./eager-loading.md) — `WithFeaturedImage` works the same way
- [`edge.go`](../../edge.go) — `MorphOne` builder
