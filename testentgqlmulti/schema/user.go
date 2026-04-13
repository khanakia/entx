package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/khanakia/entx/entgqlmulti"
)

// User exercises: full CRUD in apidash, read-only subset "PublicUser" in apipub,
// enum field (status) pulled into the subset schema, edge to Post (in-API),
// and an edge to Secret that must be filtered out of apipub.
type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("first_name").
			Annotations(entgql.OrderField("FIRST_NAME")),
		field.String("last_name"),
		field.String("email").Unique(),
		field.String("avatar").Optional(),
		field.Enum("status").
			Values("active", "suspended", "banned").
			Default("active"),
		field.Time("created_at").
			Default(timeNow).
			Annotations(entgql.OrderField("CREATED_AT")),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("posts", Post.Type),
		edge.To("secrets", Secret.Type),
	}
}

func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		// entgql annotations must be present for entgql to emit the type
		// in the monolithic schema (QueryField + MutationInputs enable those SDL fragments).
		entgql.RelayConnection(),
		entgql.QueryField(),
		entgql.Mutations(entgql.MutationCreate(), entgql.MutationUpdate()),
		entgqlmulti.ApiConfig(map[string][]entgqlmulti.ApiTarget{
			"apidash": {
				{
					Query:     true,
					Mutations: true,
					Filters:   true,
					OrderBy:   true,
				},
			},
			"apipub": {
				{
					// Subset type, read-only. Uses snake_case to test normalization.
					TypeName: "PublicUser",
					Fields:   []string{"id", "first_name", "avatar", "status"},
					Query:    true,
					Filters:  true,
				},
			},
			"apimobile": {
				{
					// Mobile uses camelCase to test normalization from the other side.
					TypeName:  "MobileUser",
					Fields:    []string{"id", "firstName", "lastName", "createdAt"},
					Query:     true,
					QueryName: "me",
				},
			},
		}),
	}
}
