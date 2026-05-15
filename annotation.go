// Package enttui hosts the schema-annotation API. The codegen extension
// (see ./codegen) reads these annotations off ent schemas and emits the
// runtime registrations.
//
// The library is schema-agnostic: nothing in here assumes any particular
// domain. Examples in doc comments use generic names like Post / Author
// / Comment — substitute your own schemas freely.
package enttui

import "entgo.io/ent/schema"

// --- schema-level ---

// Browse opts a schema in to the TUI. Default is excluded.
type Browse struct{ schema.Annotation }

// Name implements schema.Annotation.
func (Browse) Name() string { return "EntTUI.Browse" }

// Display sets the pretty name shown in the kind picker.
type Display struct {
	schema.Annotation
	Value string
}

func (Display) Name() string { return "EntTUI.Display" }

// Group sets the picker group label (e.g. "workflow", "knowledge").
type Group struct {
	schema.Annotation
	Value string
}

func (Group) Name() string { return "EntTUI.Group" }

// Icon is a single rune shown next to the display name in the picker.
type Icon struct {
	schema.Annotation
	Value string
}

func (Icon) Name() string { return "EntTUI.Icon" }

// SortDirection is asc|desc for DefaultSort.
type SortDirection string

const (
	Asc  SortDirection = "asc"
	Desc SortDirection = "desc"
)

// DefaultSort sets the initial sort column + direction.
type DefaultSort struct {
	schema.Annotation
	Field     string
	Direction SortDirection
}

func (DefaultSort) Name() string { return "EntTUI.DefaultSort" }

// PageSize overrides the runtime default (200).
type PageSize struct {
	schema.Annotation
	Value int
}

func (PageSize) Name() string { return "EntTUI.PageSize" }

// ProjectScope marks the field that holds project_id. When set the runtime
// auto-filters by the current project.
type ProjectScope struct {
	schema.Annotation
	Field string
}

func (ProjectScope) Name() string { return "EntTUI.ProjectScope" }

// CountEdges turns on per-edge count display in the preview pane. With
// it enabled, the runtime fires the generated Count closure for every
// edge whenever a row's preview is rendered, and shows the result in
// parentheses next to the edge label (e.g. "subtasks (5)"). Counts cost
// one query per edge per row-selection-change — cheap on local SQLite,
// possibly costly on remote DBs, hence opt-in.
//
//	func (Task) Annotations() []schema.Annotation {
//	    return []schema.Annotation{
//	        enttui.Browse(),
//	        enttui.CountEdges(),
//	    }
//	}
type CountEdges struct{ schema.Annotation }

func (CountEdges) Name() string { return "EntTUI.CountEdges" }

// DetailEdge designates one or more drill (1→N) edges as the "detail"
// relations for master-detail mode (`m`). The split view shows the
// master kind on top and a live child table below; with multiple edges
// the detail pane becomes tabbed, one tab per edge.
//
//	enttui.DetailEdge{Edge:  "comments"}                 // single
//	enttui.DetailEdge{Edges: []string{"tasks","comments"}} // tabbed
//
// Edge is the single-edge convenience; Edges is the multi form. When
// both are set they're concatenated (Edge first).
type DetailEdge struct {
	schema.Annotation
	Edge  string
	Edges []string
}

func (DetailEdge) Name() string { return "EntTUI.DetailEdge" }

// AllowBulkCopy enables row-selection (`space` / `a` / `c`) and the
// multi-row `y` flow — pressing `y` with one or more rows selected
// opens a format chooser (JSON array / CSV / focused-column JSON / CSV)
// and copies the result to the clipboard. Off by default.
type AllowBulkCopy struct{ schema.Annotation }

func (AllowBulkCopy) Name() string { return "EntTUI.AllowBulkCopy" }

// AllowExport enables the `X` shortcut — re-fetches every row matching
// the current filter + sort (capped at 10_000 to keep clipboards sane),
// opens a JSON / CSV chooser, copies the result. Off by default.
type AllowExport struct{ schema.Annotation }

func (AllowExport) Name() string { return "EntTUI.AllowExport" }

// AllowCreate enables the `N` (new row) shortcut. The form opens with
// every Editable() field empty, plus any scope keys from app.SetScope()
// pre-injected so the new row lands in the right tenant / project /
// etc. Off by default.
type AllowCreate struct{ schema.Annotation }

