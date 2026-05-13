package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Comment is the polymorphic child. Phase 1 keeps the MorphTo plain —
// no Required / Touch / Cascade / SoftDelete / GQL options yet; those
// come in later phases.
type Comment struct{ ent.Schema }

func (Comment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("commentable",
			entpoly.MixinAllowed(Post.Type, Video.Type, Image.Type),
		),
	}
}

func (Comment) Fields() []ent.Field {
	return []ent.Field{
		field.Text("body"),
	}
}

func (Comment) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphTo("commentable", Post.Type, Video.Type, Image.Type).
			Required().
			Touch().
			Cascade().
			SoftDelete().
			GQL(),
	}
}
