package codegen

// annotations.go — generic readers for ent schema/field annotations.
//
// ent stores annotations as `map[string]any` keyed by the annotation's
// .Name() string. Values arrive as JSON-decoded maps. We read them by
// well-known key path rather than type-asserting the original Go struct,
// which would otherwise require importing the enttui package and
// triggering a cyclic dep with this sub-package.
//
// Every helper returns the zero-value + false on miss so callers can fall
// back to convention defaults cleanly.

import (
	"sort"
)

// annotMap pulls the JSON-decoded map for a single annotation, if present.
func annotMap(annots map[string]any, name string) (map[string]any, bool) {
	if annots == nil {
		return nil, false
	}
	v, ok := annots[name]
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

// hasAnnot returns true if a marker (zero-field) annotation is present.
// Used for booleans like Browse / AsTitle / Sortable / Filterable / Hidden.
func hasAnnot(annots map[string]any, name string) bool {
	if annots == nil {
		return false
	}
	_, ok := annots[name]
	return ok
}

// annotString reads a string-valued field from an annotation.
//
//	annotString(annots, "EntTUI.Display", "Value")
func annotString(annots map[string]any, name, key string) (string, bool) {
	m, ok := annotMap(annots, name)
	if !ok {
		return "", false
	}
	s, ok := m[key].(string)
	return s, ok
}

// annotInt reads a JSON-number field as int. JSON's number type unmarshals
// to float64 — we narrow here.
func annotInt(annots map[string]any, name, key string) (int, bool) {
	m, ok := annotMap(annots, name)
	if !ok {
		return 0, false
	}
	switch v := m[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	}
	return 0, false
}

// annotRelatedColumns reads enttui.RelatedColumns — a slice of
// {Edge, Field, Label} entries. The annotation arrives JSON-decoded:
//
//	{"Columns": [{"Edge": "author", "Field": "name", "Label": "Author"}]}
func annotRelatedColumns(annots map[string]any) []struct{ Edge, Field, Label string } {
	m, ok := annotMap(annots, "EntTUI.RelatedColumns")
	if !ok {
		return nil
	}
	raw, ok := m["Columns"].([]any)
	if !ok {
		return nil
	}
	out := make([]struct{ Edge, Field, Label string }, 0, len(raw))
	for _, item := range raw {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		edge, _ := row["Edge"].(string)
		field, _ := row["Field"].(string)
		label, _ := row["Label"].(string)
		if edge == "" || field == "" {
			continue
		}
		out = append(out, struct{ Edge, Field, Label string }{edge, field, label})
	}
	return out
}

// annotStringMap reads a map[string]string field — used by enttui.Chip
// where the value is a value→tone mapping.
//
// Returns entries sorted by key for deterministic codegen output.
func annotStringMap(annots map[string]any, name, key string) ([]struct{ K, V string }, bool) {
	m, ok := annotMap(annots, name)
	if !ok {
		return nil, false
	}
	raw, ok := m[key].(map[string]any)
	if !ok {
		return nil, false
	}
	out := make([]struct{ K, V string }, 0, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		out = append(out, struct{ K, V string }{K: k, V: s})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out, len(out) > 0
}
