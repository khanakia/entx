# ADR-001: Type-safety strategy for polymorphic discriminators

**Status:** Accepted
**Date:** 2026-05-12
**Decision-makers:** entpoly maintainers

## Context

Every polymorphic relationship in entpoly stores a discriminator pair on the child:

| Column | Holds |
|---|---|
| `<relation>_id` | the parent row's primary key, stringified |
| `<relation>_type` | a stable string alias for the parent's ent type ("post", "video") |

A schema declares the *allowed* parent set via:

```go
entpoly.MorphTo("commentable", Post.Type, Video.Type)
```

The question this ADR answers: **how do we make sure the runtime values in those two columns are always consistent and always one of the allowed types?**

The naive approach — a `string` column with `Set<Morph>(p Morphable)` taking any `Morphable` — opens four classes of bug:

1. **Pass the wrong type.** `SetCommentable(article)` compiles even when `Article` is not allowed.
2. **Set id and type separately, mismatched.** `SetCommentableID("123").SetCommentableType("video")` when row 123 is actually a `Post`.
3. **Typo the type string.** `SetCommentableType("psot")` compiles fine.
4. **Bypass via raw SQL.** `INSERT INTO comments(commentable_type) VALUES('random')` succeeds.

We want to close as many of these as the language and ORM allow, without inventing a custom DSL or breaking ent's contract.

## Options considered

### Approach A — Sealed interface only

```go
// codegen emits one of these per child × relation:
type CommentCommentableParent interface {
    Morphable
    isCommentCommentableParent() // marker method, unexported
}

// only allowed parents get the marker:
func (*Post)  isCommentCommentableParent() {}
func (*Video) isCommentCommentableParent() {}

// the setter takes the sealed type:
func (c *CommentCreate) SetCommentable(p CommentCommentableParent) *CommentCreate
```

| Pro | Con |
|---|---|
| `SetCommentable(article)` → compile error | DB column is plain `VARCHAR` — raw SQL writes any string |
| **id + type set atomically from the same parent** | Reading `*c.CommentableType` returns plain `string` |
| Mirrors `edge.To`'s setter idiom | No GraphQL / entgql enum support |
| Single source of truth (the `MorphTo(...)` edge) | Comparing requires `string(ent.PostMorphKey)` cast |

Covers bugs #1, #2. Misses #3, #4.

### Approach B — Typed enum column only

```go
// Mixin emits field.Enum:
field.Enum("commentable_type").
    Values("post", "video").
    Optional().Nillable()

// ent generates:
type CommentableType string
const (
    CommentableTypePost  CommentableType = "post"
    CommentableTypeVideo CommentableType = "video"
)
func CommentableTypeValidator(ct CommentableType) error { /* runtime check */ }
```

| Pro | Con |
|---|---|
| **DB enforces** allowed set via `CHECK`/`ENUM` | Setter still takes id + type separately — can mismatch them |
| Reading returns typed value (`CommentableType`, not `string`) | No `SetCommentable(parent)` helper; user wires id + type manually |
| ent generates typed predicates: `CommentableTypeEQ(CommentableTypePost)` | Mixin must know AllowedTypes (DRY: lives on the edge too) |
| GraphQL/entgql auto-enum: `enum CommentableType { POST VIDEO }` | Adding a parent type = DB migration |
| Runtime validator catches bad writes before the DB sees them | Renames need a migration step |

Covers bugs #3, #4. Misses #1 (partially), #2.

### Approach C — Both

Sealed interface for the write path + enum-typed column for the read path & database. Each layer covers what the other can't.

| Bug class | A (interface) | B (enum) | C (both) |
|---|---|---|---|
| #1 Wrong parent type | ✅ compile error | ❌ id+type still settable | ✅ |
| #2 Mismatched id/type | ✅ atomic write | ❌ | ✅ |
| #3 Typo'd type string | ❌ | ✅ validator + typed constant | ✅ |
| #4 Raw SQL bypass | ❌ | ✅ DB CHECK | ✅ |

## Decision

**Approach C.** Ship both layers, with `MixinAllowed(...)` opting into the enum column. When `MixinAllowed` is set, the column becomes a real enum at every level of the stack — Go type, runtime validator, database constraint, generated predicate.

## How the layers stack

