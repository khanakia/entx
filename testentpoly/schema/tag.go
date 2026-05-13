package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Tag is the M2M holder. MorphedByMany emits the auto-inverse
// post.QueryTags / video.QueryTags methods.
type Tag struct{ ent.Schema }

func (Tag) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique(),
	}
}

func (Tag) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphedByMany("posts", Post.Type).
			Through("taggables", Taggable.Type),
		entpoly.MorphedByMany("videos", Video.Type).
			Through("taggables", Taggable.Type),
	}
}
