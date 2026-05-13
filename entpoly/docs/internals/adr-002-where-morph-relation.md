# ADR-002: `whereMorphRelation` API design

**Status:** Accepted
**Date:** 2026-05-13
**Decision-makers:** entpoly maintainers

## Context

Laravel has a polymorphic-aware query operator:

```php
Comment::whereHasMorph(
    'commentable',
    [Post::class, Video::class],
    function (Builder $q, string $type) {
        $q->where('approved', true);
        if ($type === Post::class) $q->where('locked', false);
    }
)->get();
```

It filters children by per-parent-type sub-predicates that are OR'd together. The SQL shape:

```sql
SELECT * FROM comments WHERE
  (commentable_type='post'  AND commentable_id IN (SELECT id FROM posts  WHERE approved AND NOT locked))
OR
  (commentable_type='video' AND commentable_id IN (SELECT id FROM videos WHERE approved))
```

We need a typed Go equivalent. The naive "fetch matching parent IDs in Go, then filter children" pattern works but is N+1 (one query per parent type plus one for the children). A single-statement sub-query is the target.

## Two execution strategies

| | Two-step (Go) | Sub-query (raw SQL via ent's sql package) |
|---|---|---|
| Queries | 1 + N | 1 |
| Code | typed Go predicates → IDs → `IDIn(...)` | `sql.Selector` w/ EXISTS / IN-subquery |
| Composability | clean — predicates compose w/ `comment.Or(...)` | predicate.Comment as `func(*sql.Selector)` |
| Reads at scale | fine for ≤10k matching parent rows; bad for 1M | best |
| Implementation | short, typed | raw-SQL escape risk, more template logic |

The sub-query approach uses ent's `dialect/sql.Selector` to emit the join. Same machinery ent itself uses for `HasXWith` on regular edges. Worth doing — the single-query form is much cheaper at scale.

## API options considered

### Option A — fluent builder, pre-fetch (two-step)

```go
matcher, _ := ent.CommentCommentableMatcher(ctx, client).
    OnPost(post.PublishedEQ(true), post.LockedEQ(false)).
    OnVideo(video.PublishedEQ(true)).
    Build(ctx)

comments, _ := client.Comment.Query().Where(matcher).All(ctx)
```

- **Pro:** typed per-parent predicates; explicit `Build` step shows intent
- **Con:** two-call pattern; needs `ctx+client` early; still 1+N queries

### Option B — fluent builder, sub-query (one-step)

```go
comments, _ := client.Comment.Query().Where(
    ent.CommentCommentableMatch().
        OnPost(post.PublishedEQ(true), post.LockedEQ(false)).
        OnVideo(video.PublishedEQ(true)),
).All(ctx)
```

- **Pro:** 1 query, no `ctx+client` plumbing, chains in `.Where()`
- **Con:** new wrapper builder type per (child × relation); user has to learn one more idiom

### Option C — per-parent predicate constructors + manual OR (chosen)

```go
comments, _ := client.Comment.Query().Where(
    comment.Or(
        ent.CommentCommentableOnPost(post.PublishedEQ(true), post.LockedEQ(false)),
        ent.CommentCommentableOnVideo(video.PublishedEQ(true)),
    ),
).All(ctx)
```

- **Pro:** smallest codegen surface — one helper per (child × allowed parent); no new types; user composes with the `Or` / `And` they already know; typed per arm
- **Con:** for multi-type filters user writes the `Or` explicitly

### Option D — closure with type token

```go
comments, _ := client.Comment.Query().Where(
    ent.CommentCommentableMatch(func(t ent.MorphKey) ent.PolyMatch {
        switch t {
        case ent.PostMorphKey:  return ent.PolyMatch{Post: []predicate.Post{post.PublishedEQ(true)}}
        case ent.VideoMorphKey: return ent.PolyMatch{Video: []predicate.Video{video.PublishedEQ(true)}}
        }
        return ent.PolyMatch{}
    }),
).All(ctx)
```

- **Pro:** closest mirror of Laravel's `function ($q, $type)`
- **Con:** ugly in Go — closure with switch, per-arm typing requires a heterogeneous `PolyMatch` struct

### Option E — single-type helpers, no multi-type API at all

```go
comments, _ := client.Comment.Query().Where(
    ent.CommentCommentableOnPost(post.PublishedEQ(true), post.LockedEQ(false)),
).All(ctx)
// only Post-attached, filtered
```

- **Pro:** trivial; same as Option C without the assumption that users want multi-type
- **Con:** no syntactic affordance for "across types"; user composes with `Or` exactly the same way as Option C

## Comparison matrix

| | A | B | C | D | E |
|---|---|---|---|---|---|
| Queries per call | 1+N | 1 | 1 | 1 | 1 |
| Typed per-parent predicates | ✅ | ✅ | ✅ | partial (heterogeneous struct) | ✅ |
| Single-statement SQL | ❌ | ✅ | ✅ | ✅ | ✅ |
| No new wrapper types | ❌ | ❌ | ✅ | ❌ | ✅ |
| Composability with `.Where()` | ✅ (after Build) | ✅ | ✅ | ✅ | ✅ |
| Multi-type call without manual Or | ❌ (matcher API) | ✅ | ❌ | ✅ | ❌ |
| Mirrors Laravel closure shape | ❌ | partial | ❌ | ✅ | ❌ |
| Codegen surface | per child + per parent + builder type | per child + per parent + builder type | per (child × parent) | per child + Match type + parser | per (child × parent) |

## Decision

**Option C** — per-parent predicate constructors, user composes the OR.

### Why

1. **Smallest codegen surface.** One helper per (child × allowed parent). No new wrapper types, no closure parsing, no separate builder API. Reuses ent's existing `predicate.Or` / `predicate.And` idiom that users already know.
2. **Pure typed Go.** Each helper takes the typed predicates of its own parent (`...predicate.Post`). No string keys, no `any`-typed closure params. Compile-time guarantee that you can't pass a `predicate.Video` to the Post helper.
3. **Single query via ent's `sql.Selector`.** Same mechanism ent uses for `HasXWith` on regular edges; not a new SQL surface, just polymorphic-aware. ent already escapes the values; no injection risk.
4. **Composable with everything else.** Drops into any existing `.Where(...)` chain. User can `comment.And` with their own scalar filters on the child too.

The "user writes the Or" cost is real but small. Typical use is single-type filter ("comments on published posts only"). When multi-type composition is needed, the explicit `Or` is exactly what readers expect:

```go
comment.Or(
    ent.CommentCommentableOnPost(post.PublishedEQ(true)),
    ent.CommentCommentableOnVideo(video.PublishedEQ(true)),
)
```

If demand for a one-call multi-type form emerges later, we can layer Option B on top — `MatchAny(...)` that takes pre-built per-parent predicates and OR's them. Option C is the load-bearing primitive; B becomes sugar.

## Resolved sub-questions

### Naming

**`CommentCommentableOnPost(...)`** — `On<Parent>` prefix reads as English ("comments **on** posts where published"). Matches the spatial intuition of polymorphic — children sit ON parents.

Alternatives rejected: `MatchPost` (too verb-ish), `WithPost` (clashes with eager-load `WithCommentable`), `ForPost` (ambiguous w/ `for` loops in user code).

### Empty predicates

**Zero arguments = type filter only** — all rows of that parent type.

```go
ent.CommentCommentableOnPost()   // every comment attached to any Post
ent.CommentCommentableOnPost(post.PublishedEQ(true))  // ...where the Post is published
```

This is the most useful default. Users who want "all rows including those with no parent" just don't add the helper at all.

### Soft-delete honoring

**Yes — when `MorphTo.SoftDelete()` was declared, the sub-query auto-includes the soft-delete-IsNil filter for that parent.**

Consistent with the other read paths (`QueryCommentable`, `WithCommentable`) that already filter soft-deleted parents. If the user wants to include soft-deleted rows in a specific match, they can stack a custom override (or query the child rows directly without the helper).

## Generated surface for the chosen design

For each (child × allowed parent) pair:

```go
// CommentCommentableOnPost returns a predicate.Comment that matches
// rows whose polymorphic "commentable" parent is a Post satisfying
// every passed predicate. Zero predicates = match every Post-typed
// comment. When MorphTo("commentable").SoftDelete() was declared on
// Comment, the sub-query auto-skips Posts with non-null deleted_at.
func CommentCommentableOnPost(preds ...predicate.Post) predicate.Comment {
    return predicate.Comment(func(s *sql.Selector) {
        sub := sql.Select(post.FieldID).From(sql.Table(post.Table))
        for _, p := range preds { p(sub) }
        // soft-delete filter (if Post has the column and MorphTo.SoftDelete is set):
        sub.Where(sql.IsNull(post.FieldDeletedAt))
        s.Where(sql.And(
            sql.EQ(s.C(comment.FieldCommentableType), string(PostMorphKey)),
            sql.In(s.C(comment.FieldCommentableID), sub),
        ))
    })
}
```

A typical user query for "comments on published posts OR videos with > 1000 views":

```go
client.Comment.Query().Where(
    comment.Or(
        ent.CommentCommentableOnPost(post.PublishedEQ(true)),
        ent.CommentCommentableOnVideo(video.ViewsGT(1000)),
    ),
).All(ctx)
```

Single query. Type-safe per arm. Composable with the rest of `.Where()`.

## When to revisit

- If users frequently write multi-type `Or` calls, add `CommentCommentableOnAny(...)` sugar that accepts pre-built per-parent predicates and OR's them
- If the parent table is huge and the sub-query becomes a perf bottleneck, expose a two-step matcher (Option A) as `PreloadingMatcher` alongside the predicate helpers
- If we add `CommentCommentableOnPostMixed(...)` style (combine multiple parents in one sub-query) — unlikely; the OR pattern composes cleanly already
