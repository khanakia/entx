# UUID-PK parents

entpoly's discriminator id column is a string by default precisely so any parent PK shape — `int`, `int64`, `string`, `uuid.UUID`, ULID — round-trips through the same machinery. For UUID parents, codegen detects the parent's PK type at the field level (`gen.Type.ID.Type.Ident`), emits a `uuid.Parse` branch in the resolver's strconv switch, and adds `github.com/google/uuid` to the imports. Reach for this when your domain uses UUIDs everywhere and you want polymorphic relations without writing manual parse code.

## When to use

- Parents have UUID primary keys (typed `uuid.UUID`, not stringified UUIDs)
- The child may also use a UUID PK — entpoly keys the eager-load map on the child's PK type
- You want zero special-casing in your application code — the same `SetTarget(parent)` / `QueryTarget(ctx)` API as int-PK parents

## Setup

Parent schemas with UUID PKs:

```go
// ent/schema/document.go
type Document struct{ ent.Schema }

func (Document) Fields() []ent.Field {
    return []ent.Field{
        field.UUID("id", uuid.UUID{}).Default(uuid.New),
        field.String("title"),
    }
}
```

```go
// ent/schema/report.go
type Report struct{ ent.Schema }

func (Report) Fields() []ent.Field {
    return []ent.Field{
        field.UUID("id", uuid.UUID{}).Default(uuid.New),
        field.String("name"),
    }
}
```

Child schema — the morph relation is declared the same way as the int-PK case:

```go
// ent/schema/annotation.go
type Annotation struct{ ent.Schema }

func (Annotation) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("target",
            entpoly.MixinAllowed(Document.Type, Report.Type),
        ),
    }
}

func (Annotation) Fields() []ent.Field {
    return []ent.Field{
        field.UUID("id", uuid.UUID{}).Default(uuid.New),
        field.String("body"),
    }
}

func (Annotation) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("target", Document.Type, Report.Type),
    }
}
```

The discriminator id column stays `field.String` — UUIDs are persisted as their canonical string form, which sorts and indexes identically across dialects.

## Wiring

Standard `ent/entc.go` — no extra options:

```go
entc.Extensions(entpoly.NewExtension())
```

`go generate ./ent` produces a `polymorphic.go` that imports `github.com/google/uuid` and emits `uuid.Parse` in the resolver's switch arm for each UUID parent.

## Usage

```go
doc := client.Document.Create().SetTitle("D").SaveX(ctx)
rep := client.Report.Create().SetName("R").SaveX(ctx)

aDoc := client.Annotation.Create().SetBody("on doc").SetTarget(doc).SaveX(ctx)
aRep := client.Annotation.Create().SetBody("on rep").SetTarget(rep).SaveX(ctx)

// String-encoded UUID round-trip via the discriminator column.
// aDoc.TargetID == doc.MorphID() (the canonical UUID string).

// Reverse resolve via QueryTarget — type-switch must recover *Document.
parent, _ := aDoc.QueryTarget(ctx)
switch p := parent.(type) {
case *ent.Document:
    fmt.Println(p.Title)
case *ent.Report:
    fmt.Println(p.Name)
}
```

## Verification

```go
// from testentpoly/uuid_test.go — TestUUID_FullRoundTrip
parent, err := aDoc.QueryTarget(ctx)
d, ok := parent.(*ent.Document)
if !ok {
    t.Fatalf("expected *Document, got %T", parent)
}
if d.ID != doc.ID {
    t.Errorf("resolved Document id = %s, want %s", d.ID, doc.ID)
}
```

A runnable end-to-end example is at [`entpoly/examples/uuid/`](../../examples/uuid/).

## Gotchas

1. **Mixed-PK `AllowedTypes` is a runtime gotcha.** Listing both an int-PK parent and a UUID-PK parent in one `MorphTo(...)` succeeds at codegen, but reverse-resolve hits a parse error at runtime when the discriminator value cannot be coerced to the expected type. A drift linter for this case is on the [v2 roadmap](../architecture.md#v2-roadmap) — for now, keep `AllowedTypes` PK-homogeneous, or ensure every reverse-resolve call site handles the parse error. The exception is **self-referential mixing** done deliberately in [`testentpoly/schema/folder.go`](../../../testentpoly/schema/folder.go), where Folder (int) + Document (uuid) work because each branch is parsed with its own decoder.
2. **The id column is `field.String`, not `field.UUID`.** entpoly stores UUIDs as strings in the discriminator column. This is on purpose — the same column has to host every parent's PK shape. Index it like any other string column.
3. **`MixinIDType("int")` does not work with UUID parents.** That option promotes the discriminator column to `field.Int64`, which cannot hold UUID strings. Keep `MixinIDType` at the default (`"string"`) for UUID parents.

## See also

- [`entpoly/examples/uuid/`](../../examples/uuid/) — full runnable example
- [`testentpoly/uuid_test.go`](../../../testentpoly/uuid_test.go)
- [Architecture § v2 roadmap](../architecture.md#v2-roadmap) — mixed-PK linter
- [Relationships reference § choosing the id type](../relationships.md#choosing-the-id-type)
