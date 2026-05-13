// Scenarios 5–6 from SCENARIOS.md: polymorphic M2M via Tag + Taggable
// pivot, and the auto-emitted inverse on Post/Video.
package testentpoly

import (
	"context"
	"sort"
	"testing"

	"github.com/khanakia/entx/entpoly/helper"
	"github.com/khanakia/entx/testentpoly/ent"
)

// attachedTagIDs returns the tag_ids currently pivoted to the given Post.
func attachedTagIDs(t *testing.T, ctx context.Context, client *ent.Client, post *ent.Post) []int {
	t.Helper()
	pivots := client.Taggable.Query().Where(ent.TaggableTaggableIs(post)).AllX(ctx)
	ids := make([]int, 0, len(pivots))
	for _, p := range pivots {
		ids = append(ids, p.TagID)
	}
	sort.Ints(ids)
	return ids
}

// attach connects a Tag to a Post via a fresh Taggable pivot row.
func attach(ctx context.Context, client *ent.Client, post *ent.Post, tagID int) {
	client.Taggable.Create().
		SetTagID(tagID).
		SetTaggable(post).
		SaveX(ctx)
}

// detach removes every pivot row for (post, tagID).
func detach(ctx context.Context, client *ent.Client, post *ent.Post, tagID int) {
	pivots := client.Taggable.Query().
		Where(ent.TaggableTaggableIs(post)).
		AllX(ctx)
	for _, p := range pivots {
		if p.TagID == tagID {
			client.Taggable.DeleteOne(p).ExecX(ctx)
		}
	}
}

// TestM2M_HelperRoundTrips — scenario 5. Exercises the helper package
// (Toggle / Sync / SyncWithoutDetach) by driving attach/detach against
// the typed pivot client.
func TestM2M_HelperRoundTrips(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	a := client.Tag.Create().SetName("a").SaveX(ctx)
	b := client.Tag.Create().SetName("b").SaveX(ctx)
	c := client.Tag.Create().SetName("c").SaveX(ctx)

	// Initial attach: a, b.
	attach(ctx, client, post, a.ID)
	attach(ctx, client, post, b.ID)

	// Sync to {b, c} → attach c, detach a.
	cur := attachedTagIDs(t, ctx, client, post)
	toAdd, toDel := helper.Sync(cur, []int{b.ID, c.ID})
	for _, id := range toAdd {
		attach(ctx, client, post, id)
	}
	for _, id := range toDel {
		detach(ctx, client, post, id)
	}
	if got := attachedTagIDs(t, ctx, client, post); !equalInt(got, []int{b.ID, c.ID}) {
		t.Errorf("after Sync: %v, want [%d %d]", got, b.ID, c.ID)
	}

	// SyncWithoutDetach: target {a, c} should only add a (c already there).
	cur = attachedTagIDs(t, ctx, client, post)
	toAdd = helper.SyncWithoutDetach(cur, []int{a.ID, c.ID})
	if len(toAdd) != 1 || toAdd[0] != a.ID {
		t.Errorf("SyncWithoutDetach toAdd = %v, want [%d]", toAdd, a.ID)
	}
	for _, id := range toAdd {
		attach(ctx, client, post, id)
	}
	if got := attachedTagIDs(t, ctx, client, post); !equalInt(got, []int{a.ID, b.ID, c.ID}) {
		t.Errorf("after SyncWithoutDetach: %v, want [%d %d %d]", got, a.ID, b.ID, c.ID)
	}

	// Toggle: target {b, c} → both currently attached, so both detach.
	cur = attachedTagIDs(t, ctx, client, post)
	toAdd, toDel = helper.Toggle(cur, []int{b.ID, c.ID})
	if len(toAdd) != 0 {
		t.Errorf("Toggle toAdd = %v, want []", toAdd)
	}
	if len(toDel) != 2 {
		t.Errorf("Toggle toDel = %v, want 2", toDel)
	}
	for _, id := range toDel {
		detach(ctx, client, post, id)
	}
	if got := attachedTagIDs(t, ctx, client, post); !equalInt(got, []int{a.ID}) {
		t.Errorf("after Toggle: %v, want [%d]", got, a.ID)
	}

	// Plain detach: detach a → empty.
	detach(ctx, client, post, a.ID)
	if got := attachedTagIDs(t, ctx, client, post); len(got) != 0 {
		t.Errorf("after detach: %v, want empty", got)
	}
}

func equalInt(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestM2M_AutoInverseFromHolder — scenario 6. Tag declares
// MorphedByMany("posts", Post.Type) and entpoly emits both directions
// — tag.QueryPosts AND the auto-inverse post.QueryTags.
func TestM2M_AutoInverseFromHolder(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post1 := client.Post.Create().SetTitle("P1").SaveX(ctx)
	post2 := client.Post.Create().SetTitle("P2").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SaveX(ctx)
	a := client.Tag.Create().SetName("a").SaveX(ctx)
	b := client.Tag.Create().SetName("b").SaveX(ctx)

	// a → post1+post2+video; b → post1
	for _, parent := range []ent.TaggableTaggableParent{post1, post2, video} {
		client.Taggable.Create().SetTagID(a.ID).SetTaggable(parent).SaveX(ctx)
	}
	client.Taggable.Create().SetTagID(b.ID).SetTaggable(post1).SaveX(ctx)

	// Auto-inverse from holder → tag.QueryPosts(ctx).
	aPosts, err := a.QueryPosts(ctx)
	if err != nil {
		t.Fatalf("a.QueryPosts: %v", err)
	}
	if len(aPosts) != 2 {
		t.Errorf("a.QueryPosts = %d, want 2", len(aPosts))
	}

	// Auto-inverse from target → post.QueryTags(ctx). The Post schema
	// does NOT declare any MorphedByMany — the back-ref is emitted by
	// scanning Tag's declaration.
	p1Tags, err := post1.QueryTags(ctx)
	if err != nil {
		t.Fatalf("post1.QueryTags: %v", err)
	}
	if len(p1Tags) != 2 {
		t.Errorf("post1.QueryTags = %d, want 2", len(p1Tags))
	}

	// Video inherits the same auto-inverse.
	vTags, err := video.QueryTags(ctx)
	if err != nil {
		t.Fatalf("video.QueryTags: %v", err)
	}
	if len(vTags) != 1 {
		t.Errorf("video.QueryTags = %d, want 1", len(vTags))
	}
}
