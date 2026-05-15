// Package runtime hosts the handwritten, framework-side core of enttui.
// Generated code (per-entity, in <project>/tui/gen/) registers entities into
// this runtime via typed EntitySpec values.
//
// Everything in this package MUST be schema-agnostic. No imports of any
// specific ent package allowed.
package runtime

import (
	"context"
)

// SortDir is asc/desc — the direction component of a single SortKey.
type SortDir int

const (
	Asc SortDir = iota
	Desc
)

// SortKey is one entry in a sort stack. The stack as a whole orders rows
// by the first key, then breaks ties with the second, and so on.
//
// Multi-column sort introduced in Phase D. Phase A–C used a single field.
type SortKey struct {
	Field string  // matches a Column.Key — generated code dispatches on this
	Dir   SortDir // ascending or descending for this key
}

// FilterOp enumerates the predicate operators a column can be filtered by.
// Phase C wires the table view + condition builder to populate these; the
// generated code maps each Op to the appropriate ent predicate (EQ, NEQ,
// LT, GT, LTE, GTE, In, NotIn, ContainsFold, IsNil, NotNil).
type FilterOp string

const (
	OpEq       FilterOp = "="
	OpNeq      FilterOp = "!="
	OpLt       FilterOp = "<"
	OpLte      FilterOp = "<="
	OpGt       FilterOp = ">"
	OpGte      FilterOp = ">="
	OpIn       FilterOp = "in"
	OpNotIn    FilterOp = "not_in"
	OpContains FilterOp = "contains" // case-insensitive substring
	OpIsNull   FilterOp = "is_null"
	OpNotNull  FilterOp = "not_null"
)

// FilterCondition is a single (column, op, value) triple. The runtime
// passes a slice of these to the generated Fetch closure, which is
// responsible for translating them into ent predicates and ANDing them
// together. Phase F (condition builder) adds boolean grouping on top.
type FilterCondition struct {
	Field string   // matches a Column.Key
	Op    FilterOp // EQ / CONTAINS / IS_NULL / etc.
	Value string   // raw string; generated code parses to int/time/etc. when needed
	// GroupID is used by the condition builder to express nested AND/OR
	// trees. Conditions sharing a GroupID are AND-composed within their
	// group; groups themselves OR together. 0 = top-level (default = AND).
	GroupID int
}

// EdgeKind classifies how an edge is presented in the UI.
type EdgeKind int

const (
	// EdgeDrill is a 1→N relationship presented as "open a new Browser
	// page filtered to these rows."
	EdgeDrill EdgeKind = iota
	// EdgeUpward is an N→1 relationship presented as "jump to the parent's
	// preview" (single entity ref).
	EdgeUpward
)

// EntityRef is an opaque pointer to one row of any registered entity.
// Used as the unit of edge navigation.
type EntityRef struct {
	Kind string // matches EntitySpec.Kind
	ID   string
}

// EntityRefList is a batch of refs of the same kind, the result of an
// EdgeDrill resolver.
type EntityRefList struct {
	Kind string
	IDs  []string
}

// ListOpts is what generated Fetch closures receive.
//
// Scope is a generic string/string bag set on the App via SetScope. The
// runtime never inspects its contents — generated Fetch closures look up
// whichever keys they recognize (e.g. "project_id" for project-scoped
// entities, "tenant_id" for tenant-scoped, etc.). Empty → no scope filter.
//
// Sort is a stack of SortKey values — order matters. The first entry is
// the primary sort key, the rest are tie-breakers.
//
// Filters is an AND-composed slice (or a grouped tree when using the
// condition builder). Empty → no predicates beyond Scope.
type ListOpts struct {
	Filter    string             // legacy substring; matches against Filterable fields (kept for Phase A/B back-compat)
	Filters   []FilterCondition  // structured per-column conditions (Phase E+)
	Sort      []SortKey          // multi-column sort stack (Phase D+)
	SortField string             // legacy single-sort field (deprecated; use Sort)
	SortDir   SortDir            // legacy single-sort dir (deprecated; use Sort)
	Offset    int
	Limit     int
	Scope     map[string]string // arbitrary consumer-defined scope filters
}

