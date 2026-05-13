# testentpoly — scenario matrix

Real-world integration harness for [`entpoly`](../entpoly). Exercises every shipped feature against an in-memory SQLite database and a real HTTP GraphQL server.

This file is the **source of truth for what the harness covers**. Each scenario maps to one or more tests in `*_test.go`. When a new entpoly feature lands, append a row here first, then write the test.

## Scope — schemas

| Schema | PK type | Role | Features exercised |
|---|---|---|---|
| `Post` | int | Parent | `MorphMany("comments")`, `MorphedByMany("tags")`, `published` field for sub-query predicates |
| `Video` | int | Parent | `MorphMany("comments")`, `MorphedByMany("tags")` |
| `Image` | int | Parent | `MorphOne("featured_image")` target — exactly-one shape |
| `Document` | uuid.UUID | Parent | UUID PK round-trip, `deleted_at` for soft-delete-aware reads |
| `Report` | uuid.UUID | Parent | UUID PK, no soft-delete (per-target auto-detection) |
| `Comment` | int | Child | `MorphTo("commentable").Required().Touch().Cascade().SoftDelete().GQL()` |
| `Annotation` | int | Child (UUID parents) | `MorphTo("target", Document, Report)` — UUID end-to-end |
| `Tag` + `Taggable` | int | M2M pivot | `MorphedByMany` inverse, attach/detach/sync via helper |
| `Folder` | int | Self-ref parent + child | `MorphTo("parent", Folder)` listed in its own AllowedTypes |
| `Event` (custom-cols) | int | Child | `MixinIDColumn("entity_pk")` + `MixinTypeColumn("entity_kind")`, custom MorphKey via `WithMorphMap` |

## Scenarios — runtime (SQLite, in-process)

| # | Scenario | Test | What it proves |
|---|---|---|---|
| 1 | CRUD: SetCommentable / ClearCommentable / reassign across types | `TestCRUD_SetClearReassign` | Both discriminator columns flip together on Save; nil after Clear |
| 2 | Forward resolve sealed iface — type switch on AllowedTypes | `TestForwardResolve_SealedSwitch` | `QueryCommentable(ctx)` returns sealed interface; type-switch hits `*Post`/`*Video`/`nil` only |
| 3 | Reverse: `post.QueryComments()` w/ predicates | `TestReverse_QueryWithPredicates` | Composable `*CommentQuery` chains `.Where(...).Limit(...).All(ctx)` |
| 4 | Eager-load `WithCommentable()` — assert 1+N queries via SQL log | `TestEagerLoad_BatchedPerType` | One SELECT per parent type, not per child row |
| 5 | M2M attach / detach / sync / syncWithoutDetach / toggle | `TestM2M_HelperRoundTrips` | All five helpers move pivot rows correctly |
| 6 | Auto-emitted inverse `post.QueryTags(ctx)` | `TestM2M_AutoInverseFromHolder` | Single `MorphedByMany` decl on Tag emits both directions |
| 7 | Typed predicates `CommentCommentableIs(post)` + `IsType(PostMorphKey)` | `TestPredicates_TypedConstructors` | Parent-bound + key-bound predicates filter correctly |
| 8 | `OnPost(post.PublishedEQ(true))` whereMorphRelation | `TestPredicates_OnParentSubquery` | Per-parent sub-query helper composes with `comment.Or(...)` |
| 9a | Hook: `Required()` rejects unset on Create + cleared on Update | `TestHook_Required` | Save returns explicit error; no row written |
| 9b | Hook: `Touch()` bumps parent's `updated_at` on every child Save | `TestHook_Touch` | Parent ts strictly newer than pre-save; fires on Create + Update |
| 9c | Hook: `Cascade()` deletes children on parent delete | `TestHook_Cascade` | Children w/ matching discriminator gone post-delete; siblings untouched |
| 9d | Hook: `SoftDelete()` filters soft-deleted parents from reads | `TestHook_SoftDelete` | Forward resolve + eager-load skip parents w/ non-null `deleted_at` |
| 10 | UUID round-trip | `TestUUID_FullRoundTrip` | Annotation.SetTarget(doc) writes uuid string; reverse resolve type-asserts back to `*Document` |
| 11 | Self-referential polymorphic | `TestSelfReferential_Folder` | Folder can host another Folder as its parent |
| 12 | Morph map rename — old key in DB, new alias in code | `TestMorphMap_Rename` | After `WithMorphMap({"legacy_post":"Post"})`, both old + new rows resolve to `*Post` |
| 13 | Custom column names round-trip | `TestCustomColumns_RoundTrip` | `entity_pk` / `entity_kind` columns used end-to-end |
| 14 | Multiple poly relations on one schema | `TestMultiRelation_OnOneSchema` | Two `MorphMixin` + two `MorphTo` declarations coexist without collision |
| 15 | Mixed-PK AllowedTypes (drift linter — negative codegen test) | `TestDriftLinter_MixedPKsRejected` | `go generate` exits non-zero with precise diff message |
| 16 | Mixin / edge AllowedTypes mismatch (drift linter — negative codegen test) | `TestDriftLinter_AllowedMismatch` | Codegen aborts; error names both lists |
| 17 | Ghost-FK column suppression | `TestGhostFK_NoLeftoverFields` | Comment struct has no `post_comments *int` cruft (reflection assertion) |

