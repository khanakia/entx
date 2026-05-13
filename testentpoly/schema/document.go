package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Document is a polymorphic parent with a UUID primary key — drives the
// UUID round-trip scenario (10). Declares deleted_at so per-target
// soft-delete auto-detection has a UUID-PK target to exercise.
type Document struct{ ent.Schema }

func (Document) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("title"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("deleted_at").Optional().Nillable(),
	}
}
