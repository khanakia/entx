package runtime

import (
	"context"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// App holds the tview Application + the registry of typed entity specs.
// Construct with New, populate via Register[T], then call Run.
type App struct {
	tv        *tview.Application
	pages     *tview.Pages
	specs     map[string]*anySpec
	kindOrder []string // registration order

	// Shared root context for fetches. Cancelled on app stop.
	ctx    context.Context
	cancel context.CancelFunc

	// Page back-stack for edge navigation.
	stack []pageEntry

	// scope is an arbitrary consumer-defined string/string map that the
	// runtime forwards to every Fetch closure via ListOpts.Scope. Generated
	// code looks up keys it knows about (e.g. "project_id"); unknown keys
	// are ignored. Nil = no scope filters applied.
	scope map[string]string

	// initialKind, if set, overrides "first registered kind" as the page
	// to mount when Run starts. Set via app.SetInitialKind("task").
	initialKind string

	// defaultViewMode is the global preferred view for any page that
	// doesn't have a per-spec EntitySpec.Default.Mode set. "table" or
	// "list". Default: "list".
	defaultViewMode string

	// instances maps a tview Pages entry name → the Go-side widget
	// struct (*browser or *tableView). Used to recover state for v-toggle.
	instances map[string]any

	// rootFlex hosts the optional left sidebar + the pages container.
	// When the sidebar is hidden, only pages occupies it.
	rootFlex *tview.Flex
	sidebar  *sidebar
	// sidebarVisible mirrors the current state so toggleSidebar is a
	// single source of truth.
	sidebarVisible bool
	// sidebarWidth in columns. Fixed; could be made configurable later.
	sidebarWidth int
}

// SetInitialKind overrides which entity kind is shown when Run starts.
// Empty (or unknown kind) → falls back to the first kind that registered.
//
//	app.SetInitialKind("task")
func (a *App) SetInitialKind(kind string) { a.initialKind = kind }

// SetDefaultViewMode picks the view used by any new page that doesn't
// override via EntitySpec.Default.Mode. Valid: "list", "table".
//
//	app.SetDefaultViewMode("table")
func (a *App) SetDefaultViewMode(mode string) { a.defaultViewMode = mode }

// SetScope attaches a generic scope filter. Generated Fetch closures look
// up whichever keys they understand and apply them as predicates.
//
//	app.SetScope("project_id", projectID)
//	app.SetScope("tenant_id",  tenantID)
//
// Passing an empty value erases the key. enttui itself never inspects the
// values — what makes any given key actionable is whether the generated
// (or hand-written) closure for an entity reads it.
func (a *App) SetScope(key, value string) {
	if a.scope == nil {
		a.scope = make(map[string]string)
	}
	if value == "" {
		delete(a.scope, key)
		return
	}
	a.scope[key] = value
}

// Scope returns a shallow copy of the current scope map. Used by the
// browser when constructing ListOpts.
func (a *App) Scope() map[string]string {
	if a.scope == nil {
		return nil
	}
	out := make(map[string]string, len(a.scope))
	for k, v := range a.scope {
		out[k] = v
	}
	return out
}

type pageEntry struct {
	name  string // tview page name
	title string // breadcrumb segment
	kind  string // entity kind backing this page (for sidebar sync)
}

// New returns an empty App. Register entities, then Run.
func New() *App {
	applyTheme()
	ctx, cancel := context.WithCancel(context.Background())
	return &App{
		tv:        tview.NewApplication(),
		pages:     tview.NewPages(),
		specs:     make(map[string]*anySpec),
		kindOrder: []string{},
		ctx:       ctx,
		cancel:    cancel,
	}
}


// Run starts the tview event loop. Blocks until the user quits.
func (a *App) Run() error {
	defer a.cancel()

	if len(a.specs) == 0 {
		return errEmpty
	}

	// Pick the initial kind: explicit override wins; otherwise first
	// registered. Falls back to registration order if the override
	// names an unknown kind.
	first := a.kindOrder[0]
	if a.initialKind != "" {
		if _, ok := a.specs[a.initialKind]; ok {
			first = a.initialKind
		}
	}
	a.pushBrowser(first, "")

	// Global key handler — single-letter shortcuts (`k`, `q`, `?`) only
	// fire when focus is NOT inside a text input; otherwise the user
	// typing "task" into the picker would re-trigger the picker.
	//
	// esc handling: only pop the page stack when the front page IS a stack
	// entry. If a modal overlay is in front (preview, filter, picker,
	// sort/cond/columns modals, help), the modal's own InputCapture
	// handles esc — we don't want to also pop the underlying stack page
	// out from under it. See bug fix: opening preview via enter, pressing
	// esc was emptying the table because popPage removed the table page.
	a.tv.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		// Ctrl-prefixed sidebar toggle works EVERYWHERE — even with focus
		// inside a text input — because it can't collide with normal
		// typing (no plain letter can produce KeyCtrlB).
		if ev.Key() == tcell.KeyCtrlB {
			a.toggleSidebar()
			return nil
		}
		// While typing into a text field, never intercept characters —
		// `k` should type a `k`, not open the picker. esc inside an input
		// is owned by the input itself (it knows whether to clear or
		// dismiss its overlay).
		if _, typing := a.tv.GetFocus().(*tview.InputField); typing {
			return ev
		}
		// While ANY modal overlay is in front (preview, filter, picker,
		// sort/condition/columns modals, help), do not intercept letter
		// shortcuts — the modal's own InputCapture owns them. This is
		// why `K` in the sort modal was opening the kind picker: the
		// global handler was eating `k` before the modal saw it.
		if a.frontIsModal() {
			if ev.Key() == tcell.KeyEscape {
				return ev
			}
			return ev
		}
		switch ev.Rune() {
		case 'k':
			a.openPicker()
			return nil
		case '\\':
			// `\` swings focus from the body INTO the sidebar (the
			// sidebar's own handlers swing it back out). Opens the
			// sidebar first if it's hidden — convenient one-key reach.
			if !a.sidebarVisible {
				a.showSidebar()
				return nil
			}
			if a.sidebar != nil {
				a.tv.SetFocus(a.sidebar.input)
			}
			return nil
		case 'q':
			a.tv.Stop()
			return nil
		case '?':
			a.openHelp()
			return nil
		}
		if ev.Key() == tcell.KeyEscape {
			a.popPage()
			return nil
		}
		return ev
	})

	// Build the root flex (sidebar | pages). Sidebar starts hidden — only
	// the pages container is added until the user presses `b`.
	a.sidebarWidth = 26
	a.rootFlex = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.pages, 0, 1, true)

	return a.tv.SetRoot(a.rootFlex, true).EnableMouse(true).Run()
}

