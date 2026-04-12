package entcascade

import (
	"encoding/json"

	"entgo.io/ent/entc/gen"
	"entgo.io/ent/schema"
)

const annotationName = "EntCascade"

// CascadeOption configures cascade delete behavior for specific edges.
type CascadeOption func(*Annotation)

// SoftDeleteEdge maps an edge name to the soft-delete field on its target type.
type SoftDeleteEdge struct {
	Edge  string `json:"edge"`
	Field string `json:"field"`
}

// Annotation controls cascade delete behavior for an ent type.
// Add it to a schema's Annotations() to generate cascade delete functions.
type Annotation struct {
	SkipEdges       []string         `json:"skip_edges,omitempty"`
	SoftDeleteEdges []SoftDeleteEdge `json:"soft_delete_edges,omitempty"`
	HardDeleteEdges []string         `json:"hard_delete_edges,omitempty"`
	UnlinkEdges     []string         `json:"unlink_edges,omitempty"`
}

func (Annotation) Name() string { return annotationName }

// Cascade marks a type for cascade delete code generation.
//
//	entcascade.Cascade()                                          // all edges, auto-detect soft delete
//	entcascade.Cascade(entcascade.SkipEdges("ai_model"))          // skip specific edges
//	entcascade.Cascade(entcascade.WithSoftDelete("files", "deleted_at"))
//	entcascade.Cascade(entcascade.WithUnlink("channels"))
func Cascade(opts ...CascadeOption) Annotation {
	ann := Annotation{}
	for _, opt := range opts {
		opt(&ann)
	}
	return ann
}

// SkipEdges excludes specific edges from the cascade delete.
func SkipEdges(edges ...string) CascadeOption {
	return func(a *Annotation) { a.SkipEdges = append(a.SkipEdges, edges...) }
}

// WithSoftDelete forces soft delete for an edge's target, using the specified field.
// The field name is the snake_case column (e.g., "deleted_at", "removed_at").
func WithSoftDelete(edge, field string) CascadeOption {
	return func(a *Annotation) {
		a.SoftDeleteEdges = append(a.SoftDeleteEdges, SoftDeleteEdge{Edge: edge, Field: field})
	}
}

// WithHardDelete forces hard delete for an edge's target, even if it has a soft-delete field.
func WithHardDelete(edges ...string) CascadeOption {
	return func(a *Annotation) { a.HardDeleteEdges = append(a.HardDeleteEdges, edges...) }
}

// WithUnlink clears the foreign key instead of deleting the target (SET NULL).
// The target entity survives; only the reference is removed.
func WithUnlink(edges ...string) CascadeOption {
	return func(a *Annotation) { a.UnlinkEdges = append(a.UnlinkEdges, edges...) }
}

// decodeAnnotation reads the EntCascade annotation from a gen.Type.
func decodeAnnotation(t *gen.Type) (*Annotation, bool) {
	raw, ok := t.Annotations[annotationName]
	if !ok {
		return nil, false
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var ann Annotation
	if err := json.Unmarshal(b, &ann); err != nil {
		return nil, false
	}
	return &ann, true
}

// Ensure Annotation satisfies schema.Annotation.
var _ schema.Annotation = Annotation{}
