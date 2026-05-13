package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Folder is a self-referential polymorphic parent — its own type is
// listed in its own AllowedTypes. Document is also allowed so the
// "two-target self-ref" path is exercised.
type Folder struct{ ent.Schema }

func (Folder) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("parent",
			entpoly.MixinAllowed(Folder.Type, Document.Type),
		),
	}
}

func (Folder) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
	}
}

func (Folder) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphTo("parent", Folder.Type, Document.Type),
	}
}
