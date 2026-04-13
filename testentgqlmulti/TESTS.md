# entgqlmulti Test Coverage

Integration tests that exercise the `entgqlmulti` extension end-to-end. Each
test generates real ent + entgql + entgqlmulti code, runs `gqlgen` codegen on
the per-API schemas, compiles everything, and executes queries over an
in-memory SQLite database.

Schemas under `schema/` define three APIs against the same ent types:

| API        | Purpose                                   | Shape                                    |
| ---------- | ----------------------------------------- | ---------------------------------------- |
| `apidash`  | Full CRUD dashboard                       | All types, mutations, filters, ordering  |
| `apipub`   | Read-only public surface                  | Subset types (`PublicUser`, etc.), queries only |
| `apimobile`| Narrow mobile projection                  | `me` query, camelCase Fields whitelist   |

## Runtime integration tests

Each test spins up three `httptest.Server` instances — one per API — backed
by a single `*ent.Client`, and issues real GraphQL HTTP requests.

| # | Test                                  | What it proves                                                               |
| - | ------------------------------------- | ---------------------------------------------------------------------------- |
| 1 | `TestApidash_CreateAndQueryUser`      | Full CRUD: mutation creates, query returns paginated connection              |
| 2 | `TestApidash_OrderByAndWhere`         | `orderBy` + `where` args are wired and affect query results                  |
| 3 | `TestApidash_MultiTargetPerEntity`    | Two GraphQL types (`Chatbot` + `ChatbotSummary`) from one ent entity         |
| 4 | `TestApidash_UpdateMutation`          | `updateChatbot` mutation round-trips ID and mutates field                    |
| 5 | `TestApipub_SubsetFieldsOnly`         | Whitelisted fields expose; non-whitelisted (`email`) rejected by validation  |
| 6 | `TestApipub_NoMutationRoot`           | `Mutations: false` omits the `Mutation` root type entirely                   |
| 7 | `TestApipub_TagsSameName`             | Same entity name shared across APIs without @goModel collision               |
| 8 | `TestApipub_FiltersWithEdgePruning`   | `hasPosts` kept; `hasPostsWith: [PostWhereInput!]` pruned when Post absent   |
| 9 | `TestApipub_PublicChatbotRenamed`     | @goModel binds subset type to ent struct; sensitive field stays hidden       |
| 10| `TestApimobile_QueryNameOverride`     | `QueryName: "me"` replaces default `mobileUsers`                             |
| 11| `TestApimobile_CamelCaseFieldsInput`  | CamelCase entries in `Fields` (e.g. `firstName`) normalize correctly         |
| 12| `TestApimobile_NoFiltersOrOrderBy`    | Missing `Filters`/`OrderBy` flags → no `where`/`orderBy` args on query       |

## Structural SDL tests

These parse the generated `.graphql` files with `gqlparser` and assert shape
without spinning up servers.

| # | Test                          | What it proves                                                         |
| - | ----------------------------- | ---------------------------------------------------------------------- |
| 13| `TestSchemaShape_Apidash`     | Full type inventory, `Secret` absent, @goModel only on renamed types   |
| 14| `TestSchemaShape_Apipub`      | Mutation root absent, `UserWhereInput` pruned of orphan edge predicates |
| 15| `TestSchemaShape_Apimobile`   | `me` present, `mobileUsers` absent, @goModel binds to `ent.User`       |

## Regenerating everything

```
GOWORK=off go run entc.go                    # ent + entgqlmulti → ent/ and api/*/schema.graphql
GOBIN=$PWD/bin GOWORK=off go install github.com/99designs/gqlgen   # once
(cd api/apidash   && ../../bin/gqlgen generate)
(cd api/apipub    && ../../bin/gqlgen generate)
(cd api/apimobile && ../../bin/gqlgen generate)
GOWORK=off go mod tidy
```

## Running

```
GOWORK=off go test -v -count=1 .
```

## Bugs surfaced during development

Building this test harness surfaced three latent bugs in `entgqlmulti`, all
now fixed:

1. **`scalar [ID!]` leaking into SDL** — entgql produces some `ast.Type`
   values with `NamedType: "[ID!]"` literal instead of a list wrapper. The
   generator's fallback-scalar branch rendered these as `scalar [ID!]`.
   Fix: strip surface-syntax markers (`[`, `]`, `!`) in `resolveTypeName`.

2. **camelCase `Fields` entries dropped** — ent's `camel()` function
   lowercases already-camelCase input (`firstName` → `firstname`), so
   target fields specified as camelCase never matched. Fix: pipe through
   `snake()` first, then `camel()`.

3. **Orphan edge predicates** — `UserWhereInput` in a subset API retained
   `hasPostsWith: [PostWhereInput!]` even when Post was not exported to
   that API, breaking gqlgen compilation. Fix: post-pass prune of any
   WhereInput field whose type references a `*WhereInput` not present in
   the emitted schema.
