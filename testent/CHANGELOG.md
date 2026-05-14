# Changelog

All notable changes to the `testent` integration harness will be
documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This module is a test harness for `entcascade`, not a library — its
version tracks the state of the scenario coverage rather than a public
API.

## [Unreleased]

### Added

- Nested-transaction tests covering
  `entcascade`'s `tx.Client()` composition path:
  - `TestCascadeNestedTx_NoErrTxStarted` — confirms generated
    `CascadeDeleteX` reuses an outer transaction instead of returning
    `ent.ErrTxStarted`.
  - `TestCascadeNestedTx_ComposeMultiple` — multiple cascades plus
    arbitrary work inside a single atomic block.
  - `TestCascadeNestedTx_WithHooks` — pre/post hooks run inside the
    composed transaction.
  - `TestCascadeNestedTx_OuterRollbackUndoesCascade` — outer rollback
    undoes the cascade.
  - `TestCascadeNestedTx_HookErrorRollsBackOuter` — a hook error
    propagates and rolls back the outer tx.
  - `TestCascadeNestedTx_StandaloneStillWorks` — standalone (no outer
    tx) behavior is unchanged.
- Nested-annotation cascade tests covering the `buildChildOps` fix in
  `entcascade`:
  - `TestCascadeDeleteWorkspace_NestedUnlink` — channels survive a
    chatbot delete with `folder_id = NULL` (intermediate
    `Folder.Cascade(WithUnlink("channels"))` honoured).
  - `TestCascadeDeleteWorkspace_NestedSoftDelete` — notes are
    soft-deleted via a non-default `archived_at` column, exercising
    `Doc.WithSoftDelete("notes", "archived_at")` on an intermediate
    type.
  - `TestCascadeDeleteWorkspace_EmptyChildren` — edge case for the
    nested-annotation code path.
- Backing schemas for the nested tests: `Workspace`, `Folder`,
  `Channel`, `Doc`, `Note`, `AuditLog`.
- `TESTS.md` — index of all integration tests and their coverage areas.
- Godoc comments on every test function describing the use case it
  covers.

### Fixed

- **`TestCascadeDeleteTeam_SkipOwner` was a no-op.** `Team`'s
  `SkipEdges("owner")` annotation was misleading: `owner` is a
  parent-pointing M2O edge (Team owns `owner_id`), which `entcascade`
  auto-skips regardless of the annotation. Removing `SkipEdges` from
  the Team schema would not have made the test fail. Added an
  `AuditLog` schema (a real O2M child of Team that `entcascade` would
  walk and hard-delete without the annotation), changed Team's
  annotation to `SkipEdges("audit_logs")`, and the
  `TestCascadeDeleteTeam_SkipEdges` /
  `TestCascadeDeleteTeamBatch` assertions now actually exercise the
  skip path: audit logs survive team deletion.

## [0.1.0]

### Added

- Initial release of the `testent` integration harness for
  `entcascade`. 24 test cases backed by an in-memory SQLite database,
  covering:
  - Basic single-edge cascade delete and batch delete
    (`CascadeDeleteX`, `CascadeDeleteXBatch`).
  - Pre/post hooks executed inside the cascade transaction
    (`CascadeDeleteXWithHooks`).
  - Soft-delete auto-detection from `deleted_at` columns.
  - `WithUnlink` (SET NULL) and `WithHardDelete` overrides.
  - `SkipEdges` to opt children out of the walk.
  - M2M through-tables and pivot row cleanup.
  - Default fall-through behavior (auto-detect soft delete vs hard
    delete).
- Generated `ent/` package committed alongside the schemas so the test
  suite stays reproducible without depending on the generator running
  first.

[Unreleased]: https://github.com/khanakia/entx/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/khanakia/entx/releases/tag/v0.1.0
