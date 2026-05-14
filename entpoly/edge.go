// edge.go — public schema-time API for declaring polymorphic edges. Every
// type users invoke from inside their schema's Edges() return method is
// defined here. The four shapes are:
//
//	MorphTo         child  →  one-of-N parents     (declares the relation)
//	MorphMany       parent →  many children        (back-ref, one-to-many)
//	MorphOne        parent →  one child            (back-ref, one-to-one)
//	MorphedByMany   holder →  one-of-N parents     (M2M back-ref via pivot)
//
// Each builder type implements ent.Edge by exposing Descriptor() and carries
// a markerAnnotation that flags it for the preprocess pass. The marker is
// the ONLY mechanism by which our codegen recognises polymorphic edges in
// the loaded gen.Graph — renaming MarkerName breaks every caller.
//
// Notes:
//
//   - The edge.Descriptor.Type field MUST resolve to a real schema name
//     for ent's graph builder to accept the edge. For MorphTo (which has
//     no concrete target — that's the whole point of polymorphism) we
//     use the first allowed parent as a placeholder Type. preprocess
//     strips the edge from gen.Type.Edges before ent's templates run, so
//     the placeholder never escapes into generated code.
//
//   - schemaName() is the reflection helper that turns Post.Type (a
//     method value of type func(Post)) into the schema name string
//     "Post". ent uses the same trick internally for edge.To. If a
//     caller passes something other than a method value (e.g. a nil or
//     an int), schemaName returns "" and the edge is silently filtered
//     out — be careful when extending the API to validate this.
//
//   - The builder methods (.IDColumn, .TypeColumn, .IDType, etc.) mutate
//     the marker annotation in place, then re-sync it into the
//     descriptor's Annotations slice via syncAnnotation(). The sync is
//     needed because the marker lives in two places: the builder field
//     (for chaining) and the descriptor (for ent's annotation pipeline).
//     Forgetting to sync after mutation produces silent stale data at
//     codegen time.
//
//   - When adding a new builder type:
//     (1) define a struct with a *edge.Descriptor + *markerAnnotation.
//     (2) implement Descriptor() and add a `var _ ent.Edge = ...` check.
//     (3) add a constructor that sets Kind to a new string constant.
//     (4) update preprocess.go's dispatch switch to handle the new Kind.
//     (5) update state.go + buildTmplData + the template + tests.
package entpoly

import (
	"encoding/json"
	"reflect"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
)

// polyEdgeAnnotations is the shared annotation set every entpoly edge
// carries: our marker (for preprocess identification) plus entsql.Skip()
// to suppress ent's automatic FK column emission. Without the Skip, ent
// would add ghost FK columns to the child for every parent edge — see
// the post_comments / video_comments cosmetic-but-confusing fields the
// previous build produced.
func polyEdgeAnnotations(marker markerAnnotation) []schema.Annotation {
	return []schema.Annotation{marker, entsql.Skip()}
}

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

	// InverseFieldName (MorphedByMany only) — the back-ref method name
	// auto-emitted on the Target type (e.g. "tags" for the
	// Tag.MorphedByMany("posts", Post.Type).Through(...) declaration
	// produces post.QueryTags(ctx) []*Tag). Empty → defaults to
	// snake-case of HolderName + "s" (e.g. "Tag" → "tags"). Override
	// for irregular plurals (Category → "categories").
	InverseFieldName string `json:"inverse_field_name,omitempty"`

	// Required marks the relation as non-nullable. Currently advisory —
	// v2 may emit a runtime hook that enforces the constraint.
	Required bool `json:"required,omitempty"`

	// Touch enables the Laravel-$touches behaviour: every successful
	// Save of this child bumps the polymorphic parent's TouchField
	// timestamp. Hook fires on OpCreate / OpUpdate / OpUpdateOne; the
	// parent update happens in the same transaction (failure rolls
	// back the whole save).
	Touch bool `json:"touch,omitempty"`

	// TouchField is the parent column name that the touch hook bumps.
	// Defaults to "updated_at". Must exist on every parent listed in
	// AllowedTypes; codegen produces a Set<PascalCase(TouchField)>
	// call and compile fails clearly if the parent is missing the
	// field.
	TouchField string `json:"touch_field,omitempty"`

	// Cascade enables application-level cascade-delete behaviour: when
	// a parent listed in AllowedTypes is deleted, every child row
	// pointing at it via this MorphTo is also deleted in the same
	// logical operation. Polymorphic columns cannot carry FK
	// constraints (the discriminator column references multiple
	// tables), so the database itself does not cascade — this hook
	// fills the gap. Pair with RegisterPolyHooks(client) at startup.
	Cascade bool `json:"cascade,omitempty"`

	// SoftDelete makes reverse resolves (QueryCommentable, eager-load,
	// any read path that resolves the parent) skip parents whose
	// SoftDeleteField is non-null. Auto-detected per allowed parent
	// at codegen time: targets without the field are passed through
	// unfiltered; targets that have it get an additional <field>IsNil
	// predicate.
	SoftDelete bool `json:"soft_delete,omitempty"`

	// SoftDeleteField names the parent timestamp column that signals
	// soft-deletion. Defaults to "deleted_at". The column must be
	// nullable; the generated filter is `<Field>IsNil()` (i.e.,
	// SELECT rows where the column IS NULL).
	SoftDeleteField string `json:"soft_delete_field,omitempty"`

	// GQL opts the relation into GraphQL union emission. When set,
	// codegen emits a Go type alias + exported Is<Union>() markers on
	// each allowed parent + a GQL<Rel>(ctx) resolver-helper. The
	// matching `.graphql` schema fragment (`union X = A | B`) is
	// emitted to the file configured via WithGQLSchemaFile on the
	// extension; without that option, the .graphql fragment is the
	// user's responsibility.
	GQL bool `json:"gql,omitempty"`

	// GQLUnionName overrides the default GraphQL union name (which is
	// PascalCase(MorphName) — "commentable" → "Commentable"). Useful
	// when you want a different GraphQL identity from the morph
	// relation name.
	GQLUnionName string `json:"gql_union_name,omitempty"`
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
			Annotations: polyEdgeAnnotations(*ann),
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

