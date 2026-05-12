package entpoly

import (
	"encoding/json"
	"reflect"

	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
)

// markerAnnotation is attached to the edge.Descriptor.Annotations slice of
// every polymorphic edge so the pre-codegen hook can identify these edges
// in the loaded gen.Graph. The marker carries every piece of metadata the
// codegen pass needs — no separate annotation lookup, no separate schema
// surface.
//
// The struct is private but JSON-marshalled into ent's annotation map, so
// the field tags drive the on-wire representation.
type markerAnnotation struct {
	// Kind is one of "morphTo" / "morphOne" / "morphMany" / "morphedByMany".
	// Drives the dispatch in preprocess.
	Kind string `json:"kind"`

	// MorphName is the relation name (e.g. "commentable"). Default
	// column names derive from this — commentable_id + commentable_type.
	MorphName string `json:"morph_name"`

	// FieldName is the back-reference / forward-reference method name
	// emitted by codegen. For MorphTo / pivot MorphTo, it is the same as
	// MorphName. For MorphMany / MorphOne / MorphedByMany it is the
	// user-supplied field name (e.g. "comments", "featured_image").
	FieldName string `json:"field_name"`

	// AllowedTypes (MorphTo only) — the parent ent schema names this
	// child may point at. Resolved at builder time via reflection over
	// the user-supplied X.Type method values.
	AllowedTypes []string `json:"allowed_types,omitempty"`

	// Target (MorphOne/MorphMany/MorphedByMany) — the concrete child or
	// parent ent schema name this back-ref targets.
	Target string `json:"target,omitempty"`

	// IDColumn overrides the default "<MorphName>_id" column name.
	IDColumn string `json:"id_column,omitempty"`

	// TypeColumn overrides the default "<MorphName>_type" column name.
	TypeColumn string `json:"type_column,omitempty"`

	// IDType is "string" (default) or "int" — selects the Go type of
	// the id column. The default is string because it accommodates any
	// parent PK shape.
	IDType string `json:"id_type,omitempty"`

	// Through (MorphedByMany only) — the pivot ent schema name. The
	// pivot itself must declare its own MorphTo for the relation.
	Through string `json:"through,omitempty"`

	// ThroughName (MorphedByMany only) — the SQL table name for the
	// pivot. Cosmetic; ent uses the schema name as the default.
	ThroughName string `json:"through_name,omitempty"`

	// Required marks the relation as non-nullable. Currently advisory —
	// v2 may emit a runtime hook that enforces the constraint.
	Required bool `json:"required,omitempty"`
}

// MarkerName identifies the annotation key for polymorphic edges. Exported
// so other extensions can detect entpoly edges if they need to coexist.
const MarkerName = "EntPolyMarker"

// Name satisfies schema.Annotation. Required by ent's annotation pipeline.
func (markerAnnotation) Name() string { return MarkerName }

// idColumn returns the resolved id column name (override or default).
func (m markerAnnotation) idColumn() string {
	if m.IDColumn != "" {
		return m.IDColumn
	}
	return m.MorphName + "_id"
}

// typeColumn returns the resolved type column name (override or default).
func (m markerAnnotation) typeColumn() string {
	if m.TypeColumn != "" {
		return m.TypeColumn
	}
	return m.MorphName + "_type"
}

// decodeMarker reads the marker annotation off an edge.Descriptor's
// Annotations slice. The slice contains schema.Annotation values; we look
// for ours by Name() and JSON round-trip the payload back into our typed
// struct (the same pattern ent itself uses to deliver annotations to
// extensions at codegen time).
func decodeMarker(anns []schema.Annotation) (*markerAnnotation, bool) {
	for _, a := range anns {
		if a.Name() != MarkerName {
			continue
		}
		// Two shapes are possible: the typed struct (when we constructed
		// the edge ourselves), or a map[string]any (after ent's pipeline
		// JSON-round-trips the annotation). Handle both.
		if m, ok := a.(markerAnnotation); ok {
			return &m, true
		}
		b, err := json.Marshal(a)
		if err != nil {
			continue
		}
		var m markerAnnotation
		if err := json.Unmarshal(b, &m); err != nil {
			continue
		}
		return &m, true
	}
	return nil, false
}

