package entpoly

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"entgo.io/ent"
	"entgo.io/ent/entc/gen"
	"entgo.io/ent/schema/field"
)

// markerToAnnotations serialises a markerAnnotation through JSON the same
// way ent's pipeline does, so the integration test exercises the decode
// path inside preprocess.go end-to-end.
func markerToAnnotations(t *testing.T, m markerAnnotation) gen.Annotations {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal marker: %v", err)
	}
	var as any
	if err := json.Unmarshal(b, &as); err != nil {
		t.Fatalf("unmarshal marker: %v", err)
	}
	return gen.Annotations{MarkerName: as}
}

// edgeWithMarker constructs a synthetic *gen.Edge carrying the given
// marker annotation. Mirrors the shape ent's loader produces from a
// schema.Edge with annotations attached.
func edgeWithMarker(t *testing.T, name string, m markerAnnotation) *gen.Edge {
	return &gen.Edge{
		Name:        name,
		Annotations: markerToAnnotations(t, m),
	}
}

// withDiscriminatorFields seeds a *gen.Type with the two columns the
// MorphMixin would have produced at schema-load time. preprocess()
// checks for these by name so the test must inject them explicitly.
func withDiscriminatorFields(t *gen.Type, relation string) *gen.Type {
	t.Fields = append(t.Fields,
		&gen.Field{Name: relation + "_id"},
		&gen.Field{Name: relation + "_type"},
	)
	return t
}

func TestPreprocess_StripsMorphToEdge(t *testing.T) {
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		FieldName:    "commentable",
		AllowedTypes: []string{"Post"},
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
	if len(comment.Edges) != 0 {
		t.Errorf("MorphTo edge was not stripped; remaining: %v", comment.Edges)
	}
	if len(e.state.Children) != 1 {
		t.Errorf("Children len = %d, want 1", len(e.state.Children))
	}
}

func TestPreprocess_MissingDiscriminatorErrors(t *testing.T) {
	// MorphTo edge declared but the discriminator fields are missing —
	// this is the "user forgot the MorphMixin" case, surface a clear error.
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
	})
	comment := &gen.Type{Name: "Comment", Edges: []*gen.Edge{commentEdge}}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, {Name: "Post"}},
	}

	err := NewExtension().preprocess(g)
	if err == nil {
		t.Fatal("expected error for missing discriminator columns, got nil")
	}
	if !strings.Contains(err.Error(), "MorphMixin") {
		t.Errorf("error should mention MorphMixin; got %q", err.Error())
	}
}

// Case #9 — every type that appears as an allowed parent of a MorphTo
// is auto-registered in the morph map (snake_case fallback) even when
// the user did not pass an explicit alias via WithMorphMap. Without
// this, the per-parent MorphID/MorphKey methods would be missing for
// any "implicit" parent type and Set<Morph> calls against them would
// fail to compile.
func TestPreprocess_RegistersAllowedTypesInMorphMap(t *testing.T) {
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post", "Video"},
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{commentEdge}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, {Name: "Post"}, {Name: "Video"}},
	}

	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if e.state.MorphMap["post"] != "Post" {
		t.Errorf("morph map missing post → Post (got %v)", e.state.MorphMap)
	}
	if e.state.MorphMap["video"] != "Video" {
		t.Errorf("morph map missing video → Video (got %v)", e.state.MorphMap)
	}
}

func TestPreprocess_ExplicitMorphMapBeatsAutoDerived(t *testing.T) {
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{commentEdge}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, {Name: "Post"}},
	}

	e := NewExtension(WithMorphMap(map[string]string{"article": "Post"}))
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if e.state.MorphMap["article"] != "Post" {
		t.Errorf("explicit alias missing: %v", e.state.MorphMap)
	}
	// Auto-derived "post" must NOT also be there for the same type.
	count := 0
	for _, v := range e.state.MorphMap {
		if v == "Post" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Post appears %d times in morph map, want 1", count)
	}
}

func TestPreprocess_StripsParentEdges(t *testing.T) {
	manyEdge := edgeWithMarker(t, "comments", markerAnnotation{
		Kind:      "morphMany",
		FieldName: "comments",
		Target:    "Comment",
		MorphName: "commentable",
	})
	oneEdge := edgeWithMarker(t, "featured_image", markerAnnotation{
		Kind:      "morphOne",
		FieldName: "featured_image",
		Target:    "Image",
		MorphName: "imageable",
	})
	post := &gen.Type{Name: "Post", Edges: []*gen.Edge{manyEdge, oneEdge}}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{post, {Name: "Comment"}, {Name: "Image"}},
	}

	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if len(post.Edges) != 0 {
		t.Errorf("parent edges not stripped: %v", post.Edges)
	}
	if len(e.state.Parents) != 2 {
		t.Errorf("Parents len = %d, want 2", len(e.state.Parents))
	}
}

func TestPreprocess_StripsHolderEdges(t *testing.T) {
	holderEdge := edgeWithMarker(t, "posts", markerAnnotation{
		Kind:        "morphedByMany",
		FieldName:   "posts",
		Target:      "Post",
		Through:     "Taggable",
		ThroughName: "taggables",
		MorphName:   "taggable",
	})
	tag := &gen.Type{Name: "Tag", Edges: []*gen.Edge{holderEdge}}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{tag, {Name: "Post"}, {Name: "Taggable"}},
	}

	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if len(tag.Edges) != 0 {
		t.Errorf("holder edge not stripped: %v", tag.Edges)
	}
	if len(e.state.Holders) != 1 {
		t.Errorf("Holders len = %d, want 1", len(e.state.Holders))
	}
}

// Case #8 — entpoly must coexist with regular ent edges. preprocess
// strips only edges carrying the marker annotation; anything else
// (edge.To / edge.From declared via standard ent builders) stays in
// t.Edges untouched, so ent's templates emit them normally.
func TestPreprocess_KeepsNonPolymorphicEdges(t *testing.T) {
	regularEdge := &gen.Edge{Name: "author"} // no marker annotation
	polyEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{regularEdge, polyEdge}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, {Name: "Post"}},
	}

	if err := NewExtension().preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if len(comment.Edges) != 1 || comment.Edges[0].Name != "author" {
		t.Errorf("non-polymorphic edge dropped or rearranged: %v", comment.Edges)
	}
}

