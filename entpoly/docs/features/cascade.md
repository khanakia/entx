# `.Cascade()` — delete children with the parent

Polymorphic discriminator columns cannot carry SQL foreign keys (the column references multiple tables), so `ON DELETE CASCADE` is impossible at the database level. `.Cascade()` fills the gap with a pre-delete hook installed on every parent type in `AllowedTypes`: when a parent dies, every child row pointing at it via this `MorphTo` is deleted in the same logical operation. Reach for this when you would have written `ON DELETE CASCADE` if SQL allowed it.

## When to use

- Children are meaningless without their parent (comments on a deleted post)
- You want a single point of declaration rather than a manual cleanup at every delete site
- Deletes happen on every dialect ent supports — the hook is application-level so SQLite, MySQL, Postgres all behave identically

## Setup

```go
func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type, Image.Type).
            Cascade(),
    }
}
```

The hook is registered against every parent in `AllowedTypes`. Deleting a `Post`, `Video`, or `Image` all trigger the cascade.

## Wiring

```go
client := ent.NewClient(...)
ent.RegisterPolyHooks(client) // installs Cascade hooks on every allowed parent
```

## Usage

```go
post := client.Post.Create().SetTitle("p").SaveX(ctx)
video := client.Video.Create().SetTitle("v").SaveX(ctx)

c1 := client.Comment.Create().SetBody("a").SetCommentable(post).SaveX(ctx)
c2 := client.Comment.Create().SetBody("b").SetCommentable(post).SaveX(ctx)
c3 := client.Comment.Create().SetBody("c").SetCommentable(video).SaveX(ctx)

client.Post.DeleteOneID(post.ID).ExecX(ctx)
// c1 and c2 are gone; c3 (on video) is untouched.
```

The pre-delete hook runs BEFORE the parent delete, so no orphan window opens. If the child delete errors, the parent delete aborts and the transaction rolls back.

## Verification

```go
// from testentpoly/hooks_test.go — TestHook_Cascade
client.Post.DeleteOneID(post.ID).ExecX(ctx)
if e, _ := client.Comment.Query().Where(comment.IDEQ(c1.ID)).Exist(ctx); e {
    t.Error("c1 should have cascaded")
}
if e, _ := client.Comment.Query().Where(comment.IDEQ(c3.ID)).Exist(ctx); !e {
    t.Error("c3 (video sibling) should still exist")
}
```

## Gotchas

1. **No SQL FK is emitted.** `.Cascade()` is purely application-level. A raw `DELETE FROM posts WHERE id = ?` issued outside ent leaves orphan child rows behind — the hook only fires through `client.Post.Delete...`. If you have non-ent writers, replicate the cleanup at the SQL layer or run an integrity job.
2. **Bulk delete fires per row.** `client.Post.Delete().Where(...)` triggers the hook once per matching parent; the children for each parent are deleted in the same wrap. A million-row bulk delete is a million separate cascade queries — for large batches, consider deleting children with a single `IDIn(...)` filter first and bypassing the cascade by chunking the parent delete.
3. **The hook lives on the parent, not the child.** `RegisterPolyHooks` installs one cascade hook per (child, allowed-parent) pair — `CommentCommentableCascadeOnPostDeleteHook`, `...OnVideoDeleteHook`, `...OnImageDeleteHook`. Adding a new parent to `AllowedTypes` requires a fresh codegen + restart for the hook to wire up.
4. **Cascade is independent of `.SoftDelete()`.** `.Cascade()` hard-deletes children. If your parents are soft-deleted (a `deleted_at` update, not a row delete), the cascade hook is never invoked — you want a separate hook on the parent's update path. See [Soft delete](./soft-delete.md).

## See also

- [Required](./required.md)
- [Soft delete](./soft-delete.md) — read-side filter; complementary, not a substitute
- [Mutations reference § cascade deletes](../mutations.md)
- [Architecture](../internals/architecture.md) — why no FK
