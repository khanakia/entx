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
		cases := make([]resolveCaseData, 0, len(c.ResolveTargets))
		for _, rt := range c.ResolveTargets {
			cases = append(cases, resolveCaseData{
				Type:       rt.SchemaName,
				IDent:      lower(rt.SchemaName),
				MorphConst: rt.SchemaName + "MorphKey",
				IDGoType:   rt.IDGoType,
			})
		}
		d.Children = append(d.Children, childData{
			Type:         c.TypeName,
			IDent:        lower(c.TypeName),
			Relation:     c.MorphName,
			RelationCap:  pascalCase(c.MorphName),
			IDField:      pascalGoFieldName(c.IDColumn),
			TypeField:    pascalGoFieldName(c.TypeColumn),
			IDIsInt:      c.IDType == "int",
			AllowedTypes: c.AllowedTypes,
			ResolveCases: cases,
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
	d.SubpackageImports = make([]string, 0, len(imports))
	for k := range imports {
		d.SubpackageImports = append(d.SubpackageImports, k)
	}
	sort.Strings(d.SubpackageImports)

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

	// SubpackageImports is the deduplicated set of `<ident>` strings
	// referenced by the typed back-ref methods. Used by the template
	// to emit the right import lines.
	SubpackageImports []string
}

// morphMapEntry is one row of the rendered morph map literal.
type morphMapEntry struct {
	Key  string
	Type string
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
	AllowedTypes []string           // Allowed parent ent schema names — drives the
	// sealed-interface marker methods.
	ResolveCases []resolveCaseData // Per-parent switch cases for the typed
	// parent resolver (Query<Relation>).
}

// resolveCaseData is one switch arm in the generated parent resolver. It
// carries everything the template needs to emit the case body without
// inspecting the runtime config.
type resolveCaseData struct {
	Type       string // Parent schema name (e.g. "Post").
	IDent      string // Parent's lowercase predicate-package name (e.g. "post").
	MorphConst string // The morph-key constant name (e.g. "PostMorphKey").
	IDGoType   string // Parent's ID Go type ("int", "int64", "string", ...).
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