// toggleSidebar shows the sidebar if hidden, hides it otherwise.
func (a *App) toggleSidebar() {
	if a.sidebarVisible {
		a.hideSidebar()
	} else {
		a.showSidebar()
	}
}

// showSidebar mounts the left-rail kind picker and focuses its filter.
// Constructed on every show so newly registered kinds appear and the
// initial selection lines up with the current front page.
func (a *App) showSidebar() {
	if a.sidebarVisible || a.rootFlex == nil {
		return
	}
	a.sidebar = newSidebar(a)
	// Re-layout: insert sidebar before pages.
	a.rootFlex.Clear().
		AddItem(a.sidebar.root, a.sidebarWidth, 0, true).
		AddItem(a.pages, 0, 1, false)
	a.sidebarVisible = true
	a.tv.SetFocus(a.sidebar.input)
}

// focusBody moves focus from the sidebar back to the front stack page's
// primary widget (browser.list or tableView.table). No-op if the sidebar
// is hidden or there's nothing on the stack.
func (a *App) focusBody() {
	if len(a.stack) == 0 {
		return
	}
	inst := a.pageInstance(a.stack[len(a.stack)-1].name)
	switch v := inst.(type) {
	case *browser:
		a.tv.SetFocus(v.list)
	case *tableView:
		a.tv.SetFocus(v.table)
	}
}

