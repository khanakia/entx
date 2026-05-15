package runtime

import (
	"context"
	"fmt"
	"sort"
)

// anySpec is the type-erased shape of an EntitySpec[T]. Closures inside
// capture the original T; callers see only string keys + string accessors.
//
// This is the single unsafe seam in enttui. Localizing it here means the
// rest of the runtime (browser, picker, edge nav) can iterate registered
// entities without any generic gymnastics.
type anySpec struct {
	kind    string
	display string
	group   string
	icon    string

	pageSize       int
	multiSort      bool
	showEdgeCounts bool
	defaultView    DefaultView

	// fetch returns the row IDs + their typed display data.
	fetch func(ctx context.Context, opts ListOpts) ([]Row, int, error)
	// getOne returns a single row by ID (used by edge drill / upward jumps).
	getOne func(ctx context.Context, id string) (*Row, error)

	columns []anyColumn
	edges   []anyEdge

	// Form support — driven by enttui.Editable() / AllowDelete().
	// Nil closures mean the corresponding operation is disabled.
	formFields []FormField
	update     func(ctx context.Context, id string, vals map[string]string) error
	deleteRow  func(ctx context.Context, id string) error
}

type anyColumn struct {
	key        string
	label      string
	chip       map[string]string
	hidden     bool
	sortable   bool
	filterable bool
	width      int
	align      string
	// enumValues is non-empty when the column is an enum — drives the
	// condition builder to show a value picker instead of a text input.
	enumValues []string
}

type anyEdge struct {
	name    string
	display string
	kind    EdgeKind
	trigger string
	// count returns N or -1 if not exposed.
	count func(ctx context.Context, id string) (int, error)
	// resolveUpward / resolveDrill bound to a row ID.
	resolveUpward func(ctx context.Context, id string) (EntityRef, error)
	resolveDrill  func(ctx context.Context, id string) (EntityRefList, error)
}

// viewState is the per-page state that survives a list↔table toggle.
// Both browser and tableView produce + consume this struct so the user
// keeps their filter / sort / pagination / selection across `v`.
type viewState struct {
	Filter          string
	Filters         []FilterCondition
	SortField       string
	SortDir         SortDir
	SortStack       []SortKey
	Page            int
	PageSize        int
	SelectedID      string
	ColumnOverrides map[string]bool // table-only; ignored by browser
}

// Row is the runtime-visible projection of one ent row. Generic — no
// hardcoded title/body/status assumption. Every value is the typed
// accessor's output stored as a string in Columns; ID is broken out
// because the runtime keys edge resolution + selection on it. JSON
// holds the ent struct serialized in its native shape (including
// eager-loaded `edges`) so `J` works against any schema.
type Row struct {
	ID      string
	Columns map[string]string
	JSON    []byte
}

