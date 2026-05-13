# entpoly

Laravel-style polymorphic relationships for [ent](https://entgo.io) — declared as schema-level edges, with a sealed Go interface, DB-enforced enum, Laravel-parity reads / writes, and an optional GraphQL union surface. Drop `MorphMixin` into a child schema and `MorphTo` into its edges; the codegen extension does the rest.

## Quickstart

```go
// ent/schema/post.go, video.go — regular ent schemas with .Type fields.
// ent/schema/comment.go — the child carrying the discriminator:

func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable", entpoly.MixinAllowed(Post.Type, Video.Type)),
    }
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type).
            Required().     // hook rejects unset / cleared writes
            Touch().        // bumps parent.updated_at on save
            Cascade().      // deletes children when parent dies
            SoftDelete().   // reverse resolves skip soft-deleted parents
            GQL(),          // emit GraphQL union surface
    }
}
```

```bash
go get github.com/khanakia/entx/entpoly
go generate ./ent
```

```go
client := ent.NewClient(...)
ent.RegisterPolyHooks(client)   // wires Required + Touch + Cascade hooks
```

## Install

```bash
go get github.com/khanakia/entx/entpoly
```

Register in `ent/entc.go`:

```go
opts := []entc.Option{
    entc.Extensions(entpoly.NewExtension()),
}
entc.Generate("./schema", config, opts...)
```

`WithMorphMap(...)` is optional — every parent gets a snake_case morph key by default. Pass an explicit map to lock aliases across renames. See [docs/morph-map.md](./docs/morph-map.md).

Run `go generate ./ent`. A sidecar `ent/polymorphic.go` is emitted alongside ent's normal output containing the `Morphable` interface, per-parent constants, sealed parent interfaces, typed setters, typed predicates, typed resolver, typed back-refs, eager-load helpers, and the runtime hooks.

## How do I…

| Task | Doc |
|---|---|
| Add a polymorphic relation (Comment → Post / Video) | [getting-started](./docs/getting-started.md) |
| Reject saves when the parent is unset | [features/required.md](./docs/features/required.md) |
| Bump the parent's `updated_at` on child save | [features/touch.md](./docs/features/touch.md) |
| Delete children when the parent is deleted | [features/cascade.md](./docs/features/cascade.md) |
| Hide soft-deleted parents from reverse resolves | [features/soft-delete.md](./docs/features/soft-delete.md) |
| Expose the relation as a GraphQL union | [features/graphql.md](./docs/features/graphql.md) |
| Use UUID-PK parents | [features/uuid-parents.md](./docs/features/uuid-parents.md) |
| Build a polymorphic many-to-many (tags) | [features/m2m-polymorphic.md](./docs/features/m2m-polymorphic.md) |
| Eager-load the parent without N+1 | [features/eager-loading.md](./docs/features/eager-loading.md) |
| Rename the `<rel>_id` / `<rel>_type` columns | [features/custom-columns.md](./docs/features/custom-columns.md) |
| List the host type in its own `AllowedTypes` | [features/self-referential.md](./docs/features/self-referential.md) |
| Filter children by typed parent predicates | [features/predicates.md](./docs/features/predicates.md) |
| Model exactly-one back-reference (MorphOne) | [features/morph-one.md](./docs/features/morph-one.md) |
| Lock morph-key aliases across renames | [morph-map.md](./docs/morph-map.md) |
| Translate a Laravel verb to entpoly | [laravel-parity.md](./docs/laravel-parity.md) |

## Core concepts

**Schema-level edges.** Polymorphic relations are declared in `Edges()` the same way as regular ent edges. No annotations, no field-spread API. The mixin contributes the discriminator columns; the edge builder carries the options.

**Sealed parent interface.** The forward resolver returns a generated Go interface (`CommentCommentableParent`) sealed to the types listed in `AllowedTypes`. Type-switches on that interface reject parents not in the set at compile time.

**Three-layer type safety.** Sealed Go interface (compile time) + ent runtime enum validator (typo'd morph key) + DB CHECK / native ENUM (raw SQL bypass). See [ADR-001](./docs/internals/adr-001-type-safety.md).

**No foreign keys.** Polymorphic columns can't carry SQL FKs — the type column references multiple tables. `Cascade()` + DB-enforced enum compensate. See [internals/architecture.md](./docs/internals/architecture.md).

## Documentation map

### Using entpoly

| Doc | Use when |
|---|---|
| [Getting started](./docs/getting-started.md) | First-time setup |
| [Per-feature how-tos](./docs/features/) | Recipe per feature (`.Required()`, `.Touch()`, `.GQL()`, …) |
| [Relationships reference](./docs/relationships.md) | Choosing a shape |
| [Mutations reference](./docs/mutations.md) | Translating Laravel verbs |
| [Laravel parity](./docs/laravel-parity.md) | Full Laravel → entpoly map |
| [Morph map](./docs/morph-map.md) | Stable aliases for type-column values |
| [FAQ](./docs/faq.md) | Common questions |
| [Feature matrix](./docs/feature-matrix.md) | Dense reference of every surface |

### Examples + tests

| Path | What it is |
|---|---|
| [examples/basic/](./examples/basic/) | Int-PK runnable example |
| [examples/uuid/](./examples/uuid/) | UUID-PK runnable example |
| [../testentpoly/](../testentpoly/) | Integration harness — every feature, real GraphQL HTTP |

### Internals / contributing

| Doc | What it covers |
|---|---|
| [Architecture](./docs/internals/architecture.md) | Codegen pipeline, edge cases, v2 roadmap |
| [ADR-001: type safety](./docs/internals/adr-001-type-safety.md) | Sealed iface + enum design |
| [ADR-002: whereMorphRelation](./docs/internals/adr-002-where-morph-relation.md) | Per-parent sub-query helper design |

## Status

| State | Items |
|---|---|
| Shipped | 13 of 13 roadmap items — see [feature-matrix.md](./docs/feature-matrix.md) |
| Backlog | 2 follow-up gaps surfaced by testentpoly — see [internals/architecture.md § v2 roadmap](./docs/internals/architecture.md#v2-roadmap) |
| Tests | 28 PASS / 2 SKIP / 0 FAIL across 5 phases in [testentpoly/](../testentpoly/) |

## License

MIT
