# testentpoly

End-to-end integration harness for the [`entpoly`](../entpoly) module — Laravel-style polymorphic relationships for the ent ORM.

This is the kitchen-sink test suite: every shipped entpoly feature exercised against a real in-memory SQLite database and a real HTTP GraphQL server (gqlgen). Sibling examples at [`entpoly/examples/`](../entpoly/examples) stay small and copy-pasteable; this harness goes wide.

| | |
|---|---|
| **Spec** | [`SCENARIOS.md`](./SCENARIOS.md) — 27-row coverage matrix, source of truth |
| **GraphQL queries** | [`QUERIES.md`](./QUERIES.md) — paste-ready queries + curl |
| **Status** | 28 PASS / 2 SKIP / 0 FAIL across 5 phases |

## Quick start

```bash
task generate            # ent codegen + gqlgen codegen
task test                # run every scenario (verbose)
task serve               # start a real GraphQL server on :8080
```

Hit `http://localhost:8080/` for the playground or `POST /query` for the API — both seeded with sample Posts, Videos, Images and Comments so the polymorphic union has data to resolve on first load.

## What the harness covers

Five phases of feature coverage. Each phase builds on the previous — `task test` runs everything in one shot.

| Phase | Scope | Files |
|---|---|---|
| **1** | CRUD, forward-resolve, reverse-query, eager-load batching, typed predicates, ghost-FK suppression | `crud_test.go`, `eagerload_test.go`, `predicates_test.go`, `ghostfk_test.go` |
| **2** | Hook options — `Required()` / `Touch()` / `Cascade()` / `SoftDelete()`, UUID parent PKs | `hooks_test.go`, `uuid_test.go` |
| **3** | Polymorphic M2M, self-referential, multi-relation, custom column names, morph-map rename | `m2m_test.go`, `selfref_test.go`, `customcols_test.go`, `multirelation_test.go`, `morphmap_test.go` |
| **4** | Drift-linter negative codegen tests + generated-artifact assertions | `drift_test.go`, `artifact_test.go` |
| **5** | GraphQL union surface over HTTP — query / mixed parents / resolver helper / marker methods / custom union name / nested eager-load / soft-deleted nulls | `gql_test.go` |

See [`SCENARIOS.md`](./SCENARIOS.md) for the full row-per-scenario table mapping every `Test*` function back to its scenario number.

## Layout

```
testentpoly/
├── SCENARIOS.md           — coverage matrix (spec)
├── QUERIES.md             — paste-ready GraphQL queries + curl
├── schema/                — ent schemas (Post, Video, Image, Document, Report,
│                            Comment, Annotation, Tag, Taggable, Folder, Event)
├── entc.go                — ent codegen entry, wires entpoly.NewExtension
├── ent/                   — generated, committed
├── api/gql/               — gqlgen-generated code + hand-written resolvers
│   ├── schema.graphql     — main GraphQL schema
│   ├── polymorphic.graphql— entpoly-emitted union declarations
│   ├── schema.resolvers.go
│   └── generated.go       — gqlgen output
├── cmd/serve/             — standalone server binary (task serve)
├── server.go              — test fixtures (openTestClient, newGQLServer)
├── tracer.go              — SQL driver wrapper that counts queries
├── gql_client.go          — HTTP GraphQL helper for *_test.go
├── *_test.go              — one file per phase / scenario group
└── Taskfile.yml           — generate / test / build / serve / clean
```

## Running

All tasks live in [`Taskfile.yml`](./Taskfile.yml).

```bash
task                       # list all tasks
task generate              # ent + gqlgen + go mod tidy
task test                  # verbose; runs every scenario
task test:short            # without -v
task build                 # compile-check the whole tree
task serve                 # standalone GraphQL server on :8080
task clean                 # remove generated artifacts
```

`GOWORK=off` is set everywhere so codegen decouples from sibling-module workspaces.

### Manual run (without Task)

```bash
GOBIN=$PWD/bin GOWORK=off go install github.com/99designs/gqlgen   # once
GOWORK=off go run entc.go                                           # ent + entpoly
(cd api/gql && ../../bin/gqlgen generate)                           # gqlgen
GOWORK=off go mod tidy
GOWORK=off go test -v -count=1 .
GOWORK=off go run ./cmd/serve                                       # start the server
```

## How the harness works

1. **ent codegen** (`entc.go`) runs ent with `entpoly.NewExtension(...)` registered as a hook. entpoly mutates the graph (strips poly edges, validates AllowedTypes, records state), then emits a sidecar `ent/polymorphic.go` after ent's normal output.
2. **gqlgen** runs over `api/gql/gqlgen.yml`, consuming `schema.graphql` + the entpoly-emitted `polymorphic.graphql` fragment.
3. **Runtime hooks** (`Required` / `Touch` / `Cascade` / `SoftDelete`) install via `ent.RegisterPolyHooks(client)`. `server.go` calls this in every test fixture.
4. **Tests** spin up a fresh in-memory SQLite per test, plus (for GQL tests) an `httptest.Server` wrapping the gqlgen handler. Real HTTP, real SQL, no mocks.
5. **The query tracer** (`tracer.go`) wraps the SQL driver to count `SELECT`s per table — that's how the eager-load test proves entpoly's 1+N (not N+1) batching.

## Regenerating after schema or entpoly changes

Any edit under `schema/` or `../entpoly/`:

```bash
task generate && task test
```

Resolvers in `api/gql/schema.resolvers.go` are hand-written but survive regeneration — gqlgen copies implementations through, only rewrites `generated.go` / `models_gen.go`.

## Known skips

Two scenarios skip with explicit `t.Skip(...)` messages pointing at upstream entpoly gaps:

| Test | Skip reason |
|---|---|
| `TestMorphMap_Rename` | `WithMorphMap` emits duplicate `*MorphKey` constants when two aliases share a schema |
| `TestDriftLinter_MixedPKsRejected` | entpoly does not yet ship a mixed-PK linter |

Both flip to real assertions when the corresponding entpoly feature lands.

## Why separate from `entpoly/examples/`

`entpoly/examples/basic` + `entpoly/examples/uuid` are **runnable docs** — small, focused, copy-pasteable, ship in the README. `testentpoly/` is the **exhaustive integration harness** — kitchen-sink schemas, every feature combined, GraphQL HTTP layer, drift-linter negative tests. Splitting them keeps the examples tight and lets the harness go as deep as it needs to.

Same split as `testent` ↔ `entcascade/examples` and `testentgqlmulti` ↔ `entgqlmulti/examples`.

## See also

- [`SCENARIOS.md`](./SCENARIOS.md) — every scenario, every test name
- [`QUERIES.md`](./QUERIES.md) — paste-ready GraphQL queries
- [`../entpoly/README.md`](../entpoly/README.md) — entpoly user docs
- [`../entpoly/docs/architecture.md`](../entpoly/docs/architecture.md) — how the codegen pipeline is built
- [`../testentgqlmulti/`](../testentgqlmulti/) — sibling harness (entgqlmulti integration tests)
