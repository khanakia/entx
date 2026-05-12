# Architecture

This document describes how `entpoly` is built — the pipeline from schema-level edge declarations to generated Go code, the data structures involved, and the places where future contributors should hook in to extend the surface.

## High-level pipeline

```
ent/schema/*.go                 (schema-as-code authored by the user)
   ├── Mixin()  → MorphMixin → discriminator columns
   └── Edges()  → MorphTo / MorphMany / MorphOne / MorphedByMany builders
       │
       │  user runs `go generate ./ent`
       ▼
ent/entc.go                     (driver: entc.Generate(...))
       │
       │  entc loads schemas (mixins run here too, so the
       │  discriminator columns are part of the loaded schema)
       │  → builds gen.Graph
       ▼
gen.Graph                       (Edges populated from schema.Edges() — the
                                 entpoly edges carry the marker annotation)
       │
       │  entc runs all registered entc.Extensions
       ▼
Extension.Hooks() (entpoly)     (gen middleware — wraps the generator)
       │
       ▼ phase 1
preprocess(g)                   (strips polymorphic edges from gen.Type.Edges,
                                 validates the mixin contributed the columns,
                                 records every relation in e.state)
       │
       ▼ phase 2
next.Generate(g)                (ent's own templates execute on the now-
                                 non-polymorphic graph — emits all normal
                                 ent files: client.go, comment.go,
                                 comment_create.go, predicates, etc.)
       │
       ▼ phase 3
generate(g)                     (renders polymorphic.go from e.state)
       │
       ▼
ent/polymorphic.go              (Morphable + MorphID/MorphKey + Set<Morph>
                                 builders + runtime morph map)
```

Each box maps to an actual file:

- `extension.go` — defines `Extension`, composes the three-phase hook chain.
- `edge.go` — builder types (`MorphTo`, `MorphMany`, `MorphOne`, `MorphedByMany`) and the marker annotation that flags entpoly edges in the graph.
- `mixin.go` — the `MorphMixin` constructor and its options.
- `preprocess.go` — graph mutation: strip poly edges, validate column presence, record state.
- `state.go` — `polyState` shared between preprocess and generate.
- `generate.go` — render the sidecar from state.
- `template.go` — loads the embedded template.
- `templates/polymorphic.tmpl` — the actual codegen template.
- `morphmap.go` — internal helpers for the morph map.
- `helper/helper.go` — runtime set-diff helpers (`Toggle`, `Sync`, `SyncWithoutDetach`).

## Why edges and not annotations

ent has two natural extension points for schema-level metadata: annotations and edges. `entpoly` could have used either, and earlier prototypes did use annotations. The current design uses edges because:

1. **`Edges()` is where users expect relationship declarations.** Anything else is an inconsistency that requires explanation.
2. **The `X.Type` method-value idiom carries typed references.** `entpoly.MorphTo("commentable", Post.Type, Video.Type)` is checked by the compiler — typos in parent type names fail at build time, not at codegen time.
3. **Edges compose with ent's existing surface.** Users already know `edge.To(...)` returns something they can chain `.Unique()` / `.Annotations()` onto. Mirror that pattern and the learning curve disappears.

The trade-off: polymorphic edges have *no concrete target* (a `MorphTo` references multiple types), but ent's graph builder requires every edge's `Type` field to resolve to a known schema. We work around this by using the first allowed parent as a placeholder `Type` and stripping the edge entirely in the preprocess phase before ent's templates run. The placeholder never escapes into generated code.

## Why mixin and not auto-field-injection

Earlier prototypes injected the discriminator columns directly into `gen.Type.Fields` during the preprocess phase. That works in theory — `gen.Type.Fields` is an exported slice — but `gen.Field` itself has unexported state, and constructing a fully-functional `gen.Field` by hand requires reaching into ent internals that are not stable across versions.

The mixin pattern is ent's officially-supported way to add fields to a schema. Mixins run during schema *load*, before `gen.Graph` is built, so by the time our preprocess hook sees the graph the discriminator columns are already present as ordinary fields — indistinguishable from columns the user wrote by hand. Every downstream ent code path (`SetCommentableID`, `comment.CommentableIDEQ`, mutation tracking, hooks) works without any special handling on our side.

