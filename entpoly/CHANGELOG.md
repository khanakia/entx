# Changelog

All notable changes to the `entpoly` package will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **`.GQL()` predicate-vs-cast collision when `MorphMixin` is used
  without `MixinAllowed`.** The codegen template unconditionally wrapped
  morph keys with `<entityPkg>.<TypeField>(string(...))`. That call is
  a valid type conversion only when ent emitted the type column as a
  `field.Enum` (i.e. `MixinAllowed(...)` was set on the mixin) — in
  which case `<entityPkg>.<TypeField>` is a named string type. When
  `MixinAllowed` is absent the type column falls back to `field.String`
  and the same identifier resolves to ent's predicate-EQ shortcut
  function instead. Wrapping a value with it is a function call, not
  a cast, and the generated `polymorphic.go` failed to compile with
  errors like:
  ```
  cannot use comment.CommentableType(string(p.MorphKey()))
    (value of type predicate.Comment) as string value in argument
    to SetCommentableType
  cannot convert (untyped constant true) to type string
  invalid case ... in switch (mismatched types predicate.Comment and string)
  ```
  preprocess now detects whether the type column was emitted as an
  enum (`t.Fields[*].Enums`) and threads the flag (`TypeIsEnum`)
  through `childInfo` → `childData` / `parentInfo` → `parentData` /
  `holderInfo` → `holderData`. The template branches on `TypeIsEnum`
  at every site that previously emitted the wrap: child Set/Update/
  UpdateOne/Mutation setters, `*Is` / `*IsType` predicates, switch
  cases in `Query<Rel>` and the eager-load `With<Rel>` loader, the
  cascade-delete hook, parent-side `MorphOne` / `MorphMany` back-refs,
  and holder-side `MorphedByMany` back-refs. Enum mode keeps the cast
  (the original two-named-types reason still applies); plain-string
  mode emits the inner `string(...)` directly.
- **`MorphedByMany.Through()` defaulted the wrong morph name when the
  pivot's table name didn't share a stem with the pivot's `MorphTo`
  morph name.** `Through("source_links", SourceLink.Type)` would
  default `MorphName` to `singularise("source_links") = "source_link"`,
  while the pivot's actual declaration was `MorphTo("sourceable", ...)`
  — so the back-ref's column accessors were resolved as
  `sourcelink.SourceLinkTypeEQ` / `p.SourceLinkID` (undefined symbols)
  instead of `sourcelink.SourceableTypeEQ` / `p.SourceableID`. The
  builder no longer defaults `MorphName` in `Through()`; preprocess
  resolves it via a pre-pass that indexes each type's `MorphTo` morph
  name before any edges are stripped. Precedence:
  1. Explicit `.MorphName(...)` on the holder builder.
  2. The pivot type's own `MorphTo` `MorphName`.
  3. `singularise(ThroughName)` (the pre-fix behavior; preserved as
     fallback for users with a pivot that has no `MorphTo`).

### Added

- **`MixinIndexName(name string)`** option on `MorphMixin` to override
  the storage key (database-side name) of the composite `(<type>, <id>)`
  index. The default ent-generated name is derived from the entity name
  and column names, which collides across ent modules sharing a single
  database when two modules declare an entity with the same Go name
  and the same morph relation. Postgres index names are schema-global,
  so the second module's `Migrate()` aborted with
  `relation "tag_taggable_type_taggable_id" already exists`. Pass a
  module-prefixed name to keep both modules co-resident:
  ```go
  entpoly.MorphMixin("taggable",
      entpoly.MixinAllowed(Post.Type, Video.Type),
      entpoly.MixinIndexName("media_tags_taggable_type_taggable_id"),
  )
  ```
  No effect when `MixinNoIndex()` is also set — the index is suppressed
  either way. Default remains empty so ent picks the index name as
  before for everyone not hitting the collision.
