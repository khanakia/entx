package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Taggable is the polymorphic pivot for Tag ↔ Post/Video.
type Taggable struct{ ent.Schema }

func (Taggable) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("taggable",
			entpoly.MixinAllowed(Post.Type, Video.Type),
		),
	}
}

func (Taggable) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tag_id"),
		field.Int("sort_order").Default(0),
	}
}

func (Taggable) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphTo("taggable", Post.Type, Video.Type),
	}
}
