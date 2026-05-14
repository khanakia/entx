# Changelog

All notable changes to the `testentgqlmulti` integration harness will be
documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This module is a test harness for `entgqlmulti`, not a library — its
version tracks the state of the scenario coverage rather than a public
API.

## [Unreleased]

_No changes since [0.1.0]._

## [0.1.0]

### Added

- Initial release of the `testentgqlmulti` end-to-end harness inside
  the `entx` workspace. Generates real `ent` + `entgql` + `entgqlmulti`
  code, runs `gqlgen` on the per-API schemas, wires everything to an
  in-memory SQLite database, and executes real GraphQL-over-HTTP
  requests against three APIs:
  - **dashboard** — full CRUD, every entity, both queries and
    mutations.
  - **public** — read-only projection (queries only, subset of
    entities, no mutations).
  - **mobile** — narrow projection with custom `TypeName`s and
    fields-whitelisted entities.
- **15 tests across 4 files** — 12 runtime tests and 3 structural
  SDL assertions. See `TESTS.md` for the full catalog.
- Discovered and fixed three latent `entgqlmulti` bugs while building
  the harness; the fixes themselves are recorded in
  `entgqlmulti/CHANGELOG.md`:
  - `scalar [ID!]` leak in generated SDL.
  - camelCase entries in `ApiTarget.Fields` silently dropped.
  - Orphan edge predicates broke `gqlgen` compile in subset APIs.
- `TESTS.md` — index of every scenario covered by the harness with the
  feature each one exercises.

[Unreleased]: https://github.com/khanakia/entx/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/khanakia/entx/releases/tag/v0.1.0