## Scenarios — GraphQL HTTP (gqlgen + httptest)

| # | Scenario | Test | What it proves |
|---|---|---|---|
| 18 | Union query `commentable { ... on Post { title } }` | `TestGQL_UnionQueryResolvesParent` | Real HTTP POST returns correctly typed parent inside union |
| 19 | Union over multiple parent types in one result set | `TestGQL_UnionMixedParents` | Mixed `*Post` + `*Video` children resolve to right union members |
| 20 | Forward resolver helper `GQLCommentable(ctx)` returns same as `QueryCommentable(ctx)` | `TestGQL_ResolverHelperParity` | Both call sites return identical parent id |
| 21 | Union member `IsCommentable()` marker satisfies gqlgen interface contract | `TestGQL_MarkerMethods` | Generated `*Post` + `*Video` both type-assert to `ent.Commentable` |
| 22 | Custom union name via `.GQL("CommentTarget")` | `TestGQL_CustomUnionName` | Emitted `.graphql` fragment declares `union CommentTarget = ...` |
| 23 | Eager-load through GraphQL connection | `TestGQL_NestedEagerLoad` | `comments { commentable { ... on Post { title }}}` runs in 1+N queries, not N+1 |
| 24 | Soft-deleted parent returns null in GraphQL union | `TestGQL_SoftDeletedParentNull` | Comment whose parent is soft-deleted → `commentable: null` in response |

## Scenarios — structural (parse generated artifacts, no server)

| # | Scenario | Test | What it proves |
|---|---|---|---|
| 25 | Generated `polymorphic.go` exports expected symbols | `TestArtifact_GeneratedSymbols` | `Morphable`, `*MorphKey`, sealed iface, `RegisterPolyHooks` all present |
| 26 | Emitted `.graphql` fragment is valid SDL | `TestArtifact_GraphQLFragmentParses` | `gqlparser` parses the fragment without errors |
| 27 | Generated SQL CHECK constraint on type column | `TestArtifact_EnumCheckConstraint` | SQLite `sqlite_master` query confirms enum constraint on `commentable_type` |

## Layout (target)

```
testentpoly/
├── schema/                    # ent schemas (int + uuid + self-ref + custom-cols)
├── entc.go                    # ent codegen entry — wires entpoly.NewExtension + WithGQLSchemaFile
├── ent/                       # generated ent code
├── api/
│   └── gql/                   # gqlgen-generated code + resolvers for the union surface
├── server.go                  # Fixture: ent.Client + httptest.Server wired to gqlgen handler
├── gql_client.go              # HTTP GraphQL helper for tests
├── crud_test.go               # scenarios 1–3
├── eagerload_test.go          # scenarios 4
├── m2m_test.go                # scenarios 5–6
├── predicates_test.go         # scenarios 7–8
├── hooks_test.go              # scenarios 9a–9d
├── uuid_test.go               # scenario 10
├── selfref_test.go            # scenario 11
├── morphmap_test.go           # scenario 12
├── customcols_test.go         # scenario 13
├── multirelation_test.go      # scenario 14
├── drift_test.go              # scenarios 15–16 (sub-process go generate)
├── ghostfk_test.go            # scenario 17
├── gql_test.go                # scenarios 18–24
├── artifact_test.go           # scenarios 25–27
├── Taskfile.yml               # generate / test / build / clean
└── README.md                  # how to run + harness explanation
```

## Running

From `testentpoly/`:

```bash
task generate          # ent + gqlgen codegen
task test              # run every scenario (verbose)
task test:short        # without -v
task build             # compile everything
task clean             # nuke generated artifacts
```

## Regenerating after entpoly changes

Any edit under `../entpoly/` or `schema/`:

```bash
task generate && task test
```

## Why separate from `entpoly/examples/`

`entpoly/examples/basic` + `entpoly/examples/uuid` are user-facing **runnable docs** — they stay small, focused on one shape each, and ship in the README as copy-pasteable snippets. `testentpoly/` is the **exhaustive integration harness** — kitchen-sink schemas, every feature combined, GraphQL HTTP layer, drift-linter negative tests. Mixing the two would (a) bloat the docs examples past the point of being readable, and (b) couple test depth to user-facing copy.

Same split as `testent` ↔ `entcascade/examples` and `testentgqlmulti` ↔ `entgqlmulti/examples`.
