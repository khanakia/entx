# `.SoftDelete()` — hide soft-deleted parents from reverse resolves

Many schemas mark "deleted" rows with a `deleted_at` timestamp instead of removing them. `.SoftDelete()` makes every reverse resolve (`QueryCommentable`, eager-load `WithCommentable`, M2M holder back-refs) skip parents whose soft-delete column is non-null. The filter is **per-target auto-detected**: only parents that actually declare the field get the predicate; the rest pass through unfiltered. Reach for this when your parent schemas already implement soft-delete (via a mixin or hand-rolled column) and you want the polymorphic read path to honour it.

## When to use

- Parent rows are soft-deleted (`UPDATE posts SET deleted_at = now()`) rather than removed
- Reverse-resolving a child should treat a soft-deleted parent as gone
- Different allowed parents have different soft-delete columns — or some have none at all

## Setup

Default column name (`deleted_at`):

```go
func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type, Image.Type).
            SoftDelete(),
    }
}
```

Custom column name:

```go
entpoly.MorphTo("commentable", Post.Type, Video.Type).SoftDelete("removed_at")
```

The parent's column must be nullable (`Optional().Nillable()`) — see [`testentpoly/schema/post.go`](../../../testentpoly/schema/post.go):

```go
field.Time("deleted_at").Optional().Nillable()
```

## Wiring

No runtime hook is needed — the filter is compiled into the generated reverse-resolve methods at codegen time. `RegisterPolyHooks` is still required for `Required` / `Touch` / `Cascade` if you stack those on the same edge.

## Usage

```go
post := client.Post.Create().SetTitle("p").SaveX(ctx)
video := client.Video.Create().SetTitle("v").SaveX(ctx)

cp := client.Comment.Create().SetBody("on post").SetCommentable(post).SaveX(ctx)
cv := client.Comment.Create().SetBody("on video").SetCommentable(video).SaveX(ctx)

// Before soft-delete: both resolve.
parent, _ := cp.QueryCommentable(ctx) // returns *Post

// Soft-delete the post.
client.Post.UpdateOneID(post.ID).SetDeletedAt(time.Now()).SaveX(ctx)

// After soft-delete: cp's parent resolves to nil; cv's resolves to *Video.
// Video schema does not declare deleted_at, so it's never filtered.
parent, _ = cp.QueryCommentable(ctx) // (nil, nil)
parent, _ = cv.QueryCommentable(ctx) // (*Video, nil)
```

Eager-load applies the same filter:

```go
r, _ := client.Comment.Query().WithCommentable().All(ctx)
// r.Commentable[cp.ID] is absent (soft-deleted post filtered out).
// r.Commentable[cv.ID] contains *Video.
```

## Verification

```go
// from testentpoly/hooks_test.go — TestHook_SoftDelete
client.Post.UpdateOneID(post.ID).SetDeletedAt(time.Now()).SaveX(ctx)

if p, _ := cp.QueryCommentable(ctx); p != nil {
    t.Errorf("post parent should be filtered, got %+v", p)
}
if v, _ := cv.QueryCommentable(ctx); v == nil {
    t.Error("video parent should still resolve (Video has no deleted_at)")
}
```

## Gotchas

1. **Per-target auto-detection means partial coverage is silent.** If only `Post` has `deleted_at` and `Video` does not, only the `Post` resolve path filters. This is intentional — you can mix and match — but means a missing column on a parent does not produce a codegen error. If you expect every parent to filter, ensure every parent in `AllowedTypes` declares the column.
2. **The filter is read-side only.** The child's `<rel>_id` / `<rel>_type` columns still reference the soft-deleted parent at the data level. If you also want to clear the discriminator when the parent is soft-deleted, add a separate hook on the parent's soft-delete update path.
3. **The column must be nullable.** The generated filter is `<field>IsNil()` — `SELECT ... WHERE deleted_at IS NULL`. A non-nullable column never satisfies the predicate and every parent appears soft-deleted from the polymorphic read path.
4. **`.Cascade()` and `.SoftDelete()` are independent.** `.Cascade()` fires when the parent row is **hard-deleted**. Soft-deleting a parent (just updating `deleted_at`) does NOT trigger the cascade hook — the row is still there. If you want children to disappear on a soft-delete, add an update-side hook on the parent.

## See also

- [Cascade](./cascade.md) — complementary, hard-delete side
- [Mutations reference § soft delete](../mutations.md)
- [Eager loading](./eager-loading.md) — soft-delete filter participates in `WithCommentable`
- [Relationships reference](../relationships.md)
