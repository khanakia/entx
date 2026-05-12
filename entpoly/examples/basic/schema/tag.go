package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Tag is the holder side of the polymorphic many-to-many relation. A
// single Tag can attach to many Posts or many Videos via the Taggable
// pivot.
type Tag struct{ ent.Schema }

func (Tag) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique(),
	}
}

func (Tag) Edges() []ent.Edge {
	return []ent.Edge{
		// One MorphedByMany per concrete parent type. The pivot
		// (Taggable) is shared across both relations.
		entpoly.MorphedByMany("posts", Post.Type).
			Through("taggables", Taggable.Type),
		entpoly.MorphedByMany("videos", Video.Type).
			Through("taggables", Taggable.Type),
	}
}
