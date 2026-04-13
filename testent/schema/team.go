package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/khanakia/entx/entcascade"
)

// Team exercises SkipEdges on a real O2M edge (audit_logs). Deleting a
// team cascades to members but preserves audit_logs for compliance.
//
// Note: the "owner" edge is M2O from Team's side (Team owns owner_id),
// so entcascade ignores it automatically — no SkipEdges needed. That's
// included here mainly to document the "parent-pointing edges are
// auto-skipped" invariant.
type Team struct {
	ent.Schema
}

func (Team) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
	}
}

func (Team) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("members", Member.Type),
		edge.To("owner", User.Type).Unique(),
		edge.To("audit_logs", AuditLog.Type),
	}
}

func (Team) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entcascade.Cascade(
			// Load-bearing: without this, entcascade would walk the O2M
			// audit_logs edge and hard-delete compliance history.
			entcascade.SkipEdges("audit_logs"),
		),
	}
}
