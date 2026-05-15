package runtime

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Row-selection support shared by browser + table.
//
// Selected rows live in a *selectionSet keyed by row ID. Both views
// embed one and forward space/a/c keystrokes to it. Rendering callers
// ask `isSelected(id)` to decide on a marker prefix.
//
// `y` becomes a multimode shortcut:
//
//   selection empty → existing behavior (copy id / cell value)
//   selection non-empty → open the format-chooser modal:
//     • JSON array of objects (every visible column)
//     • CSV (every visible column)
//     • Focused column → JSON array of strings  (table view only)
//     • Focused column → CSV of strings         (table view only)
//
// `X` is the full-dataset export. Re-fetches with the current filter +
// sort but no pagination (capped at exportRowCap), then offers the same
// JSON / CSV chooser. Both annotations gate the feature — without
// enttui.AllowBulkCopy / AllowExport the keys surface a hint via the
// view's status bar.

// statusBarHeight is the row count of the (two-line) status bar.
// Toggled to 0 by `B` to reclaim the space.
const statusBarHeight = 2

// exportRowCap caps export size to keep the clipboard sane. The cap
// applies both to bulk-copy (selection × all rows) and full export.
const exportRowCap = 10_000

// selectionSet tracks which row IDs the user has marked. Shared between
// pages of the same view via the view-state machinery — we DON'T
// preserve across kind switches (a Tasks selection isn't meaningful in
// the Comments view).
type selectionSet struct {
	ids map[string]bool
}

func newSelection() *selectionSet { return &selectionSet{ids: map[string]bool{}} }


func (s *selectionSet) has(id string) bool { return s != nil && s.ids[id] }
func (s *selectionSet) toggle(id string)   { if id == "" { return }; if s.ids[id] { delete(s.ids, id) } else { s.ids[id] = true } }
func (s *selectionSet) clear()             { s.ids = map[string]bool{} }
func (s *selectionSet) count() int         { return len(s.ids) }

func (s *selectionSet) addAll(rows []Row) {
	for _, r := range rows {
		if r.ID != "" {
			s.ids[r.ID] = true
		}
	}
}

// clampRange normalizes an order-independent [a,b] to in-bounds lo<=hi.
func clampRange(n, a, b int) (int, int) {
	if a > b {
		a, b = b, a
	}
	if a < 0 {
		a = 0
	}
	if b >= n {
		b = n - 1
	}
	return a, b
}

// rangeAllSelected reports whether EVERY row in [a,b] is already
// selected — drives the V smart-toggle (all selected → deselect span).
func (s *selectionSet) rangeAllSelected(rows []Row, a, b int) bool {
	a, b = clampRange(len(rows), a, b)
	if a > b {
		return false
	}
	for i := a; i <= b; i++ {
		if rows[i].ID != "" && !s.ids[rows[i].ID] {
			return false
		}
	}
	return true
}

// toggleRange selects the whole [a,b] span, OR deselects it when it was
// already fully selected. Returns (count, selected) so the caller can
// status-report. Order-independent.
func (s *selectionSet) toggleRange(rows []Row, a, b int) (int, bool) {
	a, b = clampRange(len(rows), a, b)
	if a > b {
		return 0, false
	}
	deselect := s.rangeAllSelected(rows, a, b)
	n := 0
	for i := a; i <= b; i++ {
		id := rows[i].ID
		if id == "" {
			continue
		}
		if deselect {
			delete(s.ids, id)
		} else {
			s.ids[id] = true
		}
		n++
	}
	return n, !deselect
}

// filteredRows returns the subset of rows whose IDs are selected. Order
// matches the input slice (caller-controlled, typically the rendered
// page order).
func (s *selectionSet) filteredRows(rows []Row) []Row {
	if s == nil || len(s.ids) == 0 {
		return nil
	}
	out := make([]Row, 0, len(s.ids))
	for _, r := range rows {
		if s.ids[r.ID] {
			out = append(out, r)
		}
	}
	return out
}

// openGotoRow shows a tiny prompt for vim-style row jumping. `n` is the
// number of rows on the current page; `jump` receives a 0-based index.
// Accepts: a 1-based integer, `$` / `last`, `1` / `^` / `first`. Out of
// range clamps. Enter commits, esc cancels.
func openGotoRow(app *App, n int, jump func(idx int)) {
	if n == 0 {
		return
	}
	input := tview.NewInputField().
		SetLabel(fmt.Sprintf(":goto (1-%d, $=last) ", n)).
		SetLabelColor(theme.Title).
		SetFieldWidth(10).
		SetFieldBackgroundColor(theme.Surface).SetFieldTextColor(theme.Text).SetPlaceholderTextColor(theme.Muted)

	close := func() { app.pages.RemovePage("goto-row") }

	input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			close()
			return
		}
		if key != tcell.KeyEnter {
			return
		}
		s := strings.TrimSpace(strings.ToLower(input.GetText()))
		close()
		switch s {
		case "", "^", "first", "g":
			jump(0)
			return
		case "$", "last", "end":
			jump(n - 1)
			return
		}
		v := 0
		for _, r := range s {
			if r < '0' || r > '9' {
				return // ignore junk
			}
			v = v*10 + int(r-'0')
		}
		idx := v - 1 // 1-based → 0-based
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		jump(idx)
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(tview.NewTextView().
			SetTextColor(theme.Muted).
			SetText(" number · $ last · 1 first · enter go · esc cancel "), 1, 0, false)
	body.SetBorder(true).
		SetTitle(" go to row ").
		SetTitleColor(theme.Title).
		SetBorderColor(theme.Border)

	app.pages.AddPage("goto-row", centerModal(body, 50, 5), true, true)
	app.tv.SetFocus(input)
}

