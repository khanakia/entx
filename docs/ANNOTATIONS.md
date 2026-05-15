# Annotations reference

> **Status:** declared in [`enttui/annotation.go`](../annotation.go), not yet wired into the codegen. For M0.5 the generator runs purely on the rules in [CONVENTIONS.md](CONVENTIONS.md). This file is the **API target** — what annotations will exist when M1 ships.

Annotations are how you override the conventional defaults at the schema level. They live next to the schema, version with the schema, and are type-checked at codegen time.

## Where they go

Three placement sites:

1. **Schema-level** — from `func (X) Annotations() []schema.Annotation`.
2. **Field-level** — `.Annotations(...)` on a `field.X(...)` builder.
3. **Edge-level** — `.Annotations(...)` on an `edge.X(...)` builder.

Each annotation is a small Go struct embedding `schema.Annotation`. ent persists them into the `codegen.Graph`, where the codegen reads them.

## Schema-level annotations

### `Browse{}`

Force-include an entity the internal-table convention would otherwise
skip. The generator skips obvious infra tables by name
(`SchemaMigration`, `AuditLog`, `QueryLog`, `PiiPattern`, `*_fts`,
`*shadow*`) — that's a **default, not a hardcode**: `enttui.Browse{}`
on the schema overrides it so you *can* browse e.g. `audit_log`.

```go
func (AuditLog) Annotations() []schema.Annotation {
    return []schema.Annotation{enttui.Browse{}}
}
```

Exclusion the other way is config-driven: `enttui.Skip("X")` /
`--skip X`. Non-internal entities are browsable with no annotation.

### `Display(string)`

Pretty name shown in the kind picker and pane titles.

```go
enttui.Display("Tasks")
```

Default: convention-pluralized type name (`Post` → "Posts", `Memory` → "Memories").

### `Group(string)`

Logical group in the picker — entries can be visually grouped, e.g. `workflow`, `knowledge`, `ops`.

```go
enttui.Group("workflow")
```

Default: `"data"`.

### `Icon(string)`

Single rune (or short emoji) displayed next to the name in the picker.

```go
enttui.Icon("✓")
```

### `DefaultSort(field, direction)`

Initial sort when the user opens this kind.

```go
enttui.DefaultSort("created_at", enttui.Desc)
```

Default: `created_at, desc` if `created_at` exists, otherwise no sort.

### `PageSize(int)`

Override the default 200-row page size.

```go
enttui.PageSize(50)
```

### Scope predicates

Scope is **config-driven**, not annotation-driven. Pass the snake_case field names you want wired as predicates via `enttui.Config.ScopeFields` (or `--scope` on the CLI):

```go
enttui.Generate("./schema", &enttui.Config{
    Target:      "../tui/gen",
    Package:     "tuigen",
    EntPkg:      "myproject/ent",
    ScopeFields: []string{"project_id", "tenant_id"},
})
```

For each scope key an entity actually has on its schema, the generated Fetch closure reads `opts.Scope[<key>]` and applies a predicate (`pred.ProjectID(v)`, `pred.TenantID(v)`, …). Entities without that field skip the predicate. Drive at runtime:

```go
app.SetScope("project_id", projectID)
```

`enttui.ProjectScope("workspace_id")` is reserved for a future "this entity uses `workspace_id` as its `project_id`" remap — not wired yet.

### `RelatedColumns(RelatedColumn…)` — entity-level

Emit one or more table columns whose values are drawn from a foreign-key
target row, not from this entity. Each entry is `{Edge, Field, Label}`:

```go
import (
    "entgo.io/ent/schema"
    "enttui"
)

func (Post) Annotations() []schema.Annotation {
    return []schema.Annotation{
        enttui.RelatedColumns(
            enttui.RelatedColumn{Edge: "author",   Field: "name",  Label: "Author"},
            enttui.RelatedColumn{Edge: "category", Field: "name",  Label: "Category"},
        ),
    }
}
```

`Edge` must match an ent edge name on the host. `Field` is any field name
on the target type. `Label` is optional — defaults to `"<Edge> <Field>"`
in title case.

Codegen behavior:

- Emits a Column with `Key = "<edge>_<field>"` and a typed Get accessor
  `r.Edges.<Edge>.<Field>` (nil-safe; pointer-typed targets also
  nil-deref-guarded).
- Adds `q.With<Edge>()` to the Fetch query so reads are batched — no
  N+1 round-trips. Multiple projections off the same edge share a single
  `With<Edge>()` call.
- **Filterable = true** — the condition builder (`f`) shows the column
  in its picker. Selecting it emits a `pred.Has<Edge>With(targetPred.<Field><Op>(v))`
  sub-select. v1 supports `= / != / contains` for string targets; enum
  / numeric / time follow the same pattern when wired in.
- **Sortable = true** — `,s` on the focused column (or adding it
  via the `,o` sort-stack modal) emits `pred.By<Edge>Field(targetPred.Field<Field>, sql.OrderAsc())`
  using ent's generated edge-order helper. Works alongside native-column
  sort keys in the multi-sort stack.