// hideSidebar removes the sidebar and returns focus to the current page.
func (a *App) hideSidebar() {
	if !a.sidebarVisible || a.rootFlex == nil {
		return
	}
	a.rootFlex.Clear().AddItem(a.pages, 0, 1, true)
	a.sidebar = nil
	a.sidebarVisible = false
	// Hand focus back to the front page widget.
	if len(a.stack) > 0 {
		if inst := a.pageInstance(a.stack[len(a.stack)-1].name); inst != nil {
			switch v := inst.(type) {
			case *browser:
				a.tv.SetFocus(v.list)
			case *tableView:
				a.tv.SetFocus(v.table)
			}
		}
	}
}

// syncSidebar nudges the sidebar's highlight onto the front page's kind.
// Critical: uses highlightCurrent (cursor-only) rather than populate
// (Clear + AddItem + SetCurrentItem). Calling populate from within a
// SetChangedFunc-triggered swap caused the cursor to jump to the last
// item on the first arrow press — the inner SetCurrentItem corrupted
// the outer one's bookkeeping.
func (a *App) syncSidebar() {
	if !a.sidebarVisible || a.sidebar == nil {
		return
	}
	a.sidebar.suppressChange = true
	a.sidebar.highlightCurrent()
	a.sidebar.suppressChange = false
}

// currentKind returns the kind backing the front stack page. Empty if
// stack is empty.
func (a *App) currentKind() string {
	if len(a.stack) == 0 {
		return ""
	}
	return a.stack[len(a.stack)-1].kind
}

// replaceTopKind swaps the top stack page for a fresh browser/table page
// of `kind`. Used by the sidebar's live-preview navigation. Stack depth
// stays the same (no growth, no shrink to drill ancestors).
func (a *App) replaceTopKind(kind string) {
	if _, ok := a.specs[kind]; !ok {
		return
	}
	if len(a.stack) == 0 {
		a.pushBrowser(kind, "")
		return
	}
	top := a.stack[len(a.stack)-1]
	a.pages.RemovePage(top.name)
	a.clearInstance(top.name)
	a.stack = a.stack[:len(a.stack)-1]
	a.pushBrowser(kind, "")
}

// pushBrowser opens a page for the given kind in the appropriate view
// mode. Mode resolution order (first non-empty wins):
//  1. spec.Default.Mode (annotation enttui.DefaultView("table") etc.)
//  2. a.defaultViewMode (app.SetDefaultViewMode("table"))
//  3. "list"  (the original list+preview UX)
//
// Optional focusID restricts a list-mode page to a single row (used by
// edge upward navigation). Ignored in table mode for v1.
func (a *App) pushBrowser(kind, focusID string) {
	spec, ok := a.specs[kind]
	if !ok {
		return
	}

	mode := spec.defaultView.Mode
	if mode == "" {
		mode = a.defaultViewMode
	}
	if mode == "" {
		mode = "list"
	}

	title := spec.display
	if title == "" {
		title = kind
	}

	if mode == "table" {
		t := newTableView(a, spec)
		name := pageName("table", kind, "")
		a.pages.AddPage(name, t.root, true, true)
		a.stack = append(a.stack, pageEntry{name: name, title: title + " (table)", kind: kind})
		a.tv.SetFocus(t.table)
		a.registerInstance(name, t)
		return
	}

	b := newBrowser(a, spec)
	name := pageName("browse", kind, focusID)
	a.pages.AddPage(name, b.root, true, true)
	a.stack = append(a.stack, pageEntry{name: name, title: title, kind: kind})
	a.tv.SetFocus(b.list)
	a.registerInstance(name, b)
	if focusID != "" {
		b.focusID(focusID)
	}
	a.syncSidebar()
}

// swapToTable replaces the current top page with a table view of the
// same spec. Carries over filter / sort / page / selection from the
// previous (browser) view so the toggle feels transparent.
func (a *App) swapToTable(spec *anySpec) {
	if len(a.stack) == 0 {
		return
	}
	top := a.stack[len(a.stack)-1]

	// Snapshot state from the outgoing view before removing it.
	var s viewState
	if b, ok := a.pageInstance(top.name).(*browser); ok && b != nil {
		s = b.state()
	} else if t, ok := a.pageInstance(top.name).(*tableView); ok && t != nil {
		s = t.state()
	}

	a.pages.RemovePage(top.name)
	a.clearInstance(top.name)

	t := newTableView(a, spec)
	name := pageName("table", spec.kind, "")
	a.stack[len(a.stack)-1] = pageEntry{name: name, title: spec.display + " (table)", kind: spec.kind}
	a.pages.AddPage(name, t.root, true, true)
	a.tv.SetFocus(t.table)
	a.registerInstance(name, t)
	t.applyState(s)
}

