// aicoder enttui example
//
// All entity wiring is generated. To regenerate after a schema change:
//
//	go run ./enttui/cmd/enttui \
//	    --schema /abs/path/to/dbent/schema \
//	    --out    ./enttui/examples/aicoder/gen \
//	    --package enttuigen
//
// Run the TUI:
//
//	go run ./enttui/examples/aicoder \
//	    --db /path/to/.aicoder/aicoder.db \
//	    --project <project_id> \
//	    --view table \
//	    --kind task
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"

	"dbent"

	enttuigen "enttui/examples/aicoder/gen"
	"enttui/runtime"
)

func main() {
	dbPath := flag.String("db", "", "path to aicoder.db (required)")
	projectID := flag.String("project", "", "project ID to scope queries (required)")
	// Initial UX options — both default to a fast, table-first browsing
	// experience. Override on the CLI when you want list+preview or a
	// different starting kind.
	view := flag.String("view", "table", "initial view mode: table | list")
	kind := flag.String("kind", "task", "kind to open first (e.g. task, memory, decision); empty = first registered")
	flag.Parse()

	if *dbPath == "" || *projectID == "" {
		fmt.Fprintln(os.Stderr, "usage: aicoder-example --db <path> --project <id> [--view table|list] [--kind task]")
		os.Exit(2)
	}

	rawDB, err := sql.Open("sqlite3", *dbPath+"?_fk=1")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer rawDB.Close()

	entdb := dbent.New(rawDB)
	client := entdb.Client()
	defer client.Close()

	app := runtime.New()

	// Project scoping is generic — set whatever scope keys your generated
	// closures understand. enttui itself knows nothing about "project_id";
	// the generated code for entities that have a project_id field reads
	// opts.Scope["project_id"] in its Fetch closure.
	app.SetScope("project_id", *projectID)

	// Per-spec annotations (enttui.DefaultView, enttui.InitialKind…) would
	// override these defaults. The CLI flags are the runtime equivalent
	// for projects that don't (yet) annotate their schemas.
	app.SetDefaultViewMode(*view)
	app.SetInitialKind(*kind)

	enttuigen.RegisterAll(app, client)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
