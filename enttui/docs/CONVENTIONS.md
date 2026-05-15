# Conventions

`enttui` runs in **convention mode** today: it inspects your ent schema and applies rules to figure out which entities to browse and how to display them — no annotations required. Annotations (see [ANNOTATIONS.md](ANNOTATIONS.md)) are designed but not yet wired to override these rules; conventions are 100% of the input for M0.5.

## Entity inclusion

An ent.Type is **browsable** if and only if:

1. It has an `ID` field (every ent type does unless explicitly disabled).
2. It's not on the internal blocklist **OR** it carries `enttui.Browse{}`.
3. It's not in the user's `enttui.Skip(...)` / `--skip` config.

Scope filtering is independent of inclusion: pass `Config.ScopeFields` (CLI: `--scope project_id,tenant_id`) and the codegen emits one predicate per match for whichever scope fields each entity actually has. Entities without those fields stay browsable, just unscoped — driven at runtime via `app.SetScope(key, value)`.

Internal blocklist (skipped by default — a convention, fully overridable):

- `SchemaMigration`, `AuditLog`, `QueryLog`, `PiiPattern`
- `*_fts` (FTS5 shadow tables), `*shadow*`

**Opt back in** with `enttui.Browse{}` on the schema (no codegen edit needed). **Opt out** of anything else with `enttui.Skip("X")` / `--skip X`.

## Display defaults

| Generated field | Rule |
|-----------------|------|
| `Kind`          | `strings.ToLower(Type.Name)` — e.g. `Post` → `"post"` |
| `Display`       | Naïve pluralizer of `Type.Name` — `Post` → `"Posts"`, `Author` → `"Authors"`, `Memory` → `"Memories"` (y → ies). |
| `Group`         | `"data"` (single group for everything until annotation support lands) |
| `Icon`          | `"•"` (placeholder) |

## Label / body / id resolution

There is **no runtime field-name guessing**. Codegen resolves three
column keys and emits them onto the spec (`LabelKey`, `BodyKey`,
`IDKey`); the runtime just reads them.

### Row label (`spec.LabelKey`)

1. The field annotated **`enttui.AsTitle{}`** wins.
2. Else convention fallback, first of: `title → name → display_name →
   label → summary`.
3. Else the id column.

### Preview body (`spec.BodyKey`)

1. The field annotated **`enttui.AsBody{}`** wins.
2. Else convention fallback, first of: `body → description → content`.
3. Else empty — preview shows only the field list.

Body-shaped columns are `Hidden` in the table by default (wide prose
makes bad cells) but still present in `Row.Columns` for the preview and
the `J` JSON copy.

### Row id (`spec.IDKey`)

The schema's **actual ID field** (`gen.Type.ID`) — whatever its name
(`id`, `uuid`, `pk`, …) and type (string / int / uuid). The generated
accessor stringifies non-string ids via `fmt.Sprintf`. No literal
`"id"` anywhere in the runtime.

### Status

`enttui.AsStatus{}` is declared but not yet wired. Status coloring is
done per-field via `enttui.Chip(map[string]string{...})` on the
relevant enum/string column.

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

Trigger key: **`enttui.Upward{Trigger:"x"}` / `Drill{Trigger:"x"}` win** (honored by codegen; skipped only if the letter is already taken on that type). Otherwise auto-pick — walk the edge name letter by letter, take the first that's:

- not reserved (`h`, `j`, `k`, `l`, `g`, `n`, `p`, `r`, `q`, `v`, `y`, `s` — the vim-faithful top-level keys)
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

Default `200`; `enttui.PageSize(N)` overrides per entity.

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

The "needs fmt" flag (`em.NeedsFmt`) is computed by walking columns — if any has kind `scalar` / `scalarPtr` (incl. an int/uuid id), we import `fmt`.

## Annotation overrides (all active)

Every convention above is just a default; these annotations override it,
and the codegen reads them today:

- `enttui.Browse{}` → force-include an internal-blocklisted entity.
- `enttui.Display/Group/Icon/PageSize/DefaultView` → identity + display.
- `enttui.AsTitle{}` / `AsBody{}` → pin the label / preview-body column.
- `enttui.Sortable()` / `Filterable()` / `Hidden()` / `Editable{}` → per-field flags (`/` quick filter + condition builder + sort + form).
- `enttui.Chip(map[string]string)` → value→tone coloring.
- `enttui.Upward{Trigger}` / `Drill{Trigger}` → pin edge keybindings.
- `enttui.RelatedColumns(...)` (+ `Pick:true`) → cross-table column / ref picker.
- `enttui.DetailEdge{Edge|Edges}` → master-detail split (`m`).
- `enttui.AllowCreate{}/AllowDelete{}/AllowBulkCopy{}/AllowExport{}` → enable the corresponding actions.

The runtime contains no schema field-name literals — all such decisions
are resolved at codegen and emitted onto the spec.
