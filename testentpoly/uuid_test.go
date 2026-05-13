// Scenario 10 from SCENARIOS.md: UUID round-trip. Annotation has a
// UUID PK and points at Document / Report (both UUID PKs). The
// discriminator id column persists the UUID as a string, and
// QueryTarget reverses it back to the concrete typed parent.
package testentpoly

import (
	"context"
	"testing"

	"github.com/khanakia/entx/testentpoly/ent"
)

// TestUUID_FullRoundTrip — scenario 10.
func TestUUID_FullRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := openTestClient(t)

	doc := client.Document.Create().SetTitle("D").SaveX(ctx)
	rep := client.Report.Create().SetName("R").SaveX(ctx)

	aDoc := client.Annotation.Create().SetBody("on doc").SetTarget(doc).SaveX(ctx)
	aRep := client.Annotation.Create().SetBody("on rep").SetTarget(rep).SaveX(ctx)

	// String-encoded UUID round-trip via the discriminator column.
	if aDoc.TargetID == nil || *aDoc.TargetID != doc.MorphID() {
		t.Errorf("doc target id = %v, want %q", aDoc.TargetID, doc.MorphID())
	}

	// Reverse resolve via QueryTarget — type-switch must recover *Document.
	parent, err := aDoc.QueryTarget(ctx)
	if err != nil {
		t.Fatalf("QueryTarget(doc): %v", err)
	}
	switch p := parent.(type) {
	case *ent.Document:
		if p.ID != doc.ID {
			t.Errorf("resolved Document id = %s, want %s", p.ID, doc.ID)
		}
	case *ent.Report:
		t.Errorf("expected Document, got Report id=%s", p.ID)
	case nil:
		t.Fatal("doc annotation parent is nil")
	}

	parent, err = aRep.QueryTarget(ctx)
	if err != nil {
		t.Fatalf("QueryTarget(rep): %v", err)
	}
	r, ok := parent.(*ent.Report)
	if !ok {
		t.Fatalf("expected *Report, got %T", parent)
	}
	if r.ID != rep.ID {
		t.Errorf("resolved Report id = %s, want %s", r.ID, rep.ID)
	}
}
