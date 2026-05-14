// Runtime tests for the morphstring example: MorphMixin without
// MixinAllowed (the type column is field.String, not field.Enum).
// Exists to give end-to-end coverage to the template branches that fire
// when <ident>.<TypeField> is the predicate-EQ shortcut function rather
// than a named string type — the path the .GQL() bug shipped on because
// no example was exercising it.
package morphstring_test

import (
	"context"
	"database/sql"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"github.com/khanakia/entx/entpoly/examples/morphstring/ent"
	"github.com/khanakia/entx/entpoly/examples/morphstring/ent/comment"
)

func openTestClient(t *testing.T) *ent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", "file:morphstring?mode=memory&cache=shared&_pragma=foreign_keys(1)")
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
	return client
}

// TestSetCommentable_RoundTrip writes both discriminator columns via
// the typed setter, reads them back through the typed predicate, and
// resolves the parent via Query<Rel>. Exercises every site the bug
// touched on a plain-string discriminator.
func TestSetCommentable_RoundTrip(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	p := client.Post.Create().SetTitle("Hello").SaveX(ctx)

	c := client.Comment.Create().
		SetBody("Nice post!").
		SetCommentable(p).
		SaveX(ctx)

	if c.CommentableID == nil || *c.CommentableID != p.MorphID() {
		t.Fatalf("CommentableID = %v, want %q", c.CommentableID, p.MorphID())
	}
	if c.CommentableType == nil || *c.CommentableType != string(ent.PostMorphKey) {
		t.Fatalf("CommentableType = %v, want %q", c.CommentableType, ent.PostMorphKey)
	}

	// Typed predicate (CommentableIsType) — uses the same predicate-EQ
	// shortcut the bug misused as a cast.
	rows := client.Comment.Query().Where(ent.CommentCommentableIsType(ent.PostMorphKey)).AllX(ctx)
	if len(rows) != 1 || rows[0].ID != c.ID {
		t.Errorf("CommentCommentableIsType(PostMorphKey) = %v, want exactly the one row", rows)
	}

	// Combined predicate (CommentableIs(parent)).
	rows = client.Comment.Query().Where(ent.CommentCommentableIs(p)).AllX(ctx)
	if len(rows) != 1 || rows[0].ID != c.ID {
		t.Errorf("CommentCommentableIs(post) = %v, want exactly the one row", rows)
	}

	// Resolver returns the typed parent through the sealed interface.
	parent, err := c.QueryCommentable(ctx)
	if err != nil {
		t.Fatalf("QueryCommentable: %v", err)
	}
	got, ok := parent.(*ent.Post)
	if !ok || got.ID != p.ID {
		t.Errorf("QueryCommentable() = %v, want *ent.Post id=%v", parent, p.ID)
	}

	// GQL union accessor — same underlying call, asserts the .GQL()
	// branch of the template emitted a working method.
	gqlParent, err := c.GQLCommentable(ctx)
	if err != nil {
		t.Fatalf("GQLCommentable: %v", err)
	}
	if _, ok := gqlParent.(*ent.Post); !ok {
		t.Errorf("GQLCommentable() = %T, want *ent.Post", gqlParent)
	}

	// Clear the relation.
	client.Comment.UpdateOne(c).ClearCommentable().ExecX(ctx)
	reloaded := client.Comment.GetX(ctx, c.ID)
	if reloaded.CommentableID != nil || reloaded.CommentableType != nil {
		t.Errorf("ClearCommentable left dangling columns: id=%v type=%v",
			reloaded.CommentableID, reloaded.CommentableType)
	}
}

// TestMorphMany_BackRef exercises the parent-side back-reference
// emitted from parentInfo (morphMany branch in the template). Same
// codegen path that previously emitted a broken cast.
func TestMorphMany_BackRef(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	p := client.Post.Create().SetTitle("p").SaveX(ctx)
	for range 3 {
		client.Comment.Create().SetBody("x").SetCommentable(p).SaveX(ctx)
	}

	got := p.QueryComments().AllX(ctx)
	if len(got) != 3 {
		t.Errorf("post.QueryComments() len = %d, want 3", len(got))
	}
}

// TestEagerLoad_With exercises the WithCommentable batched loader,
// which renders the same string(MorphConst) case-arm pattern as the
// resolver — a separate render site of the same conditional.
func TestEagerLoad_With(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	p := client.Post.Create().SetTitle("p").SaveX(ctx)
	v := client.Video.Create().SetTitle("v").SaveX(ctx)
	c1 := client.Comment.Create().SetBody("on-post").SetCommentable(p).SaveX(ctx)
	c2 := client.Comment.Create().SetBody("on-video").SetCommentable(v).SaveX(ctx)

	r, err := client.Comment.Query().WithCommentable().All(ctx)
	if err != nil {
		t.Fatalf("WithCommentable: %v", err)
	}
	if got, ok := r.Commentable[c1.ID].(*ent.Post); !ok || got.ID != p.ID {
		t.Errorf("eager-load parent for c1 = %v, want *ent.Post", r.Commentable[c1.ID])
	}
	if got, ok := r.Commentable[c2.ID].(*ent.Video); !ok || got.ID != v.ID {
		t.Errorf("eager-load parent for c2 = %v, want *ent.Video", r.Commentable[c2.ID])
	}
}

// TestPredicateFunctionStillUsableForFiltering — sanity: the predicate
// function comment.CommentableType IS still a function (now that the
// template stopped misusing it as a type). Calling it must produce a
// usable predicate.
func TestPredicateFunctionStillUsableForFiltering(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	p := client.Post.Create().SetTitle("p").SaveX(ctx)
	client.Comment.Create().SetBody("x").SetCommentable(p).SaveX(ctx)

	rows := client.Comment.Query().
		Where(comment.CommentableTypeEQ(string(ent.PostMorphKey))).
		AllX(ctx)
	if len(rows) != 1 {
		t.Errorf("CommentableTypeEQ(post) returned %d rows, want 1", len(rows))
	}
}
