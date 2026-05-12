# Getting started

This guide walks through adding `entpoly` to a fresh ent project. By the end you will have a `Comment` that can belong to either a `Post` or a `Video` — the canonical polymorphic example — declared entirely with schema-level edges.

## Prerequisites

- Go 1.22 or newer (the module declares `go 1.26`; older toolchains will see a build error).
- An existing ent project, or a fresh one — `go run -mod=mod entgo.io/ent/cmd/ent init Post` is enough to start.
- A database driver wired into your `ent.NewClient(...)` setup. `entpoly` does not care which dialect you use; the generated columns are vanilla `id` + `type` pairs.

## Install

```bash
go get github.com/khanakia/entx/entpoly
```

If you plan to use the runtime set-diff helpers for M2M relationships:

```bash
go get github.com/khanakia/entx/entpoly/helper
```

## Declare the child schema

The child of a polymorphic relation is the entity that owns the discriminator pair (`<relation>_id` + `<relation>_type`). `entpoly` provides two ingredients you place on the child schema:

1. **`MorphMixin(name)`** in `Mixin()` — adds the two discriminator columns through ent's official mixin pipeline.
2. **`MorphTo(name, parents...)`** in `Edges()` — declares the relation and the parent types it can point at.

```go
// ent/schema/comment.go
package schema

import (
    "entgo.io/ent"
    "entgo.io/ent/schema/field"

    "github.com/khanakia/entx/entpoly"
)

type Comment struct{ ent.Schema }

// Mixin adds commentable_id + commentable_type. Without it, codegen fails
// with a clear error pointing back here.
func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable"),
    }
}

// Fields are anything specific to Comment. The discriminator columns come
// from the mixin, not here.
func (Comment) Fields() []ent.Field {
    return []ent.Field{
        field.Text("body"),
    }
}

// Edges declares the polymorphic relation. The second-onward arguments
// are parent types passed via the standard ent X.Type method-value idiom.
func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type),
    }
}
```

## Declare the parent schemas

Parents do not need any extra fields or mixins. They declare the back-reference in `Edges()`:

```go
// ent/schema/post.go
package schema

import (
    "entgo.io/ent"
    "entgo.io/ent/schema/field"

    "github.com/khanakia/entx/entpoly"
)

type Post struct{ ent.Schema }

func (Post) Fields() []ent.Field {
    return []ent.Field{
        field.String("title"),
        field.Text("body").Optional(),
    }
}

func (Post) Edges() []ent.Edge {
    return []ent.Edge{
        // "comments"       → method emitted on Post (v2 codegen).
        // Comment.Type     → child schema type (typed reference).
        // "commentable"    → relation name — must match the child's MorphTo.
        entpoly.MorphMany("comments", Comment.Type, "commentable"),
    }
}
```

`Video` follows the same pattern.

## Wire the extension into ent's codegen

Open or create `ent/entc.go`:

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
        entc.Extensions(entpoly.NewExtension(
            // WithMorphMap is OPTIONAL. Every parent type referenced
            // from a MorphTo edge auto-registers with its snake_case
            // alias (Post → "post", Video → "video"). Pass an explicit
            // map only when you want non-default aliases — typically
            // to keep persisted "*_type" values stable across a
            // Go-side rename. See docs/morph-map.md.
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

The simpler form is also valid when you are happy with snake_case defaults:

```go
opts := []entc.Option{
    entc.Extensions(entpoly.NewExtension()),
}
```

Run codegen:

```bash
go generate ./ent
```

A new file `ent/polymorphic.go` is emitted containing:

- A `Morphable` interface (`MorphID() string` + `MorphKey() string`).
- `MorphID()` / `MorphKey()` methods on every parent type (`*Post`, `*Video`, …).
- `SetCommentable(p Morphable)` and `ClearCommentable()` on every Comment builder (`Create`, `Update`, `UpdateOne`, `Mutation`).
- Runtime `morphTypeMap` plus `MorphTypeFor` / `MorphTypeName` lookups.

## Use it

```go
ctx := context.Background()
post, _ := client.Post.Create().SetTitle("Hello").Save(ctx)
video, _ := client.Video.Create().SetTitle("Demo").SetURL("https://...").Save(ctx)

// Attach a comment to the post via the typed Morphable helper.
c1, _ := client.Comment.Create().
    SetBody("Nice post!").
    SetCommentable(post).
    Save(ctx)

// Reassign across parent types.
_, _ = c1.Update().SetCommentable(video).Save(ctx)

// Clear the polymorphic parent.
_, _ = c1.Update().ClearCommentable().Save(ctx)
```

Querying the back-reference is manual in v1 (typed back-ref methods land in v2 codegen):

```go
import "your/ent/comment"

// Every comment attached to a post.
postComments, _ := client.Comment.Query().
    Where(
        comment.CommentableIDEQ(post.MorphID()),
        comment.CommentableTypeEQ("post"),
    ).
    All(ctx)
```

## Next steps

- [Relationships reference](./relationships.md) — every polymorphic shape, with full schema + usage examples.
- [Mutations reference](./mutations.md) — Laravel-to-ent translation table for every relationship verb.
- [Morph map](./morph-map.md) — how the discriminator string is resolved and the renaming workflow.
- [Architecture](./architecture.md) — how the codegen extension is structured.
- [FAQ](./faq.md) — answers to common questions about FKs, cascades, dialects, and GraphQL.
