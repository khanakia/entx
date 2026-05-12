package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Post is a polymorphic parent — Comments belong to it, an Image features
// on it, and Tags attach to it (via the Taggable pivot). The polymorphism
// is declared entirely on the child / pivot / holder schemas; Post itself
// only opts into the back-references via Edges().
type Post struct{ ent.Schema }

func (Post) Fields() []ent.Field {
	return []ent.Field{
		field.String("title"),
		field.Text("body").Optional(),
	}
}

func (Post) Edges() []ent.Edge {
	return []ent.Edge{
		// Many comments belong to this post via Comment.commentable.
		entpoly.MorphMany("comments", Comment.Type, "commentable"),
		// At most one featured image references this post via Image.imageable.
		entpoly.MorphOne("featured_image", Image.Type, "imageable"),
	}
}
