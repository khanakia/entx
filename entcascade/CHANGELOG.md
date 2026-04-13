# Changelog

All notable changes to the `entcascade` package will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- Respect `Cascade()` annotations on intermediate types during nested
  cascades. Previously, `buildChildOps` walked a parent type's edges
  recursively but only read the root type's annotation — any
  `WithUnlink`, `WithSoftDelete`, `SkipEdges`, or `WithHardDelete` rules
  declared on an intermediate type were silently ignored. For example,
  `Folder.Cascade(WithUnlink("channels"))` was not honored when `Chatbot`
  cascaded through `Folder` into `Channel`, causing channels to be
  hard-deleted instead of unlinked. `buildChildOps` now applies the same
  rule-priority ladder as `buildOps`
  (skip → unlink → soft delete → hard delete override → auto-detect →
  default), so intermediate-type policies are respected throughout the
  traversal.

### Added

- Integration tests in `testent/` for the nested-annotation path:
  - `TestCascadeDeleteWorkspace_NestedUnlink` — regression for the bug
    above; asserts channels survive with `folder_id = NULL`.
  - `TestCascadeDeleteWorkspace_NestedSoftDelete` — asserts notes are
    soft-deleted via a non-default `archived_at` column, which is only
    possible when the intermediate `Doc.WithSoftDelete("notes", "archived_at")`
    annotation is read during the nested walk.
  - `TestCascadeDeleteWorkspace_EmptyChildren` — edge case for the
    nested-annotation code path.
- New schemas backing these tests: `Workspace`, `Folder`, `Channel`,
  `Doc`, `Note`.
- `testent/TESTS.md` — index of all integration tests and their coverage.
- Godoc comments on every test function describing the use case it covers.

### Changed

- `entcascade/README.md` now documents that intermediate types'
  annotations are respected in nested cascades.

## [0.1.0]

### Added

- Initial release of the `entcascade` ent codegen extension. Generates
  transactional cascade-delete functions (`CascadeDeleteX`,
  `CascadeDeleteXBatch`, `CascadeDeleteXWithHooks`) for types annotated
  with `entcascade.Cascade()`. Supports `SkipEdges`, `WithUnlink`,
  `WithSoftDelete`, `WithHardDelete`, auto-detection of `deleted_at`,
  M2M through-tables, and pre/post hooks inside the transaction.

[Unreleased]: https://github.com/khanakia/entx/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/khanakia/entx/releases/tag/v0.1.0
