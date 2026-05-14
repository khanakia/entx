package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

type Post struct{ ent.Schema }

func (Post) Fields() []ent.Field {
	return []ent.Field{
		field.String("title"),
	}
}

func (Post) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphMany("comments", Comment.Type, "commentable"),
		entpoly.MorphOne("featured_image", Image.Type, "imageable"),
		// Divergent-name back-ref: parent "Post", child "SourceLink",
		// morph "sourceable" (no stem match with SourceLink). Tests
		// that codegen derives column accessors from the morph name
		// not the entity name.
		entpoly.MorphMany("source_links", SourceLink.Type, "sourceable"),
	}
}
