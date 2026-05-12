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
	// needs only the last segment.
	d := &tmplData{Package: path.Base(s.Package)}
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
		d.Children = append(d.Children, childData{
			Type:        c.TypeName,
			Relation:    c.MorphName,
			RelationCap: pascalCase(c.MorphName),
			IDField:     pascalGoFieldName(c.IDColumn),
			TypeField:   pascalGoFieldName(c.TypeColumn),
			IDIsInt:     c.IDType == "int",
		})
	}

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

	// MorphMap is the effective morph map. Sorted by Key for stable
	// output.
	MorphMap []morphMapEntry

	// Children carries one entry per MorphTo declaration. Drives the
	// Set/Clear builder method emission.
	Children []childData
}

// morphMapEntry is one row of the rendered morph map literal.
type morphMapEntry struct {
	Key  string
	Type string
}

// childData drives Set<Morph>/Clear<Morph> generation for one child type.
type childData struct {
	Type        string // The ent schema name (e.g. "Comment").
	Relation    string // The morph relation name (e.g. "commentable").
	RelationCap string // The PascalCase of Relation (e.g. "Commentable").
	IDField     string // ent's Go-side field name for the id column.
	TypeField   string // ent's Go-side field name for the type column.
	IDIsInt     bool   // True when the id column is int64 (vs string).
}

// ──────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────

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
