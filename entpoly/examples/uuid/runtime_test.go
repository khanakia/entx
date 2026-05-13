// Runtime test for the UUID-PK example. Proves the UUID-specific
// codegen branches do the right thing end-to-end against a real
// (in-memory SQLite) database:
//
//   - The polymorphic id column round-trips uuid.UUID values via
//     uuid.Parse on the read path.
//   - The eager-load result map is keyed by uuid.UUID (Annotation's PK).
//   - The forward resolver QueryTarget returns a typed parent
//     (*Document or *Report), no `any`.
package uuid_test

import (
	"context"
	"database/sql"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/khanakia/entx/entpoly/examples/uuid/ent"
)

func openClient(t *testing.T) *ent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", "file:uuid?mode=memory&cache=shared&_pragma=foreign_keys(1)")
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

func TestUUIDPolymorphicResolver(t *testing.T) {
	ctx := context.Background()
	client := openClient(t)

	doc := client.Document.Create().SetTitle("D").SaveX(ctx)
	rep := client.Report.Create().SetName("R").SaveX(ctx)

	// Attach Annotation to a Document. Set<Morph> writes the UUID id
	// stringified into the morph_id column; the resolver parses it
	// back via uuid.Parse.
	a := client.Annotation.Create().SetBody("on doc").SetTarget(doc).SaveX(ctx)

	parent, err := a.QueryTarget(ctx)
	if err != nil {
		t.Fatalf("QueryTarget: %v", err)
	}
	d, ok := parent.(*ent.Document)
	if !ok {
		t.Fatalf("resolved type = %T, want *ent.Document", parent)
	}
	if d.ID != doc.ID {
		t.Errorf("resolved id = %v, want %v", d.ID, doc.ID)
	}

	// Reassign to a Report.
	a = a.Update().SetTarget(rep).SaveX(ctx)
	parent, err = a.QueryTarget(ctx)
	if err != nil {
		t.Fatalf("QueryTarget after reassign: %v", err)
	}
	r, ok := parent.(*ent.Report)
	if !ok {
		t.Fatalf("after reassign, resolved type = %T, want *ent.Report", parent)
	}
	if r.ID != rep.ID {
		t.Errorf("resolved id = %v, want %v", r.ID, rep.ID)
	}
}

func TestUUIDEagerLoadKeyedByUUID(t *testing.T) {
	ctx := context.Background()
	client := openClient(t)

	doc1 := client.Document.Create().SetTitle("D1").SaveX(ctx)
	doc2 := client.Document.Create().SetTitle("D2").SaveX(ctx)
	rep := client.Report.Create().SetName("R").SaveX(ctx)

	a1 := client.Annotation.Create().SetBody("a1").SetTarget(doc1).SaveX(ctx)
	a2 := client.Annotation.Create().SetBody("a2").SetTarget(doc2).SaveX(ctx)
	a3 := client.Annotation.Create().SetBody("a3").SetTarget(rep).SaveX(ctx)

	r, err := client.Annotation.Query().WithTarget().All(ctx)
	if err != nil {
		t.Fatalf("WithTarget().All: %v", err)
	}
	if len(r.Annotations) != 3 {
		t.Errorf("Annotations len = %d, want 3", len(r.Annotations))
	}
	// Map is keyed by Annotation's UUID PK — verify three entries
	// and the per-row typed parent.
	if len(r.Target) != 3 {
		t.Errorf("Target map len = %d, want 3", len(r.Target))
	}
	for _, pair := range []struct {
		annID  uuid.UUID
		wantID uuid.UUID
		wantT  string
	}{
		{a1.ID, doc1.ID, "Document"},
		{a2.ID, doc2.ID, "Document"},
		{a3.ID, rep.ID, "Report"},
	} {
		p, ok := r.Target[pair.annID]
		if !ok {
			t.Errorf("missing parent for annotation %v", pair.annID)
			continue
		}
		switch x := p.(type) {
		case *ent.Document:
			if pair.wantT != "Document" {
				t.Errorf("annotation %v: got Document, want %s", pair.annID, pair.wantT)
			}
			if x.ID != pair.wantID {
				t.Errorf("annotation %v: doc id = %v, want %v", pair.annID, x.ID, pair.wantID)
			}
		case *ent.Report:
			if pair.wantT != "Report" {
				t.Errorf("annotation %v: got Report, want %s", pair.annID, pair.wantT)
			}
			if x.ID != pair.wantID {
				t.Errorf("annotation %v: report id = %v, want %v", pair.annID, x.ID, pair.wantID)
			}
		}
	}
}
