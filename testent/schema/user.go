package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/khanakia/entx/entcascade"
)

// User tests basic cascade: deleting a user cascades to all posts.
type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.String("email").Unique(),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("posts", Post.Type),
		edge.To("profile", Profile.Type).Unique(),
	}
}

func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entcascade.Cascade(),
	}
}
