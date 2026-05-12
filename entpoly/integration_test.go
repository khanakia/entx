package entpoly

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"entgo.io/ent/entc/gen"
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
		"func (*Post) MorphKey() string",
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
