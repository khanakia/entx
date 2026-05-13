package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Report is the second UUID-PK parent for Annotation. Deliberately omits
// deleted_at to exercise per-target soft-delete auto-detection — codegen
// must skip the IsNil predicate for this branch.
type Report struct{ ent.Schema }

func (Report) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("name"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
