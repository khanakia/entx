package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Image demonstrates the one-to-one polymorphic shape: any imageable
// parent has at most one featured Image. The schema declares both the
// discriminator mixin and the MorphTo edge for type-safe writes.
type Image struct{ ent.Schema }

func (Image) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("imageable"),
	}
}

func (Image) Fields() []ent.Field {
	return []ent.Field{
		field.String("url"),
		field.Int("width").Optional(),
		field.Int("height").Optional(),
	}
}

func (Image) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphTo("imageable", Post.Type),
	}
}
