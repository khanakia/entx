// generate.go — phase 3 of the codegen pipeline (the "render the sidecar"
// pass). Reads polyState (built by preprocess.go), transforms it into the
// flat tmplData shape the template iterates over, executes the embedded
// template, runs go/format over the result, and atomically writes the
// formatted bytes to <target>/polymorphic.go.
//
// Notes:
//
//   - Adding a new emission to the template is a four-step change:
//     (1) add a field to tmplData, (2) populate it in buildTmplData,
//     (3) reference it in templates/polymorphic.tmpl, (4) test the
//     output in examples/basic/runtime_test.go. See doc.go for the
//     full walk-through.
//
//   - Sorting is load-bearing. Every slice we populate from a map is
//     sorted before append so two `go generate` runs against the same
//     schema produce byte-identical output. Never iterate a Go map
//     directly when building tmplData.
//
//   - tmplData.Package is the LAST segment of gen.Config.Package (the
//     import path). Both the bare name (for `package X`) and the full
//     import path (for sub-package imports) are needed; we keep them
//     separate fields rather than re-deriving inside the template.
//
//   - go/format failures are template bugs, not graph bugs. When they
//     happen we still write the unformatted bytes to disk so the
//     developer can `cat polymorphic.go` and see what the template
//     produced. Do not skip the write on format error.
package entpoly

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path"
	"path/filepath"
	"sort"

	"entgo.io/ent/entc/gen"
)

// generate emits the polymorphic.go sidecar file from the state captured
// during preprocess. The flow is:
//
//  1. Build the tmplData by transforming polyState slices into the
//     template-ready shapes.
//  2. Execute the embedded template into a buffer.
//  3. Run go/format over the buffer; on failure, write the raw bytes so
//     the developer can inspect what went wrong.
//  4. Atomically write the formatted result to <target>/polymorphic.go.
//
// generate is a no-op when preprocess recorded no polymorphic participants
// — emitting an empty sidecar would force callers to add a .gitignore
// entry for a file they never asked for.
func (e *Extension) generate(g *gen.Graph) error {
	if e.state == nil || !e.state.hasParticipants() {
		return nil
	}

	data, err := e.buildTmplData()
	if err != nil {
		return fmt.Errorf("entpoly: build template data: %w", err)
	}

	var buf bytes.Buffer
	if err := polyTmpl.ExecuteTemplate(&buf, "file", data); err != nil {
		return fmt.Errorf("entpoly: execute template: %w", err)
	}

	outPath := filepath.Join(g.Config.Target, "polymorphic.go")
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// Format failed → write the raw bytes so the developer can see
		// what the template produced. The format failure is almost
		// always a template bug, not a graph problem.
		_ = os.WriteFile(outPath, buf.Bytes(), 0o644)
		return fmt.Errorf("entpoly: format generated code: %w", err)
	}
	return os.WriteFile(outPath, formatted, 0o644)
}

// hasParticipants reports whether the state captured any polymorphic
// declarations during preprocess. Used by generate() to short-circuit
// when the schema declared no polymorphism at all.
func (s *polyState) hasParticipants() bool {
	return len(s.Children) > 0 || len(s.Parents) > 0 || len(s.Holders) > 0
}

