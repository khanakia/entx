# `.Required()` — reject unset / cleared writes

Polymorphic discriminator columns are nullable by default so the `Clear<Morph>()` helper can dissociate a child from its parent. `.Required()` adds a runtime hook that rejects writes which leave the relation unset on Create or explicitly clear it on Update — the Laravel equivalent of marking the relation non-nullable at the model layer.

## When to use

- The relation must always point at some parent (a `Comment` with no `commentable` is meaningless)
- You want an early, typed rejection rather than a downstream `NOT NULL` constraint that fires from the database
- A future caller might accidentally `ClearCommentable()` and you want that path closed

## Setup

```go
// ent/schema/comment.go
func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type, Image.Type).
            Required(),
    }
}
```

`Required()` is independent of `MixinAllowed(...)` and composes freely with `.Touch()`, `.Cascade()`, `.SoftDelete()`, `.GQL()` on the same edge — see [`testentpoly/schema/comment.go`](../../../testentpoly/schema/comment.go) for all five stacked.

## Wiring

The hook is dormant until `RegisterPolyHooks` is called on the client:

```go
client := ent.NewClient(...)
ent.RegisterPolyHooks(client) // installs every Required + Touch + Cascade hook
```

Without that call `.Required()` is silently advisory — the schema still compiles, but writes that omit the discriminator succeed against the database.

## Usage

```go
// Rejected — no parent set.
_, err := client.Comment.Create().SetBody("orphan").Save(ctx)
// err.Error() contains:
//   entpoly: Comment.commentable is required — call SetCommentable(parent) before Save

// Rejected — explicit clear on update.
_, err = c.Update().ClearCommentable().Save(ctx)
// err.Error() contains:
//   entpoly: cannot ClearCommentable() — Comment.commentable is Required()

// Accepted.
post := client.Post.Create().SetTitle("p").SaveX(ctx)
c, _ := client.Comment.Create().SetBody("ok").SetCommentable(post).Save(ctx)
```

The hook returns errors wrapped in the sentinel `errEntPolyRequired`, so callers can detect with `errors.Is(err, ent.ErrPolyRequired)` if that symbol is re-exported in your project (the sentinel is unexported in the generated package; in practice the substring `"is required"` is what tests look for).

## Verification

```go
// scenario 9a from testentpoly
if _, err := client.Comment.Create().SetBody("orphan").Save(ctx); err == nil {
    t.Fatal("expected error on Create without SetCommentable, got nil")
} else if !strings.Contains(strings.ToLower(err.Error()), "required") {
    t.Errorf("error %q should mention 'required'", err.Error())
}
```

Full assertions live in [`testentpoly/hooks_test.go`](../../../testentpoly/hooks_test.go) — `TestHook_Required`.

## Gotchas

1. **Forgetting `RegisterPolyHooks(client)` silently disables enforcement.** The schema still declares `.Required()`, codegen still emits the hook, but the hook is never installed on the client. Writes that omit the discriminator succeed. If you have a test that asserts the rejection, ensure your `openTestClient` helper calls `RegisterPolyHooks` (the testentpoly harness does this in `openTestClient`).
2. **`.Required()` does NOT make the columns `NOT NULL` at the database level.** The `<rel>_id` / `<rel>_type` columns are still nullable so the `Clear<Morph>()` codegen helper compiles. The constraint is enforced in the ent hook, not in DDL.
3. **A transactional client needs its own hook installation.** `RegisterPolyHooks(client)` installs the hooks on the root client; if you start a long-running transaction with `client.Tx(ctx)`, ent re-uses the same hooks, but a manually-constructed `*ent.Tx` (rare) needs the per-relation hook re-applied via the exposed `Comment<Rel>RequiredHook()` getter.

## See also

- [Touch](./touch.md) — pairs cleanly via `RegisterPolyHooks`
- [Mutations reference](../mutations.md) — full Laravel-to-ent mutation table
- [Architecture](../architecture.md) — hook ordering details
- [Getting started](../getting-started.md)
