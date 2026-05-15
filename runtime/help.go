package runtime

// help.go — searchable, neatly-aligned shortcut palette overlay.
//
// Renders as a tview.Table with three columns (Category, Keys, Action).
// tview.Table handles alignment + cell widths properly, sidestepping the
// alignment mess we'd hit using a tview.List + manual padding on strings
// that contain color tags and multibyte runes (↑, ↓, em-dash, etc.).
//
// UX:
//   - filter input at the top — type to live-filter across all columns
//   - ↑/↓ on input drives the table
//   - enter / esc closes

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// helpEntry is one searchable keybinding row.
type helpEntry struct {
	Keys     string // e.g. "v" or "ctrl+u" or "↑ / ↓"
	Action   string // human-readable description
	Category string // "Global", "Browser", "Table", "Edges", "Modals"
}

// helpEntries is the canonical list of every keybinding in the app. New
// shortcuts MUST be added here so the help palette stays accurate.
var helpEntries = []helpEntry{
	// --- Global (work everywhere) ---
	{Category: "Global", Keys: "k", Action: "Open kind picker (modal)"},
	{Category: "Global", Keys: "ctrl+b", Action: "Toggle sidebar (works inside text inputs too)"},
	{Category: "Global", Keys: "\\", Action: "Focus sidebar from body (opens if hidden)"},
	{Category: "Sidebar", Keys: "type", Action: "Filter kinds by display name / kind id"},
	{Category: "Sidebar", Keys: "↑ ↓ pgup pgdn", Action: "Move selection (live-swaps the body)"},
	{Category: "Sidebar", Keys: "tab", Action: "Cycle focus input ↔ list"},
	{Category: "Sidebar", Keys: "\\", Action: "Send focus to body (sidebar stays open)"},
	{Category: "Sidebar", Keys: "enter / esc / ctrl+b", Action: "Close sidebar"},
	{Category: "Global", Keys: "?", Action: "Open this help palette"},
	{Category: "Global", Keys: "F", Action: "Capabilities matrix — every kind × edit/new/del/bulk/export"},
	{Category: "Global", Keys: "i", Action: "Capabilities card for the CURRENT view (what's on/off + how to enable)"},
	{Category: "Global", Keys: "M", Action: "Toggle mouse capture (default off → terminal text selection / copy works)"},
	{Category: "Global", Keys: "B", Action: "Show / hide the status bar"},
	{Category: "Global", Keys: "#", Action: "Toggle row-number prefix (default on)"},
	{Category: "Global", Keys: ":", Action: "Go to row — number / $ last / 1 first (vim-style)"},
	{Category: "Global", Keys: "q", Action: "Quit"},
	{Category: "Global", Keys: "esc", Action: "Back / close modal"},

	// --- Browser (list+preview view) ---
	{Category: "Browser", Keys: "v", Action: "Toggle list+preview ↔ table view"},
	{Category: "Global", Keys: "m", Action: "Master-detail split (master + live child table; tabbed if >1 edge; needs enttui.DetailEdge{})"},
	{Category: "Master-detail", Keys: "tab", Action: "Switch focus master ⇄ detail pane"},
	{Category: "Master-detail", Keys: "] [", Action: "Cycle detail tabs (multi-edge)"},
	{Category: "Master-detail", Keys: "m / esc", Action: "Exit the split"},
	{Category: "Browser", Keys: "tab", Action: "Switch focus list ↔ preview"},
	{Category: "Browser", Keys: "↑ ↓ j k", Action: "Move selection"},
	{Category: "Browser", Keys: "← → h l", Action: "Switch pane"},
	{Category: "Browser", Keys: "enter", Action: "Open preview / drill primary edge"},
	{Category: "Browser", Keys: "/", Action: "Filter (substring on title + body)"},
	{Category: "Browser", Keys: "ctrl+u", Action: "Clear filter"},
	{Category: "Browser", Keys: "s", Action: "Cycle sort direction"},
	{Category: "Browser", Keys: "f", Action: "Open condition builder (shared with table view)"},
	{Category: "Browser", Keys: "S", Action: "Open sort-stack modal"},
	{Category: "Browser", Keys: "c", Action: "Open columns show/hide modal"},
	{Category: "Browser", Keys: "y", Action: "Copy current row's id to clipboard"},
	{Category: "Browser", Keys: "Y", Action: "Copy whole row (id, title, status, body) as TSV"},
	{Category: "Browser", Keys: "J", Action: "Copy row as pretty-printed JSON"},
	{Category: "Browser", Keys: "e", Action: "Edit current row (opt-in via enttui.Editable() per field)"},
	{Category: "Browser", Keys: "N", Action: "New row (requires enttui.AllowCreate{}; scope keys auto-injected)"},
	{Category: "Browser", Keys: "D", Action: "Delete current row with confirm (requires enttui.AllowDelete{})"},
	{Category: "Browser", Keys: "space", Action: "Toggle row selection (requires enttui.AllowBulkCopy{})"},
	{Category: "Browser", Keys: "V", Action: "Range toggle: V drops anchor, move, V again selects span (or deselects if all already selected)"},
	{Category: "Browser", Keys: "*  /  0", Action: "Select all visible / clear selection (esc also clears before it pops the page)"},
	{Category: "Browser", Keys: "y (with selection)", Action: "Bulk copy selected rows → format chooser (JSON/CSV)"},
	{Category: "Browser", Keys: "X", Action: "Export full filtered+sorted dataset (requires enttui.AllowExport{})"},
	{Category: "Browser", Keys: "r", Action: "Refresh data"},
	{Category: "Browser", Keys: "n  p", Action: "Next / previous page"},
	{Category: "Browser", Keys: "g  G", Action: "First / last page"},
	{Category: "Browser", Keys: "+  -", Action: "Cycle page size (10 / 20 / 50 / 100 / 200 / 500 / 1000)"},
	{Category: "Browser", Keys: "space pgdn ctrl+f", Action: "Scroll preview down"},
	{Category: "Browser", Keys: "pgup ctrl+b", Action: "Scroll preview up"},

	// --- Table (datatable view) ---
	{Category: "Table", Keys: "v", Action: "Toggle back to list+preview view"},
	{Category: "Table", Keys: "↑ ↓", Action: "Move row selection"},
	{Category: "Table", Keys: "← →", Action: "Move column focus"},
	{Category: "Table", Keys: "enter", Action: "Open preview overlay for row"},
	{Category: "Table", Keys: "s", Action: "Sort on focused column"},
	{Category: "Table", Keys: "S", Action: "Open sort-stack reorder modal"},
	{Category: "Table", Keys: "f", Action: "Open condition builder"},
	{Category: "Table", Keys: "c", Action: "Open columns show/hide modal"},
	{Category: "Table", Keys: "/", Action: "Substring filter (legacy / global)"},
	{Category: "Table", Keys: "ctrl+u", Action: "Clear filter"},
	{Category: "Table", Keys: "r", Action: "Refresh"},
	{Category: "Table", Keys: "n  p", Action: "Next / previous page"},
	{Category: "Table", Keys: "g  G", Action: "First / last page"},
	{Category: "Table", Keys: "+  -", Action: "Cycle page size (10 / 20 / 50 / 100 / 200 / 500 / 1000)"},
	{Category: "Table", Keys: "y", Action: "Copy focused cell value to clipboard"},
	{Category: "Table", Keys: "Y", Action: "Copy focused row (all visible columns, tab-separated)"},
	{Category: "Table", Keys: "J", Action: "Copy row as pretty-printed JSON"},
	{Category: "Table", Keys: "e", Action: "Edit current row (opt-in via enttui.Editable() per field)"},
	{Category: "Table", Keys: "N", Action: "New row (requires enttui.AllowCreate{})"},
	{Category: "Table", Keys: "D", Action: "Delete current row with confirm (requires enttui.AllowDelete{})"},
	{Category: "Table", Keys: "space", Action: "Toggle row selection (requires enttui.AllowBulkCopy{})"},
	{Category: "Table", Keys: "V", Action: "Range toggle: V drops anchor, move, V again selects span (or deselects if all already selected)"},
	{Category: "Browser", Keys: "*  /  0", Action: "Select all visible / clear selection (esc also clears before it pops the page)"},
	{Category: "Table", Keys: "y (with selection)", Action: "Bulk copy → JSON/CSV (focused column variant offered)"},
	{Category: "Table", Keys: "X", Action: "Export full filtered+sorted dataset (requires enttui.AllowExport{})"},

	// --- Edges ---
	{Category: "Edges", Keys: "enter", Action: "Follow primary drill edge"},
	{Category: "Edges", Keys: "<letter>", Action: "Per-edge trigger (shown in preview footer)"},

	// --- Modals ---
	{Category: "Modals", Keys: "ctrl+↑ / shift+↑ / K", Action: "(sort modal) move entry up"},
	{Category: "Modals", Keys: "ctrl+↓ / shift+↓ / J", Action: "(sort modal) move entry down"},
	{Category: "Modals", Keys: "enter", Action: "(sort modal) flip direction asc ↔ desc"},
	{Category: "Modals", Keys: "c", Action: "(sort modal) clear stack"},
	{Category: "Modals", Keys: "d", Action: "(sort / condition) delete entry"},
	{Category: "Modals", Keys: "a / + / n", Action: "(condition builder) add new condition"},
	{Category: "Modals", Keys: "enter / e", Action: "(condition builder) edit focused condition"},
	{Category: "Modals", Keys: "s", Action: "(condition builder) apply from anywhere"},
	{Category: "Modals", Keys: "enter", Action: "Apply / select / toggle"},
	{Category: "Modals", Keys: "esc", Action: "Close modal without applying"},

	// --- Help palette (this overlay) ---
	{Category: "Help", Keys: "type", Action: "Full-text filter across category / key / action"},
	{Category: "Help", Keys: "@<cat>", Action: "Scope filter to one category (e.g. @table, @modals)"},
	{Category: "Help", Keys: "ctrl+e", Action: "Export shown keybindings → CSV file (cwd)"},
	{Category: "Help", Keys: "↑ ↓ pgup pgdn", Action: "Move selection"},
	{Category: "Help", Keys: "enter / esc", Action: "Close help"},
}