- Generated file gains an aliased import of the target predicate
  package (e.g. `entAuthor "myproject/ent/author"`) and pulls in
  `entgo.io/ent/dialect/sql` for the sort helper. Both are emitted only
  when at least one related column is present.
- Unknown edge name or unknown field name → entry is silently dropped.

### `DetailEdge{}` — entity-level

Designates drill (1→N) edge(s) for the master-detail split (`m`). Two
forms, concatenated when both set (`Edge` first):

```go
enttui.DetailEdge{Edge:  "tasks"}                        // single
enttui.DetailEdge{Edges: []string{"repos", "memories"}}  // tabbed
```

Codegen emits `DetailEdges []string` on the spec. Runtime: `m` opens a
two-pane page (master table + live child table). Multiple edges → the
detail pane is tabbed (`]`/`[` cycle); each tab is a full child
`tableView`, built lazily — the child kind is learned from the edge's
first `resolveDrill`, so different edges may target different kinds.
Moving the master cursor re-filters the active tab via an in-memory
`idFilter`. Edges that don't exist or aren't drill edges are skipped.

### `AllowBulkCopy{}` — entity-level

Enables row-selection (`space` toggles, `v` visual-range, `ctrl+a`
selects all visible, `esc` clears) and the bulk yank flow. With one or
more rows selected, `yv` opens a format-chooser modal:

- **JSON array** of `{id, col1, col2, ...}` objects
- **CSV** with `id` + every visible column as header
- **focused-column JSON** (table view only) — array of strings, one per
  selected row, drawn from whichever column has cursor focus
- **focused-column CSV** — same, single-column CSV

All copies go to the OS clipboard via `atotto/clipboard`. Status bar
shows `[blue]☐ space select · y copy[-]` plus `[yellow]N selected[-]`
when any row is marked.

```go
func (Post) Annotations() []schema.Annotation {
    return []schema.Annotation{enttui.AllowBulkCopy{}}
}
```

### `AllowExport{}` — entity-level

Enables the **`,x`** shortcut. With rows selected (`space`), exports
exactly those — your pick overrides the filter. With no selection,
re-fetches every row matching the current filter / sort / scope (capped
at 10 000). Then the JSON / CSV chooser, then a destination modal: an
editable path field (default `<cwd>/<kind>-<timestamp>.<ext>`) with
**Save to file** / **Copy to clipboard** / **Cancel**. Truncation is
surfaced in the status message.

```go
func (Post) Annotations() []schema.Annotation {
    return []schema.Annotation{enttui.AllowExport{}}
}
```

Status bar shows `[blue]⇩ X export[-]` when active.

### `AllowCreate{}` — entity-level

Enables the **`,a`** (add / new row) keybinding. The form opens with every
Editable field empty; scope keys from `app.SetScope(...)` are
auto-injected so the new row lands in the right tenant / project / etc.
Generated code emits a `Create: func(ctx, vals) (id, err)` closure that
calls `client.X.Create().SetX(v)…Save(ctx)`.

```go
func (Post) Annotations() []schema.Annotation {
    return []schema.Annotation{enttui.AllowCreate{}}
}
```

Status bar shows `[green]+ ,a new[-]` when active.

### `AllowDelete{}` — entity-level

Enables the **`,d`** (delete with confirm) keybinding for this entity.
Off by default — destructive actions opt-in only. Generated code emits a
`Delete: func(ctx, id) error { return client.X.DeleteOneID(id).Exec(ctx) }`
closure. Status bar shows `[red]✗ ,d delete[-]` when active.

```go
func (Post) Annotations() []schema.Annotation {
    return []schema.Annotation{enttui.AllowDelete{}}
}
```

## Field-level annotations

Attached to a field builder:

```go
field.String("title").
    Annotations(enttui.Sortable(), enttui.Filterable(), enttui.Editable{}),
```

### `AsTitle{}`

Pins this field as the **row label** (list label / table-overlay title /
master-detail child label / ref-picker label). Resolved at codegen into
`spec.LabelKey` — the runtime never name-guesses. Without it, the
convention fallback picks the first of `title → name → display_name →
label → summary`, then the id.

```go
field.String("headline").Annotations(enttui.AsTitle{}),
```

### `AsBody{}`

Pins this field as the **preview body** (the prose block under the
field list; also Hidden from the table by default). Codegen →
`spec.BodyKey`. Fallback when unset: first of `body → description →
content`, else no body block.

```go
field.Text("content").Annotations(enttui.AsBody{}),
```

> Note: there is no hardcoded "id" column either — the runtime uses the
> schema's actual ID field (`spec.IDKey`), whatever its name or type
> (string / int / uuid); the generated accessor stringifies it.
> `AsStatus{}` is declared but not yet wired (status coloring is via
> `enttui.Chip` on the field).

### `Sortable()`

