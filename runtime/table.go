package runtime

// table.go — Phase A of the table-view roadmap.
//
// Layout:
//
//   ┌─────────────────────────────────────────────────────┐
//   │ Title  Status  Priority  Created   …                │  header row (sticky)
//   ├─────────────────────────────────────────────────────┤
//   │ Ship payment v2  doing   p0        2 days ago  …    │
//   │ Wire migration   todo    p1        3 days ago  …    │
//   │ …                                                   │
//   └─────────────────────────────────────────────────────┘
//                       status bar (1 line)
//
// Reuses spec.fetch + spec.columns (same data the browser consumes). The
// preview is opened as a Pages overlay on `enter`, so the preview template
// stays the same between view modes.
//
// Phase A scope: read-only, full-screen table, fixed sort, global filter,
// no pagination UI. Phases B–G are additive on this base.

import (
	"context"
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// tableView is one Page rendering an entity as a tview.Table. Held by the
// App's page stack same way *browser is.
type tableView struct {
	app  *App
	spec *anySpec

	root  *tview.Flex
	table *tview.Table
	stat  *tview.TextView

	rows   []Row
	total  int
	offset int

	filter    string
	sortField string
	sortDir   SortDir

	// Pagination state (Phase B).
	page     int
	pageSize int

	// Multi-sort stack (Phase D). When non-empty, supersedes sortField/Dir.
	sortStack []SortKey

	// Per-column filters (Phase E). AND-composed.
	colFilters []FilterCondition

	// User-overridden visible columns (Phase G). Nil = use spec defaults.
	columnOverrides map[string]bool

	// Row-selection set (driven by space / V / * / 0; consumed by `y`).
	selection  *selectionSet
	selAnchor  int  // V-range anchor data-row index; -1 = unset
	showRowNum    bool // `#` toggles a 1-based index prefix in column 0
	statusVisible bool // `B` collapses the status bar
	// idFilter, when non-nil, restricts rows to this set — used by the
	// master-detail split so the detail table shows only the selected
	// master row's children.
	idFilter map[string]bool
}

// state captures the current view state for handoff to the browser view
// on `v` toggle. Filter / sort stack / column overrides / page state /
// selected row all survive.
func (t *tableView) state() viewState {
	id := ""
	if r, _ := t.table.GetSelection(); r >= 1 && r-1 < len(t.rows) {
		id = t.rows[r-1].ID
	}
	stack := append([]SortKey(nil), t.sortStack...)
	filters := append([]FilterCondition(nil), t.colFilters...)
	var overrides map[string]bool
	if t.columnOverrides != nil {
		overrides = make(map[string]bool, len(t.columnOverrides))
		for k, v := range t.columnOverrides {
			overrides[k] = v
		}
	}
	return viewState{
		Filter:          t.filter,
		Filters:         filters,
		SortField:       t.sortField,
		SortDir:         t.sortDir,
		SortStack:       stack,
		Page:            t.page,
		PageSize:        t.pageSize,
		SelectedID:      id,
		ColumnOverrides: overrides,
	}
}

// applyState seeds this tableView from a previous view's state.
func (t *tableView) applyState(s viewState) {
	if s.Filter != "" {
		t.filter = s.Filter
	}
	if s.Filters != nil {
		t.colFilters = append([]FilterCondition(nil), s.Filters...)
	}
	if s.SortField != "" {
		t.sortField = s.SortField
		t.sortDir = s.SortDir
	}
	if s.SortStack != nil {
		t.sortStack = append([]SortKey(nil), s.SortStack...)
	}
	if s.PageSize > 0 {
		t.pageSize = s.PageSize
	}
	t.page = s.Page
	if s.ColumnOverrides != nil {
		t.columnOverrides = make(map[string]bool, len(s.ColumnOverrides))
		for k, v := range s.ColumnOverrides {
			t.columnOverrides[k] = v
		}
	}
	t.refresh()
	if s.SelectedID != "" {
		t.focusID(s.SelectedID)
	}
}

// focusID moves selection to the data row with the given ID, if present.
// Used after view toggles to preserve selection.
func (t *tableView) focusID(id string) {
	for i, r := range t.rows {
		if r.ID == id {
			t.table.Select(i+1, t.colOffset()) // +1 header; skip the # col
			return
		}
	}
}

// newTableView builds the widget tree for an entity in table mode. Does
// NOT push itself to the Pages stack — the caller does. Refresh runs once
// before the function returns so cells are populated when shown.
// colOffset is 1 when the dedicated row-number column is shown (it
// occupies table column 0), else 0. Every data-column index in the
// table is `colsIndex + colOffset()`.
func (t *tableView) toggleStatus() {
	t.statusVisible = !t.statusVisible
	h := statusBarHeight
	if !t.statusVisible {
		h = 0
	}
	t.root.ResizeItem(t.stat, h, 0)
}

func (t *tableView) colOffset() int {
	if t.showRowNum {
		return 1
	}
	return 0
}

// focusedDataCol maps the table's selected column back to an index into
// the visibleColumns slice, accounting for the row-number column. Always
// clamped to a valid index (never negative, never past the end).
func (t *tableView) focusedDataCol() int {
	_, c := t.table.GetSelection()
	i := c - t.colOffset()
	if i < 0 {
		return 0
	}
	if n := len(t.visibleColumns()); n > 0 && i >= n {
		return n - 1
	}
	return i
}

func newTableView(app *App, spec *anySpec) *tableView {
	ps := spec.pageSize
	if ps <= 0 {
		ps = 200
	}
	t := &tableView{
		app:       app,
		spec:      spec,
		selection:  newSelection(),
		selAnchor:  -1,
		showRowNum: true,
		sortField:  spec.defaultView.SortField,
		sortDir:   spec.defaultView.SortDir,
		pageSize:  ps,
	}

	// Selectable on BOTH axes so ←→ moves column focus (Phase D `s` sorts
	// the focused column) and ↑↓ moves row focus.
	//
	// SetFixed(1, 0) pins the header row in place during vertical scroll.
	// SetSelectedStyle paints the focused cell with high contrast so the
	// user can actually see where they are when navigating with arrows.
	t.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, true).
		SetFixed(1, 0).
		SetSeparator(' ').
		SetSelectedStyle(tcell.StyleDefault.
			Background(tcell.ColorDodgerBlue).
			Foreground(tcell.ColorBlack).
			Attributes(tcell.AttrBold))
	t.table.SetBorder(true).
		SetTitle(" " + spec.display + " (table) ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorOrange) // table is the focused pane

	t.stat = tview.NewTextView().SetDynamicColors(true)

	t.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t.table, 0, 1, true).
		AddItem(t.stat, statusBarHeight, 0, false)
	t.statusVisible = true

	t.table.SetSelectedFunc(func(int, int) { t.openPreviewOverlay() })
	t.table.SetInputCapture(t.keyCapture)

	t.refresh()
	return t
}

