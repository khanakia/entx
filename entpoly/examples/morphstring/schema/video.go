package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

type Video struct{ ent.Schema }

func (Video) Fields() []ent.Field {
	return []ent.Field{
		field.String("title"),
	}
}

func (Video) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphMany("comments", Comment.Type, "commentable"),
	}
}
