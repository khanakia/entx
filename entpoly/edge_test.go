package entpoly

import "testing"

// fakeSchema is a stand-in for an ent schema struct so the reflect-based
// schemaName helper has something with the right shape to inspect. Each
// test uses its own zero-arg method to mimic the Post.Type idiom users
// supply at call sites.
type fakeSchema struct{}

func (fakeSchema) Type() {}

// Methods with the same Type signature on differently-named types are
// what schemaName(...) discriminates between. The receiver type name is
// the schema name.
type Post struct{}
type Video struct{}
type Image struct{}

func (Post) Type()  {}
func (Video) Type() {}
func (Image) Type() {}

func TestSchemaName_FromMethodValue(t *testing.T) {
	if got := schemaName(Post.Type); got != "Post" {
		t.Errorf("schemaName(Post.Type) = %q, want Post", got)
	}
	if got := schemaName(Video.Type); got != "Video" {
		t.Errorf("schemaName(Video.Type) = %q, want Video", got)
	}
}

func TestSchemaName_NonMethodValueReturnsEmpty(t *testing.T) {
	if got := schemaName(42); got != "" {
		t.Errorf("schemaName(42) = %q, want empty", got)
	}
	if got := schemaName(nil); got != "" {
		t.Errorf("schemaName(nil) = %q, want empty", got)
	}
}

func TestMorphTo_RecordsAllowedParents(t *testing.T) {
	b := MorphTo("commentable", Post.Type, Video.Type)
	d := b.Descriptor()
	if d.Name != "commentable" {
		t.Errorf("Name = %q, want commentable", d.Name)
	}
	if len(d.Annotations) != 1 {
		t.Fatalf("Annotations len = %d, want 1", len(d.Annotations))
	}
	m, ok := decodeMarker(d.Annotations)
	if !ok {
		t.Fatal("marker annotation missing")
	}
	if m.Kind != "morphTo" {
		t.Errorf("Kind = %q, want morphTo", m.Kind)
	}
	if len(m.AllowedTypes) != 2 || m.AllowedTypes[0] != "Post" || m.AllowedTypes[1] != "Video" {
		t.Errorf("AllowedTypes = %v, want [Post Video]", m.AllowedTypes)
	}
}

func TestMorphTo_DefaultIDType(t *testing.T) {
	b := MorphTo("commentable", Post.Type)
	m, _ := decodeMarker(b.Descriptor().Annotations)
	if m.IDType != "string" {
		t.Errorf("default IDType = %q, want string", m.IDType)
	}
}

func TestMorphTo_BuilderChaining(t *testing.T) {
	b := MorphTo("commentable", Post.Type).
		IDColumn("parent_id").
		TypeColumn("parent_type").
		IDType("int").
		Required()
	m, _ := decodeMarker(b.Descriptor().Annotations)
	if m.IDColumn != "parent_id" {
		t.Errorf("IDColumn = %q, want parent_id", m.IDColumn)
	}
	if m.TypeColumn != "parent_type" {
		t.Errorf("TypeColumn = %q, want parent_type", m.TypeColumn)
	}
	if m.IDType != "int" {
		t.Errorf("IDType = %q, want int", m.IDType)
	}
	if !m.Required {
		t.Error("Required not set")
	}
}

func TestMorphTo_DefaultColumnNamesFromRelation(t *testing.T) {
	b := MorphTo("commentable", Post.Type)
	m, _ := decodeMarker(b.Descriptor().Annotations)
	if got := m.idColumn(); got != "commentable_id" {
		t.Errorf("idColumn = %q, want commentable_id", got)
	}
	if got := m.typeColumn(); got != "commentable_type" {
		t.Errorf("typeColumn = %q, want commentable_type", got)
	}
}

func TestMorphTo_OverrideColumnNames(t *testing.T) {
	b := MorphTo("commentable", Post.Type).IDColumn("pid").TypeColumn("ptype")
	m, _ := decodeMarker(b.Descriptor().Annotations)
	if got := m.idColumn(); got != "pid" {
		t.Errorf("idColumn = %q, want pid", got)
	}
	if got := m.typeColumn(); got != "ptype" {
		t.Errorf("typeColumn = %q, want ptype", got)
	}
}

func TestMorphMany_PopulatesFields(t *testing.T) {
	b := MorphMany("comments", Post.Type, "commentable")
	m, _ := decodeMarker(b.Descriptor().Annotations)
	if m.Kind != "morphMany" {
		t.Errorf("Kind = %q, want morphMany", m.Kind)
	}
	if m.FieldName != "comments" || m.Target != "Post" || m.MorphName != "commentable" {
		t.Errorf("fields wrong: %+v", m)
	}
}

func TestMorphOne_PopulatesFields(t *testing.T) {
	b := MorphOne("featured_image", Image.Type, "imageable")
	m, _ := decodeMarker(b.Descriptor().Annotations)
	if m.Kind != "morphOne" {
		t.Errorf("Kind = %q, want morphOne", m.Kind)
	}
	if m.FieldName != "featured_image" || m.Target != "Image" || m.MorphName != "imageable" {
		t.Errorf("fields wrong: %+v", m)
	}
}

func TestMorphedByMany_WithThrough(t *testing.T) {
	b := MorphedByMany("posts", Post.Type).Through("taggables", fakeSchema.Type)
	m, _ := decodeMarker(b.Descriptor().Annotations)
	if m.Kind != "morphedByMany" {
		t.Errorf("Kind = %q, want morphedByMany", m.Kind)
	}
	if m.Target != "Post" {
		t.Errorf("Target = %q, want Post", m.Target)
	}
	if m.Through != "fakeSchema" {
		t.Errorf("Through = %q, want fakeSchema", m.Through)
	}
	if m.ThroughName != "taggables" {
		t.Errorf("ThroughName = %q, want taggables", m.ThroughName)
	}
	// MorphName defaults to singularised through name.
	if m.MorphName != "taggable" {
		t.Errorf("MorphName = %q, want taggable (auto-derived)", m.MorphName)
	}
}

func TestMorphedByMany_MorphNameOverride(t *testing.T) {
	b := MorphedByMany("posts", Post.Type).
		Through("taggables", fakeSchema.Type).
		MorphName("custom")
	m, _ := decodeMarker(b.Descriptor().Annotations)
	if m.MorphName != "custom" {
		t.Errorf("MorphName = %q, want custom", m.MorphName)
	}
}

func TestSingularise(t *testing.T) {
	cases := []struct{ in, want string }{
		{"taggables", "taggable"},
		{"posts", "post"},
		{"categories", "category"},
		{"x", "x"},
		{"", ""},
		{"already", "already"}, // doesn't end in 's' or 'ies' — passthrough
	}
	for _, c := range cases {
		if got := singularise(c.in); got != c.want {
			t.Errorf("singularise(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
