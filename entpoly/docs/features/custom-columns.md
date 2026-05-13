# Custom discriminator column names

Default discriminator columns are `<relation>_id` and `<relation>_type`. When the relation is named `entity`, the columns become `entity_id` and `entity_type`. Override both via matching settings on the mixin AND the edge when an existing legacy schema uses different names, when two relations on one schema would otherwise collide, or when you simply prefer a different naming convention.

## When to use

- Migrating from a legacy schema where the columns are already named (e.g. `entity_pk` / `entity_kind`)
- Two morph relations on one schema would default-collide and one of them needs renaming
- The default name conflicts with a column ent emits for another edge

## Setup

```go
// ent/schema/event.go
type Event struct{ ent.Schema }

func (Event) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("entity",
            entpoly.MixinAllowed(Post.Type, Video.Type),
            entpoly.MixinIDColumn("entity_pk"),
            entpoly.MixinTypeColumn("entity_kind"),
        ),
    }
}

func (Event) Fields() []ent.Field {
    return []ent.Field{field.String("name")}
}

func (Event) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("entity", Post.Type, Video.Type).
            IDColumn("entity_pk").
            TypeColumn("entity_kind"),
    }
}
```

The overrides must agree on both sides — preprocess validates this at codegen time and surfaces a precise error including the right `MixinIDColumn(...)` / `MixinTypeColumn(...)` call to add. See [`testentpoly/schema/event.go`](../../../testentpoly/schema/event.go).

## Wiring

None — column renaming is pure schema metadata; no runtime changes.

## Usage

The typed setter / resolver method names are still derived from the **morph relation** name (`entity`), not the column name — `SetEntity(p)` / `QueryEntity(ctx)`. The column rename only changes the database schema and the Go struct field names:

```go
post := client.Post.Create().SetTitle("p").SaveX(ctx)
e := client.Event.Create().SetName("login").SetEntity(post).SaveX(ctx)

// Struct fields are PascalCased from the column names:
// e.EntityPk   *string
// e.EntityKind *event.EntityKind

// Reverse resolve still uses the relation name:
parent, _ := e.QueryEntity(ctx)
p, ok := parent.(*ent.Post)
```

## Verification

```go
// from testentpoly/customcols_test.go — TestCustomColumns_RoundTrip
e := client.Event.Create().SetName("login").SetEntity(post).SaveX(ctx)

if e.EntityPk == nil || *e.EntityPk != post.MorphID() {
    t.Errorf("entity_pk = %v, want %q", e.EntityPk, post.MorphID())
}
if e.EntityKind == nil || string(*e.EntityKind) != string(ent.PostMorphKey) {
    t.Errorf("entity_kind = %v, want %q", e.EntityKind, ent.PostMorphKey)
}
```

Confirm the column names landed in SQL:

```sql
SELECT sql FROM sqlite_master WHERE name = 'events';
-- CREATE TABLE `events` (
--   ...
--   `entity_pk` text NULL,
--   `entity_kind` text NULL CHECK (entity_kind IN ('post','video'))
--   ...
-- )
```

## Gotchas

1. **Mismatched overrides surface at codegen time with a specific remediation.** If the edge declares `.IDColumn("entity_pk")` but the mixin omits `MixinIDColumn("entity_pk")` (or vice-versa), preprocess fails with an `entpoly:` error spelling out the missing call. The fix is always to make both sides agree.
2. **`MixinIDType` must match `.IDType` too.** If the mixin says `MixinIDType("int")` and the edge says `.IDType("string")`, the generated `polymorphic.go` mismatches the underlying column type and the Go build fails.
3. **The composite index follows the renamed columns.** `MorphMixin` emits an `index.Fields(typeCol, idCol)` automatically using the renamed names. Disable via `MixinNoIndex()` only when a different access pattern dominates — every back-ref read path uses this index.
4. **Struct field PascalCasing is column-driven, not relation-driven.** `entity_pk` → `EntityPk`, `entity_kind` → `EntityKind`. The typed setter `SetEntity(p)` is relation-driven (`entity` → `Entity`). Renaming the column without renaming the relation produces this split — by design, since the relation name drives the API surface and the column name drives storage.

## See also

- [`testentpoly/schema/event.go`](../../../testentpoly/schema/event.go)
- [`testentpoly/customcols_test.go`](../../../testentpoly/customcols_test.go)
- [Relationships reference § custom column names](../relationships.md)
- [Architecture](../architecture.md)
