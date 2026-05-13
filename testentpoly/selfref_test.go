// Scenario 11 from SCENARIOS.md: a Folder can polymorphically reference
// another Folder (or a Document) as its parent. Self-reference works
// because Folder.Type is listed in its own AllowedTypes.
package testentpoly

import (
	"context"
	"testing"

	"github.com/khanakia/entx/testentpoly/ent"
)

// TestSelfReferential_Folder — scenario 11.
func TestSelfReferential_Folder(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	root := client.Folder.Create().SetName("root").SaveX(ctx)
	child := client.Folder.Create().SetName("child").SetParent(root).SaveX(ctx)

	parent, err := child.QueryParent(ctx)
	if err != nil {
		t.Fatalf("QueryParent: %v", err)
	}
	got, ok := parent.(*ent.Folder)
	if !ok {
		t.Fatalf("expected *Folder, got %T", parent)
	}
	if got.ID != root.ID {
		t.Errorf("parent id = %d, want %d", got.ID, root.ID)
	}

	// Also points at a Document (UUID PK) — the other allowed branch.
	doc := client.Document.Create().SetTitle("D").SaveX(ctx)
	mixed := client.Folder.Create().SetName("mixed").SetParent(doc).SaveX(ctx)
	mp, err := mixed.QueryParent(ctx)
	if err != nil {
		t.Fatalf("QueryParent(doc): %v", err)
	}
	if _, ok := mp.(*ent.Document); !ok {
		t.Errorf("expected *Document, got %T", mp)
	}
}
