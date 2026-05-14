// Package enttui hosts the schema-annotation API. The codegen extension
// (see ./gen) reads these annotations off ent schemas and emits runtime
// registrations.
//
// Status: API surface declared. Codegen reads these starting M0.5; for now
// the example in ./examples/aicoder wires entities by hand to validate the
// runtime first.
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
