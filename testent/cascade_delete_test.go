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

// TestCascadeDeleteUser_Basic covers a full single-entity cascade: deleting a
// user removes the user, their posts, each post's comments, each post's M2M
// junction rows (post_tags), and the user's profile — while unrelated rows
// (the Tag itself, referenced only via the junction) survive untouched.
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

// TestCascadeDeletePost_Nested covers a mid-tree cascade: deleting a post
// (not the top-level user) removes the post itself, its comments, and its
// M2M junction rows — while the parent user and the referenced tags remain.
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

// TestCascadeDeleteCategory_Unlink covers the WithUnlink rule: deleting a
// category SET-NULLs the category_id on its posts instead of deleting them.
// The post survives with category_id = NULL.
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

// TestCascadeDeleteArticle_SoftDelete covers auto-detected soft delete:
// because Revision has a deleted_at field, cascading an Article must set
// deleted_at on revisions rather than hard-deleting them. The article row
// itself is still hard-deleted.
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
// Test 5: SkipEdges — Team cascade deletes Members but preserves AuditLogs
// ---------------------------------------------------------------------------

// TestCascadeDeleteTeam_SkipEdges covers the SkipEdges rule on a real O2M
// edge that entcascade would otherwise walk. Team.Cascade(SkipEdges(
// "audit_logs")) preserves compliance history when a team is deleted.
// Members (non-skipped O2M) are still removed.
//
// Bonus assertion: the owner user also survives. That's NOT because of
// SkipEdges — it's because Team owns the owner_id FK, making "owner" a
// parent-pointing M2O edge that entcascade auto-skips. Adding
// SkipEdges("owner") would have been a no-op.
func TestCascadeDeleteTeam_SkipEdges(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	owner := client.User.Create().SetName("owner").SetEmail("owner@test.com").SaveX(ctx)
	team := client.Team.Create().SetName("alpha").SetOwnerID(owner.ID).SaveX(ctx)
	member1 := client.User.Create().SetName("m1").SetEmail("m1@test.com").SaveX(ctx)
	client.Member.Create().SetRole("dev").SetTeamID(team.ID).SetUserID(member1.ID).SaveX(ctx)
	// Audit logs attached to the team — must survive the cascade.
	client.AuditLog.Create().SetAction("created").SetTeamID(team.ID).SaveX(ctx)
	client.AuditLog.Create().SetAction("renamed").SetTeamID(team.ID).SaveX(ctx)

	if err := ent.CascadeDeleteTeam(ctx, client, team.ID); err != nil {
		t.Fatalf("CascadeDeleteTeam: %v", err)
	}

	// Team gone.
	if n := client.Team.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 teams, got %d", n)
	}
	// Members gone (non-skipped O2M).
	if n := client.Member.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 members, got %d", n)
	}
	// Audit logs survive — THIS is the SkipEdges assertion.
	if n := client.AuditLog.Query().CountX(ctx); n != 2 {
		t.Errorf("expected 2 audit logs preserved (SkipEdges), got %d", n)
	}
	// Bonus: owner + member users survive (M2O auto-skip + members is a
	// separate entity table, not User).
	if n := client.User.Query().CountX(ctx); n != 2 {
		t.Errorf("expected 2 users (owner + member1 survive), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 6: Batch delete — multiple users at once
// ---------------------------------------------------------------------------

// TestCascadeDeleteUserBatch covers the batch API: deleting multiple users
// in a single transaction cascades each one's subtree, using IN (...)
// predicates instead of per-id queries.
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

// TestCascadeDeleteUserBatch_Empty covers the empty-slice short-circuit:
// calling the batch API with no ids must be a zero-query no-op, not an error.
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

// TestCascadeDeleteUser_WithHooks covers the Pre/Post hook injection points:
// Pre runs before any delete ops (can see the full entity), Post runs after
// all deletes (can observe post-deletion state), both execute inside the
// cascade transaction so they see transactional state.
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

// TestCascadeDeleteUser_PreHookError covers pre-hook abort semantics: a Pre
// hook returning an error must abort the cascade and roll back the
// transaction, leaving the entity untouched.
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

// TestCascadeDeleteUser_Idempotent covers double-delete safety: re-running
// CascadeDelete on an already-deleted id must not error, because generated
// WHERE clauses match zero rows rather than raising "not found".
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

// TestCascadeDeleteUser_DeepNested covers fan-out at depth: a user with
// multiple posts, each with multiple comments. Verifies the cascade queries
// mid-level ids then uses IN (...) to remove grandchildren in bulk.
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

// TestCascadeDeleteUser_NoChildren covers the no-children case: cascade still
// deletes the root entity cleanly when it has zero dependents (the generated
// mid-level IN(...) guards must handle empty id slices without erroring).
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

// TestCascadeDeleteUser_PostHookError covers late-failure rollback: a Post
// hook returning an error must roll back even though delete ops already ran.
// Verifies the entire subtree reappears after rollback (nothing committed).
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

// TestCascadeDeleteUserBatch_SingleItem covers the batch API with a
// one-element slice: behavior must match the single-entity call, confirming
// callers can pass variable-length slices without a special case.
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

// TestCascadeDeleteCategory_UnlinkNoChildren covers WithUnlink on an entity
// with zero linked children: the category is deleted and the UPDATE SET NULL
// runs against zero rows (no error, no side effects).
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

// TestCascadeDeletePost_MultipleTags covers M2M junction cleanup at scale:
// a post with N tags has N junction rows deleted in one statement, and all
// the originally-referenced tags survive (junction-only deletion).
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

// TestCascadeDeleteArticle_SoftDeleteIdempotent covers the soft-delete path
// under repeated calls: after the article is gone, a second CascadeDelete
// on the same id finds no rows (WHERE article_id = <gone>) and returns
// cleanly without touching anything.
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

// TestCascadeDeleteCategoryBatch_Unlink covers batching combined with
// WithUnlink: the generated code must emit a single UPDATE ... IN (...) to
// clear category_id across every matching post at once, then delete all
// categories. Posts from every deleted category survive with NULL category_id.
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

// TestCascadeDeleteUser_ProfileOnly covers a partial subtree: the user has
// a 1:1 Profile but no posts. Verifies the cascade still executes every
// registered op (including those that find zero rows to act on) without
// short-circuiting.
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

// TestCascadeDeleteArticleBatch_SoftDelete covers batch + auto-soft-delete
// together: revisions across every batched article get deleted_at set via
// a single UPDATE ... WHERE article_id IN (...), and every revision row
// remains in the table (soft, not hard).
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

// TestCascadeDeleteUser_Isolation covers blast-radius containment: deleting
// user A leaves user B's full subtree (posts, comments, profile, M2M rows)
// intact. Guards against accidental cross-owner queries missing a WHERE.
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

// TestCascadeDeleteCategory_UnlinkPreservesData covers unlink fidelity:
// after SET category_id = NULL, every other column on the post (title,
// body, author_id) is unchanged. Guards against an accidental bulk-UPDATE
// that overwrites more than the FK.
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

// TestCascadeDeleteTeamBatch covers batch + SkipEdges together: deleting
// multiple teams cascades members across every batched team while
// preserving audit logs (SkipEdges) and users (owner is auto-skipped M2O;
// member user rows live in a separate table from Member join rows).
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
	// Audit logs for each team — must all survive.
	client.AuditLog.Create().SetAction("t1-created").SetTeamID(t1.ID).SaveX(ctx)
	client.AuditLog.Create().SetAction("t2-created").SetTeamID(t2.ID).SaveX(ctx)
	client.AuditLog.Create().SetAction("t2-renamed").SetTeamID(t2.ID).SaveX(ctx)

	if err := ent.CascadeDeleteTeamBatch(ctx, client, []int{t1.ID, t2.ID}); err != nil {
		t.Fatalf("CascadeDeleteTeamBatch: %v", err)
	}

	if n := client.Team.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 teams, got %d", n)
	}
	if n := client.Member.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 members, got %d", n)
	}
	// Audit logs across both teams survive — SkipEdges applied to batch path.
	if n := client.AuditLog.Query().CountX(ctx); n != 3 {
		t.Errorf("expected 3 audit logs preserved, got %d", n)
	}
	// All users survive (owner auto-skipped, members join table lives elsewhere).
	if n := client.User.Query().CountX(ctx); n != 3 {
		t.Errorf("expected 3 users (all survive), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 24: Nonexistent ID — cascade delete on ID that never existed
// ---------------------------------------------------------------------------

// TestCascadeDeleteUser_NonexistentID covers calling cascade on an id that
// never existed: every delete/update hits zero rows and returns cleanly.
// Ensures the cascade contract is "id-based", not "existence-based".
func TestCascadeDeleteUser_NonexistentID(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	if err := ent.CascadeDeleteUser(ctx, client, 99999); err != nil {
		t.Fatalf("expected no error for nonexistent ID, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 25: NESTED-ANNOTATION REGRESSION — Workspace → Folder → Channel.
// Folder carries WithUnlink("channels"); that rule MUST be respected when
// Workspace cascades through Folder. Before the fix, buildChildOps ignored
// intermediate-type annotations and hard-deleted channels via their folder_id.
// After the fix, channels are unlinked (folder_id = NULL) and survive.
// ---------------------------------------------------------------------------

// TestCascadeDeleteWorkspace_NestedUnlink covers the nested-annotation
// regression: Folder (intermediate) carries WithUnlink("channels"), so when
// Workspace cascades through Folder the channels MUST be unlinked, not
// hard-deleted. Before the fix, buildChildOps ignored intermediate
// annotations and channels were erroneously deleted.
func TestCascadeDeleteWorkspace_NestedUnlink(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	ws := client.Workspace.Create().SetName("ws-unlink").SaveX(ctx)
	f := client.Folder.Create().SetName("f1").SetWorkspaceID(ws.ID).SaveX(ctx)
	c := client.Channel.Create().SetName("c1").SetFolderID(f.ID).SaveX(ctx)

	if err := ent.CascadeDeleteWorkspace(ctx, client, ws.ID); err != nil {
		t.Fatalf("CascadeDeleteWorkspace: %v", err)
	}

	// Workspace + Folder hard-deleted.
	if n := client.Workspace.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 workspaces, got %d", n)
	}
	if n := client.Folder.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 folders, got %d", n)
	}

	// Channel MUST survive with folder_id cleared — this is the regression guard.
	survivor, err := client.Channel.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("expected channel to survive, got error: %v", err)
	}
	if survivor.FolderID != nil {
		t.Errorf("expected channel.folder_id = NULL (unlinked); got %v", *survivor.FolderID)
	}
	if survivor.Name != "c1" {
		t.Errorf("expected channel name preserved; got %q", survivor.Name)
	}
}

// ---------------------------------------------------------------------------
// Test 26: NESTED-ANNOTATION REGRESSION — Workspace → Doc → Note.
// Doc carries WithSoftDelete("notes", "archived_at"). The non-default field
// name is deliberate: auto-detect only looks for "deleted_at", so this can
// only pass if Doc's annotation is consulted during the nested walk.
// ---------------------------------------------------------------------------

// TestCascadeDeleteWorkspace_NestedSoftDelete covers the nested-annotation
// regression for the soft-delete rule: Doc (intermediate) carries
// WithSoftDelete("notes", "archived_at"). The custom field name is deliberate
// — auto-detect only looks for "deleted_at", so the only way notes get
// archived_at set during Workspace → Doc → Note is by reading Doc's
// annotation during the nested walk.
func TestCascadeDeleteWorkspace_NestedSoftDelete(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	ws := client.Workspace.Create().SetName("ws-soft").SaveX(ctx)
	d := client.Doc.Create().SetTitle("d1").SetWorkspaceID(ws.ID).SaveX(ctx)
	n1 := client.Note.Create().SetBody("n1").SetDocID(d.ID).SaveX(ctx)
	n2 := client.Note.Create().SetBody("n2").SetDocID(d.ID).SaveX(ctx)

	if err := ent.CascadeDeleteWorkspace(ctx, client, ws.ID); err != nil {
		t.Fatalf("CascadeDeleteWorkspace: %v", err)
	}

	// Workspace + Doc hard-deleted.
	if n := client.Workspace.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 workspaces, got %d", n)
	}
	if n := client.Doc.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 docs, got %d", n)
	}

	// Notes survive with archived_at set (soft-deleted via intermediate rule).
	notes := client.Note.Query().AllX(ctx)
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes (soft-deleted), got %d", len(notes))
	}
	seen := map[int]bool{n1.ID: false, n2.ID: false}
	for _, note := range notes {
		if note.ArchivedAt == nil {
			t.Errorf("note %d: expected archived_at set; got nil", note.ID)
		}
		seen[note.ID] = true
	}
	for id, ok := range seen {
		if !ok {
			t.Errorf("note %d not found after cascade", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 27: NESTED-ANNOTATION — direct Workspace delete with no children
// leaves the channel unlink and doc soft-delete as no-ops.
// ---------------------------------------------------------------------------

// TestCascadeDeleteWorkspace_EmptyChildren covers the empty-children edge
// for the nested-annotation code path: an annotated intermediate type with
// no dependents must still complete cleanly. Confirms the if-len > 0 guards
// generated around nested cascades work for the Workspace chain too.
func TestCascadeDeleteWorkspace_EmptyChildren(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	ws := client.Workspace.Create().SetName("ws-empty").SaveX(ctx)

	if err := ent.CascadeDeleteWorkspace(ctx, client, ws.ID); err != nil {
		t.Fatalf("CascadeDeleteWorkspace empty: %v", err)
	}
	if n := client.Workspace.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 workspaces, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Nested-tx composition tests (entcascade-nested-tx-plan.md)
// ---------------------------------------------------------------------------

// TestCascadeNestedTx_NoErrTxStarted covers the primary regression: calling
// a cascade function with tx.Client() must NOT return ent.ErrTxStarted. The
// generated code detects the transactional client and reuses it.
func TestCascadeNestedTx_NoErrTxStarted(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("nt1").SetEmail("nt1@test.com").SaveX(ctx)

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("start tx: %v", err)
	}
	if err := ent.CascadeDeleteUser(ctx, tx.Client(), u.ID); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, ent.ErrTxStarted) {
			t.Fatalf("cascade returned ErrTxStarted; nested-tx path not engaged")
		}
		t.Fatalf("CascadeDeleteUser inside tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if n := client.User.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 users after committed nested cascade, got %d", n)
	}
}

// TestCascadeNestedTx_ComposeMultiple covers the headline use case:
// multiple cascade calls inside a single caller-owned transaction all share
// one DB transaction and commit atomically.
func TestCascadeNestedTx_ComposeMultiple(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	uA := client.User.Create().SetName("A").SetEmail("a@test.com").SaveX(ctx)
	uB := client.User.Create().SetName("B").SetEmail("b@test.com").SaveX(ctx)
	pA := client.Post.Create().SetTitle("pA").SetBody("b").SetAuthorID(uA.ID).SaveX(ctx)
	pB := client.Post.Create().SetTitle("pB").SetBody("b").SetAuthorID(uB.ID).SaveX(ctx)
	client.Comment.Create().SetBody("cA").SetPostID(pA.ID).SaveX(ctx)
	client.Comment.Create().SetBody("cB").SetPostID(pB.ID).SaveX(ctx)

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("start tx: %v", err)
	}
	if err := ent.CascadeDeleteUser(ctx, tx.Client(), uA.ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("cascade A: %v", err)
	}
	if err := ent.CascadeDeleteUserBatch(ctx, tx.Client(), []int{uB.ID}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("cascade B batch: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if n := client.User.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 users after combined cascades, got %d", n)
	}
	if n := client.Post.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 posts, got %d", n)
	}
	if n := client.Comment.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 comments, got %d", n)
	}
}

