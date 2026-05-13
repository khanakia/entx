# `.Touch()` — bump parent timestamp on child save

Laravel's `$touches = ['commentable']` bumps the parent's `updated_at` whenever a child is saved. `.Touch()` ports the same behaviour: every successful Create / Update / UpdateOne of the polymorphic child fires a same-transaction update against the polymorphic parent's timestamp column. Reach for it when audit fields on the parent must reflect any activity on the children — caches keyed off `updated_at`, ETags, "last activity" feeds.

## When to use

- A parent's `updated_at` should advance when any child mutates (comments → post, line items → invoice)
- A cache or CDN key keys off the parent's timestamp and stale children must invalidate the parent
- You want the Laravel `$touches` ergonomics without writing a per-relation hook by hand

## Setup

Default — bump `updated_at`:

```go
func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type, Image.Type).
            Touch(),
    }
}
```

Custom field — bump `modified_at` instead:

```go
entpoly.MorphTo("commentable", Post.Type, Video.Type).Touch("modified_at")
```

Every parent in `AllowedTypes` must declare the timestamp field. See [`testentpoly/schema/post.go`](../../../testentpoly/schema/post.go) for the canonical declaration:

```go
field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now)
```

## Wiring

```go
client := ent.NewClient(...)
ent.RegisterPolyHooks(client) // installs Touch alongside Required + Cascade
```

Hooks fire on `OpCreate`, `OpUpdate`, and `OpUpdateOne`. The parent update runs after the child save succeeds; a touch failure rolls back the entire mutation, matching Laravel's transactional behaviour.

## Usage

```go
post := client.Post.Create().SetTitle("hello").SaveX(ctx)
before := post.UpdatedAt

// Any successful Comment Save bumps post.UpdatedAt.
c := client.Comment.Create().SetBody("hi").SetCommentable(post).SaveX(ctx)

after := client.Post.GetX(ctx, post.ID)
// after.UpdatedAt > before — Create touched the parent

_ = c.Update().SetBody("edit").SaveX(ctx)
afterUpdate := client.Post.GetX(ctx, post.ID)
// afterUpdate.UpdatedAt > after.UpdatedAt — Update touched the parent too
```

## Verification

```go
// from testentpoly/hooks_test.go — TestHook_Touch
post := client.Post.Create().SetTitle("P").SaveX(ctx)
original := post.UpdatedAt

time.Sleep(10 * time.Millisecond)
client.Comment.Create().SetBody("hi").SetCommentable(post).SaveX(ctx)

afterCreate := client.Post.GetX(ctx, post.ID)
if !afterCreate.UpdatedAt.After(original) {
    t.Errorf("post.UpdatedAt = %v, want after %v", afterCreate.UpdatedAt, original)
}
```

## Gotchas

1. **Every allowed parent must declare the touched field.** If `Touch("modified_at")` is declared but `Video` lacks `field.Time("modified_at")`, codegen emits a `SetModifiedAt(...)` call on `*VideoUpdateOne` that doesn't exist and the Go build fails with an `undefined: SetModifiedAt` error pointing at the generated `polymorphic.go`. The fix is to add the field on every parent in `AllowedTypes` — not to special-case the schema that's missing it.
2. **`RegisterPolyHooks` must be called.** Without it the hook is dormant; the schema declaration `.Touch()` is silently advisory.
3. **Touch updates are NOT optional.** A failed parent `SaveX` (e.g. row deleted concurrently) propagates as a child-save failure. If you need best-effort touches that ignore parent-gone errors, wrap the save in your own retry / swallow logic at the call site.

## See also

- [Required](./required.md) — pairs via the same `RegisterPolyHooks` call
- [Cascade](./cascade.md) — same hook entry point, opposite direction
- [Mutations reference](../mutations.md)
- [Architecture](../architecture.md) — hook ordering and registration
