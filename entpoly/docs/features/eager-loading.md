# Eager loading — `WithCommentable()`

`WithCommentable()` (and every other `With<Morph>()` emitted per `MorphTo`) batches parent loads by morph key: one `SELECT` per parent type that has at least one child in the result set, not one per row. The Laravel equivalent is `Comment::with('commentable')->get()` — same shape, same 1+N(parent types) contract. Reach for it whenever you would otherwise issue a `QueryCommentable` inside a loop.

## When to use

- You're rendering a list of children and need each one's parent in the same response
- Forward resolves (`QueryCommentable`) in a loop produce N+1 queries
- A GraphQL resolver returns `[Comment!]!` with `commentable { ... }` selected
- You want to verify batching with the query tracer (`testentpoly/tracer.go`)

## Setup

No schema flag — the `With<Morph>()` method is emitted on every `*<Child>Query` for every `MorphTo` declared on the child.

## Wiring

None — `WithCommentable` is a regular query modifier; no hook installation required.

## Usage

```go
// Eager-load every comment's parent in one round trip per parent type.
r, err := client.Comment.Query().WithCommentable().All(ctx)
if err != nil { /* ... */ }

// The returned struct has both the comments and a map of parents keyed by child ID.
for _, c := range r.Comments {
    parent := r.Commentable[c.ID] // CommentCommentableParent — type-switch on it
    switch p := parent.(type) {
    case *ent.Post:
        fmt.Println(p.Title)
    case *ent.Video:
        fmt.Println(p.Title)
    case *ent.Image:
        fmt.Println(p.URL)
    case nil:
        // No parent (soft-deleted / orphan / not yet set).
    }
}
```

Without `WithCommentable()`, each `c.QueryCommentable(ctx)` issues its own query — the classic N+1.

## Verification

The query tracer in [`testentpoly/tracer.go`](../../../testentpoly/tracer.go) counts SELECTs per table. Confirms one SELECT per parent type that has children:

```go
// from testentpoly/eagerload_test.go — TestEagerLoad_BatchedPerType
// Seeded: 9 comments across 3 posts, 2 videos, 1 image.
tr.Reset()
r, _ := client.Comment.Query().Order(ent.Asc(comment.FieldID)).WithCommentable().All(ctx)

if got := tr.CountSelectsFrom("posts");  got != 1 { t.Errorf("posts SELECTs = %d, want 1", got) }
if got := tr.CountSelectsFrom("videos"); got != 1 { t.Errorf("videos SELECTs = %d, want 1", got) }
if got := tr.CountSelectsFrom("images"); got != 1 { t.Errorf("images SELECTs = %d, want 1", got) }
```

The full assertion lives in [`testentpoly/eagerload_test.go`](../../../testentpoly/eagerload_test.go).

## Gotchas

1. **`r.Commentable` is keyed by child ID, not parent ID.** Each child gets at most one entry; the value is the sealed-interface parent. Children whose parent is soft-deleted (when `.SoftDelete()` is on the edge) are absent from the map entirely — there's no `nil` placeholder.
2. **Soft-delete filter participates.** If the edge declares `.SoftDelete()`, soft-deleted parents drop out of the eager-load batch. The corresponding child's entry is missing from `r.Commentable`. See [Soft delete](./soft-delete.md).
3. **Eager-load runs a separate query per parent type.** Three SELECTs for a Post/Video/Image union, not one. If you need a single-query shape (e.g. for a CTE-friendly path), drop to the typed predicates and write the union yourself.
4. **The map is built in Go from the discriminator id strings.** Parse errors (e.g. mixed-PK `AllowedTypes` with the wrong decoder branch) surface inside `WithCommentable` rather than in the typed forward resolve. See [UUID parents](./uuid-parents.md) gotcha 1.

## See also

- [`testentpoly/eagerload_test.go`](../../../testentpoly/eagerload_test.go)
- [`testentpoly/tracer.go`](../../../testentpoly/tracer.go) — the query tracer used to verify batching
- [Soft delete](./soft-delete.md)
- [Predicates](./predicates.md) — forward resolve alternative
- [Architecture](../architecture.md)
