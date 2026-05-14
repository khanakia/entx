package runtime

import (
	"context"
	"fmt"
	"sort"
	"time"
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

	pageSize    int
	defaultView DefaultView

	// fetch returns the row IDs + their typed display data.
	fetch func(ctx context.Context, opts ListOpts) ([]Row, int, error)
	// getOne returns a single row by ID (used by edge drill / upward jumps).
	getOne func(ctx context.Context, id string) (*Row, error)

	columns []anyColumn
	edges   []anyEdge
}

type anyColumn struct {
	key    string
	label  string
	chip   map[string]string
	hidden bool
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

// Row is the runtime-visible projection of one ent row. Strings only —
// every typed accessor in the spec has been applied at fetch time.
type Row struct {
	ID        string
	Title     string
	Body      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
	// Columns map: Column.Key → formatted value.
	Columns map[string]string
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
		kind:        spec.Kind,
		display:     spec.Display,
		group:       spec.Group,
		icon:        spec.Icon,
		pageSize:    spec.PageSize,
		defaultView: spec.Default,
		fetch:       fetch,
		getOne:      getOne,
		columns:     columns,
		edges:       edges,
	}
	app.kindOrder = append(app.kindOrder, spec.Kind)
}

// projectRow applies the spec's typed accessors to one row → Row.
func projectRow[T any](spec EntitySpec[T], r T) Row {
	out := Row{
		ID:      extractID(spec, r),
		Columns: make(map[string]string, len(spec.Columns)),
	}
	if spec.Title != nil {
		out.Title = spec.Title(r)
	}
	if spec.Body != nil {
		out.Body = spec.Body(r)
	}
	if spec.Status != nil {
		out.Status = spec.Status(r)
	}
	if spec.CreatedAt != nil {
		out.CreatedAt = spec.CreatedAt(r)
	}
	if spec.UpdatedAt != nil {
		out.UpdatedAt = spec.UpdatedAt(r)
	}
	for _, c := range spec.Columns {
		if c.Hidden {
			continue
		}
		out.Columns[c.Key] = c.Get(r)
	}
	return out
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