The cost is one extra line on the child schema:

```go
func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{entpoly.MorphMixin("commentable")}
}
```

`preprocess` validates the mixin was added: if the discriminator columns are missing when the polymorphic edge is encountered, codegen fails with a clear error pointing back at the missing `MorphMixin(...)` call.

## File-by-file tour

### `doc.go`

Package-level documentation only. Read this first when learning the API surface.

### `edge.go`

Four builder types — `MorphToBuilder`, `MorphManyBuilder`, `MorphOneBuilder`, `MorphedByManyBuilder` — each implementing `ent.Edge` by exposing a `Descriptor() *edge.Descriptor` method. Every builder carries a `markerAnnotation` that records the polymorphic intent + every user-supplied override.

The marker annotation is JSON-encoded by ent's annotation pipeline; `decodeMarker` and `decodeMarkerAny` round-trip it back to the typed struct on the codegen side.

The `schemaName(t any) string` helper extracts the schema name from a method value via reflection, mirroring how `edge.To` itself identifies edge targets. `Post.Type` is just a method value `func(Post)`; `reflect.TypeOf(t).In(0).Name()` returns `"Post"`.

### `mixin.go`

`MorphMixin(relation, opts...)` returns an `ent.Mixin` whose `Fields()` produces the two discriminator columns. Options:

- `MixinIDColumn` / `MixinTypeColumn` — override the default `<relation>_id` / `<relation>_type` names.
- `MixinIDType("int")` / `MixinIDType("string")` — pick the Go type of the id column.
- `MixinAllowed(Post.Type, Video.Type)` — promote the type column from `field.String` to `field.Enum(...)`. When set, the database (CHECK constraint or native ENUM) enforces the closed set, ent generates a typed `<TypeColumn>` Go type and a runtime validator, and entgql consumers get a typed enum field. Use this to layer DB-level safety on top of the sealed-interface setter — see [ADR-001](./adr-001-type-safety.md) for the full design.

Keeping the mixin and edge in agreement is the user's responsibility; `preprocess` validates by checking that the columns the edge expects exist on the type, and emits a precise error pointing at the right `MixinIDColumn` / `MixinTypeColumn` / `MixinAllowed` call on mismatch.

### `morphmap.go`

`MorphMap` is `map[string]string` from morph key to ent schema name. Two methods exist for codegen-time use:

- `keyForType(schemaName)` — reverse lookup with snake_case fallback.
- `resolveTarget(g, name)` — finds the `*gen.Type` for a schema name (linear scan; the graph is small).

`snake()` is a deliberately conservative converter — it inserts an underscore before each uppercase letter and lowercases the result. Acronyms like `HTTP` become `h_t_t_p` rather than `http`. Users wanting different aliases register them explicitly in the morph map.

### `extension.go`

The `Extension` struct implements `entc.Extension`. The hook is a single three-phase composition: `preprocess → next.Generate → generate`. `next.Generate` is ent's own codegen; sandwiching it between our two phases lets entpoly mutate the graph *before* ent reads it and emit the sidecar *after* ent has finished.

`WithMorphMap(...)` is the only user-facing extension option in v1.

### `preprocess.go`

The heart of the transformation. For every `*gen.Type`:

1. Scan `t.Edges` for edges carrying the marker annotation.
2. Decode the marker; dispatch on `Kind`.
3. For `morphTo` edges: validate the mixin's columns are present; record a `childInfo`; auto-register the allowed parents in the morph map.
4. For `morphMany` / `morphOne` edges: record a `parentInfo`; mark the host as a parent participant.
5. For `morphedByMany` edges: validate `.Through(...)` was called; record a `holderInfo`; mark the target as a parent participant.
6. Strip every poly edge from `t.Edges` so ent's templates do not see them.

Auto-registration of parent participants in the morph map ensures that types referenced only as parents (never declaring a polymorphic relation themselves) still get a `MorphKey()` method in the generated code.

### `state.go`

`polyState` is the only shared state between preprocess and generate. It is rebuilt fresh in every preprocess call so tests can re-run codegen in a single process without leaking state between runs.