func TestGenerate_EndToEnd(t *testing.T) {
	tmp := t.TempDir()
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		FieldName:    "commentable",
		AllowedTypes: []string{"Post", "Video"},
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{commentEdge}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent", Target: tmp},
		Nodes:  []*gen.Type{comment, {Name: "Post"}, {Name: "Video"}},
	}

	e := NewExtension(WithMorphMap(map[string]string{
		"post":  "Post",
		"video": "Video",
	}))
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if err := e.generate(g); err != nil {
		t.Fatalf("generate: %v", err)
	}

	out := filepath.Join(tmp, "polymorphic.go")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	// Output must parse as valid Go.
	if _, err := parser.ParseFile(token.NewFileSet(), out, data, 0); err != nil {
		t.Fatalf("output is invalid Go: %v\n----\n%s", err, data)
	}

	src := string(data)
	for _, want := range []string{
		"package ent",
		"type Morphable interface",
		"morphTypeMap",
		`"post"`, `"Post"`,
		"func (e *Post) MorphID() string",
		"func (*Post) MorphKey() MorphKey",
		"func (c *CommentCreate) SetCommentable",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

// Case #10 — a project that imports entpoly but doesn't actually
// declare any polymorphic relations should not get a stray
// polymorphic.go file (it would force them to .gitignore it). The
// hasParticipants check short-circuits the entire sidecar emit.
func TestGenerate_NoParticipantsSkipsEmit(t *testing.T) {
	tmp := t.TempDir()
	g := &gen.Graph{
		Config: &gen.Config{Package: "ent", Target: tmp},
		Nodes:  []*gen.Type{{Name: "Post"}},
	}
	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if err := e.generate(g); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "polymorphic.go")); !os.IsNotExist(err) {
		t.Error("polymorphic.go was created for a graph with no participants")
	}
}

func TestHook_RunsFullPipeline(t *testing.T) {
	tmp := t.TempDir()
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{commentEdge}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent", Target: tmp},
		Nodes:  []*gen.Type{comment, {Name: "Post"}},
	}

	innerCalled := false
	inner := gen.GenerateFunc(func(g *gen.Graph) error {
		innerCalled = true
		// Inner generator sees the stripped graph — verify the edge is gone.
		if len(g.Nodes[0].Edges) != 0 {
			t.Errorf("inner generator saw unstripped graph: %v", g.Nodes[0].Edges)
		}
		return nil
	})

	e := NewExtension(WithMorphMap(map[string]string{"post": "Post"}))
	hook := e.Hooks()[0]
	if err := hook(inner).Generate(g); err != nil {
		t.Fatalf("hook chain: %v", err)
	}
	if !innerCalled {
		t.Error("inner generator was not invoked")
	}
	if _, err := os.Stat(filepath.Join(tmp, "polymorphic.go")); err != nil {
		t.Errorf("sidecar missing: %v", err)
	}
}

// typeWithID is a *gen.Type with a synthetic ID field of the given Go
// type. Used by the per-parent-ID-type tests below to drive
// preprocess's per-target strconv-flavour selection.
func typeWithID(name string, idType field.Type) *gen.Type {
	return &gen.Type{
		Name: name,
		ID:   &gen.Field{Name: "id", Type: &field.TypeInfo{Type: idType}},
	}
}

// typeWithCustomID builds a *gen.Type with a custom Go-typed ID — used
// to exercise the uuid.UUID branch (or any other non-builtin PK).
func typeWithCustomID(name, ident, pkgPath string) *gen.Type {
	return &gen.Type{
		Name: name,
		ID: &gen.Field{
			Name: "id",
			Type: &field.TypeInfo{
				Type:    field.TypeUUID,
				Ident:   ident,
				PkgPath: pkgPath,
			},
		},
	}
}

// TestIDGoType_Builtin verifies the helper returns the canonical
// builtin name for int / int64 / string PKs and leaves PkgPath empty.
func TestIDGoType_Builtin(t *testing.T) {
	cases := []struct {
		name string
		t    field.Type
		want string
	}{
		{"int", field.TypeInt, "int"},
		{"int64", field.TypeInt64, "int64"},
		{"string", field.TypeString, "string"},
	}
	for _, c := range cases {
		gt, pkg := idGoType(typeWithID("X", c.t))
		if gt != c.want {
			t.Errorf("%s: goType = %q, want %q", c.name, gt, c.want)
		}
		if pkg != "" {
			t.Errorf("%s: pkg = %q, want empty (builtin)", c.name, pkg)
		}
	}
}

// TestIDGoType_UUID verifies the helper returns the Ident ("uuid.UUID")
// and the PkgPath ("github.com/google/uuid") for a UUID PK — the two
// pieces the template needs to render the right Go type AND emit the
// import.
func TestIDGoType_UUID(t *testing.T) {
	gt, pkg := idGoType(typeWithCustomID("Document", "uuid.UUID", "github.com/google/uuid"))
	if gt != "uuid.UUID" {
		t.Errorf("UUID goType = %q, want uuid.UUID", gt)
	}
	if pkg != "github.com/google/uuid" {
		t.Errorf("UUID pkgPath = %q, want github.com/google/uuid", pkg)
	}
}

// TestIDGoType_NilDefensiveDefault verifies the helper returns the
// pass-through string default when t is nil or has no ID — the
// downstream resolver branches on the string default for non-int
// non-int64 PKs.
func TestIDGoType_NilDefensiveDefault(t *testing.T) {
	gt, pkg := idGoType(nil)
	if gt != "string" {
		t.Errorf("nil: goType = %q, want string", gt)
	}
	if pkg != "" {
		t.Errorf("nil: pkg = %q, want empty", pkg)
	}

	emptyType := &gen.Type{Name: "Empty"}
	gt, pkg = idGoType(emptyType)
	if gt != "string" || pkg != "" {
		t.Errorf("empty type: got (%q, %q), want (string, \"\")", gt, pkg)
	}
}

