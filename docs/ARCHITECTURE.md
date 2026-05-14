# Architecture

> AI / new-contributor context. If you're a *user* of enttui, the [README](../README.md) is what you want.

`enttui` is split into two cleanly-separated layers. Knowing why the split exists is the most important thing in this document.

## The two layers

```
┌────────────────────────────────────────────────────────────────────────────┐
│                       USER'S PROJECT (consumer)                            │
│                                                                            │
│   import _ "enttui/runtime"                                                │
│   import "myproject/tui/gen"   ← generated, schema-specific                │
│                                                                            │
│   func main() {                                                            │
│       app := runtime.New()                                                 │
│       gen.RegisterAll(app, client, projectID)                              │
│       app.Run()                                                            │
│   }                                                                        │
└──────┬─────────────────────────────────────────────────────────────┬───────┘
       │                                                             │
       ▼                                                             ▼
┌──────────────────────────────┐   ┌────────────────────────────────────────┐
│   enttui/runtime/            │   │     myproject/tui/gen/                 │
│   (handwritten, generic)     │   │     (codegen output)                   │
│                              │   │                                        │
│ • App + tview wiring         │◀──│ • One register_<name>.go per entity    │
│ • Browser page (list+preview)│   │ • Typed Fetch closures                 │
│ • Picker modal               │   │ • Typed Title/Body/Status accessors    │
│ • Edge back-stack            │   │ • Edge Resolve closures                │
│ • Service interface          │   │ • register_all.go aggregator           │
│ • text/template renderers    │   │                                        │
└──────────────────────────────┘   └────────────────────────────────────────┘
                                                ▲
                                                │
                                   ┌────────────────────────────┐
                                   │  enttui/cmd/enttui (CLI)   │
                                   │  + enttui/codegen/ (package)   │
                                   │                            │
                                   │  Reads:                    │
                                   │   • your ent schema        │
                                   │     via entc.LoadGraph     │
                                   │                            │
                                   │  Writes:                   │
                                   │   • register_*.go files    │
                                   │     into --out directory   │
                                   └────────────────────────────┘
```

### Layer 1 — `enttui/runtime/`  (handwritten, schema-agnostic)

Plain Go. Imports `tview` + `tcell`. **Never imports any specific ent type.** Operates on:

- `Service` interface (`List`, `Get`) — not yet used externally but reserved for non-tview backends.
- `EntitySpec[T]` — typed description of one browsable entity. Per-type closures for fetch, accessors, edges.
- `Row` — runtime-visible projection (strings + a typed Columns map) the UI actually renders.
- Two `text/template` files (`templates/preview.tmpl`, `templates/status.tmpl`) embedded via `//go:embed`.

This layer is **testable in isolation** against an in-memory `EntitySpec` fixture. No DB, no ent, no codegen needed to exercise the focus model, edge stack, picker filter, etc.

### Layer 2 — `enttui/codegen/` + `enttui/cmd/enttui/`  (codegen)

`enttui/codegen/generate.go` is the **engine**: opens an ent schema via `entc.LoadGraph`, walks `graph.Nodes`, extracts each `*gen.Type` into an `EntityMeta` struct, then renders two templates (entity.tmpl + register_all.tmpl) into the user's output directory.

`enttui/cmd/enttui/main.go` is a **thin CLI** over `codegen.Generate(Options{...})`. Flags only.

`enttui/codegen/templates/` holds the actual code-generation templates. They emit a minimal slice of glue:
- Imports for `context`, `time`, `fmt` (gated by `NeedsFmt`), the ent client package, the per-entity predicate package, and `enttui/runtime`.
- One `registerName(app, client, projectID)` function.
- Inside, one `runtime.Register(app, runtime.EntitySpec[*ent.Name]{...})` call.

## Why the split

We deliberately resisted putting everything into the codegen layer (the alternative: emit a full `main.go` per project). The two-layer split:

1. **Generated code stays small and dumb.** A user reading `register_task.go` sees ~50 lines of obvious wiring, not a UI framework.
2. **Framework upgrades don't break consumer schemas.** Bug fixes to `runtime/browser.go` flow into every consumer without regeneration. Only **shape changes** to `EntitySpec[T]` force a regen.
3. **The UI framework is testable.** `enttui/runtime/*_test.go` (when added) exercises the widget tree without touching ent or codegen.
4. **A future web target works the same way.** Write `enttui/runtime/web/` that consumes the same `EntitySpec` registrations. The codegen output doesn't change. (Not built yet but the data layer was designed for it.)
5. **Easy removal.** `rm -rf gen/` drops the project's dependency on enttui. Runtime can also be vendored, deleted in seconds.

## Type erasure boundary

Generics are used **inside** `EntitySpec[T]` so accessors are typed:

```go
runtime.EntitySpec[*ent.Task]{
    Title: func(r *ent.Task) string { return r.Title },
}
```

But the runtime stores a heterogeneous registry of all entity kinds. The seam is at registration:

```go
// enttui/runtime/registry.go
func Register[T any](app *App, spec EntitySpec[T]) {
    // The typed Spec[T] is collapsed into a type-erased *anySpec.
    // Closures capture T; everything else flows as string / interface{}.
    ...
}
```

Inside the runtime, only `*anySpec` exists. Browser, picker, edge nav are non-generic. **All unsafe boundaries live in `registry.go`** — they're easy to audit + golden-test.

## Data flow on one user keystroke

User presses `↓` in the list pane:

1. tview's `*tview.List` calls our `SetChangedFunc(...)` callback → `b.refreshPreview()`.
2. `b.refreshPreview` reads `b.rows[idx]` (already-projected `Row` values from the last `Fetch`).
3. Builds a `previewData{ Fields, Body, Edges }` from the Row + the entity's `anySpec.columns / .edges`.
4. Executes `templates/preview.tmpl` with that data → emits a string with tview color tags.
5. `b.prev.SetText(...)` — tview repaints.

User presses `c` (a configured edge trigger):

1. `b.listKeyCapture` sees the rune, finds the matching `anyEdge`.
2. `b.followEdge` calls `edge.resolveDrill(ctx, currentRowID)` — that's a closure generated by codegen that runs the actual ent query.
3. The returned `EntityRefList{Kind, IDs}` is handed back to `App.pushBrowserList`.
4. `pushBrowserList` constructs a new browser scoped to those IDs, adds it as a new tview `Page`, pushes onto the back-stack.
5. User presses `esc` → top page popped, browser is restored with selection intact.

## What's intentionally not here

- **No database connection logic.** The runtime never opens a DB. The consumer constructs the ent.Client, generated closures call its Query methods.
- **No authentication / authorization.** The TUI is single-process and trusts the caller. Apply project-scoping or row-level checks inside the consumer's own `Fetch` overrides if needed.
- **No mutation.** v1 is browse-only. Adding edit/forms is M2+ in the roadmap.
- **No theming hooks.** Colors are hardcoded in templates + the tview Styles override in `runtime/theme.go`. M2 will expose a theming API.

## Module shape

See [DEVELOPING.md](DEVELOPING.md) for the on-disk layout.