// refresh re-fetches and repopulates every cell. Header row is row 0.
func (t *tableView) refresh() {
	ctx, cancel := context.WithTimeout(t.app.ctx, 5*time.Second)
	defer cancel()

	opts := ListOpts{
		Filter:    t.filter,
		Filters:   t.colFilters,
		Sort:      t.sortStack,
		SortField: t.sortField,
		SortDir:   t.sortDir,
		Offset:    t.page * t.pageSize,
		Limit:     t.pageSize,
		Scope:     t.app.Scope(),
	}
	rows, total, err := t.spec.fetch(ctx, opts)
	if err != nil {
		t.table.Clear()
		t.table.SetCell(0, 0, tview.NewTableCell("[red]error: "+err.Error()).SetTextColor(tcell.ColorRed))
		t.updateStatus(err.Error())
		return
	}
	// Master-detail: when this table is the detail pane, restrict to
	// the parent's child IDs (in-memory, like browser.idFilter).
	if t.idFilter != nil {
		filtered := rows[:0:0]
		for _, r := range rows {
			if t.idFilter[r.ID] {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
		total = len(rows)
	}

	// Snapshot cursor + scroll position so a refresh (sort / filter /
	// page change) doesn't yank focus back to column 0 / row 1 — the
	// user's column focus is a meaningful piece of UI state (sort/filter
	// operate on it). Clamped after repopulating in case the new rowset
	// is shorter or visible columns changed.
	selRow, selCol := t.table.GetSelection()
	rowOff, colOff := t.table.GetOffset()

	t.rows = rows
	t.total = total
	t.table.Clear()

	// Header row. NotSelectable + the table-wide SetFixed(1, 0) means the
	// header never accepts focus; ↑/↓ navigation skips straight past it
	// onto data rows. Sort-stack position indicator appended when active.
	cols := t.visibleColumns()
	off := t.colOffset()
	if off == 1 {
		// Dedicated, non-selectable row-number / selection column.
		t.table.SetCell(0, 0, tview.NewTableCell("#").
			SetTextColor(tcell.ColorYellow).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false))
	}
	for c, col := range cols {
		label := col.label
		for i, k := range t.sortStack {
			if k.Field == col.key {
				dir := "↑"
				if k.Dir == Desc {
					dir = "↓"
				}
				label = fmt.Sprintf("%s %s%d", col.label, dir, i+1)
				break
			}
		}
		cell := tview.NewTableCell(label).
			SetTextColor(tcell.ColorYellow).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false).
			SetExpansion(1)
		t.table.SetCell(0, c+off, cell)
	}

	// Data rows.
	width := len(fmt.Sprintf("%d", len(rows)))
	for r, row := range rows {
		if off == 1 {
			// Row number + selection marker live here, OUT of the id
			// column. Non-selectable so the cursor skips it.
			num := fmt.Sprintf("%*d", width, r+1)
			if t.selection.has(row.ID) {
				num = "[yellow]✓[-]" + num
			}
			t.table.SetCell(r+1, 0, tview.NewTableCell(num).
				SetTextColor(tcell.ColorGray).
				SetSelectable(false))
		}
		for c, col := range cols {
			v := row.Columns[col.key]
			label := truncate(v, 60)
			// When the # column is off, the selection ✓ still needs a
			// home — prefix the first data column like before.
			if off == 0 && c == 0 && t.selection.has(row.ID) {
				label = "[yellow]✓[-] " + label
			}
			cell := tview.NewTableCell(label).SetExpansion(1)
			if col.chip != nil {
				if tone, ok := col.chip[v]; ok {
					cell.SetTextColor(tcellColor(tone))
				}
			}
			t.table.SetCell(r+1, c+off, cell)
		}
	}

	// Restore cursor + scroll. Clamp row to the new dataset; clamp col to
	// the new visible column count (a Phase G column-hide could've
	// shortened it). Fall back to (1, 0) when there was no prior
	// selection (initial mount).
	if len(rows) > 0 {
		if selRow < 1 {
			selRow = 1
		}
		if selRow > len(rows) {
			selRow = len(rows)
		}
		// Valid data columns live in [off, off+len(cols)-1]; column 0
		// is the non-selectable # column when on.
		if selCol < off {
			selCol = off
		}
		if selCol >= off+len(cols) {
			selCol = off + len(cols) - 1
		}
		t.table.Select(selRow, selCol)
		t.table.SetOffset(rowOff, colOff)
	}
	t.updateStatus("")
}

