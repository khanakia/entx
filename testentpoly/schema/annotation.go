package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"

	"github.com/khanakia/entx/entpoly"
)

// Annotation is the polymorphic child of UUID-PK parents. Used by
// scenario 10 (UUID round trip). A second MorphTo / MorphMixin is added
// in phase 3 for scenario 14 (multi-relation on one schema).
type Annotation struct{ ent.Schema }

func (Annotation) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("target",
			entpoly.MixinAllowed(Document.Type, Report.Type),
		),
		// Second poly relation on the same schema — scenario 14. Points
		// at the int-PK Post/Video to ensure two MorphMixin + two
		// MorphTo decls (different ID types) coexist on one child.
		entpoly.MorphMixin("secondary",
			entpoly.MixinAllowed(Post.Type, Video.Type),
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
		entpoly.MorphTo("target", Document.Type, Report.Type).
			GQL("AnnotationTarget"),
		entpoly.MorphTo("secondary", Post.Type, Video.Type),
	}
}