// Case #13 — non-builtin parent ID type (uuid.UUID). TestPreprocess_RecordsTargetUUIDIDType verifies that when a MorphTo's
// allowed parent uses uuid.UUID, preprocess captures BOTH the Go-type
// identifier ("uuid.UUID") and the import path so the generated file
// can render both correctly.
func TestPreprocess_RecordsTargetUUIDIDType(t *testing.T) {
	commentEdge := edgeWithMarker(t, "target", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "target",
		AllowedTypes: []string{"Document"},
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Annotation"}, "target")
	comment.Edges = []*gen.Edge{commentEdge}
	doc := typeWithCustomID("Document", "uuid.UUID", "github.com/google/uuid")

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, doc},
	}
	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	rt := e.state.Children[0].ResolveTargets
	if rt[0].IDGoType != "uuid.UUID" {
		t.Errorf("ResolveTargets[0].IDGoType = %q, want uuid.UUID", rt[0].IDGoType)
	}
	if rt[0].IDPkgPath != "github.com/google/uuid" {
		t.Errorf("ResolveTargets[0].IDPkgPath = %q, want github.com/google/uuid", rt[0].IDPkgPath)
	}
}

// Case #15 — SoftDelete() per-parent detection.
// TestPreprocess_SoftDeleteAutoDetectsPerParent verifies that
// HasSoftDelete is set on resolveTargetRef only for targets that
// actually declare the soft-delete field, so the template emits the
// IsNil filter for those parents and skips it for parents that don't
// have the column.
func TestPreprocess_SoftDeleteAutoDetectsPerParent(t *testing.T) {
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:            "morphTo",
		MorphName:       "commentable",
		AllowedTypes:    []string{"Post", "Video"},
		IDType:          "string",
		SoftDelete:      true,
		SoftDeleteField: "deleted_at",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{commentEdge}

	// Post HAS deleted_at, Video does NOT.
	post := &gen.Type{
		Name: "Post",
		ID:   &gen.Field{Name: "id", Type: &field.TypeInfo{Type: field.TypeInt}},
		Fields: []*gen.Field{
			{Name: "title"},
			{Name: "deleted_at"},
		},
	}
	video := typeWithID("Video", field.TypeInt)

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, post, video},
	}
	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	rt := e.state.Children[0].ResolveTargets
	byName := map[string]bool{}
	for _, r := range rt {
		byName[r.SchemaName] = r.HasSoftDelete
	}
	if !byName["Post"] {
		t.Error("Post has deleted_at, HasSoftDelete should be true")
	}
	if byName["Video"] {
		t.Error("Video has NO deleted_at, HasSoftDelete should be false")
	}
}

// TestPreprocess_SoftDeleteOffWhenFlagNotSet — even if a parent has
// the field, no filter should activate unless .SoftDelete() was
// explicitly opted into on the MorphTo.
func TestPreprocess_SoftDeleteOffWhenFlagNotSet(t *testing.T) {
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
		// SoftDelete intentionally false
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{commentEdge}
	post := &gen.Type{
		Name:   "Post",
		ID:     &gen.Field{Name: "id", Type: &field.TypeInfo{Type: field.TypeInt}},
		Fields: []*gen.Field{{Name: "deleted_at"}},
	}
	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, post},
	}
	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if e.state.Children[0].ResolveTargets[0].HasSoftDelete {
		t.Error("HasSoftDelete should be false when MorphTo.SoftDelete is not set")
	}
}

// Case #17 — GQL() propagates through preprocess.
func TestPreprocess_GQLFlagFlowsThrough(t *testing.T) {
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
		GQL:          true,
		GQLUnionName: "PostUnion",
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
	if !e.state.Children[0].GQL {
		t.Error("GQL flag did not flow through to childInfo")
	}
	if e.state.Children[0].GQLUnionName != "PostUnion" {
		t.Errorf("GQLUnionName = %q, want PostUnion", e.state.Children[0].GQLUnionName)
	}
}

// TestBuildTmplData_GQLChildrenFilteredAndDefaultName — verifies the
// filter that splits Children → GQLChildren AND that the default
// union name is PascalCase(MorphName) when GQLUnionName is empty.
func TestBuildTmplData_GQLChildrenFilteredAndDefaultName(t *testing.T) {
	e := NewExtension()
	e.state = &polyState{
		Package: "ent",
		Children: []childInfo{
			{
				TypeName:     "Comment",
				MorphName:    "commentable",
				IDColumn:     "commentable_id",
				TypeColumn:   "commentable_type",
				IDType:       "string",
				AllowedTypes: []string{"Post"},
				GQL:          true,
				// GQLUnionName empty → should default to "Commentable"
			},
			{
				TypeName:     "Image",
				MorphName:    "imageable",
				IDColumn:     "imageable_id",
				TypeColumn:   "imageable_type",
				IDType:       "string",
				AllowedTypes: []string{"Post"},
				GQL:          false, // should be filtered out
			},
		},
		MorphMap: map[string]string{"post": "Post"},
	}
	d, err := e.buildTmplData()
	if err != nil {
		t.Fatalf("buildTmplData: %v", err)
	}
	if !d.HasGQL {
		t.Error("HasGQL = false, want true")
	}
	if len(d.GQLChildren) != 1 {
		t.Fatalf("GQLChildren len = %d, want 1", len(d.GQLChildren))
	}
	if d.GQLChildren[0].GQLUnionName != "Commentable" {
		t.Errorf("default GQLUnionName = %q, want Commentable", d.GQLChildren[0].GQLUnionName)
	}
}

// TestWriteGQLSchema_SingleUnion verifies the simplest emit shape.
func TestWriteGQLSchema_SingleUnion(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "polymorphic.graphql")
	children := []childData{
		{GQLUnionName: "Commentable", AllowedTypes: []string{"Post", "Video"}},
	}
	if err := writeGQLSchema(path, children); err != nil {
		t.Fatalf("writeGQLSchema: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "union Commentable = Post | Video") {
		t.Errorf("output missing expected union line:\n%s", got)
	}
}