// keyCapture handles table-specific shortcuts. Returns nil if eaten.
func (t *tableView) keyCapture(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Rune() {
	case 'v':
		// Toggle back to list+preview browser.
		t.app.swapToBrowser(t.spec)
		return nil
	case '/':
		t.openFilter()
		return nil
	case 's':
		t.cycleSortOnFocused()
		return nil
	case 'S':
		// Open the multi-sort modal (Phase D).
		t.openSortModal()
		return nil
	case 'f':
		// Open the condition-builder modal (Phase F).
		t.openConditionBuilder()
		return nil
	case 'c':
		// Open the column show/hide modal (Phase G).
		t.openColumnsModal()
		return nil
	case 'r':
		t.refresh()
		return nil
	case 'i':
		t.app.openKindInfo(t.spec)
		return nil
	case 'm':
		// Open the two-pane master-detail split (works from table view
		// too — no need to `v` back to list first).
		if len(t.spec.detailEdges) == 0 {
			t.updateStatus("no detail edge — add enttui.DetailEdge{Edge:\"<edge>\"} to the schema")
			return nil
		}
		t.app.pushMasterDetail(t.spec)
		return nil
	case '#':
		// Capture the focused DATA column before the offset changes so
		// the cursor stays on the same column after toggling (otherwise
		// hiding the # column shifted the cursor one column right).
		dataCol := t.focusedDataCol()
		selRow, _ := t.table.GetSelection()
		t.showRowNum = !t.showRowNum
		t.refresh()
		if selRow >= 1 {
			t.table.Select(selRow, dataCol+t.colOffset())
		}
		return nil
	case ':':
		_, col := t.table.GetSelection()
		openGotoRow(t.app, len(t.rows), func(idx int) {
			t.table.Select(idx+1, col) // +1: row 0 is the header
		})
		return nil
	case ' ':
		if !t.spec.allowBulkCopy {
			t.updateStatus("selection not enabled — add enttui.AllowBulkCopy{} to the schema")
			return nil
		}
		row, _ := t.table.GetSelection()
		if row >= 1 && row-1 < len(t.rows) {
			t.selection.toggle(t.rows[row-1].ID)
			t.refresh()
		}
		return nil
	case 'V':
		if !t.spec.allowBulkCopy {
			t.updateStatus("selection not enabled — add enttui.AllowBulkCopy{} to the schema")
			return nil
		}
		rsel, _ := t.table.GetSelection()
		cur := rsel - 1 // data-row index (row 0 = header)
		if cur < 0 {
			return nil
		}
		if t.selAnchor < 0 {
			t.selAnchor = cur
			t.updateStatus("range anchor set — move to the other end, press V again")
		} else {
			n, sel := t.selection.toggleRange(t.rows, t.selAnchor, cur)
			t.selAnchor = -1
			t.refresh()
			verb := "deselected"
			if sel {
				verb = "selected"
			}
			t.updateStatus(fmt.Sprintf("%s %d rows in range", verb, n))
		}
		return nil
	case '*':
		if t.spec.allowBulkCopy {
			t.selection.addAll(t.rows)
			t.refresh()
		}
		return nil
	case '0':
		if t.spec.allowBulkCopy {
			t.selection.clear()
			t.selAnchor = -1
			t.refresh()
		}
		return nil
	case 'y':
		if t.spec.allowBulkCopy && t.selection.count() > 0 {
			t.openBulkCopy()
			return nil
		}
		t.copyFocusedCell()
		return nil
	case 'Y':
		t.copyFocusedRow()
		return nil
	case 'X':
		if !t.spec.allowExport {
			t.updateStatus("export not enabled — add enttui.AllowExport{} to the schema")
			return nil
		}
		t.openExport()
		return nil
	case 'J':
		t.copyFocusedRowJSON()
		return nil
	case 'e':
		row, _ := t.table.GetSelection()
		if row >= 1 && row-1 < len(t.rows) {
			openEditForm(t.app, t.spec, t.rows[row-1], t.updateStatus, t.refresh)
		}
		return nil
	case 'N':
		openCreateForm(t.app, t.spec, t.updateStatus, t.refresh)
		return nil
	case 'D':
		row, _ := t.table.GetSelection()
		if row >= 1 && row-1 < len(t.rows) {
			r := t.rows[row-1]
			openDeleteConfirm(t.app, t.spec, r, t.updateStatus, t.refresh)
		}
		return nil
	case 'n':
		if (t.page+1)*t.pageSize < t.total {
			t.page++
			t.refresh()
		}
		return nil
	case 'p':
		if t.page > 0 {
			t.page--
			t.refresh()
		}
		return nil
	case 'g':
		if t.page != 0 {
			t.page = 0
			t.refresh()
		}
		return nil
	case 'G':
		if t.pageSize > 0 && t.total > 0 {
			t.page = (t.total - 1) / t.pageSize
			t.refresh()
		}
		return nil
	case '+', '=':
		t.pageSize = nextPageSize(t.pageSize, +1)
		t.page = 0
		t.refresh()
		return nil
	case '-', '_':
		t.pageSize = nextPageSize(t.pageSize, -1)
		t.page = 0
		t.refresh()
		return nil
	}
	switch ev.Key() {
	case tcell.KeyCtrlU:
		// Clear ALL filtering — substring + condition-builder.
		if t.filter != "" || len(t.colFilters) > 0 {
			t.filter = ""
			t.colFilters = nil
			t.page = 0
			t.refresh()
			t.updateStatus("filters cleared")
		}
		return nil
	case tcell.KeyEnter:
		// Enter always opens the preview overlay. To drill into an
		// edge, press its single-char trigger (shown in the preview
		// footer). No magic "primary drill" — every edge is explicit.
		t.openPreviewOverlay()
		return nil
	}
	return ev
}

