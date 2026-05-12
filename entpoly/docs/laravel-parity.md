# Laravel parity — full reference

This document maps every Laravel polymorphic relationship operation to its `entpoly` equivalent. If you know Laravel and you are wiring an ent schema, this is the single page to skim.

The two sides are **wire-compatible**: a row written by Laravel can be read by entpoly and vice versa, as long as the morph keys match (use `Relation::enforceMorphMap` on the PHP side and `entpoly.WithMorphMap` on the Go side to make them line up).

## Schema declaration

### Laravel

```php
// app/Models/Comment.php
class Comment extends Model {
    public function commentable() {
        return $this->morphTo();
    }
}

// app/Models/Post.php
class Post extends Model {
    public function comments() {
        return $this->morphMany(Comment::class, 'commentable');
    }
}
```

### entpoly

```go
// ent/schema/comment.go
type Comment struct{ ent.Schema }

func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable", entpoly.MixinAllowed(Post.Type, Video.Type)),
    }
}
func (Comment) Fields() []ent.Field { return []ent.Field{field.Text("body")} }
func (Comment) Edges() []ent.Edge {
    return []ent.Edge{entpoly.MorphTo("commentable", Post.Type, Video.Type)}
}

// ent/schema/post.go
type Post struct{ ent.Schema }
func (Post) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphMany("comments", Comment.Type, "commentable"),
    }
}
```

## Read

### Forward — child → parent (`$comment->commentable`)

| Laravel | entpoly | Returns |
|---|---|---|
| `$comment->commentable` | `comment.QueryCommentable(ctx)` | `(CommentCommentableParent, error)` — sealed interface; `(nil, nil)` if unset |
| `$comment->commentable_id` | `*comment.CommentableID` | `*string` (or `*int64` with `IDType("int")`) |
| `$comment->commentable_type` | `*comment.CommentableType` | `*comment.CommentableType` — **typed enum**, not raw string |

The return type of `QueryCommentable` is the **sealed interface**. The Go compiler restricts the type switch to the AllowedTypes — no `any`, no `interface{}` escape hatch:

```go
parent, err := comment.QueryCommentable(ctx)
if err != nil { return err }
switch p := parent.(type) {
case *ent.Post:   /* typed *Post  */
case *ent.Video:  /* typed *Video */
case nil:         /* unset        */
}
// case *ent.Article: → COMPILE ERROR
```

### Reverse — parent → children

| Laravel | entpoly | Returns |
|---|---|---|
| `$post->comments` (MorphMany) | `post.QueryComments()` | `*CommentQuery` — composable builder |
| `$post->comments()->where('approved', true)->get()` | `post.QueryComments().Where(comment.ApprovedEQ(true)).All(ctx)` | `[]*Comment` |
| `$post->image` (MorphOne) | `post.QueryFeaturedImage(ctx)` | `(*Image, error)` — `(nil, nil)` if unset |

### Reverse — holder ↔ pivot (`MorphedByMany`)

In v1, M2M back-refs go through the pivot directly:

```go
// All tags attached to a post
pivots, _ := client.Taggable.Query().
    Where(ent.TaggableTaggableIs(post)).  // typed predicate, accepts only AllowedTypes
    All(ctx)

tagIDs := make([]int, len(pivots))
for i, p := range pivots { tagIDs[i] = p.TagID }
tags, _ := client.Tag.Query().Where(tag.IDIn(tagIDs...)).All(ctx)
```

v2 will emit `tag.QueryPosts(ctx)` / `post.QueryTags(ctx)` directly.

## Write

