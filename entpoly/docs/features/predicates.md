# Typed predicates

entpoly emits two families of predicate constructors per `MorphTo`:

1. **Parent-bound matchers** — `ent.CommentCommentableIs(post)`, `ent.CommentCommentableIsType(ent.PostMorphKey)` — match every child whose discriminator points at a specific parent / parent type. Zero string literals at the call site.
2. **Per-parent sub-query helpers** — `ent.CommentCommentableOnPost(post.PublishedEQ(true))` — match children whose parent satisfies a typed predicate on that specific parent type. The Laravel `whereHasMorph(...)` equivalent.

Reach for these whenever you would otherwise write `comment.CommentableTypeEQ("post")` by hand — the typed forms cannot drift if you rename the parent schema and the morph key is wrapped in a generated constant.

## When to use

- Filtering children by parent in a `Where(...)` clause
- Composing multi-type predicates via `comment.Or(...)` — draft posts OR any video
- Avoiding raw string literals (`"post"`) in query call sites
- Pushing the filter into the database rather than fetching in Go and post-filtering

## Setup

No schema flag — both families are emitted automatically for every `MorphTo`. The `MorphKey` constants (`ent.PostMorphKey`, `ent.VideoMorphKey`, ...) are emitted once per registered parent.

## Wiring

None.

## Usage

Parent-bound — `Is(parent)` and `IsType(key)`:

```go
import (
    "github.com/your/proj/ent"
)

// Every comment whose parent IS this specific post.
n, _ := client.Comment.Query().Where(ent.CommentCommentableIs(post)).Count(ctx)

// Every comment whose parent IS a Post (any post).
n, _ := client.Comment.Query().Where(ent.CommentCommentableIsType(ent.PostMorphKey)).Count(ctx)
```

Per-parent sub-query — `OnPost(...)`, `OnVideo(...)`:

```go
import (
    "github.com/your/proj/ent"
    "github.com/your/proj/ent/comment"
    "github.com/your/proj/ent/post"
)

// Comments whose parent Post is published.
pub, _ := client.Comment.Query().
    Where(ent.CommentCommentableOnPost(post.PublishedEQ(true))).
    All(ctx)

// Multi-type OR — draft posts OR any video.
multi, _ := client.Comment.Query().
    Where(comment.Or(
        ent.CommentCommentableOnPost(post.PublishedEQ(false)),
        ent.CommentCommentableOnVideo(),
    )).
    All(ctx)

// Zero-predicate form — every comment whose parent is a Post.
all, _ := client.Comment.Query().Where(ent.CommentCommentableOnPost()).All(ctx)
```

The `On<Parent>(...)` helper accepts zero or more typed predicates from the parent's predicate package (`ent/post`, `ent/video`). They are AND'd together inside the sub-query, then joined to the child via a `commentable_type = 'post' AND commentable_id IN (...)` clause. See [ADR-002](../internals/adr-002-where-morph-relation.md) for the design rationale.

## Verification

Parent-bound:

```go
// from testentpoly/predicates_test.go — TestPredicates_TypedConstructors
if got := client.Comment.Query().Where(ent.CommentCommentableIs(post1)).CountX(ctx); got != 2 {
    t.Errorf("CommentCommentableIs(post1) = %d, want 2", got)
}
if got := client.Comment.Query().Where(ent.CommentCommentableIsType(ent.PostMorphKey)).CountX(ctx); got != 3 {
    t.Errorf("IsType(PostMorphKey) = %d, want 3", got)
}
```

Per-parent sub-query:

```go
// from testentpoly/predicates_test.go — TestPredicates_OnParentSubquery
pub := client.Comment.Query().
    Where(ent.CommentCommentableOnPost(post.PublishedEQ(true))).
    AllX(ctx)
// Returns only the comment on the published Post.
```

## Gotchas

1. **`MorphKey` constants are typed, not raw strings.** `ent.PostMorphKey` is `commentable.CommentableType("post")`, not `"post"`. The conversion is automatic in `IsType(...)`; if you ever need the raw string, call `string(ent.PostMorphKey)`.
2. **The per-parent helper name is fixed by `MorphedByMany.InverseName` / `MorphTo` relation name.** `CommentCommentable*` comes from the Comment schema + the `commentable` relation. Renaming the relation regenerates every helper name; expect compile errors across the codebase that you can fix with a simple search-and-replace.
3. **`On<Parent>()` with no args matches every child whose parent is that type.** This is the zero-predicate degenerate case — equivalent to `IsType(<MorphKey>)`. Use whichever reads more clearly in context.
4. **Sub-query predicates run in the database, not in Go.** The `IN (SELECT ...)` shape works on every dialect ent supports, but the parent table is queried in the same round trip — useful for scale, but every parent table needs the standard indexes on the predicates you pass.

## See also

- [ADR-002: whereMorphRelation API](../internals/adr-002-where-morph-relation.md) — design rationale for the per-parent helpers
- [`testentpoly/predicates_test.go`](../../../testentpoly/predicates_test.go)
- [Relationships reference § typed predicates](../relationships.md)
- [Eager loading](./eager-loading.md) — alternative read path
