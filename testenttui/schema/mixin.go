package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
	"github.com/google/uuid"
)

// idMixin gives every entity a string primary key. enttui's generated
// glue projects every row id through a string (Row.ID is string), so
// the demo schemas use string ids to match — the same pattern the
// original aicoder example used. (int / uuid native PKs are a tracked
// enttui codegen follow-up.)
type idMixin struct{ mixin.Schema }

func (idMixin) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			DefaultFunc(func() string { return uuid.NewString() }).
			Immutable().
			Unique(),
	}
}

// uuidMixin gives an entity a native uuid.UUID primary key. Used to
// prove the codegen handles a uuid-typed ID per entity (different table,
// different ID type) — the conversion paths differ from string/int.
type uuidMixin struct{ mixin.Schema }

func (uuidMixin) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
	}
}
