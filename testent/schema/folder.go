package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entcascade"
)

// Folder is the INTERMEDIATE type for the nested-unlink regression test.
// Its WithUnlink("channels") rule must fire when Workspace cascades through
// Folder into Channel — otherwise channels would be hard-deleted by the
// default nested-cascade path.
type Folder struct {
	ent.Schema
}

func (Folder) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.Int("workspace_id"),
	}
}

func (Folder) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("workspace", Workspace.Type).
			Ref("folders").
			Unique().
			Required().
			Field("workspace_id"),
		edge.To("channels", Channel.Type),
	}
}

func (Folder) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entcascade.Cascade(
			entcascade.WithUnlink("channels"),
		),
	}
}
