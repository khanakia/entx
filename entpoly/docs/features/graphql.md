# GraphQL union surface (`.GQL()`)

A `.GQL()` opt-in on a `MorphTo` edge wires the polymorphic relation into gqlgen as a real GraphQL union — `union Commentable = Post | Video | Image`. entpoly emits the Go-side glue (type alias, marker methods, resolver helper) and an optional `.graphql` fragment so the same declaration drives both the database schema and the GraphQL surface. Reach for this when you want a single `commentable` field on `type Comment` that resolves to multiple concrete object types depending on which parent the row actually points at.

## When to use

- The polymorphic child is exposed through a GraphQL API and clients want one field per relation rather than one per parent type
- You want type-narrowing in the schema (`... on Post`, `... on Video`) instead of nullable per-parent fields
- The Go-side sealed interface (`CommentCommentableParent`) should be the same value the resolver returns to gqlgen
- gqlgen union-member contracts (every member needs an `Is<Union>()` marker) should be machine-emitted rather than hand-maintained

## Setup

Schema declaration on the child:

```go
// ent/schema/comment.go
package schema

import (
    "entgo.io/ent"
    "entgo.io/ent/schema/field"

    "github.com/khanakia/entx/entpoly"
)

type Comment struct{ ent.Schema }

func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable",
            entpoly.MixinAllowed(Post.Type, Video.Type, Image.Type),
        ),
    }
}

func (Comment) Fields() []ent.Field {
    return []ent.Field{field.Text("body")}
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type, Image.Type).
            GQL(), // default union name "Commentable"
    }
}
```

To rename the GraphQL union without renaming the morph relation, pass an argument: `.GQL("PostOrVideo")`.

`.GQL()` works with either discriminator mode — `MixinAllowed(...)` (the type column is `field.Enum`) **or** plain `MorphMixin("commentable")` (the type column is `field.String`). The Go-level surface and the emitted `.graphql` fragment are identical; the generated cast in `polymorphic.go` adapts to the column type internally. The enum mode is still recommended for end-to-end DB integrity (a CHECK / native ENUM rejects invalid morph keys at the storage layer), but it is not required to opt the relation into GraphQL.

## Wiring

`ent/entc.go` — register the extension and point it at the `.graphql` schema file you want the union fragment emitted into:

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
            entpoly.WithGQLSchemaFile("./api/gql/polymorphic.graphql"),
        )),
    }
    if err := entc.Generate("./schema", &gen.Config{}, opts...); err != nil {
        log.Fatalf("ent codegen: %v", err)
    }
}
```

After `go generate ./ent`, the emitted file looks like:

```graphql
# api/gql/polymorphic.graphql
union Commentable = Post | Video | Image
```

Tell gqlgen to pick the file up alongside your hand-written schema (`gqlgen.yml`):

```yaml
schema:
  - schema.graphql
  - polymorphic.graphql

autobind:
  - "github.com/your/proj/ent"

models:
  Commentable:
    model: github.com/your/proj/ent.Commentable
```

The `autobind` line makes gqlgen reuse the generated `ent.Comment`/`ent.Post`/... structs; the `Commentable` model binding tells gqlgen that the Go alias for the union lives in the ent package.

Resolver — forward the field to the entpoly-emitted helper:

```go
// api/gql/schema.resolvers.go
func (r *commentResolver) Commentable(ctx context.Context, obj *ent.Comment) (ent.Commentable, error) {
    return obj.GQLCommentable(ctx)
}
```

`GQLCommentable` returns the sealed interface (which is the same Go type as `Commentable`), so gqlgen sees a `*Post` / `*Video` / `*Image` and dispatches against the matching `Is<Union>()` marker entpoly emitted on each parent.

Runtime — install hooks once at startup:

```go
client := ent.NewClient(...)
ent.RegisterPolyHooks(client)
```

## Usage

Query the union from the playground or a curl call:

```graphql
{
  comments {
    id
    body
    commentable {
      __typename
      ... on Post  { id title }
      ... on Video { id title }
      ... on Image { id url }
    }
  }
}
```

```bash
curl -s -X POST http://localhost:8080/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"{ comments { id body commentable { __typename ... on Post { id title } ... on Video { id title } ... on Image { id url } } } }"}' \
  | jq
```

## Verification

Start the kitchen-sink server (seeds 2 Posts, 1 Video, 1 Image, 5 Comments) and hit the playground:

```bash
cd testentpoly && task serve
# open http://localhost:8080/
```

Or assert the marker contract from a Go test:

```go
var _ ent.Commentable = (*ent.Post)(nil)
var _ ent.Commentable = (*ent.Video)(nil)
var _ ent.Commentable = (*ent.Image)(nil)
```

Each parent type satisfies the union by virtue of the generated `func (*Post) IsCommentable() {}` marker.

## Gotchas

1. **Resolver Go-type must match the autobind target.** If `models.Commentable` is bound to `ent.Commentable` but your resolver signature returns a different alias, gqlgen prints a confusing "type mismatch" at runtime. Always use the entpoly-emitted helper `c.GQL<Rel>(ctx)` — its return type is guaranteed to be the bound alias.
2. **One `.graphql` file for every GQL-enabled relation.** `entpoly.WithGQLSchemaFile(...)` takes a single path. All `.GQL()` unions in the project share that file; entpoly rewrites it on every codegen pass.
3. **Marker collisions when one type plays multiple unions.** A parent type listed in two different `.GQL()` relations gets two separate `Is<Union>()` markers, both emitted on the same struct. Harmless at the Go level (each is a unique method), but worth knowing if you ever wrap the parent in your own interface.
4. **Without `WithGQLSchemaFile`, the `union ... = ...` fragment is your responsibility.** The Go side (alias, markers, resolver helper) is emitted regardless; only the SDL fragment depends on the option.

## See also

- [`testentpoly/QUERIES.md`](../../../testentpoly/QUERIES.md) — paste-ready queries against the running server
- [`testentpoly/cmd/serve/main.go`](../../../testentpoly/cmd/serve/main.go) — end-to-end server wiring
- [Architecture](../internals/architecture.md) — how `.GQL()` plugs into the codegen pipeline
- [Getting started](../getting-started.md) — adding entpoly to a fresh ent project
- [Laravel parity](../laravel-parity.md) — Laravel `morphTo` → entpoly verb mapping
