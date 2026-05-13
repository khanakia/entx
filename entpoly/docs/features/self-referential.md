# Self-referential polymorphic relations

Listing a host type in its own `AllowedTypes` makes the relation self-referential — a `Folder` whose parent may be another `Folder`, a `Reaction` whose target may be another `Reaction`. entpoly needs no special handling: the morph map registers the host type alongside every other parent, and the sealed-interface setter accepts it like any other allowed parent. The compile-time guarantees (sealed interface, `MorphKey` constant, typed predicates) work transparently — `SetParent(folder)` compiles iff `Folder.Type` is in `AllowedTypes`.

## When to use

- Hierarchical data — folders, categories, comments-on-comments
- A polymorphic relation that genuinely accepts the host type as one of multiple options (Folder → Folder or Document)
- The "parent" relationship is naturally polymorphic, not a forced choice between two edges

## Setup

```go
// testentpoly/schema/folder.go
type Folder struct{ ent.Schema }

func (Folder) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("parent",
            entpoly.MixinAllowed(Folder.Type, Document.Type),
        ),
    }
}

func (Folder) Fields() []ent.Field {
    return []ent.Field{field.String("name")}
}

func (Folder) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("parent", Folder.Type, Document.Type),
    }
}
```

The schema lists its own `.Type` method value alongside any other allowed parents. `Document.Type` is a UUID-PK parent in the testentpoly schema; the self-referential `Folder.Type` is an int-PK parent — the **mixed-PK** combination works here because each branch uses its own decoder, but see the gotcha below.

## Wiring

Standard extension registration; no self-referential-specific options.

## Usage

```go
root := client.Folder.Create().SetName("root").SaveX(ctx)
child := client.Folder.Create().SetName("child").SetParent(root).SaveX(ctx)

// Reverse resolve — type-switch must recover *Folder.
parent, _ := child.QueryParent(ctx)
got, ok := parent.(*ent.Folder)
if !ok { /* compile-time impossible if AllowedTypes is Folder+Document */ }

// Point at a Document instead.
doc := client.Document.Create().SetTitle("D").SaveX(ctx)
mixed := client.Folder.Create().SetName("mixed").SetParent(doc).SaveX(ctx)
```

## Verification

```go
// from testentpoly/selfref_test.go — TestSelfReferential_Folder
root := client.Folder.Create().SetName("root").SaveX(ctx)
child := client.Folder.Create().SetName("child").SetParent(root).SaveX(ctx)

parent, _ := child.QueryParent(ctx)
got, ok := parent.(*ent.Folder)
if !ok || got.ID != root.ID {
    t.Errorf("self-ref resolve failed: %T %v", parent, parent)
}
```

## Gotchas

1. **Cycles are not detected by entpoly.** Creating `A.parent = B` and `B.parent = A` succeeds at every layer — entpoly only enforces type membership, not graph shape. If your domain needs cycle-free hierarchies (folders, categories), add a validation hook on the child's create / update path.
2. **Cascade with a self-referential parent recurses one level only.** `.Cascade()` deletes the children pointing at the parent that was just deleted; but if those children themselves have grandchildren via the same morph relation, those grandchildren are NOT deleted (the cascade hook fires on the original parent's delete, not on each cascaded child's delete). For true recursive deletion, write an application-level helper.
3. **Mixed-PK `AllowedTypes` only works because each branch decodes its own column.** Folder is int-PK; Document is UUID-PK. The reverse resolver picks the decoder by morph key, so each branch is parsed independently. This is the same path the [v2 mixed-PK linter](../internals/architecture.md#v2-roadmap) is tracking — works today, but adding a third parent with yet another PK shape stretches the pattern.

## See also

- [`testentpoly/schema/folder.go`](../../../testentpoly/schema/folder.go)
- [`testentpoly/selfref_test.go`](../../../testentpoly/selfref_test.go)
- [Relationships reference § shape 4](../relationships.md)
- [UUID parents](./uuid-parents.md) — mixed-PK story
