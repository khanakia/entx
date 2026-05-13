// Edge-case test file. Each test header references a case number from the
// table at the top of preprocess.go — look there for the broader rationale
// and at the relevant handler for the implementation.
//
// Case index (mirrored from preprocess.go):
//
//   #1  MorphTo with no parents       → TestPreprocess_MorphToWithNoParentsErrors
//   #2  Mixin/edge column mismatch    → TestPreprocess_CustomColumnMismatchSurfacedInError
//   #3  Multiple MorphTo on schema    → TestPreprocess_TwoMorphToOnSameSchema
//   #4  Self-referential poly         → TestPreprocess_SelfReferentialPolymorphic
//   #5  MorphedByMany w/o Through     → TestPreprocess_MorphedByManyWithoutThroughErrors
//   #6  MorphedByMany w/o parent      → (defensive — covered by builder API)
//   #7  Mixin overrides               → TestMorphMixin_OverrideColumnNames, _IntID, _Default*
//   #8  Non-poly edges preserved      → TestPreprocess_KeepsNonPolymorphicEdges (integration_test.go)
//   #9  Parent auto-registration      → TestPreprocess_RegistersAllowedTypesInMorphMap (integration_test.go)
//   #10 No participants → no emit     → TestGenerate_NoParticipantsSkipsEmit (integration_test.go)
package entpoly

import (
	"strings"
	"testing"

	"entgo.io/ent"
	"entgo.io/ent/entc/gen"
)

// ──────────────────────────────────────────────────────────────────────────
// MorphTo edge cases
// ──────────────────────────────────────────────────────────────────────────

// Case #1 — MorphTo declared without any allowed parent types. The
// builder placeholder type would resolve to a non-existent schema and
// ent's graph builder would fail downstream with a confusing error;
// preprocess catches this early with a clear remediation hint.
func TestPreprocess_MorphToWithNoParentsErrors(t *testing.T) {
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: nil, // ← deliberately empty
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{commentEdge}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment},
	}

	err := NewExtension().preprocess(g)
	if err == nil {
		t.Fatal("expected error for MorphTo with no parents, got nil")
	}
	if !strings.Contains(err.Error(), "no allowed parent types") {
		t.Errorf("error should mention no parents; got %q", err.Error())
	}
}

