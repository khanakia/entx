package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Taggable is the polymorphic pivot for the Tag ↔ Post/Video M2M. It is a
// regular ent schema with the discriminator mixin + a MorphTo edge, plus
// any pivot-specific fields (added_by, sort_order). From entpoly's
// perspective the pivot is just another polymorphic child — Tag is the
// thing that adds the M2M holder back-reference.
type Taggable struct{ ent.Schema }

func (Taggable) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("taggable", entpoly.MixinAllowed(Post.Type, Video.Type)),
	}
}

func (Taggable) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tag_id"),
		field.String("added_by").Optional(),
		field.Int("sort_order").Default(0),
	}
}

func (Taggable) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphTo("taggable", Post.Type, Video.Type),
	}
}
