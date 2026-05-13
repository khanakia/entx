package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Event uses custom discriminator column names — entity_pk / entity_kind
// instead of the default <morph>_id / <morph>_type. Drives scenario 13.
type Event struct{ ent.Schema }

func (Event) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("entity",
			entpoly.MixinAllowed(Post.Type, Video.Type),
			entpoly.MixinIDColumn("entity_pk"),
			entpoly.MixinTypeColumn("entity_kind"),
		),
	}
}

func (Event) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
	}
}

func (Event) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphTo("entity", Post.Type, Video.Type).
			IDColumn("entity_pk").
			TypeColumn("entity_kind"),
	}
}
