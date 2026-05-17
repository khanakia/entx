# entx

Extensions for [ent](https://entgo.io) — the Go entity framework.

## Packages

| Package | Description |
|---------|-------------|
| [entcascade](./entcascade) | Generate cascade delete functions from schema annotations |
| [entgqlmulti](./entgqlmulti) | Generate per-API GraphQL schemas from a single ent schema |
| [entpoly](./entpoly) | Laravel-style polymorphic relationships — MorphTo / MorphOne / MorphMany / MorphedByMany with compile-time + DB-level type safety + GraphQL union surface |
| [entreadonly](./entreadonly) | Make a schema read-only at codegen — strips Create/Update/Delete builders so writes fail to compile, while edges + queries + GraphQL stay intact |
| [entskiptable](./entskiptable) | Exclude externally-owned tables from ent auto-migration via a composable migration DiffHook (no DDL on tables you only read) |
| [enttui](./enttui) | Schema-driven terminal UI / admin browser for ent — a `k9s`-style keyboard-driven data explorer generated straight from your schema |

## Installation

```bash
go get github.com/khanakia/entx/entcascade
go get github.com/khanakia/entx/entgqlmulti
go get github.com/khanakia/entx/entpoly
go get github.com/khanakia/entx/entreadonly
go get github.com/khanakia/entx/entskiptable
go get github.com/khanakia/entx/enttui
```

Each package is a standalone Go module — install only what you need.

## entcascade

Many ent projects disable foreign keys (`WithForeignKeys(false)`) for faster migrations, flexible schema evolution, and cross-database portability. The trade-off: no `ON DELETE CASCADE` from the database. You need application-level cascade deletes.

`entcascade` generates them automatically from schema annotations — no manual delete functions to write or maintain.

```go
// One annotation on the schema
func (User) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entcascade.Cascade(),
    }
}
```

```go
// Generated — deletes user + all posts, comments, profile in a transaction
err := ent.CascadeDeleteUser(ctx, client, userID)
```

**Features:** nested cascades, soft delete auto-detection, batch delete, unlink (SET NULL), skip edges, pre/post hooks, idempotent deletes, transaction safety.

See [entcascade/README.md](./entcascade/README.md) for full documentation, use cases, and before/after comparison.

## entgqlmulti

Generate separate GraphQL schemas for different APIs from the same ent schema. Each API gets only the types, fields, and operations it needs.

```go
// Schema annotation — expose Chatbot in dashboard API (full CRUD) and public API (read-only)
func (Chatbot) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entgqlmulti.ApiConfig(map[string][]entgqlmulti.ApiTarget{
            "apidash": {{Query: true, Mutations: true, Filters: true}},
            "apipub":  {{TypeName: "PublicBot", Fields: []string{"name", "avatar"}, Query: true}},
        }),
    }
}
```

See [entgqlmulti/README.md](./entgqlmulti/README.md) for full documentation.

## entpoly

Laravel-style polymorphic relationships for ent — declared as schema-level edges, with compile-time + DB-level type safety, opt-in runtime hooks, and an optional GraphQL union surface.

```go
// Comment can attach to any AllowedTypes parent (Post, Video, Image, …)
func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{entpoly.MorphMixin("commentable", entpoly.MixinAllowed(Post.Type, Video.Type))}
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type).
            Required().     // hook rejects unset / cleared writes
            Touch().        // bumps parent.updated_at on save
            Cascade().      // deletes children when parent dies
            SoftDelete().   // filters soft-deleted parents on read
            GQL(),          // emits union Commentable = Post | Video for gqlgen
    }
}
```

```go
// Type-safe forward resolve — sealed interface, no any escape hatch
switch p := comment.QueryCommentable(ctx).(type) {
case *ent.Post:   // typed *Post
case *ent.Video:  // typed *Video
case nil:         // unset
}
// case *ent.Article: → COMPILE ERROR — Article not in AllowedTypes
```

**Features:** four relation shapes (MorphTo / MorphOne / MorphMany / MorphedByMany), sealed Go interfaces, DB-enforced enum on the type column, UUID parent PKs, polymorphic M2M with attach/detach/sync helpers, self-referential, eager-load batching (1+N, not N+1), opt-in Required / Touch / Cascade / SoftDelete hooks, GraphQL union codegen, ghost-FK column suppression.

See [entpoly/README.md](./entpoly/README.md) for the full feature matrix, [entpoly/docs/features/](./entpoly/docs/features/) for per-feature step-by-step guides, and [testentpoly/](./testentpoly/) for the end-to-end integration harness.

## entreadonly

Make an ent schema read-only **at code-generation time**. ent normally generates a full write surface (`Create`/`Update`/`Delete` builders + client methods) for every schema. When a table is owned by another module/service and your app only models it to query it and traverse edges to it, those write builders are a footgun — they compile, so anyone can write the foreign table and bypass the real owner's validation.

```go
// app's projection of authmgr's auth_users — read it, never write it
func (User) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entsql.Annotation{Table: "auth_users"},
        entreadonly.ReadOnly(), // app.User.Create() etc. won't even exist
    }
}
```

An annotation-driven entc extension records the marked types; a deterministic post-codegen AST pass removes their write surface. `client.User.Create()/Update()/Delete()` no longer exist — writes fail to **compile** — while `Query()`, edges, eager-loading and entgql stay fully intact. Works where `ent.View` can't (it breaks FK edges and entgql node codegen).

See [entreadonly/README.md](./entreadonly/README.md) for the multi-module auth use case, the "why can't this be dynamic" explanation, and the full mechanism.

## entskiptable

Stop ent auto-migration from emitting `CREATE` / `ALTER` / `DROP` DDL against tables your client only **reads** — tables owned by another service, another ent client, a view, or a hand-managed migration. Modeling such a table as an `ent.Schema` (so edges work) otherwise lets `client.Schema.Create()` reshape a table you don't own.

```go
err := client.Schema.Create(ctx,
    schema.WithForeignKeys(false),
    schema.WithDiffHook(entskiptable.SkipHook(
        entskiptable.Any(
            entskiptable.ByPrefix("auth_"),          // a module-owned namespace
            entskiptable.ByName("billing_accounts"), // a specific foreign table
        ),
    )),
)
```

A small composable migration `DiffHook` filters the computed schema changes: any table-scoped change targeting an excluded table is dropped before it is applied. Reads, edges, and code-generation are untouched.

See [entskiptable/README.md](./entskiptable/README.md) for the full rationale, API, and FAQ.

## enttui

A schema-driven **terminal UI / admin browser** for ent — think **`k9s` for your ent models**. Point it at the same `./schema` directory you pass to `entc.Generate`, run one extra codegen pass, and every entity becomes a fast, keyboard-driven view for browsing, searching, filtering, sorting, and editing — no hand-written admin panels, no web server, no reflection.

```
┌─ Tasks ──────────────────────────────────────┐┌─ preview ─────────────────────────────────────────┐
│ ▸ Migrate 5 lists to ServerDataTable         ││ id: tsk_019e25a435f47…                            │
│   Apply useDelayedFlag to /areas             ││ title: Migrate 5 lists to ServerDataTable         │
│   Implement TaskAssignees GraphQL resolver   ││ status: doing  priority: p1                       │
└──────────────────────────────────────────────┘└───────────────────────────────────────────────────┘
 Tasks  52/52  sort:created_at↓   j/k move · / filter · , leader · ? help · enter open · esc back · q quit
```

Works with any ent SQL dialect (SQLite, PostgreSQL, MySQL), any ID type (`string` / `int` / `uuid.UUID`, per entity), and any terminal (tview/tcell). It runs as a standalone codegen pass and does **not** modify or replace your existing `entc` generation.

See [enttui/README.md](./enttui/README.md) for setup, theming (Catppuccin), vim-style navigation, and the [testenttui/](./testenttui/) demo.

## Development

This repo is a [Go workspace](https://go.dev/doc/tutorial/workspaces); each package is an independently versioned module:

| Module             | Purpose                                                                 |
| ------------------ | ----------------------------------------------------------------------- |
| `entcascade/`      | Cascade-delete generator (source)                                       |
| `entgqlmulti/`     | Per-API GraphQL schema generator (source)                               |
| `entpoly/`         | Polymorphic relationships generator (source)                            |
| `entreadonly/`     | Compile-time read-only schema (extension + AST strip) (source)          |
| `entskiptable/`    | Migration DiffHook excluding externally-owned tables (source)           |
| `enttui/`          | Schema-driven terminal UI / admin browser for ent (source)              |
| `testent/`         | Integration harness for `entcascade` (ent + SQLite)                     |
| `testentgqlmulti/` | End-to-end harness for `entgqlmulti` (ent + entgql + gqlgen + SQLite)   |
| `testentpoly/`     | End-to-end harness for `entpoly` (ent + gqlgen + SQLite + HTTP server)  |
| `testenttui/`      | Demo / harness for `enttui`                                             |

```bash
# entcascade tests
task test                  # run the cascade integration suite
task generate              # regenerate testent/ent

# entgqlmulti end-to-end tests
task gqlmulti:generate     # regenerate ent + gqlgen for all three APIs
task gqlmulti:test         # run the 15-test entgqlmulti suite

# entpoly end-to-end tests
cd testentpoly
task generate              # ent + gqlgen codegen
task test                  # run the 30-test entpoly suite (28 PASS, 2 SKIP)
task serve                 # standalone GraphQL server on :8080 (seeded sample data)

# Whole repo
task build                 # compile all modules
task tidy                  # go mod tidy everywhere
```

See [`testentgqlmulti/README.md`](./testentgqlmulti/README.md) and [`testentpoly/README.md`](./testentpoly/README.md) for per-harness documentation. Full test matrices: [`testentgqlmulti/TESTS.md`](./testentgqlmulti/TESTS.md), [`testentpoly/SCENARIOS.md`](./testentpoly/SCENARIOS.md). Paste-ready GraphQL queries for entpoly: [`testentpoly/QUERIES.md`](./testentpoly/QUERIES.md).

Requires [Task](https://taskfile.dev) and Go 1.22+.

## License

MIT
