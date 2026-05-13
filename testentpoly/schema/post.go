package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Post is a polymorphic parent for Comment via the "commentable" relation.
// Phase 1 keeps it minimal: just a title, a published flag (used by
// sub-query predicate scenarios), and updated_at (so Touch hooks added in
// later phases compile against this schema unchanged).
type Post struct{ ent.Schema }

func (Post) Fields() []ent.Field {
	return []ent.Field{
		field.String("title"),
		field.Bool("published").Default(false),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		// deleted_at is the column Comment.MorphTo(...).SoftDelete()
		// filters on. Optional + Nillable so reverse-resolve can use IS NULL.
		// Per-target auto-detection: only schemas that declare this field
		// gain the soft-delete predicate; Video deliberately does NOT
		// declare it.
		field.Time("deleted_at").Optional().Nillable(),
	}
}

func (Post) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphMany("comments", Comment.Type, "commentable"),
	}
}
