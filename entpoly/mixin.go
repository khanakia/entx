package entpoly

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
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
//	    return []ent.Mixin{entpoly.MorphMixin("commentable")}
//	}
//
// The mixin pattern is used (rather than auto-injection in a codegen
// hook) because ent's internal Field state is partially unexported and
// constructing gen.Field values directly is fragile across ent versions.
// Mixins are ent's officially-supported extension point for adding
// fields and are guaranteed to compose cleanly with everything downstream.
//
// The discriminator columns default to string id + string type, optional
// + nillable so the relation can be cleared. Customise with options:
//
//	entpoly.MorphMixin("commentable",
//	    entpoly.MixinIDType("int"),
//	    entpoly.MixinIDColumn("parent_id"),
//	    entpoly.MixinTypeColumn("parent_type"),
//	)
//
// Customisation here must match the same overrides on the MorphTo edge
// builder for the same relation — entpoly does not yet cross-check
// agreement, so a mismatch surfaces as a Go compile error in the
// generated polymorphic.go.
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

// morphMixin is the internal mixin implementation. It embeds the default
// mixin.Schema so the unimplemented methods (Annotations, Edges, etc.)
// return empty slices without us having to provide them explicitly.
type morphMixin struct {
	mixin.Schema
	relation   string
	idType     string
	idColumn   string
	typeColumn string
}

// Fields returns the two discriminator columns. Called by ent's mixin
// processor during schema load — by the time our preprocess hook runs,
// these fields are already part of the type's Fields slice in gen.Graph.
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
	typeField := field.String(typeCol).Optional().Nillable().StorageKey(typeCol)

	return []ent.Field{idField, typeField}
}