// TestWriteGQLSchema_MultipleUnions — two relations produce two
// lines in iteration order (the caller is responsible for sorting,
// which buildTmplData does via the Children iteration order).
func TestWriteGQLSchema_MultipleUnions(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "polymorphic.graphql")
	children := []childData{
		{GQLUnionName: "Commentable", AllowedTypes: []string{"Post", "Video"}},
		{GQLUnionName: "Imageable", AllowedTypes: []string{"Post"}},
	}
	if err := writeGQLSchema(path, children); err != nil {
		t.Fatalf("writeGQLSchema: %v", err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	for _, want := range []string{
		"union Commentable = Post | Video",
		"union Imageable = Post",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

// TestWriteGQLSchema_CustomUnionName verifies the override path —
// GQLUnionName is what lands in the schema, not the relation name.
func TestWriteGQLSchema_CustomUnionName(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "polymorphic.graphql")
	children := []childData{
		{GQLUnionName: "PostOrVideo", AllowedTypes: []string{"Post", "Video"}},
	}
	if err := writeGQLSchema(path, children); err != nil {
		t.Fatalf("writeGQLSchema: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "union PostOrVideo = Post | Video") {
		t.Errorf("custom union name not used:\n%s", got)
	}
	if strings.Contains(string(got), "union Commentable") {
		t.Errorf("output should not include the default name:\n%s", got)
	}
}

// TestWriteGQLSchema_DeterministicOutput — same input, byte-identical
// output. Important for codegen-friendly diffs.
func TestWriteGQLSchema_DeterministicOutput(t *testing.T) {
	tmp := t.TempDir()
	p1 := filepath.Join(tmp, "a.graphql")
	p2 := filepath.Join(tmp, "b.graphql")
	children := []childData{
		{GQLUnionName: "Commentable", AllowedTypes: []string{"Post", "Video"}},
		{GQLUnionName: "Imageable", AllowedTypes: []string{"Post"}},
	}
	if err := writeGQLSchema(p1, children); err != nil {
		t.Fatal(err)
	}
	if err := writeGQLSchema(p2, children); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(p1)
	b, _ := os.ReadFile(p2)
	if string(a) != string(b) {
		t.Errorf("non-deterministic output:\n%s\n---\n%s", a, b)
	}
}

// Case #14 — Cascade() propagates through preprocess. TestPreprocess_CascadeFlagFlowsThrough verifies the .Cascade()
// builder option lands in childInfo so the template's cascade-hook
// emission picks it up.
func TestPreprocess_CascadeFlagFlowsThrough(t *testing.T) {
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
		Cascade:      true,
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
	if !e.state.Children[0].Cascade {
		t.Error("Cascade flag did not flow through to childInfo")
	}
}

// Case #11 — Ghost FK columns left behind by ent's edge processor
// after our edge strip. TestPreprocess_StripsGhostForeignKeys verifies
// that the ForeignKeys
// + Fields cleanup pass removes ent's auto-added FK column entries
// that came from a now-stripped polymorphic edge. Without this pass,
// the generated Comment struct would carry leftover unexported fields
// like `post_comments *int` for every parent declaring MorphMany on
// Comment — cosmetic clutter that confuses readers of the generated
// code.
func TestPreprocess_StripsGhostForeignKeys(t *testing.T) {
	// Comment is the polymorphic child here. We seed it with a ghost
	// FK entry whose Edge carries our marker — exactly the shape ent
	// produces for an edge.To(Comment.Type) on a parent type before
	// our preprocess runs.
	ghostEdge := &gen.Edge{
		Name:        "post_comments",
		Annotations: markerToAnnotations(t, markerAnnotation{Kind: "morphMany"}),
	}
	ghostField := &gen.Field{Name: "post_comments"}
	comment := withDiscriminatorFields(&gen.Type{
		Name:        "Comment",
		ForeignKeys: []*gen.ForeignKey{{Field: ghostField, Edge: ghostEdge}},
		Fields:      []*gen.Field{ghostField},
	}, "commentable")
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
	})
	comment.Edges = []*gen.Edge{commentEdge}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, {Name: "Post"}},
	}
	if err := NewExtension().preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if len(comment.ForeignKeys) != 0 {
		t.Errorf("ghost FK not stripped: %+v", comment.ForeignKeys)
	}
	for _, f := range comment.Fields {
		if f.Name == "post_comments" {
			t.Errorf("ghost FK field not stripped from Fields: %s", f.Name)
		}
	}
}

// TestPreprocess_KeepsNonPolyForeignKeys is the safety net for the
// strip pass — only FKs whose Edge carries our marker get removed.
// Regular ent FK columns (from a true edge.To) must survive.
func TestPreprocess_KeepsNonPolyForeignKeys(t *testing.T) {
	// FK whose Edge has NO marker (a real, non-polymorphic edge).
	realEdge := &gen.Edge{Name: "author"}
	realField := &gen.Field{Name: "author_id"}
	comment := withDiscriminatorFields(&gen.Type{
		Name:        "Comment",
		ForeignKeys: []*gen.ForeignKey{{Field: realField, Edge: realEdge}},
		Fields:      []*gen.Field{realField},
	}, "commentable")
	polyEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
	})
	comment.Edges = []*gen.Edge{polyEdge, realEdge}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, {Name: "Post"}},
	}
	if err := NewExtension().preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if len(comment.ForeignKeys) != 1 || comment.ForeignKeys[0].Field.Name != "author_id" {
		t.Errorf("real FK was incorrectly stripped: %+v", comment.ForeignKeys)
	}
}