// openPreviewOverlay shows the existing preview template as a centered
// Pages modal. esc pops it. Edges + scroll all keep working.
func (t *tableView) openPreviewOverlay() {
	row, col := t.table.GetSelection()
	if row < 1 || row-1 >= len(t.rows) { // row 0 = header
		return
	}
	_ = col
	r := t.rows[row-1]

	// Build the same previewData the browser uses.
	data := previewData{}
	addField := func(k, v string) {
		if v == "" {
			return
		}
		data.Fields = append(data.Fields, previewField{Key: k, Value: v})
	}
	for _, c := range t.spec.columns {
		v := r.Columns[c.key]
		if v == "" {
			continue
		}
		if c.key != "" && c.key == t.spec.bodyKey {
			data.Body = v
			continue
		}
		if c.chip != nil {
			v = colorChip(c.chip, v)
		}
		addField(c.label, v)
	}
	// Same edge-count logic as browser.refreshPreview — see comment there.
	for _, e := range t.spec.edges {
		pe := previewEdge{Trigger: e.trigger, Display: e.display}
		if t.spec.showEdgeCounts && e.count != nil {
			ctx, cancel := context.WithTimeout(t.app.ctx, 2*time.Second)
			n, err := e.count(ctx, r.ID)
			cancel()
			if err == nil {
				pe.Count = fmt.Sprintf("%d", n)
			}
		}
		data.Edges = append(data.Edges, pe)
	}

	body := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	body.SetText(renderPreview(data))
	body.SetBorder(true).
		SetTitle(" " + rowLabel(r, t.spec.labelKey) + " ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorOrange)

	// Trigger edges directly from inside the overlay too.
	body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			t.app.pages.RemovePage("preview-overlay")
			t.app.tv.SetFocus(t.table)
			return nil
		}
		if rn := ev.Rune(); rn != 0 {
			for _, e := range t.spec.edges {
				if e.trigger == string(rn) {
					t.app.pages.RemovePage("preview-overlay")
					t.followEdge(e, r.ID)
					return nil
				}
			}
		}
		return ev
	})

	// Center the modal: ~70% wide, ~80% tall.
	modal := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(
			tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(body, 0, 8, true).
				AddItem(nil, 0, 1, false),
			0, 7, true,
		).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage("preview-overlay", modal, true, true)
	t.app.tv.SetFocus(body)
}