// ──────────────────────────────────────────────────────────────────────────
// Schema-name reflection (mirrors edge.To's reflect-based identification)
// ──────────────────────────────────────────────────────────────────────────

// schemaName extracts the ent schema name from a Schema.Type method value
// (e.g. the `Post.Type` syntax users pass to edge.To). ent itself uses
// the same reflection trick: the method value has signature
// func(Post), so reflect.TypeOf(t).In(0).Name() returns "Post".
//
// Returns the empty string when t is not a method value or has no
// receiver, so callers can detect and skip malformed input gracefully.
func schemaName(t any) string {
	rt := reflect.TypeOf(t)
	if rt == nil || rt.Kind() != reflect.Func || rt.NumIn() == 0 {
		return ""
	}
	return rt.In(0).Name()
}

// ──────────────────────────────────────────────────────────────────────────
// MorphTo — child-side declaration
// ──────────────────────────────────────────────────────────────────────────

// MorphToBuilder is the fluent edge builder for the child side of a
// polymorphic relation. It implements ent.Edge by exposing Descriptor().
// Users place the builder inside a schema's Edges() method.
type MorphToBuilder struct {
	desc *edge.Descriptor
	ann  *markerAnnotation
}

// MorphTo declares the child side of a polymorphic relation. The first
// argument is the relation name (drives default column names); the rest
// are the parent schema types this child may reference, passed via the
// schema-type method-value syntax (Post.Type, Video.Type).
//
//	func (Comment) Edges() []ent.Edge {
//	    return []ent.Edge{
//	        entpoly.MorphTo("commentable", Post.Type, Video.Type),
//	    }
//	}
//
// The discriminator columns (commentable_id + commentable_type by default)
// are injected into the child's Fields() at codegen time — the user does
// not have to declare them manually.
func MorphTo(name string, parents ...any) *MorphToBuilder {
	parentNames := make([]string, 0, len(parents))
	for _, p := range parents {
		if n := schemaName(p); n != "" {
			parentNames = append(parentNames, n)
		}
	}
	ann := &markerAnnotation{
		Kind:         "morphTo",
		MorphName:    name,
		FieldName:    name,
		AllowedTypes: parentNames,
		IDType:       "string",
	}
	// edge.Descriptor.Type must reference a real schema for ent's graph
	// builder to accept the edge. We use the first allowed parent as a
	// placeholder; the preprocess hook strips this edge from the graph
	// before any codegen template runs, so ent never actually emits a
	// FK to the placeholder.
	placeholder := "Schema"
	if len(parentNames) > 0 {
		placeholder = parentNames[0]
	}
	return &MorphToBuilder{
		desc: &edge.Descriptor{
			Name:        name,
			Type:        placeholder,
			Annotations: []schema.Annotation{*ann},
		},
		ann: ann,
	}
}

// IDColumn overrides the default "<MorphName>_id" column name. Useful
// when you want the discriminator id to match an existing legacy column
// name or when two relations on the same schema would otherwise collide.
func (b *MorphToBuilder) IDColumn(name string) *MorphToBuilder {
	b.ann.IDColumn = name
	b.syncAnnotation()
	return b
}

// TypeColumn overrides the default "<MorphName>_type" column name.
func (b *MorphToBuilder) TypeColumn(name string) *MorphToBuilder {
	b.ann.TypeColumn = name
	b.syncAnnotation()
	return b
}

// IDType selects the Go type of the id column: "string" (default) or
// "int". Use "int" only when every allowed parent has an int64 primary
// key.
func (b *MorphToBuilder) IDType(t string) *MorphToBuilder {
	b.ann.IDType = t
	b.syncAnnotation()
	return b
}

// Required marks the relation as non-nullable. Currently advisory — the
// discriminator columns are still emitted as nullable to support the
// Clear<Morph>() codegen helper, but a future v2 runtime hook will reject
// writes that leave them null.
func (b *MorphToBuilder) Required() *MorphToBuilder {
	b.ann.Required = true
	b.syncAnnotation()
	return b
}

// Descriptor satisfies ent.Edge by returning the underlying descriptor
// with the latest annotation state.
func (b *MorphToBuilder) Descriptor() *edge.Descriptor {
	b.syncAnnotation()
	return b.desc
}

