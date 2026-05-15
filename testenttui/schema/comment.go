package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	enttui "github.com/khanakia/entx/enttui"
)

// Comment — child of both Post and User. Exercises two upward
// breadcrumb edges + editable body.
type Comment struct{ ent.Schema }

func (Comment) Fields() []ent.Field {
	return []ent.Field{
		field.Text("body").
			NotEmpty().
			Annotations(enttui.AsTitle{}, enttui.AsBody{}, enttui.Editable{}, enttui.Filterable{}),
		field.Time("created_at").
			Default(time.Now).
			Annotations(enttui.Sortable{}),
	}
}

func (Comment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("post", Post.Type).
			Ref("comments").
			Unique().
			Annotations(enttui.Upward{Trigger: "p"}),
		edge.From("author", User.Type).
			Ref("comments").
			Unique().
			Annotations(enttui.Upward{Trigger: "u"}),
	}
}

func (Comment) Annotations() []schema.Annotation {
	return []schema.Annotation{
		enttui.Display{Value: "Comments"},
		enttui.Group{Value: "content"},
		enttui.Icon{Value: "C"},
		enttui.AllowCreate{},
		enttui.AllowDelete{},
		enttui.DefaultSort{Field: "created_at", Direction: enttui.Desc},
		enttui.RelatedColumns(
			enttui.RelatedColumn{Edge: "author", Field: "name", Label: "Author", Pick: true},
			enttui.RelatedColumn{Edge: "post", Field: "title", Label: "Post", Pick: true},
		),
	}
}
