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

### `Browse()`

Opt this schema into the TUI.

```go
func (Post) Annotations() []schema.Annotation {
    return []schema.Annotation{enttui.Browse()}
}
```

Today every non-internal ent type with an ID is browsable, so `Browse()` is reserved for a future "exclude unless explicitly marked" mode. Including it now is a no-op.

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
- **Sortable = true** — pressing `s` on the column header (or adding it
  via the `S` sort-stack modal) emits `pred.By<Edge>Field(targetPred.Field<Field>, sql.OrderAsc())`
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

Enables row-selection (`space` toggles, `*` selects all visible, `0`
clears) and the multi-row `y` copy flow. Pressing `y` with one or more
rows selected opens a format-chooser modal:

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

Enables the **`X`** shortcut. With rows selected (`space`), exports
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

Enables the **`N`** (new row) keybinding. The form opens with every
Editable field empty; scope keys from `app.SetScope(...)` are
auto-injected so the new row lands in the right tenant / project / etc.
Generated code emits a `Create: func(ctx, vals) (id, err)` closure that
calls `client.X.Create().SetX(v)…Save(ctx)`.

```go
func (Post) Annotations() []schema.Annotation {
    return []schema.Annotation{enttui.AllowCreate{}}
}
```

Status bar shows `[green]+ N new[-]` when active.

### `AllowDelete{}` — entity-level

Enables the **`D`** (delete with confirm) keybinding for this entity.
Off by default — destructive actions opt-in only. Generated code emits a
`Delete: func(ctx, id) error { return client.X.DeleteOneID(id).Exec(ctx) }`
closure. Status bar shows `[red]✗ D delete[-]` when active.

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

> **Note:** `AsTitle()` / `AsBody()` / `AsStatus()` were removed. The
> library no longer has a hero-field concept — every field is just a
> column, and the row label / preview body are picked by convention
> (`title` → `name` → `display_name` → `id` for the label;
> `body` / `description` / `content` rendered as the preview body).
> See [CONVENTIONS.md](CONVENTIONS.md).

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
`[green]✎ e edit[-]`. Pressing `e` on a schema with none → status hint
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
    Annotations(enttui.Upward("p")),
```

### `Upward(trigger)`

Marks this `Unique()` edge as breadcrumb-style (jumps to a single parent row). `trigger` is the key the user presses on a list row to follow it.

If omitted, the generator auto-picks a trigger letter from the edge name.

### `Drill(trigger)`

Marks this non-unique edge as drillable (opens a new browser scoped to those rows). `trigger` is usually a single letter (`"c"` for comments, `"r"` for replies, etc.) — `"enter"` no longer has special meaning.

```go
edge.To("comments", Comment.Type).
    Annotations(enttui.Drill("c")),
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
        field.String("title").NotEmpty(),
        field.Text("body").Optional().Nillable(),
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
