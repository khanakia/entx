// mixin.go — MorphMixin and its options. The mixin is what physically adds
// the discriminator columns (<rel>_id and <rel>_type) to a child schema.
// We use a mixin rather than auto-injecting fields from the preprocess
// pass because ent's gen.Field has unexported state and constructing one
// by hand is fragile across ent versions; mixins are the officially
// supported extension point and compose cleanly with everything downstream.
//
// Notes:
//
//   - MixinAllowed promotes the type column from field.String to
//     field.Enum. This is the single biggest lever for safety — when set,
//     the database, ent's runtime validator, AND the typed Go column all
//     enforce the closed set. See docs/adr-001-type-safety.md for the
//     full rationale; the short version is that approach C (sealed
//     interface + enum column) beats either alone.
//
//   - The mixin runs at schema LOAD time, before our extension's
//     preprocess hook sees the graph. So new mixin options must work
//     with information the user passes EXPLICITLY at declaration time —
//     the mixin cannot read the edge's AllowedTypes or any other
//     extension state.
//
//   - The user must keep MixinAllowed and MorphTo's parent list in sync.
//     Today preprocess catches drift via the "missing column" diagnostic
//     when only one side has overrides for IDColumn/TypeColumn. A v2
//     enhancement (tracked in docs/architecture.md) would also lint that
//     the AllowedTypes lists agree.
//
//   - snakeForMixin is a deliberate duplicate of morphmap.go's snake().
//     The reason: this file runs at schema-load time, far from the
//     codegen graph context, and we want zero cross-file coupling at
//     load time. If you update the snake_case rules in one place, update
//     both — and add a test in entpoly/edgecase_test.go that documents
//     the new behaviour.
package entpoly

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"
)

// MixinOption configures a MorphMixin instance. Options are orthogonal —
// applying two options that affect the same setting follows the standard
// functional-options ordering (last write wins).
type MixinOption func(*morphMixin)

