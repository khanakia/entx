# Changelog

All notable changes to the `testentpoly` integration harness will be
documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This module is a test harness, not a library — its version tracks the
state of the scenario coverage rather than a public API.

## [Unreleased]

_No changes since [0.1.0]._

Pending work (mirrors the open items in `entpoly/CHANGELOG.md`'s
`[Unreleased]` section): exercising the `MixinIndexName` cross-module
collision and the `MorphedByMany.Through()` morph-name resolution
end-to-end here would require a second ent module in the workspace.
Until then those fixes are covered by `entpoly/examples/morphstring/`
plus unit tests inside `entpoly/`.

## [0.1.0]

### Added

- Initial release of the `testentpoly` integration harness — exercises
  every shipped `entpoly` feature against an in-memory SQLite database
  and a real `gqlgen` HTTP server. Pattern mirrors `testentgqlmulti`.
- **27-row scenario matrix** in `SCENARIOS.md`:
  - Phase 1 — CRUD / forward-resolve / reverse-query / eager-load
    batching via query tracer / typed predicates / ghost-FK
    suppression.
  - Phase 2 — `Required` / `Touch` / `Cascade` / `SoftDelete` hooks
    plus UUID-typed parents.
  - Phase 3 — polymorphic M2M (`attach` / `detach` / `sync` /
    `syncWithoutDetach` / `toggle`), self-referential, multi-relation,
    custom column names, morph-map rename (skipped: upstream
    duplicate-constant bug), drift linter mixed-PK (skipped: upstream
    gap).
  - Phase 4 — generated-artifact assertions (symbols, `.graphql`
    parse, enum constraint).
  - Phase 5 — GraphQL HTTP: union query, mixed parents, resolver
    helper parity, marker methods, custom union name, nested
    eager-load, soft-deleted null.
- **`QUERIES.md`** — 9 paste-ready GraphQL queries with `curl` variants
  for manual exploration via `task serve` (standalone server on
  `:8080` with seeded data).
- **`Taskfile.yml`** — `task test`, `task serve`, `task gql:gen`,
  `task migrate`, etc.
- Result on initial release: **28 PASS / 2 SKIP / 0 FAIL** across all
  phases. Two skips document upstream entpoly gaps tracked in
  `entpoly/docs/architecture.md` § v2 roadmap (`WithMorphMap` duplicate
  constants, mixed-PK drift linter).

### Changed

- Added `./testentpoly` to the workspace `go.work`. Transitive
  consequence of `go work sync`: bumped `google/uuid` from `v1.3.0` to
  `v1.6.0` across the workspace.

### Fixed

- `.gitignore` covers the `serve` binary the standalone GraphQL server
  builds locally; an accidentally-committed copy was removed.

[Unreleased]: https://github.com/khanakia/entx/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/khanakia/entx/releases/tag/v0.1.0
