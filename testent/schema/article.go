package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/khanakia/entx/entcascade"
)

// Article tests soft delete: deleting an article soft-deletes its revisions
// (Revision has a deleted_at field, auto-detected by entcascade).
type Article struct {
	ent.Schema
}

func (Article) Fields() []ent.Field {
	return []ent.Field{
		field.String("title"),
	}
}

func (Article) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("revisions", Revision.Type),
	}
}

func (Article) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entcascade.Cascade(),
	}
}
