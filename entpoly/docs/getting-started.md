# Getting started

This guide walks through adding `entpoly` to a fresh ent project. By the end you will have a `Comment` that can belong to either a `Post` or a `Video` — the canonical polymorphic example — declared entirely with schema-level edges, with typed forward and reverse traversal, an optional GraphQL union surface, and (optionally) the database-enforced enum on the type column.

## Prerequisites

- Go 1.22 or newer (the module declares `go 1.26`; older toolchains will see a build error).
- An existing ent project, or a fresh one — `go run -mod=mod entgo.io/ent/cmd/ent init Post` is enough to start.
- A database driver wired into your `ent.NewClient(...)` setup. `entpoly` does not care which dialect you use; the generated columns are vanilla `id` + `type` pairs.

## Install

```bash
go get github.com/khanakia/entx/entpoly
```

If you plan to use the runtime set-diff helpers for M2M relationships (Laravel-style `attach` / `detach` / `sync` / `toggle`):

```bash
go get github.com/khanakia/entx/entpoly/helper
```

## Declare the child schema

The child of a polymorphic relation is the entity that owns the discriminator pair (`<relation>_id` + `<relation>_type`). `entpoly` provides two ingredients you place on the child schema:

1. **`MorphMixin(name, opts...)`** in `Mixin()` — adds the two discriminator columns through ent's official mixin pipeline.
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
//
// MixinAllowed is OPTIONAL but recommended — it promotes the type column
// to a real field.Enum so the database CHECK constraint (or native ENUM
// on MySQL) and ent's runtime validator both enforce the closed parent
// set. Without it, the type column is a plain string and only the
// sealed-interface setter restricts writes — see ADR-001 for the design.
func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable",
            entpoly.MixinAllowed(Post.Type, Video.Type),
        ),
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

### `MixinAllowed` vs bare `MorphMixin`

Both are supported:

| Mode | Mixin call | Type column | DB constraint | When to pick |
|---|---|---|---|---|
| Enum (recommended) | `MorphMixin("commentable", MixinAllowed(Post.Type, Video.Type))` | `field.Enum` with values | CHECK / native ENUM | You want the DB to reject invalid morph keys end-to-end. |
| Plain string | `MorphMixin("commentable")` | `field.String` (optional + nillable) | None | You're prototyping, integrating with a legacy table that's already a string, or you can't promote the column to an enum yet. |

The codegen emits the same Go-level API in both modes — sealed parent interface, typed `SetCommentable`, typed predicates, typed resolver, eager-load. The only difference is what `polymorphic.go` casts through (`comment.CommentableType` when enum, raw `string` when plain).

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
        // "comments"    → method name emitted on Post (post.QueryComments()).
        // Comment.Type  → child schema type (typed reference).
        // "commentable" → relation name; must match the child's MorphTo.
        entpoly.MorphMany("comments", Comment.Type, "commentable"),
    }
}
```

`Video` follows the same pattern. `MorphOne` is the same call shape for at-most-one back-refs (e.g. `Post.featured_image`).

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
            // map only when you want non-default aliases — typically to
            // keep persisted "*_type" values stable across a Go-side
            // rename. See docs/morph-map.md.
            entpoly.WithMorphMap(map[string]string{
                "post":  "Post",
                "video": "Video",
            }),
            // WithGQLSchemaFile is OPTIONAL. Set it when you opt one
            // or more MorphTo edges into .GQL(): entpoly writes a
            // sidecar .graphql fragment with the union declarations.
            // entpoly.WithGQLSchemaFile("./api/gql/polymorphic.graphql"),
        )),
    }
    if err := entc.Generate("./schema", &gen.Config{}, opts...); err != nil {
        log.Fatalf("ent codegen: %v", err)
    }
}
```

The minimal form is also valid:

```go
entc.Extensions(entpoly.NewExtension())
```

Run codegen:

```bash
go generate ./ent
```

A new file `ent/polymorphic.go` is emitted alongside ent's normal output. It contains:

- A `MorphKey` named string type and a `Morphable` interface (`MorphID()` + `MorphKey()`).
- `MorphID()` / `MorphKey()` methods on every parent type (`*Post`, `*Video`, …).
- Per-parent morph-key constants (`ent.PostMorphKey`, `ent.VideoMorphKey`).
- The sealed parent interface (`CommentCommentableParent`) — only the types listed in `AllowedTypes` implement it.
- Typed setters on every Comment builder: `SetCommentable(p)` / `ClearCommentable()` on `Create`, `Update`, `UpdateOne`, `Mutation`.
- Typed predicates: `CommentCommentableIs(parent)`, `CommentCommentableIsType(morphKey)`.
- Typed forward resolver: `(*Comment).QueryCommentable(ctx) (CommentCommentableParent, error)`.
- Typed eager-load batcher: `CommentQuery.WithCommentable().All(ctx)` returns a typed result map (1+N queries — one per distinct parent type — not N+1).
- Per-parent sub-query predicate constructors: `CommentCommentableOnPost(post.PublishedEQ(true))` (Laravel `whereHasMorph` equivalent).
- Parent-side back-refs from `MorphMany` / `MorphOne` declarations: `(*Post).QueryComments() *CommentQuery`.
- M2M back-refs from `MorphedByMany` declarations (both directions auto-emitted).
- The runtime hook installer: `RegisterPolyHooks(client)` (wires `Required` / `Touch` / `Cascade` hooks declared on edges).
- The Go-side GraphQL union surface when any `MorphTo` was chained with `.GQL()` (type alias + exported `Is<Union>()` markers + `GQL<Rel>(ctx)` resolver helper).

## Use it

