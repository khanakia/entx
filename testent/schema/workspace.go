package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entcascade"
)

// Workspace is the ROOT for the nested-annotation test chains. Its own
// Cascade() has no edge rules — all nested behavior must come from
// intermediate types' annotations (Folder.WithUnlink, Doc.WithSoftDelete).
//
// Chains:
//
//	Workspace → Folder (WithUnlink "channels") → Channel
//	Workspace → Doc    (WithSoftDelete "notes") → Note
type Workspace struct {
	ent.Schema
}

func (Workspace) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique(),
	}
}

func (Workspace) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("folders", Folder.Type),
		edge.To("docs", Doc.Type),
	}
}

func (Workspace) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entcascade.Cascade(),
	}
}
