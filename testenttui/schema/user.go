package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	enttui "github.com/khanakia/entx/enttui"
)

// User — a person who authors posts and comments. Exercises: title
// field, editable fields, drill-down edge, detail-edge split, full
// create/delete/export/bulk-copy capability set.
type User struct{ ent.Schema }

func (User) Mixin() []ent.Mixin { return []ent.Mixin{idMixin{}} }

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			NotEmpty().
			Annotations(enttui.AsTitle{}, enttui.Editable{}, enttui.Sortable{}, enttui.Filterable{}),
		field.String("email").
			NotEmpty().
			Unique().
			Annotations(enttui.Editable{}, enttui.Filterable{}),
		field.String("bio").
			Optional().
			Annotations(enttui.Editable{}, enttui.AsBody{}),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("posts", Post.Type).
			Annotations(enttui.Drill{Trigger: "o"}),
		edge.To("comments", Comment.Type),
	}
}

func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		enttui.Display{Value: "Users"},
		enttui.Group{Value: "people"},
		enttui.Icon{Value: "U"},
		enttui.AllowCreate{},
		enttui.AllowDelete{},
		enttui.AllowExport{},
		enttui.AllowBulkCopy{},
		enttui.DetailEdge{Edge: "posts"},
	}
}