func (AllowCreate) Name() string { return "EntTUI.AllowCreate" }

// AllowDelete enables the `D` (delete with confirm) shortcut. Off by
// default — destructive actions opt-in only.
type AllowDelete struct{ schema.Annotation }

func (AllowDelete) Name() string { return "EntTUI.AllowDelete" }

// --- field-level ---

// AsTitle marks a field as the row title.
type AsTitle struct{ schema.Annotation }

func (AsTitle) Name() string { return "EntTUI.AsTitle" }

// AsBody marks a field as the preview body.
type AsBody struct{ schema.Annotation }

func (AsBody) Name() string { return "EntTUI.AsBody" }

// AsStatus marks a field as the status chip source.
type AsStatus struct{ schema.Annotation }

func (AsStatus) Name() string { return "EntTUI.AsStatus" }

// Sortable enables sort cycling on this field.
type Sortable struct{ schema.Annotation }

func (Sortable) Name() string { return "EntTUI.Sortable" }

// Filterable includes the field in substring filter searches.
type Filterable struct{ schema.Annotation }

func (Filterable) Name() string { return "EntTUI.Filterable" }

// Hidden excludes the field from all views.
type Hidden struct{ schema.Annotation }

func (Hidden) Name() string { return "EntTUI.Hidden" }

// Editable marks a field as user-editable in the edit / create form
// modal. Opt-in per field — if no fields on an entity carry Editable(),
// the entity has no edit UI at all (safe default for read-only browsers).
//
//	field.String("title").NotEmpty().
//	    Annotations(enttui.Editable()),
type Editable struct{ schema.Annotation }

func (Editable) Name() string { return "EntTUI.Editable" }

// Chip attaches a value→tone color map to an enum field.
// Tones: success, warn, danger, info, muted.
type Chip struct {
	schema.Annotation
	Tones map[string]string
}

func (Chip) Name() string { return "EntTUI.Chip" }

// FormatKind enumerates value formatters.
type FormatKind string

const (
	FormatRaw          FormatKind = ""
	FormatPrettyJSON   FormatKind = "json"
	FormatRelativeTime FormatKind = "relative-time"
)

// Format attaches a formatter to a field.
type Format struct {
	schema.Annotation
	Kind FormatKind
}

func (Format) Name() string { return "EntTUI.Format" }

// RelatedColumns attaches one or more "draw a value from an FK target"
// columns to an entity. Schema-level annotation; each entry produces
// one column in the table.
//
// Example: on a Post entity, show the parent Author's name + email:
//
//	enttui.RelatedColumns(
//	    enttui.RelatedColumn{Edge: "author", Field: "name",  Label: "Author"},
//	    enttui.RelatedColumn{Edge: "author", Field: "email"},
//	)
//
// Codegen detects the edge target type, validates the field exists, and
// emits a column whose Get accessor reads r.Edges.<Edge>.<Field>. The
// host's Fetch closure eager-loads via With<Edge>() so there's no N+1.
type RelatedColumn struct {
	Edge  string // ent edge name on the host (e.g. "author")
	Field string // ent field name on the target type (e.g. "name", "email")
	Label string // optional display label; defaults to "<Edge> <Field>"
}

// RelatedColumnsAnnot is the multi-entry annotation read by the codegen.
type RelatedColumnsAnnot struct {
	schema.Annotation
	Columns []RelatedColumn
}

func (RelatedColumnsAnnot) Name() string { return "EntTUI.RelatedColumns" }

// RelatedColumns is the schema-side constructor. Pass any number of
// RelatedColumn entries.
func RelatedColumns(cols ...RelatedColumn) RelatedColumnsAnnot {
	return RelatedColumnsAnnot{Columns: cols}
}

// --- edge-level ---

// Upward marks an N→1 edge as a breadcrumb-style jump. Trigger is the key
// that follows it (e.g. "p" for project).
type Upward struct {
	schema.Annotation
	Trigger string
}

func (Upward) Name() string { return "EntTUI.Upward" }

// Drill marks a 1→N edge as a "open these rows in a new browser page"
// jump. Trigger is the key (often "enter").
type Drill struct {
	schema.Annotation
	Trigger string
}

func (Drill) Name() string { return "EntTUI.Drill" }
