# entpoly basic example

A complete polymorphic schema set demonstrating every relationship type:

- **MorphMany** — `Post` has many `Comment`s, `Video` has many `Comment`s
- **MorphOne** — `Post` has a featured `Image`
- **MorphedByMany** — `Tag` attaches to many `Post`s and `Video`s via the `Taggable` pivot

Every relation is declared at schema level: a `MorphMixin` in `Mixin()` adds the discriminator columns, and a `MorphTo` / `MorphMany` / `MorphOne` / `MorphedByMany` edge in `Edges()` declares the relation itself. No annotations, no field-spreads.

## Layout

```
ent/
  entc.go             ← codegen entry point, registers entpoly extension
  generate.go         ← go:generate directive
  schema/
    post.go           ← parent (back-refs in Edges)
    video.go          ← parent
    image.go          ← polymorphic child (MorphMixin + MorphTo "imageable")
    comment.go        ← polymorphic child (MorphMixin + MorphTo "commentable")
    tag.go            ← M2M holder (MorphedByMany)
    taggable.go       ← M2M pivot (MorphMixin + MorphTo "taggable")
```

## Generate

From this directory:

```bash
go generate ./ent
```

This emits the typed client + a sidecar `ent/polymorphic.go` containing:

- A `Morphable` interface
- `MorphID()` / `MorphKey()` methods on every parent type — `*Post`, `*Video`, `*Image`
- `SetCommentable(p Morphable)` / `ClearCommentable()` on every Comment builder
- Same `Set*` / `Clear*` for the other polymorphic relations
- `MorphTypeFor` / `MorphTypeName` lookups + the runtime `morphTypeMap`

## Usage sketch

```go
ctx := context.Background()
post, _ := client.Post.Create().SetTitle("Hello").Save(ctx)

// Set the polymorphic parent via the typed Morphable helper.
comment, _ := client.Comment.Create().
    SetBody("Nice post!").
    SetCommentable(post).
    Save(ctx)

// Reassign to another parent type.
video, _ := client.Video.Create().SetTitle("Demo").SetURL("...").Save(ctx)
_, _ = comment.Update().SetCommentable(video).Save(ctx)

// Clear it.
_, _ = comment.Update().ClearCommentable().Save(ctx)

// Read back-refs (v1) — use the typed predicate package directly:
import "github.com/khanakia/entx/entpoly/examples/basic/ent/comment"

postComments, _ := client.Comment.Query().Where(
    comment.CommentableIDEQ(post.MorphID()),
    comment.CommentableTypeEQ("post"),
).All(ctx)
```

## Tag a post via the polymorphic pivot

```go
import "github.com/khanakia/entx/entpoly/examples/basic/ent/taggable"

golang, _ := client.Tag.Create().SetName("golang").Save(ctx)

// Insert a pivot row pointing at the post.
_, _ = client.Taggable.Create().
    SetTagID(golang.ID).
    SetTaggable(post).
    SetAddedBy("aman").
    Save(ctx)

// Read all tags attached to a post:
pivots, _ := client.Taggable.Query().Where(
    taggable.TaggableIDEQ(post.MorphID()),
    taggable.TaggableTypeEQ("post"),
).All(ctx)
```

## Notes

- Polymorphic id columns are strings by default. Override per relation with the matching `MixinIDType("int")` + `.IDType("int")` pair.
- No foreign keys are emitted on the polymorphic columns (the only way the same column can target multiple tables).
- A composite index on `(<name>_type, <name>_id)` is recommended for read performance — declare it manually via `Indexes()` until v2 emits it automatically.
- Typed back-refs (`post.QueryComments()`) land in v2 codegen. v1 expects callers to use the typed predicate package as shown above.
