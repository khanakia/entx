# Conventions

`enttui` runs in **convention mode** today: it inspects your ent schema and applies rules to figure out which entities to browse and how to display them — no annotations required. Annotations (see [ANNOTATIONS.md](ANNOTATIONS.md)) are designed but not yet wired to override these rules; conventions are 100% of the input for M0.5.

## Entity inclusion

An ent.Type is **browsable** if and only if:

1. It has an `ID` field (every ent type does unless explicitly disabled).
2. It's not on the **internal blocklist** (see below).
3. It's not in the user's `--skip` flag.

Scope filtering is independent of inclusion: pass `Config.ScopeFields` (CLI: `--scope project_id,tenant_id`) and the codegen emits one predicate per match for whichever scope fields each entity actually has. Entities without those fields stay browsable, just unscoped — driven at runtime via `app.SetScope(key, value)`.

Internal blocklist (skipped automatically):

- `SchemaMigration`
- `AuditLog`
- `QueryLog`
- `PiiPattern`
- Anything ending in `_fts` (FTS5 shadow tables)
- Anything containing `shadow` in the name

Override the blocklist behavior with `--skip Type1,Type2` for additional exclusions; **there is no opt-back-in flag yet** — if you need a blocklisted type, edit the generator's `isInternal()` and rebuild.

## Display defaults

| Generated field | Rule |
|-----------------|------|
| `Kind`          | `strings.ToLower(Type.Name)` — e.g. `Post` → `"post"` |
| `Display`       | Naïve pluralizer of `Type.Name` — `Post` → `"Posts"`, `Author` → `"Authors"`, `Memory` → `"Memories"` (y → ies). |
| `Group`         | `"data"` (single group for everything until annotation support lands) |
| `Icon`          | `"•"` (placeholder) |

## Hero accessors

These three closures are what `runtime.Row.Title` / `.Body` / `.Status` are populated from. They drive the list pane label, the preview body, and the colored chip.

### Title

First match in field iteration order (which is mixin fields first, then `Fields()` order):

1. A field named **`title`**.
2. A field named **`name`**.
3. A field named **`display_name`**.

If none match → no Title accessor is emitted; list rows fall back to ID.

### Body

First match:

1. **`body`**
2. **`description`**
3. **`content`**

If none → no Body accessor; preview shows only fields.

### Status

First **enum-typed** field matching:

1. **`status`**
2. **`severity`**
3. **`kind`**
4. **`state`**

The accessor returns `string(r.Field)`. Color chips happen only if you've also added `enttui.Chip(...)` to the field (annotations not wired yet) — until then, status appears as plain text.

## Columns

Every field is included as a column **except**:

- `body` / `description` / `content` (already used as the preview body)
- JSON / `[]byte` fields (too noisy to show as a string)

Hidden fields (M1) will further filter this list. Today no field is hidden by default.

## Time columns

If the type has `created_at` → `EntitySpec.CreatedAt` accessor is emitted + a column.
If `updated_at` → `EntitySpec.UpdatedAt` accessor + column.

Both are rendered as `2006-01-02 15:04:05`. Optional time fields use `if r.X == nil { return "" }` first.

## Filter

No field-name hardcoding. A field is **Filterable** when:

- it carries `enttui.Filterable()`, **or**
- the convention auto-marks it: every `string` / `enum` field is Filterable by default (**opt-out** with `enttui.Hidden()`).

Two consumers of that flag:

- **Condition builder (`f`)** — lists every Filterable column (string + enum), typed operators per kind.
- **`/` quick filter** — `opts.Filter` is wired to `Or(<F>ContainsFold(opts.Filter), …)` across every Filterable **string** field (enum predicates have no `ContainsFold`, so enums are excluded from `/` — use `f` for those). This means `/manual` finds rows by a `source_kind`-style column, not just `title`/`body`.

Because the default is opt-out, `id` / FK columns are searchable too unless you `enttui.Hidden()` them. To make the surface precise, hide noisy columns or (future) switch the generator to opt-in.

## Sort

Today: only `created_at` is sortable. The runtime cycles ascending ↔ descending on `s`. If the type has no `created_at`, no sort is applied (`s` does nothing).

Planned (M1): annotation `enttui.Sortable()` per field; runtime cycles through them.

## Edges

Every edge whose target Type is itself browsable is included. (Edges to skipped types — e.g. an edge to `AuditLog` — are silently dropped.)

| Edge property      | Generated mapping |
|--------------------|-------------------|
| `e.Unique == true` | `EdgeUpward`. Display: `"→ <TargetDisplay>"`. Triggered by an auto-picked single letter. |
| `e.Unique == false`| `EdgeDrill`. Display: `<TargetDisplay>`. First non-unique edge claims `enter`. |

Trigger key picking: walk the edge name letter by letter, take the first one that's:

- not reserved (`k`, `q`, `s`, `r`, `h`, `j`, `l`)
- not already used by another edge on this type
- a-z only

If the edge name has no usable letter (e.g. all letters are reserved), the picker falls back to scanning a..z. Worst case → `?` placeholder.

Generic examples:

| Type    | Edge       | Unique | Generated trigger | Display    |
|---------|------------|--------|-------------------|------------|
| Post    | author     | true   | `a`               | → Authors  |
| Post    | category   | true   | `c`               | → Categorys|
| Post    | comments   | false  | `o`               | Comments   |
| Author  | posts      | false  | `o`               | Posts      |
| Order   | customer   | true   | `u`               | → Customers|

## Page size

Hardcoded `200`. Annotation `enttui.PageSize(N)` will override (M1).

## Field-type kind discrimination

The generator's `fieldKind()` reduces every `codegen.Field` to one of:

| Kind        | Go shape         | Template emits |
|-------------|------------------|----------------|
| `string`    | `string`         | `return r.Foo` |
| `stringPtr` | `*string`        | `if r.Foo == nil { return "" }; return *r.Foo` |
| `enum`      | `post.Status` etc. | `return string(r.Foo)` |
| `enumPtr`   | `*post.Status`   | nil-guard + `string(*r.Foo)` |
| `time`      | `time.Time`      | `if r.Foo.IsZero() { return "" }; return r.Foo.Format(...)` |
| `timePtr`   | `*time.Time`     | nil-guard + `IsZero` + format |
| `scalar`    | `int`/`float`/`bool` | `return fmt.Sprintf("%v", r.Foo)` |
| `scalarPtr` | `*int` etc.      | nil-guard + Sprintf |

The "needs fmt" flag (`em.NeedsFmt`) is computed by walking columns and hero fields — if any of them have kind `scalar` or `scalarPtr`, we import `fmt`; otherwise we don't (avoiding "unused import" errors).

## What annotations will change (M1)

The annotations declared in `enttui/annotation.go` are the future overrides:

- `enttui.Browse()` → reserved for the future "exclude unless explicitly marked" mode.
- `enttui.Display("…")`, `Group("…")`, `Icon("…")` → override display labels.
- `enttui.AsTitle()`, `AsBody()`, `AsStatus()` → override the field-name heuristic (e.g. use `name` even when both `title` and `name` exist).
- `enttui.Sortable()` / `Filterable()` / `Hidden()` → per-field flags.
- `enttui.Chip(map[string]string)` → status color map.
- `enttui.Drill(trigger)` / `Upward(trigger)` → control edge trigger keys, override the first-letter heuristic.

Until the codegen reads these, drop in conventions only.
