package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/khanakia/entx/entgqlmulti"
)

// Tag lives in multiple APIs under the same name in apidash (full CRUD)
// and apipub (read-only subset with no mutations/orderby/filters).
type Tag struct {
	ent.Schema
}

func (Tag) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique().
			Annotations(entgql.OrderField("NAME")),
		field.String("color").Optional(),
	}
}

func (Tag) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("posts", Post.Type).Ref("tags"),
	}
}

func (Tag) Annotations() []schema.Annotation {
	return []schema.Annotation{
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
					Query: true,
					// No TypeName override: proves @goModel is NOT emitted when
					// the GraphQL type name equals the Ent type name.
				},
			},
		}),
	}
}
