package schema

import "testing"

// TestSchemasInstantiate is a smoke test that exercises every example
// schema's Mixin() / Fields() / Edges() once. The goal is not to verify
// any property — that work happens inside the entpoly package — but to
// guard against accidental breakage of the example code, which doubles
// as documentation.
func TestSchemasInstantiate(t *testing.T) {
	// Polymorphic children (own discriminator via Mixin + MorphTo edge).
	if got := (Comment{}.Mixin()); len(got) == 0 {
		t.Error("Comment.Mixin() returned empty")
	}
	if got := (Comment{}.Edges()); len(got) == 0 {
		t.Error("Comment.Edges() returned empty")
	}
	if got := (Image{}.Mixin()); len(got) == 0 {
		t.Error("Image.Mixin() returned empty")
	}
	if got := (Image{}.Edges()); len(got) == 0 {
		t.Error("Image.Edges() returned empty")
	}

	// Polymorphic pivot (also a child).
	if got := (Taggable{}.Mixin()); len(got) == 0 {
		t.Error("Taggable.Mixin() returned empty")
	}
	if got := (Taggable{}.Edges()); len(got) == 0 {
		t.Error("Taggable.Edges() returned empty")
	}

	// M2M holder (no discriminator on itself, only back-refs).
	if got := (Tag{}.Edges()); len(got) == 0 {
		t.Error("Tag.Edges() returned empty")
	}

	// Plain parents — Post/Video declare back-refs in Edges, no Mixin.
	if got := (Post{}.Fields()); len(got) == 0 {
		t.Error("Post.Fields() returned empty")
	}
	if got := (Post{}.Edges()); len(got) == 0 {
		t.Error("Post.Edges() returned empty")
	}
	if got := (Video{}.Fields()); len(got) == 0 {
		t.Error("Video.Fields() returned empty")
	}
	if got := (Video{}.Edges()); len(got) == 0 {
		t.Error("Video.Edges() returned empty")
	}
}
