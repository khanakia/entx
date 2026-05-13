# Per-feature how-to guides

Step-by-step guides for each entpoly feature. Each guide follows the same shape — when to use, setup, wiring, usage, verification, gotchas, see-also — so you can read one without reading the others.

| Guide | What it covers |
|---|---|
| [GraphQL](./graphql.md) | `.GQL()` end-to-end — schema → entc.go → gqlgen.yml → resolver → curl |
| [Required](./required.md) | `.Required()` hook — reject unset / cleared writes |
| [Touch](./touch.md) | `.Touch()` hook — bump parent timestamp on child save |
| [Cascade](./cascade.md) | `.Cascade()` hook — delete children with the parent |
| [Soft delete](./soft-delete.md) | `.SoftDelete()` — hide soft-deleted parents from reverse resolves |
| [UUID parents](./uuid-parents.md) | UUID PK setup — codegen detects per-parent shape |
| [M2M polymorphic](./m2m-polymorphic.md) | `MorphedByMany` + pivot + auto-inverse + `helper.Sync`/`Toggle` |
| [Eager loading](./eager-loading.md) | `WithCommentable()` 1+N(types) batching |
| [Custom columns](./custom-columns.md) | `MixinIDColumn` / `MixinTypeColumn` + matching edge overrides |
| [Self-referential](./self-referential.md) | Host type listed in its own `AllowedTypes` |
| [Predicates](./predicates.md) | Typed predicate constructors + per-parent sub-query helpers |
| [MorphOne](./morph-one.md) | Exactly-one parent-side back-reference |
| [Morph map](../morph-map.md) | Stable aliases via `entpoly.WithMorphMap(...)` |

For the broader docs, see the top-level [documentation index](../../README.md#documentation-index).
