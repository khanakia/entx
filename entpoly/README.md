# entpoly

Laravel-style polymorphic relationships for [ent](https://entgo.io). Declares `MorphOne`, `MorphMany`, `MorphTo`, and `MorphedByMany` as **schema-level edges** that look and feel exactly like ent's built-in `edge.To` / `edge.From`.

```go
func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{entpoly.MorphMixin("commentable")}
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type),
    }
}

func (Post) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphMany("comments", Comment.Type, "commentable"),
        entpoly.MorphOne("featured_image", Image.Type, "imageable"),
    }
}

func (Tag) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphedByMany("posts",  Post.Type).Through("taggables", Taggable.Type),
        entpoly.MorphedByMany("videos", Video.Type).Through("taggables", Taggable.Type),
    }
}
```

That's it. No annotations, no field spreads, no manual column declarations. The mixin adds the discriminator columns; the edge declares the relation; the codegen extension wires the typed `SetCommentable(p Morphable)` / `ClearCommentable()` methods onto every Comment builder.

## Why

ent does not support polymorphic relationships natively ([ent/ent#1048](https://github.com/ent/ent/issues/1048), open since 2020). `entpoly` fills that gap with the smallest possible schema surface and codegen that emits typed Go helpers — no `interface{}` gymnastics, no per-project boilerplate.

## Relationship types

| Type | What | Example |
|---|---|---|
| `MorphTo` | child holds `<name>_id` + `<name>_type` | a `Comment` belongs to a `Post` *or* a `Video` |
| `MorphOne` | parent has one child per type | a `Post` has one `Image` (featured image) |
| `MorphMany` | parent has many children per type | a `Post` has many `Comment`s |
| `MorphedByMany` | M2M holder back-ref via pivot | a `Tag` attaches to many `Post`s or `Video`s |

## Install

```bash
go get github.com/khanakia/entx/entpoly
```

## Quick start

### 1. Declare schemas (edges-only API)

The **child** uses a mixin for the discriminator columns and an edge for the relation:

```go
// ent/schema/comment.go
import "github.com/khanakia/entx/entpoly"

type Comment struct{ ent.Schema }

func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable"),
    }
}

func (Comment) Fields() []ent.Field {
    return []ent.Field{
        field.Text("body"),
    }
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type),
    }
}
```

The **parent** only declares its back-reference. No fields, no mixin:

```go
// ent/schema/post.go
type Post struct{ ent.Schema }

func (Post) Fields() []ent.Field {
    return []ent.Field{field.String("title")}
}

func (Post) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphMany("comments", Comment.Type, "commentable"),
    }
}
```

### 2. Register the extension in `ent/entc.go`

```go
//go:build ignore

package main

import (
    "log"

    "entgo.io/ent/entc"
    "entgo.io/ent/entc/gen"

    "github.com/khanakia/entx/entpoly"
)

func main() {
    opts := []entc.Option{
        // WithMorphMap is optional — every parent type referenced
        // from a MorphTo edge auto-registers with a snake_case alias
        // ("Post" → "post"). Pass an explicit map only to override
        // the default (e.g. to keep "post" stable across a Go-side
        // rename to Article). See docs/morph-map.md.
        entc.Extensions(entpoly.NewExtension(
            entpoly.WithMorphMap(map[string]string{
                "post":  "Post",
                "video": "Video",
            }),
        )),
    }
    if err := entc.Generate("./schema", &gen.Config{}, opts...); err != nil {
        log.Fatalf("ent codegen: %v", err)
    }
}
```

Run `go generate ./ent`. A new file `ent/polymorphic.go` is emitted alongside ent's normal output.

### 3. Use the generated surface

```go
ctx := context.Background()
post, _ := client.Post.Create().SetTitle("Hello").Save(ctx)

// SetCommentable accepts any parent that satisfies Morphable.
c, _ := client.Comment.Create().
    SetBody("Nice!").
    SetCommentable(post).
    Save(ctx)

// Reassign to a different parent type.
video, _ := client.Video.Create().SetTitle("Demo").SetURL("...").Save(ctx)
_, _ = c.Update().SetCommentable(video).Save(ctx)

// Clear the polymorphic parent.
_, _ = c.Update().ClearCommentable().Save(ctx)

// Read back-refs manually (v1) using ent's typed predicate package:
import "your/ent/comment"

postComments, _ := client.Comment.Query().
    Where(
        comment.CommentableIDEQ(post.MorphID()),
        comment.CommentableTypeEQ("post"),
    ).
    All(ctx)
```

## Custom column names

By default the discriminator columns are `<relation>_id` and `<relation>_type`. Override them via matching options on the mixin **and** edge — both must agree:

```go
func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable",
            entpoly.MixinIDColumn("parent_id"),
            entpoly.MixinTypeColumn("parent_type"),
            entpoly.MixinIDType("int"),
        ),
    }
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type).
            IDColumn("parent_id").
            TypeColumn("parent_type").
            IDType("int"),
    }
}
```

A mismatch surfaces at codegen time with a precise error message pointing at which option to add.

## Foreign keys

Polymorphic columns reference multiple tables, so SQL cannot enforce a FK constraint. `entpoly` therefore emits **no** foreign keys on the discriminator pair. Pair with [`entcascade`](../entcascade) for application-level cascade deletes when you need them.

`MorphMixin` emits a default composite index on `(<name>_type, <name>_id)` to keep back-ref reads fast at scale. Opt out via `entpoly.MixinNoIndex()` if a different access pattern dominates your workload.

## Laravel parity — full reference

Every Laravel polymorphic relationship operation maps to a typed ent equivalent. **No `any`, no manual SQL, no string-typed predicates.**

### Read

| Laravel | entpoly | Return type |
|---|---|---|
| `$post->image` (MorphOne) | `post.QueryFeaturedImage(ctx)` | `(*Image, error)` — `(nil, nil)` if not set |
| `$post->comments` (MorphMany) | `post.QueryComments()` | `*CommentQuery` (composable: `.Where`/`.Order`/`.Limit`/`.All`) |
| `$comment->commentable` (MorphTo reverse) | `comment.QueryCommentable(ctx)` | **sealed `CommentCommentableParent` interface** — type-switch over Post/Video only |
| `$comment->commentable_id`, `$comment->commentable_type` | `*c.CommentableID`, `*c.CommentableType` | `*string`, `*comment.CommentableType` (typed enum) |
| `$tag->posts` (MorphedByMany) | manual pivot query | `[]*Post` via `client.Taggable.Query().Where(...).All(ctx)` (v1) |

### Write

| Laravel | entpoly |
|---|---|
| `$comment->commentable()->associate($post)` | `c.Update().SetCommentable(post).Save(ctx)` |
| `$comment->commentable()->dissociate()` | `c.Update().ClearCommentable().Save(ctx)` |
| `$post->comments()->save($c)` | `c.Update().SetCommentable(post).Save(ctx)` |
| `$post->comments()->create([...])` | `client.Comment.Create().SetBody(...).SetCommentable(post).Save(ctx)` |
| `$post->comments()->saveMany([$a,$b])` | `client.Comment.MapCreateBulk(rows, func(c, i){...}).Save(ctx)` |

### M2M (MorphedByMany via pivot)

| Laravel | entpoly |
|---|---|
| `$post->tags()->attach($tag)` | `client.Taggable.Create().SetTagID(tag.ID).SetTaggable(post).Save(ctx)` |
| `$post->tags()->attach($tag, ['by'=>1])` | same + `.SetAddedBy("aman")` (pivot extras are real ent fields) |
| `$post->tags()->detach($tag)` | `client.Taggable.Delete().Where(...).Exec(ctx)` |
| `$post->tags()->sync([1,2,3])` | `helper.Sync(attached, target)` → apply diff w/ Create/Delete |
| `$post->tags()->syncWithoutDetaching([1,2])` | `helper.SyncWithoutDetach(attached, target)` |
| `$post->tags()->toggle([1,2])` | `helper.Toggle(attached, target)` |
| `$post->tags()->updateExistingPivot($tagID, [...])` | `client.Taggable.Update().Where(...).SetSortOrder(5).Save(ctx)` |

### Query / predicate

| Laravel | entpoly |
|---|---|
| `Comment::whereHasMorph('commentable', [Post::class], $q)` | `client.Comment.Query().Where(ent.CommentCommentableIs(post)).All(ctx)` |
| `Comment::where('commentable_type', Post::class)` | `client.Comment.Query().Where(ent.CommentCommentableIsType(ent.PostMorphKey)).All(ctx)` |
| Manual SQL: `WHERE commentable_type = 'post'` | typed predicate (see above) |

### Morph map (alias control)

| Laravel | entpoly |
|---|---|
| `Relation::enforceMorphMap(['post'=>'App\Models\Post'])` | `entpoly.WithMorphMap(map[string]string{"post":"Post"})` in `entc.go` (optional — snake_case default) |
| `$post->getMorphClass()` | `post.MorphKey()` → typed `MorphKey` constant |

## Reverse resolve — `$comment->commentable` in Go terms

The most-asked-about feature. Laravel returns "Post | Video | null"; we return a sealed interface that the compiler validates:

```go
parent, err := comment.QueryCommentable(ctx)
if err != nil { return err }

switch p := parent.(type) {
case *ent.Post:
    fmt.Println(p.Title)        // typed *Post — IDE completes, compiler checks
case *ent.Video:
    fmt.Println(p.URL)          // typed *Video
case nil:
    // discriminator pair was NULL
}
// `case *ent.Article:` would NOT compile — Article is not in AllowedTypes
```

The sealed interface (`CommentCommentableParent`) accepts only types listed in `MorphTo(name, Post.Type, Video.Type)`. Any other type fails at compile time with `cannot use *ent.X as CommentCommentableParent value (missing method isCommentCommentableParent)`.

## Type-safety summary

Three layers, all compile- or DB-enforced:

| Layer | Catches |
|---|---|
| **Sealed interface** (Go compile time) | `SetCommentable(article)` — wrong parent type |
| **ent enum runtime validator** | `SetCommentableType("psot")` — typo'd morph key |
| **DB CHECK / native ENUM** | `INSERT ... commentable_type='random'` — raw SQL bypass |

See [ADR-001: Type-safety strategy](./docs/adr-001-type-safety.md) for the full design rationale, alternative approaches considered, and tradeoffs.

## Documentation

| Doc | Use when |
|---|---|
| [Getting started](./docs/getting-started.md) | Adding entpoly to a fresh project |
| [Relationships reference](./docs/relationships.md) | Picking the right shape for your use case |
| [Mutations reference](./docs/mutations.md) | Translating from Laravel verbs to ent builders |
| [Morph map](./docs/morph-map.md) | Discriminator strings + the rename workflow |
| [Architecture](./docs/architecture.md) | How the extension is built; how to contribute |
| [FAQ](./docs/faq.md) | Common questions about FKs, cascades, dialects, GraphQL |
| [Laravel parity](./docs/laravel-parity.md) | Every Laravel polymorphic operation → entpoly equivalent (read, write, M2M, queries, morph map) |
| [ADR-001: Type-safety strategy](./docs/adr-001-type-safety.md) | Why we use sealed interface + enum column (Approach C) — diagrams, tradeoffs, alternatives we rejected |

## License

MIT
