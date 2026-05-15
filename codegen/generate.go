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

var templates = template.Must(template.New("").ParseFS(tmplFS, "templates/*.tmpl"))

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
	// ScopeFields lists ent field names (snake_case, e.g. "project_id",
	// "tenant_id", "org_id") that should be wired as scope predicates in
	// the generated Fetch closures. For each scope field an entity has,
	// the closure reads opts.Scope["<field>"] and applies a predicate.
	// Entities without the field skip the predicate (still browsable, just
	// unscoped). Leave empty for a fully un-scoped generic install.
	ScopeFields []string
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
	Name             string // ent type name, e.g. "Post"
	Kind             string // url-safe lower-case kind id, e.g. "post"
	Display          string // pretty plural for the picker, e.g. "Posts"
	Group            string
	Icon             string
	PredPkg          string // predicate package name on disk, e.g. "post"
	PredAlias        string // import alias used in generated file, e.g. "entPost"
	// ScopeFields are the subset of Options.ScopeFields this entity
	// actually has on its schema. Template emits one predicate per entry.
	ScopeFields      []ScopeFieldMeta
	FilterPredicates []string // legacy substring predicates for ListOpts.Filter
	Columns          []FieldMeta
	Edges            []EdgeMeta
	NeedsFmt         bool
	NeedsStrings     bool // true when any enum FilterableField → strings.Split for In/NotIn
	NeedsSQL         bool // true when any sortable related column → ent dialect/sql import

	// WithLoaders is the list of edge Go names this entity eager-loads
	// during Fetch so related-table columns can render without N+1
	// queries. Populated for every unique edge whose target type has a
	// detectable title field (name / title / display_name / …).
	WithLoaders []string

	// RelatedImports holds the additional ent predicate-package imports
	// needed by related-column filter dispatch. One entry per unique
	// target type referenced by any RelatedColumn. Deduped + sorted.
	RelatedImports []ImportSpec

	// Phase C — schema annotation results.
	MultiSort      bool   // enttui.MultiSort() — default true
	DefaultView    string // enttui.DefaultView("list" | "table"); default ""
	PageSize       int    // enttui.PageSize(N); 0 → spec default (200)
	ShowEdgeCounts bool   // enttui.CountEdges() — enables count fetch in preview

	// Form support — driven by enttui.Editable() per-field +
	// AllowCreate / AllowDelete at the entity level.
	EditableFields []FieldMeta
	AllowCreate    bool
	AllowDelete    bool
	AllowBulkCopy  bool
	AllowExport    bool
	DetailEdges    []string // enttui.DetailEdge — master-detail child relation(s)

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
	Editable    bool // user-editable in the form modal (enttui.Editable())
	Required    bool // schema-side NotEmpty / non-nillable — surfaced to form
	Width       int
	Align       string
	ChipEntries []ChipEntry // sorted by key for deterministic output

	// EnumGoType is non-empty for enum fields and holds the generated Go
	// type name (e.g. "post.Status"). Used by the filter dispatch to cast
	// the FilterCondition.Value string into the typed enum.
	EnumGoType string

	// EnumGoTypeCast is the aliased reference used by the generated
	// form setters — e.g. `entPost.Status`. Pre-computed at extract
	// time so the setterExpr sub-template doesn't need EntityMeta in
	// scope.
	EnumGoTypeCast string

	// EnumValues holds the declared values for enum fields. Emitted into
	// the runtime Column literal so the condition builder can show a
	// value picker.
	EnumValues []string

	// Ref-picker metadata (RelatedColumn{Pick:true}). RefKind is the
	// registered kind of the target whose rows the picker lists.
	// GoName holds the host FK field's struct name (the setter target);
	// RefNillable says whether that FK column is clearable.
	RefKind     string
	RefNillable bool

	// Related fields render a column drawn from an eager-loaded edge
	// target instead of the row itself. EdgeGoName + TargetTitleField
	// drive the template's `r.Edges.<EdgeGoName>.<TargetTitleField>`
	// access. Kind stays "related" so valueExpr knows to nil-check the
	// edge pointer.
	EdgeGoName       string // e.g. "Author"
	TargetTitleField string // Go struct field on the target type, e.g. "Name"
	TargetTitleKind  string // kind of the target field — drives ptr-safety

	// Related-column filter-dispatch metadata. When set, the filter
	// template emits a case that wraps the target predicate in
	// pred.Has<Edge>With(...) so SQL stays valid:
	//
	//   q.Where(entPost.HasAuthorWith(entAuthor.NameContainsFold(v)))
	HasEdgeMethod   string // "HasAuthorWith"
	TargetPredAlias string // "entAuthor" — the import alias on the host file
}