- **`examples/morphstring/`** — second runnable example covering the
  bare-`MorphMixin` path (no `MixinAllowed`, type column = `field.String`).
  Mirrors `examples/basic` but every mixin is plain-string, so `go build
  ./...` and `go test ./...` exercise the non-enum code path in the
  template. Schemas:
  - `Post`, `Video` — polymorphic parents.
  - `Comment` (`commentable`) — stem-matching child + `.GQL("Commentable")`.
  - `Image` (`imageable`) — stem-matching child, no `.GQL()`.
  - `SourceLink` (`sourceable`) — divergent-name child (no stem overlap),
    regression guard for the second bug-report case.
  - `Bookmark` (`pinnable`) — second divergent-name pair with
    `.GQL("Pinnable")`.
  Runtime smoke test covers `SetCommentable`, `CommentCommentableIs(IsType)`
  predicates, `QueryCommentable` / `GQLCommentable` resolvers,
  `ClearCommentable`, `QueryComments` `morphMany` back-ref, and
  `WithCommentable` batched eager-loader.
- **Template render matrix tests** — `TestRender_Matrix_ChildAxes`
  renders the template across every meaningful axis (`TypeIsEnum`,
  `Required`, `Touch`, `Cascade`, `SoftDelete`, `GQL`, `IDInt`,
  `ChildIDInt`/`Int64`/`UUID`, `ParentIDInt`/`Int64`/`UUID`, plus an
  all-flags case) and parses the output back as Go. 34 sub-tests.
  `TestRender_Matrix_MultipleAllowedTypes` covers per-parent
  `ResolveCases` with mixed ID types (`int` + `int64` + `string` +
  `uuid.UUID`). `TestRender_Matrix_ParentAndHolder` covers
  `MorphOne` / `MorphMany` parent back-refs and `MorphedByMany` holder
  back-refs in both enum and string modes. Negative assertion in every
  non-enum case: the buggy `<ident>.<TypeField>(string(...))` cast must
  not appear in the rendered output.
- **Regression tests** for the `MorphedByMany` morph-name resolution
  fix:
  - `TestPreprocess_MorphedByMany_ResolvesMorphNameFromPivot` —
    asserts `Through("source_links", SourceLink.Type)` with no explicit
    `.MorphName(...)` resolves to `Sourceable*` accessors via the
    pivot's `MorphTo` declaration.
  - `TestPreprocess_MorphedByMany_ExplicitMorphNameWins` — guarantees
    `.MorphName("custom")` overrides the pivot lookup.
  - `TestPreprocess_MorphedByMany_FallbackToSingularise` — pivot
    without `MorphTo` falls back to `singularise(ThroughName)` so
    pre-fix behavior is preserved for users who relied on it.
