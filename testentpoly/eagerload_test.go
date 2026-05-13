// Scenario 4 from SCENARIOS.md: WithCommentable() eager-load runs in
// 1 + N(parent types) queries — not one per child row.
package testentpoly

import (
	"context"
	"testing"

	"github.com/khanakia/entx/testentpoly/ent"
	"github.com/khanakia/entx/testentpoly/ent/comment"
)

// TestEagerLoad_BatchedPerType — scenario 4.
func TestEagerLoad_BatchedPerType(t *testing.T) {
	ctx := context.Background()
	client, tr := openTracedClient(t)

	// Seed 3 posts, 2 videos, 1 image — and 10 comments distributed
	// across them. If eager-load were per-row, we'd see ~10 SELECTs
	// against the parent tables. The contract is one SELECT per parent
	// type that has at least one child.
	p1 := client.Post.Create().SetTitle("p1").SaveX(ctx)
	p2 := client.Post.Create().SetTitle("p2").SaveX(ctx)
	p3 := client.Post.Create().SetTitle("p3").SaveX(ctx)
	v1 := client.Video.Create().SetTitle("v1").SaveX(ctx)
	v2 := client.Video.Create().SetTitle("v2").SaveX(ctx)
	img := client.Image.Create().SetURL("u").SaveX(ctx)

	// 4 on posts, 3 on videos, 2 on image, 1 orphan.
	for _, parent := range []ent.CommentCommentableParent{p1, p1, p2, p3} {
		_ = client.Comment.Create().SetBody("on post").SetCommentable(parent).SaveX(ctx)
	}
	for _, parent := range []ent.CommentCommentableParent{v1, v2, v2} {
		_ = client.Comment.Create().SetBody("on video").SetCommentable(parent).SaveX(ctx)
	}
	for _, parent := range []ent.CommentCommentableParent{img, img} {
		_ = client.Comment.Create().SetBody("on image").SetCommentable(parent).SaveX(ctx)
	}
	// Orphan rows are no longer reachable through the typed builder after
	// Required() landed in phase 2.

	// Reset the tracer right before the operation under measurement.
	tr.Reset()

	r, err := client.Comment.Query().Order(ent.Asc(comment.FieldID)).WithCommentable().All(ctx)
	if err != nil {
		t.Fatalf("WithCommentable: %v", err)
	}
	if len(r.Comments) != 9 {
		t.Fatalf("Comments len = %d, want 9", len(r.Comments))
	}

	// Eager-load contract: one SELECT per parent type that has children.
	if got := tr.CountSelectsFrom("posts"); got != 1 {
		t.Errorf("SELECTs from posts = %d, want 1\nrecorded:\n%v", got, tr.Snapshot())
	}
	if got := tr.CountSelectsFrom("videos"); got != 1 {
		t.Errorf("SELECTs from videos = %d, want 1\nrecorded:\n%v", got, tr.Snapshot())
	}
	if got := tr.CountSelectsFrom("images"); got != 1 {
		t.Errorf("SELECTs from images = %d, want 1\nrecorded:\n%v", got, tr.Snapshot())
	}

	// Sanity: each non-orphan child resolved to the right concrete type.
	matched := 0
	for _, ch := range r.Comments {
		parent := r.Commentable[ch.ID]
		if parent == nil {
			continue
		}
		switch parent.(type) {
		case *ent.Post, *ent.Video, *ent.Image:
			matched++
		default:
			t.Errorf("unexpected resolved parent type %T", parent)
		}
	}
	if matched != 9 {
		t.Errorf("resolved-parent count = %d, want 9", matched)
	}
}