`childInfo`, `parentInfo`, and `holderInfo` precompute the strings the template iterates over — case-converted, resolved-from-overrides — so the template stays free of string manipulation.

`childInfo.ResolveTargets` is the additional table needed by the typed forward resolver. Each entry carries a parent schema name + that parent's Go ID type (`int`, `int64`, `string`, etc.) — populated by `preprocess.handleMorphTo` from the loaded gen.Graph. The template uses this to emit the per-case conversion (`strconv.Atoi` for int, `strconv.ParseInt` for int64, pass-through for strings) before calling the right ent client's `Get`.

### `generate.go`

Phase 3 of the pipeline. Transforms `polyState` into the template-ready `tmplData` shape, executes the embedded template, runs `go/format.Source` over the result, and writes to `<target>/polymorphic.go`. If `go/format` fails (almost always a template bug), the unformatted bytes are still written so the developer can inspect what went wrong.

The transformation includes one subtle detail: `gen.Config.Package` is the full import path (e.g. `"github.com/org/proj/ent"`), but the `package` declaration in the emitted file needs only the last segment (`"ent"`). `path.Base` handles that.

### `template.go` + `templates/polymorphic.tmpl`

The template uses Go's `text/template` package (not `html/template` — we are emitting Go source, not HTML). The output structure:

1. `Morphable` interface.
2. `morphTypeMap` literal + `MorphTypeFor` / `MorphTypeName` lookups.
3. Per-parent `MorphID()` + `MorphKey()` methods.
4. Per-child `Set<Morph>` / `Clear<Morph>` builder methods on `Create`, `Update`, `UpdateOne`, and `Mutation`.

The template now emits **typed back-reference methods**:

- `MorphMany` → `post.QueryComments() *CommentQuery` — composable ent query builder.
- `MorphOne` → `post.QueryFeaturedImage(ctx) (*Image, error)` — `(nil, nil)` when unset, typed error on driver/context failure.
- `MorphTo` (forward, child → parent) → `comment.QueryCommentable(ctx) (CommentCommentableParent, error)` — sealed-interface return; the caller type-switches over the AllowedTypes only.

The forward resolver dispatches over `*c.<TypeField>` (an ent-generated enum value), converts the stringified morph id back to the parent's real PK type (`strconv.Atoi`, `strconv.ParseInt`, or pass-through for string IDs), and calls the right `New<Parent>Client(c.config).Get(ctx, id)`.

The only typed back-ref still pending is the M2M holder side (`tag.QueryPosts(ctx) []*Post`); see the v2 roadmap below.

### `helper/helper.go`

Pure runtime helpers — no codegen, no graph, no client dependency. Three functions, all generic over a `comparable` id type:

| Function | Semantics |
|---|---|
| `Toggle(attached, target)` | Flip the attached state of every id in `target`. |
| `Sync(attached, target)` | Replace `attached` with `target` (returns the diff). |
| `SyncWithoutDetach(attached, target)` | Idempotently add every id in `target` not already attached. |

The helpers do not touch the database. They compute set diffs; applying them is the caller's job. This keeps the helper package free of any client-shape dependency.

## Conventions and invariants

These are the rules the codegen relies on. Break them and the generated code will not compile.

1. **Mixin + edge agree on column names, id type, and allowed parents.** If you override on one, override on the other to match. `MixinAllowed`'s parent list must equal the list passed to `MorphTo`.
2. **`MorphTo` requires at least one allowed parent type.** Builder-time validation catches the typical empty case; preprocess re-validates for safety.
3. **`MorphedByMany` requires `.Through(...)`.** preprocess errors with a remediation hint.
4. **Schema names are stable identifiers.** The morph map's right-hand side must match the ent type name exactly (case-sensitive).
5. **Deterministic codegen output.** Slices are sorted before iteration. Maps are emitted via sorted-key iteration. Two `go generate` runs against the same schema must produce byte-identical output.
6. **One MorphMixin per relation, one MorphTo edge per relation.** Multiple polymorphic relations on the same schema are supported — declare one of each per relation.
7. **Parent ID types are inspected at codegen time.** The forward resolver picks the right `strconv` flavour based on each allowed parent's `gen.Type.ID.Type.String()`. If you add a new ID shape (e.g. UUID via `field.UUID`), check that the generated polymorphic.go uses the right conversion; only `int`, `int64`, and string-ish IDs are exercised by the test suite today.

