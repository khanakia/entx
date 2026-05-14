# enttui

**Generate a terminal UI for any ent project — from your schema.**

`enttui` reads your existing [ent](https://entgo.io) schema, applies conventions (and, optionally, schema annotations) and emits a thin per-entity glue layer that wires every type into a generic [tview](https://github.com/rivo/tview) browser. You get a `k9s`-style data explorer for free, no UI code to write.

```
┌─ Tasks ──────────────────────────────────────┐┌─ preview ─────────────────────────────────────────┐
│ ▸ Migrate 5 lists to ServerDataTable         ││ id: tsk_019e25a435f47…                            │
│   Apply useDelayedFlag to /areas             ││ title: Migrate 5 lists to ServerDataTable         │
│   Implement TaskAssignees GraphQL resolver   ││ status: doing  priority: p1                       │
│   Add row-selection model to ServerDataTable ││ created: 2026-05-14 14:09:39                      │
│   …                                          ││ ────────────────────────────────────────          │
│                                              ││ Wire the new gateway, migrate existing rows...    │
└──────────────────────────────────────────────┘└───────────────────────────────────────────────────┘
 Tasks  52/52  sort:created_at↓   k kind · tab pane · / filter · ctrl+u clear · s sort · enter open · esc back · q quit
```

---

## Table of contents

- [What you get](#what-you-get)
- [Requirements](#requirements)
- [Step 1 — generate glue for your schema](#step-1--generate-glue-for-your-schema)
- [Step 2 — embed in your CLI](#step-2--embed-in-your-cli)
- [Step 3 — run the TUI](#step-3--run-the-tui)
- [Keybindings](#keybindings)
- [Customizing per-schema behavior](#customizing-per-schema-behavior)
- [Skipping entities](#skipping-entities)
- [Removing enttui from your project](#removing-enttui-from-your-project)
- [Where to go next](#where-to-go-next)

---

## What you get

- One paginated **list pane** + **preview pane** per entity.
- A **kind picker** modal (`k`) — fuzzy-search any entity by name.
- **Edge navigation** — drill into 1→N edges (`enter`), jump up N→1 edges (`l` / `p` / …), with a back-stack (`esc`).
- **Sort** (`s`), **substring filter** (`/`), pagination, status chips with semantic colors.
- All accessors are **typed Go closures**, generated from your schema — no runtime reflection, no `fmt.Sprintf("%v", any)` blow-ups.
- **Zero hand-written adapter code** per entity. Add a schema → regenerate → it shows up.

---

## Requirements

- Go 1.22 or newer.
- An ent schema directory that compiles (the same one you pass to `entc.Generate`).
- A generated ent client package — e.g. `mymodule/ent` or `mymodule/gen/ent`.
- A modern terminal (any tcell-supported terminal: iTerm2, Terminal.app, kitty, alacritty, etc.).

`enttui` does **not** require you to change your existing `entc` codegen — it runs as a separate pass.

---

## Step 1 — generate glue for your schema

`enttui.Generate` is shaped exactly like `entc.Generate`. Drop it into the codegen `main.go` that already runs your ent generation:

```go
// dbent/cmd/codegen/main.go
package main

import (
    "log"

    "entgo.io/ent/entc"
    entcgen "entgo.io/ent/entc/gen"

    "enttui"
)

//go:generate go run -mod=mod .

func main() {
    // Existing ent generation — unchanged.
    if err := entc.Generate("./schema", &entcgen.Config{
        Target:  "./gen/ent",
        Package: "myproject/ent",
    }); err != nil {
        log.Fatal(err)
    }

    // NEW — enttui pass. Same shape as entc.Generate.
    //
    // ScopeFields: optional list of snake_case field names that the
    // generator wires as scope predicates. For every scope key an entity
    // actually has on its schema, the generated Fetch closure reads
    // opts.Scope[key] and applies a predicate. Drop it for a fully
    // un-scoped (single-tenant) install.
    if err := enttui.Generate("./schema", &enttui.Config{
        Target:      "../tui/gen",
        Package:     "tuigen",
        EntPkg:      "myproject/ent",
        ScopeFields: []string{"project_id"}, // or "tenant_id", "org_id", etc.
    }, enttui.Skip("AuditLog", "QueryLog")); err != nil {
        log.Fatal(err)
    }
}
```

One `go generate ./...` then runs both passes.

Output:

```
tui/gen/
├── register_all.go        # top-level RegisterAll(app, client, projectID)
├── register_task.go       # one file per browsable entity
├── register_memory.go
└── …                      # ≈ one per non-internal ent.Type
```

Every file starts with `// Code generated by enttui. DO NOT EDIT.` and is deterministic — commit alongside your generated ent code.

### Alternative: CLI

If you prefer invoking from a Makefile / Bazel rule / shell script, the bundled CLI is a thin wrapper around `enttui.Generate(...)`:

```bash
go run enttui/cmd/enttui \
    --schema  ./dbent/schema \
    --out     ./tui/gen \
    --package tuigen \
    --ent-pkg myproject/ent
```

Either path produces identical output.

---

## Step 2 — register the generated code with your ent client

After Step 1 you have a Go package at `myproject/tui/gen/` containing one `RegisterAll(app, client)` function. To wire it into your app:

### 2a. Open your ent client like you normally do

Nothing enttui-specific yet — this is whatever boilerplate you already use to connect to your database. Example for SQLite:

```go
import (
    "database/sql"

    _ "github.com/mattn/go-sqlite3"

    "entgo.io/ent/dialect"
    entsql "entgo.io/ent/dialect/sql"

    "myproject/ent"  // your generated ent client (from `entc.Generate`)
)

rawDB, err := sql.Open("sqlite3", "myapp.db?_fk=1")
if err != nil { log.Fatal(err) }
defer rawDB.Close()

drv := entsql.OpenDB(dialect.SQLite, rawDB)
client := ent.NewClient(ent.Driver(drv))
defer client.Close()
```

Postgres, MySQL, or a project-specific helper like `dbent.New(rawDB).Client()` work the same way — enttui only cares that you end up with an `*ent.Client`.

### 2b. Construct the enttui App and pass it the client

```go
import (
    "enttui/runtime"
    tuigen "myproject/tui/gen"  // <-- generated package from Step 1
)

// Build the tview application + page stack
app := runtime.New()

// (optional) generic scope filter — generated Fetch closures look up
// whichever keys they understand: a schema with project_id reads
// "project_id", a schema with tenant_id reads "tenant_id", etc.
app.SetScope("project_id", "prj_xxx")

// Hand the *ent.Client to the generated registry. This auto-registers
// every entity (one runtime.Register call per ent type) using typed
// closures over your client.
tuigen.RegisterAll(app, client)

// Blocks until the user presses q / ctrl+c.
if err := app.Run(); err != nil {
    log.Fatal(err)
}
```

That's the entire integration: **open client → `runtime.New` → `SetScope` (optional) → `RegisterAll(app, client)` → `Run`**.

`enttui` itself knows nothing about "project_id" — it forwards whatever scope keys you set to every Fetch closure and lets the generated code decide what to do with them.

### 2c. Wrap it as a cobra command (or use it however)

```go
func newTUICmd(client *ent.Client) *cobra.Command {
    var projectID string
    cmd := &cobra.Command{
        Use:   "tui",
        Short: "Launch the entity browser TUI",
        RunE: func(*cobra.Command, []string) error {
            app := runtime.New()
            if projectID != "" {
                app.SetScope("project_id", projectID)
            }
            tuigen.RegisterAll(app, client)
            return app.Run()
        },
    }
    cmd.Flags().StringVar(&projectID, "project", "", "scope queries to a project")
    return cmd
}
```

See the [Complete working example](#complete-working-example) below for a single-file, copy-paste runnable variant.

---

## Step 3 — run the TUI

```bash
./mycli tui
```

You'll see the first registered kind in a two-pane layout. Press `k` to open the kind picker and switch between entities.

> **Important:** running is decoupled from code generation. Once you've generated and committed `tui/gen/`, the app launches purely by compiling against those files. Re-run codegen only when the schema changes.

---

## Complete working example

Here is a full, copy-paste-runnable `main.go` for a tiny project. It opens SQLite, builds the ent client, sets a project scope, and launches the TUI:

```go
// cmd/mycli/main.go
package main

import (
    "database/sql"
    "flag"
    "fmt"
    "log"
    "os"

    _ "github.com/mattn/go-sqlite3"        // sqlite driver registration

    "myproject/ent"                         // your generated ent client
    tuigen "myproject/tui/gen"              // enttui-generated glue (from Step 1)

    "enttui/runtime"
)

func main() {
    dbPath := flag.String("db", "", "path to sqlite file (required)")
    projectID := flag.String("project", "", "project id to scope queries (optional)")
    flag.Parse()

    if *dbPath == "" {
        fmt.Fprintln(os.Stderr, "usage: mycli --db <path> [--project <id>]")
        os.Exit(2)
    }

    // 1) open the database + build the ent client
    rawDB, err := sql.Open("sqlite3", *dbPath+"?_fk=1")
    if err != nil {
        log.Fatalf("open db: %v", err)
    }
    defer rawDB.Close()

    drv := entsql.OpenDB(dialect.SQLite, rawDB)
    client := ent.NewClient(ent.Driver(drv))
    defer client.Close()

    // 2) build the enttui app + register everything
    app := runtime.New()

    // 3) set whichever scope keys your codegen recognizes
    if *projectID != "" {
        app.SetScope("project_id", *projectID)
    }

    // 4) wire the generated registry — auto-registers every entity
    tuigen.RegisterAll(app, client)

    // 5) blocks until the user quits (q / ctrl+c)
    if err := app.Run(); err != nil {
        log.Fatal(err)
    }
}
```

You'll also need the ent dialect imports if your client is built directly:

```go
import (
    "entgo.io/ent/dialect"
    entsql "entgo.io/ent/dialect/sql"
)
```

If you already have a helper that returns an `*ent.Client` (e.g. `dbent.New(rawDB).Client()`), drop the `entsql.OpenDB` / `ent.NewClient` block and use your helper instead — `runtime.Register` and `tuigen.RegisterAll` only need a `*ent.Client`.

### What goes in each layer

| Step | Code lives in | Purpose |
|------|---------------|---------|
| 1 — open DB + build client | Your project (`cmd/mycli/main.go` or a helper) | enttui never opens DBs; you control connection lifecycle |
| 2 — `runtime.New()` | `enttui/runtime/app.go` | Constructs the tview Application + page stack |
| 3 — `app.SetScope(k, v)` | `enttui/runtime/app.go` | Generic scope bag; forwarded to every Fetch closure |
| 4 — `tuigen.RegisterAll(...)` | **Generated** `tui/gen/register_all.go` | Calls one `registerX(app, client)` per entity |
| inside each `registerX` | **Generated** `tui/gen/register_<name>.go` | Builds the typed `EntitySpec[*ent.X]` + closures; calls `runtime.Register` |
| `runtime.Register[T]` | `enttui/runtime/registry.go` | Type-erases the typed spec into `*anySpec` stored on `App` |
| 5 — `app.Run()` | `enttui/runtime/app.go` | tview event loop; mounts the first kind, handles all keys until quit |

### Where the generated code came from

If you're wondering how `myproject/tui/gen/register_task.go` was produced, the chain is:

```
go generate ./...                          ← invokes the codegen
  └─ runs:  enttui.Generate(...)           in your codegen main.go
            (or: go run enttui/cmd/enttui …)
                  ↓
       implementation: enttui/codegen/generate.go
       templates:      enttui/codegen/templates/
                       ├── entity.tmpl       ← shape of register_<name>.go
                       └── register_all.tmpl ← shape of register_all.go
                  ↓
       writes to:      ./tui/gen/*.go
```

Each `register_<name>.go` is the output of feeding one `*gen.Type` (parsed from your `./schema/`) through `entity.tmpl`. Open one and read top to bottom — the entire UI-to-DB binding for that entity is in that single file.

---

## Keybindings

| Key            | Action                                                  |
|----------------|---------------------------------------------------------|
| **`k`**        | Open kind picker (fuzzy-searchable modal)               |
| **`ctrl+b`**   | Toggle the left-rail kind sidebar (live preview)        |
| **`\`** (backslash) | Send focus to the sidebar (opens it if hidden); from the sidebar, send focus back to the body |
| **`tab`**      | Switch focus between list pane ↔ preview pane           |
| **`↑ / ↓`**    | Move selection in the focused pane                      |
| **`enter`**    | Open the highlighted edge (or focus preview if none)    |
| **`l p c …`**  | Single-letter triggers for each edge (see preview footer) |
| **`/`**        | Open filter input (substring match on title + body)     |
| **`ctrl+u`**   | Clear the active filter                                 |
| **`s`**        | Cycle sort direction (asc / desc on created_at)         |
| **`esc`**      | Pop the current page (back-stack)                       |
| **`?`**        | Show keybindings help                                   |
| **`q`**        | Quit                                                    |
| **`ctrl+f / pgdn / space`** | Scroll preview down half page              |
| **`ctrl+b / pgup`** | Scroll preview up half page                        |

When an input field has focus (filter, picker, sidebar), single-letter shortcuts go to the input — `k` types a `k`, not "open picker." `esc` always closes the input. `ctrl+b` is the one exception: it's caught BEFORE the typing-guard so it can toggle the sidebar even while you're mid-filter.

### Sidebar (left rail)

Hidden by default; `ctrl+b` shows / hides it. Lists every registered kind, filtered by typing into the input at the top:

- **Type** any text → filter by display name or kind id.
- **`↑ / ↓ / pgup / pgdn`** in the input → move the list cursor while the filter stays focused.
- **`tab`** → cycle focus input ↔ list.
- **Selection change** swaps the body to that kind immediately (live preview). Filtering down to a single result auto-opens it.
- **`\`** → send focus to the body without closing the sidebar.
- **`enter` / `esc` / `ctrl+b`** → close the sidebar.

The sidebar's highlight always reflects the kind shown in the body — drilling an edge, jumping via the modal `k` picker, or `esc`-popping the back-stack all keep the sidebar in sync.

---

## Customizing per-schema behavior

By default `enttui` runs in **convention mode** — see [docs/CONVENTIONS.md](docs/CONVENTIONS.md) for the full list of rules. The short version:

- Every non-internal ent type with an ID is browsable. Pass `Config.ScopeFields` to wire generic scope predicates (e.g. `project_id`, `tenant_id`); entities that don't have that field stay browsable, just unscoped.
- `title` / `name` / `display_name` → row title.
- `body` / `description` / `content` → preview body.
- `status` / `severity` / `kind` (when enum) → status chip.
- `created_at`, `updated_at` → time columns.
- Unique edges → upward links; non-unique → drillable.

You can override any of these (and add per-field hints like color chips, hidden fields, custom triggers) by attaching annotations to your schema. See [docs/ANNOTATIONS.md](docs/ANNOTATIONS.md):

```go
import "enttui"

func (Task) Annotations() []schema.Annotation {
    return []schema.Annotation{
        enttui.Browse(),
        enttui.Display("Tasks"),
        enttui.Group("workflow"),
        enttui.Icon("✓"),
    }
}

func (Task) Fields() []ent.Field {
    return []ent.Field{
        field.Enum("status").Values("todo", "doing", "done").
            Annotations(
                enttui.AsStatus(),
                enttui.Chip(map[string]string{
                    "todo":  "muted",
                    "doing": "warn",
                    "done":  "success",
                }),
            ),
        field.String("internal_hash").
            Annotations(enttui.Hidden()),
    }
}
```

> The annotation API is fully declared in `enttui/annotation.go`; wiring annotations into the codegen pipeline is **planned for M1** — today the generator runs purely on conventions.

---

## Skipping entities

Exclude entities you don't want browsable:

```go
enttui.Generate("./schema", &enttui.Config{...}, enttui.Skip("AuditLog", "QueryLog", "SchemaMigration"))
```

Or with the CLI:

```bash
go run enttui/cmd/enttui --schema ./schema --out ./tui/gen --skip AuditLog,QueryLog,SchemaMigration
```

Built-in internal types (`*_fts`, `audit_log`, `query_log`, `pii_pattern`, etc.) are auto-skipped — see [docs/CONVENTIONS.md](docs/CONVENTIONS.md).

---

## Removing enttui from your project

`enttui` is intentionally easy to rip out:

1. `rm -rf ./tui/gen`
2. Remove the `tuigen.RegisterAll(...)` call and the `tui` cobra command.
3. Remove the `enttui.Generate(...)` step from your codegen `main.go`.
4. `go mod tidy` to drop the dependency.

No third-party server, no DB schema migrations, no runtime hooks to peel back.

---

## Where to go next

- **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** — how the two-layer split (runtime vs codegen) works.
- **[docs/CODEGEN.md](docs/CODEGEN.md)** — pipeline + generated output shape.
- **[docs/RUNTIME.md](docs/RUNTIME.md)** — runtime package internals (browser, picker, focus model).
- **[docs/ANNOTATIONS.md](docs/ANNOTATIONS.md)** — full annotation reference.
- **[docs/CONVENTIONS.md](docs/CONVENTIONS.md)** — fallback rules when no annotations are present.
- **[docs/EDGE-NAVIGATION.md](docs/EDGE-NAVIGATION.md)** — how drill / upward edges work.
- **[docs/DEVELOPING.md](docs/DEVELOPING.md)** — contributing to enttui itself.
- **[docs/INTERNAL.md](docs/INTERNAL.md)** — Taskfile + demo DB locations (this repo's POC only — not relevant to library users).