// ChipEntry is one (value, tone) pair from an enttui.Chip annotation.
// Sorted by Value for deterministic template output.
type ChipEntry struct {
	Value string
	Tone  string
}

// ImportSpec is one aliased Go import emitted into the generated file —
// used for related-column filter dispatch which needs the target type's
// predicate package (e.g. `entAuthor "myproject/ent/author"`).
type ImportSpec struct {
	Alias string
	Path  string
}

// ScopeFieldMeta describes one (snake_case, GoName) pair for a scope
// field present on this entity. Key is the lookup key in opts.Scope;
// GoName is the predicate method on the entity's `*.Type` predicate
// package (e.g. ProjectID, TenantID).
type ScopeFieldMeta struct {
	Key    string
	GoName string
}

// EdgeMeta describes one ent edge for template emission.
type EdgeMeta struct {
	Name       string // edge name on the host, snake_case (e.g. "author")
	GoName     string // CamelCase Go method name (e.g. "Author" → client.Post.QueryAuthor)
	Display    string // "→ Authors" (upward) or "Comments" (drill)
	Kind       string // "EdgeUpward" or "EdgeDrill"
	Trigger    string // single-char key
	TargetKind string // target entity kind, lower-case (e.g. "author")
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
		Package:        opts.Package,
		EntPkgPath:     opts.EntPkgPath,
		Name:           n.Name,
		Kind:           strings.ToLower(n.Name),
		Display:        pluralize(n.Name),
		Group:          "data",
		Icon:           "•",
		PredPkg:        strings.ToLower(n.Name),
		PredAlias:      "ent" + n.Name,
		MultiSort:      true, // default true per ADR-103
		ShowEdgeCounts: true, // default on; opt out with enttui.NoEdgeCounts (TBD)
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
	if hasAnnot(n.Annotations, "EntTUI.CountEdges") {
		em.ShowEdgeCounts = true
	}
	if hasAnnot(n.Annotations, "EntTUI.AllowCreate") {
		em.AllowCreate = true
	}
	if hasAnnot(n.Annotations, "EntTUI.AllowBulkCopy") {
		em.AllowBulkCopy = true
	}
	if hasAnnot(n.Annotations, "EntTUI.AllowExport") {
		em.AllowExport = true
	}
	em.DetailEdges = annotDetailEdges(n.Annotations)
	if hasAnnot(n.Annotations, "EntTUI.AllowDelete") {
		em.AllowDelete = true
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
		fm.Editable = hasAnnot(f.Annotations, "EntTUI.Editable")
		// "Required" mirrors the schema's notion of must-be-set. ent
		// exposes this via field.Optional + field.Nillable; a field is
		// required when it's neither.
		fm.Required = !f.Optional && !f.Nillable && f.Name != "id"
		// Enum cast used by the generated form setter. e.g. "entPost.Status".
		if f.IsEnum() {
			fm.EnumGoTypeCast = em.PredAlias + "." + fm.GoName
		}
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

		// Generic scope match: if this field name is in opts.ScopeFields,
		// record the (key, GoName) pair so the template can emit the
		// predicate. No project-specific hardcoding — works for any
		// tenant_id / org_id / workspace_id convention.
		for _, sk := range opts.ScopeFields {
			if f.Name == sk {
				em.ScopeFields = append(em.ScopeFields, ScopeFieldMeta{
					Key:    sk,
					GoName: f.StructField(),
				})
				break
			}
		}

		// Time-typed fields are sortable by convention.
		if f.IsTime() && !fm.Sortable && !fm.Hidden {
			fm.Sortable = true
		}
		// Body-shaped fields (long prose) get hidden from the table by
		// default — they still live in r.Columns so the preview pane
		// (and the J clipboard shortcut) can render them as prose. No
		// hardcoded hero-field concept; just a hint that wide text
		// makes for bad table cells.
		if !fm.Hidden && isBodyFieldName(f.Name) {
			fm.Hidden = true
		}

		// Convention: every string + enum field is filterable by default
		// so the condition builder has plenty of columns to work with.
		// Codegen emits actual predicates only for string/stringPtr in v1;
		// enum filtering surfaces in the UI but falls through silently
		// until Phase F2 lands typed predicates. Opt out via enttui.Hidden().
		if !fm.Filterable && !fm.Hidden && (f.IsString() || f.IsEnum()) {
			fm.Filterable = true
		}
		// Convention: also mark every enum + string field as sortable by
		// default — feels natural for table columns. created_at is
		// already marked sortable above. Opt out via enttui.Hidden().
		if !fm.Sortable && !fm.Hidden && (f.IsString() || f.IsEnum() || f.IsTime()) {
			fm.Sortable = true
		}

		// Global `/` substring filter: OR a case-insensitive
		// ContainsFold across every string field the schema marked
		// Filterable — zero field-name hardcoding. Authors control
		// participation with enttui.Filterable() / enttui.Hidden()
		// (the same flags the condition builder honors). ContainsFold
		// only exists for string-typed predicates, so non-strings are
		// skipped here regardless of the flag.
		if fm.Filterable && f.IsString() {
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
		if fm.Editable {
			em.EditableFields = append(em.EditableFields, fm)
		}
	}
	if n.ID == nil {
		return em, false
	}

	// Skip entities that are clearly internal/shadow.
	if isInternal(n.Name) {
		return em, false
	}

	// Convention: every entity with an ID + not in the internal blocklist
	// + not passed via --skip is browsable. Scope predicates are emitted
	// only for fields the entity actually has (see em.ScopeFields above).

	// Edges.
	used := map[string]bool{}
	edgeByName := map[string]*gen.Edge{}
	for _, e := range n.Edges {
		targetKind, ok := kindByType[e.Type.Name]
		if !ok {
			continue
		}
		em.Edges = append(em.Edges, edgeMetaOf(e, targetKind, used))
		edgeByName[e.Name] = e
	}

	// Related columns — annotation-driven. The user lists which
	// (edge, field) projections they want; we look up the edge's target
	// type, validate the field exists, and emit a column. Eager-load
	// each referenced edge via With<Edge>() so reads are N-batched.
	importsSeen := map[string]bool{} // dedupe by alias
	for _, rc := range annotRelatedColumns(n.Annotations) {
		e, ok := edgeByName[rc.Edge]
		if !ok {
			continue // unknown edge — silently skip
		}
		var tf *gen.Field
		if e.Type.ID != nil && e.Type.ID.Name == rc.Field {
			tf = e.Type.ID
		} else {
			for _, f := range e.Type.Fields {
				if f.Name == rc.Field {
					tf = f
					break
				}
			}
		}
		if tf == nil {
			continue // unknown field on target — skip
		}
		tfm := fieldMetaOf(tf)
		label := rc.Label
		if label == "" {
			label = toLabel(rc.Edge) + " " + tfm.Label
		}

		// Predicate-package import for the target type (used by the
		// related-filter dispatch case). One alias per target type.
		targetAlias := "ent" + e.Type.Name
		targetPkg := strings.ToLower(e.Type.Name)
		if !importsSeen[targetAlias] && targetAlias != em.PredAlias {
			importsSeen[targetAlias] = true
			em.RelatedImports = append(em.RelatedImports, ImportSpec{
				Alias: targetAlias,
				Path:  opts.EntPkgPath + "/" + targetPkg,
			})
		}

		em.Columns = append(em.Columns, FieldMeta{
			Key:              rc.Edge + "_" + tfm.Key,
			Label:            label,
			Kind:             "related",
			Filterable:       true, // related columns ARE filterable via HasEdgeWith
			Sortable:         true, // related columns ARE sortable via ByEdgeField
			EdgeGoName:       e.StructField(),
			TargetTitleField: tfm.GoName,
			TargetTitleKind:  tfm.Kind,
			HasEdgeMethod:    "Has" + e.StructField() + "With",
			TargetPredAlias:  targetAlias,
			// Reuse GoName for the predicate method on the target type
			// (e.g. "Title" → entTasklist.TitleEQ / TitleContainsFold).
			GoName: tfm.GoName,
		})
		em.NeedsSQL = true

		// Pick → an editable reference. Resolve the host FK column
		// (the edge's relation column; fall back to "<edge>_id") and
		// emit a ref FormField so the form opens a target-row picker
		// and writes that FK instead of the user typing an opaque id.
		if rc.Pick {
			fkCol := ""
			if len(e.Rel.Columns) > 0 {
				fkCol = e.Rel.Columns[0]
			}
			if fkCol == "" {
				fkCol = rc.Edge + "_id"
			}
			var hf *gen.Field
			for _, f := range n.Fields {
				if f.Name == fkCol {
					hf = f
					break
				}
			}
			if hf != nil {
				if tk, ok := kindByType[e.Type.Name]; ok {
					hfm := fieldMetaOf(hf)
					em.EditableFields = append(em.EditableFields, FieldMeta{
						Key:         fkCol,
						Label:       label,
						Kind:        "ref",
						Required:    !hf.Optional && !hf.Nillable,
						GoName:      hfm.GoName, // host FK setter target
						RefKind:     tk,         // registered target kind
						RefNillable: hf.Optional || hf.Nillable,
					})
				}
			}
		}

		// Avoid duplicate WithX() calls if the user picks multiple
		// fields off the same edge.
		alreadyLoaded := false
		for _, w := range em.WithLoaders {
			if w == e.StructField() {
				alreadyLoaded = true
				break
			}
		}
		if !alreadyLoaded {
			em.WithLoaders = append(em.WithLoaders, e.StructField())
		}
	}

	// Sort imports for deterministic output.
	sort.SliceStable(em.RelatedImports, func(i, j int) bool {
		return em.RelatedImports[i].Alias < em.RelatedImports[j].Alias
	})

	// Re-derive Sortable/Filterable lists now that related columns have
	// been appended (the earlier per-field loop missed them since they
	// weren't in em.Columns yet at that point).
	for _, c := range em.Columns {
		if c.Kind != "related" {
			continue
		}
		if c.Filterable {
			em.FilterableFields = append(em.FilterableFields, c)
		}
		if c.Sortable {
			em.SortableFields = append(em.SortableFields, c)
		}
	}

	// Decide whether the generated file needs to import "fmt".
	em.NeedsFmt = columnsNeedFmt(em.Columns)

	// "strings" is needed when any filterable field is an enum — the
	// OpIn / OpNotIn dispatch splits a "|"-joined value.
	for _, f := range em.FilterableFields {
		if f.Kind == "enum" || f.Kind == "enumPtr" {
			em.NeedsStrings = true
			break
		}
	}

	return em, true
}

// isBodyFieldName recognizes the conventional "long prose" field names
// — these still become columns, but are Hidden in the table by default
// so they don't blow out row heights. Mirrors runtime's isBodyColumnKey.
func isBodyFieldName(n string) bool {
	switch n {
	case "body", "description", "content":
		return true
	}
	return false
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
	fm := FieldMeta{
		GoName: f.StructField(),
		Key:    f.Name,
		Label:  toLabel(f.Name),
		Kind:   fieldKind(f),
	}
	if f.IsEnum() {
		// gen.Field.EnumValues() returns the declared values in
		// schema order — fed verbatim into the runtime so the
		// condition builder's value picker mirrors the schema.
		fm.EnumValues = append([]string(nil), f.EnumValues()...)
	}
	return fm
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
	// Every scalar field becomes a column. Body-shaped fields stay
	// columns too — they're just marked Hidden so the table view skips
	// them by default while the preview pane still has access to the
	// value via r.Columns. Byte slabs + JSON fields drop out.
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
	} else {
		em.Kind = "EdgeDrill"
		em.Display = pluralize(e.Type.Name)
	}
	// Every edge gets a single-char letter trigger — no magic "first
	// drill edge claims enter". With multiple drill edges (TaskList has
	// tasks + subtasks + comments) the implicit primary was confusing.
	// Enter always means "open preview"; drill via the visible letter.
	em.Trigger = pickTrigger(e.Name, used)
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