```
┌──────────────────────────────────────────────────────────────────────┐
│ Application code                                                     │
│                                                                      │
│   client.Comment.Create().SetCommentable(post).Save(ctx)             │
│                              ▲                                       │
│                              │ takes CommentCommentableParent        │
│                              │ (sealed interface — Article fails     │
│                              │  to compile because it has no marker  │
│                              │  method isCommentCommentableParent)   │
└──────────────────────────────┼───────────────────────────────────────┘
                               │
                               ▼ Layer 1: compile-time
┌──────────────────────────────────────────────────────────────────────┐
│ entpoly sidecar (polymorphic.go)                                     │
│                                                                      │
│   type CommentCommentableParent interface {                          │
│     Morphable                                                        │
│     isCommentCommentableParent()                                     │
│   }                                                                  │
│   func (*Post) isCommentCommentableParent() {}                       │
│   func (*Video) isCommentCommentableParent() {}                      │
│                                                                      │
│   func (c *CommentCreate) SetCommentable(p CommentCommentableParent) │
│       *CommentCreate {                                               │
│     return c.SetCommentableID(p.MorphID()).                          │
│              SetCommentableType(comment.CommentableType(             │
│                  string(p.MorphKey())))                              │
│   }                                                                  │
└──────────────────────────────┼───────────────────────────────────────┘
                               │
                               ▼ Layer 2: ent runtime validator
┌──────────────────────────────────────────────────────────────────────┐
│ ent generated (comment/comment.go)                                   │
│                                                                      │
│   type CommentableType string                                        │
│   const CommentableTypePost CommentableType = "post"                 │
│   const CommentableTypeVideo CommentableType = "video"               │
│                                                                      │
│   func CommentableTypeValidator(ct CommentableType) error {          │
│     switch ct {                                                      │
│     case CommentableTypePost, CommentableTypeVideo: return nil       │
│     default: return fmt.Errorf("invalid enum value: %q", ct)         │
│     }                                                                │
│   }                                                                  │
└──────────────────────────────┼───────────────────────────────────────┘
                               │
                               ▼ Layer 3: database
┌──────────────────────────────────────────────────────────────────────┐
│ Generated migration                                                  │
│                                                                      │
│   commentable_type ENUM('post','video')   -- MySQL/PG enum type      │
│   -- OR --                                                           │
│   commentable_type VARCHAR(...) CHECK (commentable_type IN           │
│                                        ('post','video'))   -- SQLite │
└──────────────────────────────────────────────────────────────────────┘
```

## How each bug is caught

```
Bug #1: SetCommentable(article)
    │
    ▼  cannot use *Article as CommentCommentableParent
       (missing method isCommentCommentableParent)
    Layer 1 ✋ caught at compile time


Bug #2: c.SetCommentableID(post.ID).SetCommentableType("video")
    │
    ▼ Application bypasses SetCommentable wrapper
    │ id and type are independent fields → mismatch survives both
    │ Go type system and validator
    ✗ NOT caught by any layer when used directly
    │
    │ Mitigation: deprecate raw setters / lint usage
    │ Real protection: always use SetCommentable(post)


Bug #3: c.SetCommentableType("psot")
    │  But SetCommentableType actually takes CommentableType (named
    │  type), not string. Direct string literal fails to compile.
    ▼  Caller must cast: SetCommentableType(comment.CommentableType("psot"))
    │  Cast succeeds (any string can be cast), then:
    ▼  ent runtime validator: invalid enum value "psot"
    Layer 2 ✋ caught at Save(ctx)


Bug #4: INSERT INTO comments(commentable_type) VALUES('random')
    │
    ▼  Direct SQL bypasses Go entirely
    Layer 3 ✋ DB CHECK / ENUM constraint rejects the row
```

## Why not just Approach A?