// TestPreprocess_RecordsTargetIDGoTypeString verifies that when a
// MorphTo's allowed parent has a string ID (the UUID / ULID case),
// preprocess records "string" in childInfo.ResolveTargets — which
// the template uses to skip strconv entirely and pass the morph id
// through unchanged into the parent's Get(ctx, id) call.
func TestPreprocess_RecordsTargetIDGoTypeString(t *testing.T) {
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"User"},
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{commentEdge}
	user := typeWithID("User", field.TypeString)

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, user},
	}
	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if len(e.state.Children) != 1 {
		t.Fatalf("Children len = %d, want 1", len(e.state.Children))
	}
	rt := e.state.Children[0].ResolveTargets
	if len(rt) != 1 || rt[0].IDGoType != "string" {
		t.Errorf("ResolveTargets[0].IDGoType = %v, want string", rt)
	}
}

// TestPreprocess_RecordsTargetIDGoTypeInt64 covers the int64 PK
// path. The template's strconv branch picks ParseInt over Atoi when
// this string is "int64".
func TestPreprocess_RecordsTargetIDGoTypeInt64(t *testing.T) {
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"BigPost"},
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{commentEdge}
	bigPost := typeWithID("BigPost", field.TypeInt64)

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, bigPost},
	}
	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	rt := e.state.Children[0].ResolveTargets
	if rt[0].IDGoType != "int64" {
		t.Errorf("IDGoType = %q, want int64", rt[0].IDGoType)
	}
}

// TestPreprocess_MixedAllowedTypesRecordedSeparately verifies the
// per-parent ID typing — each allowed parent's ID type is recorded
// independently, so a polymorphic relation referencing both int and
// string parents emits the correct strconv flavour per branch.
func TestPreprocess_MixedAllowedTypesRecordedSeparately(t *testing.T) {
	commentEdge := edgeWithMarker(t, "commentable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "commentable",
		AllowedTypes: []string{"Post", "User"},
		IDType:       "string",
	})
	comment := withDiscriminatorFields(&gen.Type{Name: "Comment"}, "commentable")
	comment.Edges = []*gen.Edge{commentEdge}
	post := typeWithID("Post", field.TypeInt)
	user := typeWithID("User", field.TypeString)

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{comment, post, user},
	}
	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	rt := e.state.Children[0].ResolveTargets
	if len(rt) != 2 {
		t.Fatalf("ResolveTargets len = %d, want 2", len(rt))
	}
	gotByName := map[string]string{}
	for _, r := range rt {
		gotByName[r.SchemaName] = r.IDGoType
	}
	if gotByName["Post"] != "int" {
		t.Errorf("Post IDGoType = %q, want int", gotByName["Post"])
	}
	if gotByName["User"] != "string" {
		t.Errorf("User IDGoType = %q, want string", gotByName["User"])
	}
}

// TestRender_TypeIsEnumFalse_OmitsPredicateCast — regression for the
// "comment.CommentableType used as a cast but it's the predicate-EQ
// shortcut function" bug. When MorphMixin is used without MixinAllowed,
// the type column is a plain string and ent does NOT emit a named
// string type for it — `comment.CommentableType` resolves to the
// predicate constructor, so wrapping a value with it is a function
// call, not a cast. The template must omit the wrap in that case.
func TestRender_TypeIsEnumFalse_OmitsPredicateCast(t *testing.T) {
	e := NewExtension()
	e.state = &polyState{
		Package: "ent",
		MorphMap: map[string]string{"post": "Post"},
		Children: []childInfo{{
			TypeName:      "Comment",
			MorphName:     "commentable",
			IDColumn:      "commentable_id",
			TypeColumn:    "commentable_type",
			IDType:        "string",
			TypeIsEnum:    false,
			ChildIDGoType: "string",
			AllowedTypes:  []string{"Post"},
			GQL:           true,
			ResolveTargets: []resolveTargetRef{
				{SchemaName: "Post", IDGoType: "string"},
			},
		}},
	}
	d, err := e.buildTmplData()
	if err != nil {
		t.Fatalf("buildTmplData: %v", err)
	}
	var buf strings.Builder
	if err := polyTmpl.ExecuteTemplate(&buf, "file", d); err != nil {
		t.Fatalf("template execute: %v", err)
	}
	out := buf.String()
	// Negative assertion: the buggy cast must not appear anywhere.
	if strings.Contains(out, "comment.CommentableType(string(") {
		t.Errorf("rendered template contains buggy cast `comment.CommentableType(string(...))` when TypeIsEnum=false")
	}
	// Positive assertion: the un-wrapped form must appear.
	if !strings.Contains(out, "Set"+"CommentableType(string(p.MorphKey()))") {
		t.Errorf("rendered template missing the un-wrapped SetCommentableType(string(p.MorphKey())) form")
	}
	// Sanity-check the file actually parses as Go syntax.
	if _, perr := parser.ParseFile(token.NewFileSet(), "polymorphic.go", out, parser.AllErrors); perr != nil {
		t.Errorf("rendered template does not parse as Go: %v", perr)
	}
}