// --- format chooser modal ---

// formatChoice enumerates the four bulk-copy variants + the two export
// variants. The view picks which subset to show.
type formatChoice int

const (
	formatJSONArray formatChoice = iota
	formatCSV
	formatColJSON
	formatColCSV
)

// openFormatChooser shows a button picker for the export/bulk-copy
// format. `focusedCol` is empty when the caller doesn't have a column
// concept (browser view) — column variants stay hidden in that case.
//
// onPick is invoked with the user's choice; cancel just closes.
func openFormatChooser(app *App, focusedCol string, onPick func(formatChoice)) {
	close := func() { app.pages.RemovePage("format-chooser") }

	form := tview.NewForm().
		SetButtonsAlign(tview.AlignCenter).
		SetButtonBackgroundColor(theme.SelectionBg).
		SetButtonTextColor(theme.Text)

	form.AddButton("JSON array", func() { close(); onPick(formatJSONArray) })
	form.AddButton("CSV", func() { close(); onPick(formatCSV) })
	if focusedCol != "" {
		form.AddButton(focusedCol+" → JSON", func() { close(); onPick(formatColJSON) })
		form.AddButton(focusedCol+" → CSV", func() { close(); onPick(formatColCSV) })
	}

	hint := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetTextColor(theme.Muted).
		SetText("← → / tab : switch · enter : pick · esc : cancel")

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 3, 0, true).
		AddItem(hint, 1, 0, false)
	body.SetBorder(true).
		SetTitle(" copy as ").
		SetTitleColor(theme.Title).
		SetBorderColor(theme.Border)

	body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyLeft:
			return tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
		case tcell.KeyRight:
			return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		}
		return ev
	})

	app.pages.AddPage("format-chooser", centerModal(body, 70, 5), true, true)
	app.tv.SetFocus(form)
}

// --- formatters ---

// formatRows serializes `rows` according to `choice`. visibleCols
// drives the column set + column ordering for JSON object keys and CSV
// headers. focusedCol is used only by formatCol* variants.
func formatRows(rows []Row, visibleCols []anyColumn, focusedCol string, choice formatChoice) (string, error) {
	switch choice {
	case formatJSONArray:
		return rowsToJSONArray(rows, visibleCols)
	case formatCSV:
		return rowsToCSV(rows, visibleCols)
	case formatColJSON:
		return colToJSON(rows, focusedCol)
	case formatColCSV:
		return colToCSV(rows, focusedCol)
	}
	return "", fmt.Errorf("unknown format")
}

