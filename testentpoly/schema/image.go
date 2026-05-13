package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Image is a third polymorphic parent for Comment in phase 1. (Future
// phases swap this around for a MorphOne shape; for now it just provides
// a third AllowedType target.)
type Image struct{ ent.Schema }

func (Image) Fields() []ent.Field {
	return []ent.Field{
		field.String("url"),
		// updated_at required because Comment.MorphTo lists Image as an
		// AllowedType and uses Touch(); every allowed parent must have
		// the touch column.
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Image) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphMany("comments", Comment.Type, "commentable"),
	}
}
