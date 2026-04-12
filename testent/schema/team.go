package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/khanakia/entx/entcascade"
)

// Team tests SkipEdges: deleting a team cascades to members
// but skips the "owner" edge (owner user survives).
type Team struct {
	ent.Schema
}

func (Team) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
	}
}

func (Team) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("members", Member.Type),
		edge.To("owner", User.Type).Unique(),
	}
}

func (Team) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entcascade.Cascade(
			entcascade.SkipEdges("owner"),
		),
	}
}