func rowsToJSONArray(rows []Row, cols []anyColumn) (string, error) {
	out := make([]map[string]string, 0, len(rows))
	for _, r := range rows {
		obj := make(map[string]string, len(cols))
		// Ensure ID is present even if no Column happens to be keyed "id".
		obj["id"] = r.ID
		for _, c := range cols {
			obj[c.key] = r.Columns[c.key]
		}
		out = append(out, obj)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func rowsToCSV(rows []Row, cols []anyColumn) (string, error) {
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	// Header: id + every visible column key.
	header := []string{"id"}
	for _, c := range cols {
		header = append(header, c.key)
	}
	if err := w.Write(header); err != nil {
		return "", err
	}
	for _, r := range rows {
		rec := make([]string, 0, len(header))
		rec = append(rec, r.ID)
		for _, c := range cols {
			rec = append(rec, r.Columns[c.key])
		}
		if err := w.Write(rec); err != nil {
			return "", err
		}
	}
	w.Flush()
	return buf.String(), w.Error()
}

func colToJSON(rows []Row, col string) (string, error) {
	if col == "" {
		return "", fmt.Errorf("no focused column")
	}
	vals := make([]string, 0, len(rows))
	for _, r := range rows {
		vals = append(vals, r.Columns[col])
	}
	b, err := json.MarshalIndent(vals, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func colToCSV(rows []Row, col string) (string, error) {
	if col == "" {
		return "", fmt.Errorf("no focused column")
	}
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{col}); err != nil {
		return "", err
	}
	for _, r := range rows {
		if err := w.Write([]string{r.Columns[col]}); err != nil {
			return "", err
		}
	}
	w.Flush()
	return buf.String(), w.Error()
}

// formatLabel returns a human-readable name for status messages.
func formatLabel(c formatChoice) string {
	switch c {
	case formatJSONArray:
		return "JSON"
	case formatCSV:
		return "CSV"
	case formatColJSON:
		return "column JSON"
	case formatColCSV:
		return "column CSV"
	}
	return "?"
}

// --- export ---

// runExport exports rows as JSON/CSV to a file or clipboard.
//
// Scope precedence:
//   - selected non-empty → export EXACTLY those rows (no re-fetch). The
//     user explicitly picked them; respect it. This is the fix for
//     "selected some rows but X dumped the whole filtered set".
//   - selected empty → re-fetch every row under the current filter /
//     sort / scope (capped at exportRowCap) and export that.
func runExport(host *modalHost, fetch func(ctx context.Context, opts ListOpts) ([]Row, int, error), opts ListOpts, visibleCols []anyColumn, selected []Row) {
	if !host.app.exportAllowed(host) {
		host.updateStatus("export not enabled — add enttui.AllowExport{} to the schema")
		return
	}
	kind := ""
	if len(host.app.stack) > 0 {
		kind = host.app.stack[len(host.app.stack)-1].kind
	}
	openFormatChooser(host.app, "", func(choice formatChoice) {
		var rows []Row
		var total int
		scopeNote := ""

		if len(selected) > 0 {
			rows = selected
			total = len(selected)
			scopeNote = "selected "
		} else {
			opts.Offset = 0
			opts.Limit = exportRowCap
			ctx, cancel := context.WithTimeout(host.app.ctx, 30*time.Second)
			defer cancel()
			fetched, ftotal, err := fetch(ctx, opts)
			if err != nil {
				host.updateStatus("export failed: " + err.Error())
				return
			}
			rows, total = fetched, ftotal
		}

		text, ferr := formatRows(rows, visibleCols, "", choice)
		if ferr != nil {
			host.updateStatus("format failed: " + ferr.Error())
			return
		}
		label := fmt.Sprintf("%s%d rows", scopeNote, len(rows))
		if total > len(rows) {
			label += fmt.Sprintf(" (truncated from %d — cap %d)", total, exportRowCap)
		}
		openExportDestination(host, kind, choice, text, label)
	})
}

// openExportDestination asks for a file path (default suggested from
// kind + timestamp + format extension) or copy-to-clipboard. Two
// buttons: Save / Clipboard / Cancel.
func openExportDestination(host *modalHost, kind string, choice formatChoice, text, label string) {
	ext := "csv"
	if choice == formatJSONArray || choice == formatColJSON {
		ext = "json"
	}
	cwd, _ := os.Getwd()
	def := filepath.Join(cwd, fmt.Sprintf("%s-%s.%s", kind, time.Now().Format("20060102-150405"), ext))

	input := tview.NewInputField().
		SetLabel("path  ").
		SetLabelColor(theme.Title).
		SetFieldBackgroundColor(theme.Surface).SetFieldTextColor(theme.Text).SetPlaceholderTextColor(theme.Muted).
		SetText(def)

	status := tview.NewTextView().SetDynamicColors(true).SetTextColor(theme.Muted)
	close := func() { host.app.pages.RemovePage("export-dest") }

	form := tview.NewForm().
		SetButtonsAlign(tview.AlignCenter).
		SetButtonBackgroundColor(theme.SelectionBg).
		SetButtonTextColor(theme.Text).
		AddButton("Save to file", func() {
			path := strings.TrimSpace(input.GetText())
			if path == "" {
				status.SetText("[red]path is required[-]")
				return
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				status.SetText("[red]write failed: " + err.Error() + "[-]")
				return
			}
			close()
			host.updateStatus(fmt.Sprintf("wrote %s as %s → %s", label, formatLabel(choice), path))
		}).
		AddButton("Copy to clipboard", func() {
			close()
			copyToClipboard(host, text, label+" as "+formatLabel(choice))
		}).
		AddButton("Cancel", close)

	hint := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetTextColor(theme.Muted).
		SetText("← → / tab : switch · enter : pick · esc : cancel")

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, false).
		AddItem(form, 3, 0, true).
		AddItem(status, 1, 0, false).
		AddItem(hint, 1, 0, false)
	body.SetBorder(true).
		SetTitle(fmt.Sprintf(" export %s ", formatLabel(choice))).
		SetTitleColor(theme.Title).
		SetBorderColor(theme.Border)

	body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyLeft:
			return tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
		case tcell.KeyRight:
			return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		}
		return ev
	})

	host.app.pages.AddPage("export-dest", centerModal(body, 90, 9), true, true)
	host.app.tv.SetFocus(form)
}

// exportAllowed is on App so we can route through the spec lookup.
func (a *App) exportAllowed(h *modalHost) bool {
	// host points at one entity's state; we recover the spec via the
	// front page kind.
	if len(a.stack) == 0 {
		return false
	}
	if s := a.specs[a.stack[len(a.stack)-1].kind]; s != nil {
		return s.allowExport
	}
	return false
}
