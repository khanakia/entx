// Scenarios 1–3 from SCENARIOS.md:
//
//   1. CRUD: SetCommentable / ClearCommentable / reassign across types.
//   2. Forward resolve via sealed-interface type switch.
//   3. Reverse: post.QueryComments() composes with predicates.
package testentpoly

import (
	"context"
	"testing"

	"github.com/khanakia/entx/testentpoly/ent"
	"github.com/khanakia/entx/testentpoly/ent/comment"
)

// TestCRUD_SetClearReassign — scenario 1.
func TestCRUD_SetClearReassign(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SaveX(ctx)

	// Initial Set writes both discriminator columns.
	c := client.Comment.Create().SetBody("hi").SetCommentable(post).SaveX(ctx)
	if c.CommentableID == nil || *c.CommentableID != post.MorphID() {
		t.Fatalf("initial Set: id = %v, want %q", c.CommentableID, post.MorphID())
	}
	if c.CommentableType == nil || *c.CommentableType != comment.CommentableType(string(ent.PostMorphKey)) {
		t.Fatalf("initial Set: type = %v, want %q", c.CommentableType, ent.PostMorphKey)
	}

	// Reassign across types — both columns flip together.
	c = c.Update().SetCommentable(video).SaveX(ctx)
	if *c.CommentableType != comment.CommentableType(string(ent.VideoMorphKey)) {
		t.Errorf("after reassign: type = %q, want %q", *c.CommentableType, ent.VideoMorphKey)
	}
	if *c.CommentableID != video.MorphID() {
		t.Errorf("after reassign: id = %q, want %q", *c.CommentableID, video.MorphID())
	}

	// Clear path is rejected for Required() relations — covered in
	// TestHook_Required. After phase 2 the Comment.commentable relation
	// is Required(), so ClearCommentable() is no longer a valid path for
	// this scenario.
}

// TestForwardResolve_SealedSwitch — scenario 2.
func TestForwardResolve_SealedSwitch(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SaveX(ctx)
	img := client.Image.Create().SetURL("u").SaveX(ctx)

	cp := client.Comment.Create().SetBody("on post").SetCommentable(post).SaveX(ctx)
	cv := client.Comment.Create().SetBody("on video").SetCommentable(video).SaveX(ctx)
	ci := client.Comment.Create().SetBody("on image").SetCommentable(img).SaveX(ctx)
	// Orphan path is no longer reachable through the typed builder after
	// Required() landed in phase 2; the nil-resolve branch is covered by
	// the soft-delete hook tests where the parent disappears post-write.

	// Forward resolve returns the sealed interface; type-switch must
	// cover only the AllowedTypes (Post / Video / Image).
	cases := []struct {
		c    *ent.Comment
		want string
	}{
		{cp, "post"},
		{cv, "video"},
		{ci, "image"},
	}
	for _, tc := range cases {
		parent, err := tc.c.QueryCommentable(ctx)
		if err != nil {
			t.Fatalf("QueryCommentable(%q): %v", tc.want, err)
		}
		var got string
		switch p := parent.(type) {
		case *ent.Post:
			got = "post"
			if p.ID != post.ID {
				t.Errorf("%q: post id = %d, want %d", tc.want, p.ID, post.ID)
			}
		case *ent.Video:
			got = "video"
			if p.ID != video.ID {
				t.Errorf("%q: video id = %d, want %d", tc.want, p.ID, video.ID)
			}
		case *ent.Image:
			got = "image"
			if p.ID != img.ID {
				t.Errorf("%q: image id = %d, want %d", tc.want, p.ID, img.ID)
			}
		case nil:
			got = "nil"
		}
		if got != tc.want {
			t.Errorf("resolved parent for %q = %q, want %q", tc.want, got, tc.want)
		}
	}
}

// TestReverse_QueryWithPredicates — scenario 3. post.QueryComments()
// returns *CommentQuery so it composes with the full ent builder API.
func TestReverse_QueryWithPredicates(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SaveX(ctx)

	_ = client.Comment.Create().SetBody("aaa").SetCommentable(post).SaveX(ctx)
	_ = client.Comment.Create().SetBody("bbb").SetCommentable(post).SaveX(ctx)
	_ = client.Comment.Create().SetBody("ccc").SetCommentable(post).SaveX(ctx)
	_ = client.Comment.Create().SetBody("zzz").SetCommentable(video).SaveX(ctx)

	if n := post.QueryComments().CountX(ctx); n != 3 {
		t.Errorf("post.QueryComments count = %d, want 3", n)
	}

	// Compose with .Where + .Limit. Body predicates from the generated
	// comment package should chain transparently.
	got := post.QueryComments().
		Where(comment.BodyEQ("bbb")).
		Limit(1).
		AllX(ctx)
	if len(got) != 1 || got[0].Body != "bbb" {
		t.Errorf("filtered query = %+v, want exactly bbb", got)
	}

	// Cross-parent isolation: video sees its own.
	if n := video.QueryComments().CountX(ctx); n != 1 {
		t.Errorf("video.QueryComments count = %d, want 1", n)
	}
}