## Extension points (for contributors)

- **New edge type** — add a builder struct + constructor in `edge.go`. Wire a new `Kind` into the dispatch in `preprocess.go` and a new handler that records a slice in `polyState`.
- **New template emission** — add a section to `templates/polymorphic.tmpl`. Add fields to `tmplData` in `generate.go` first; the template can only see what `buildTmplData` populates.
- **New runtime helper** — add to `helper/helper.go` with full tests. Helpers must stay reflection-free, allocation-light, and database-agnostic.
- **New `Extension` option** — add an `Option` constructor (returns `func(*Extension)`). Make it idempotent — registering the same option twice must produce the same final state.

## What v1 ships (vs. earlier drafts of this document)

The v1 codegen has expanded since the first design sketch. Everything listed below is now generated end-to-end and verified by the runtime test suite (`examples/basic/runtime_test.go`).

| Surface | Status |
|---|---|
| Discriminator columns (`<rel>_id`, `<rel>_type`) | ✅ via `MorphMixin` |
| Optional DB-level enum (CHECK / native ENUM) for the type column | ✅ via `MorphMixin(name, MixinAllowed(...))` |
| ent enum runtime validator + typed `<TypeColumn>` Go type | ✅ ent generates from `field.Enum` |
| Per-parent typed `MorphKey` constants (`PostMorphKey`, ...) | ✅ |
| Named `MorphKey` type — rejects raw string literals | ✅ |
| Sealed parent interface per relation (`CommentCommentableParent`) | ✅ — `Set<Morph>(p sealed)` |
| Per-parent `MorphID() string` + `MorphKey() MorphKey` | ✅ |
| `Set<Morph>` / `Clear<Morph>` on Create / Update / UpdateOne / Mutation builders | ✅ |
| Typed predicate constructors: `<Child><Rel>Is(parent)`, `<Child><Rel>IsType(MorphKey)` | ✅ |
| **Typed forward resolver** `comment.QueryCommentable(ctx) → sealed iface, error` | ✅ — switch over allowed parents only, no `any` |
| **Typed reverse back-refs** `post.QueryComments() *CommentQuery` (MorphMany) | ✅ |
| **Typed reverse back-ref** `post.QueryFeaturedImage(ctx) (*Image, error)` (MorphOne) | ✅ — `(nil, nil)` for unset |
| ID conversion in resolver — `int`, `int64`, `string` (UUIDs) | ✅ per-parent at codegen time |
| Set-diff helpers `Toggle` / `Sync` / `SyncWithoutDetach` for M2M | ✅ in `helper/` |

## v2 roadmap

What is still ahead:

| Deferred | Reason |
|---|---|
| Typed M2M holder back-refs (`tag.QueryPosts(ctx) []*Post`) | Currently the user reads the pivot manually then queries the target. Codegen for both ends of the M2M is mechanical — not blocked, just not landed. |
| Eager-load batching (`client.Comment.Query().WithCommentable(ctx)`) | Builds on the existing typed resolver: emit a method that groups child rows by `<rel>_type`, then fires one batch query per parent type. |
| Auto-touch on parent update (Laravel `$touches`) | Runtime hook registration via the extension — bump `parent.updated_at` whenever a child saves. |
| Composite index emission on `(<rel>_type, <rel>_id)` | Atlas annotation injection at codegen time so the read path scales without a manual `Indexes()` declaration. |
| GraphQL union resolver helper for entgql consumers | Optional emit — wires the `<rel>_type` discriminator to a GraphQL union type. |
| Validation hook for `AllowedTypes` mismatch between mixin and edge | Today preprocess catches the column-name mismatch via "missing column" error; an explicit cross-check between `MixinAllowed` and `MorphTo`'s parent list would surface drift earlier with a clearer message. |

None of these are blocked by upstream ent — they are implementation work inside `entpoly`. The v1 line was drawn at "feature-complete typed read + write surface across all four shapes, DB-level enforcement opt-in, full Laravel parity on the canonical operations". v2 will fill in batching, M2M-side typing, and the platform-level integrations.
