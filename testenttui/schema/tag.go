package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	enttui "github.com/khanakia/entx/enttui"
)

// Tag — simple M2M target. Exercises a minimal entity + back-edge.
type Tag struct{ ent.Schema }

func (Tag) Mixin() []ent.Mixin { return []ent.Mixin{uuidMixin{}} }

func (Tag) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			NotEmpty().
			Unique().
			Annotations(enttui.AsTitle{}, enttui.Editable{}, enttui.Sortable{}, enttui.Filterable{}),
	}
}

func (Tag) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("posts", Post.Type).Ref("tags"),
	}
}

func (Tag) Annotations() []schema.Annotation {
	return []schema.Annotation{
		enttui.Display{Value: "Tags"},
		enttui.Group{Value: "content"},
		enttui.Icon{Value: "T"},
		enttui.AllowCreate{},
		enttui.AllowDelete{},
	}
}
