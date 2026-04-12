package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// PostTag is the junction table for Post <-> Tag M2M.
// entcascade hard-deletes junction rows during cascade.
type PostTag struct {
	ent.Schema
}

func (PostTag) Fields() []ent.Field {
	return []ent.Field{
		field.Int("post_id"),
		field.Int("tag_id"),
	}
}

func (PostTag) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("post", Post.Type).
			Unique().
			Required().
			Field("post_id"),
		edge.To("tag", Tag.Type).
			Unique().
			Required().
			Field("tag_id"),
	}
}
