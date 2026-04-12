package testent

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/khanakia/entx/testent/ent"
	"github.com/khanakia/entx/testent/ent/enttest"
	"github.com/khanakia/entx/testent/ent/migrate"

	_ "github.com/khanakia/entx/testent/ent/runtime"
	_ "modernc.org/sqlite"
)

// newClient creates a fresh in-memory SQLite client with auto-migration.
func newClient(t *testing.T) *ent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", "file:ent?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	// Enable FK pragma so ent's migration check passes, but skip FK creation
	// in the schema (WithForeignKeys=false) — this mirrors real usage where
	// entcascade handles cascades at the app level.
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	drv := entsql.OpenDB(dialect.SQLite, db)
	return enttest.NewClient(t,
		enttest.WithOptions(ent.Driver(drv)),
		enttest.WithMigrateOptions(migrate.WithForeignKeys(false)),
	)
}

// ---------------------------------------------------------------------------
// Test 1: Basic O2M cascade — User → Posts → Comments + PostTags
// ---------------------------------------------------------------------------

func TestCascadeDeleteUser_Basic(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	// Create user with a post that has a comment and a tag.
	u := client.User.Create().SetName("alice").SetEmail("alice@test.com").SaveX(ctx)
	p := client.Post.Create().SetTitle("hello").SetBody("world").SetAuthorID(u.ID).SaveX(ctx)
	client.Comment.Create().SetBody("nice post").SetPostID(p.ID).SaveX(ctx)
	tag := client.Tag.Create().SetName("go").SaveX(ctx)
	client.PostTag.Create().SetPostID(p.ID).SetTagID(tag.ID).SaveX(ctx)
	client.Profile.Create().SetBio("dev").SetUserID(u.ID).SaveX(ctx)

	// Cascade delete user.
	if err := ent.CascadeDeleteUser(ctx, client, u.ID); err != nil {
		t.Fatalf("CascadeDeleteUser: %v", err)
	}

	// User gone.
	if n := client.User.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 users, got %d", n)
	}
	// Post gone.
	if n := client.Post.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 posts, got %d", n)
	}
	// Comment gone.
	if n := client.Comment.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 comments, got %d", n)
	}
	// PostTag junction gone.
	if n := client.PostTag.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 post_tags, got %d", n)
	}
	// Profile gone.
	if n := client.Profile.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 profiles, got %d", n)
	}
	// Tag survives (only junction row is deleted).
	if n := client.Tag.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 tag (survived), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Nested cascade — Post has Comments + PostTags
// ---------------------------------------------------------------------------

func TestCascadeDeletePost_Nested(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("bob").SetEmail("bob@test.com").SaveX(ctx)
	p := client.Post.Create().SetTitle("post1").SetBody("body1").SetAuthorID(u.ID).SaveX(ctx)
	client.Comment.Create().SetBody("c1").SetPostID(p.ID).SaveX(ctx)
	client.Comment.Create().SetBody("c2").SetPostID(p.ID).SaveX(ctx)
	tag := client.Tag.Create().SetName("rust").SaveX(ctx)
	client.PostTag.Create().SetPostID(p.ID).SetTagID(tag.ID).SaveX(ctx)

	if err := ent.CascadeDeletePost(ctx, client, p.ID); err != nil {
		t.Fatalf("CascadeDeletePost: %v", err)
	}

	if n := client.Post.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 posts, got %d", n)
	}
	if n := client.Comment.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 comments, got %d", n)
	}
	if n := client.PostTag.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 post_tags, got %d", n)
	}
	// User survives.
	if n := client.User.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 user (survived), got %d", n)
	}
	// Tag survives.
	if n := client.Tag.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 tag (survived), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 3: Unlink — Category deletion clears FK on Posts (posts survive)
// ---------------------------------------------------------------------------

