package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entcascade"
)

// Doc is the INTERMEDIATE type for the nested soft-delete regression test.
// WithSoftDelete("notes", "archived_at") uses a NON-default field name
// (archived_at, not deleted_at) so it proves the annotation is consulted —
// the auto-detect path would look only for "deleted_at" and miss it.
type Doc struct {
	ent.Schema
}

func (Doc) Fields() []ent.Field {
	return []ent.Field{
		field.String("title"),
		field.Int("workspace_id"),
	}
}

func (Doc) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("workspace", Workspace.Type).
			Ref("docs").
			Unique().
			Required().
			Field("workspace_id"),
		edge.To("notes", Note.Type),
	}
}

func (Doc) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entcascade.Cascade(
			entcascade.WithSoftDelete("notes", "archived_at"),
		),
	}
}