// Touch enables Laravel-$touches semantics: every successful Save of
// this child bumps the polymorphic parent's timestamp column. Hook
// fires on OpCreate / OpUpdate / OpUpdateOne; the parent update
// happens in the same logical operation, so a failure rolls back the
// whole save.
//
//	entpoly.MorphTo("commentable", Post.Type, Video.Type).Touch()
//	  // → bumps parent.updated_at on every Comment save
//
//	entpoly.MorphTo("commentable", Post.Type, Video.Type).Touch("modified_at")
//	  // → bumps parent.modified_at instead
//
// Every parent in AllowedTypes must declare the timestamp column.
// Without it, codegen emits a Set<Field>(...) call referencing a method
// that doesn't exist, and the build fails with a clear "undefined"
// error pointing at the missing field.
//
// Pair with RegisterPolyHooks(client) at startup to install the hook
// — without that call Touch is silently advisory.
func (b *MorphToBuilder) Touch(fieldName ...string) *MorphToBuilder {
	b.ann.Touch = true
	b.ann.TouchField = "updated_at"
	if len(fieldName) > 0 && fieldName[0] != "" {
		b.ann.TouchField = fieldName[0]
	}
	b.syncAnnotation()
	return b
}

// Cascade enables application-level cascade-delete: every parent listed
// in AllowedTypes runs a pre-delete hook (registered by
// RegisterPolyHooks) that deletes every child row pointing at the
// parent via this MorphTo. Without Cascade, deleting a parent leaves
// orphan child rows whose discriminator references a now-missing row.
//
//	entpoly.MorphTo("commentable", Post.Type, Video.Type).Cascade()
//	  // Post.DeleteOneID(p.ID).Save(ctx) now also deletes every Comment
//	  // with commentable_type='post' AND commentable_id=p.ID.
//
// Cascade does NOT change the SQL schema — there is still no FK
// constraint. The hook runs the cascade in application code, so it
// works on every dialect ent supports.
func (b *MorphToBuilder) Cascade() *MorphToBuilder {
	b.ann.Cascade = true
	b.syncAnnotation()
	return b
}

