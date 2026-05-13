package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Report is a second UUID-PK parent. Together with Document it gives
// Annotation two allowed parent types so the codegen exercises both
// branches of the type switch.
type Report struct{ ent.Schema }

func (Report) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("name"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
