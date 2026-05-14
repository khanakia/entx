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
	"strings"

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
	{Category: "Global", Keys: "k", Action: "Open kind picker"},
	{Category: "Global", Keys: "?", Action: "Open this help palette"},
	{Category: "Global", Keys: "q", Action: "Quit"},
	{Category: "Global", Keys: "esc", Action: "Back / close modal"},

	// --- Browser (list+preview view) ---
	{Category: "Browser", Keys: "v", Action: "Toggle list+preview ↔ table view"},
	{Category: "Browser", Keys: "tab", Action: "Switch focus list ↔ preview"},
	{Category: "Browser", Keys: "↑ ↓ j k", Action: "Move selection"},
	{Category: "Browser", Keys: "← → h l", Action: "Switch pane"},
	{Category: "Browser", Keys: "enter", Action: "Open preview / drill primary edge"},
	{Category: "Browser", Keys: "/", Action: "Filter (substring on title + body)"},
	{Category: "Browser", Keys: "ctrl+u", Action: "Clear filter"},
	{Category: "Browser", Keys: "s", Action: "Cycle sort direction"},
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
		if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyEnter {
			closeFn()
			return nil
		}
		return ev
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(tview.NewBox().SetBorder(false), 1, 0, false). // 1-line gap
		AddItem(tbl, 0, 1, false).
		AddItem(tview.NewTextView().
			SetText(" type to search · ↑/↓ nav · esc / enter close ").
			SetTextColor(tcell.ColorGray), 1, 0, false)
	body.SetBorder(true).
		SetTitle(" keybindings ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	a.pages.AddPage("help", centerModal(body, 90, 32), true, true)
	a.tv.SetFocus(input)
}
