# entpoly internals

Implementation notes and design rationale. **Not required reading** to use entpoly — see the [user docs](../) for that.

Use these when:
- You hit edge-case behaviour and want to know why entpoly behaves a certain way.
- You're contributing to entpoly itself.
- You're auditing the type-safety story before adopting.

| Doc | What it covers |
|---|---|
| [architecture.md](./architecture.md) | The codegen extension pipeline: preprocess → ent.Generate → generate. Edge-cases table (20 documented cases). v2 roadmap. |
| [adr-001-type-safety.md](./adr-001-type-safety.md) | Why sealed Go interface + DB enum (Approach C). Alternatives considered and rejected. |
| [adr-002-where-morph-relation.md](./adr-002-where-morph-relation.md) | Why per-parent predicate constructors (`OnPost`/`OnVideo`) over closures or builder objects for `whereMorphRelation`. |