// buildTmplData transforms polyState (the raw recording of preprocess
// output) into tmplData (the shape the template iterates over). All
// per-entry strings are precomputed here so the template stays free of
// case-conversion / concatenation.
func (e *Extension) buildTmplData() (*tmplData, error) {
	s := e.state

	// Stable morph map for the rendered map literal. Sort the keys so
	// two codegen runs against an identical schema produce byte-
	// identical output (the deterministic-codegen contract).
	keys := make([]string, 0, len(s.MorphMap))
	for k := range s.MorphMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// gen.Config.Package is the full import path (e.g.
	// "github.com/org/proj/ent"); the file's `package X` declaration
	// needs only the last segment. We also keep the full path so the
	// template can emit imports for predicate sub-packages.
	d := &tmplData{
		Package:    path.Base(s.Package),
		ImportPath: s.Package,
	}
	for _, k := range keys {
		d.MorphMap = append(d.MorphMap, morphMapEntry{Key: k, Type: s.MorphMap[k]})
	}

	// Children: precompute PascalCase relation prefix and the capitalised
	// id/type field names that match ent's field-method emission.
	sortedChildren := append([]childInfo(nil), s.Children...)
	sort.Slice(sortedChildren, func(i, j int) bool {
		return sortedChildren[i].TypeName < sortedChildren[j].TypeName
	})
	for _, c := range sortedChildren {
		softDelFieldCap := pascalGoFieldName(c.SoftDeleteField)
		cases := make([]resolveCaseData, 0, len(c.ResolveTargets))
		for _, rt := range c.ResolveTargets {
			cases = append(cases, resolveCaseData{
				Type:               rt.SchemaName,
				IDent:              lower(rt.SchemaName),
				MorphConst:         rt.SchemaName + "MorphKey",
				IDGoType:           rt.IDGoType,
				HasSoftDelete:      rt.HasSoftDelete,
				SoftDeleteFieldCap: softDelFieldCap,
			})
		}
		touchField := c.TouchField
		if touchField == "" {
			touchField = "updated_at"
		}
		d.Children = append(d.Children, childData{
			Type:          c.TypeName,
			IDent:         lower(c.TypeName),
			Relation:      c.MorphName,
			RelationCap:   pascalCase(c.MorphName),
			IDField:       pascalGoFieldName(c.IDColumn),
			TypeField:     pascalGoFieldName(c.TypeColumn),
			IDIsInt:       c.IDType == "int",
			Required:      c.Required,
			Touch:         c.Touch,
			TouchField:    touchField,
			TouchFieldCap: pascalCase(touchField),
			Cascade:       c.Cascade,
			ChildIDGoType: c.ChildIDGoType,
			ChildPlural:   c.TypeName + "s",
			AllowedTypes:  c.AllowedTypes,
			ResolveCases:  cases,
		})
	}

	// Parents: precompute target idents + column field names so the
	// template can emit typed back-ref query methods. Look up the
	// matching child to get the actual column overrides — the parent's
	// MorphOne/MorphMany may have its own IDColumn/TypeColumn overrides
	// or fall back to the morph-name defaults.
	sortedParents := append([]parentInfo(nil), s.Parents...)
	sort.Slice(sortedParents, func(i, j int) bool {
		if sortedParents[i].ParentName != sortedParents[j].ParentName {
			return sortedParents[i].ParentName < sortedParents[j].ParentName
		}
		return sortedParents[i].FieldName < sortedParents[j].FieldName
	})
	imports := map[string]struct{}{}
	for _, c := range d.Children {
		imports[c.IDent] = struct{}{}
		// Resolver / eager-load both reference each allowed parent's
		// predicate sub-package (e.g. `document.IDIn(...)`), so every
		// case's IDent must end up in the import set.
		for _, rc := range c.ResolveCases {
			imports[rc.IDent] = struct{}{}
		}
	}
	for _, p := range sortedParents {
		idCol := p.IDColumn
		if idCol == "" {
			idCol = p.MorphName + "_id"
		}
		typeCol := p.TypeColumn
		if typeCol == "" {
			typeCol = p.MorphName + "_type"
		}
		targetIDent := lower(p.Target)
		imports[targetIDent] = struct{}{}
		d.Parents = append(d.Parents, parentData{
			ParentName:  p.ParentName,
			FieldName:   p.FieldName,
			FieldCap:    pascalCase(p.FieldName),
			Target:      p.Target,
			TargetIDent: targetIDent,
			MorphName:   p.MorphName,
			IDField:     pascalGoFieldName(idCol),
			TypeField:   pascalGoFieldName(typeCol),
			Kind:        p.Kind,
		})
	}
	// Holders: precompute pivot column-method names so the template can
	// emit typed M2M back-ref methods without string manipulation.
	sortedHolders := append([]holderInfo(nil), s.Holders...)
	sort.Slice(sortedHolders, func(i, j int) bool {
		if sortedHolders[i].HolderName != sortedHolders[j].HolderName {
			return sortedHolders[i].HolderName < sortedHolders[j].HolderName
		}
		return sortedHolders[i].FieldName < sortedHolders[j].FieldName
	})
	for _, h := range sortedHolders {
		idCol := h.IDColumn
		if idCol == "" {
			idCol = h.MorphName + "_id"
		}
		typeCol := h.TypeColumn
		if typeCol == "" {
			typeCol = h.MorphName + "_type"
		}
		targetIDent := lower(h.Target)
		pivotIDent := lower(h.Pivot)
		// Convention: the pivot's FK to the holder is "<holder>_id" in
		// snake_case (e.g. Tag holder → "tag_id" column). Could be
		// configurable in a future option.
		holderFK := lower(h.HolderName) + "_id"
		imports[targetIDent] = struct{}{}
		imports[pivotIDent] = struct{}{}
		holderIDent := lower(h.HolderName)
		imports[holderIDent] = struct{}{}
		d.Holders = append(d.Holders, holderData{
			HolderName:      h.HolderName,
			HolderIDent:     holderIDent,
			HolderIDGoType:  h.HolderIDGoType,
			FieldName:       h.FieldName,
			FieldCap:        pascalCase(h.FieldName),
			InverseFieldCap: pascalCase(h.InverseFieldName),
			Target:          h.Target,
			TargetIDent:     targetIDent,
			TargetIDGoType:  h.TargetIDGoType,
			TargetMorph:     h.Target + "MorphKey",
			Pivot:           h.Pivot,
			PivotIDent:      pivotIDent,
			HolderFKField:   pascalGoFieldName(holderFK),
			PivotIDField:    pascalGoFieldName(idCol),
			PivotTypeField:  pascalGoFieldName(typeCol),
		})
	}

	d.SubpackageImports = make([]string, 0, len(imports))
	for k := range imports {
		d.SubpackageImports = append(d.SubpackageImports, k)
	}
	sort.Strings(d.SubpackageImports)

	// Pre-filter the required- and touched-children lists once so the
	// template can iterate them without re-checking the flags.
	for _, c := range d.Children {
		if c.Required {
			d.RequiredChildren = append(d.RequiredChildren, c)
		}
		if c.Touch {
			d.TouchedChildren = append(d.TouchedChildren, c)
		}
		if c.Cascade {
			d.CascadedChildren = append(d.CascadedChildren, c)
		}
	}
	d.HasRequired = len(d.RequiredChildren) > 0
	d.HasTouch = len(d.TouchedChildren) > 0
	d.HasCascade = len(d.CascadedChildren) > 0
	d.HasPolyHooks = d.HasRequired || d.HasTouch || d.HasCascade

	// Collect non-builtin ID-type import paths so the generated file
	// can import "github.com/google/uuid" (etc) for UUID-typed PKs.
	// Deduplicate via a set since the same package may appear on
	// multiple targets.
	extra := map[string]struct{}{}
	for _, c := range s.Children {
		if c.ChildIDPkgPath != "" {
			extra[c.ChildIDPkgPath] = struct{}{}
		}
		for _, rt := range c.ResolveTargets {
			if rt.IDPkgPath != "" {
				extra[rt.IDPkgPath] = struct{}{}
			}
		}
	}
	for _, h := range s.Holders {
		if h.TargetIDPkgPath != "" {
			extra[h.TargetIDPkgPath] = struct{}{}
		}
		if h.HolderIDPkgPath != "" {
			extra[h.HolderIDPkgPath] = struct{}{}
		}
	}
	d.ExtraImports = make([]string, 0, len(extra))
	for p := range extra {
		d.ExtraImports = append(d.ExtraImports, p)
	}
	sort.Strings(d.ExtraImports)

	return d, nil
}