// Column describes one displayable field of an entity. Generated code
// produces []Column[T] where T is the ent row type.
//
// The boolean flags are populated by codegen based on schema annotations
// — Sortable() / Filterable() / Hidden(). Width / Align / Chip likewise.
type Column[T any] struct {
	Key        string             // stable key (matches ent field name)
	Label      string             // pretty label shown to the user
	Get        func(T) string     // typed accessor — no reflect
	Chip       map[string]string  // optional value→tone color map ("done"→"success")
	Hidden     bool               // never shown
	Sortable   bool               // appears in the sort cycle
	Filterable bool               // appears in filter row + condition builder
	Width      int                // preferred column width in cells; 0 = auto
	Align      string             // "left" (default), "right", "center"
	// EnumValues, when non-empty, makes this column an enum: the condition
	// builder shows a picker of these exact values (single-select for
	// `=`/`!=`, multi-select for `in`/`not_in`) instead of a free-text
	// input. The generated Fetch closure dispatches OpIn / OpNotIn for
	// enum columns into the typed `pred.<Field>In(...)` predicate.
	EnumValues []string
}

// EdgeSpec is one navigable edge from an entity. Generated.
type EdgeSpec[T any] struct {
	Name    string // ent edge name
	Display string // pretty label
	Kind    EdgeKind
	Trigger string // keybinding (e.g. "enter", "p", "c")
	// Count is optional; if set, the preview shows "(N)" next to the edge.
	Count func(ctx context.Context, row T) (int, error)
	// ResolveUpward is set when Kind == EdgeUpward.
	ResolveUpward func(ctx context.Context, row T) (EntityRef, error)
	// ResolveDrill is set when Kind == EdgeDrill.
	ResolveDrill func(ctx context.Context, row T) (EntityRefList, error)
}

// FormField describes one user-editable input in the edit/create modal.
// Driven by enttui.Editable() per-field annotations. The runtime renders
// a widget appropriate to Kind:
//
//   - "string" / "stringPtr" → text input
//   - "enum" / "enumPtr"     → dropdown over EnumValues
//   - everything else        → text input with type-cast in the generated
//                              setter (codegen handles strconv / parse)
//
// Required marks the field as non-nullable on the schema (used by the
// form to red-flag empty submissions before hitting the DB).
type FormField struct {
	Key        string   // matches a Column.Key — codegen dispatches on this
	Label      string   // pretty label shown next to the input
	Kind       string   // mirrors Column kind: string / stringPtr / enum / enumPtr / time / scalar
	Required   bool     // schema-side NotEmpty / non-nillable
	EnumValues []string // populated for enum / enumPtr kinds; drives the dropdown
}

// DefaultView captures the entity's preferred sort/filter + view mode at
// first open.
type DefaultView struct {
	SortField string
	SortDir   SortDir
	// Mode is "list" (list+preview, default) or "table" (table fullscreen).
	// Per-entity annotation enttui.DefaultView("table") flips this.
	Mode string
}

// EntitySpec is the typed description of one browsable entity. Generated
// code emits one EntitySpec[T] per ent schema annotated with Browse().
type EntitySpec[T any] struct {
	// Identity
	Kind    string // url-safe ident, e.g. "task"
	Display string // pretty label for the picker
	Group   string // picker group, e.g. "workflow"
	Icon    string // single rune, e.g. "✓"

	// Behavior
	PageSize       int  // initial page size; runtime caps at 1000
	MultiSort      bool // allow sort stack (true by default; false = single column only)
	ShowEdgeCounts bool // when true, the preview pane calls each edge's Count closure and renders the result next to the trigger label
	Default        DefaultView

	// Data
	Fetch func(ctx context.Context, opts ListOpts) (rows []T, total int, err error)

	// JSON serializes the typed row in ent's native format (including
	// any eager-loaded `edges`). Codegen emits `return json.Marshal(r)`
	// which goes through *ent.X.MarshalJSON. Used by the `J` clipboard
	// shortcut. Optional — when nil, J copies the flat columns map.
	JSON func(T) ([]byte, error)

	// FormFields lists the user-editable fields for this entity. Empty =
	// the form modal won't show anything to edit (e.g. read-only browser).
	// Driven by enttui.Editable() per-field annotations in codegen.
	FormFields []FormField

	// Update saves edits on an existing row. vals is keyed by FormField.Key,
	// values are strings the runtime form collected from the user; the
	// generated closure casts/parses each into the right ent setter.
	// Nil = no edit support.
	Update func(ctx context.Context, id string, vals map[string]string) error

	// Delete removes the row by ID. Nil = entity not deletable
	// (no enttui.AllowDelete() annotation).
	Delete func(ctx context.Context, id string) error

	// Display accessors — all visible columns in declaration order.
	Columns []Column[T]

	// Navigation
	Edges []EdgeSpec[T]
}
