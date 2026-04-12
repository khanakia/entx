package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Profile tests O2O cascade: deleting a user deletes their profile.
type Profile struct {
	ent.Schema
}

func (Profile) Fields() []ent.Field {
	return []ent.Field{
		field.String("bio").Optional(),
		field.Int("user_id").Unique(),
	}
}

func (Profile) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("profile").
			Unique().
			Required().
			Field("user_id"),
	}
}
