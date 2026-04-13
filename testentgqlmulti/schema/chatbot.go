package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/khanakia/entx/entgqlmulti"
)

// Chatbot exercises multi-target per API: two GraphQL types ("Chatbot" and
// "ChatbotSummary") generated from the same Ent entity under one API (apidash).
// Both Go-model-bind to ent.Chatbot. The summary uses QueryName to override
// the auto-derived query field.
type Chatbot struct {
	ent.Schema
}

func (Chatbot) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			Annotations(entgql.OrderField("NAME")),
		field.String("avatar").Optional(),
		field.String("description").Optional(),
		field.String("status").Default("draft"),
		field.String("api_key").Sensitive(),
	}
}

func (Chatbot) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entgql.RelayConnection(),
		entgql.QueryField(),
		entgql.Mutations(entgql.MutationCreate(), entgql.MutationUpdate()),
		entgqlmulti.ApiConfig(map[string][]entgqlmulti.ApiTarget{
			"apidash": {
				{
					// Full Chatbot type with all fields + CRUD.
					Query:     true,
					Mutations: true,
					Filters:   true,
					OrderBy:   true,
				},
				{
					// Same Ent entity, different GraphQL type: only a few fields.
					TypeName:  "ChatbotSummary",
					Fields:    []string{"id", "name", "status"},
					Query:     true,
					QueryName: "chatbotSummaries",
				},
			},
			"apipub": {
				{
					TypeName: "PublicChatbot",
					Fields:   []string{"id", "name", "avatar", "description"},
					Query:    true,
				},
			},
		}),
	}
}
