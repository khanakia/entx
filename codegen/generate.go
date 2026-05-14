// Package gen produces enttui registrations from ent schemas.
//
// Pipeline:
//  1. entc.LoadGraph parses ./schema into an in-memory gen.Graph.
//  2. We walk graph.Nodes, applying convention rules (and, later, enttui
//     annotations) to pick browsable entities + their display / sort /
//     filter / edge metadata.
//  3. We render two templates per run: one register_<Name>.go per entity,
//     plus a register_all.go aggregator.
//  4. Output is gofmt'd and written under outDir.
//
// The runtime side (enttui/runtime) is hand-written and pre-existing —
// codegen only emits the thin glue layer that wires *ent.X structs into
// the runtime registry.
package codegen

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

//go:embed templates/*.tmpl
var tmplFS embed.FS

var templates = template.Must(template.New("").
	Funcs(template.FuncMap{
		"title": strings.Title, //nolint:staticcheck
	}).
	ParseFS(tmplFS, "templates/*.tmpl"))

// Options configure a generator run.
type Options struct {
	// SchemaPath is the path to the ent schema directory (e.g. "./schema").
	SchemaPath string
	// OutDir is where generated *.go files are written.
	OutDir string
	// Package is the package name to declare at the top of generated files.
	Package string
	// EntPkgPath is the import path of the generated ent package
	// (e.g. "dbent/gen/ent").
	EntPkgPath string
	// Skip excludes specific entity Names (ent type names) from generation.
	Skip map[string]bool
}

// EntityMeta is the per-entity context passed to the entity template.
//
// NeedsFmt is set if any field accessor (title/body/column) renders via
// fmt.Sprintf — the template uses it to gate the "fmt" import.
//
// MultiSort / DefaultView / PageSize / NeedsRuntime are populated from
// schema-level annotations during extraction.
type EntityMeta struct {
	Package          string
	EntPkgPath       string
	Name             string // Task
	Kind             string // task (snake_case kind id)
	Display          string // Tasks
	Group            string
	Icon             string
	PredPkg          string // task (lowercase, package-name for predicates)
	PredAlias        string // entTask (import alias)
	HasProjectID     bool
	HasCreated       bool
	HasUpdated       bool
	FilterPredicates []string // legacy substring predicates for ListOpts.Filter
	TitleField       *FieldMeta
	BodyField        *FieldMeta
	StatusField      *FieldMeta
	Columns          []FieldMeta
	Edges            []EdgeMeta
	NeedsFmt         bool

	// Phase C — schema annotation results.
	MultiSort   bool   // enttui.MultiSort() — default true
	DefaultView string // enttui.DefaultView("list" | "table"); default ""
	PageSize    int    // enttui.PageSize(N); 0 → spec default (200)

	// Phase C/D — fields the user marked Sortable() (in declaration order).
	SortableFields []FieldMeta
	// Phase C/E — fields the user marked Filterable().
	FilterableFields []FieldMeta
}

// FieldMeta describes one ent field for template emission.
//
// Sortable / Filterable / Hidden / Chip / Width / Align are populated from
// field-level annotations during extraction.
type FieldMeta struct {
	GoName string // Title
	Key    string // title (snake_case)
	Label  string // Title
	// Kind discriminates how the template renders the value:
	//   "string", "stringPtr", "enum", "enumPtr", "time", "timePtr",
	//   "scalar", "scalarPtr"
	Kind string

	// Phase C — annotation results.
	Sortable    bool
	Filterable  bool
	Hidden      bool
	Width       int
	Align       string
	ChipEntries []ChipEntry // sorted by key for deterministic output

	// EnumGoType is non-empty for enum fields and holds the generated Go
	// type name (e.g. "task.Status"). Used by the filter dispatch to cast
	// the FilterCondition.Value string into the typed enum.
	EnumGoType string
}

// ChipEntry is one (value, tone) pair from an enttui.Chip annotation.
// Sorted by Value for deterministic template output.
type ChipEntry struct {
	Value string
	Tone  string
}