// followEdge mirrors browser.followEdge — runs the typed resolver and
// pushes a new page on success.
func (t *tableView) followEdge(e anyEdge, rowID string) {
	ctx, cancel := context.WithTimeout(t.app.ctx, 5*time.Second)
	defer cancel()
	switch e.kind {
	case EdgeUpward:
		if e.resolveUpward == nil {
			return
		}
		ref, err := e.resolveUpward(ctx, rowID)
		if err != nil {
			t.updateStatus("edge: " + err.Error())
			return
		}
		t.app.pushBrowser(ref.Kind, ref.ID)
	case EdgeDrill:
		if e.resolveDrill == nil {
			return
		}
		refs, err := e.resolveDrill(ctx, rowID)
		if err != nil {
			t.updateStatus("edge: " + err.Error())
			return
		}
		t.app.pushBrowserList(refs)
	}
}

// openFilter mirrors browser.openFilter — same UX, opens a Pages overlay
// with an input field.
func (t *tableView) openFilter() {
	input := tview.NewInputField().
		SetLabel("/ ").
		SetText(t.filter).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldWidth(40)

	close := func() {
		t.app.pages.RemovePage("filter")
		t.app.tv.SetFocus(t.table)
	}
	input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			t.filter = input.GetText()
			close()
			t.refresh()
		case tcell.KeyEscape:
			close()
		}
	})

	frame := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true)
	frame.SetBorder(true).SetTitle(" filter (esc to cancel) ")

	modal := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(
			tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(frame, 3, 0, true).
				AddItem(nil, 0, 1, false),
			50, 0, true,
		).
		AddItem(nil, 0, 1, false)
	t.app.pages.AddPage("filter", modal, true, true)
	t.app.tv.SetFocus(input)
}

// cycleSort flips the existing sort direction (Phase A: single column).
// Phase D will extend this to a stack across columns.
func (t *tableView) cycleSort() {
	if t.sortDir == Asc {
		t.sortDir = Desc
	} else {
		t.sortDir = Asc
	}
	t.refresh()
}

