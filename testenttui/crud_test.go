package testenttui

import (
	"context"
	"database/sql"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/khanakia/entx/enttui/runtime"
	"github.com/khanakia/entx/testenttui/ent"
	"github.com/khanakia/entx/testenttui/tui/gen"
)

// TestRegisterAndCRUDAcrossIDTypes is an end-to-end check that the
// generated glue compiles and registers against a schema whose entities
// use THREE different ID Go types at once:
//
//	User    → string      (uuid-string mixin)
//	Post    → int         (ent default PK)
//	Comment → int          (a second int entity)
//	Tag     → uuid.UUID   (native uuid PK)
//
// The `var _ T = x.ID` lines below are compile-time assertions: this
// test file only builds if the codegen produced specs whose ID types
// match per entity. gen.RegisterAll then smoke-tests registration.
func TestRegisterAndCRUDAcrossIDTypes(t *testing.T) {
	db, err := sql.Open("sqlite", "file:tt_idtypes?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", db)))
	defer client.Close()

	ctx := context.Background()
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	u := client.User.Create().SetName("Ada").SetEmail("ada@x.test").SaveX(ctx)
	p := client.Post.Create().SetTitle("Hello").SetStatus("draft").SetAuthor(u).SaveX(ctx)
	tg := client.Tag.Create().SetName("go").SaveX(ctx)
	c := client.Comment.Create().SetBody("nice").SetPost(p).SetAuthor(u).SaveX(ctx)

	// Compile-time per-entity ID-type proof.
	var _ string = u.ID
	var _ int = p.ID
	var _ int = c.ID
	var _ uuid.UUID = tg.ID

	// Round-trip read-back so the IDs are actually exercised, not just typed.
	if _, err := client.User.Get(ctx, u.ID); err != nil {
		t.Fatalf("User.Get(string id): %v", err)
	}
	if _, err := client.Post.Get(ctx, p.ID); err != nil {
		t.Fatalf("Post.Get(int id): %v", err)
	}
	if _, err := client.Tag.Get(ctx, tg.ID); err != nil {
		t.Fatalf("Tag.Get(uuid id): %v", err)
	}

	// RegisterAll wires every generated spec into the runtime. No screen
	// is started (we never call app.Run), so this is a safe smoke test
	// that the generated register_*.go all compile + register cleanly.
	app := runtime.New()
	gen.RegisterAll(app, client)
}