func TestCascadeDeleteCategory_Unlink(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	cat := client.Category.Create().SetName("tech").SaveX(ctx)
	u := client.User.Create().SetName("carol").SetEmail("carol@test.com").SaveX(ctx)
	p := client.Post.Create().SetTitle("linked").SetBody("body").SetAuthorID(u.ID).SetCategoryID(cat.ID).SaveX(ctx)

	if err := ent.CascadeDeleteCategory(ctx, client, cat.ID); err != nil {
		t.Fatalf("CascadeDeleteCategory: %v", err)
	}

	// Category gone.
	if n := client.Category.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 categories, got %d", n)
	}
	// Post survives with nil category_id.
	post := client.Post.GetX(ctx, p.ID)
	if post.CategoryID != nil {
		t.Errorf("expected nil category_id, got %v", *post.CategoryID)
	}
}

// ---------------------------------------------------------------------------
// Test 4: Soft delete — Article deletion soft-deletes Revisions
// ---------------------------------------------------------------------------

func TestCascadeDeleteArticle_SoftDelete(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	art := client.Article.Create().SetTitle("design doc").SaveX(ctx)
	client.Revision.Create().SetBody("v1").SetVersion(1).SetArticleID(art.ID).SaveX(ctx)
	client.Revision.Create().SetBody("v2").SetVersion(2).SetArticleID(art.ID).SaveX(ctx)

	if err := ent.CascadeDeleteArticle(ctx, client, art.ID); err != nil {
		t.Fatalf("CascadeDeleteArticle: %v", err)
	}

	// Article hard-deleted.
	if n := client.Article.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 articles, got %d", n)
	}
	// Revisions still exist (soft-deleted — deleted_at set).
	revisions := client.Revision.Query().AllX(ctx)
	if len(revisions) != 2 {
		t.Fatalf("expected 2 revisions (soft-deleted), got %d", len(revisions))
	}
	for _, r := range revisions {
		if r.DeletedAt == nil {
			t.Errorf("revision %d: expected deleted_at to be set, got nil", r.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 5: SkipEdges — Team deletion cascades Members, skips Owner
// ---------------------------------------------------------------------------

func TestCascadeDeleteTeam_SkipOwner(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	owner := client.User.Create().SetName("owner").SetEmail("owner@test.com").SaveX(ctx)
	team := client.Team.Create().SetName("alpha").SetOwnerID(owner.ID).SaveX(ctx)
	member1 := client.User.Create().SetName("m1").SetEmail("m1@test.com").SaveX(ctx)
	client.Member.Create().SetRole("dev").SetTeamID(team.ID).SetUserID(member1.ID).SaveX(ctx)

	if err := ent.CascadeDeleteTeam(ctx, client, team.ID); err != nil {
		t.Fatalf("CascadeDeleteTeam: %v", err)
	}

	// Team gone.
	if n := client.Team.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 teams, got %d", n)
	}
	// Members gone.
	if n := client.Member.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 members, got %d", n)
	}
	// Owner survives (edge was skipped).
	if n := client.User.Query().CountX(ctx); n != 2 {
		t.Errorf("expected 2 users (owner + member1 survive), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 6: Batch delete — multiple users at once
// ---------------------------------------------------------------------------

func TestCascadeDeleteUserBatch(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u1 := client.User.Create().SetName("u1").SetEmail("u1@test.com").SaveX(ctx)
	u2 := client.User.Create().SetName("u2").SetEmail("u2@test.com").SaveX(ctx)
	p1 := client.Post.Create().SetTitle("p1").SetBody("b1").SetAuthorID(u1.ID).SaveX(ctx)
	p2 := client.Post.Create().SetTitle("p2").SetBody("b2").SetAuthorID(u2.ID).SaveX(ctx)
	client.Comment.Create().SetBody("c1").SetPostID(p1.ID).SaveX(ctx)
	client.Comment.Create().SetBody("c2").SetPostID(p2.ID).SaveX(ctx)

	if err := ent.CascadeDeleteUserBatch(ctx, client, []int{u1.ID, u2.ID}); err != nil {
		t.Fatalf("CascadeDeleteUserBatch: %v", err)
	}

	if n := client.User.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 users, got %d", n)
	}
	if n := client.Post.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 posts, got %d", n)
	}
	if n := client.Comment.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 comments, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 7: Batch delete with empty slice — no-op
// ---------------------------------------------------------------------------

func TestCascadeDeleteUserBatch_Empty(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	if err := ent.CascadeDeleteUserBatch(ctx, client, []int{}); err != nil {
		t.Fatalf("expected no error for empty batch, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 8: WithHooks — Pre/Post hooks fire inside transaction
// ---------------------------------------------------------------------------

func TestCascadeDeleteUser_WithHooks(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("hooked").SetEmail("hook@test.com").SaveX(ctx)

	var preRan, postRan bool
	hooks := ent.CascadeDeleteUserHooks{
		Pre: func(ctx context.Context, c *ent.Client, id int) error {
			preRan = true
			// Verify user still exists inside tx before delete.
			if n := c.User.Query().CountX(ctx); n != 1 {
				t.Errorf("pre hook: expected 1 user, got %d", n)
			}
			return nil
		},
		Post: func(ctx context.Context, c *ent.Client, id int) error {
			postRan = true
			// User is gone inside tx after delete.
			if n := c.User.Query().CountX(ctx); n != 0 {
				t.Errorf("post hook: expected 0 users, got %d", n)
			}
			return nil
		},
	}

	if err := ent.CascadeDeleteUserWithHooks(ctx, client, u.ID, hooks); err != nil {
		t.Fatalf("CascadeDeleteUserWithHooks: %v", err)
	}

	if !preRan {
		t.Error("pre hook did not run")
	}
	if !postRan {
		t.Error("post hook did not run")
	}
}

// ---------------------------------------------------------------------------
// Test 9: Pre hook error aborts and rolls back
// ---------------------------------------------------------------------------

func TestCascadeDeleteUser_PreHookError(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("abort").SetEmail("abort@test.com").SaveX(ctx)

	hooks := ent.CascadeDeleteUserHooks{
		Pre: func(ctx context.Context, c *ent.Client, id int) error {
			return errors.New("abort delete")
		},
	}

	err := ent.CascadeDeleteUserWithHooks(ctx, client, u.ID, hooks)
	if err == nil {
		t.Fatal("expected error from pre hook, got nil")
	}

	// User should still exist (rolled back).
	if n := client.User.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 user after rollback, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 10: Idempotent — deleting already-deleted entity is not an error
// ---------------------------------------------------------------------------

func TestCascadeDeleteUser_Idempotent(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("gone").SetEmail("gone@test.com").SaveX(ctx)

	// Delete once.
	if err := ent.CascadeDeleteUser(ctx, client, u.ID); err != nil {
		t.Fatalf("first delete: %v", err)
	}

	// Delete again — should not error (idempotent WHERE clause).
	if err := ent.CascadeDeleteUser(ctx, client, u.ID); err != nil {
		t.Fatalf("second delete (idempotent): %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 11: Deep nested — User → Post → Comment (3-level cascade)
// ---------------------------------------------------------------------------

func TestCascadeDeleteUser_DeepNested(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("deep").SetEmail("deep@test.com").SaveX(ctx)
	p1 := client.Post.Create().SetTitle("p1").SetBody("b").SetAuthorID(u.ID).SaveX(ctx)
	p2 := client.Post.Create().SetTitle("p2").SetBody("b").SetAuthorID(u.ID).SaveX(ctx)
	// Multiple comments per post.
	for i := 0; i < 3; i++ {
		client.Comment.Create().SetBody("c").SetPostID(p1.ID).SaveX(ctx)
		client.Comment.Create().SetBody("c").SetPostID(p2.ID).SaveX(ctx)
	}

	if err := ent.CascadeDeleteUser(ctx, client, u.ID); err != nil {
		t.Fatalf("CascadeDeleteUser deep: %v", err)
	}

	if n := client.Comment.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 comments after deep cascade, got %d", n)
	}
	if n := client.Post.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 posts after deep cascade, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 12: Parent with no children — cascade is a no-op for children
// ---------------------------------------------------------------------------

func TestCascadeDeleteUser_NoChildren(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("loner").SetEmail("loner@test.com").SaveX(ctx)

	if err := ent.CascadeDeleteUser(ctx, client, u.ID); err != nil {
		t.Fatalf("CascadeDeleteUser no children: %v", err)
	}

	if n := client.User.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 users, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 13: Post hook error — rollback after delete ops already executed
// ---------------------------------------------------------------------------

func TestCascadeDeleteUser_PostHookError(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("postfail").SetEmail("postfail@test.com").SaveX(ctx)
	client.Post.Create().SetTitle("p").SetBody("b").SetAuthorID(u.ID).SaveX(ctx)

	hooks := ent.CascadeDeleteUserHooks{
		Post: func(ctx context.Context, c *ent.Client, id int) error {
			return errors.New("post hook failed")
		},
	}

	err := ent.CascadeDeleteUserWithHooks(ctx, client, u.ID, hooks)
	if err == nil {
		t.Fatal("expected error from post hook, got nil")
	}

	// Everything should be rolled back — user and post still exist.
	if n := client.User.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 user after rollback, got %d", n)
	}
	if n := client.Post.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 post after rollback, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 14: Batch with single item — same behavior as single delete
// ---------------------------------------------------------------------------

func TestCascadeDeleteUserBatch_SingleItem(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("solo").SetEmail("solo@test.com").SaveX(ctx)
	p := client.Post.Create().SetTitle("p").SetBody("b").SetAuthorID(u.ID).SaveX(ctx)
	client.Comment.Create().SetBody("c").SetPostID(p.ID).SaveX(ctx)

	if err := ent.CascadeDeleteUserBatch(ctx, client, []int{u.ID}); err != nil {
		t.Fatalf("batch single: %v", err)
	}

	if n := client.User.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 users, got %d", n)
	}
	if n := client.Comment.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 comments, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 15: Unlink with no linked posts — no-op, no error
// ---------------------------------------------------------------------------

func TestCascadeDeleteCategory_UnlinkNoChildren(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	cat := client.Category.Create().SetName("empty-cat").SaveX(ctx)

	if err := ent.CascadeDeleteCategory(ctx, client, cat.ID); err != nil {
		t.Fatalf("CascadeDeleteCategory no children: %v", err)
	}

	if n := client.Category.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 categories, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 16: Multiple tags on a single post — all junction rows removed
// ---------------------------------------------------------------------------

func TestCascadeDeletePost_MultipleTags(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("tagger").SetEmail("tagger@test.com").SaveX(ctx)
	p := client.Post.Create().SetTitle("tagged").SetBody("b").SetAuthorID(u.ID).SaveX(ctx)

	for _, name := range []string{"go", "rust", "zig", "c"} {
		tag := client.Tag.Create().SetName(name).SaveX(ctx)
		client.PostTag.Create().SetPostID(p.ID).SetTagID(tag.ID).SaveX(ctx)
	}

	if err := ent.CascadeDeletePost(ctx, client, p.ID); err != nil {
		t.Fatalf("CascadeDeletePost multiple tags: %v", err)
	}

	if n := client.PostTag.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 post_tags, got %d", n)
	}
	// All 4 tags survive.
	if n := client.Tag.Query().CountX(ctx); n != 4 {
		t.Errorf("expected 4 tags (survived), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 17: Soft delete idempotent — second delete re-sets deleted_at
// ---------------------------------------------------------------------------

func TestCascadeDeleteArticle_SoftDeleteIdempotent(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	art := client.Article.Create().SetTitle("twice").SaveX(ctx)
	client.Revision.Create().SetBody("v1").SetVersion(1).SetArticleID(art.ID).SaveX(ctx)

	// First delete.
	if err := ent.CascadeDeleteArticle(ctx, client, art.ID); err != nil {
		t.Fatalf("first delete: %v", err)
	}

	// Second delete — article is gone, but revision still has article_id pointing
	// to a non-existent article. The cascade should not error.
	if err := ent.CascadeDeleteArticle(ctx, client, art.ID); err != nil {
		t.Fatalf("second delete (idempotent): %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 18: Batch category unlink — multiple categories at once
// ---------------------------------------------------------------------------

func TestCascadeDeleteCategoryBatch_Unlink(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	cat1 := client.Category.Create().SetName("cat1").SaveX(ctx)
	cat2 := client.Category.Create().SetName("cat2").SaveX(ctx)
	u := client.User.Create().SetName("multi").SetEmail("multi@test.com").SaveX(ctx)
	p1 := client.Post.Create().SetTitle("p1").SetBody("b").SetAuthorID(u.ID).SetCategoryID(cat1.ID).SaveX(ctx)
	p2 := client.Post.Create().SetTitle("p2").SetBody("b").SetAuthorID(u.ID).SetCategoryID(cat2.ID).SaveX(ctx)

	if err := ent.CascadeDeleteCategoryBatch(ctx, client, []int{cat1.ID, cat2.ID}); err != nil {
		t.Fatalf("CascadeDeleteCategoryBatch: %v", err)
	}

	if n := client.Category.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 categories, got %d", n)
	}
	// Both posts survive with nil category_id.
	for _, id := range []int{p1.ID, p2.ID} {
		post := client.Post.GetX(ctx, id)
		if post.CategoryID != nil {
			t.Errorf("post %d: expected nil category_id, got %v", id, *post.CategoryID)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 19: User with profile but no posts — only profile cascaded
// ---------------------------------------------------------------------------

func TestCascadeDeleteUser_ProfileOnly(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("profiler").SetEmail("profiler@test.com").SaveX(ctx)
	client.Profile.Create().SetBio("just a profile").SetUserID(u.ID).SaveX(ctx)

	if err := ent.CascadeDeleteUser(ctx, client, u.ID); err != nil {
		t.Fatalf("CascadeDeleteUser profile only: %v", err)
	}

	if n := client.User.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 users, got %d", n)
	}
	if n := client.Profile.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 profiles, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 20: Batch article soft delete — multiple articles at once
// ---------------------------------------------------------------------------

func TestCascadeDeleteArticleBatch_SoftDelete(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	art1 := client.Article.Create().SetTitle("a1").SaveX(ctx)
	art2 := client.Article.Create().SetTitle("a2").SaveX(ctx)
	client.Revision.Create().SetBody("r1").SetVersion(1).SetArticleID(art1.ID).SaveX(ctx)
	client.Revision.Create().SetBody("r2").SetVersion(1).SetArticleID(art2.ID).SaveX(ctx)
	client.Revision.Create().SetBody("r3").SetVersion(2).SetArticleID(art2.ID).SaveX(ctx)

	if err := ent.CascadeDeleteArticleBatch(ctx, client, []int{art1.ID, art2.ID}); err != nil {
		t.Fatalf("CascadeDeleteArticleBatch: %v", err)
	}

	if n := client.Article.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 articles, got %d", n)
	}
	revisions := client.Revision.Query().AllX(ctx)
	if len(revisions) != 3 {
		t.Fatalf("expected 3 revisions (soft-deleted), got %d", len(revisions))
	}
	for _, r := range revisions {
		if r.DeletedAt == nil {
			t.Errorf("revision %d: expected deleted_at set, got nil", r.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 21: Isolation — deleting one user leaves other users' data intact
// ---------------------------------------------------------------------------

func TestCascadeDeleteUser_Isolation(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	// User A with full tree.
	uA := client.User.Create().SetName("userA").SetEmail("a@test.com").SaveX(ctx)
	pA := client.Post.Create().SetTitle("pA").SetBody("b").SetAuthorID(uA.ID).SaveX(ctx)
	client.Comment.Create().SetBody("cA").SetPostID(pA.ID).SaveX(ctx)
	client.Profile.Create().SetBio("bioA").SetUserID(uA.ID).SaveX(ctx)
	tagA := client.Tag.Create().SetName("tagA").SaveX(ctx)
	client.PostTag.Create().SetPostID(pA.ID).SetTagID(tagA.ID).SaveX(ctx)

	// User B with full tree.
	uB := client.User.Create().SetName("userB").SetEmail("b@test.com").SaveX(ctx)
	pB := client.Post.Create().SetTitle("pB").SetBody("b").SetAuthorID(uB.ID).SaveX(ctx)
	client.Comment.Create().SetBody("cB").SetPostID(pB.ID).SaveX(ctx)
	client.Profile.Create().SetBio("bioB").SetUserID(uB.ID).SaveX(ctx)
	client.PostTag.Create().SetPostID(pB.ID).SetTagID(tagA.ID).SaveX(ctx)

	// Delete only user A.
	if err := ent.CascadeDeleteUser(ctx, client, uA.ID); err != nil {
		t.Fatalf("CascadeDeleteUser A: %v", err)
	}

	// User B's entire tree is untouched.
	if n := client.User.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 user (B), got %d", n)
	}
	if n := client.Post.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 post (B's), got %d", n)
	}
	if n := client.Comment.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 comment (B's), got %d", n)
	}
	if n := client.Profile.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 profile (B's), got %d", n)
	}
	// B's junction row survives, A's is deleted.
	if n := client.PostTag.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 post_tag (B's), got %d", n)
	}
	// Tag survives (shared between A and B, only junction rows differ).
	if n := client.Tag.Query().CountX(ctx); n != 1 {
		t.Errorf("expected 1 tag (survived), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 22: Unlink preserves post's other fields
// ---------------------------------------------------------------------------

func TestCascadeDeleteCategory_UnlinkPreservesData(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	cat := client.Category.Create().SetName("will-die").SaveX(ctx)
	u := client.User.Create().SetName("writer").SetEmail("writer@test.com").SaveX(ctx)
	client.Post.Create().
		SetTitle("important").
		SetBody("keep this body").
		SetAuthorID(u.ID).
		SetCategoryID(cat.ID).
		SaveX(ctx)

	if err := ent.CascadeDeleteCategory(ctx, client, cat.ID); err != nil {
		t.Fatalf("CascadeDeleteCategory: %v", err)
	}

	post := client.Post.Query().OnlyX(ctx)
	if post.Title != "important" {
		t.Errorf("expected title 'important', got %q", post.Title)
	}
	if post.Body != "keep this body" {
		t.Errorf("expected body preserved, got %q", post.Body)
	}
	if post.AuthorID != u.ID {
		t.Errorf("expected author_id %d, got %d", u.ID, post.AuthorID)
	}
}

// ---------------------------------------------------------------------------
// Test 23: Batch team delete — multiple teams, shared users survive
// ---------------------------------------------------------------------------

func TestCascadeDeleteTeamBatch(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	owner := client.User.Create().SetName("boss").SetEmail("boss@test.com").SaveX(ctx)
	t1 := client.Team.Create().SetName("t1").SetOwnerID(owner.ID).SaveX(ctx)
	t2 := client.Team.Create().SetName("t2").SetOwnerID(owner.ID).SaveX(ctx)
	m1 := client.User.Create().SetName("m1").SetEmail("m1@test.com").SaveX(ctx)
	m2 := client.User.Create().SetName("m2").SetEmail("m2@test.com").SaveX(ctx)
	client.Member.Create().SetRole("dev").SetTeamID(t1.ID).SetUserID(m1.ID).SaveX(ctx)
	client.Member.Create().SetRole("pm").SetTeamID(t2.ID).SetUserID(m2.ID).SaveX(ctx)
	// m1 is also in t2.
	client.Member.Create().SetRole("dev").SetTeamID(t2.ID).SetUserID(m1.ID).SaveX(ctx)

	if err := ent.CascadeDeleteTeamBatch(ctx, client, []int{t1.ID, t2.ID}); err != nil {
		t.Fatalf("CascadeDeleteTeamBatch: %v", err)
	}

	if n := client.Team.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 teams, got %d", n)
	}
	if n := client.Member.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 members, got %d", n)
	}
	// All users survive (owner skipped, members are leaf nodes not users).
	if n := client.User.Query().CountX(ctx); n != 3 {
		t.Errorf("expected 3 users (all survive), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 24: Nonexistent ID — cascade delete on ID that never existed
// ---------------------------------------------------------------------------

func TestCascadeDeleteUser_NonexistentID(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	if err := ent.CascadeDeleteUser(ctx, client, 99999); err != nil {
		t.Fatalf("expected no error for nonexistent ID, got: %v", err)
	}
}
