# testentgqlmulti

End-to-end integration harness for the [`entgqlmulti`](../entgqlmulti) extension.

Generates real ent + entgql + `entgqlmulti` code, runs [gqlgen](https://gqlgen.com/) on the per-API schemas, wires everything to an in-memory SQLite database, and executes GraphQL queries over HTTP to prove the extension works end-to-end — not just that the SDL parses.

## What it covers

Three GraphQL APIs backed by the same ent schema, exercised against real HTTP servers:

| API         | Shape                                                                     |
| ----------- | ------------------------------------------------------------------------- |
| `apidash`   | Full CRUD dashboard — all fields, mutations, filters, orderBy             |
| `apipub`    | Read-only public surface — subset types (`PublicUser`, `PublicChatbot`)   |
| `apimobile` | Narrow mobile projection — `me` query, camelCase fields whitelist         |

15 tests across 4 files — see [TESTS.md](./TESTS.md) for the full matrix.

## Layout

```
testentgqlmulti/
├── schema/                    # ent schemas with ApiConfig annotations
├── entc.go                    # ent + entgqlmulti codegen entry point
├── ent/                       # generated ent code
├── api/
│   ├── apidash/               # gqlgen-generated code + resolvers
│   ├── apipub/
│   └── apimobile/
├── server.go                  # Fixture: one ent.Client fans out to 3 httptest.Servers
├── gql_client.go              # HTTP GraphQL helper for tests
├── apidash_test.go            # dashboard API runtime tests
├── apipub_test.go             # public API runtime tests
├── apimobile_test.go          # mobile API runtime tests
├── schema_shape_test.go       # structural SDL assertions
├── Taskfile.yml               # generate / test / build / clean tasks
└── TESTS.md                   # test catalog
```

## Running

All tasks are in [`Taskfile.yml`](./Taskfile.yml). From this directory:

```bash
task generate          # ent codegen + gqlgen codegen for all three APIs
task test              # run all 15 tests (verbose)
task test:short        # run tests without -v
task build             # compile everything
task clean             # remove generated artifacts
```

From `entx-main/`:

```bash
task gqlmulti:generate # full regen from the repo root
task gqlmulti:test     # run integration suite from the repo root
```

Without Task:

```bash
# one-time: install gqlgen into ./bin
GOBIN=$PWD/bin GOWORK=off go install github.com/99designs/gqlgen

# regenerate everything
GOWORK=off go run entc.go
(cd api/apidash   && ../../bin/gqlgen generate)
(cd api/apipub    && ../../bin/gqlgen generate)
(cd api/apimobile && ../../bin/gqlgen generate)
GOWORK=off go mod tidy

# run the tests
GOWORK=off go test -v -count=1 .
```

## How the harness works

1. **ent codegen** (`entc.go`) runs ent with `entgql.WithSchemaHook(entgqlmulti.New(...).SchemaHook())`.
2. For each configured API, `entgqlmulti` filters the monolithic ent schema into a per-API `schema.graphql`.
3. **gqlgen** runs over each API's `gqlgen.yml`, producing `generated.go` + `models_gen.go` + resolver stubs.
4. Resolvers (hand-written, checked in) call ent query/mutation methods on a shared `*ent.Client`.
5. Tests use `NewFixture(t)` from `server.go` — one in-memory SQLite client, three `httptest.Server`s (one per API).
6. Tests execute real GraphQL-over-HTTP requests and assert on the JSON responses.

## Regenerating after schema changes

Any edit under `schema/` or any change to `entgqlmulti/generator.go`:

```bash
task generate && task test
```

Resolvers in `api/*/schema.resolvers.go` are hand-written but survive regeneration — gqlgen copies implementations through when regenerating. Only the `generated.go` / `models_gen.go` files get rewritten.

## Why separate from `testent`

`testent/` exercises `entcascade` at the ent runtime layer (no GraphQL). `testentgqlmulti/` is specifically for `entgqlmulti`'s SDL generation + the compile-time contract with gqlgen. Keeping them apart lets each use the ent feature set it needs (e.g., entgqlmulti needs `entgql` + `entgql.OrderField` field annotations; entcascade does not).
