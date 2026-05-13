// Scenario 13 from SCENARIOS.md: Event uses entity_pk / entity_kind
// as the discriminator columns instead of the default entity_id /
// entity_type. End-to-end round trip via the typed setter + reverse
// resolver confirms the override is honoured.
package testentpoly

import (
	"context"
	"testing"

	"github.com/khanakia/entx/testentpoly/ent"
)

// TestCustomColumns_RoundTrip — scenario 13.
func TestCustomColumns_RoundTrip(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	post := client.Post.Create().SetTitle("P").SaveX(ctx)
	e := client.Event.Create().SetName("login").SetEntity(post).SaveX(ctx)

	// Discriminator columns surface on the struct as EntityPk / EntityKind.
	if e.EntityPk == nil || *e.EntityPk != post.MorphID() {
		t.Errorf("entity_pk = %v, want %q", e.EntityPk, post.MorphID())
	}
	if e.EntityKind == nil || string(*e.EntityKind) != string(ent.PostMorphKey) {
		t.Errorf("entity_kind = %v, want %q", e.EntityKind, ent.PostMorphKey)
	}

	// Reverse resolve uses the same custom columns under the hood.
	parent, err := e.QueryEntity(ctx)
	if err != nil {
		t.Fatalf("QueryEntity: %v", err)
	}
	p, ok := parent.(*ent.Post)
	if !ok {
		t.Fatalf("expected *Post, got %T", parent)
	}
	if p.ID != post.ID {
		t.Errorf("resolved Post id = %d, want %d", p.ID, post.ID)
	}
}
