package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Channel is the leaf in the Workspace → Folder → Channel chain.
// folder_id is nullable so the unlink op (SET folder_id = NULL) is legal.
// When Workspace is cascade-deleted, Folder goes away but Channel survives
// with folder_id cleared — proving the intermediate WithUnlink fired.
type Channel struct {
	ent.Schema
}

func (Channel) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.Int("folder_id").Optional().Nillable(),
	}
}

func (Channel) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("folder", Folder.Type).
			Ref("channels").
			Unique().
			Field("folder_id"),
	}
}
