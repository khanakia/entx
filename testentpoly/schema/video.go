package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Video is a second polymorphic parent for Comment. Same shape as Post,
// minus the published flag.
type Video struct{ ent.Schema }

func (Video) Fields() []ent.Field {
	return []ent.Field{
		field.String("title"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Video) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphMany("comments", Comment.Type, "commentable"),
	}
}