// openHelp shows a searchable list of every keybinding. Replaces the
// previous tview.Modal blob.
func (a *App) openHelp() {
	input := tview.NewInputField().
		SetLabel("filter › ").
		SetLabelColor(tcell.ColorYellow).
		SetFieldWidth(50).
		SetFieldBackgroundColor(tcell.ColorDefault)

	tbl := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false). // rows selectable, cells not
		SetFixed(1, 0).             // header row pinned
		SetSeparator(' ').
		SetSelectedStyle(tcell.StyleDefault.
			Background(tcell.ColorDodgerBlue).
			Foreground(tcell.ColorBlack).
			Attributes(tcell.AttrBold))

	// populate is recreated as a closure so the input's SetChangedFunc
	// can re-run it with whatever filter the user has typed so far.
	var shown []helpEntry
	populate := func() {
		tbl.Clear()
		// Header row.
		tbl.SetCell(0, 0, tview.NewTableCell("CATEGORY").
			SetTextColor(tcell.ColorYellow).SetAttributes(tcell.AttrBold).
			SetSelectable(false).SetExpansion(1))
		tbl.SetCell(0, 1, tview.NewTableCell("KEY").
			SetTextColor(tcell.ColorYellow).SetAttributes(tcell.AttrBold).
			SetSelectable(false).SetExpansion(2))
		tbl.SetCell(0, 2, tview.NewTableCell("ACTION").
			SetTextColor(tcell.ColorYellow).SetAttributes(tcell.AttrBold).
			SetSelectable(false).SetExpansion(5))

		// Data rows.
		for r, e := range shown {
			tbl.SetCell(r+1, 0, tview.NewTableCell(e.Category).
				SetTextColor(tcell.ColorAqua).SetExpansion(1))
			tbl.SetCell(r+1, 1, tview.NewTableCell(e.Keys).
				SetTextColor(tcell.ColorOrange).SetAttributes(tcell.AttrBold).SetExpansion(2))
			tbl.SetCell(r+1, 2, tview.NewTableCell(e.Action).SetExpansion(5))
		}

		// Always focus the first data row when populated.
		if len(shown) > 0 {
			tbl.Select(1, 0)
		}
	}

	applyFilter := func(text string) {
		q := strings.ToLower(strings.TrimSpace(text))
		if q == "" {
			shown = helpEntries
			populate()
			return
		}
		// `@cat` syntax → scope the match to Category only. Everything
		// after the @ is a case-insensitive substring of the category
		// name; a trailing space + more text AND-composes a full-text
		// filter within that category (e.g. "@table sort").
		if strings.HasPrefix(q, "@") {
			rest := strings.TrimPrefix(q, "@")
			cat, term, _ := strings.Cut(rest, " ")
			cat = strings.TrimSpace(cat)
			term = strings.TrimSpace(term)
			shown = make([]helpEntry, 0, len(helpEntries))
			for _, e := range helpEntries {
				if !strings.Contains(strings.ToLower(e.Category), cat) {
					continue
				}
				if term != "" {
					hay := strings.ToLower(e.Keys + " " + e.Action)
					if !strings.Contains(hay, term) {
						continue
					}
				}
				shown = append(shown, e)
			}
			populate()
			return
		}
		shown = make([]helpEntry, 0, len(helpEntries))
		for _, e := range helpEntries {
			hay := strings.ToLower(e.Keys + " " + e.Action + " " + e.Category)
			if strings.Contains(hay, q) {
				shown = append(shown, e)
			}
		}
		populate()
	}
	applyFilter("")

	footer := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" type to search · [yellow]@cat[-] scope to category · [yellow]ctrl+e[-] export CSV · ↑/↓ nav · esc close ").
		SetTextColor(tcell.ColorGray)

	exportCSV := func() {
		cwd, _ := os.Getwd()
		path := filepath.Join(cwd, "enttui-keybindings-"+time.Now().Format("20060102-150405")+".csv")
		f, err := os.Create(path)
		if err != nil {
			footer.SetText(" [red]export failed: " + err.Error() + "[-]")
			return
		}
		w := csv.NewWriter(f)
		_ = w.Write([]string{"category", "keys", "action"})
		for _, e := range shown {
			_ = w.Write([]string{e.Category, e.Keys, e.Action})
		}
		w.Flush()
		cerr := w.Error()
		_ = f.Close()
		if cerr != nil {
			footer.SetText(" [red]export failed: " + cerr.Error() + "[-]")
			return
		}
		footer.SetText(fmt.Sprintf(" [green]wrote %d rows → %s[-]", len(shown), path))
	}

	input.SetChangedFunc(applyFilter)

	// Arrow keys from the input drive the table (fzf-like).
	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			r, _ := tbl.GetSelection()
			if r < tbl.GetRowCount()-1 {
				tbl.Select(r+1, 0)
			}
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			r, _ := tbl.GetSelection()
			if r > 1 { // never land on header
				tbl.Select(r-1, 0)
			}
			return nil
		case tcell.KeyPgDn:
			r, _ := tbl.GetSelection()
			next := min(r+10, tbl.GetRowCount()-1)
			tbl.Select(next, 0)
			return nil
		case tcell.KeyPgUp:
			r, _ := tbl.GetSelection()
			prev := max(r-10, 1)
			tbl.Select(prev, 0)
			return nil
		case tcell.KeyCtrlE:
			exportCSV()
			return nil
		}
		return ev
	})

	closeFn := func() { a.pages.RemovePage("help") }
	input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEscape, tcell.KeyEnter:
			closeFn()
		}
	})
	tbl.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape, tcell.KeyEnter:
			closeFn()
			return nil
		case tcell.KeyCtrlE:
			exportCSV()
			return nil
		}
		return ev
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(tview.NewBox().SetBorder(false), 1, 0, false). // 1-line gap
		AddItem(tbl, 0, 1, false).
		AddItem(footer, 1, 0, false)
	body.SetBorder(true).
		SetTitle(" keybindings ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	a.pages.AddPage("help", centerModal(body, 90, 32), true, true)
	a.tv.SetFocus(input)
}