// Register adds a typed EntitySpec to the app. Generated code calls this
// once per entity. Generics are erased at the boundary so the App can hold
// a heterogeneous registry.
func Register[T any](app *App, spec EntitySpec[T]) {
	if spec.Kind == "" {
		panic("enttui: EntitySpec.Kind is required")
	}
	if spec.Fetch == nil {
		panic("enttui: EntitySpec.Fetch is required for " + spec.Kind)
	}
	if spec.PageSize == 0 {
		spec.PageSize = 200
	}

	// Type-erased fetch closure.
	fetch := func(ctx context.Context, opts ListOpts) ([]Row, int, error) {
		rows, total, err := spec.Fetch(ctx, opts)
		if err != nil {
			return nil, 0, err
		}
		out := make([]Row, 0, len(rows))
		for _, r := range rows {
			out = append(out, projectRow(spec, r))
		}
		return out, total, nil
	}

	// Type-erased get-one closure — fetches without filter, finds the
	// first row matching ID. Generated code can override this with a
	// proper `client.X.Get(ctx, id)` for efficiency, but the default
	// works.
	getOne := func(ctx context.Context, id string) (*Row, error) {
		// Naive fallback — will be overridden once we add a GetOne
		// closure to EntitySpec. Sufficient for M0.
		rows, _, err := spec.Fetch(ctx, ListOpts{Limit: spec.PageSize})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			row := projectRow(spec, r)
			if row.ID == id {
				return &row, nil
			}
		}
		return nil, fmt.Errorf("enttui: %s/%s not found", spec.Kind, id)
	}

	columns := make([]anyColumn, 0, len(spec.Columns))
	for _, c := range spec.Columns {
		columns = append(columns, anyColumn{
			key: c.Key, label: c.Label, chip: c.Chip, hidden: c.Hidden,
			sortable: c.Sortable, filterable: c.Filterable,
			width: c.Width, align: c.Align,
			enumValues: c.EnumValues,
		})
	}

	edges := make([]anyEdge, 0, len(spec.Edges))
	for _, e := range spec.Edges {
		ae := anyEdge{
			name: e.Name, display: e.Display, kind: e.Kind, trigger: e.Trigger,
		}
		if e.Count != nil {
			cnt := e.Count
			ae.count = func(ctx context.Context, id string) (int, error) {
				// Resolve row first, then call typed Count.
				rows, _, err := spec.Fetch(ctx, ListOpts{Limit: spec.PageSize})
				if err != nil {
					return -1, err
				}
				for _, r := range rows {
					if extractID(spec, r) == id {
						return cnt(ctx, r)
					}
				}
				return -1, fmt.Errorf("row not found")
			}
		}
		if e.ResolveUpward != nil {
			res := e.ResolveUpward
			ae.resolveUpward = func(ctx context.Context, id string) (EntityRef, error) {
				rows, _, err := spec.Fetch(ctx, ListOpts{Limit: spec.PageSize})
				if err != nil {
					return EntityRef{}, err
				}
				for _, r := range rows {
					if extractID(spec, r) == id {
						return res(ctx, r)
					}
				}
				return EntityRef{}, fmt.Errorf("row not found")
			}
		}
		if e.ResolveDrill != nil {
			res := e.ResolveDrill
			ae.resolveDrill = func(ctx context.Context, id string) (EntityRefList, error) {
				rows, _, err := spec.Fetch(ctx, ListOpts{Limit: spec.PageSize})
				if err != nil {
					return EntityRefList{}, err
				}
				for _, r := range rows {
					if extractID(spec, r) == id {
						return res(ctx, r)
					}
				}
				return EntityRefList{}, fmt.Errorf("row not found")
			}
		}
		edges = append(edges, ae)
	}

	app.specs[spec.Kind] = &anySpec{
		kind:           spec.Kind,
		display:        spec.Display,
		group:          spec.Group,
		icon:           spec.Icon,
		pageSize:       spec.PageSize,
		multiSort:      spec.MultiSort,
		showEdgeCounts: spec.ShowEdgeCounts,
		defaultView:    spec.Default,
		fetch:       fetch,
		getOne:      getOne,
		columns:     columns,
		edges:       edges,
		formFields: spec.FormFields,
		update:     spec.Update,
		deleteRow:  spec.Delete,
	}
	app.kindOrder = append(app.kindOrder, spec.Kind)
}

// projectRow applies the spec's typed accessors to one row → Row.
// Generic: no hardcoded field assumption. Every column is a string;
// `JSON` carries the row's ent-native serialization (including
// eager-loaded `edges`) for the `J` clipboard shortcut.
//
// Hidden columns are still projected — the UI decides whether to
// render them, but downstream consumers (clipboard, future filters)
// can still see the value.
func projectRow[T any](spec EntitySpec[T], r T) Row {
	out := Row{
		ID:      extractID(spec, r),
		Columns: make(map[string]string, len(spec.Columns)),
	}
	for _, c := range spec.Columns {
		out.Columns[c.Key] = c.Get(r)
	}
	if spec.JSON != nil {
		if b, err := spec.JSON(r); err == nil {
			out.JSON = b
		}
	}
	return out
}

// rowLabel returns the best single-line label for a row — preferring
// common name-shaped columns, falling back to the id when none match.
// Schemas without any of these columns still get a usable display.
//
// "Common shapes" are convention, not requirement. The list is small
// and easy to extend without changing every caller.
func rowLabel(r Row) string {
	for _, k := range []string{"title", "name", "display_name", "label", "summary"} {
		if v := r.Columns[k]; v != "" {
			return v
		}
	}
	return r.ID
}

// isBodyColumnKey reports whether a column key looks like a long-prose
// field that should be rendered as the preview body rather than a
// one-line field. Convention-based; schemas without any of these still
// just get an empty body. No special-casing at the codegen layer.
func isBodyColumnKey(k string) bool {
	switch k {
	case "body", "description", "content":
		return true
	}
	return false
}

// extractID looks for a column keyed "id". Generated code always emits one.
// Falls back to empty string (display-only entities).
func extractID[T any](spec EntitySpec[T], r T) string {
	for _, c := range spec.Columns {
		if c.Key == "id" {
			return c.Get(r)
		}
	}
	return ""
}

// kindList returns registered kinds in registration order, grouped.
func (a *App) kindList() []*anySpec {
	out := make([]*anySpec, 0, len(a.specs))
	for _, k := range a.kindOrder {
		if s := a.specs[k]; s != nil {
			out = append(out, s)
		}
	}
	return out
}

// kindListSortedByDisplay returns kinds alphabetically by Display label.
// Used by the picker.
func (a *App) kindListSortedByDisplay() []*anySpec {
	out := a.kindList()
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].display < out[j].display
	})
	return out
}