// syncAnnotation copies the builder's annotation pointer into the
// descriptor's Annotations slice. This is needed after every builder
// method because the annotation lives in two places (we want the
// descriptor's slice to be the source of truth for ent's pipeline but
// the builder's annotation to be the source of truth for chaining).
func (b *MorphToBuilder) syncAnnotation() {
	b.desc.Annotations = []schema.Annotation{*b.ann}
}

// Compile-time assertion that the builder satisfies ent.Edge.
var _ ent.Edge = (*MorphToBuilder)(nil)

// ──────────────────────────────────────────────────────────────────────────
// MorphMany — parent-side one-to-many back-reference
// ──────────────────────────────────────────────────────────────────────────

// MorphManyBuilder is the fluent edge builder for the parent side of a
// one-to-many polymorphic relation.
type MorphManyBuilder struct {
	desc *edge.Descriptor
	ann  *markerAnnotation
}

// MorphMany declares a one-to-many polymorphic back-reference on the
// parent. The field name (e.g. "comments") is the method emitted on the
// parent type by v2 codegen; the child type (e.g. Comment.Type) is the
// concrete schema this back-ref returns; the morph name (e.g.
// "commentable") must match the child's MorphTo declaration.
//
//	func (Post) Edges() []ent.Edge {
//	    return []ent.Edge{
//	        entpoly.MorphMany("comments", Comment.Type, "commentable"),
//	    }
//	}
func MorphMany(field string, child any, morphName string) *MorphManyBuilder {
	target := schemaName(child)
	ann := &markerAnnotation{
		Kind:      "morphMany",
		FieldName: field,
		MorphName: morphName,
		Target:    target,
	}
	return &MorphManyBuilder{
		desc: &edge.Descriptor{
			Name:        field,
			Type:        target,
			Annotations: []schema.Annotation{*ann},
		},
		ann: ann,
	}
}

// IDColumn overrides the default "<MorphName>_id" column on the child.
// Must match the override on the corresponding MorphTo.
func (b *MorphManyBuilder) IDColumn(name string) *MorphManyBuilder {
	b.ann.IDColumn = name
	b.syncAnnotation()
	return b
}

// TypeColumn overrides the default "<MorphName>_type" column on the child.
func (b *MorphManyBuilder) TypeColumn(name string) *MorphManyBuilder {
	b.ann.TypeColumn = name
	b.syncAnnotation()
	return b
}

// Descriptor satisfies ent.Edge.
func (b *MorphManyBuilder) Descriptor() *edge.Descriptor {
	b.syncAnnotation()
	return b.desc
}

func (b *MorphManyBuilder) syncAnnotation() {
	b.desc.Annotations = []schema.Annotation{*b.ann}
}

var _ ent.Edge = (*MorphManyBuilder)(nil)

// ──────────────────────────────────────────────────────────────────────────
// MorphOne — parent-side one-to-one back-reference
// ──────────────────────────────────────────────────────────────────────────

// MorphOneBuilder is the fluent edge builder for the parent side of a
// one-to-one polymorphic relation.
type MorphOneBuilder struct {
	desc *edge.Descriptor
	ann  *markerAnnotation
}

// MorphOne declares a one-to-one polymorphic back-reference on the parent.
// Mirrors MorphMany but the v2 codegen will return a single entity (or
// nil) instead of a query.
//
//	func (Post) Edges() []ent.Edge {
//	    return []ent.Edge{
//	        entpoly.MorphOne("featured_image", Image.Type, "imageable"),
//	    }
//	}
func MorphOne(field string, child any, morphName string) *MorphOneBuilder {
	target := schemaName(child)
	ann := &markerAnnotation{
		Kind:      "morphOne",
		FieldName: field,
		MorphName: morphName,
		Target:    target,
	}
	return &MorphOneBuilder{
		desc: &edge.Descriptor{
			Name:        field,
			Type:        target,
			Annotations: []schema.Annotation{*ann},
		},
		ann: ann,
	}
}

// IDColumn overrides the default "<MorphName>_id" column on the child.
func (b *MorphOneBuilder) IDColumn(name string) *MorphOneBuilder {
	b.ann.IDColumn = name
	b.syncAnnotation()
	return b
}

