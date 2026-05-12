package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Comment is the canonical polymorphic child — its body belongs to any
// commentable parent (Post or Video). The relation is fully declared via
// the MorphMixin (which adds commentable_id + commentable_type columns)
// plus the MorphTo edge (which carries the metadata + allowed parent
// types).
type Comment struct{ ent.Schema }

// Mixin pulls in entpoly's discriminator columns. Without this line the
// preprocess hook errors out at codegen time with a "missing column"
// diagnostic.
func (Comment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("commentable"),
	}
}

func (Comment) Fields() []ent.Field {
	return []ent.Field{
		field.Text("body"),
	}
}

func (Comment) Edges() []ent.Edge {
	return []ent.Edge{
		// MorphTo declares the polymorphic side: the discriminator pair
		// can point at either a Post or a Video. The parent types are
		// passed via the ent .Type method-value idiom.
		entpoly.MorphTo("commentable", Post.Type, Video.Type),
	}
}