// Case #2 — the user adds an IDColumn override on the edge but forgets
// to mirror it on the mixin (so the mixin emitted the default column
// names while the edge expects custom ones). The error message must
// guide them to the matching MixinIDColumn(...) call.
func TestPreprocess_CustomColumnMismatchSurfacedInError(t *testing.T) {
	// Edge declares IDColumn override, but the mixin (simulated via
	// fields) used the default. preprocess() should mention the
	// MixinIDColumn option in the hint.
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"},
		IDColumn:     "parent_id", // override
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable") // default cols
	comment.Edges = []*gen.Edge{commentEdge}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, {Name: "Post"}},
	}

	err := NewExtension().preprocess(g)
	if err == nil {
		t.Fatal("expected error for column mismatch")
	}
	if !strings.Contains(err.Error(), "MixinIDColumn") {
		t.Errorf("error should mention MixinIDColumn hint; got %q", err.Error())
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Multi-relation: one schema, two MorphTo edges
// ──────────────────────────────────────────────────────────────────────────

// Case #3 — a schema participates in two independent polymorphic
// relations. Each gets its own mixin + edge; preprocess records both
// in Children and strips both edges. The generated polymorphic.go
// emits independent Set/Clear builders for each relation.
func TestPreprocess_TwoMorphToOnSameSchema(t *testing.T) {
	// A schema that doubles as the child of two independent polymorphic
	// relations (e.g. an Audit row that records both the actor and the
	// target as polymorphic refs).
	commentableEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
	})
	imageableEdge := edgeWithMarker(t, "imageable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "imageable",
		AllowedTypes: []string{"Video"},
		IDType:       "string",
	})
	audit := &gen.Type{Name: "Audit"}
	audit = withDiscriminatorFields(audit, "commentable")
	audit = withDiscriminatorFields(audit, "imageable")
	audit.Edges = []*gen.Edge{commentableEdge, imageableEdge}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{audit, {Name: "Post"}, {Name: "Video"}},
	}

	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if len(e.state.Children) != 2 {
		t.Errorf("Children len = %d, want 2 (both relations recorded)", len(e.state.Children))
	}
	if len(audit.Edges) != 0 {
		t.Errorf("both poly edges should be stripped: %v", audit.Edges)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Self-referential polymorphic
// ──────────────────────────────────────────────────────────────────────────

// Case #4 — a polymorphic relation lists its own host type as one of
// the allowed parents (Comment → Comment for threaded replies). The
// host appears as both a child (in Children) and an auto-registered
// parent (in MorphMap); no special handling needed.
func TestPreprocess_SelfReferentialPolymorphic(t *testing.T) {
	// A Comment that can comment on another Comment.
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post", "Comment"}, // self in list
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{commentEdge}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, {Name: "Post"}},
	}

	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	// Comment must appear as both a child and a registered parent in
	// the morph map.
	if len(e.state.Children) != 1 {
		t.Errorf("Children len = %d, want 1", len(e.state.Children))
	}
	if e.state.MorphMap["comment"] != "Comment" {
		t.Errorf("morph map missing self-reference comment→Comment: %v", e.state.MorphMap)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// MorphedByMany validation
// ──────────────────────────────────────────────────────────────────────────

// Case #5 — MorphedByMany declared without the required .Through()
// chained call. The pivot is the routing target for the M2M; without
// it the relation has no implementation, so we surface the omission
// at preprocess with a remediation hint pointing at .Through(...).
func TestPreprocess_MorphedByManyWithoutThroughErrors(t *testing.T) {
	tagEdge := edgeWithMarker(t, "posts", markerAnnotation{
		Kind:      "morphedByMany",
		FieldName: "posts",
		Target:    "Post",
		// Through deliberately empty
	})
	tag := &gen.Type{Name: "Tag", Edges: []*gen.Edge{tagEdge}}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{tag, {Name: "Post"}},
	}

	err := NewExtension().preprocess(g)
	if err == nil {
		t.Fatal("expected error for MorphedByMany without Through")
	}
	if !strings.Contains(err.Error(), "Through") {
		t.Errorf("error should mention Through; got %q", err.Error())
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Multiple parent annotations on one type
// ──────────────────────────────────────────────────────────────────────────

// Companion to case #3 on the parent side — a parent type that hosts
// multiple polymorphic back-references (Post has many Comments AND
// has one featured Image). Each is recorded independently; both
// edges are stripped from t.Edges before ent's templates run.
func TestPreprocess_MultipleParentEdgesOnOneSchema(t *testing.T) {
	// Post hosts both MorphMany("comments") and MorphOne("featured_image").
	commentsEdge := edgeWithMarker(t, "comments", markerAnnotation{
		Kind: "morphMany", FieldName: "comments", Target: "Comment", MorphName: "commentable",
	})
	imageEdge := edgeWithMarker(t, "featured_image", markerAnnotation{
		Kind: "morphOne", FieldName: "featured_image", Target: "Image", MorphName: "imageable",
	})
	post := &gen.Type{Name: "Post", Edges: []*gen.Edge{commentsEdge, imageEdge}}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{post, {Name: "Comment"}, {Name: "Image"}},
	}

	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if len(e.state.Parents) != 2 {
		t.Errorf("Parents len = %d, want 2", len(e.state.Parents))
	}
	if len(post.Edges) != 0 {
		t.Errorf("parent edges not stripped: %v", post.Edges)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Case #7 — MorphMixin column-name overrides
// ──────────────────────────────────────────────────────────────────────────
//
// MixinIDColumn / MixinTypeColumn / MixinIDType change what the mixin
// emits. Mismatches with the corresponding edge overrides are caught
// at preprocess (see case #2); these tests verify the mixin emits
// what it advertises in the first place.

func TestMorphMixin_DefaultColumnNames(t *testing.T) {
	m := MorphMixin("commentable")
	fields := m.Fields()
	if len(fields) != 2 {
		t.Fatalf("Fields len = %d, want 2", len(fields))
	}
	if d := fields[0].Descriptor(); d.Name != "commentable_id" {
		t.Errorf("first field name = %q, want commentable_id", d.Name)
	}
	if d := fields[1].Descriptor(); d.Name != "commentable_type" {
		t.Errorf("second field name = %q, want commentable_type", d.Name)
	}
}

func TestMorphMixin_OverrideColumnNames(t *testing.T) {
	m := MorphMixin("commentable",
		MixinIDColumn("parent_id"),
		MixinTypeColumn("parent_type"),
	)
	fields := m.Fields()
	if d := fields[0].Descriptor(); d.Name != "parent_id" {
		t.Errorf("id field name = %q, want parent_id", d.Name)
	}
	if d := fields[1].Descriptor(); d.Name != "parent_type" {
		t.Errorf("type field name = %q, want parent_type", d.Name)
	}
}

func TestMorphMixin_IntID(t *testing.T) {
	m := MorphMixin("commentable", MixinIDType("int"))
	fields := m.Fields()
	d := fields[0].Descriptor()
	// field.Int64 produces Info.Type == int64; we don't import field
	// here so we check the descriptor's Info.Type stringification.
	if d.Info == nil {
		t.Fatal("Info is nil")
	}
	if d.Info.Type.String() != "int64" {
		t.Errorf("id type = %q, want int64", d.Info.Type.String())
	}
}

func TestMorphMixin_StringIDDefault(t *testing.T) {
	m := MorphMixin("commentable")
	d := m.Fields()[0].Descriptor()
	if d.Info.Type.String() != "string" {
		t.Errorf("default id type = %q, want string", d.Info.Type.String())
	}
}

func TestMorphMixin_FieldsAreNullable(t *testing.T) {
	for _, f := range MorphMixin("commentable").Fields() {
		d := f.Descriptor()
		if !d.Optional {
			t.Errorf("field %q not Optional", d.Name)
		}
		if !d.Nillable {
			t.Errorf("field %q not Nillable", d.Name)
		}
	}
}

// MorphMixin emits a composite index over (type, id) so the back-ref read
// path scales. The index is essentially mandatory — every typed back-ref
// query in polymorphic.go filters on both columns together. Verifies the
// shape (column order: type first, id second) and the opt-out.
func TestMorphMixin_DefaultEmitsCompositeIndex(t *testing.T) {
	m, ok := MorphMixin("commentable").(interface{ Indexes() []ent.Index })
	if !ok {
		t.Fatal("mixin does not implement Indexes()")
	}
	idx := m.Indexes()
	if len(idx) != 1 {
		t.Fatalf("Indexes len = %d, want 1", len(idx))
	}
	cols := idx[0].Descriptor().Fields
	if len(cols) != 2 {
		t.Fatalf("index columns = %d, want 2", len(cols))
	}
	if cols[0] != "commentable_type" || cols[1] != "commentable_id" {
		t.Errorf("index columns = %v, want [commentable_type commentable_id]", cols)
	}
}

func TestMorphMixin_NoIndexOpt(t *testing.T) {
	m := MorphMixin("commentable", MixinNoIndex())
	idx := m.(interface{ Indexes() []ent.Index }).Indexes()
	if len(idx) != 0 {
		t.Errorf("MixinNoIndex() Indexes = %v, want empty", idx)
	}
}

func TestMorphMixin_NoIndexComposeswithAllowed(t *testing.T) {
	// MixinNoIndex must compose with the other options without
	// disabling them — Fields() should still emit the enum-typed
	// type column when MixinAllowed is set alongside.
	m := MorphMixin("commentable",
		MixinAllowed(Post.Type, Video.Type),
		MixinNoIndex(),
	)
	indexer := m.(interface{ Indexes() []ent.Index })
	if got := indexer.Indexes(); len(got) != 0 {
		t.Errorf("Indexes = %v, want empty", got)
	}
	fields := m.Fields()
	if len(fields) != 2 {
		t.Fatalf("Fields len = %d, want 2", len(fields))
	}
	typeField := fields[1].Descriptor()
	if len(typeField.Enums) == 0 {
		t.Error("MixinAllowed should still emit enum values even with MixinNoIndex")
	}
}

func TestMorphMixin_CompositeIndexUsesCustomColumnNames(t *testing.T) {
	m := MorphMixin("commentable",
		MixinIDColumn("parent_id"),
		MixinTypeColumn("parent_type"),
	)
	idx := m.(interface{ Indexes() []ent.Index }).Indexes()
	cols := idx[0].Descriptor().Fields
	if cols[0] != "parent_type" || cols[1] != "parent_id" {
		t.Errorf("custom-cols index = %v, want [parent_type parent_id]", cols)
	}
}

// Case #12 — drift linter. When the mixin emits the type column as
// field.Enum (via MixinAllowed), the set of enum values must match
// the edge's AllowedTypes set. Mismatches surface as a clear diff at
// codegen time.
func TestPreprocess_DriftBetweenMixinAndEdgeErrors(t *testing.T) {
	// Mixin contributed columns for "post" and "video", but the edge's
	// AllowedTypes only mention "post" — Video is in MixinAllowed but
	// NOT in MorphTo.
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"}, // only Post here
		IDType:       "string",
	})
	comment := &gen.Type{
		Name: "Comment",
		Fields: []*gen.Field{
			{Name: "commentable_id"},
			{
				Name: "commentable_type",
				Enums: []gen.Enum{
					{Name: "Post", Value: "post"},
					{Name: "Video", Value: "video"}, // mixin has Video too
				},
			},
		},
		Edges: []*gen.Edge{commentEdge},
	}
	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, {Name: "Post"}, {Name: "Video"}},
	}
	err := NewExtension().preprocess(g)
	if err == nil {
		t.Fatal("expected drift error, got nil")
	}
	if !strings.Contains(err.Error(), "drifted apart") {
		t.Errorf("error should mention drift; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "video") {
		t.Errorf("error should name the missing edge entry %q; got %q", "video", err.Error())
	}
}

