package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Revision has a deleted_at field — entcascade auto-detects this
// and generates soft-delete (UPDATE SET deleted_at) instead of hard delete.
type Revision struct {
	ent.Schema
}

func (Revision) Fields() []ent.Field {
	return []ent.Field{
		field.String("body"),
		field.Int("version"),
		field.Int("article_id"),
		field.Time("deleted_at").Optional().Nillable(),
	}
}

func (Revision) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("article", Article.Type).
			Ref("revisions").
			Unique().
			Required().
			Field("article_id"),
	}
}