- **Regression tests** for `MixinIndexName`:
  - `TestMixinIndexName_StorageKeyApplied` — verifies the override
    lands on the index descriptor.
  - `TestMixinIndexName_DefaultUnset` — verifies the option is opt-in
    (default `StorageKey` is empty so ent's default naming continues).

### Changed

- `MorphedByMany.Through()` no longer eagerly defaults `MorphName`.
  Existing callers that chained `.MorphName("…")` are unaffected. Callers
  who relied on the implicit `singularise(table)` default still get it
  via the new preprocess fallback; the only behavior change is that the
  pivot's own `MorphTo` declaration is now consulted first when no
  explicit name was provided. Update the relevant test
  (`TestMorphedByMany_WithThrough`) asserts the builder leaves
  `MorphName` empty for preprocess to resolve.

## [0.1.0]

### Added

- Initial release of the `entpoly` ent codegen extension — Laravel-style
  polymorphic relationships for ent.
- **`MorphMixin(relation, opts...)`** adds the discriminator columns
  (`<relation>_id`, `<relation>_type`) to a child schema. Options:
  - `MixinAllowed(parents...)` — promote the type column to a real
    `field.Enum` so the database CHECK constraint and ent's typed
    predicates enforce the closed set.
  - `MixinIDType("int" | "string")` — switch the id column between
    `field.Int64` and `field.String`.
  - `MixinIDColumn(name)` / `MixinTypeColumn(name)` — column-name
    overrides; preprocess validates the override agrees with the
    matching `MorphTo` edge.
  - `MixinNoIndex()` — suppress the composite `(<type>, <id>)` index.
- **`MorphTo(relation, parents...)`** edge builder for the child side
  of a polymorphic relation. Chainable options:
  - `IDColumn(name)` / `TypeColumn(name)` — column-name overrides on
    the edge.
  - `Required()` — runtime hook that rejects `Save` when the
    discriminator pair is unset (Create) or cleared (Update).
  - `Touch(field?)` — Laravel `$touches` semantics; bumps the parent's
    `updated_at` (or named column) on every child `Save`.
  - `Cascade()` — pre-delete hook on every allowed parent that deletes
    every child row pointing at the parent.
  - `SoftDelete(field?)` — filter parents whose soft-delete column is
    non-null in `Query<Rel>` / `With<Rel>` / `<Child><Rel>OnParent`.
  - `GQL(unionName?)` — emit a `gqlgen`-recognisable union surface
    (type alias + exported `Is<Union>()` markers + `GQL<Rel>(ctx)`
    resolver helper), plus a sidecar `.graphql` fragment when
    `WithGQLSchemaFile(...)` is set on the extension.
- **`MorphOne(field, child, relation)`** / **`MorphMany(field, child,
  relation)`** parent-side back-refs — auto-emit
  `parent.Query<Field>(ctx)` (`*Child` or `*ChildQuery`).
- **`MorphedByMany(field, parent).Through(table, pivot)`** — M2M holder
  back-ref through a pivot schema. Options: `.MorphName(name)`,
  `.InverseName(name)`, `.IDColumn(name)`, `.TypeColumn(name)`.
- **`NewExtension(opts...)`** — codegen extension entry point. Options:
  - `WithMorphMap(map[string]string)` — override the default
    snake-case morph keys.
  - `WithGQLSchemaFile(path)` — write a sidecar `.graphql` file with
    one `union` declaration per `.GQL()` relation.
- **`RegisterPolyHooks(client)`** — install the runtime hooks declared
  via `.Required()`, `.Touch()`, and `.Cascade()`. Call once after
  client creation.
- Auto-emitted Go surface in `polymorphic.go`:
  - Sealed parent interfaces (`<Child><Rel>Parent`) restricting
    `Set<Rel>` to declared `AllowedTypes` (compile-time discriminated
    union via unexported marker methods).
  - Per-parent morph-key constants (`PostMorphKey`, ...) with the
    named `MorphKey` type so typos in literals don't compile.
  - Typed parent resolver `child.Query<Rel>(ctx) (<Child><Rel>Parent,
    error)` — dispatches on the persisted discriminator.
  - Eager-load batcher `query.With<Rel>().All(ctx)` — one query per
    distinct parent type in the batch, typed result map keyed by
    child ID.
  - Per-parent sub-query predicate constructors
    `<Child><Rel>On<Parent>(preds...)` (Laravel `whereHasMorph`
    equivalent).
  - Predicate constructors `<Child><Rel>Is(parent)` and
    `<Child><Rel>IsType(MorphKey)`.
  - GraphQL union surface when `.GQL()` is set: Go type alias matching
    the sealed interface, exported `Is<Union>()` markers per allowed
    parent, and a `child.GQL<Rel>(ctx)` resolver helper.
- Preprocess-time linters:
  - Missing `MorphMixin` → clear error with the exact line to add.
  - Drift between `MixinAllowed(...)` enum values and `MorphTo`
    `AllowedTypes` → symmetric-diff error with remediation hint.
  - `MorphedByMany` without `.Through(...)` or without a parent type →
    clear error.
  - Ghost FK columns ent's edge processor would otherwise leave behind
    after we strip our edges are removed.
- Auto-detection per allowed parent of the soft-delete column when
  `MorphTo(...).SoftDelete()` is set (skips parents that don't declare
  the column).
- Non-builtin parent ID Go types (`uuid.UUID`, ULID, etc.) — `idGoType`
  surfaces the import path so the generated `polymorphic.go` imports
  the right package and uses the right parse function (`uuid.Parse`,
  `strconv.Atoi`, `strconv.ParseInt`).
- Example projects:
  - `examples/basic` — every feature wired into a single `ent` package
    with an in-memory SQLite test (`runtime_test.go`).
  - `examples/uuid` — UUID-typed parent IDs across `MorphTo`,
    `MorphedByMany`, and the resolver.

[Unreleased]: https://github.com/khanakia/entx/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/khanakia/entx/releases/tag/v0.1.0
