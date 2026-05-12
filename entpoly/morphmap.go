// morphmap.go — pure helpers for the morph-key ↔ schema-name mapping plus
// the snake_case fallback used when no explicit alias is registered. No IO,
// no graph mutation; functions here are safe to call from any phase.
//
// Notes:
//
//   - keyForType is a LINEAR SCAN over a typically small map. Building a
//     reverse index would be marginally faster but would require wrapping
//     MorphMap in a struct, which hurts JSON serialisation and test
//     ergonomics. The map is small (< 50 entries in any real project);
//     do not optimise unless a profile says so.
//
//   - snake() deliberately does NOT collapse acronym runs ("HTTPRequest"
//     becomes "h_t_t_p_request", not "http_request"). The rule for when
//     to collapse and when not is heuristic and locale-fragile; users
//     wanting different aliases should register them explicitly via
//     WithMorphMap. Changing this behaviour breaks the back-compat
//     contract — every existing schema relies on the current rule.
//
//   - resolveTarget is reserved for future use cases that need to look
//     up a target type by string name; preprocess uses a slightly
//     different lookup (findTypeByName) that lives on Extension because
//     it captures the graph pointer for the duration of the pass.
package entpoly

import (
	"strings"

	"entgo.io/ent/entc/gen"
)

// MorphMap maps a stable string alias (the value persisted in the "*_type"
// column on the child) to the corresponding ent schema type name. The alias
// is the *durable* identity of a parent type — keeping it stable across
// Go-side renames is the entire reason this map exists.
//
// The map is constructed via entpoly.WithMorphMap(...) at codegen time. The
// codegen pass reads it via keyForType(); the runtime reads it via the
// generated MorphTypeFor / MorphTypeName functions in polymorphic.go.
//
// MorphMap is a plain map[string]string, not a struct, so it is trivially
// JSON-serialisable, mergeable via maps.Copy, and comparable in tests.
type MorphMap map[string]string

// keyForType returns the morph key for an ent type name. Resolution order:
//
//  1. If the map has an explicit entry whose value equals schemaName,
//     return that entry's key (the explicit alias).
//  2. Otherwise, return snake_case(schemaName) as the default key.
//
// This fallback is what lets small projects skip MorphMap configuration
// entirely — the codegen still produces sensible morph keys, they are
// just tied to the Go-side type names.
//
// The function is intentionally a linear scan over a typically small map.
// Building a reverse index would be marginally faster but would force the
// map to be wrapped in a struct, hurting JSON serialisation and tests.
func (m MorphMap) keyForType(schemaName string) string {
	for alias, target := range m {
		if target == schemaName {
			return alias
		}
	}
	return snake(schemaName)
}

// snake converts an ent type name into its default morph key. The rule is
// simple and predictable: insert an underscore before each uppercase letter
// after the first, then lowercase everything.
//
//	"Post"          → "post"
//	"FeaturedPost"  → "featured_post"
//	"HTTPRequest"   → "h_t_t_p_request"   (no acronym collapsing on purpose)
//	"X"             → "x"
//
// We deliberately do *not* collapse runs of uppercase letters (the way
// Rails / Laravel do for "HTTPRequest" → "http_request"). The rule for
// when to collapse and when not to is heuristic, fragile across locales,
// and would surprise users who hit an edge case. If acronym collapsing is
// what you want, register an explicit MorphMap entry — that is the
// canonical escape hatch.
func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// resolveTarget returns the *gen.Type for a schema name, or nil when the
// schema is not present in the graph. Linear scan because gen.Graph does
// not expose an indexed lookup; the graph is small enough (tens of types
// at most in any realistic project) that this is fine.
//
// Used during analysis when a ParentAnnotation references a target by
// string name and the codegen needs the resolved *gen.Type to inspect its
// id field, package name, etc.
func resolveTarget(g *gen.Graph, name string) *gen.Type {
	for _, t := range g.Nodes {
		if t.Name == name {
			return t
		}
	}
	return nil
}