// TestRender_TypeIsEnumTrue_KeepsCast — companion to the regression test
// above: when MixinAllowed *is* used, the named-string-type cast must
// still be emitted (the enum-vs-MorphKey two-named-types case).
func TestRender_TypeIsEnumTrue_KeepsCast(t *testing.T) {
	e := NewExtension()
	e.state = &polyState{
		Package: "ent",
		MorphMap: map[string]string{"post": "Post"},
		Children: []childInfo{{
			TypeName:     "Comment",
			MorphName:    "commentable",
			IDColumn:     "commentable_id",
			TypeColumn:   "commentable_type",
			IDType:       "string",
			TypeIsEnum:   true,
			AllowedTypes: []string{"Post"},
			ResolveTargets: []resolveTargetRef{
				{SchemaName: "Post", IDGoType: "string"},
			},
		}},
	}
	d, err := e.buildTmplData()
	if err != nil {
		t.Fatalf("buildTmplData: %v", err)
	}
	var buf strings.Builder
	if err := polyTmpl.ExecuteTemplate(&buf, "file", d); err != nil {
		t.Fatalf("template execute: %v", err)
	}
	if !strings.Contains(buf.String(), "comment.CommentableType(string(p.MorphKey()))") {
		t.Errorf("rendered template missing enum-mode cast `comment.CommentableType(string(p.MorphKey()))`")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Render matrix — every meaningful axis of the template, parsed back as Go
// ──────────────────────────────────────────────────────────────────────────
//
// Background: the .GQL() bug shipped because every example schema enabled
// MixinAllowed, hiding the plain-string code path. To prevent the same
// shape of miss for any other axis the template branches on, the test
// below renders the template across a deliberately broad combination of
// inputs and asserts each result PARSES as Go and is free of the
// known-bad cast pattern when TypeIsEnum=false.
//
// "Parses as Go" is a weak check — it does not prove the output type-
// checks against a real ent package — but it catches every kind of bug
// the previous tests missed (missing tokens, malformed cast, unbalanced
// braces). For full type-check coverage extend examples/* with a schema
// exercising each new axis combination.

// renderState executes the embedded template against a polyState and
// returns the rendered source. Centralised so each matrix case is a
// one-liner.
func renderState(t *testing.T, s *polyState) string {
	t.Helper()
	e := NewExtension()
	e.state = s
	d, err := e.buildTmplData()
	if err != nil {
		t.Fatalf("buildTmplData: %v", err)
	}
	var buf strings.Builder
	if err := polyTmpl.ExecuteTemplate(&buf, "file", d); err != nil {
		t.Fatalf("template execute: %v", err)
	}
	return buf.String()
}

// assertParsesAsGo fails the test if the rendered output is not a valid
// Go source file. Dumps the offending output to a temp file on failure
// so the developer can read what the template produced.
func assertParsesAsGo(t *testing.T, name, src string) {
	t.Helper()
	if _, err := parser.ParseFile(token.NewFileSet(), "polymorphic.go", src, parser.AllErrors); err != nil {
		dump := filepath.Join(t.TempDir(), name+".go")
		_ = os.WriteFile(dump, []byte(src), 0o644)
		t.Fatalf("rendered template does not parse (%s): %v — dump: %s", name, err, dump)
	}
}

// assertNoBadCast catches the specific pattern that shipped as the
// .GQL() bug: <ident>.<TypeField>(string(...) wrapping a value when
// TypeIsEnum=false. The cast pattern is unique enough that scanning for
// the substring is reliable.
func assertNoBadCast(t *testing.T, name, src, ident, typeField string) {
	t.Helper()
	bad := ident + "." + typeField + "(string("
	if strings.Contains(src, bad) {
		t.Errorf("%s: rendered template contains forbidden predicate-as-cast %q", name, bad)
	}
}

// minimalChild returns a childInfo with sane defaults that produces a
// renderable polyState. Tests override individual fields.
func minimalChild() childInfo {
	return childInfo{
		TypeName:      "Comment",
		MorphName:     "commentable",
		IDColumn:      "commentable_id",
		TypeColumn:    "commentable_type",
		IDType:        "string",
		ChildIDGoType: "string",
		AllowedTypes:  []string{"Post"},
		ResolveTargets: []resolveTargetRef{
			{SchemaName: "Post", IDGoType: "string"},
		},
	}
}

// TestRender_Matrix_ChildAxes — for every meaningful axis on a child
// declaration, render with that axis flipped both ways and assert the
// output is valid Go. When TypeIsEnum=false, also assert the buggy
// predicate-as-cast does not appear anywhere.
func TestRender_Matrix_ChildAxes(t *testing.T) {
	type tweak func(*childInfo)
	axes := map[string]tweak{
		"baseline":         func(c *childInfo) {},
		"GQL":              func(c *childInfo) { c.GQL = true; c.GQLUnionName = "Commentable" },
		"Required":         func(c *childInfo) { c.Required = true },
		"Touch":            func(c *childInfo) { c.Touch = true; c.TouchField = "updated_at" },
		"Cascade":          func(c *childInfo) { c.Cascade = true },
		"SoftDelete":       func(c *childInfo) { c.SoftDelete = true; c.SoftDeleteField = "deleted_at"; c.ResolveTargets[0].HasSoftDelete = true },
		"IDInt":            func(c *childInfo) { c.IDType = "int" },
		"ChildIDInt":       func(c *childInfo) { c.ChildIDGoType = "int" },
		"ChildIDInt64":     func(c *childInfo) { c.ChildIDGoType = "int64" },
		"ChildIDUUID":      func(c *childInfo) { c.ChildIDGoType = "uuid.UUID"; c.ChildIDPkgPath = "github.com/google/uuid" },
		"ParentIDInt":      func(c *childInfo) { c.ResolveTargets[0].IDGoType = "int" },
		"ParentIDInt64":    func(c *childInfo) { c.ResolveTargets[0].IDGoType = "int64" },
		"ParentIDUUID":     func(c *childInfo) { c.ResolveTargets[0].IDGoType = "uuid.UUID"; c.ResolveTargets[0].IDPkgPath = "github.com/google/uuid" },
		"AllFlags": func(c *childInfo) {
			c.GQL, c.GQLUnionName = true, "Commentable"
			c.Required = true
			c.Touch, c.TouchField = true, "updated_at"
			c.Cascade = true
			c.SoftDelete, c.SoftDeleteField = true, "deleted_at"
			c.ResolveTargets[0].HasSoftDelete = true
		},
	}

	for _, typeIsEnum := range []bool{true, false} {
		for axisName, apply := range axes {
			name := axisName
			if typeIsEnum {
				name += "_Enum"
			} else {
				name += "_String"
			}
			t.Run(name, func(t *testing.T) {
				c := minimalChild()
				c.TypeIsEnum = typeIsEnum
				apply(&c)
				s := &polyState{
					Package:  "ent",
					MorphMap: map[string]string{"post": "Post"},
					Children: []childInfo{c},
				}
				out := renderState(t, s)
				assertParsesAsGo(t, name, out)
				if !typeIsEnum {
					assertNoBadCast(t, name, out, "comment", "CommentableType")
				}
			})
		}
	}
}

// TestRender_Matrix_MultipleAllowedTypes — exercise the per-parent
// ResolveCases loop (switch arms, eager-load buckets, cascade hook
// per-allowed-parent emission). Each parent gets a different ID Go
// type so every strconv branch in the template renders.
func TestRender_Matrix_MultipleAllowedTypes(t *testing.T) {
	for _, typeIsEnum := range []bool{true, false} {
		name := "MixedParentIDs_String"
		if typeIsEnum {
			name = "MixedParentIDs_Enum"
		}
		t.Run(name, func(t *testing.T) {
			c := minimalChild()
			c.TypeIsEnum = typeIsEnum
			c.AllowedTypes = []string{"Post", "Video", "Doc", "Note"}
			c.ResolveTargets = []resolveTargetRef{
				{SchemaName: "Post", IDGoType: "int"},
				{SchemaName: "Video", IDGoType: "int64"},
				{SchemaName: "Doc", IDGoType: "string"},
				{SchemaName: "Note", IDGoType: "uuid.UUID", IDPkgPath: "github.com/google/uuid"},
			}
			c.Cascade = true
			c.GQL, c.GQLUnionName = true, "Commentable"
			s := &polyState{
				Package: "ent",
				MorphMap: map[string]string{
					"post":  "Post",
					"video": "Video",
					"doc":   "Doc",
					"note":  "Note",
				},
				Children: []childInfo{c},
			}
			out := renderState(t, s)
			assertParsesAsGo(t, name, out)
			if !typeIsEnum {
				assertNoBadCast(t, name, out, "comment", "CommentableType")
			}
		})
	}
}

// TestRender_Matrix_ParentAndHolder — the bug had cousin sites on the
// MorphOne/MorphMany (parentInfo) and MorphedByMany (holderInfo) back-
// refs. Same axis matters there too — the back-ref's typed predicate
// uses the same <ident>.<TypeField>(string(...)) shape.
func TestRender_Matrix_ParentAndHolder(t *testing.T) {
	for _, typeIsEnum := range []bool{true, false} {
		name := "ParentMorphOne_String"
		if typeIsEnum {
			name = "ParentMorphOne_Enum"
		}
		t.Run(name, func(t *testing.T) {
			c := minimalChild()
			c.TypeName = "Image"
			c.MorphName = "imageable"
			c.IDColumn = "imageable_id"
			c.TypeColumn = "imageable_type"
			c.TypeIsEnum = typeIsEnum
			c.ResolveTargets = []resolveTargetRef{{SchemaName: "Post", IDGoType: "int"}}
			s := &polyState{
				Package:  "ent",
				MorphMap: map[string]string{"post": "Post"},
				Children: []childInfo{c},
				Parents: []parentInfo{{
					ParentName: "Post",
					FieldName:  "featured_image",
					Target:     "Image",
					MorphName:  "imageable",
					Kind:       "morphOne",
					TypeIsEnum: typeIsEnum,
				}, {
					ParentName: "Post",
					FieldName:  "comments",
					Target:     "Image",
					MorphName:  "imageable",
					Kind:       "morphMany",
					TypeIsEnum: typeIsEnum,
				}},
			}
			out := renderState(t, s)
			assertParsesAsGo(t, name, out)
			if !typeIsEnum {
				// The bad pattern would be `image.ImageableType(string(`.
				assertNoBadCast(t, name, out, "image", "ImageableType")
			}
		})
	}

	for _, typeIsEnum := range []bool{true, false} {
		name := "HolderMorphedByMany_String"
		if typeIsEnum {
			name = "HolderMorphedByMany_Enum"
		}
		t.Run(name, func(t *testing.T) {
			s := &polyState{
				Package:  "ent",
				MorphMap: map[string]string{"post": "Post"},
				Children: []childInfo{minimalChild()}, // need a child for Comment imports
				Holders: []holderInfo{{
					HolderName:       "Tag",
					FieldName:        "posts",
					InverseFieldName: "tags",
					Target:           "Post",
					Pivot:            "Taggable",
					MorphName:        "taggable",
					TargetIDGoType:   "int",
					HolderIDGoType:   "int",
					TypeIsEnum:       typeIsEnum,
				}},
			}
			out := renderState(t, s)
			assertParsesAsGo(t, name, out)
			if !typeIsEnum {
				// Bad pattern on pivot: `taggable.TaggableType(string(`.
				assertNoBadCast(t, name, out, "taggable", "TaggableType")
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────
// MorphedByMany morph-name resolution (regression: Through() defaulted
// to singularise(table) even when the pivot had its own MorphTo).
// ──────────────────────────────────────────────────────────────────────────

// TestPreprocess_MorphedByMany_ResolvesMorphNameFromPivot is the
// regression guard for the Through() defaulting bug. The pivot is
// SourceLink with MorphTo("sourceable", ...). A holder declares
// MorphedByMany without calling .MorphName(...), and Through() supplies
// a table name ("source_links") whose singularise default ("source_link")
// is WRONG — the right morph name is "sourceable", taken from the
// pivot's MorphTo. preprocess must pick the latter so the back-ref's
// column accessors (PivotIDField / PivotTypeField) name the columns
// SourceLink actually has.
func TestPreprocess_MorphedByMany_ResolvesMorphNameFromPivot(t *testing.T) {
	// Pivot: SourceLink with MorphTo("sourceable", Post.Type)
	pivotMorphTo := edgeWithMarker(t, "sourceable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "sourceable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
	})
	sourceLink := withDiscriminatorFields(&gen.Type{Name: "SourceLink"}, "sourceable")
	sourceLink.Edges = []*gen.Edge{pivotMorphTo}

	// Holder: Source with MorphedByMany("decisions", Post.Type).Through("source_links", SourceLink.Type)
	// — note the deliberate omission of .MorphName(...); the test
	// asserts the pivot's MorphTo wins.
	holderMbM := edgeWithMarker(t, "decisions", markerAnnotation{
		Kind:        "morphedByMany",
		FieldName:   "decisions",
		Target:      "Post",
		Through:     "SourceLink",
		ThroughName: "source_links",
		// MorphName intentionally empty.
	})
	source := &gen.Type{Name: "Source", Edges: []*gen.Edge{holderMbM}}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{source, sourceLink, {Name: "Post"}},
	}
	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if len(e.state.Holders) != 1 {
		t.Fatalf("Holders len = %d, want 1", len(e.state.Holders))
	}
	h := e.state.Holders[0]
	// The fix: morph name comes from the pivot's MorphTo, NOT from
	// singularise(ThroughName).
	if got := h; false {
		_ = got
	}
	// Inspect what handleHolder recorded — the pivot column-method
	// names are derived from m.MorphName at preprocess time. We
	// cross-check via the rendered tmplData since holderInfo doesn't
	// expose the resolved MorphName directly.
	d, err := e.buildTmplData()
	if err != nil {
		t.Fatalf("buildTmplData: %v", err)
	}
	if len(d.Holders) != 1 {
		t.Fatalf("Holders in tmplData = %d, want 1", len(d.Holders))
	}
	hd := d.Holders[0]
	if hd.PivotIDField != "SourceableID" {
		t.Errorf("PivotIDField = %q, want SourceableID (resolved from pivot MorphTo)", hd.PivotIDField)
	}
	if hd.PivotTypeField != "SourceableType" {
		t.Errorf("PivotTypeField = %q, want SourceableType (resolved from pivot MorphTo)", hd.PivotTypeField)
	}
}

// TestPreprocess_MorphedByMany_ExplicitMorphNameWins guarantees the
// pivot lookup never overrides an explicit caller decision. If the user
// chained .MorphName("custom") the resolved name must be "custom",
// regardless of what the pivot's MorphTo says.
func TestPreprocess_MorphedByMany_ExplicitMorphNameWins(t *testing.T) {
	pivotMorphTo := edgeWithMarker(t, "sourceable", markerAnnotation{
		Kind:         "morphTo",
		MorphName:    "sourceable",
		AllowedTypes: []string{"Post"},
		IDType:       "string",
	})
	pivot := withDiscriminatorFields(&gen.Type{Name: "Pivot"}, "sourceable")
	pivot.Edges = []*gen.Edge{pivotMorphTo}

	holderMbM := edgeWithMarker(t, "items", markerAnnotation{
		Kind:        "morphedByMany",
		FieldName:   "items",
		Target:      "Post",
		Through:     "Pivot",
		ThroughName: "source_links",
		MorphName:   "custom", // explicit; must win over pivot's "sourceable"
	})
	holder := &gen.Type{Name: "Holder", Edges: []*gen.Edge{holderMbM}}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{holder, pivot, {Name: "Post"}},
	}
	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	d, _ := e.buildTmplData()
	hd := d.Holders[0]
	if hd.PivotIDField != "CustomID" {
		t.Errorf("PivotIDField = %q, want CustomID (explicit MorphName wins)", hd.PivotIDField)
	}
}

// TestPreprocess_MorphedByMany_FallbackToSingularise covers the third
// branch in the resolution chain: no explicit MorphName, pivot type has
// no MorphTo declared — fall back to singularise(ThroughName) so the
// pre-fix behavior is preserved for users who relied on it.
func TestPreprocess_MorphedByMany_FallbackToSingularise(t *testing.T) {
	// Pivot without MorphTo (e.g. plain join table that entpoly didn't
	// emit fields for — defensive case).
	pivot := &gen.Type{Name: "Taggable"}

	holderMbM := edgeWithMarker(t, "posts", markerAnnotation{
		Kind:        "morphedByMany",
		FieldName:   "posts",
		Target:      "Post",
		Through:     "Taggable",
		ThroughName: "taggables",
		// MorphName empty.
	})
	tag := &gen.Type{Name: "Tag", Edges: []*gen.Edge{holderMbM}}

	g := &gen.Graph{
		Config: &gen.Config{Package: "ent"},
		Nodes:  []*gen.Type{tag, pivot, {Name: "Post"}},
	}
	e := NewExtension()
	if err := e.preprocess(g); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	d, _ := e.buildTmplData()
	hd := d.Holders[0]
	if hd.PivotIDField != "TaggableID" {
		t.Errorf("PivotIDField = %q, want TaggableID (singularise fallback)", hd.PivotIDField)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// MixinIndexName — composite index storage-key override
// ──────────────────────────────────────────────────────────────────────────

// TestMixinIndexName_StorageKeyApplied is the regression guard for the
// cross-module index-name collision. The mixin's composite index now
// accepts a storage-key override so two modules sharing a database can
// each pick a unique index name (default ent name is derived from the
// entity, which collides when both modules declare the same Go-named
// entity).
func TestMixinIndexName_StorageKeyApplied(t *testing.T) {
	m, ok := MorphMixin("taggable",
		MixinIndexName("media_tags_taggable_type_taggable_id"),
	).(interface{ Indexes() []ent.Index })
	if !ok {
		t.Fatal("mixin does not implement Indexes()")
	}
	idxs := m.Indexes()
	if len(idxs) != 1 {
		t.Fatalf("Indexes() len = %d, want 1", len(idxs))
	}
	desc := idxs[0].Descriptor()
	if desc.StorageKey != "media_tags_taggable_type_taggable_id" {
		t.Errorf("index StorageKey = %q, want media_tags_taggable_type_taggable_id", desc.StorageKey)
	}
}

// TestMixinIndexName_DefaultUnset verifies the option is opt-in: a
// mixin built without MixinIndexName leaves StorageKey empty so ent's
// default naming continues to apply for everyone who isn't hitting the
// cross-module collision.
func TestMixinIndexName_DefaultUnset(t *testing.T) {
	m, _ := MorphMixin("taggable").(interface{ Indexes() []ent.Index })
	idxs := m.Indexes()
	if idxs[0].Descriptor().StorageKey != "" {
		t.Errorf("default StorageKey = %q, want empty (ent picks the name)", idxs[0].Descriptor().StorageKey)
	}
}
