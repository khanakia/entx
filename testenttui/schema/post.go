package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	enttui "github.com/khanakia/entx/enttui"
)

// Post — exercises: enum + chip tones + status, body, default-sort,
// upward edge (author breadcrumb) with a Pick reference column, M2M
// tags, multi-edge detail split.
type Post struct{ ent.Schema }

func (Post) Fields() []ent.Field {
	return []ent.Field{
		field.String("title").
			NotEmpty().
			Annotations(enttui.AsTitle{}, enttui.Editable{}, enttui.Sortable{}, enttui.Filterable{}),
		field.Text("body").
			Optional().
			Annotations(enttui.AsBody{}, enttui.Editable{}),
		field.Enum("status").
			Values("draft", "published", "archived").
			Default("draft").
			Annotations(
				enttui.AsStatus{},
				enttui.Editable{},
				enttui.Sortable{},
				enttui.Filterable{},
				enttui.Chip{Tones: map[string]string{
					"draft":     "muted",
					"published": "success",
					"archived":  "danger",
				}},
			),
		field.Time("created_at").
			Default(time.Now).
			Annotations(enttui.Sortable{}),
	}
}

func (Post) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("author", User.Type).
			Ref("posts").
			Unique().
			Annotations(enttui.Upward{Trigger: "u"}),
		edge.To("tags", Tag.Type),
		edge.To("comments", Comment.Type).
			Annotations(enttui.Drill{Trigger: "n"}),
	}
}

func (Post) Annotations() []schema.Annotation {
	return []schema.Annotation{
		enttui.Display{Value: "Posts"},
		enttui.Group{Value: "content"},
		enttui.Icon{Value: "P"},
		enttui.AllowCreate{},
		enttui.AllowDelete{},
		enttui.AllowExport{},
		enttui.AllowBulkCopy{},
		enttui.DefaultSort{Field: "created_at", Direction: enttui.Desc},
		enttui.DetailEdge{Edges: []string{"comments", "tags"}},
		enttui.RelatedColumns(
			enttui.RelatedColumn{Edge: "author", Field: "name", Label: "Author", Pick: true},
			enttui.RelatedColumn{Edge: "author", Field: "email"},
		),
	}
}