// EdgeMeta describes one ent edge for template emission.
type EdgeMeta struct {
	Name       string // tasklist (snake_case)
	GoName     string // Tasklist (used by client.X.QueryTasklist)
	Display    string // → TaskList  or  Tasks
	Kind       string // "EdgeUpward" or "EdgeDrill"
	Trigger    string // single-char key (or "enter")
	TargetKind string // tasklist
}

// RegisterAllData is passed to the register_all template.
type RegisterAllData struct {
	Package    string
	EntPkgPath string
	Entities   []EntityMeta
}

// Generate runs the full pipeline once.
func Generate(opts Options) error {
	if opts.SchemaPath == "" {
		return fmt.Errorf("enttui/codegen: SchemaPath is required")
	}
	if opts.OutDir == "" {
		return fmt.Errorf("enttui/codegen: OutDir is required")
	}
	if opts.Package == "" {
		opts.Package = "enttui"
	}
	if opts.EntPkgPath == "" {
		opts.EntPkgPath = "dbent/gen/ent"
	}
	if opts.Skip == nil {
		opts.Skip = map[string]bool{}
	}

	graph, err := entc.LoadGraph(opts.SchemaPath, &gen.Config{
		Target:  filepath.Join(os.TempDir(), "enttui-load-stub"),
		Package: "stub",
	})
	if err != nil {
		return fmt.Errorf("enttui/codegen: load schema: %w", err)
	}

	// Map ent Type → kind name so edges can resolve target kinds.
	kindByType := make(map[string]string, len(graph.Nodes))
	for _, n := range graph.Nodes {
		kindByType[n.Name] = strings.ToLower(n.Name)
	}

	entities := make([]EntityMeta, 0, len(graph.Nodes))
	for _, n := range graph.Nodes {
		if opts.Skip[n.Name] {
			continue
		}
		em, ok := extractEntity(n, opts, kindByType)
		if !ok {
			continue
		}
		entities = append(entities, em)
	}
	// Deterministic order for stable diffs.
	sort.SliceStable(entities, func(i, j int) bool { return entities[i].Name < entities[j].Name })

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return err
	}

	for _, e := range entities {
		path := filepath.Join(opts.OutDir, "register_"+strings.ToLower(e.Name)+".go")
		if err := renderToFile("entity.tmpl", e, path); err != nil {
			return fmt.Errorf("render %s: %w", e.Name, err)
		}
	}

	aggPath := filepath.Join(opts.OutDir, "register_all.go")
	if err := renderToFile("register_all.tmpl", RegisterAllData{
		Package:    opts.Package,
		EntPkgPath: opts.EntPkgPath,
		Entities:   entities,
	}, aggPath); err != nil {
		return fmt.Errorf("render register_all: %w", err)
	}

	return nil
}

