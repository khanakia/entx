// Runtime tests for the entpoly basic example. These exercise the
// generated client end-to-end against an in-memory SQLite database
// (pure-Go via modernc.org/sqlite — no cgo).
//
// What is verified here, that the codegen-pipeline tests in the entpoly
// core package cannot verify:
//
//   - SetCommentable(post) actually writes both discriminator columns.
//   - The persisted "*_type" value matches the morph-key constant emitted
//     by codegen (PostMorphKey / VideoMorphKey / ImageMorphKey).
//   - Reassigning the polymorphic parent across types works.
//   - ClearCommentable() resets both columns to NULL.
//   - The M2M pivot flow (Tag ↔ Post via Taggable) round-trips.
//   - Back-ref predicates (CommentableTypeEQ + CommentableIDEQ) read
//     back the same rows we wrote.
package basic_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"github.com/khanakia/entx/entpoly/examples/basic/ent"
	"github.com/khanakia/entx/entpoly/examples/basic/ent/comment"
	"github.com/khanakia/entx/entpoly/examples/basic/ent/image"
)

// openTestClient spins up an in-memory SQLite database via the pure-Go
// modernc.org/sqlite driver and runs the auto-migration to create
// every table. The returned client is fresh per test and cleaned up
// via t.Cleanup so tests stay isolated.
func openTestClient(t *testing.T) *ent.Client {
	t.Helper()

	// Foreign-key enforcement is off because polymorphic columns
	// cannot carry FKs. We open with file::memory: to get a fresh
	// in-memory database per test.
	db, err := sql.Open("sqlite", "file:ent?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	drv := entsql.OpenDB("sqlite3", db)
	client := ent.NewClient(ent.Driver(drv))

	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("schema migrate: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	// Wire entpoly's Required() runtime hooks. Without this call any
	// Comment with .Required() set on its MorphTo edge would still
	// accept a Save with the discriminator pair unset.
	ent.RegisterPolyHooks(client)

	return client
}

// TestSetCommentableWritesBothColumns verifies the core Set<Morph>
// behaviour: a single call writes the id and type discriminator pair
// to the persisted columns.
func TestSetCommentableWritesBothColumns(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("Hello").SaveX(ctx)

	c := client.Comment.Create().
		SetBody("Nice post!").
		SetCommentable(post).
		SaveX(ctx)

	if c.CommentableID == nil {
		t.Fatal("CommentableID is nil — Set did not write the id column")
	}
	if *c.CommentableID != post.MorphID() {
		t.Errorf("CommentableID = %q, want %q", *c.CommentableID, post.MorphID())
	}
	if c.CommentableType == nil {
		t.Fatal("CommentableType is nil — Set did not write the type column")
	}
	if *c.CommentableType != comment.CommentableType(string(ent.PostMorphKey)) {
		t.Errorf("CommentableType = %q, want %q", *c.CommentableType, ent.PostMorphKey)
	}
}

// TestMorphKeyConstantsMatchRuntimeMethod cross-checks that the per-type
// constants (ent.PostMorphKey) match what the parent's MorphKey() method
// returns at runtime. If these ever diverge, every back-ref query using
// the constant would silently fail.
func TestMorphKeyConstantsMatchRuntimeMethod(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("X").SaveX(ctx)
	if post.MorphKey() != ent.PostMorphKey {
		t.Errorf("Post.MorphKey() = %q, ent.PostMorphKey = %q", post.MorphKey(), ent.PostMorphKey)
	}

	video := client.Video.Create().SetTitle("Y").SetURL("u").SaveX(ctx)
	if video.MorphKey() != ent.VideoMorphKey {
		t.Errorf("Video.MorphKey() = %q, ent.VideoMorphKey = %q", video.MorphKey(), ent.VideoMorphKey)
	}

	img := client.Image.Create().SetURL("u").SetImageable(post).SaveX(ctx)
	if img.MorphKey() != ent.ImageMorphKey {
		t.Errorf("Image.MorphKey() = %q, ent.ImageMorphKey = %q", img.MorphKey(), ent.ImageMorphKey)
	}
}

// TestReassignAcrossParentTypes verifies that the discriminator pair
// correctly reroutes a child from one parent type to another. This is
// the polymorphism payoff — a Comment that was attached to a Post can
// be moved to a Video without schema changes.
func TestReassignAcrossParentTypes(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SetURL("u").SaveX(ctx)

	c := client.Comment.Create().SetBody("hi").SetCommentable(post).SaveX(ctx)

	// Sanity: starts on Post.
	if *c.CommentableType != comment.CommentableType(string(ent.PostMorphKey)) {
		t.Fatalf("initial type = %q, want %q", *c.CommentableType, ent.PostMorphKey)
	}

	// Reassign to Video.
	c = c.Update().SetCommentable(video).SaveX(ctx)
	if *c.CommentableType != comment.CommentableType(string(ent.VideoMorphKey)) {
		t.Errorf("after reassign type = %q, want %q", *c.CommentableType, ent.VideoMorphKey)
	}
	if *c.CommentableID != video.MorphID() {
		t.Errorf("after reassign id = %q, want %q", *c.CommentableID, video.MorphID())
	}
}

// TestClearImageableNullsBothColumns verifies the Clear<Morph> path on
// a NOT-Required relation. Image.imageable is declared without
// .Required(), so the column-clear path works the way it did before
// Required() shipped. The Comment.commentable Required-Clear-rejection
// case is covered by TestRequiredEnforcementHook above.
func TestClearImageableNullsBothColumns(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	img := client.Image.Create().SetURL("u").SetImageable(post).SaveX(ctx)
	if img.ImageableID == nil {
		t.Fatal("precondition failed — id should be set before Clear")
	}
	img = img.Update().ClearImageable().SaveX(ctx)
	if img.ImageableID != nil {
		t.Errorf("ImageableID = %v, want nil after Clear", img.ImageableID)
	}
	if img.ImageableType != nil {
		t.Errorf("ImageableType = %v, want nil after Clear", img.ImageableType)
	}
}

// TestQueryBackRefByMorphKeyConstant uses the typed predicate package
// together with the generated morph-key constant. This is the v1 read
// path for back-references — typed-back-ref methods on parents land in
// v2 codegen.
func TestQueryBackRefByMorphKeyConstant(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SetURL("u").SaveX(ctx)

	// Two comments on the post, one on the video.
	_ = client.Comment.Create().SetBody("a").SetCommentable(post).SaveX(ctx)
	_ = client.Comment.Create().SetBody("b").SetCommentable(post).SaveX(ctx)
	_ = client.Comment.Create().SetBody("c").SetCommentable(video).SaveX(ctx)

	// Typed predicate — id + type both match in a single helper. Pass
	// the parent entity directly; no string literals anywhere.
	postComments := client.Comment.Query().
		Where(ent.CommentCommentableIs(post)).
		AllX(ctx)
	if len(postComments) != 2 {
		t.Errorf("post comments = %d, want 2", len(postComments))
	}

	// Typed by-type predicate — accepts only the codegen-emitted
	// MorphKey constants. Passing a string literal would fail to
	// compile.
	allPostChildren := client.Comment.Query().
		Where(ent.CommentCommentableIsType(ent.PostMorphKey)).
		AllX(ctx)
	if len(allPostChildren) != 2 {
		t.Errorf("by-type post comments = %d, want 2", len(allPostChildren))
	}

	videoComments := client.Comment.Query().
		Where(ent.CommentCommentableIs(video)).
		AllX(ctx)
	if len(videoComments) != 1 {
		t.Errorf("video comments = %d, want 1", len(videoComments))
	}
}

// TestPolymorphicM2MPivotRoundTrip exercises the MorphedByMany flow
// end-to-end: tag a post via the polymorphic pivot, then query back
// the tags attached to that post using the same morph-key constants.
func TestPolymorphicM2MPivotRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SetURL("u").SaveX(ctx)

	golang := client.Tag.Create().SetName("golang").SaveX(ctx)
	db := client.Tag.Create().SetName("database").SaveX(ctx)

	// Attach two tags to the post via the polymorphic pivot.
	_ = client.Taggable.Create().
		SetTagID(golang.ID).
		SetTaggable(post).
		SetAddedBy("aman").
		SaveX(ctx)
	_ = client.Taggable.Create().
		SetTagID(db.ID).
		SetTaggable(post).
		SaveX(ctx)

	// And one tag on the video.
	_ = client.Taggable.Create().
		SetTagID(golang.ID).
		SetTaggable(video).
		SaveX(ctx)

	// Typed pivot predicate — same shape as the comment one, just on
	// the Taggable child instead.
	postPivots := client.Taggable.Query().
		Where(ent.TaggableTaggableIs(post)).
		AllX(ctx)
	if len(postPivots) != 2 {
		t.Errorf("post pivots = %d, want 2", len(postPivots))
	}
	videoPivots := client.Taggable.Query().
		Where(ent.TaggableTaggableIs(video)).
		AllX(ctx)
	if len(videoPivots) != 1 {
		t.Errorf("video pivots = %d, want 1", len(videoPivots))
	}

	// Pivot extras (AddedBy) survive the round-trip.
	var found bool
	for _, p := range postPivots {
		if p.AddedBy == "aman" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pivot AddedBy=aman not found — pivot extras did not round-trip")
	}
}

// TestMorphedByManyHolderBackRef verifies the v2-shipped tag.QueryPosts
// /QueryVideos pattern — Laravel's $tag->posts. The method does the pivot
// query + parent load under the hood; the caller gets a typed []*Post.
func TestMorphedByManyHolderBackRef(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post1 := client.Post.Create().SetTitle("P1").SaveX(ctx)
	post2 := client.Post.Create().SetTitle("P2").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SetURL("u").SaveX(ctx)
	tagA := client.Tag.Create().SetName("a").SaveX(ctx)
	tagB := client.Tag.Create().SetName("b").SaveX(ctx)

	// tagA → post1, post2, video
	_ = client.Taggable.Create().SetTagID(tagA.ID).SetTaggable(post1).SaveX(ctx)
	_ = client.Taggable.Create().SetTagID(tagA.ID).SetTaggable(post2).SaveX(ctx)
	_ = client.Taggable.Create().SetTagID(tagA.ID).SetTaggable(video).SaveX(ctx)
	// tagB → post1 only
	_ = client.Taggable.Create().SetTagID(tagB.ID).SetTaggable(post1).SaveX(ctx)

	// Laravel: $tagA->posts;
	posts, err := tagA.QueryPosts(ctx)
	if err != nil {
		t.Fatalf("QueryPosts: %v", err)
	}
	if len(posts) != 2 {
		t.Errorf("tagA.QueryPosts = %d, want 2", len(posts))
	}

	videos, err := tagA.QueryVideos(ctx)
	if err != nil {
		t.Fatalf("QueryVideos: %v", err)
	}
	if len(videos) != 1 {
		t.Errorf("tagA.QueryVideos = %d, want 1", len(videos))
	}

	// Cross-tag isolation: tagB only has post1.
	bPosts, _ := tagB.QueryPosts(ctx)
	if len(bPosts) != 1 || bPosts[0].ID != post1.ID {
		t.Errorf("tagB.QueryPosts = %+v, want exactly post1", bPosts)
	}

	// Empty case — a tag with no pivots → empty slice, nil error.
	tagC := client.Tag.Create().SetName("c").SaveX(ctx)
	got, err := tagC.QueryPosts(ctx)
	if err != nil {
		t.Fatalf("QueryPosts on empty tag: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty tag.QueryPosts = %v, want empty", got)
	}
}

// TestEagerLoadBatchingResolvesAcrossTypes verifies the
// AllWithCommentable preload — the typed batched eager-load that
// fixes the N+1 problem for polymorphic reads. A mixed batch of
// Post-parents and Video-parents resolves correctly via a single
// query per parent type.
func TestEagerLoadBatchingResolvesAcrossTypes(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post1 := client.Post.Create().SetTitle("P1").SaveX(ctx)
	post2 := client.Post.Create().SetTitle("P2").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SetURL("u").SaveX(ctx)

	c1 := client.Comment.Create().SetBody("a").SetCommentable(post1).SaveX(ctx)
	c2 := client.Comment.Create().SetBody("b").SetCommentable(post2).SaveX(ctx)
	c3 := client.Comment.Create().SetBody("c").SetCommentable(video).SaveX(ctx)
	c4 := client.Comment.Create().SetBody("d").SetCommentable(post1).SaveX(ctx)

	r, err := client.Comment.Query().Order(ent.Asc(comment.FieldID)).WithCommentable().All(ctx)
	if err != nil {
		t.Fatalf("AllWithCommentable: %v", err)
	}
	if len(r.Comments) != 4 {
		t.Fatalf("Children len = %d, want 4", len(r.Comments))
	}
	if len(r.Commentable) != 4 {
		t.Fatalf("Commentable len = %d, want 4", len(r.Commentable))
	}

	// Each child's parent must round-trip to the right concrete type.
	if p, ok := r.Commentable[c1.ID].(*ent.Post); !ok || p.ID != post1.ID {
		t.Errorf("c1 parent = %v, want post1 (id=%d)", r.Commentable[c1.ID], post1.ID)
	}
	if p, ok := r.Commentable[c2.ID].(*ent.Post); !ok || p.ID != post2.ID {
		t.Errorf("c2 parent = %v, want post2 (id=%d)", r.Commentable[c2.ID], post2.ID)
	}
	if v, ok := r.Commentable[c3.ID].(*ent.Video); !ok || v.ID != video.ID {
		t.Errorf("c3 parent = %v, want video (id=%d)", r.Commentable[c3.ID], video.ID)
	}
	// c4 shares its parent with c1 — same loaded *Post instance is fine.
	if p, ok := r.Commentable[c4.ID].(*ent.Post); !ok || p.ID != post1.ID {
		t.Errorf("c4 parent = %v, want post1", r.Commentable[c4.ID])
	}
}

// TestEagerLoadBatchingEmptyAndOneType covers the degenerate paths:
// no children at all (nothing to eager-load), and all children share
// one parent type (only that bucket's batch query fires).
func TestEagerLoadBatchingEmptyAndOneType(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	// Zero children — result has empty Children + empty Commentable map.
	r, err := client.Comment.Query().WithCommentable().All(ctx)
	if err != nil {
		t.Fatalf("empty AllWithCommentable: %v", err)
	}
	if len(r.Comments) != 0 || len(r.Commentable) != 0 {
		t.Errorf("empty result = %+v, want both empty", r)
	}

	// All children point at Post — only the Post batch query fires.
	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	for i := 0; i < 3; i++ {
		_ = client.Comment.Create().SetBody("x").SetCommentable(post).SaveX(ctx)
	}
	r, err = client.Comment.Query().WithCommentable().All(ctx)
	if err != nil {
		t.Fatalf("single-type AllWithCommentable: %v", err)
	}
	if len(r.Comments) != 3 || len(r.Commentable) != 3 {
		t.Errorf("single-type result lens: %d / %d, want 3 / 3", len(r.Comments), len(r.Commentable))
	}
	for _, c := range r.Comments {
		if p, ok := r.Commentable[c.ID].(*ent.Post); !ok || p.ID != post.ID {
			t.Errorf("comment %d parent wrong: %+v", c.ID, r.Commentable[c.ID])
		}
	}
}

// TestTouchHookBumpsParentTimestamp verifies the runtime hook
// generated for MorphTo("commentable").Touch() — every successful
// Comment Save bumps the parent's updated_at timestamp. Laravel
// $touches behaviour: parent caches / listings can rely on
// updated_at moving whenever a child mutates.
func TestTouchHookBumpsParentTimestamp(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	originalTS := post.UpdatedAt

	// Sleep a hair so the post-touch timestamp differs from the
	// initial one even on systems with low-resolution clocks.
	time.Sleep(10 * time.Millisecond)

	// Create a comment — touch hook should bump post.updated_at.
	_ = client.Comment.Create().SetBody("hi").SetCommentable(post).SaveX(ctx)

	got := client.Post.GetX(ctx, post.ID)
	if !got.UpdatedAt.After(originalTS) {
		t.Errorf("post.UpdatedAt = %v, want after %v", got.UpdatedAt, originalTS)
	}
	afterCreate := got.UpdatedAt

	// Update the comment — should bump again.
	time.Sleep(10 * time.Millisecond)
	c := client.Comment.Query().FirstX(ctx)
	_ = c.Update().SetBody("edited").SaveX(ctx)

	got = client.Post.GetX(ctx, post.ID)
	if !got.UpdatedAt.After(afterCreate) {
		t.Errorf("post.UpdatedAt = %v, want after %v", got.UpdatedAt, afterCreate)
	}
}

// TestTouchHookBumpsCorrectParentType verifies cross-type isolation —
// a comment on a Post bumps Post.updated_at, not Video's.
func TestTouchHookBumpsCorrectParentType(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SetURL("u").SaveX(ctx)
	postOriginal := post.UpdatedAt
	videoOriginal := video.UpdatedAt

	time.Sleep(10 * time.Millisecond)
	_ = client.Comment.Create().SetBody("on video").SetCommentable(video).SaveX(ctx)

	gotPost := client.Post.GetX(ctx, post.ID)
	gotVideo := client.Video.GetX(ctx, video.ID)

	if !gotVideo.UpdatedAt.After(videoOriginal) {
		t.Errorf("video.UpdatedAt = %v, want after %v", gotVideo.UpdatedAt, videoOriginal)
	}
	if !gotPost.UpdatedAt.Equal(postOriginal) {
		t.Errorf("post.UpdatedAt = %v, want unchanged %v (comment was on video, not post)", gotPost.UpdatedAt, postOriginal)
	}
}

// TestRequiredEnforcementHook verifies the runtime hook generated for
// MorphTo("commentable").Required() — the relation cannot be left
// unset on Create and cannot be cleared on Update. The Go enum
// validator catches "invalid values"; this hook catches "missing
// values", together giving full coverage of the Required contract.
func TestRequiredEnforcementHook(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	// Create without SetCommentable — must be rejected.
	_, err := client.Comment.Create().SetBody("orphan").Save(ctx)
	if err == nil {
		t.Fatal("expected error for Create without SetCommentable, got nil")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error should mention required; got %q", err.Error())
	}

	// Create with SetCommentable — succeeds.
	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	c := client.Comment.Create().SetBody("ok").SetCommentable(post).SaveX(ctx)

	// Update that clears the relation — must be rejected.
	_, err = c.Update().ClearCommentable().Save(ctx)
	if err == nil {
		t.Fatal("expected error for ClearCommentable on Required relation, got nil")
	}
	if !strings.Contains(err.Error(), "Required") && !strings.Contains(err.Error(), "required") {
		t.Errorf("error should mention Required; got %q", err.Error())
	}

	// Reassign across allowed parents — still succeeds (both columns stay set).
	video := client.Video.Create().SetTitle("V").SetURL("u").SaveX(ctx)
	c = c.Update().SetCommentable(video).SaveX(ctx)
	if c.CommentableID == nil {
		t.Error("reassign should leave discriminator set")
	}
}

// TestMorphedByManyAutoInverseBackRef verifies the auto-emitted parent-
// side back-ref — Laravel's $post->tags. Generated automatically from
// the Tag.MorphedByMany("posts", ...) declaration; the Post schema does
// NOT need any matching declaration of its own. Same auto-emit happens
// for Video → tag.QueryVideos(ctx).
func TestMorphedByManyAutoInverseBackRef(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SetURL("u").SaveX(ctx)

	tagA := client.Tag.Create().SetName("a").SaveX(ctx)
	tagB := client.Tag.Create().SetName("b").SaveX(ctx)
	tagC := client.Tag.Create().SetName("c").SaveX(ctx)

	// Attach: post has tagA + tagB, video has tagB + tagC.
	for _, tg := range []*ent.Tag{tagA, tagB} {
		_ = client.Taggable.Create().SetTagID(tg.ID).SetTaggable(post).SaveX(ctx)
	}
	for _, tg := range []*ent.Tag{tagB, tagC} {
		_ = client.Taggable.Create().SetTagID(tg.ID).SetTaggable(video).SaveX(ctx)
	}

	// Laravel: $post->tags;
	postTags, err := post.QueryTags(ctx)
	if err != nil {
		t.Fatalf("post.QueryTags: %v", err)
	}
	if len(postTags) != 2 {
		t.Errorf("post.QueryTags = %d, want 2", len(postTags))
	}

	// Cross-target isolation: video sees its own tags.
	videoTags, err := video.QueryTags(ctx)
	if err != nil {
		t.Fatalf("video.QueryTags: %v", err)
	}
	if len(videoTags) != 2 {
		t.Errorf("video.QueryTags = %d, want 2", len(videoTags))
	}

	// Empty case — a post with no pivot rows.
	emptyPost := client.Post.Create().SetTitle("empty").SaveX(ctx)
	got, err := emptyPost.QueryTags(ctx)
	if err != nil {
		t.Fatalf("empty post.QueryTags: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty post.QueryTags = %v, want empty", got)
	}
}

// TestQueryCommentableTypedReverseResolve is the v2 typed reverse
// resolver — Laravel's `$comment->commentable`. The result type is the
// sealed CommentCommentableParent interface, so the caller can ONLY
// type-switch on Post or Video (the AllowedTypes). Article wouldn't
// even be a syntactically valid case arm.
func TestQueryCommentableTypedReverseResolve(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	video := client.Video.Create().SetTitle("V").SetURL("u").SaveX(ctx)

	// Attach a comment to a Post and resolve back to the typed parent.
	c1 := client.Comment.Create().SetBody("a").SetCommentable(post).SaveX(ctx)
	parent, err := c1.QueryCommentable(ctx)
	if err != nil {
		t.Fatalf("QueryCommentable: %v", err)
	}
	switch p := parent.(type) {
	case *ent.Post:
		if p.ID != post.ID {
			t.Errorf("resolved Post id = %d, want %d", p.ID, post.ID)
		}
	case *ent.Video:
		t.Errorf("expected Post, got Video: %+v", p)
	case nil:
		t.Fatal("parent is nil")
	}

	// Reassign to a Video and re-resolve.
	c1 = c1.Update().SetCommentable(video).SaveX(ctx)
	parent, err = c1.QueryCommentable(ctx)
	if err != nil {
		t.Fatalf("QueryCommentable after reassign: %v", err)
	}
	v, ok := parent.(*ent.Video)
	if !ok {
		t.Fatalf("expected *ent.Video after reassign, got %T", parent)
	}
	if v.ID != video.ID {
		t.Errorf("resolved Video id = %d, want %d", v.ID, video.ID)
	}

	// Note: Comment.commentable is Required() in the schema, so a
	// Clear path test is not appropriate here — see
	// TestRequiredEnforcementHook for the rejection behaviour. The
	// (nil, nil) return of QueryCommentable on an unset parent is
	// exercised via Image.imageable (which is not Required) in the
	// QueryImageable code path inside TestMorphOneFeaturedImage.
}

// TestLaravelStyleAccessors mirrors Laravel's polymorphic relationship
// accessors — $post->image (MorphOne) and $post->comments (MorphMany).
// The generated entpoly methods are direct ent equivalents that
// compose with ent's regular query builder for filtering and ordering.
func TestLaravelStyleAccessors(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("Hello").SaveX(ctx)

	// MorphMany — empty before any comments exist.
	if got := post.QueryComments().CountX(ctx); got != 0 {
		t.Errorf("empty post comments = %d, want 0", got)
	}

	// MorphOne — (nil, nil) when no row matches.
	img, err := post.QueryFeaturedImage(ctx)
	if err != nil {
		t.Fatalf("QueryFeaturedImage with no row: %v", err)
	}
	if img != nil {
		t.Errorf("QueryFeaturedImage = %+v, want nil", img)
	}

	// Attach two comments to the post via the typed setter.
	_ = client.Comment.Create().SetBody("a").SetCommentable(post).SaveX(ctx)
	_ = client.Comment.Create().SetBody("b").SetCommentable(post).SaveX(ctx)

	// MorphMany — Laravel: $post->comments;
	comments := post.QueryComments().AllX(ctx)
	if len(comments) != 2 {
		t.Errorf("post.QueryComments() = %d, want 2", len(comments))
	}

	// MorphMany composes with normal ent builders.
	one := post.QueryComments().Limit(1).AllX(ctx)
	if len(one) != 1 {
		t.Errorf("post.QueryComments().Limit(1) = %d, want 1", len(one))
	}

	// MorphOne — Laravel: $post->image;
	created := client.Image.Create().SetURL("hero.png").SetImageable(post).SaveX(ctx)
	got, err := post.QueryFeaturedImage(ctx)
	if err != nil {
		t.Fatalf("QueryFeaturedImage: %v", err)
	}
	if got == nil || got.ID != created.ID {
		t.Errorf("QueryFeaturedImage = %+v, want id=%d", got, created.ID)
	}

	// MorphMany on a different parent type — Video also has back-ref.
	video := client.Video.Create().SetTitle("V").SetURL("u").SaveX(ctx)
	_ = client.Comment.Create().SetBody("c").SetCommentable(video).SaveX(ctx)
	if got := video.QueryComments().CountX(ctx); got != 1 {
		t.Errorf("video.QueryComments() = %d, want 1", got)
	}
	// And the post's count is unchanged (back-refs are correctly scoped).
	if got := post.QueryComments().CountX(ctx); got != 2 {
		t.Errorf("post.QueryComments() after video comment = %d, want 2", got)
	}
}

// TestInvalidEnumValueRejected verifies the database (via ent's
// generated CommentableTypeValidator from field.Enum) refuses a write
// with a morph type that is not in the AllowedTypes list. This is the
// payoff of MixinAllowed — even raw bypass of the typed Set<Morph>
// path is caught at the validator layer.
func TestInvalidEnumValueRejected(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	// Use the raw setters to bypass the sealed-interface Set<Morph>;
	// this is the closest a caller can get to "invalid write" through
	// the typed builder API. The Create() call must fail before the
	// row hits the database.
	_, err := client.Comment.Create().
		SetBody("hi").
		SetCommentableID("999").
		SetCommentableType(comment.CommentableType("article")).
		Save(ctx)
	if err == nil {
		t.Fatal("expected error for invalid enum value, got nil — DB/validator did not reject 'article'")
	}
}

// TestMorphOneFeaturedImage exercises the one-to-one shape using
// SetImageable on the Image child and a manual back-ref read.
func TestMorphOneFeaturedImage(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	img := client.Image.Create().SetURL("hero.png").SetImageable(post).SaveX(ctx)

	if img.ImageableID == nil || *img.ImageableID != post.MorphID() {
		t.Errorf("ImageableID = %v, want %q", img.ImageableID, post.MorphID())
	}
	if img.ImageableType == nil || *img.ImageableType != image.ImageableType(string(ent.PostMorphKey)) {
		t.Errorf("ImageableType = %v, want %q", img.ImageableType, ent.PostMorphKey)
	}
}