// TestCascadeNestedTx_WithHooks covers hook semantics under composition:
// Pre/Post hooks still fire when the cascade is reusing an outer tx, and
// they observe transactional state (pre sees the entity, post does not).
func TestCascadeNestedTx_WithHooks(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("hk").SetEmail("hk@test.com").SaveX(ctx)

	var preRan, postRan bool
	hooks := ent.CascadeDeleteUserHooks{
		Pre: func(ctx context.Context, c *ent.Client, id int) error {
			preRan = true
			if n := c.User.Query().CountX(ctx); n != 1 {
				t.Errorf("pre hook: expected 1 user in tx, got %d", n)
			}
			return nil
		},
		Post: func(ctx context.Context, c *ent.Client, id int) error {
			postRan = true
			if n := c.User.Query().CountX(ctx); n != 0 {
				t.Errorf("post hook: expected 0 users in tx, got %d", n)
			}
			return nil
		},
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("start tx: %v", err)
	}
	if err := ent.CascadeDeleteUserWithHooks(ctx, tx.Client(), u.ID, hooks); err != nil {
		_ = tx.Rollback()
		t.Fatalf("cascade with hooks inside tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if !preRan || !postRan {
		t.Errorf("hooks did not both run: pre=%v post=%v", preRan, postRan)
	}
	if n := client.User.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 users after committed cascade, got %d", n)
	}
}