```go
ctx := context.Background()

// One-time setup if you used .Required() / .Touch() / .Cascade() on
// any edge. Safe to call on a fresh client even when no edges opted in.
ent.RegisterPolyHooks(client)

post, _  := client.Post.Create().SetTitle("Hello").Save(ctx)
video, _ := client.Video.Create().SetTitle("Demo").SetURL("https://...").Save(ctx)

// Attach a comment to the post via the typed setter. The argument is
// typed to the sealed CommentCommentableParent interface — passing a
// type not in AllowedTypes is a compile error.
c1, _ := client.Comment.Create().
    SetBody("Nice post!").
    SetCommentable(post).
    Save(ctx)

// Reassign across parent types.
_, _ = c1.Update().SetCommentable(video).Save(ctx)

// Clear the polymorphic parent.
_, _ = c1.Update().ClearCommentable().Save(ctx)
```

### Forward resolve — sealed interface, no `any` escape hatch

```go
parent, err := c1.QueryCommentable(ctx)
if err != nil { return err }
switch p := parent.(type) {
case *ent.Post:
    // typed *ent.Post
case *ent.Video:
    // typed *ent.Video
case nil:
    // discriminator unset
}
```

### Reverse — typed back-refs, composable

```go
// MorphMany → query builder; chain Where/Limit/Order/All as usual.
postComments, _ := post.QueryComments().All(ctx)
recent, _       := post.QueryComments().Order(ent.Desc(comment.FieldID)).Limit(10).All(ctx)

// MorphOne → single entity; (nil, nil) when unset.
img, _ := post.QueryFeaturedImage(ctx)
```

### Typed predicates — no string literals

```go
import "your/ent/comment"

// Every comment whose parent is exactly this Post (id + type both match).
client.Comment.Query().
    Where(ent.CommentCommentableIs(post)).
    All(ctx)

// Every comment whose parent is ANY Post (type only).
client.Comment.Query().
    Where(ent.CommentCommentableIsType(ent.PostMorphKey)).
    All(ctx)

// Laravel whereHasMorph equivalent: comments on Posts where Published is true.
client.Comment.Query().
    Where(ent.CommentCommentableOnPost(post.PublishedEQ(true))).
    All(ctx)
```

### Eager-load — batched per parent type

```go
r, err := client.Comment.Query().Limit(50).WithCommentable().All(ctx)
if err != nil { return err }
for _, c := range r.Comments {
    switch p := r.Commentable[c.ID].(type) {
    case *ent.Post:   /* ... */
    case *ent.Video:  /* ... */
    case nil:         /* unset */
    }
}
// Total queries: 1 (children) + N (distinct parent types in the batch).
// Compared to N+1 for per-row QueryCommentable(ctx) calls.
```

## Optional: opt-in runtime behaviours

Chain on the `MorphTo` edge builder:

```go
entpoly.MorphTo("commentable", Post.Type, Video.Type).
    Required().     // hook rejects unset / cleared writes
    Touch().        // bumps parent.updated_at on every child Save
    Cascade().      // deletes children when parent is deleted
    SoftDelete().   // reverse resolves skip soft-deleted parents
    GQL()           // emit GraphQL union surface (Go side + .graphql)
```

All four are wired via `ent.RegisterPolyHooks(client)`. Per-feature how-tos:
- [`features/required.md`](./features/required.md)
- [`features/touch.md`](./features/touch.md)
- [`features/cascade.md`](./features/cascade.md)
- [`features/soft-delete.md`](./features/soft-delete.md)
- [`features/graphql.md`](./features/graphql.md)

## Optional: cross-module index-name override

If your project links two ent modules that share a database AND both modules declare an entity with the same Go name plus the same morph relation (e.g. `dbent.Tag` + `mediamgr.Tag` both using `MorphMixin("taggable", ...)`), the composite `(<type>, <id>)` index that the mixin emits collides. Postgres index names are schema-global, so the second module's `Migrate()` aborts with `relation "tag_taggable_type_taggable_id" already exists`.

Pass a module-prefixed storage key to one of them:

```go
entpoly.MorphMixin("taggable",
    entpoly.MixinAllowed(Post.Type, Video.Type),
    entpoly.MixinIndexName("media_tags_taggable_type_taggable_id"),
)
```

You only need this when the collision actually occurs; the default leaves index naming to ent.

## Optional: a runnable example to compare against

The repo ships two end-to-end examples that demonstrate every code path:

| Path | What it covers |
|---|---|
| [`examples/basic/`](../examples/basic/) | `MixinAllowed` enum mode + every feature (`.Required()`, `.Touch()`, `.Cascade()`, `.SoftDelete()`, `.GQL()`), int-PK parents, M2M via Tag/Taggable. |
| [`examples/morphstring/`](../examples/morphstring/) | Bare `MorphMixin` (no `MixinAllowed`) — type column is `field.String`, no DB enum. Covers stem-matching (`Comment`/`commentable`) and divergent-name (`SourceLink`/`sourceable`, `Bookmark`/`pinnable`) child schemas. |
| [`examples/uuid/`](../examples/uuid/) | UUID-PK parents through the resolver, M2M, eager-load. |

Run any of them: `cd examples/<name> && go run entc.go && go test ./...`.

## Next steps

- [Relationships reference](./relationships.md) — every polymorphic shape, with full schema + usage examples.
- [Mutations reference](./mutations.md) — Laravel-to-ent translation table for every relationship verb.
- [Per-feature how-tos](./features/) — recipe per feature.
- [Feature matrix](./feature-matrix.md) — dense reference of every surface.
- [Morph map](./morph-map.md) — how the discriminator string is resolved and the renaming workflow.
- [Architecture](./internals/architecture.md) — how the codegen extension is structured.
- [FAQ](./faq.md) — answers to common questions about FKs, cascades, dialects, and GraphQL.