The sealed interface alone covers the two highest-impact bugs (#1, #2) but leaves the database open to bad data. Any team with multiple services writing to the same DB — or any direct SQL admin task — can introduce invalid `*_type` values. We do not want entpoly to depend on "everyone uses the typed setter."

## Why not just Approach B?

The enum-typed column hardens the read path and the database, but it does not solve the most common write-side bug: setting `id` and `type` independently and getting them mismatched. Without the sealed-interface setter, every caller has to remember the discipline. A typical Laravel polymorphic bug looks like:

```php
$comment->commentable_id   = $post->id;
$comment->commentable_type = 'Video';  // wrong; should be 'Post'
$comment->save();
```

Approach B alone would still let this compile and validate at every layer (because `'Video'` *is* a valid enum value) — the integrity violation is across the *pair*, not within a single column.

## Migration friction

Approach C has one real cost over A: changing the allowed set requires a DB migration (the enum values shift). With Approach A, adding a new allowed parent type is code-only. Tradeoff:

| Change | Approach A | Approach C |
|---|---|---|
| Add `Article` to allowed types | Edit schema, run `go generate` | Edit schema + run `go generate` + run DB migration |
| Rename `Post` → `Article` (Go-side only) | Edit WithMorphMap to keep `"post"` key | Same — keep `"post"` enum value |
| Rename morph key `"post"` → `"article"` (DB-side) | Update map, no DB change | Migration: rename enum value |

For schemas under active iteration, the migration cost is real. For schemas in steady state (most production code), the cost is negligible because the allowed set rarely changes.

The compromise: `MixinAllowed(...)` is **optional**. Schemas that need flexibility skip it and stay on Approach A (plain `field.String`). Schemas that want max safety opt in and get Approach C.

## API surface

```go
// Schema declaration:
type Comment struct{ ent.Schema }

func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable",
            entpoly.MixinAllowed(Post.Type, Video.Type),  // ← Layer 2+3
        ),
    }
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type),  // ← Layer 1
    }
}
```

The two `Post.Type, Video.Type` lists must agree. `preprocess` cross-checks them at codegen time and surfaces a precise diff if they drift apart (showing exactly which entry is missing from which side).

## Comparable systems

| System | Approach | Notes |
|---|---|---|
| Laravel Eloquent (`morphTo`) | None of A/B/C | Plain string column, no type-side validation. The `Relation::enforceMorphMap()` call locks the *alias map* but not the allowed *set*. |
| Rails ActiveRecord (`polymorphic: true`) | None of A/B/C | Plain string column. Validations are user-added per-model. |
| TypeORM (`@Polymorphic`) | A-ish | Generic typing on the entity but no runtime DB constraint. |
| Prisma (`@map`) | N/A | No first-class polymorphism — users implement via separate FK columns. |
| Hasura tracking | C-ish | Tracks allowed roots via metadata; DB enforces via separate FK shape (not polymorphic). |
| **entpoly** | **C (opt-in)** | Compile-time + runtime + DB. Strongest stack of any Go ORM polymorphism solution we are aware of. |

## What we explicitly did NOT do

- **Generate a `MorphTo()` GraphQL union resolver.** Out of scope for v1. The enum column gives entgql enough to emit a sane shape.
- **Auto-derive `MixinAllowed` from the edge.** The mixin runs at schema load, before our extension can see the graph. Threading the list through ent would couple us to ent's internal load order — fragile across versions. Explicit list in both places is the price of clean architecture.
- **DB-side foreign key on the `_id` column.** Cannot exist by definition — the column references multiple tables. This is the polymorphism tradeoff.
- **Per-type FK columns (discriminated union).** Considered as an alternative to polymorphism entirely. Lost the polymorphism / column-flexibility trade-off, but a separate user-choice that ent already supports without entpoly.

## When to revisit this decision

- If ent ships first-class polymorphism support (track [ent/ent#1048](https://github.com/ent/ent/issues/1048)), some of this code becomes redundant.
- If Go gains tagged unions / proper sum types, the sealed-interface pattern can be simplified.
- If the migration friction of Approach C proves too painful in real projects, we may make `MixinAllowed` the default OFF and add a separate `MorphMixinStrict` for opt-in safety.

## References

- entpoly source: [edge.go](../edge.go), [mixin.go](../mixin.go), [templates/polymorphic.tmpl](../templates/polymorphic.tmpl)
- ent enum field: [entgo.io/docs/schema-fields/#enum-fields](https://entgo.io/docs/schema-fields/#enum-fields)
- Go sealed-interface pattern: see Rob Pike's discussion of method sets as type discriminators
- Laravel polymorphism: [laravel.com/docs/eloquent-relationships#polymorphic-relationships](https://laravel.com/docs/eloquent-relationships#polymorphic-relationships)
- Original ent issue: [ent/ent#1048](https://github.com/ent/ent/issues/1048)
