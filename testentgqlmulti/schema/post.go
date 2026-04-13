package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/khanakia/entx/entgqlmulti"
)

// Post tests edge filtering: present in apidash (so User.posts survives there)
// but absent from apipub and apimobile (so User.posts must be stripped out).
type Post struct {
	ent.Schema
}

func (Post) Fields() []ent.Field {
	return []ent.Field{
		field.String("title").
			Annotations(entgql.OrderField("TITLE")),
		field.Text("body"),
		field.Bool("published").Default(false),
	}
}

func (Post) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("author", User.Type).
			Ref("posts").
			Unique(),
		edge.To("tags", Tag.Type),
	}
}

func (Post) Annotations() []schema.Annotation {
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
		}),
	}
}