// TestCascadeNestedTx_OuterRollbackUndoesCascade covers atomicity across
// the outer boundary: after a cascade runs successfully inside a tx, the
// caller rolling back must undo the cascaded deletes. Proves the cascade
// truly reused the outer tx rather than quietly committing its own.
func TestCascadeNestedTx_OuterRollbackUndoesCascade(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("rb").SetEmail("rb@test.com").SaveX(ctx)
	client.Post.Create().SetTitle("keep").SetBody("b").SetAuthorID(u.ID).SaveX(ctx)

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("start tx: %v", err)
	}
	if err := ent.CascadeDeleteUser(ctx, tx.Client(), u.ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("cascade inside tx: %v", err)
	}
	// Caller decides to abort.
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if n := client.User.Query().CountX(ctx); n != 1 {
		t.Errorf("expected user to survive outer rollback, got count=%d", n)
	}
	if n := client.Post.Query().CountX(ctx); n != 1 {
		t.Errorf("expected post to survive outer rollback, got count=%d", n)
	}
}

// TestCascadeNestedTx_HookErrorRollsBackOuter covers failure propagation
// from a hook to the outer tx: when Pre returns an error inside a nested-
// tx call, the cascade returns the error without committing, and the
// caller's rollback undoes earlier steps in the same tx.
func TestCascadeNestedTx_HookErrorRollsBackOuter(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	uA := client.User.Create().SetName("survive").SetEmail("s@test.com").SaveX(ctx)
	uB := client.User.Create().SetName("abort").SetEmail("abort@test.com").SaveX(ctx)

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("start tx: %v", err)
	}

	// Step 1: delete uA successfully inside the tx.
	if err := ent.CascadeDeleteUser(ctx, tx.Client(), uA.ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("cascade A: %v", err)
	}

	// Step 2: uB cascade whose Pre hook aborts.
	hooks := ent.CascadeDeleteUserHooks{
		Pre: func(ctx context.Context, c *ent.Client, id int) error {
			return errors.New("pre abort")
		},
	}
	err = ent.CascadeDeleteUserWithHooks(ctx, tx.Client(), uB.ID, hooks)
	if err == nil {
		_ = tx.Rollback()
		t.Fatal("expected Pre hook error, got nil")
	}
	// Caller observes the error and rolls back the outer tx.
	if rerr := tx.Rollback(); rerr != nil {
		t.Fatalf("rollback: %v", rerr)
	}

	// Both users must survive the rolled-back outer tx.
	if n := client.User.Query().CountX(ctx); n != 2 {
		t.Errorf("expected 2 users after outer rollback, got %d", n)
	}
}

// TestCascadeNestedTx_StandaloneStillWorks covers the non-regression side:
// calling a cascade without an outer tx must still create and commit its
// own transaction (the existing contract is unchanged).
func TestCascadeNestedTx_StandaloneStillWorks(t *testing.T) {
	client := newClient(t)
	defer client.Close()
	ctx := context.Background()

	u := client.User.Create().SetName("solo").SetEmail("solo@test.com").SaveX(ctx)

	if err := ent.CascadeDeleteUser(ctx, client, u.ID); err != nil {
		t.Fatalf("standalone cascade: %v", err)
	}
	if n := client.User.Query().CountX(ctx); n != 0 {
		t.Errorf("expected 0 users, got %d", n)
	}
}
