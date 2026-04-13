# Cascade Delete Test Coverage

The integration tests in `cascade_delete_test.go` exercise every feature of the
`entcascade` extension against real ent-generated code and an in-memory SQLite
database. Each test is documented inline with a godoc comment; this table is a
quick index.

| #  | Test                                    | Use case                                                                               |
| -- | --------------------------------------- | -------------------------------------------------------------------------------------- |
| 1  | `..._Basic`                             | Full single-entity cascade (posts, comments, M2M, profile); shared tags survive        |
| 2  | `...Post_Nested`                        | Mid-tree cascade (parent user untouched)                                               |
| 3  | `...Category_Unlink`                    | `WithUnlink` rule — SET NULL on children                                               |
| 4  | `...Article_SoftDelete`                 | Auto-detected soft delete via `deleted_at`                                             |
| 5  | `...Team_SkipOwner`                     | `SkipEdges` preserves a specific edge                                                  |
| 6  | `...UserBatch`                          | Batch API with multiple ids                                                            |
| 7  | `...UserBatch_Empty`                    | Empty-slice short-circuit                                                              |
| 8  | `...WithHooks`                          | Pre/Post hooks inside transaction                                                      |
| 9  | `...PreHookError`                       | Pre-hook abort + rollback                                                              |
| 10 | `..._Idempotent`                        | Double-delete no-op                                                                    |
| 11 | `..._DeepNested`                        | 3-level fan-out cascade                                                                |
| 12 | `..._NoChildren`                        | Root with zero dependents                                                              |
| 13 | `...PostHookError`                      | Late-failure rollback after deletes ran                                                |
| 14 | `...Batch_SingleItem`                   | Batch with one id matches single call                                                  |
| 15 | `...UnlinkNoChildren`                   | Unlink with zero linked rows                                                           |
| 16 | `...MultipleTags`                       | N junction rows removed, tags survive                                                  |
| 17 | `...SoftDeleteIdempotent`               | Second soft-delete is no-op                                                            |
| 18 | `...CategoryBatch_Unlink`               | Batch + unlink together                                                                |
| 19 | `..._ProfileOnly`                       | Partial subtree, some ops find zero rows                                               |
| 20 | `...ArticleBatch_SoftDelete`            | Batch + soft delete                                                                    |
| 21 | `..._Isolation`                         | Blast-radius containment between siblings                                              |
| 22 | `...UnlinkPreservesData`                | Non-FK columns untouched by UPDATE                                                     |
| 23 | `...TeamBatch`                          | Batch + SkipEdges                                                                      |
| 24 | `...NonexistentID`                      | ID that never existed                                                                  |
| 25 | `...Workspace_NestedUnlink`             | **Regression: nested `WithUnlink` on intermediate type**                               |
| 26 | `...Workspace_NestedSoftDelete`         | **Regression: nested `WithSoftDelete` (non-default field) on intermediate**            |
| 27 | `...Workspace_EmptyChildren`            | Nested-annotation path with empty children                                             |
| 28 | `NestedTx_NoErrTxStarted`               | **Regression: cascade under `tx.Client()` must not return `ErrTxStarted`**             |
| 29 | `NestedTx_ComposeMultiple`              | Multiple cascade calls inside one outer tx commit atomically                            |
| 30 | `NestedTx_WithHooks`                    | Pre/Post hooks fire under a caller-owned tx                                            |
| 31 | `NestedTx_OuterRollbackUndoesCascade`   | Outer rollback after cascade undoes cascaded deletes (proves tx reuse)                 |
| 32 | `NestedTx_HookErrorRollsBackOuter`      | Hook error propagates; caller rollback undoes prior cascade steps in same tx           |
| 33 | `NestedTx_StandaloneStillWorks`         | Non-regression: cascade without outer tx still creates+commits its own                 |

## Running

```
GOWORK=off go test -v .
```

## Regenerating ent code after schema changes

```
GOWORK=off go run entc.go
```
