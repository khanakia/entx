# Runtime internals

Reference for `enttui/runtime/`. Read this if you're modifying focus / layout / rendering code, debugging a UI hang, or porting the runtime to a non-tview backend.

## Package layout

```
enttui/runtime/
├── spec.go        EntitySpec[T], Column[T], EdgeSpec[T], ListOpts, Row
├── registry.go    Register[T](app, spec) + type-erased *anySpec
├── app.go         App, page stack, global key handler, picker / help modals
├── browser.go     One Browser page: list + preview + status bar
├── picker.go      Kind picker modal (fuzzy, k)
├── sidebar.go     Persistent left-rail kind switcher (ctrl+b)
├── preview.go     text/template runners for preview + status
├── theme.go       tview.Styles overrides + tone → tcell color mapping
└── templates/
    ├── preview.tmpl
    └── status.tmpl
```

## Key types

### `EntitySpec[T]`  (`spec.go`)

The **typed** description of one browsable entity. Generated code emits one per ent schema:

```go
type EntitySpec[T any] struct {
    Kind     string                // url-safe id, e.g. "task"
    Display  string                // pretty label
    Group    string                // picker group
    Icon     string                // single rune
    PageSize int
    Default  DefaultView           // initial sort/dir
    Fetch    func(ctx, opts) (rows []T, total int, err error)
    Title    func(T) string
    Body     func(T) string
    Status   func(T) string
    CreatedAt func(T) time.Time
    UpdatedAt func(T) time.Time
    Columns  []Column[T]           // [{Key, Label, Get, Chip}]
    Edges    []EdgeSpec[T]
}
```

Everything is closures over `T`. The compiler catches `r.WrongField`.

### `Row`  (`registry.go`)

The **untyped** projection the UI actually renders:

```go
type Row struct {
    ID, Title, Body, Status string
    CreatedAt, UpdatedAt    time.Time
    Columns map[string]string  // per-column rendered value
}
```

Produced by `projectRow(spec, t)` at fetch time. The browser/preview never see `T`.

### `*anySpec`  (`registry.go`)

The type-erased shape inside the runtime. Mirrors `EntitySpec[T]` but with string accessors. **The single unsafe seam in enttui.** Generic gymnastics live in `Register[T]` and `projectRow[T]`; everything else operates on `*anySpec`.

## The App + page stack

`runtime.App` is one tview Application + a `Pages` widget + a stack:

```go
type App struct {
    tv        *tview.Application
    pages     *tview.Pages         // tview Page registry
    specs     map[string]*anySpec  // kind → spec
    kindOrder []string
    ctx       context.Context      // shared root ctx for all fetches
    cancel    context.CancelFunc
    stack     []pageEntry          // back-stack for edge nav
}
```

Pages are pushed in three flows:

| Flow | Function | Notes |
|------|----------|-------|
| Initial mount | `pushBrowser(firstKind, "")` | First kind in registration order |
| Picker selection | `pushBrowser(kind, "")` | Kind chosen via `k` modal |
| Edge upward | `pushBrowser(targetKind, targetID)` | Browser opens, scrolls to row |
| Edge drill | `pushBrowserList(EntityRefList)` | Browser scoped via `setIDFilter` |

`popPage()` removes the top page and switches back to the previous. `esc` always triggers it (unless inside an input — then `esc` clears the input).

## Browser page  (`browser.go`)

Three widgets in a vertical flex:

```
┌──────────────────┬──────────────────┐
│                  │                  │
│   list           │   preview        │   ← horizontal flex
│   (tview.List)   │   (tview.TextView)│
│                  │                  │
└──────────────────┴──────────────────┘
   status bar (tview.TextView, 1 line)
```

### Focus model

`zoneList` (default) ↔ `zonePreview`. Tab / `→` / `h` / `l` cycles. Focused pane gets an **orange border**; inactive panes keep the dodger-blue default. This is set explicitly in `focusList()` / `focusPreview()` — tview doesn't automatically style focused widgets.

### Refresh cycle

```go
b.refresh()       // calls spec.fetch, populates b.rows / b.total, fills the List
b.refreshPreview() // called every time selection changes; builds previewData, renders template
```

Refresh runs **synchronously** today. A 200-row sqlite query is sub-millisecond — async wasn't worth the complexity. If a remote-DB backend appears later, switch `b.spec.fetch` to a goroutine + tea-style message channel.

### List windowing

`tview.List` already paginates internally — we just `AddItem` for every row and let tview scroll. No manual offset/window logic.

### Filter

Pressing `/` opens a tview `InputField` inside a fresh `Pages` overlay (`"filter"`). Done callbacks (`enter` / `esc`) close the overlay; enter writes `b.filter` and triggers a refresh. The filter string is sent to `spec.fetch(ctx, ListOpts{Filter: …})` — the generated code translates it to ent predicates (substring on title/body).

### Edges

Each edge has a single-letter Trigger. Pressing that key on a selected row calls `b.followEdge(e)`:

- **Upward** (`EdgeUpward`): calls `e.resolveUpward(ctx, rowID)` → `EntityRef{Kind, ID}` → `app.pushBrowser(kind, id)`.
- **Drill** (`EdgeDrill`): calls `e.resolveDrill(ctx, rowID)` → `EntityRefList{Kind, IDs}` → `app.pushBrowserList(refs)`.

The resolvers are closures generated by codegen that wrap ent queries (e.g. `client.Task.QueryTasklist(r).Only(ctx)`).

