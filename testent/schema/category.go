package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/khanakia/entx/entcascade"
)

// Category tests WithUnlink: deleting a category clears the FK on posts
// instead of deleting them. Posts survive with category_id = NULL.
type Category struct {
	ent.Schema
}

func (Category) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique(),
	}
}

func (Category) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("posts", Post.Type),
	}
}

func (Category) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entcascade.Cascade(
			entcascade.WithUnlink("posts"),
		),
	}
}
