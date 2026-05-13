// Scenario 7 from SCENARIOS.md: typed predicate constructors —
// CommentCommentableIs(parent) and CommentCommentableIsType(key) filter
// children correctly without leaking string literals into the call site.
package testentpoly

import (
	"context"
	"testing"

	"github.com/khanakia/entx/testentpoly/ent"
	"github.com/khanakia/entx/testentpoly/ent/comment"
	"github.com/khanakia/entx/testentpoly/ent/post"
)

// TestPredicates_TypedConstructors — scenario 7.
func TestPredicates_TypedConstructors(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post1 := client.Post.Create().SetTitle("P1").SaveX(ctx)
	post2 := client.Post.Create().SetTitle("P2").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SaveX(ctx)
	img := client.Image.Create().SetURL("u").SaveX(ctx)

	_ = client.Comment.Create().SetBody("a").SetCommentable(post1).SaveX(ctx)
	_ = client.Comment.Create().SetBody("b").SetCommentable(post1).SaveX(ctx)
	_ = client.Comment.Create().SetBody("c").SetCommentable(post2).SaveX(ctx)
	_ = client.Comment.Create().SetBody("d").SetCommentable(video).SaveX(ctx)
	_ = client.Comment.Create().SetBody("e").SetCommentable(img).SaveX(ctx)

	// Parent-bound predicate: id + type match in one call.
	if got := client.Comment.Query().Where(ent.CommentCommentableIs(post1)).CountX(ctx); got != 2 {
		t.Errorf("CommentCommentableIs(post1) = %d, want 2", got)
	}
	if got := client.Comment.Query().Where(ent.CommentCommentableIs(post2)).CountX(ctx); got != 1 {
		t.Errorf("CommentCommentableIs(post2) = %d, want 1", got)
	}
	if got := client.Comment.Query().Where(ent.CommentCommentableIs(video)).CountX(ctx); got != 1 {
		t.Errorf("CommentCommentableIs(video) = %d, want 1", got)
	}

	// Key-bound predicate: matches every child of the given parent type.
	if got := client.Comment.Query().Where(ent.CommentCommentableIsType(ent.PostMorphKey)).CountX(ctx); got != 3 {
		t.Errorf("IsType(PostMorphKey) = %d, want 3", got)
	}
	if got := client.Comment.Query().Where(ent.CommentCommentableIsType(ent.VideoMorphKey)).CountX(ctx); got != 1 {
		t.Errorf("IsType(VideoMorphKey) = %d, want 1", got)
	}
	if got := client.Comment.Query().Where(ent.CommentCommentableIsType(ent.ImageMorphKey)).CountX(ctx); got != 1 {
		t.Errorf("IsType(ImageMorphKey) = %d, want 1", got)
	}
}

// TestPredicates_OnParentSubquery — scenario 8. The whereMorphRelation
// helper composes sub-query predicates on a specific parent type and
// chains with comment.Or for multi-type matching.
func TestPredicates_OnParentSubquery(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	postPub := client.Post.Create().SetTitle("Published").SetPublished(true).SaveX(ctx)
	postDraft := client.Post.Create().SetTitle("Draft").SetPublished(false).SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SaveX(ctx)

	cPub := client.Comment.Create().SetBody("on pub").SetCommentable(postPub).SaveX(ctx)
	cDraft := client.Comment.Create().SetBody("on draft").SetCommentable(postDraft).SaveX(ctx)
	cVideo := client.Comment.Create().SetBody("on video").SetCommentable(video).SaveX(ctx)
	_ = cVideo

	// Zero-predicate form: every comment whose parent is a Post.
	if got := client.Comment.Query().Where(ent.CommentCommentableOnPost()).CountX(ctx); got != 2 {
		t.Errorf("CommentCommentableOnPost() = %d, want 2", got)
	}

	// Predicate-on-parent: only Comment whose parent Post is published.
	pub := client.Comment.Query().
		Where(ent.CommentCommentableOnPost(post.PublishedEQ(true))).
		AllX(ctx)
	if len(pub) != 1 || pub[0].ID != cPub.ID {
		t.Errorf("OnPost(Published) = %+v, want [cPub=%d]", pub, cPub.ID)
	}

	// Multi-type OR: draft posts OR any video.
	multi := client.Comment.Query().
		Where(comment.Or(
			ent.CommentCommentableOnPost(post.PublishedEQ(false)),
			ent.CommentCommentableOnVideo(),
		)).
		AllX(ctx)
	got := map[int]bool{}
	for _, c := range multi {
		got[c.ID] = true
	}
	if !got[cDraft.ID] || !got[cVideo.ID] || len(multi) != 2 {
		t.Errorf("multi-type Or = %v (len %d), want cDraft (%d) and cVideo (%d)", got, len(multi), cDraft.ID, cVideo.ID)
	}
}