## Picker modal  (`picker.go`)

Renders inside a centered `Pages` overlay. Two stacked widgets: a `tview.InputField` on top, a `tview.List` below.

- Typing in the input filters the list (substring match on `Display` + `Kind`).
- Arrow keys / `ctrl+n` / `ctrl+p` / `pgdn` / `pgup` while the input has focus drive the list without losing the input — fzf-style.
- Enter selects → closes the picker, pushes a new Browser page.
- Esc cancels.

The picker always allocates a fresh `p.shown` slice — never aliases `p.all` — to avoid the "type query, clear, see duplicates" bug.

## Sidebar  (`sidebar.go`)

A persistent left-rail companion to the modal picker. Hidden by default, toggled with `ctrl+b`. Lives in `App.rootFlex` as a sibling of the pages container — NOT a tview Pages overlay — so it stays visible while the user interacts with the body.

Layout: `[InputField (filter) | List (kinds)]` in a vertical Flex.

Behavior:

- Typing in the input filters by display name / kind id.
- `↑ / ↓ / pgup / pgdn` and `ctrl+n / ctrl+p` inside the input forward to a `move(delta)` shim that nudges the list's selection — user never has to leave the input to navigate.
- `tab` cycles input ↔ list focus.
- The list's `SetChangedFunc` is the **live-preview engine**: every selection change calls `app.replaceTopKind(kind)` which removes the top stack page and pushes a fresh one for the new kind. Stack depth stays the same.
- Re-entrance trap: a naive `syncSidebar` that calls full `populate()` (Clear + AddItem + SetCurrentItem) during a swap caused the cursor to teleport to the last item on the first arrow press — the nested SetCurrentItem corrupted the outer SetCurrentItem's bookkeeping. Fix: `syncSidebar` now only calls `highlightCurrent()` (cursor-only) under `suppressChange`.
- Filter narrowing auto-opens `shown[0]` when the current page's kind drops out of the filtered set — typing "Task" while sitting on "Plan" jumps the body to Task immediately.
- `\` (backslash) toggles focus between sidebar and body without closing the sidebar (`App.focusBody()`).
- `ctrl+b` is caught at the very top of the global handler — BEFORE the typing-guard — so it can toggle even with focus inside the sidebar's own input. Plain `b` is just a filter character.

Kind sync: `pushBrowser`, `pushBrowserList`, and the `swapTo*` toggles all stamp `pageEntry.kind` and call `App.syncSidebar()` so the sidebar's highlight tracks whichever kind is on top of the stack — regardless of how the user got there.

## Templates  (`preview.tmpl` + `status.tmpl`)

Both live in `enttui/runtime/templates/` and are embedded via `//go:embed templates/*.tmpl`. Parsed once at package init with `template.ParseFS`.

- **`preview.tmpl`** renders the right pane: aqua bold labels, terminal-default value text, a separator, the body, and an edges footer.
- **`status.tmpl`** renders the bottom status bar: pane name pill, count, sort indicator, optional filter chip, help hint.

Editing the templates → reflow / retheme without touching Go code.

## Theme  (`theme.go`)

Installed via `applyTheme()` called from `runtime.New()`. Overrides `tview.Styles` to set:

- **Backgrounds** → `ColorDefault` (terminal-native) so the app blends into light + dark themes.
- **Borders** → DodgerBlue (matches k9s).
- **Titles** → Yellow.
- **Selected row** → DodgerBlue bg + Black fg.

Tone → tcell color name mapping lives in `toneColor()` — `success` → green, `warn` → orange, `danger` → red, `info` → dodgerblue, `muted` → gray.

## Global key handler

Set on the tview Application in `App.Run`:

| Key | Action | When suppressed |
|-----|--------|-----------------|
| `ctrl+b` | Toggle sidebar | Never (caught BEFORE the typing-guard) |
| `k` | Open picker | While focus is in a tview.InputField |
| `q` | Quit | While focus is in a tview.InputField |
| `?` | Help modal | While focus is in a tview.InputField |
| `\` | Focus sidebar (opens if hidden) | While focus is in a tview.InputField |
| `esc` | Pop page | Always fires (even in inputs — there it closes the input first) |

The "is focus an input field" check uses `a.tv.GetFocus().(*tview.InputField)`. If you add new modals with text input, the check still works.

## Context cancellation

`App.New()` creates a root `context.WithCancel`. `App.Run` defers `a.cancel()`. Every browser refresh + edge resolve derives a 5-second timeout from `a.ctx`. If the app shuts down with queries in flight, they're cancelled cleanly.

## Adding a new pane / page type

1. Build a new widget tree (typically a `tview.Flex` with whatever children).
2. Wrap as a struct that owns its tview state, like `browser` / `picker`.
3. Push via `app.pages.AddPage("uniqueName", widget, true, true)` and `app.stack = append(...)` if it should participate in the back-stack.
4. Pop with `app.popPage()`.
5. Wire keys via `widget.SetInputCapture(...)`. Don't rely on the global handler for widget-specific keys — it's a fallback only.

## Testing the runtime without a real schema

Construct an `EntitySpec[FakeT]` with a hardcoded `Fetch`. Register it. Drive `app.Run` in a goroutine while sending synthesized `tcell.EventKey` events via `tview.Application.QueueEvent`. The widget tree behaves identically — no DB or codegen needed.

(Tests are not yet checked in; that's part of M2.)
