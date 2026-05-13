// Scenario 14 from SCENARIOS.md: two polymorphic relations on one child
// schema. Annotation has BOTH `target` (UUID parents) and `secondary`
// (int-PK parents) — the codegen must emit both setter pairs, both
// sealed interfaces, and both reverse resolvers without collision.
package testentpoly

import (
	"context"
	"testing"

	"github.com/khanakia/entx/testentpoly/ent"
)

// TestMultiRelation_OnOneSchema — scenario 14.
func TestMultiRelation_OnOneSchema(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	doc := client.Document.Create().SetTitle("D").SaveX(ctx)
	post := client.Post.Create().SetTitle("P").SaveX(ctx)

	a := client.Annotation.Create().
		SetBody("b").
		SetTarget(doc).
		SetSecondary(post).
		SaveX(ctx)

	target, err := a.QueryTarget(ctx)
	if err != nil {
		t.Fatalf("QueryTarget: %v", err)
	}
	d, ok := target.(*ent.Document)
	if !ok || d.ID != doc.ID {
		t.Errorf("target = %v (%T), want doc %s", target, target, doc.ID)
	}

	secondary, err := a.QuerySecondary(ctx)
	if err != nil {
		t.Fatalf("QuerySecondary: %v", err)
	}
	p, ok := secondary.(*ent.Post)
	if !ok || p.ID != post.ID {
		t.Errorf("secondary = %v (%T), want post %d", secondary, secondary, post.ID)
	}
}