| Laravel | entpoly |
|---|---|
| `$c->commentable()->associate($post)` | `c.Update().SetCommentable(post).Save(ctx)` |
| `$c->commentable()->dissociate()` | `c.Update().ClearCommentable().Save(ctx)` |
| `$post->comments()->save($c)` | same as `associate`: `c.Update().SetCommentable(post).Save(ctx)` |
| `$post->comments()->create([...])` | `client.Comment.Create().SetBody("hi").SetCommentable(post).Save(ctx)` |
| `$post->comments()->saveMany([$a,$b])` | `client.Comment.MapCreateBulk(rows, func(c, i){ c.SetBody(rows[i].body).SetCommentable(post) }).Save(ctx)` |
| `$comment->touch()` (touches parent's `updated_at`) | manual via ent hook — see [docs/mutations.md](./mutations.md#touch-parents-on-child-save) |

`SetCommentable` takes the **sealed interface** as its parameter. Passing a type that isn't in `AllowedTypes` is a compile error:

```go
client.Comment.Create().SetCommentable(article)  // COMPILE ERROR
// cannot use *Article as CommentCommentableParent value (missing method isCommentCommentableParent)
```

## Many-to-many polymorphic (`MorphedByMany` + pivot)

A polymorphic M2M uses a pivot schema (e.g. `Taggable`) that has its own `MorphTo` to the parent + an int FK to the M2M target.

| Laravel | entpoly |
|---|---|
| `$post->tags()->attach($tag)` | `client.Taggable.Create().SetTagID(tag.ID).SetTaggable(post).Save(ctx)` |
| `$post->tags()->attach($tag, ['by'=>1])` | same + `.SetAddedBy("aman")` (pivot extras are real fields) |
| `$post->tags()->detach($tag)` | `client.Taggable.Delete().Where(ent.TaggableTaggableIs(post), taggable.TagID(tag.ID)).Exec(ctx)` |
| `$post->tags()->sync([1,2,3])` | `helper.Sync(attached, target)` → apply diff with Create/Delete |
| `$post->tags()->syncWithoutDetaching([1,2])` | `helper.SyncWithoutDetach(attached, target)` |
| `$post->tags()->toggle([1,2])` | `helper.Toggle(attached, target)` |
| `$post->tags()->updateExistingPivot($tagID, [...])` | `client.Taggable.Update().Where(...).SetSortOrder(5).Save(ctx)` |

### `sync` example end-to-end

Laravel:

```php
$post->tags()->sync([1, 2, 3]);
```

entpoly:

```go
// 1. Read currently-attached tag IDs.
attached, _ := client.Taggable.Query().
    Where(ent.TaggableTaggableIs(post)).
    Select(taggable.FieldTagID).Ints(ctx)

// 2. Compute the diff.
toAttach, toDetach := helper.Sync(attached, []int{1, 2, 3})

// 3. Apply the diff.
for _, tid := range toAttach {
    _, _ = client.Taggable.Create().SetTagID(tid).SetTaggable(post).Save(ctx)
}
if len(toDetach) > 0 {
    _, _ = client.Taggable.Delete().
        Where(ent.TaggableTaggableIs(post), taggable.TagIDIn(toDetach...)).
        Exec(ctx)
}
```

## Query / predicate

| Laravel | entpoly |
|---|---|
| `Comment::whereHasMorph('commentable', [Post::class], $q)` | `client.Comment.Query().Where(ent.CommentCommentableIs(post)).All(ctx)` |
| `Comment::where('commentable_type', Post::class)` | `client.Comment.Query().Where(ent.CommentCommentableIsType(ent.PostMorphKey)).All(ctx)` |
| Manual SQL `WHERE commentable_type='post'` | typed predicate above — string literals not needed |

The codegen-emitted `ent.CommentCommentableIs(parent)` takes the sealed-interface parent; `ent.CommentCommentableIsType(MorphKey)` takes the typed `MorphKey` constant. Neither accepts a raw string literal — `ent.CommentCommentableIsType("psot")` fails to compile.

## Morph map (the type-column alias)

| Laravel | entpoly |
|---|---|
| `Relation::enforceMorphMap(['post'=>'App\Models\Post'])` | `entpoly.WithMorphMap(map[string]string{"post":"Post"})` in `entc.go` |
| `$post->getMorphClass()` | `post.MorphKey()` → typed `MorphKey` constant (`ent.PostMorphKey`) |
| Default (full class name in `commentable_type`) | Default (snake_case of ent schema name) |
| Required for refactor-safety | **Optional** — auto-registers snake_case for every parent in `MorphTo` |

## Validation

| Laravel | entpoly |
|---|---|
| `protected $morphMap` enforcement | `MixinAllowed(...)` emits `field.Enum` → runtime validator + DB CHECK |
| Manual model-level validation hooks | ent hooks; reject if `*_type` outside allowed set |
| `protected $touches = ['commentable']` | manual via ent hook (see [mutations.md](./mutations.md#touch-parents-on-child-save)) |

## What Laravel has that we don't yet (v2 backlog)

| Laravel | Status in entpoly |
|---|---|
| `MorphedByMany` typed back-refs (`$tag->posts`) | v2 codegen — manual pivot query for now |
| `whereMorphRelation('commentable', ...)` w/ closure over per-type sub-queries | v2 — manual per-type composition |
| `$post->load('comments')` eager loading (single batched query) | v2 — chain `.WithComments()`-style helpers |
| `$comment->touch()` parent timestamps | manual ent hook |
| Soft-delete-aware reverse resolve | manual filter |

## What entpoly has that Laravel doesn't

| Feature | Notes |
|---|---|
| **Compile-time** Go type restriction (sealed interface) | Laravel only restricts via documentation / runtime validators |
| **Typed reverse resolver** (no `any`) | Laravel's `commentable` is loosely typed at the language level |
| **DB-level enum** for the morph_type column | Laravel uses plain VARCHAR (string column) |
| Typed morph-key constants (`ent.PostMorphKey`) | Laravel uses raw strings everywhere |
| ent's full feature set on the pivot (hooks, privacy, transactions, soft-delete) | Laravel pivots are loose dictionaries by default |

## See also

- [docs/relationships.md](./relationships.md) — schema patterns per shape
- [docs/mutations.md](./mutations.md) — per-verb write API
- [docs/morph-map.md](./morph-map.md) — alias / rename workflow
- [docs/adr-001-type-safety.md](./adr-001-type-safety.md) — design rationale, alternatives, tradeoffs
- [examples/basic/](../examples/basic) — runnable example w/ tests