// TypeColumn overrides the default "<MorphName>_type" column on the child.
func (b *MorphOneBuilder) TypeColumn(name string) *MorphOneBuilder {
	b.ann.TypeColumn = name
	b.syncAnnotation()
	return b
}

// Descriptor satisfies ent.Edge.
func (b *MorphOneBuilder) Descriptor() *edge.Descriptor {
	b.syncAnnotation()
	return b.desc
}

func (b *MorphOneBuilder) syncAnnotation() {
	b.desc.Annotations = []schema.Annotation{*b.ann}
}

var _ ent.Edge = (*MorphOneBuilder)(nil)

// ──────────────────────────────────────────────────────────────────────────
// MorphedByMany — M2M holder back-reference through a pivot
// ──────────────────────────────────────────────────────────────────────────

// MorphedByManyBuilder is the fluent edge builder for the holder side of
// a polymorphic many-to-many relation. The pivot is configured via
// .Through(name, pivotType).
type MorphedByManyBuilder struct {
	desc *edge.Descriptor
	ann  *markerAnnotation
}

// MorphedByMany declares a many-to-many polymorphic back-reference from
// the holder (e.g. Tag) to a concrete parent (e.g. Post). The pivot
// schema must be configured with .Through(...) and must itself declare a
// MorphTo for the morph relation.
//
//	func (Tag) Edges() []ent.Edge {
//	    return []ent.Edge{
//	        entpoly.MorphedByMany("posts", Post.Type).
//	            Through("taggables", Taggable.Type),
//	    }
//	}
func MorphedByMany(field string, parent any) *MorphedByManyBuilder {
	target := schemaName(parent)
	ann := &markerAnnotation{
		Kind:      "morphedByMany",
		FieldName: field,
		Target:    target,
	}
	return &MorphedByManyBuilder{
		desc: &edge.Descriptor{
			Name:        field,
			Type:        target,
			Annotations: []schema.Annotation{*ann},
		},
		ann: ann,
	}
}

// Through configures the pivot for the relation. The pivot table name
// (e.g. "taggables") is cosmetic; the pivot type (e.g. Taggable.Type) is
// the ent schema that owns the pivot rows.
func (b *MorphedByManyBuilder) Through(name string, pivot any) *MorphedByManyBuilder {
	b.ann.ThroughName = name
	b.ann.Through = schemaName(pivot)
	// Default the morph name from the pivot's name, matching Laravel's
	// "taggables" → "taggable" convention.
	if b.ann.MorphName == "" {
		b.ann.MorphName = singularise(name)
	}
	b.syncAnnotation()
	return b
}

// MorphName overrides the morph relation name (default: singularised pivot
// table name). Must match the MorphTo declaration on the pivot schema.
func (b *MorphedByManyBuilder) MorphName(name string) *MorphedByManyBuilder {
	b.ann.MorphName = name
	b.syncAnnotation()
	return b
}

// IDColumn overrides the default "<MorphName>_id" column on the pivot.
func (b *MorphedByManyBuilder) IDColumn(name string) *MorphedByManyBuilder {
	b.ann.IDColumn = name
	b.syncAnnotation()
	return b
}

// TypeColumn overrides the default "<MorphName>_type" column on the pivot.
func (b *MorphedByManyBuilder) TypeColumn(name string) *MorphedByManyBuilder {
	b.ann.TypeColumn = name
	b.syncAnnotation()
	return b
}

// Descriptor satisfies ent.Edge.
func (b *MorphedByManyBuilder) Descriptor() *edge.Descriptor {
	b.syncAnnotation()
	return b.desc
}

func (b *MorphedByManyBuilder) syncAnnotation() {
	b.desc.Annotations = []schema.Annotation{*b.ann}
}

var _ ent.Edge = (*MorphedByManyBuilder)(nil)

// singularise is a tiny English plural→singular rule used to default
// MorphName from the pivot table name (Laravel convention). Handles the
// only forms we expect: trailing "s", "ies"→"y". Anything else passes
// through unchanged — users with irregular pluralisation should call
// MorphName(...) explicitly.
func singularise(s string) string {
	switch {
	case len(s) > 3 && s[len(s)-3:] == "ies":
		return s[:len(s)-3] + "y"
	case len(s) > 1 && s[len(s)-1] == 's':
		return s[:len(s)-1]
	default:
		return s
	}
}