func TestPreprocess_AgreementBetweenMixinAndEdgePasses(t *testing.T) {
	// Mixin enum values match edge AllowedTypes exactly — no error.
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post", "Video"},
		IDType:       "string",
	})
	comment := &gen.Type{
		Name: "Comment",
		Fields: []*gen.Field{
			{Name: "commentable_id"},
			{
				Name: "commentable_type",
				Enums: []gen.Enum{
					{Name: "Post", Value: "post"},
					{Name: "Video", Value: "video"},
				},
			},
		},
		Edges: []*gen.Edge{commentEdge},
	}
	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, {Name: "Post"}, {Name: "Video"}},
	}
	if err := NewExtension().preprocess(g); err != nil {
		t.Errorf("agreement should pass without error, got %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// singularise — pivot-name → morph-name default derivation
// ──────────────────────────────────────────────────────────────────────────
//
// MorphedByMany.Through("taggables", ...) auto-derives the morph name
// "taggable" from the singularised table name. The function is
// deliberately a tiny heuristic (-s and -ies rules only); irregular
// plurals require an explicit .MorphName(...) override on the edge.
// These tests pin down the exact behaviour so users can predict the
// derived name without reading the source.

func TestSingularise_HandlesShortInputs(t *testing.T) {
	// Documents the actual heuristic behaviour. The function is
	// deliberately simple (only -s and -ies rules) — users with
	// irregular plurals must override via MorphName().
	cases := []struct{ in, want string }{
		{"", ""},      // empty in, empty out
		{"s", "s"},    // 1-char "s" — too short for the >1 rule, passthrough
		{"as", "a"},   // trailing s with at least 2 chars
		{"ies", "ie"}, // exactly 3 chars: not >3 so falls through to -s rule
		{"bies", "by"},
		{"taggables", "taggable"},
		{"categories", "category"},
	}
	for _, c := range cases {
		if got := singularise(c.in); got != c.want {
			t.Errorf("singularise(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