// updateStatus paints the bottom status bar. Re-uses the same template
// as the browser so the two modes feel consistent.
func (t *tableView) updateStatus(msg string) {
	// Phase D — multi-sort stack supersedes the single-key fields.
	sortLabel := t.sortField
	dir := "↑"
	if t.sortDir == Desc {
		dir = "↓"
	}
	if len(t.sortStack) > 0 {
		sortLabel = formatSortStack(t.sortStack)
		dir = "" // already encoded into sortLabel
	}

	page := 0
	pages := 0
	if t.pageSize > 0 && t.total > 0 {
		pages = (t.total + t.pageSize - 1) / t.pageSize
		page = t.page + 1
	}

	visibleRows := len(t.rows)
	t.stat.SetText(renderStatus(statusData{
		Display:   t.spec.display + " (table)",
		Count:     fmt.Sprintf("%d/%d", visibleRows, t.total),
		SortField: sortLabel,
		SortDir:   dir,
		Filter:    t.filter,
		Error:     msg,
		Page:      page,
		Pages:     pages,
		PageSize:  t.pageSize,
		CanEdit:     t.spec.update != nil && len(t.spec.formFields) > 0,
		CanCreate:   t.spec.create != nil && len(t.spec.formFields) > 0,
		CanDelete:   t.spec.deleteRow != nil,
		CanBulkCopy: t.spec.allowBulkCopy,
		CanExport:   t.spec.allowExport,
		SelCount:    t.selection.count(),
	}))
}

// visibleColumns returns the subset of spec.columns we want shown in the
// table — drops hidden + the body column (body lives in preview overlay).
func visibleColumns(spec *anySpec) []anyColumn {
	out := make([]anyColumn, 0, len(spec.columns))
	for _, c := range spec.columns {
		if c.hidden || c.key == "body" {
			continue
		}
		out = append(out, c)
	}
	return out
}

// tcellColor maps a tone string into a tcell.Color for direct cell coloring.
// (browser.colorChip wraps text in tview tags; table cells use SetTextColor.)
func tcellColor(tone string) tcell.Color {
	switch tone {
	case "success":
		return tcell.ColorGreen
	case "warn":
		return tcell.ColorOrange
	case "danger":
		return tcell.ColorRed
	case "info":
		return tcell.ColorDodgerBlue
	case "muted":
		return tcell.ColorGray
	}
	return tcell.ColorWhite
}

// truncate cuts s to n runes, appending an ellipsis if shortened.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// host builds a *modalHost wired to this tableView's live state so the
// shared filter / sort / columns modals can mutate it directly.
func (t *tableView) host() *modalHost {
	return &modalHost{
		app:               t.app,
		specColumns:       t.spec.columns,
		filterableColumns: t.filterableColumns(),
		filtersPtr:        &t.colFilters,
		sortStackPtr:      &t.sortStack,
		overridesPtr:      &t.columnOverrides,
		refresh:           t.refresh,
		resetPage:         func() { t.page = 0 },
		updateStatus:      t.updateStatus,
	}
}

// openBulkCopy runs the format chooser against the table's selection.
// The focused column is offered as a per-column variant, so users can
// copy just the one cell value across every selected row.
func (t *tableView) openBulkCopy() {
	rows := t.selection.filteredRows(t.rows)
	if len(rows) == 0 {
		return
	}
	cols := t.visibleColumns()
	focused := ""
	if i := t.focusedDataCol(); i < len(cols) {
		focused = cols[i].key
	}
	openFormatChooser(t.app, focused, func(choice formatChoice) {
		text, err := formatRows(rows, cols, focused, choice)
		if err != nil {
			t.updateStatus("format failed: " + err.Error())
			return
		}
		copyToClipboard(t.host(), text, fmt.Sprintf("%d rows as %s", len(rows), formatLabel(choice)))
	})
}

// openExport re-fetches every row matching the current filter+sort and
// copies as JSON or CSV.
func (t *tableView) openExport() {
	opts := ListOpts{
		Filter:    t.filter,
		Filters:   t.colFilters,
		Sort:      t.sortStack,
		SortField: t.sortField,
		SortDir:   t.sortDir,
		Scope:     t.app.Scope(),
	}
	runExport(t.host(), t.spec.fetch, opts, t.visibleColumns(), t.selection.filteredRows(t.rows))
}
