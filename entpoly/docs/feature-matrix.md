# entpoly — feature matrix

Dense reference of every surface entpoly emits. This is the lookup table; for task-oriented walkthroughs see [docs/features/](./features/), and for the design rationale see [docs/internals/](./internals/).

## Schema declaration

| Feature | Surface | Docs |
|---|---|---|
| Four relation shapes | `MorphTo` / `MorphOne` / `MorphMany` / `MorphedByMany` — all in `Edges()` | [Relationships reference](./relationships.md) |
| Mixin for discriminator columns | `MorphMixin(name)` adds `<rel>_id` + `<rel>_type` to a child schema | [getting-started](./getting-started.md) |
| DB-enforced enum on the type column | `MixinAllowed(Post.Type, Video.Type)` → `field.Enum(...)` w/ CHECK constraint | [ADR-001](./internals/adr-001-type-safety.md) |
| Auto composite index on `(<type>, <id>)` | Emitted by default; opt out via `MixinNoIndex()` | [relationships#foreign-keys](./relationships.md) |
| Override the composite-index storage key | `MixinIndexName("media_tags_taggable_type_taggable_id")` — needed when two ent modules sharing a DB declare an entity with the same Go name and the same morph relation (Postgres index names are schema-global). | [getting-started.md § cross-module index-name override](./getting-started.md#optional-cross-module-index-name-override) |
| Custom column names | `MixinIDColumn` / `MixinTypeColumn` + matching `IDColumn` / `TypeColumn` on the edge | [relationships#custom-column-names](./relationships.md) |
| `int` / `int64` / `string` / **`uuid.UUID`** parent PKs | Auto-detected per parent; codegen emits matching `strconv` / `uuid.Parse` branch | [examples/uuid/](../examples/uuid/) |
| Multiple polymorphic relations per schema | One `MorphMixin` + one `MorphTo` per relation, side by side | [faq.md](./faq.md) |
| Self-referential polymorphic | List the host type in its own `AllowedTypes` | [relationships#shape-4](./relationships.md) |

## Compile-time type safety

| Feature | What it catches | Where |
|---|---|---|
| Sealed parent interface per relation | `SetCommentable(article)` → compile error (Article not in AllowedTypes) | generated `CommentCommentableParent` interface |
| Named `MorphKey` type + per-parent constants | Raw string literals in predicates fail to compile | `ent.PostMorphKey`, `ent.VideoMorphKey` |
| Typed forward resolver (no `any`) | Type-switch only accepts AllowedTypes | `comment.QueryCommentable(ctx)` returns sealed iface |
| Mixin / edge `AllowedTypes` drift linter | Mismatched lists fail at codegen time with a precise diff | [internals/architecture.md § Edge cases](./internals/architecture.md) |
| Typed predicate constructors | `ent.CommentCommentableIs(post)` / `ent.CommentCommentableIsType(ent.PostMorphKey)` | generated in `polymorphic.go` |
| Ghost-FK column suppression | No leftover `post_comments *int` cruft on the child struct | `entsql.Skip()` + preprocess Field cleanup |

## Reads (Laravel parity)

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

## Writes (Laravel parity)

| Laravel | entpoly |
|---|---|
| `$c->commentable()->associate($post)` | `c.Update().SetCommentable(post).Save(ctx)` |
| `$c->commentable()->dissociate()` | `c.Update().ClearCommentable().Save(ctx)` |
| `$post->comments()->save($c)` | `client.Comment.Create().SetBody(...).SetCommentable(post).Save(ctx)` |
| Attach / detach / sync (M2M) | `client.Taggable.Create()...` + `helper.Toggle` / `helper.Sync` / `helper.SyncWithoutDetach` |

[Mutations reference →](./mutations.md)

## Runtime hooks (opt-in per relation)

| Option | What it does |
|---|---|
| `.Required()` | Rejects Save when discriminator is unset on Create OR cleared on Update |
| `.Touch()` / `.Touch("modified_at")` | Bumps parent's timestamp column on every child Save (Laravel `$touches`) |
| `.Cascade()` | Pre-delete hook on every allowed parent — deletes polymorphic children when the parent dies |
| `.SoftDelete()` / `.SoftDelete("removed_at")` | Reverse resolves skip parents whose soft-delete column is non-null; per-parent auto-detection |
| `.GQL()` / `.GQL("CustomName")` | Emits a GraphQL union surface — Go type alias + exported `Is<Union>()` markers + `GQL<Rel>(ctx)` resolver-helper. Optional `.graphql` schema fragment via `entpoly.WithGQLSchemaFile(...)` |

All four wire through one call: `ent.RegisterPolyHooks(client)` at startup. [Mutations reference →](./mutations.md)

## GraphQL union surface (`.GQL()`)

| Emission | Purpose |
|---|---|
| Go type alias `type Commentable = CommentCommentableParent` | gqlgen recognises the union by Go type identity |
| Exported markers `func (*Post) IsCommentable() {}` on every allowed parent | gqlgen union-member contract — every member type must declare the interface marker |
| Resolver helper `c.GQLCommentable(ctx) (Commentable, error)` | One-liner for gqlgen resolvers — same result as `c.QueryCommentable(ctx)` |
| Optional `.graphql` fragment via `entpoly.WithGQLSchemaFile("./graph/poly.graphql")` | `union Commentable = Post \| Video \| Image` lands in your schema directory ready for gqlgen codegen |

End-to-end queries (paste-ready) live in [`testentpoly/QUERIES.md`](../../testentpoly/QUERIES.md). Spin up a real server with `cd testentpoly && task serve`.

## What we DON'T do

| Thing | Why |
|---|---|
| Foreign-key constraints on polymorphic columns | SQL FKs target exactly one table; the discriminator column references multiple tables. No FK is possible. We compensate w/ `Cascade()` + DB-enforced enum. |
| Modify ent's struct codegen | Our sidecar `polymorphic.go` lives in the ent package and adds methods, never fields. Keeps the integration shallow + version-portable. |

## Three layers of type safety

| Layer | Catches |
|---|---|
| **Sealed Go interface** (compile time) | `SetCommentable(article)` — wrong parent type |
| **ent runtime enum validator** | `SetCommentableType("psot")` — typo'd morph key |
| **DB CHECK / native ENUM** | `INSERT ... commentable_type='random'` — raw SQL bypass |

See [ADR-001: Type-safety strategy](./internals/adr-001-type-safety.md) for the design rationale, the three alternatives considered, and the trade-offs.