// swapToBrowser is the inverse — table → list+preview. Same state
// preservation as swapToTable.
func (a *App) swapToBrowser(spec *anySpec) {
	if len(a.stack) == 0 {
		return
	}
	top := a.stack[len(a.stack)-1]

	var s viewState
	if t, ok := a.pageInstance(top.name).(*tableView); ok && t != nil {
		s = t.state()
	} else if b, ok := a.pageInstance(top.name).(*browser); ok && b != nil {
		s = b.state()
	}

	a.pages.RemovePage(top.name)
	a.clearInstance(top.name)

	b := newBrowser(a, spec)
	name := pageName("browse", spec.kind, "")
	a.stack[len(a.stack)-1] = pageEntry{name: name, title: spec.display, kind: spec.kind}
	a.pages.AddPage(name, b.root, true, true)
	a.tv.SetFocus(b.list)
	a.registerInstance(name, b)
	b.applyState(s)
}

// --- page-name → widget-instance registry ---
//
// tview.Pages stores primitives but not Go-side instance pointers, so we
// keep a parallel map to recover the *browser / *tableView for state
// handoff during view toggles. Cleared on popPage / RemovePage paths.

func (a *App) registerInstance(name string, inst any) {
	if a.instances == nil {
		a.instances = map[string]any{}
	}
	a.instances[name] = inst
}

func (a *App) pageInstance(name string) any {
	if a.instances == nil {
		return nil
	}
	return a.instances[name]
}

func (a *App) clearInstance(name string) {
	delete(a.instances, name)
}

// pushBrowserList opens a new Browser page filtered to a fixed set of
// IDs (used by edge drill).
func (a *App) pushBrowserList(refs EntityRefList) {
	spec, ok := a.specs[refs.Kind]
	if !ok {
		return
	}
	b := newBrowser(a, spec)
	b.setIDFilter(refs.IDs)
	name := pageName("drill", refs.Kind, "")
	title := spec.display + " (drilled)"
	a.pages.AddPage(name, b.root, true, true)
	a.stack = append(a.stack, pageEntry{name: name, title: title, kind: refs.Kind})
	a.tv.SetFocus(b.list)
	a.registerInstance(name, b)
	a.syncSidebar()
}

// frontIsModal returns true when the front tview page is NOT one of the
// stack pages (i.e. an overlay like preview / filter / picker / etc.).
// Used by the global esc handler to defer to the modal's own InputCapture
// instead of popping the underlying stack page.
func (a *App) frontIsModal() bool {
	front, _ := a.pages.GetFrontPage()
	if front == "" {
		return false
	}
	for _, e := range a.stack {
		if e.name == front {
			return false
		}
	}
	return true
}

// popPage returns to the previous page in the back-stack.
func (a *App) popPage() {
	if len(a.stack) <= 1 {
		return
	}
	top := a.stack[len(a.stack)-1]
	a.pages.RemovePage(top.name)
	a.clearInstance(top.name)
	a.stack = a.stack[:len(a.stack)-1]
	prev := a.stack[len(a.stack)-1]
	a.pages.SwitchToPage(prev.name)
}

// openPicker shows the kind picker modal.
func (a *App) openPicker() {
	p := newPicker(a)
	a.pages.AddPage("picker", p.root, true, true)
	a.tv.SetFocus(p.input)
}

// closePicker is called from the picker when the user dismisses or selects.
func (a *App) closePicker() {
	a.pages.RemovePage("picker")
	if len(a.stack) > 0 {
		a.pages.SwitchToPage(a.stack[len(a.stack)-1].name)
	}
}

// openHelp lives in help.go — searchable shortcut palette overlay.

var errEmpty = &runtimeError{msg: "enttui: no entities registered — call Register[T] before Run"}

type runtimeError struct{ msg string }

func (e *runtimeError) Error() string { return e.msg }

func pageName(prefix, kind, id string) string {
	if id == "" {
		return prefix + ":" + kind
	}
	return prefix + ":" + kind + ":" + id
}
