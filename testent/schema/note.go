package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Note is the leaf in the Workspace → Doc → Note chain.
// archived_at is the custom soft-delete column referenced by Doc's
// WithSoftDelete("notes", "archived_at") rule.
type Note struct {
	ent.Schema
}

func (Note) Fields() []ent.Field {
	return []ent.Field{
		field.String("body"),
		field.Int("doc_id"),
		field.Time("archived_at").Optional().Nillable(),
	}
}

func (Note) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("doc", Doc.Type).
			Ref("notes").
			Unique().
			Required().
			Field("doc_id"),
	}
}