func extractEntity(n *gen.Type, opts Options, kindByType map[string]string) (EntityMeta, bool) {
	em := EntityMeta{
		Package:    opts.Package,
		EntPkgPath: opts.EntPkgPath,
		Name:       n.Name,
		Kind:       strings.ToLower(n.Name),
		Display:    pluralize(n.Name),
		Group:      "data",
		Icon:       "•",
		PredPkg:    strings.ToLower(n.Name),
		PredAlias:  "ent" + n.Name,
		MultiSort:  true, // default true per ADR-103
	}

	// --- Schema-level annotations (enttui.Display, Group, Icon, …) ---
	if s, ok := annotString(n.Annotations, "EntTUI.Display", "Value"); ok {
		em.Display = s
	}
	if s, ok := annotString(n.Annotations, "EntTUI.Group", "Value"); ok {
		em.Group = s
	}
	if s, ok := annotString(n.Annotations, "EntTUI.Icon", "Value"); ok {
		em.Icon = s
	}
	if n, ok := annotInt(n.Annotations, "EntTUI.PageSize", "Value"); ok {
		em.PageSize = n
	}
	if s, ok := annotString(n.Annotations, "EntTUI.DefaultView", "Value"); ok {
		em.DefaultView = s
	}

	// Iterate ID field separately — gen.Type stores it on .ID, not in .Fields.
	allFields := []*gen.Field{}
	if n.ID != nil {
		allFields = append(allFields, n.ID)
	}
	allFields = append(allFields, n.Fields...)
	for _, f := range allFields {
		fm := fieldMetaOf(f)

		// --- Field-level annotation reads (Phase C) ---
		fm.Sortable = hasAnnot(f.Annotations, "EntTUI.Sortable")
		fm.Filterable = hasAnnot(f.Annotations, "EntTUI.Filterable")
		fm.Hidden = hasAnnot(f.Annotations, "EntTUI.Hidden")
		if w, ok := annotInt(f.Annotations, "EntTUI.Width", "Value"); ok {
			fm.Width = w
		}
		if a, ok := annotString(f.Annotations, "EntTUI.Align", "Value"); ok {
			fm.Align = a
		}
		if entries, ok := annotStringMap(f.Annotations, "EntTUI.Chip", "Tones"); ok {
			for _, kv := range entries {
				fm.ChipEntries = append(fm.ChipEntries, ChipEntry{Value: kv.K, Tone: kv.V})
			}
		}

		// AsTitle / AsBody / AsStatus annotations override the
		// convention-based hero-field detection below.
		if hasAnnot(f.Annotations, "EntTUI.AsTitle") {
			m := fm
			em.TitleField = &m
		}
		if hasAnnot(f.Annotations, "EntTUI.AsBody") {
			m := fm
			em.BodyField = &m
		}
		if hasAnnot(f.Annotations, "EntTUI.AsStatus") {
			m := fm
			em.StatusField = &m
		}

		switch f.Name {
		case "id":
			// id is always rendered via the columns block too.
		case "project_id":
			em.HasProjectID = true
		case "created_at":
			em.HasCreated = true
			if !fm.Sortable {
				// created_at is sortable by convention even without annotation.
				fm.Sortable = true
			}
		case "updated_at":
			em.HasUpdated = true
		case "title", "name":
			if em.TitleField == nil {
				m := fm
				em.TitleField = &m
			}
		case "body", "description", "content":
			if em.BodyField == nil {
				m := fm
				em.BodyField = &m
			}
		case "status", "severity", "kind", "state":
			if em.StatusField == nil && f.IsEnum() {
				m := fm
				em.StatusField = &m
			}
		}

		// Convention: title-ish + body-ish string fields are filterable
		// even without the explicit annotation. Keeps existing behavior.
		if !fm.Filterable && f.IsString() &&
			(f.Name == "title" || f.Name == "name" || f.Name == "body" || f.Name == "description") {
			fm.Filterable = true
		}

		// Legacy substring predicates list (still used for the global `/`
		// filter in the browser view; Phase E uses structured filters).
		if (f.Name == "title" || f.Name == "name" || f.Name == "body" || f.Name == "description") && f.IsString() {
			em.FilterPredicates = append(em.FilterPredicates, fm.GoName+"ContainsFold")
		}

		// Columns: skip body (rendered as preview body, not column),
		// always include id, time fields, and meaningful scalars. Hidden
		// fields stay in the slice but carry the .Hidden flag so the UI
		// can re-show them on demand.
		if shouldRenderAsColumn(f) {
			em.Columns = append(em.Columns, fm)
		}
		// Sortable / filterable indexes (used by the multi-sort dispatch
		// + the condition builder operator menu).
		if fm.Sortable {
			em.SortableFields = append(em.SortableFields, fm)
		}
		if fm.Filterable {
			em.FilterableFields = append(em.FilterableFields, fm)
		}
	}
	if n.ID == nil {
		return em, false
	}

	// Skip entities that are clearly internal/shadow.
	if isInternal(n.Name) {
		return em, false
	}

	// Convention: only emit entities scoped to a project for the v1 POC.
	if !em.HasProjectID {
		return em, false
	}

	// Edges.
	used := map[string]bool{}
	for _, e := range n.Edges {
		targetKind, ok := kindByType[e.Type.Name]
		if !ok {
			continue
		}
		em.Edges = append(em.Edges, edgeMetaOf(e, targetKind, used))
	}

	// Decide whether the generated file needs to import "fmt".
	em.NeedsFmt = fieldNeedsFmt(em.TitleField) ||
		fieldNeedsFmt(em.BodyField) ||
		columnsNeedFmt(em.Columns)

	return em, true
}

func fieldNeedsFmt(f *FieldMeta) bool {
	if f == nil {
		return false
	}
	return f.Kind == "scalar" || f.Kind == "scalarPtr"
}

