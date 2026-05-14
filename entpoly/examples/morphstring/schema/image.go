package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

type Image struct{ ent.Schema }

func (Image) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("imageable"),
	}
}

func (Image) Fields() []ent.Field {
	return []ent.Field{
		field.String("url"),
	}
}

func (Image) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphTo("imageable", Post.Type),
	}
}
