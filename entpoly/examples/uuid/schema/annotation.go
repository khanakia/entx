package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"

	"github.com/khanakia/entx/entpoly"
)

// Annotation is the polymorphic child — points at any Document or
// Report via a single discriminator pair. Because both parents use
// uuid.UUID PKs, entpoly's codegen:
//
//   - imports github.com/google/uuid into polymorphic.go
//   - emits uuid.Parse branches in the resolver's strconv switch
//   - keys the eager-load result map on Annotation's UUID PK
//     (map[uuid.UUID]AnnotationTargetParent)
type Annotation struct{ ent.Schema }

func (Annotation) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("target",
			entpoly.MixinAllowed(Document.Type, Report.Type),
		),
	}
}

func (Annotation) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("body"),
	}
}

func (Annotation) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphTo("target", Document.Type, Report.Type),
	}
}
