# FAQ

Questions that come up the first few times someone uses `entpoly`.

## Why does ent not support this natively?

ent's edge model has a single concrete target type per edge — declared at schema time and compiled into the predicate package, the typed query builders, and the eager-load helpers. Polymorphism breaks that one-to-one mapping: a `Comment.commentable` edge points at `Post` *or* `Video`, and ent's codegen has no place to put a target that is a union.

The ent maintainers have considered polymorphism multiple times — see [ent/ent#1048](https://github.com/ent/ent/issues/1048), open since 2020 — but no first-party support is on the roadmap. `entpoly` fills the gap by emitting a *parallel* surface (`Morphable` + `Set<Morph>` builders) that runs alongside the standard ent codegen instead of trying to bolt polymorphism into the edge system.

## Why is there no foreign key on the polymorphic columns?

SQL foreign keys reference a single table. The whole point of a polymorphic column is that it references multiple tables. The two cannot coexist; you have to give up FKs to get polymorphism.

If you need strict referential integrity per parent type, the alternative is one non-polymorphic FK column per parent (and a CHECK constraint that exactly one of them is set). That has its own trade-offs — wide row, sparse columns, hard to extend — but it is genuinely the only option SQL gives you that preserves FK guarantees. Pick your trade.

## Will polymorphic deletes cascade?

No, not automatically. Because there is no FK constraint, the database will not cascade for you. Options:

1. Delete children explicitly before the parent — straightforward in a transaction.
2. Pair `entpoly` with the [`entcascade`](../../entcascade) extension, which generates application-level cascade-delete helpers driven by schema annotations.
3. Add a `BEFORE DELETE` trigger or a dedicated background job to sweep orphans.

`entpoly` ships its own cascade in v1 via the `.Cascade()` builder option:

```go
entpoly.MorphTo("commentable", Post.Type, Video.Type).Cascade()
```

Wire it once at startup with `ent.RegisterPolyHooks(client)`. From then on, deleting any allowed parent (e.g. `client.Post.DeleteOneID(p.ID).Save(ctx)`) also deletes every polymorphic child pointing at it, in the same logical operation. Works on every dialect — the cascade runs in application code via an ent pre-delete hook, no FK constraint required (FKs are impossible for polymorphic columns).

You can still pair with [`entcascade`](../../entcascade) for non-polymorphic edges in the same project; the two extensions coexist without interference.

## Can parents use UUID primary keys?

Yes. ent's `field.UUID("id", uuid.UUID{})` works without any entpoly-specific configuration:

```go
type Document struct{ ent.Schema }
func (Document) Fields() []ent.Field {
    return []ent.Field{
        field.UUID("id", uuid.UUID{}).Default(uuid.New),
        field.String("title"),
    }
}
```

entpoly's preprocess pass detects the custom Go-typed PK via `gen.Type.ID.Type.Ident` and:

- Adds `import "github.com/google/uuid"` to the generated `polymorphic.go`
- Emits a `uuid.Parse(*c.TargetID)` branch in the resolver / eager-load / cascade switches
- Types the eager-load result map as `map[uuid.UUID]...` (when the CHILD also has a UUID PK)
- Same Set/Clear ergonomics — `client.Annotation.Create().SetTarget(doc).Save(ctx)` stringifies via `MorphID()`

See `examples/uuid/` for a runnable end-to-end example (Document + Report parents, Annotation child, all UUID-keyed).

## Can I store ints in the morph id column?

Yes — pass matching options to the mixin and the edge:

```go
func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable", entpoly.MixinIDType("int")),
    }
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type).IDType("int"),
    }
}
```

Use this only when *every* parent type uses an int PK. Mixed PK shapes (some int, some UUID) require the default string column.

The string default is conservative on purpose: it works everywhere, and the storage overhead is minor for typical row counts. Optimise only when measurement says it matters.

## What happens if I rename an ent type?

Depends on what the morph map said. Two scenarios:

1. **Type was explicitly registered.** Update the right-hand side of `WithMorphMap` to the new name. The morph key (left-hand side) does not change, so persisted data is unaffected.
2. **Type relied on the snake_case default.** Renaming the schema changes the default morph key. Persisted rows with the old key become unreachable from the typed read path. Fix this *before* you ship the rename by registering an explicit morph map entry tied to the old key.

See [morph-map.md](./morph-map.md) for the full renaming workflow.

## Can the same schema declare more than one polymorphic relation?

Yes. Add one `MorphMixin(...)` and one `MorphTo(...)` edge per relation:

```go
type Audit struct{ ent.Schema }

func (Audit) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("actor"),
        entpoly.MorphMixin("target"),
    }
}

func (Audit) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("actor",  User.Type, ServiceAccount.Type),
        entpoly.MorphTo("target", Post.Type, Video.Type, Comment.Type),
    }
}
```

The generated `polymorphic.go` emits independent `SetActor` / `ClearActor` and `SetTarget` / `ClearTarget` builders. Each relation is fully independent — different parent type sets, different overrides on column names, even different id types are fine.

## How does this compare to a discriminated-union table per parent type?

A discriminated union — one boring FK column per possible parent type, with a CHECK constraint that exactly one is non-null — is the SQL-purist alternative to polymorphism. Trade-offs:

| Aspect | `entpoly` polymorphism | Discriminated union |
|---|---|---|
| FK integrity | None (manual cascade) | Full per-column |
| Adding a new parent type | Just update morph map | Add a column to the child table |
| Query performance | Index on `(type, id)` | Multiple sparse indexes, one per FK |
| Schema readability | One pair of columns | One pair per parent type |
| Generic queries ("all comments today") | Trivial | Trivial |
| Constraints across types ("comments on either Post or User only") | Application-level | Built into the CHECK constraint |

Polymorphism wins on flexibility and schema brevity. Discriminated unions win on database-enforced integrity. Pick based on which side of that trade matters more for your domain.

## Does `entpoly` work with GraphQL / entgql?

The generated `Set<Morph>` builders and `Morphable` interface are vanilla Go — they work everywhere ent works, including under entgql. There is no special GraphQL helper today: if you want a GraphQL union type on the read side (`union Commentable = Post | Video`), you wire it in your entgql resolvers using the `commentable_type` and `commentable_id` fields.

A future minor release may emit a GraphQL union schema fragment automatically; it has not landed because the entgql integration surface is still settling upstream.

## Does it work with all SQL dialects?

Yes. `entpoly` generates only standard SQL types via ent's `field.String` / `field.Int64` constructors — no dialect-specific column types, no proprietary index syntax. The composite index recommendation (`<name>_type, <name>_id`) works on every dialect ent supports.

NoSQL backends are out of scope — ent itself does not target them.

## How big is the generated code?

The sidecar `polymorphic.go` adds:

- One `Morphable` interface (3 lines).
- One `morphTypeMap` + two lookup helpers (~20 lines).
- Two methods per parent type (`MorphID` + `MorphKey`) — 4 lines per type.
- Six methods per child relation (`Set` / `Clear` on Create/Update/UpdateOne plus the same on Mutation) — about 40 lines per relation.

For a project with five parents and three polymorphic children, the file weighs in around 250 lines. That is the entire footprint; nothing else is added.

## Can I read `polymorphic.go` instead of this docs?

Yes — please do. The generated file is short, readable, and the authoritative source of truth for what each method actually does. The docs in this directory cover *intent*; the generated code covers *behaviour*. Read both.
