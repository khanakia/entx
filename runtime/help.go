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
	// --- Navigation (vim-faithful, no leader) ---
	{Category: "Navigate", Keys: "j k ↑ ↓", Action: "Move row"},
	{Category: "Navigate", Keys: "h l ← →", Action: "Switch pane (browser) / move column (table)"},
	{Category: "Navigate", Keys: "g g", Action: "First row (current page)"},
	{Category: "Navigate", Keys: "G", Action: "Last row (current page)"},
	{Category: "Navigate", Keys: ":", Action: "Go to row — number / $ last / 1 first"},
	{Category: "Navigate", Keys: "n p", Action: "Next / previous page"},
	{Category: "Navigate", Keys: "+ -", Action: "Cycle page size (10…1000)"},
	{Category: "Navigate", Keys: "tab", Action: "Switch focus (list ↔ preview / master ⇄ detail)"},
	{Category: "Navigate", Keys: "enter", Action: "Open preview / activate"},
	{Category: "Navigate", Keys: "esc", Action: "Clear selection → else back / close"},
	{Category: "Navigate", Keys: "r", Action: "Refresh"},
	{Category: "Navigate", Keys: "q", Action: "Quit"},
	{Category: "Navigate", Keys: "?", Action: "This help palette"},
	{Category: "Navigate", Keys: "ctrl+b / \\", Action: "Sidebar toggle / focus"},
	{Category: "Navigate", Keys: "<letter>", Action: "Follow that edge (shown in preview footer)"},

	// --- Filter / search ---
	{Category: "Filter", Keys: "/", Action: "Quick filter (substring across Filterable string cols)"},
	{Category: "Filter", Keys: "ctrl+u", Action: "Clear ALL filters (substring + conditions)"},

	// --- Selection (vim visual) ---
	{Category: "Select", Keys: "space", Action: "Toggle row selection (needs enttui.AllowBulkCopy{})"},
	{Category: "Select", Keys: "v", Action: "Visual range: v, move, v again — (de)select span"},
	{Category: "Select", Keys: "ctrl+a", Action: "Select all visible"},
	{Category: "Select", Keys: "esc", Action: "Clear selection"},

	// --- Yank (vim y operator, two-key) ---
	{Category: "Yank", Keys: "y y", Action: "Copy row as TSV"},
	{Category: "Yank", Keys: "y c", Action: "Copy focused cell (table) / id (browser)"},
	{Category: "Yank", Keys: "y j", Action: "Copy row as JSON"},
	{Category: "Yank", Keys: "y v", Action: "Copy selection → format chooser"},

	// --- Leader (`,` opens the which-key menu) ---
	{Category: "Leader (,)", Keys: ", v", Action: "Toggle list ⇄ table view"},
	{Category: "Leader (,)", Keys: ", e / , a / , d", Action: "Edit / add / delete row"},
	{Category: "Leader (,)", Keys: ", f", Action: "Filter — condition builder"},
	{Category: "Leader (,)", Keys: ", o", Action: "Order — sort-stack modal"},
	{Category: "Leader (,)", Keys: ", s", Action: "Sort focused column (cycle dir)"},
	{Category: "Leader (,)", Keys: ", c", Action: "Columns show/hide"},
	{Category: "Leader (,)", Keys: ", x", Action: "Export (JSON/CSV → file)"},
	{Category: "Leader (,)", Keys: ", m", Action: "Master-detail split"},
	{Category: "Leader (,)", Keys: ", i / , K", Action: "This-view capabilities / all-kinds matrix"},
	{Category: "Leader (,)", Keys: ", k", Action: "Kind picker (modal)"},
	{Category: "Leader (,)", Keys: ", t #", Action: "Toggle row numbers"},
	{Category: "Leader (,)", Keys: ", t b", Action: "Toggle status bar"},
	{Category: "Leader (,)", Keys: ", t m", Action: "Toggle mouse capture"},

	// --- Sidebar ---
	{Category: "Sidebar", Keys: "type", Action: "Filter kinds by display / id"},
	{Category: "Sidebar", Keys: "↑ ↓ pgup pgdn", Action: "Move selection (live-swaps body)"},
	{Category: "Sidebar", Keys: "tab", Action: "Cycle focus input ↔ list"},
	{Category: "Sidebar", Keys: "enter / esc / ctrl+b", Action: "Close sidebar"},

	// --- Master-detail ---
	{Category: "Master-detail", Keys: "tab", Action: "Switch focus master ⇄ detail pane"},
	{Category: "Master-detail", Keys: "] [", Action: "Cycle detail tabs (multi-edge)"},
	{Category: "Master-detail", Keys: ", m / esc", Action: "Exit the split"},

	// --- Modals (one consistent set) ---
	{Category: "Modals", Keys: "enter", Action: "Apply / select / toggle / flip"},
	{Category: "Modals", Keys: "esc", Action: "Cancel / close without applying"},
	{Category: "Modals", Keys: "tab", Action: "Cycle regions (list ↔ buttons)"},
	{Category: "Modals", Keys: "ctrl+↑ ↓ / K J", Action: "(sort modal) move entry"},
	{Category: "Modals", Keys: "d", Action: "(sort / condition) delete entry"},
	{Category: "Modals", Keys: "a", Action: "(condition builder) add condition"},
	{Category: "Modals", Keys: "s / ctrl+s", Action: "(condition / columns) apply from anywhere"},

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
		SetLabelColor(theme.Title).
		SetFieldWidth(50).
		SetFieldBackgroundColor(theme.Surface).SetFieldTextColor(theme.Text).SetPlaceholderTextColor(theme.Muted)

	tbl := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false). // rows selectable, cells not
		SetFixed(1, 0).             // header row pinned
		SetSeparator(' ').
		SetSelectedStyle(selStyle())

	// populate is recreated as a closure so the input's SetChangedFunc
	// can re-run it with whatever filter the user has typed so far.
	var shown []helpEntry
	populate := func() {
		tbl.Clear()
		// Header row.
		tbl.SetCell(0, 0, tview.NewTableCell("CATEGORY").
			SetTextColor(theme.Title).SetAttributes(tcell.AttrBold).
			SetSelectable(false).SetExpansion(1))
		tbl.SetCell(0, 1, tview.NewTableCell("KEY").
			SetTextColor(theme.Title).SetAttributes(tcell.AttrBold).
			SetSelectable(false).SetExpansion(2))
		tbl.SetCell(0, 2, tview.NewTableCell("ACTION").
			SetTextColor(theme.Title).SetAttributes(tcell.AttrBold).
			SetSelectable(false).SetExpansion(5))

		// Data rows.
		for r, e := range shown {
			tbl.SetCell(r+1, 0, tview.NewTableCell(e.Category).
				SetTextColor(theme.Accent2).SetExpansion(1))
			tbl.SetCell(r+1, 1, tview.NewTableCell(e.Keys).
				SetTextColor(theme.Warning).SetAttributes(tcell.AttrBold).SetExpansion(2))
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
		SetTextColor(theme.Muted)

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
		SetTitleColor(theme.Title).
		SetBorderColor(theme.Border)

	a.pages.AddPage("help", centerModal(body, 90, 32), true, true)
	a.tv.SetFocus(input)
}
