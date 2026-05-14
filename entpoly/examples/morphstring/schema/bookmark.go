// Bookmark — second divergent-name fixture from the maintainer's
// follow-up bug report. Entity "Bookmark", morph "pinnable" — no stem
// overlap. Mirror of SourceLink/sourceable so the regression guard
// covers two distinct divergent pairs (one rule fits all).
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

type Bookmark struct{ ent.Schema }

func (Bookmark) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("pinnable"),
	}
}

func (Bookmark) Fields() []ent.Field {
	return []ent.Field{
		field.String("label"),
	}
}

func (Bookmark) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphTo("pinnable", Post.Type, Video.Type).GQL("Pinnable"),
	}
}
