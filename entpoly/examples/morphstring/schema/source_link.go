// SourceLink reproduces the "entity name diverges from morph noun" case
// from the maintainer's second bug report. Entity "SourceLink", morph
// "sourceable" — there is no stem match between SourceLink/Sourceable
// so any template site that conflates them surfaces here even without
// .GQL() chained.
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

type SourceLink struct{ ent.Schema }

func (SourceLink) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("sourceable"),
	}
}

func (SourceLink) Fields() []ent.Field {
	return []ent.Field{
		field.String("url"),
	}
}

func (SourceLink) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphTo("sourceable", Post.Type, Video.Type),
	}
}
