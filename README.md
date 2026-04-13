# entx

Extensions for [ent](https://entgo.io) — the Go entity framework.

## Packages

| Package | Description |
|---------|-------------|
| [entcascade](./entcascade) | Generate cascade delete functions from schema annotations |
| [entgqlmulti](./entgqlmulti) | Generate per-API GraphQL schemas from a single ent schema |

## Installation

```bash
go get github.com/khanakia/entx/entcascade
go get github.com/khanakia/entx/entgqlmulti
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

## Development

This repo is a [Go workspace](https://go.dev/doc/tutorial/workspaces) with four modules:

| Module             | Purpose                                                                 |
| ------------------ | ----------------------------------------------------------------------- |
| `entcascade/`      | Cascade-delete generator (source)                                       |
| `entgqlmulti/`     | Per-API GraphQL schema generator (source)                               |
| `testent/`         | Integration harness for `entcascade` (ent + SQLite)                     |
| `testentgqlmulti/` | End-to-end harness for `entgqlmulti` (ent + entgql + gqlgen + SQLite)   |

```bash
# entcascade tests
task test                  # run the cascade integration suite
task generate              # regenerate testent/ent

# entgqlmulti end-to-end tests
task gqlmulti:generate     # regenerate ent + gqlgen for all three APIs
task gqlmulti:test         # run the 15-test entgqlmulti suite

# Whole repo
task build                 # compile all modules
task tidy                  # go mod tidy everywhere
```

See [`testentgqlmulti/README.md`](./testentgqlmulti/README.md) for the entgqlmulti harness and [`testentgqlmulti/TESTS.md`](./testentgqlmulti/TESTS.md) for the full test matrix.

Requires [Task](https://taskfile.dev) and Go 1.22+.

## License

MIT