Lets the runtime cycle sort through this field. Multiple `Sortable()` fields cycle in declaration order.

### `Filterable()`

Includes this field in the substring filter predicate (defaults to title + body + name only).

### `Editable{}`

Marks a field as user-editable in the **`e`** (edit form) modal. Opt-in
per field — a schema with no `Editable{}` annotations has no edit UI at
all. Generated code emits a setter for each editable field in the
`Update` closure:

- string / stringPtr → `u.SetX(v)` (stringPtr clears on empty input)
- enum / enumPtr → `u.SetX(targetPkg.Type(v))` (enumPtr clears on empty)

```go
field.String("title").NotEmpty().
    Annotations(enttui.Editable{}),
field.Enum("status").Values("draft", "published").
    Annotations(enttui.Editable{}),
```

When any field on the entity carries `Editable{}`, the status bar shows
`[green]✎ ,e edit[-]`. Pressing `,e` on a schema with none → status hint
points at this annotation.

### `Hidden()`

Never include this field in any UI surface — not the columns list, not the filter, not the preview meta.

### `Chip(map[string]string)`

For enum-typed fields, attach a value→tone map. Tones: `success`, `warn`, `danger`, `info`, `muted`.

```go
field.Enum("status").Values("todo", "doing", "done", "blocked").
    Annotations(enttui.Chip(map[string]string{
        "todo":    "muted",
        "doing":   "warn",
        "done":    "success",
        "blocked": "danger",
    })),
```

### `Format(FormatKind)`

Attach a value formatter for the preview rendering.

```go
field.JSON("payload", map[string]any{}).
    Annotations(enttui.Format(enttui.FormatPrettyJSON)),
field.Time("created_at").
    Annotations(enttui.Format(enttui.FormatRelativeTime)),
```

Available formatters (M1):

| Kind                 | Behavior |
|----------------------|----------|
| `FormatRaw`          | `fmt.Sprintf("%v", value)` (default for non-string) |
| `FormatPrettyJSON`   | `json.MarshalIndent`, with shadcn-ish indentation |
| `FormatRelativeTime` | "3 minutes ago" instead of absolute timestamp |

## Edge-level annotations

Attached to an edge builder:

```go
edge.From("project", Project.Type).Ref("tasks").Unique().
    Annotations(enttui.Upward{Trigger: "p"}),
```

### `Upward{Trigger:"x"}`

Pins the keybinding for this `Unique()` (breadcrumb) edge. **Honored by
codegen** — the generated `EdgeSpec.Trigger` is exactly your letter
(skipped only if already taken by another edge on the same type). If
omitted, a free letter is auto-picked from the edge name.

### `Drill{Trigger:"x"}`

Pins the key for this non-unique (drill) edge. Same precedence: your
letter wins, else auto-pick. `"enter"` has no special meaning — every
edge is a visible single-letter trigger; `enter` opens the preview.

```go
edge.To("comments", Comment.Type).
    Annotations(enttui.Drill{Trigger: "c"}),
```

## Example: fully-annotated schema

```go
package schema

import (
    "entgo.io/ent"
    "entgo.io/ent/schema"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"

    "enttui"
)

type Post struct{ ent.Schema }

func (Post) Annotations() []schema.Annotation {
    return []schema.Annotation{
        enttui.Display("Posts"),
        enttui.Group("content"),
        enttui.Icon("📝"),
        enttui.PageSize(50),
        enttui.RelatedColumns(
            enttui.RelatedColumn{Edge: "author", Field: "name", Label: "Author"},
        ),
    }
}

func (Post) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").Unique().Immutable(),
        field.String("title").NotEmpty().
            Annotations(enttui.AsTitle{}), // explicit row label
        field.Text("body").Optional().Nillable().
            Annotations(enttui.AsBody{}),  // explicit preview body
        field.Enum("status").Values("draft", "published", "archived").
            Annotations(
                enttui.Chip(map[string]string{
                    "draft":     "muted",
                    "published": "success",
                    "archived":  "warn",
                }),
            ),
        field.String("internal_hash").
            Annotations(enttui.Hidden()),
        field.Time("created_at").Default(time.Now),
        field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
    }
}

func (Post) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("author", Author.Type).Ref("posts").Unique(),
        edge.To("comments", Comment.Type).
            Annotations(enttui.Drill("c")),
    }
}
```

## How conventions still apply

When you annotate, conventions fill in any gap. Examples:

- You annotate `Group("workflow")` but skip `Display()` → display falls back to convention pluralizer.
- You annotate `AsStatus()` on a string field but skip `Chip()` → status renders without colors.
- You skip everything → the schema browses with full convention defaults.

There's never a "you broke this by annotating one thing" trap. Annotations are additive overrides.

## Related docs

- [CONVENTIONS.md](CONVENTIONS.md) — the rules applied when no annotations override.
- [CODEGEN.md](CODEGEN.md) — pipeline that consumes both conventions and annotations.
