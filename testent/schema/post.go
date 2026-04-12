package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/khanakia/entx/entcascade"
)

// Post tests nested cascade: deleting a post cascades to comments.
// Also has M2M through PostTag for junction table cascade.
type Post struct {
	ent.Schema
}

func (Post) Fields() []ent.Field {
	return []ent.Field{
		field.String("title"),
		field.String("body"),
		field.Int("author_id").Optional(),
		field.Int("category_id").Optional().Nillable(),
	}
}

func (Post) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("author", User.Type).
			Ref("posts").
			Unique().
			Field("author_id"),
		edge.To("comments", Comment.Type),
		edge.To("tags", Tag.Type).
			Through("post_tags", PostTag.Type),
		edge.From("category", Category.Type).
			Ref("posts").
			Unique().
			Field("category_id"),
	}
}

func (Post) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entcascade.Cascade(),
	}
}
