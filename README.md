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

This repo is a [Go workspace](https://go.dev/doc/tutorial/workspaces):

```bash
# Run tests
task test

# Regenerate ent code (testent)
task generate

# Build all modules
task build

# Tidy all modules
task tidy
```

Requires [Task](https://taskfile.dev) and Go 1.22+.

## License

MIT