// pascalGoFieldName converts a snake_case column name (e.g. "commentable_id")
// to the matching Go-side capitalised field name ent generates from it
// (e.g. "CommentableID"). The trailing "_id" suffix turns into "ID" because
// ent emits ID in all caps when it is the suffix of a column name.
func pascalGoFieldName(col string) string {
	// Normalise IDs at the tail: "_id" → "ID", anything else gets the
	// straight pascalCase treatment.
	switch {
	case len(col) >= 3 && col[len(col)-3:] == "_id":
		return pascalCase(col[:len(col)-3]) + "ID"
	default:
		return pascalCase(col)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Template data structures
// ──────────────────────────────────────────────────────────────────────────

// tmplData is the root template input. buildTmplData() produces one of
// these per codegen pass; the template renders it into a single Go source
// file.
type tmplData struct {
	// Package is the target Go package name (e.g. "ent"). Matches the
	// package the rest of ent's generated code lives in.
	Package string

	// ImportPath is the full import path of the target package (e.g.
	// "github.com/org/proj/ent"). Used by the template to import the
	// predicate + per-child sub-packages for typed predicate
	// constructors.
	ImportPath string

	// MorphMap is the effective morph map. Sorted by Key for stable
	// output.
	MorphMap []morphMapEntry

	// Children carries one entry per MorphTo declaration. Drives the
	// Set/Clear builder method emission and the typed predicate
	// constructors.
	Children []childData

	// Parents carries one entry per MorphOne / MorphMany declaration.
	// Drives the typed back-ref method emission on the parent entity
	// (e.g. post.QueryFeaturedImage(ctx) → *Image).
	Parents []parentData

	// Holders carries one entry per MorphedByMany declaration. Drives
	// the typed M2M back-ref method emission on the holder entity
	// (e.g. tag.QueryPosts(ctx) → []*Post via the Taggable pivot).
	Holders []holderData

	// SubpackageImports is the deduplicated set of `<ident>` strings
	// referenced by the typed back-ref methods. Used by the template
	// to emit the right import lines.
	SubpackageImports []string

	// ExtraImports holds the deduplicated import paths for any
	// non-builtin Go-typed ID encountered across the graph (typically
	// "github.com/google/uuid" for uuid.UUID PKs). Emitted in the
	// import block so the generated polymorphic.go compiles when the
	// child / parent / holder uses a custom PK type.
	ExtraImports []string

	// HasRequired is true when at least one child declared Required().
	// Drives the emission of the RegisterPolyHooks function and the
	// `errors` import needed by the runtime hooks.
	HasRequired bool

	// HasTouch is true when at least one child declared Touch(). Used
	// alongside HasRequired to gate the time / errors import emission
	// and the RegisterPolyHooks function itself.
	HasTouch bool

	// HasCascade is true when at least one child declared Cascade().
	HasCascade bool

	// RequiredChildren is the subset of Children that need a Required
	// hook. Lifted here so the template can iterate without filtering.
	RequiredChildren []childData

	// TouchedChildren is the subset of Children that need a Touch hook.
	TouchedChildren []childData

	// CascadedChildren is the subset of Children that need a per-parent
	// pre-delete cascade hook.
	CascadedChildren []childData

	// HasPolyHooks is the OR of HasRequired / HasTouch / HasCascade —
	// RegisterPolyHooks is emitted when any of them is true.
	HasPolyHooks bool
}

// morphMapEntry is one row of the rendered morph map literal.
type morphMapEntry struct {
	Key  string
	Type string
}

// holderData drives the typed M2M back-ref method emission on a holder
// entity (e.g. tag.QueryPosts(ctx) []*Post). The method emits a two-step
// query under the hood: first read pivot rows that match holder.ID +
// target morph-key, then load the target rows by id.
type holderData struct {
	HolderName       string // Holder schema name (e.g. "Tag").
	HolderIDent      string // Holder's lowercase predicate-package name.
	HolderIDGoType   string // Holder's ID Go type ("int", "int64", "string").
	FieldName        string // Back-ref method name on holder (e.g. "posts").
	FieldCap         string // PascalCase of FieldName (e.g. "Posts").
	InverseFieldCap  string // PascalCase of the inverse method name on target (e.g. "Tags").
	Target           string // Concrete parent schema name (e.g. "Post").
	TargetIDent      string // Target's lowercase predicate-package name.
	TargetIDGoType   string // Target's ID Go type ("int", "int64", "string").
	TargetMorph      string // Morph-key constant for the target (e.g. "PostMorphKey").
	Pivot            string // Pivot schema name (e.g. "Taggable").
	PivotIDent       string // Pivot's lowercase predicate-package name.
	HolderFKField    string // Pivot's column-method name for the holder FK (e.g. "TagID").
	PivotIDField     string // Pivot's morph-id column method name (e.g. "TaggableID").
	PivotTypeField   string // Pivot's morph-type column method name (e.g. "TaggableType").
}

// parentData drives the typed back-ref method emission on a parent entity
// (e.g. post.QueryFeaturedImage(ctx) → *Image for MorphOne, or
// post.QueryComments() → *CommentQuery for MorphMany).
//
// Every field is precomputed during analysis so the template stays free
// of string manipulation.
type parentData struct {
	ParentName  string // Host schema name (e.g. "Post").
	FieldName   string // Back-ref field name (e.g. "featured_image").
	FieldCap    string // PascalCase of FieldName (e.g. "FeaturedImage").
	Target      string // Child schema name (e.g. "Image").
	TargetIDent string // Child's lowercase predicate-package name (e.g. "image").
	MorphName   string // The relation name on the child (e.g. "imageable").
	IDField     string // Pascal column name on the child (e.g. "ImageableID").
	TypeField   string // Pascal column name on the child (e.g. "ImageableType").
	Kind        string // "morphOne" → single entity result; "morphMany" → query builder.
}

// childData drives Set<Morph>/Clear<Morph> generation for one child type
// and the typed predicate constructors emitted at package root.
type childData struct {
	Type         string             // The ent schema name (e.g. "Comment").
	IDent        string             // The lowercase predicate-package name (e.g. "comment").
	Relation     string             // The morph relation name (e.g. "commentable").
	RelationCap  string             // The PascalCase of Relation (e.g. "Commentable").
	IDField      string             // ent's Go-side field name for the id column.
	TypeField    string             // ent's Go-side field name for the type column.
	IDIsInt      bool               // True when the id column is int64 (vs string).
	Required       bool             // True when MorphTo(...).Required() was set — the
	// template emits a runtime hook that rejects
	// Saves leaving the discriminator unset or cleared.
	Touch          bool             // True when MorphTo(...).Touch(...) was set.
	TouchField     string           // Parent column name to bump (e.g. "updated_at").
	TouchFieldCap  string           // PascalCase for setter name (e.g. "UpdatedAt" → Set<TouchFieldCap>).
	Cascade        bool             // True when MorphTo(...).Cascade() was set — emits a
	// pre-delete hook on every allowed parent that
	// deletes polymorphic children pointing at the
	// parent.
	ChildIDGoType  string           // The child schema's own ID type (e.g. "int") —
	// used as the map-key type in the eager-load
	// result struct so the lookup is typed end-to-end.
	ChildPlural    string           // Plural of child Type name (e.g. "Comments" for
	// Comment). Used as the result struct's slice
	// field name so the API reads naturally:
	// `for _, c := range r.Comments { ... }`.
	// Default is Type + "s"; irregular plurals not
	// auto-handled (rename via codegen if needed).
	AllowedTypes   []string         // Allowed parent ent schema names — drives the
	// sealed-interface marker methods.
	ResolveCases []resolveCaseData // Per-parent switch cases for the typed
	// parent resolver (Query<Relation>) and the
	// touch hook's parent-update dispatch.
}

// resolveCaseData is one switch arm in the generated parent resolver. It
// carries everything the template needs to emit the case body without
// inspecting the runtime config.
type resolveCaseData struct {
	Type             string // Parent schema name (e.g. "Post").
	IDent            string // Parent's lowercase predicate-package name (e.g. "post").
	MorphConst       string // The morph-key constant name (e.g. "PostMorphKey").
	IDGoType         string // Parent's ID Go type ("int", "int64", "string", ...).
	HasSoftDelete    bool   // This parent declares the soft-delete column.
	SoftDeleteFieldCap string // PascalCase column name → ent's predicate fn (e.g. "DeletedAt" → `<ident>.DeletedAtIsNil()`).
}

// ──────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────

// lower returns the lowercase form of s — used to derive the ent predicate
// sub-package name from a schema type (e.g. "Comment" → "comment").
func lower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// pascalCase converts a snake_case / kebab-case identifier to PascalCase.
// Used for relation-name → method-suffix conversion (commentable →
// Commentable) and column-name → Go-field-name conversion.
func pascalCase(s string) string {
	out := make([]byte, 0, len(s))
	up := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || c == '-' {
			up = true
			continue
		}
		if up && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		up = false
		out = append(out, c)
	}
	return string(out)
}

// lookupAlias returns the morph key registered for the given schema name,
// or ("", false) when no alias is registered. Linear scan; the map is
// small.
func lookupAlias(m map[string]string, schemaName string) (string, bool) {
	for k, v := range m {
		if v == schemaName {
			return k, true
		}
	}
	return "", false
}

// jsonMarshal / jsonUnmarshal are thin wrappers around encoding/json that
// keep preprocess.go free of the encoding/json import (so its top reads
// as graph mutation, not serialisation). The wrappers are private; the
// indirection is purely cosmetic.
func jsonMarshal(v any) ([]byte, error)      { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error    { return json.Unmarshal(b, v) }
