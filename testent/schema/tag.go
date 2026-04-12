package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Tag is the target of M2M through PostTag.
// Tags survive when posts are deleted — only junction rows are removed.
type Tag struct {
	ent.Schema
}

func (Tag) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique(),
	}
}

func (Tag) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("posts", Post.Type).
			Ref("tags").
			Through("post_tags", PostTag.Type),
	}
}
