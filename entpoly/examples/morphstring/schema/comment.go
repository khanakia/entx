// Comment uses bare MorphMixin — NO MixinAllowed — so the type column
// is emitted as field.String (not field.Enum). This exercises the
// template branch where <ident>.<TypeField> is the predicate function,
// not a named string type.
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

type Comment struct{ ent.Schema }

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
		entpoly.MorphTo("commentable", Post.Type, Video.Type).GQL("Commentable"),
	}
}
