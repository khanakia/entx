package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Video mirrors Post as a polymorphic parent. Same back-references via
// Edges(); no special handling needed.
type Video struct{ ent.Schema }

func (Video) Fields() []ent.Field {
	return []ent.Field{
		field.String("title"),
		field.String("url"),
		// updated_at must exist on every parent listed in
		// Comment.MorphTo's AllowedTypes when Touch() is enabled —
		// codegen calls SetUpdatedAt on each branch.
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Video) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphMany("comments", Comment.Type, "commentable"),
	}
}