// SoftDelete filters soft-deleted parents out of every reverse resolve
// path (QueryCommentable, eager-load WithCommentable, M2M holder).
// Auto-detects per allowed parent: only targets that actually declare
// the field gain the `<field>IsNil()` filter — other targets pass
// through unchanged.
//
// Default field name is "deleted_at". Override:
//
//	entpoly.MorphTo("commentable", Post.Type).SoftDelete()                // → deleted_at
//	entpoly.MorphTo("commentable", Post.Type).SoftDelete("removed_at")    // override
//
// The field must be nullable on the parent. SoftDelete does not delete;
// it only filters reads. Pair with whatever soft-delete mixin / hook
// your parents already use.
func (b *MorphToBuilder) SoftDelete(fieldName ...string) *MorphToBuilder {
	b.ann.SoftDelete = true
	b.ann.SoftDeleteField = "deleted_at"
	if len(fieldName) > 0 && fieldName[0] != "" {
		b.ann.SoftDeleteField = fieldName[0]
	}
	b.syncAnnotation()
	return b
}

// GQL opts the relation into GraphQL union emission. When set, codegen
// produces three things in polymorphic.go:
//
//   - A type alias `type <Union> = <Child><Rel>Parent` so the sealed
//     interface doubles as a gqlgen-recognisable union type.
//   - Exported marker methods `func (*Post) Is<Union>() {}` on every
//     allowed parent. gqlgen reflects over these to wire the union
//     variants at runtime.
//   - A resolver-helper `func (*<Child>) GQL<Rel>(ctx) (<Union>, error)`
//     that wraps QueryCommentable for use in a gqlgen resolver:
//
//	    func (r *commentResolver) Commentable(ctx, c *ent.Comment) (ent.Commentable, error) {
//	        return c.GQLCommentable(ctx)
//	    }
//
// The matching GraphQL schema fragment is emitted to the file
// configured via entpoly.WithGQLSchemaFile(...) on the extension; if
// that option is not set, the user must write the `union ... = ...`
// declaration in their .graphql schema by hand.
//
// Default union name is PascalCase(MorphName) — "commentable" →
// "Commentable". Override:
//
//	entpoly.MorphTo("commentable", Post.Type).GQL()              // → Commentable
//	entpoly.MorphTo("commentable", Post.Type).GQL("PostOrVideo") // → PostOrVideo
func (b *MorphToBuilder) GQL(unionName ...string) *MorphToBuilder {
	b.ann.GQL = true
	if len(unionName) > 0 && unionName[0] != "" {
		b.ann.GQLUnionName = unionName[0]
	}
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
	b.desc.Annotations = polyEdgeAnnotations(*b.ann)
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
			Annotations: polyEdgeAnnotations(*ann),
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
	b.desc.Annotations = polyEdgeAnnotations(*b.ann)
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
			Annotations: polyEdgeAnnotations(*ann),
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
	b.desc.Annotations = polyEdgeAnnotations(*b.ann)
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
			Annotations: polyEdgeAnnotations(*ann),
		},
		ann: ann,
	}
}

// Through configures the pivot for the relation. The pivot table name
// (e.g. "taggables") is cosmetic; the pivot type (e.g. Taggable.Type) is
// the ent schema that owns the pivot rows.
//
// MorphName is intentionally NOT defaulted here — preprocess resolves
// it by first looking up the pivot type's MorphTo declaration (its
// MorphName is the source of truth for the discriminator columns) and
// only falling back to singularise(name) when no MorphTo is present.
// Defaulting eagerly to singularise(name) would set the wrong
// discriminator column whenever the pivot's table name doesn't share a
// stem with its MorphTo morph name (e.g. "source_links" pivot with
// MorphTo("sourceable", ...) — singularise gives "source_link" but
// the correct relation is "sourceable").
func (b *MorphedByManyBuilder) Through(name string, pivot any) *MorphedByManyBuilder {
	b.ann.ThroughName = name
	b.ann.Through = schemaName(pivot)
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

// InverseName overrides the auto-emitted back-ref method name on the
// Target type. Default is snake-case of the holder type name plus "s"
// (e.g. Tag → "tags" → post.QueryTags). Override for irregular plurals:
//
//	entpoly.MorphedByMany("posts", Post.Type).
//	    Through("categorizables", Categorizable.Type).
//	    InverseName("categories")   // → post.QueryCategories(ctx)
//
// The codegen reflects this name as PascalCase on the emitted method
// (categories → QueryCategories).
func (b *MorphedByManyBuilder) InverseName(name string) *MorphedByManyBuilder {
	b.ann.InverseFieldName = name
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
	b.desc.Annotations = polyEdgeAnnotations(*b.ann)
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
