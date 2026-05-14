# Changelog

This workspace ships several independent Go modules. Each module keeps
its own detailed changelog; this file is an **index** plus a summary of
the highlights per module's `[Unreleased]` section, so a workspace user
can see at a glance what has moved.

The per-module files follow [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Modules and per-module changelogs

| Module | Kind | Changelog |
|---|---|---|
| `entcascade` | Library — ent codegen extension (cascade deletes) | [entcascade/CHANGELOG.md](entcascade/CHANGELOG.md) |
| `entgqlmulti` | Library — ent+entgql extension (per-API SDL split) | [entgqlmulti/CHANGELOG.md](entgqlmulti/CHANGELOG.md) |
| `entpoly` | Library — ent codegen extension (polymorphic relations) | [entpoly/CHANGELOG.md](entpoly/CHANGELOG.md) |
| `testent` | Test harness for `entcascade` | [testent/CHANGELOG.md](testent/CHANGELOG.md) |
| `testentgqlmulti` | Test harness for `entgqlmulti` | [testentgqlmulti/CHANGELOG.md](testentgqlmulti/CHANGELOG.md) |
| `testentpoly` | Test harness for `entpoly` | [testentpoly/CHANGELOG.md](testentpoly/CHANGELOG.md) |

## What's in each module's `[Unreleased]` right now

### `entpoly`

- **Fixed** — `.GQL()` predicate-vs-cast collision when `MorphMixin`
  is used without `MixinAllowed` (the type column was `field.String`,
  not `field.Enum`, so wrapping a value with `<entityPkg>.<TypeField>`
  was a function call, not a cast).
- **Fixed** — `MorphedByMany.Through()` defaulted the wrong morph name
  whenever the pivot's table name didn't share a stem with the pivot's
  `MorphTo` morph name (e.g. `Through("source_links", SourceLink.Type)`
  with the pivot declaring `MorphTo("sourceable", ...)`). Resolution
  moved to preprocess: explicit `.MorphName(...)` > pivot's `MorphTo` >
  `singularise(ThroughName)` fallback.
- **Added** — `MixinIndexName(name)` option to override the composite
  index storage key, so two ent modules sharing one database can
  declare an entity with the same Go name and morph relation without
  the Postgres index-name collision.
- **Added** — second runnable example `examples/morphstring/` covering
  the bare-`MorphMixin` (no `MixinAllowed`) code path, plus matrix
  render tests and regression tests for every fix above.

### `entcascade`

- **Added** — nested-transaction composition. `CascadeDeleteX`,
  `CascadeDeleteXWithHooks`, and `CascadeDeleteXBatch` detect a
  caller's transactional client and reuse the existing transaction
  instead of starting a new one.
- **Fixed** — intermediate-type `Cascade()` annotations are now
  respected during nested cascades (`buildChildOps` applies the same
  rule-priority ladder as `buildOps` instead of only reading the root
  type's annotation).

### `entgqlmulti`

- **Added** — end-to-end integration harness at `testentgqlmulti/`.
- **Fixed** — `scalar [ID!]` leak in generated SDL.
- **Fixed** — camelCase entries in `ApiTarget.Fields` silently dropped.
- **Fixed** — orphan edge predicates broke `gqlgen` compile in subset
  APIs.

### `testent` / `testentgqlmulti` / `testentpoly`

These are test harnesses; their changelogs track the state of scenario
coverage rather than a public API. See the per-module files above for
details.

## How to read these

- Public libraries (`entcascade`, `entgqlmulti`, `entpoly`) follow
  SemVer. Their `[Unreleased]` section accumulates everything that will
  go into the next tagged release.
- Test harnesses (`test*` modules) don't ship a public API; their
  changelogs help reviewers understand what new ground each commit
  covers.
- A workspace-wide release is the union of all six modules' tagged
  versions at the SHA being released. There is no single workspace
  version number — `go.work` is a development convenience, not a
  release artefact.