func columnsNeedFmt(cols []FieldMeta) bool {
	for _, c := range cols {
		if c.Kind == "scalar" || c.Kind == "scalarPtr" {
			return true
		}
	}
	return false
}

func fieldMetaOf(f *gen.Field) FieldMeta {
	return FieldMeta{
		GoName: f.StructField(),
		Key:    f.Name,
		Label:  toLabel(f.Name),
		Kind:   fieldKind(f),
	}
}

// fieldKind reduces a gen.Field to a string bucket the template switches
// on. Keeps the template free of any complex type logic.
//
// Buckets: time, enum, enumPtr, string, stringPtr, scalar, scalarPtr.
func fieldKind(f *gen.Field) string {
	ptr := f.Nillable
	switch {
	case f.IsTime():
		if ptr {
			return "timePtr"
		}
		return "time"
	case f.IsEnum():
		if ptr {
			return "enumPtr"
		}
		return "enum"
	case f.IsString():
		if ptr {
			return "stringPtr"
		}
		return "string"
	default:
		if ptr {
			return "scalarPtr"
		}
		return "scalar"
	}
}

func shouldRenderAsColumn(f *gen.Field) bool {
	// Render basically every scalar in declaration order, except the body
	// (handled separately) and any JSON/byte slabs.
	switch f.Name {
	case "body", "description", "content":
		return false
	}
	if f.IsBytes() || f.IsJSON() {
		return false
	}
	return true
}

func edgeMetaOf(e *gen.Edge, targetKind string, used map[string]bool) EdgeMeta {
	em := EdgeMeta{
		Name:       e.Name,
		GoName:     e.StructField(), // ent's canonical Go method name (CamelCase, no underscores)
		TargetKind: targetKind,
	}
	if e.Unique {
		em.Kind = "EdgeUpward"
		em.Display = "→ " + pluralize(e.Type.Name)
		em.Trigger = pickTrigger(e.Name, used)
	} else {
		em.Kind = "EdgeDrill"
		em.Display = pluralize(e.Type.Name)
		em.Trigger = pickTrigger(e.Name, used)
		// First non-unique edge can claim "enter".
		if !used["enter"] {
			em.Trigger = "enter"
			used["enter"] = true
		}
	}
	return em
}

// pickTrigger walks an edge name looking for an unused single character,
// avoiding reserved keys (k, q, /, s, r, enter, esc).
func pickTrigger(name string, used map[string]bool) string {
	reserved := map[string]bool{
		"k": true, "q": true, "s": true, "r": true, "h": true, "j": true, "l": true,
	}
	for _, r := range strings.ToLower(name) {
		if r < 'a' || r > 'z' {
			continue
		}
		key := string(r)
		if reserved[key] || used[key] {
			continue
		}
		used[key] = true
		return key
	}
	// Fallback: a-z exhaust.
	for r := 'a'; r <= 'z'; r++ {
		key := string(r)
		if reserved[key] || used[key] {
			continue
		}
		used[key] = true
		return key
	}
	return "?"
}

func isInternal(name string) bool {
	low := strings.ToLower(name)
	switch low {
	case "schemamigration", "auditlog", "querylog", "piipattern":
		return true
	}
	return strings.HasSuffix(low, "_fts") || strings.Contains(low, "shadow")
}

func toLabel(snake string) string {
	parts := strings.Split(snake, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// pluralize is a deliberately dumb English pluralizer for display labels.
// Caller can override per-entity via annotations later.
func pluralize(name string) string {
	if strings.HasSuffix(name, "s") {
		return name
	}
	if strings.HasSuffix(name, "y") {
		return name[:len(name)-1] + "ies"
	}
	return name + "s"
}

func renderToFile(tmplName string, data any, path string) error {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, tmplName, data); err != nil {
		return err
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// Write the unformatted source anyway so the user can see the
		// template output that failed to parse.
		_ = os.WriteFile(path+".unformatted", buf.Bytes(), 0o644)
		return fmt.Errorf("gofmt: %w (raw written to %s.unformatted)", err, path)
	}
	return os.WriteFile(path, formatted, 0o644)
}
