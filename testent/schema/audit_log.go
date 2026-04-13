package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// AuditLog exists to exercise SkipEdges on a REAL O2M edge — one that
// entcascade would otherwise walk and delete. When a Team is cascade-
// deleted, its audit logs must survive (compliance/history requirement),
// so Team's Cascade() annotation carries SkipEdges("audit_logs").
//
// team_id is optional+nullable so the row remains valid after the referenced
// team row is gone.
type AuditLog struct {
	ent.Schema
}

func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.String("action"),
		field.Int("team_id").Optional().Nillable(),
	}
}

func (AuditLog) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("team", Team.Type).
			Ref("audit_logs").
			Unique().
			Field("team_id"),
	}
}
