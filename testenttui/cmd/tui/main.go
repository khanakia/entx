// testenttui — a self-contained enttui demo.
//
// Regenerate after a schema change:
//
//	cd testenttui
//	go run entc.go                                   # ent codegen
//	go run github.com/khanakia/entx/enttui/cmd/enttui \
//	    --schema ./schema --out ./tui/gen \
//	    --package gen --ent-pkg github.com/khanakia/entx/testenttui/ent
//
// Run:
//
//	go run ./testenttui/cmd/tui            # in-memory sqlite, seeded
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"

	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"github.com/khanakia/entx/enttui/runtime"
	"github.com/khanakia/entx/testenttui/ent"
	"github.com/khanakia/entx/testenttui/tui/gen"
)

func main() {
	view := flag.String("view", "table", "initial view mode: table | list")
	kind := flag.String("kind", "post", "kind to open first")
	flag.Parse()

	db, err := sql.Open("sqlite", "file:testenttui?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", db)))
	defer client.Close()

	ctx := context.Background()
	if err := client.Schema.Create(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	seed(ctx, client)

	app := runtime.New()
	app.SetDefaultViewMode(*view)
	app.SetInitialKind(*kind)
	gen.RegisterAll(app, client)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// seed inserts a small, deterministic dataset so the TUI has something
// to browse on first run.
func seed(ctx context.Context, client *ent.Client) {
	if n, _ := client.User.Query().Count(ctx); n > 0 {
		return
	}

	ada := client.User.Create().SetName("Ada Lovelace").SetEmail("ada@example.com").SetBio("First programmer.").SaveX(ctx)
	alan := client.User.Create().SetName("Alan Turing").SetEmail("alan@example.com").SetBio("Halting problem.").SaveX(ctx)

	go1 := client.Tag.Create().SetName("go").SaveX(ctx)
	tui := client.Tag.Create().SetName("tui").SaveX(ctx)
	ent1 := client.Tag.Create().SetName("ent").SaveX(ctx)

	p1 := client.Post.Create().
		SetTitle("Designing a schema-driven TUI").
		SetBody("How enttui turns ent schemas into a terminal UI.").
		SetStatus("published").
		SetAuthor(ada).
		AddTags(go1, tui, ent1).
		SaveX(ctx)
	p2 := client.Post.Create().
		SetTitle("Annotations cheat-sheet").
		SetBody("Display, Drill, RelatedColumns, DetailEdge…").
		SetStatus("draft").
		SetAuthor(alan).
		AddTags(go1, ent1).
		SaveX(ctx)
	client.Post.Create().
		SetTitle("Archived experiment").
		SetStatus("archived").
		SetAuthor(ada).
		SaveX(ctx)

	client.Comment.Create().SetBody("Great write-up!").SetPost(p1).SetAuthor(alan).SaveX(ctx)
	client.Comment.Create().SetBody("Needs a light theme.").SetPost(p1).SetAuthor(ada).SaveX(ctx)
	client.Comment.Create().SetBody("Bookmarking this.").SetPost(p2).SetAuthor(ada).SaveX(ctx)
}
