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
func (Task) Annotations() []schema.Annotation {
    return []schema.Annotation{enttui.Browse()}
}
```

Today every non-internal ent type with an ID is browsable, so `Browse()` is reserved for a future "exclude unless explicitly marked" mode. Including it now is a no-op.

### `Display(string)`

Pretty name shown in the kind picker and pane titles.

```go
enttui.Display("Tasks")
```

Default: convention-pluralized type name (`Task` → "Tasks", `Memory` → "Memories").

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

## Field-level annotations

Attached to a field builder:

```go
field.String("title").
    Annotations(enttui.AsTitle()),
```

### `AsTitle()`

Marks this field as the row title (overrides title/name/display_name heuristic).

### `AsBody()`

Marks this field as the preview body (overrides body/description/content heuristic).

### `AsStatus()`

Marks this field as the status chip source (overrides status/severity/kind heuristic).

### `Sortable()`

Lets the runtime cycle sort through this field. Multiple `Sortable()` fields cycle in declaration order.

### `Filterable()`

Includes this field in the substring filter predicate (defaults to title + body + name only).

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

Marks this non-unique edge as drillable (opens a new browser scoped to those rows). `trigger` is usually `"enter"` for the primary edge.

```go
edge.To("subtasks", Task.Type).
    Annotations(enttui.Drill("enter")),
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

type Task struct{ ent.Schema }

func (Task) Annotations() []schema.Annotation {
    return []schema.Annotation{
        enttui.Browse(),
        enttui.Display("Tasks"),
        enttui.Group("workflow"),
        enttui.Icon("✓"),
        enttui.DefaultSort("created_at", enttui.Desc),
        enttui.PageSize(200),
    }
}

func (Task) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").Unique().Immutable(),
        field.String("project_id").NotEmpty().Immutable(),
        field.String("title").NotEmpty().
            Annotations(enttui.AsTitle(), enttui.Filterable()),
        field.Text("body").Optional().Nillable().
            Annotations(enttui.AsBody(), enttui.Filterable()),
        field.Enum("status").Values("todo", "doing", "done", "blocked").
            Annotations(
                enttui.AsStatus(),
                enttui.Sortable(),
                enttui.Filterable(),
                enttui.Chip(map[string]string{
                    "todo":    "muted",
                    "doing":   "warn",
                    "done":    "success",
                    "blocked": "danger",
                }),
            ),
        field.Enum("priority").Values("p0", "p1", "p2", "p3").
            Annotations(
                enttui.Sortable(),
                enttui.Filterable(),
                enttui.Chip(map[string]string{
                    "p0": "danger", "p1": "warn", "p2": "info", "p3": "muted",
                }),
            ),
        field.String("internal_hash").
            Annotations(enttui.Hidden()),
        field.JSON("payload", map[string]any{}).
            Annotations(enttui.Format(enttui.FormatPrettyJSON)),
        field.Time("created_at").Default(time.Now).
            Annotations(enttui.Sortable(), enttui.Format(enttui.FormatRelativeTime)),
        field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
    }
}

func (Task) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("project", Project.Type).Ref("tasks").Unique().
            Annotations(enttui.Upward("p")),
        edge.From("tasklist", TaskList.Type).Ref("tasks").Unique().
            Annotations(enttui.Upward("l")),
        edge.To("subtasks", Task.Type).
            Annotations(enttui.Drill("enter")),
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
