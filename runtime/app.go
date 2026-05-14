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
}

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

	// Pick the first registered kind as initial view.
	first := a.kindOrder[0]
	a.pushBrowser(first, "")

	// Global key handler — single-letter shortcuts (`k`, `q`, `?`) only
	// fire when focus is NOT inside a text input; otherwise the user
	// typing "task" into the picker would re-trigger the picker.
	a.tv.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		// Don't eat keystrokes while the user is typing in an input field.
		if _, typing := a.tv.GetFocus().(*tview.InputField); typing {
			if ev.Key() == tcell.KeyEscape {
				a.popPage()
				return nil
			}
			return ev
		}
		switch ev.Rune() {
		case 'k':
			a.openPicker()
			return nil
		case 'q':
			a.tv.Stop()
			return nil
		case '?':
			a.openHelp()
			return nil
		}
		switch ev.Key() {
		case tcell.KeyEscape:
			a.popPage()
			return nil
		}
		return ev
	})

	return a.tv.SetRoot(a.pages, true).EnableMouse(true).Run()
}

// pushBrowser opens a new Browser page for the given kind. Optional ID
// restricts the page to a single row (used by edge upward navigation).
func (a *App) pushBrowser(kind, focusID string) {
	spec, ok := a.specs[kind]
	if !ok {
		return
	}
	b := newBrowser(a, spec)
	name := pageName("browse", kind, focusID)
	title := spec.display
	if title == "" {
		title = kind
	}
	a.pages.AddPage(name, b.root, true, true)
	a.stack = append(a.stack, pageEntry{name: name, title: title})
	a.tv.SetFocus(b.list)
	if focusID != "" {
		b.focusID(focusID)
	}
}

// swapToTable replaces the current top page with a table view of the same
// spec. Used by the browser's `v` key.
func (a *App) swapToTable(spec *anySpec) {
	if len(a.stack) == 0 {
		return
	}
	top := a.stack[len(a.stack)-1]
	a.pages.RemovePage(top.name)

	t := newTableView(a, spec)
	name := pageName("table", spec.kind, "")
	a.stack[len(a.stack)-1] = pageEntry{name: name, title: spec.display + " (table)"}
	a.pages.AddPage(name, t.root, true, true)
	a.tv.SetFocus(t.table)
}

// swapToBrowser is the inverse — used by the table view's `v` key.
func (a *App) swapToBrowser(spec *anySpec) {
	if len(a.stack) == 0 {
		return
	}
	top := a.stack[len(a.stack)-1]
	a.pages.RemovePage(top.name)

	b := newBrowser(a, spec)
	name := pageName("browse", spec.kind, "")
	a.stack[len(a.stack)-1] = pageEntry{name: name, title: spec.display}
	a.pages.AddPage(name, b.root, true, true)
	a.tv.SetFocus(b.list)
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
	a.stack = append(a.stack, pageEntry{name: name, title: title})
	a.tv.SetFocus(b.list)
}

// popPage returns to the previous page in the back-stack.
func (a *App) popPage() {
	if len(a.stack) <= 1 {
		return
	}
	top := a.stack[len(a.stack)-1]
	a.pages.RemovePage(top.name)
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

// openHelp shows a modal with keybindings.
func (a *App) openHelp() {
	m := tview.NewModal().
		SetText(helpText).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			a.pages.RemovePage("help")
		})
	a.pages.AddPage("help", m, true, true)
}

const helpText = `enttui — keybindings

  k        open kind picker
  /        filter (substring)
  s        cycle sort
  enter    open preview / drill into edge
  esc      back / close modal
  ?        this help
  q        quit
`

var errEmpty = &runtimeError{msg: "enttui: no entities registered — call Register[T] before Run"}

type runtimeError struct{ msg string }

func (e *runtimeError) Error() string { return e.msg }

func pageName(prefix, kind, id string) string {
	if id == "" {
		return prefix + ":" + kind
	}
	return prefix + ":" + kind + ":" + id
}
