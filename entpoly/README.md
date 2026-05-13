# entpoly

**Laravel-style polymorphic relationships for [ent](https://entgo.io) — declared as schema-level edges, generated with the strongest type safety any Go ORM offers.**

```go
func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{entpoly.MorphMixin("commentable", entpoly.MixinAllowed(Post.Type, Video.Type))}
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type).
            Required().     // hook rejects unset / cleared writes
            Touch().        // bumps parent.updated_at on save
            Cascade().      // deletes children when parent dies
            SoftDelete(),   // filters soft-deleted parents on read
    }
}
```

```go
client := ent.NewClient(...)
ent.RegisterPolyHooks(client)   // wires Required + Touch + Cascade hooks
```

---

## Features

### Schema declaration

| Feature | Surface | Docs |
|---|---|---|
| Four relation shapes | `MorphTo` / `MorphOne` / `MorphMany` / `MorphedByMany` — all in `Edges()` | [Relationships reference](./docs/relationships.md) |
| Mixin for discriminator columns | `MorphMixin(name)` adds `<rel>_id` + `<rel>_type` to a child schema | [getting-started](./docs/getting-started.md) |
| DB-enforced enum on the type column | `MixinAllowed(Post.Type, Video.Type)` → `field.Enum(...)` w/ CHECK constraint | [ADR-001](./docs/adr-001-type-safety.md) |
| Auto composite index on `(<type>, <id>)` | Emitted by default; opt out via `MixinNoIndex()` | [relationships#foreign-keys](./docs/relationships.md) |
| Custom column names | `MixinIDColumn` / `MixinTypeColumn` + matching `IDColumn` / `TypeColumn` on the edge | [relationships#custom-column-names](./docs/relationships.md) |
| `int` / `int64` / `string` / **`uuid.UUID`** parent PKs | Auto-detected per parent; codegen emits matching `strconv` / `uuid.Parse` branch | [examples/uuid/](./examples/uuid/) |
| Multiple polymorphic relations per schema | One `MorphMixin` + one `MorphTo` per relation, side by side | [faq.md](./docs/faq.md) |
| Self-referential polymorphic | List the host type in its own `AllowedTypes` | [relationships#shape-4](./docs/relationships.md) |

### Compile-time type safety

| Feature | What it catches | Where |
|---|---|---|
| Sealed parent interface per relation | `SetCommentable(article)` → compile error (Article not in AllowedTypes) | generated `CommentCommentableParent` interface |
| Named `MorphKey` type + per-parent constants | Raw string literals in predicates fail to compile | `ent.PostMorphKey`, `ent.VideoMorphKey` |
| Typed forward resolver (no `any`) | Type-switch only accepts AllowedTypes | `comment.QueryCommentable(ctx)` returns sealed iface |
| Mixin / edge `AllowedTypes` drift linter | Mismatched lists fail at codegen time with a precise diff | [docs/architecture.md § Edge cases](./docs/architecture.md) |
| Typed predicate constructors | `ent.CommentCommentableIs(post)` / `ent.CommentCommentableIsType(ent.PostMorphKey)` | generated in `polymorphic.go` |
| Ghost-FK column suppression | No leftover `post_comments *int` cruft on the child struct | `entsql.Skip()` + preprocess Field cleanup |

### Reads (Laravel parity)

| Laravel | entpoly | Notes |
|---|---|---|
| `$comment->commentable` | `comment.QueryCommentable(ctx)` | Returns sealed interface — type-switch on `*Post`/`*Video`/`nil` only |
| `$post->comments` (MorphMany) | `post.QueryComments()` | `*CommentQuery` — composable, chain `.Where()`/`.Limit()`/`.All()` |
| `$post->image` (MorphOne) | `post.QueryFeaturedImage(ctx)` | `(*Image, error)`; `(nil, nil)` for unset |
| `$tag->posts` (MorphedByMany) | `tag.QueryPosts(ctx)` | `[]*Post` via batched pivot lookup |
| `$post->tags` (auto-emitted M2M inverse) | `post.QueryTags(ctx)` | derived from same `MorphedByMany` declaration |
| `Comment::with('commentable')->get()` | `cq.WithCommentable().All(ctx)` | Typed eager-load batched per parent type → 1+N queries, not N+1 |
| Typed predicates | `ent.CommentCommentableIs(post)` / `ent.CommentCommentableIsType(ent.PostMorphKey)` | typed; no `"post"` string literals |
| `Comment::whereHasMorph('commentable', [Post], fn ($q) => ...)` | `ent.CommentCommentableOnPost(post.PublishedEQ(true))` | per-parent sub-query helper; compose multi-type via `comment.Or` |

### Writes (Laravel parity)

| Laravel | entpoly |
|---|---|
| `$c->commentable()->associate($post)` | `c.Update().SetCommentable(post).Save(ctx)` |
| `$c->commentable()->dissociate()` | `c.Update().ClearCommentable().Save(ctx)` |
| `$post->comments()->save($c)` | `client.Comment.Create().SetBody(...).SetCommentable(post).Save(ctx)` |
| Attach / detach / sync (M2M) | `client.Taggable.Create()...` + `helper.Toggle` / `helper.Sync` / `helper.SyncWithoutDetach` |

[Mutations reference →](./docs/mutations.md)

### Runtime hooks (opt-in per relation)

| Option | What it does |
|---|---|
| `.Required()` | Rejects Save when discriminator is unset on Create OR cleared on Update |
| `.Touch()` / `.Touch("modified_at")` | Bumps parent's timestamp column on every child Save (Laravel `$touches`) |
| `.Cascade()` | Pre-delete hook on every allowed parent — deletes polymorphic children when the parent dies |
| `.SoftDelete()` / `.SoftDelete("removed_at")` | Reverse resolves skip parents whose soft-delete column is non-null; per-parent auto-detection |
| `.GQL()` / `.GQL("CustomName")` | Emits a GraphQL union surface — Go type alias + exported `Is<Union>()` markers + `GQL<Rel>(ctx)` resolver-helper. Optional `.graphql` schema fragment via `entpoly.WithGQLSchemaFile(...)` |

All four wire through one call: `ent.RegisterPolyHooks(client)` at startup. [Mutations reference →](./docs/mutations.md)

### GraphQL union surface (`.GQL()`)

Adding `.GQL()` to a `MorphTo` emits everything gqlgen needs to expose the relation as a GraphQL union:

| Emission | Purpose |
|---|---|
| Go type alias `type Commentable = CommentCommentableParent` | gqlgen recognises the union by Go type identity |
| Exported markers `func (*Post) IsCommentable() {}` on every allowed parent | gqlgen union-member contract — every member type must declare the interface marker |
| Resolver helper `c.GQLCommentable(ctx) (Commentable, error)` | One-liner for gqlgen resolvers — same result as `c.QueryCommentable(ctx)` |
| Optional `.graphql` fragment via `entpoly.WithGQLSchemaFile("./graph/poly.graphql")` | `union Commentable = Post \| Video \| Image` lands in your schema directory ready for gqlgen codegen |

End-to-end queries (paste-ready) live in [`testentpoly/QUERIES.md`](../testentpoly/QUERIES.md). Spin up a real server with `cd testentpoly && task serve`.

### What we DON'T do

| Thing | Why |
|---|---|
| Foreign-key constraints on polymorphic columns | SQL FKs target exactly one table; the discriminator column references multiple tables. No FK is possible. We compensate w/ `Cascade()` + DB-enforced enum. |
| Modify ent's struct codegen | Our sidecar `polymorphic.go` lives in the ent package and adds methods, never fields. Keeps the integration shallow + version-portable. |

---

## Install

```bash
go get github.com/khanakia/entx/entpoly
```

Register in `ent/entc.go`:

```go
opts := []entc.Option{
    entc.Extensions(entpoly.NewExtension(
        // Optional — every parent gets a snake_case morph key by
        // default. Pass an explicit map to lock aliases across renames.
        entpoly.WithMorphMap(map[string]string{
            "post":  "Post",
            "video": "Video",
        }),
    )),
}
entc.Generate("./schema", config, opts...)
```

Run `go generate ./ent`. A sidecar `ent/polymorphic.go` is emitted alongside ent's normal output containing the `Morphable` interface, per-parent constants, sealed parent interfaces, typed setters, typed predicates, typed resolver, typed back-refs, eager-load helpers, and the runtime hooks.

[Getting started →](./docs/getting-started.md)

---

## Three layers of type safety

| Layer | Catches |
|---|---|
| **Sealed Go interface** (compile time) | `SetCommentable(article)` — wrong parent type |
| **ent runtime enum validator** | `SetCommentableType("psot")` — typo'd morph key |
| **DB CHECK / native ENUM** | `INSERT ... commentable_type='random'` — raw SQL bypass |

See [ADR-001: Type-safety strategy](./docs/adr-001-type-safety.md) for the design rationale, the three alternatives we considered, and the trade-offs.

---

## Documentation index

| Doc | Use when |
|---|---|
| [Getting started](./docs/getting-started.md) | Adding entpoly to a fresh project |
| [Relationships reference](./docs/relationships.md) | Choosing a shape for your domain |
| [Mutations reference](./docs/mutations.md) | Translating from Laravel verbs to ent builders |
| [Laravel parity](./docs/laravel-parity.md) | Full Laravel → entpoly mapping |
| [Morph map](./docs/morph-map.md) | Stable aliases + the rename workflow |
| [Architecture](./docs/architecture.md) | How the codegen extension is built; how to contribute |
| [ADR-001: Type-safety strategy](./docs/adr-001-type-safety.md) | Why sealed interface + enum column (Approach C) — diagrams, tradeoffs, alternatives rejected |
| [ADR-002: `whereMorphRelation` API](./docs/adr-002-where-morph-relation.md) | Why per-parent predicate constructors (`OnPost`/`OnVideo`) over closures or builder objects |
| [FAQ](./docs/faq.md) | Common questions about FKs, cascades, dialects, GraphQL |

### Per-feature how-to guides

Step-by-step guides for each feature, with the same shape — when to use, setup, wiring, usage, verification, gotchas. Index: [`docs/features/`](./docs/features/).

| Guide | What it covers |
|---|---|
| [GraphQL](./docs/features/graphql.md) | `.GQL()` end-to-end — schema → entc.go → gqlgen.yml → resolver → curl |
| [Required](./docs/features/required.md) | `.Required()` hook — reject unset / cleared writes |
| [Touch](./docs/features/touch.md) | `.Touch()` hook — bump parent timestamp on child save |
| [Cascade](./docs/features/cascade.md) | `.Cascade()` hook — delete children with the parent |
| [Soft delete](./docs/features/soft-delete.md) | `.SoftDelete()` — hide soft-deleted parents from reverse resolves |
| [UUID parents](./docs/features/uuid-parents.md) | UUID PK setup — codegen detects per-parent shape |
| [M2M polymorphic](./docs/features/m2m-polymorphic.md) | `MorphedByMany` + pivot + auto-inverse + `helper.Sync`/`Toggle` |
| [Eager loading](./docs/features/eager-loading.md) | `WithCommentable()` 1+N(types) batching |
| [Custom columns](./docs/features/custom-columns.md) | `MixinIDColumn` / `MixinTypeColumn` + matching edge overrides |
| [Self-referential](./docs/features/self-referential.md) | Host type listed in its own `AllowedTypes` |
| [Predicates](./docs/features/predicates.md) | Typed predicate constructors + per-parent sub-query helpers |
| [MorphOne](./docs/features/morph-one.md) | Exactly-one parent-side back-reference |
| [Morph map](./docs/morph-map.md) | Stable aliases via `entpoly.WithMorphMap(...)` |

---

## Examples

| Example | Demonstrates |
|---|---|
| [examples/basic/](./examples/basic/) | Int-PK parents (Post, Video, Image), Comment child w/ all options (`Required`, `Touch`, `Cascade`, `SoftDelete`, `GQL`), Tag/Taggable M2M, eager-load |
| [examples/uuid/](./examples/uuid/) | UUID-PK parents (Document, Report), Annotation child — full UUID round-trip |
| [../testentpoly/](../testentpoly/) | **Full integration harness** — every feature, real HTTP GraphQL server, drift-linter negative tests, query tracer for eager-load batching. 27-row scenario matrix in [testentpoly/SCENARIOS.md](../testentpoly/SCENARIOS.md), paste-ready GraphQL queries in [testentpoly/QUERIES.md](../testentpoly/QUERIES.md). |

The two `examples/` are minimal runnable docs (`go test ./examples/...`). `testentpoly/` is the kitchen-sink integration suite — schema variants, hook combinations, polymorphic M2M, self-ref, custom column names, morph-map rename, drift-linter negatives, structural artifact assertions, and a real gqlgen HTTP server (`task serve`).

---

## Status

| State | Items |
|---|---|
| ✅ Shipped | 13 of 13 roadmap items — see [docs/architecture.md § What v1 ships](./docs/architecture.md) |
| ⏳ Backlog | 2 follow-up gaps surfaced by `testentpoly` — see [docs/architecture.md § v2 roadmap](./docs/architecture.md#v2-roadmap) (dup `MorphKey` constants under aliased `WithMorphMap`; mixed-PK drift linter) |
| 🧪 Test coverage | `entpoly/` core: 6 codegen GQL tests + integration tests. `examples/basic/` + `examples/uuid/`: runtime tests against in-memory SQLite. `testentpoly/`: 28 PASS / 2 SKIP / 0 FAIL across 5 phases — real ent codegen + real gqlgen + real HTTP + query tracer |

---

## License

MIT