// MorphMixin returns an ent mixin that adds the discriminator columns
// (<relation>_id and <relation>_type) for a polymorphic relation. Place
// it on the child schema's Mixin() return so the fields are part of the
// schema by the time ent's graph builder runs.
//
//	func (Comment) Mixin() []ent.Mixin {
//	    return []ent.Mixin{
//	        entpoly.MorphMixin("commentable", entpoly.MixinAllowed(Post.Type, Video.Type)),
//	    }
//	}
//
// The mixin pattern is used (rather than auto-injection in a codegen
// hook) because ent's internal Field state is partially unexported and
// constructing gen.Field values directly is fragile across ent versions.
// Mixins are ent's officially-supported extension point for adding
// fields and are guaranteed to compose cleanly with everything downstream.
//
// The discriminator columns default to string id + string type, optional
// + nillable so the relation can be cleared. When MixinAllowed is set,
// the type column becomes a proper enum (field.Enum) — the database
// enforces the allowed set via a CHECK constraint and ent emits typed
// predicates for the column. Without MixinAllowed, the column falls back
// to a plain string and only the sealed-interface setter restricts the
// write path.
//
// Customise with options:
//
//	entpoly.MorphMixin("commentable",
//	    entpoly.MixinAllowed(Post.Type, Video.Type),  // emit enum column
//	    entpoly.MixinIDType("int"),
//	    entpoly.MixinIDColumn("parent_id"),
//	    entpoly.MixinTypeColumn("parent_type"),
//	)
//
// The allowed list on the mixin and the parent list on the corresponding
// MorphTo edge must agree. Preprocess validates this and surfaces a clear
// error if they drift apart.
func MorphMixin(relation string, opts ...MixinOption) ent.Mixin {
	m := &morphMixin{
		relation: relation,
		idType:   "string",
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// MixinIDType selects the id column's Go type: "string" (default) or "int".
// Use "int" only when every allowed parent has an int64 primary key.
func MixinIDType(t string) MixinOption {
	return func(m *morphMixin) { m.idType = t }
}

// MixinIDColumn overrides the default "<relation>_id" column name.
func MixinIDColumn(name string) MixinOption {
	return func(m *morphMixin) { m.idColumn = name }
}

// MixinTypeColumn overrides the default "<relation>_type" column name.
func MixinTypeColumn(name string) MixinOption {
	return func(m *morphMixin) { m.typeColumn = name }
}

// MixinNoIndex disables the composite index on (<type>, <id>) that the
// mixin emits by default. The composite index is essentially mandatory
// for the read path — every back-ref query (post.QueryComments(),
// QueryCommentable, the typed predicates) filters on both columns
// together — so the index is on by default. Disable it only when you
// know a different access pattern dominates and you want to save the
// write overhead.
func MixinNoIndex() MixinOption {
	return func(m *morphMixin) { m.noIndex = true }
}

// MixinAllowed enumerates the parent ent schema types this child may
// reference. When non-empty, the mixin emits the type column as
// field.Enum(allowed_keys...) — so the database (and entgql, and ent's
// generated predicates) treat the column as a constrained enum rather
// than a plain string.
//
// Pass the same set of X.Type method values you pass to the matching
// MorphTo edge. preprocess validates that the two lists agree; a drift
// becomes an error at codegen time, not at runtime.
//
//	entpoly.MorphMixin("commentable",
//	    entpoly.MixinAllowed(Post.Type, Video.Type),
//	)
//
// When MixinAllowed is omitted, the type column is a plain optional
// string and runs without any DB-level constraint. The sealed-interface
// setter still restricts writes through the typed builders.
func MixinAllowed(parents ...any) MixinOption {
	return func(m *morphMixin) {
		for _, p := range parents {
			if n := schemaName(p); n != "" {
				// Default mapping schema-name → morph-key is snake_case.
				// Users with custom morph maps still get the right value
				// because preprocess re-resolves keys against the
				// extension's morph map at codegen time. Storing the
				// resolved alias here keeps the mixin self-contained.
				m.allowed = append(m.allowed, snakeForMixin(n))
			}
		}
	}
}

// morphMixin is the internal mixin implementation. It embeds the default
// mixin.Schema so the unimplemented methods (Annotations, Edges, etc.)
// return empty slices without us having to provide them explicitly.
type morphMixin struct {
	mixin.Schema
	relation   string
	idType     string
	idColumn   string
	typeColumn string
	allowed    []string // morph-key values for field.Enum; empty → field.String
	noIndex    bool     // true → skip the composite (type, id) index emission
}

// Indexes returns the composite index that makes the back-ref read path
// scale. Every back-ref query — post.QueryComments(), QueryCommentable,
// the typed predicates emitted in polymorphic.go — filters on both the
// type column AND the id column together, so a multi-column index over
// the pair is the natural shape.
//
// Index column order is (type, id) rather than (id, type) because:
//   - The type column has lower cardinality (one value per allowed
//     parent), so it makes a better leading column for prefix scans.
//   - Counts and "all rows of type X" queries (the common case in
//     polymorphic dashboards) prefix-match on type alone.
//
// Disable via MixinNoIndex() if a different access pattern dominates
// and the write overhead matters.
func (m *morphMixin) Indexes() []ent.Index {
	if m.noIndex {
		return nil
	}
	idCol := m.idColumn
	if idCol == "" {
		idCol = m.relation + "_id"
	}
	typeCol := m.typeColumn
	if typeCol == "" {
		typeCol = m.relation + "_type"
	}
	return []ent.Index{
		index.Fields(typeCol, idCol),
	}
}

// snakeForMixin is a local copy of the snake helper that lives on
// morphmap.go. We duplicate the tiny string transform here so the mixin
// (which runs at schema-load time, far from the codegen graph) has no
// cross-file dependency surprise.
func snakeForMixin(s string) string {
	out := make([]byte, 0, len(s)+4)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i > 0 && c >= 'A' && c <= 'Z' {
			out = append(out, '_')
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

// Fields returns the two discriminator columns. Called by ent's mixin
// processor during schema load — by the time our preprocess hook runs,
// these fields are already part of the type's Fields slice in gen.Graph.
//
// The type column shape depends on whether MixinAllowed was set:
//
//   - empty: field.String — open-ended, no DB constraint, only the
//     sealed-interface setter restricts writes.
//   - non-empty: field.Enum — the database enforces the allowed set
//     via a CHECK constraint and ent emits typed predicates over the
//     enum values. Combined with the sealed-interface setter, this is
//     the "best of both worlds" approach C from the design brainstorm.
func (m *morphMixin) Fields() []ent.Field {
	idCol := m.idColumn
	if idCol == "" {
		idCol = m.relation + "_id"
	}
	typeCol := m.typeColumn
	if typeCol == "" {
		typeCol = m.relation + "_type"
	}

	var idField ent.Field
	switch m.idType {
	case "int":
		idField = field.Int64(idCol).Optional().Nillable().StorageKey(idCol)
	default:
		idField = field.String(idCol).Optional().Nillable().StorageKey(idCol)
	}

	var typeField ent.Field
	if len(m.allowed) > 0 {
		// field.Enum with the snake-case morph keys. Values appear in
		// the same order they were registered, which preprocess later
		// validates against the matching MorphTo edge.
		typeField = field.Enum(typeCol).
			Values(m.allowed...).
			Optional().
			Nillable().
			StorageKey(typeCol)
	} else {
		typeField = field.String(typeCol).Optional().Nillable().StorageKey(typeCol)
	}

	return []ent.Field{idField, typeField}
}
