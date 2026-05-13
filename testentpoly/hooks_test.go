// Scenarios 9a–9d from SCENARIOS.md: the runtime hooks emitted by
// MorphTo's Required / Touch / Cascade / SoftDelete options. All four
// hooks are installed by ent.RegisterPolyHooks(client) wired in
// openTestClient.
package testentpoly

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/khanakia/entx/testentpoly/ent"
	"github.com/khanakia/entx/testentpoly/ent/comment"
)

// TestHook_Required — scenario 9a. Required() rejects Create that omits
// the discriminator and rejects ClearCommentable on Update.
func TestHook_Required(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	// Create without SetCommentable — rejected.
	if _, err := client.Comment.Create().SetBody("orphan").Save(ctx); err == nil {
		t.Fatal("expected error on Create without SetCommentable, got nil")
	} else if !strings.Contains(strings.ToLower(err.Error()), "required") {
		t.Errorf("error %q should mention 'required'", err.Error())
	}

	// Sanity: no orphan row was written.
	if n := client.Comment.Query().CountX(ctx); n != 0 {
		t.Errorf("comment count after rejected Create = %d, want 0", n)
	}

	// Create with SetCommentable — succeeds.
	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	c := client.Comment.Create().SetBody("ok").SetCommentable(post).SaveX(ctx)

	// ClearCommentable rejected on Update.
	if _, err := c.Update().ClearCommentable().Save(ctx); err == nil {
		t.Fatal("expected error on ClearCommentable, got nil")
	}
}

// TestHook_Touch — scenario 9b. Each Comment Save bumps the parent's
// updated_at.
func TestHook_Touch(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	original := post.UpdatedAt

	time.Sleep(10 * time.Millisecond)
	c := client.Comment.Create().SetBody("hi").SetCommentable(post).SaveX(ctx)

	afterCreate := client.Post.GetX(ctx, post.ID)
	if !afterCreate.UpdatedAt.After(original) {
		t.Errorf("post.UpdatedAt = %v, want after %v (touch on Create)", afterCreate.UpdatedAt, original)
	}

	time.Sleep(10 * time.Millisecond)
	_ = c.Update().SetBody("edited").SaveX(ctx)

	afterUpdate := client.Post.GetX(ctx, post.ID)
	if !afterUpdate.UpdatedAt.After(afterCreate.UpdatedAt) {
		t.Errorf("post.UpdatedAt = %v, want after %v (touch on Update)", afterUpdate.UpdatedAt, afterCreate.UpdatedAt)
	}
}

// TestHook_Cascade — scenario 9c. Deleting a parent removes the
// children pointing at it; siblings on a different parent untouched.
func TestHook_Cascade(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SaveX(ctx)

	c1 := client.Comment.Create().SetBody("a").SetCommentable(post).SaveX(ctx)
	c2 := client.Comment.Create().SetBody("b").SetCommentable(post).SaveX(ctx)
	c3 := client.Comment.Create().SetBody("c").SetCommentable(video).SaveX(ctx)

	if n := client.Comment.Query().CountX(ctx); n != 3 {
		t.Fatalf("precondition: count = %d, want 3", n)
	}

	client.Post.DeleteOneID(post.ID).ExecX(ctx)

	if e, _ := client.Comment.Query().Where(comment.IDEQ(c1.ID)).Exist(ctx); e {
		t.Error("c1 should have cascaded")
	}
	if e, _ := client.Comment.Query().Where(comment.IDEQ(c2.ID)).Exist(ctx); e {
		t.Error("c2 should have cascaded")
	}
	if e, _ := client.Comment.Query().Where(comment.IDEQ(c3.ID)).Exist(ctx); !e {
		t.Error("c3 (video sibling) should still exist")
	}

	client.Video.DeleteOneID(video.ID).ExecX(ctx)
	if n := client.Comment.Query().CountX(ctx); n != 0 {
		t.Errorf("after video delete: count = %d, want 0", n)
	}
}

// TestHook_SoftDelete — scenario 9d. Soft-deleted parents are filtered
// out of reverse-resolve / eager-load. Per-target auto-detection: Post
// has deleted_at and is filtered; Video does not declare deleted_at and
// is therefore not affected.
func TestHook_SoftDelete(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SaveX(ctx)

	cp := client.Comment.Create().SetBody("on post").SetCommentable(post).SaveX(ctx)
	cv := client.Comment.Create().SetBody("on video").SetCommentable(video).SaveX(ctx)

	// Pre-soft-delete: parent resolves.
	if p, _ := cp.QueryCommentable(ctx); p == nil {
		t.Fatal("pre soft-delete: post parent should resolve")
	}

	// Soft-delete the post.
	client.Post.UpdateOneID(post.ID).SetDeletedAt(time.Now()).SaveX(ctx)

	// Post-soft-delete: reverse resolve returns nil for the post-attached
	// child; the video-attached child is unaffected.
	if p, _ := cp.QueryCommentable(ctx); p != nil {
		t.Errorf("post-soft-delete: post parent should be filtered, got %+v", p)
	}
	if v, _ := cv.QueryCommentable(ctx); v == nil {
		t.Error("video parent should still resolve (Video has no deleted_at)")
	} else if _, ok := v.(*ent.Video); !ok {
		t.Errorf("video parent wrong type: %T", v)
	}

	// Eager-load: soft-deleted parent absent from map; video parent present.
	r, err := client.Comment.Query().WithCommentable().All(ctx)
	if err != nil {
		t.Fatalf("WithCommentable: %v", err)
	}
	if _, ok := r.Commentable[cp.ID]; ok {
		t.Errorf("eager-load map should not contain soft-deleted parent for cp=%d", cp.ID)
	}
	if _, ok := r.Commentable[cv.ID]; !ok {
		t.Errorf("eager-load map should contain video parent for cv=%d", cv.ID)
	}
}
