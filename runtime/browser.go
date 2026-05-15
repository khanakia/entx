package runtime

// browser.go — one tview Page rendering a single entity kind.
//
// Layout (top-down):
//
//   ┌──────────────────────────────────────────────┐
//   │ ┌─list pane─┐ ┌─preview pane──────────────┐  │   bodyRow (flex column)
//   │ │           │ │                           │  │
//   │ │           │ │                           │  │
//   │ └───────────┘ └───────────────────────────┘  │
//   │ status bar (1 line, tview.TextView)          │
//   └──────────────────────────────────────────────┘
//
// Three input states the browser can be in:
//   - list focused   → arrow keys nav list, edge triggers follow edges
//   - preview focused → arrow keys scroll the textview
//   - filter modal open → input field on a separate Pages overlay
//
// All Fetch calls go through spec.fetch (typed Service.List closure
// captured at codegen time). The browser itself is schema-agnostic.

import (
	"context"
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// browser is one Page in the tview Pages stack. Holds widget references,
// fetch state, and view modifiers (filter / sort / id-filter for drill).
type browser struct {
	app  *App     // back-pointer for pushBrowser / pushBrowserList / closePicker
	spec *anySpec // type-erased entity spec — fetch + columns + edges
	root *tview.Flex
	list *tview.List     // left pane: row labels
	prev *tview.TextView // right pane: meta + body + edge footer
	stat *tview.TextView // bottom 1-line status

	// input is reserved for a (currently unused) inline filter — the
	// /-key filter actually opens a separate Pages overlay so kept here
	// only for future inline mode.
	input *tview.InputField

	// rows mirrors the most recent fetch — projectedRow values, not the
	// underlying *ent.T (those never escape the spec's typed closures).
	rows   []Row
	total  int // unfiltered total count from spec.fetch
	offset int // pagination offset (always 0 in v1 — paging deferred)

	filter    string
	sortField string
	sortDir   SortDir

	// Pagination state (Phase B). pageSize defaults to spec.pageSize and
	// can be cycled via +/-. page is 0-indexed internally but rendered
	// 1-indexed in the status bar.
	page     int
	pageSize int

	idFilter map[string]bool // non-nil = restrict to this set (drill mode)

	// Opaque cargo from the table view that the browser doesn't itself use
	// but must preserve so a table→list→table round-trip keeps the user's
	// per-column filters, multi-sort stack, and column visibility map.
	// Set by applyState, returned verbatim by state().
	carriedFilters         []FilterCondition
	carriedSortStack       []SortKey
	carriedColumnOverrides map[string]bool

	// Row-selection set (driven by space/a/c, consumed by `y` bulk
	// copy). Always allocated; empty when the user hasn't marked
	// anything. Independent of view toggles — selection IS view-local.
	selection *selectionSet
	// selAnchor is the V-range anchor row index; -1 = unset.
	selAnchor int
}

// newBrowser builds the widget tree for one entity kind. It does NOT add
// itself to the Pages stack — the caller (App.pushBrowser / pushBrowserList)
// does that. The newly-constructed browser is already refreshed once and
// has the list pane focused.
// state captures the current view state for handoff to the table view
// (the `v` toggle preserves filter / sort / page / selection across modes).
func (b *browser) state() viewState {
	id := ""
	if idx := b.list.GetCurrentItem(); idx >= 0 && idx < len(b.rows) {
		id = b.rows[idx].ID
	}
	return viewState{
		Filter:          b.filter,
		Filters:         append([]FilterCondition(nil), b.carriedFilters...),
		SortField:       b.sortField,
		SortDir:         b.sortDir,
		SortStack:       append([]SortKey(nil), b.carriedSortStack...),
		Page:            b.page,
		PageSize:        b.pageSize,
		SelectedID:      id,
		ColumnOverrides: cloneStringBoolMap(b.carriedColumnOverrides),
	}
}

// host builds a *modalHost referencing this browser's carried state.
// "A view is just a layout" — the same condition builder / sort / column
// modals run here, and any mutations they make to carriedFilters /
// carriedSortStack / carriedColumnOverrides become visible immediately
// via b.refresh().
func (b *browser) host() *modalHost {
	filterable := make([]anyColumn, 0, len(b.spec.columns))
	for _, c := range b.spec.columns {
		if c.filterable {
			filterable = append(filterable, c)
		}
	}
	return &modalHost{
		app:               b.app,
		specColumns:       b.spec.columns,
		filterableColumns: filterable,
		filtersPtr:        &b.carriedFilters,
		sortStackPtr:      &b.carriedSortStack,
		overridesPtr:      &b.carriedColumnOverrides,
		refresh:           b.refresh,
		resetPage:         func() { b.page = 0 },
		updateStatus:      b.updateStatus,
	}
}

// cloneStringBoolMap returns a copy of m, or nil if m is nil. Used to
// pass column visibility through the browser opaque-cargo path without
// risk of two views aliasing the same map.
func cloneStringBoolMap(m map[string]bool) map[string]bool {
	if m == nil {
		return nil
	}
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// applyState seeds this browser from a previous view's state. Refresh
// runs once at the end; SelectedID is honored if the row exists.
func (b *browser) applyState(s viewState) {
	if s.Filter != "" {
		b.filter = s.Filter
	}
	if s.SortField != "" {
		b.sortField = s.SortField
		b.sortDir = s.SortDir
	}
	if s.PageSize > 0 {
		b.pageSize = s.PageSize
	}
	b.page = s.Page
	// Stash the table-only fields so a table→list→table round-trip
	// doesn't lose them. The browser doesn't render with these, but
	// state() returns them verbatim on the way back.
	b.carriedFilters = append([]FilterCondition(nil), s.Filters...)
	b.carriedSortStack = append([]SortKey(nil), s.SortStack...)
	b.carriedColumnOverrides = cloneStringBoolMap(s.ColumnOverrides)
	b.refresh()
	if s.SelectedID != "" {
		b.focusID(s.SelectedID)
	}
}

// pageSizesCycle is the set of page-size values + / - cycles through.
// Annotation-driven custom sizes will land in Phase C.
var pageSizesCycle = []int{10, 20, 50, 100, 200, 500, 1000}

// nextPageSize advances cur to the next (or previous, if dir==-1) value
// in pageSizesCycle. Snaps to the nearest cycle member if cur isn't in the
// list — handles entities with annotated PageSize values outside the cycle.
func nextPageSize(cur, dir int) int {
	idx := 0
	for i, v := range pageSizesCycle {
		if v == cur {
			idx = i
			break
		}
		if v > cur {
			idx = i
			break
		}
	}
	idx += dir
	if idx < 0 {
		idx = 0
	}
	if idx >= len(pageSizesCycle) {
		idx = len(pageSizesCycle) - 1
	}
	return pageSizesCycle[idx]
}

func newBrowser(app *App, spec *anySpec) *browser {
	ps := spec.pageSize
	if ps <= 0 {
		ps = 200
	}
	b := &browser{
		app:       app,
		spec:      spec,
		selection: newSelection(),
		selAnchor: -1,
		sortField: spec.defaultView.SortField,
		sortDir:   spec.defaultView.SortDir,
		pageSize:  ps,
	}

	b.list = tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDodgerBlue).
		SetSelectedTextColor(tcell.ColorBlack)
	b.list.SetBorder(true).
		SetTitle(" " + spec.display + " ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	b.prev = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	b.prev.SetBorder(true).
		SetTitle(" preview ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	b.stat = tview.NewTextView().
		SetDynamicColors(true)

	b.input = tview.NewInputField().
		SetLabel("/ ").
		SetFieldBackgroundColor(tcell.ColorDefault)

	listPane := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(b.list, 0, 1, true)

	bodyRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(listPane, 0, 2, true).
		AddItem(b.prev, 0, 3, false)

	b.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(bodyRow, 0, 1, true).
		AddItem(b.stat, 1, 0, false)

	b.list.SetChangedFunc(func(int, string, string, rune) { b.refreshPreview() })
	b.list.SetInputCapture(b.listKeyCapture)
	b.prev.SetInputCapture(b.previewKeyCapture)

	b.refresh()
	// Start with list focused — paint its border orange to show focus.
	b.list.SetBorderColor(tcell.ColorOrange)
	b.list.SetTitleColor(tcell.ColorOrange)
	return b
}

// refresh re-fetches and repopulates the list.
func (b *browser) refresh() {
	ctx, cancel := context.WithTimeout(b.app.ctx, 5*time.Second)
	defer cancel()

	// A view is just a layout — filter / sort / column visibility carry
	// over from any other view the user passed through. The browser
	// itself doesn't (yet) expose UI to edit the per-column filter list
	// or multi-sort stack, but it does APPLY them so toggling to table,
	// adding filters, toggling back keeps the dataset narrowed.
	opts := ListOpts{
		Filter:    b.filter,
		Filters:   b.carriedFilters,
		Sort:      b.carriedSortStack,
		SortField: b.sortField,
		SortDir:   b.sortDir,
		Offset:    b.page * b.pageSize,
		Limit:     b.pageSize,
		Scope:     b.app.Scope(),
	}
	rows, total, err := b.spec.fetch(ctx, opts)
	if err != nil {
		b.list.Clear()
		b.list.AddItem("[red]error: "+err.Error(), "", 0, nil)
		b.updateStatus(err.Error())
		return
	}

	if b.idFilter != nil {
		filtered := rows[:0:0]
		for _, r := range rows {
			if b.idFilter[r.ID] {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
		total = len(rows)
	}

	b.rows = rows
	b.total = total

	b.list.Clear()
	for _, r := range rows {
		// Generic label: use whichever common-name column the spec
		// happens to have. None of these are required — schemas with
		// none fall back to the id, which always exists.
		label := rowLabel(r)
		if b.selection.has(r.ID) {
			label = "[yellow]✓[-] " + label
		}
		b.list.AddItem(label, "", 0, nil)
	}
	if len(rows) == 0 {
		b.prev.SetText("[gray](no items)")
	} else {
		b.list.SetCurrentItem(0)
		b.refreshPreview()
	}
	b.updateStatus("")
}

// refreshPreview updates the right pane for the currently selected row.
func (b *browser) refreshPreview() {
	if len(b.rows) == 0 {
		b.prev.SetText("")
		return
	}
	idx := b.list.GetCurrentItem()
	if idx < 0 || idx >= len(b.rows) {
		return
	}
	r := b.rows[idx]

	data := previewData{}

	addField := func(k, v string) {
		if v == "" {
			return
		}
		data.Fields = append(data.Fields, previewField{Key: k, Value: v})
	}
	// Generic preview: walk every column the spec declares, render each
	// non-empty value as a field. Body-shaped columns (long-prose by
	// convention) get rendered as the body block instead of a one-liner
	// field. No assumption about which fields exist.
	for _, c := range b.spec.columns {
		v := r.Columns[c.key]
		if v == "" {
			continue
		}
		if isBodyColumnKey(c.key) {
			data.Body = v
			continue
		}
		if c.chip != nil {
			v = colorChip(c.chip, v)
		}
		addField(c.label, v)
	}

	// When the spec was annotated with enttui.CountEdges(), fire each
	// edge's Count closure for the current row and embed the result in
	// the preview footer. One short-timeout context per edge — on local
	// SQLite this is microseconds per call.
	for _, e := range b.spec.edges {
		pe := previewEdge{Trigger: e.trigger, Display: e.display}
		if b.spec.showEdgeCounts && e.count != nil {
			ctx, cancel := context.WithTimeout(b.app.ctx, 2*time.Second)
			n, err := e.count(ctx, r.ID)
			cancel()
			if err == nil {
				pe.Count = fmt.Sprintf("%d", n)
			}
		}
		data.Edges = append(data.Edges, pe)
	}

	b.prev.SetText(renderPreview(data))
	b.prev.ScrollToBeginning()
}

func (b *browser) listKeyCapture(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Rune() {
	case '/':
		b.openFilter()
		return nil
	case 's':
		b.cycleSort()
		return nil
	case 'r':
		b.refresh()
		return nil
	case 'i':
		// Capabilities card for THIS view.
		b.app.openKindInfo(b.spec)
		return nil
	case 'f':
		// Same condition builder the table view opens — operates on the
		// shared carried-filters state, so adding a condition here is
		// immediately reflected in the table view too.
		openConditionBuilder(b.host())
		return nil
	case 'S':
		openSortModal(b.host())
		return nil
	case 'c':
		openColumnsModal(b.host())
		return nil
	case ' ':
		// Toggle selection on the focused row. AllowBulkCopy gates this
		// so read-only library users don't trip over an unexpected key.
		if !b.spec.allowBulkCopy {
			b.updateStatus("selection not enabled — add enttui.AllowBulkCopy{} to the schema")
			return nil
		}
		if idx := b.list.GetCurrentItem(); idx >= 0 && idx < len(b.rows) {
			b.selection.toggle(b.rows[idx].ID)
			b.refreshDisplayOnly()
		}
		return nil
	case 'V':
		// Range select. First V drops an anchor at the cursor; move
		// to the far end; second V marks the whole span. Saves
		// space-tapping N times for 1..N.
		if !b.spec.allowBulkCopy {
			b.updateStatus("selection not enabled — add enttui.AllowBulkCopy{} to the schema")
			return nil
		}
		cur := b.list.GetCurrentItem()
		if b.selAnchor < 0 {
			b.selAnchor = cur
			b.updateStatus("range anchor set — move to the other end, press V again")
		} else {
			n, sel := b.selection.toggleRange(b.rows, b.selAnchor, cur)
			b.selAnchor = -1
			b.refreshDisplayOnly()
			verb := "deselected"
			if sel {
				verb = "selected"
			}
			b.updateStatus(fmt.Sprintf("%s %d rows in range", verb, n))
		}
		return nil
	case '*':
		if b.spec.allowBulkCopy {
			b.selection.addAll(b.rows)
			b.refreshDisplayOnly()
		}
		return nil
	case '0':
		if b.spec.allowBulkCopy {
			b.selection.clear()
			b.selAnchor = -1
			b.refreshDisplayOnly()
		}
		return nil
	case 'y':
		// Multi-row branch when selection is non-empty.
		if b.spec.allowBulkCopy && b.selection.count() > 0 {
			b.openBulkCopy("")
			return nil
		}
		b.copyFocusedID()
		return nil
	case 'Y':
		b.copyFocusedRow()
		return nil
	case 'X':
		// Full export — re-fetch all filtered/sorted rows and copy.
		if !b.spec.allowExport {
			b.updateStatus("export not enabled — add enttui.AllowExport{} to the schema")
			return nil
		}
		b.openExport()
		return nil
	case 'J':
		b.copyFocusedRowJSON()
		return nil
	case 'e':
		// Edit current row. If the spec has no Editable() fields, the
		// form surfaces a "no editable fields" hint in the status bar.
		// Refresh is wired as onSaved — runs only after the save
		// completes, so the row reflects the new values immediately.
		if idx := b.list.GetCurrentItem(); idx >= 0 && idx < len(b.rows) {
			openEditForm(b.app, b.spec, b.rows[idx], b.updateStatus, b.refresh)
		}
		return nil
	case 'N':
		// New row. Capital N because lowercase n is the next-page key.
		// Scope keys (e.g. project_id) pre-injected via app.SetScope().
		openCreateForm(b.app, b.spec, b.updateStatus, b.refresh)
		return nil
	case 'D':
		if idx := b.list.GetCurrentItem(); idx >= 0 && idx < len(b.rows) {
			row := b.rows[idx]
			openDeleteConfirm(b.app, b.spec, row, b.updateStatus, b.refresh)
		}
		return nil
	case 'v':
		// Phase A: swap this page to a table view of the same spec.
		// Filter / sort state is intentionally NOT carried across — keeps
		// the toggle stateless. Phase D can preserve state.
		b.app.swapToTable(b.spec)
		return nil
	case 'n':
		// Phase B: next page (clamped).
		if (b.page+1)*b.pageSize < b.total {
			b.page++
			b.refresh()
		}
		return nil
	case 'p':
		// Phase B: previous page (clamped).
		if b.page > 0 {
			b.page--
			b.refresh()
		}
		return nil
	case 'G':
		// Phase B: jump to last page.
		if b.pageSize > 0 && b.total > 0 {
			b.page = (b.total - 1) / b.pageSize
			b.refresh()
		}
		return nil
	case 'g':
		// Phase B: jump to first page.
		if b.page != 0 {
			b.page = 0
			b.refresh()
		}
		return nil
	case '+', '=':
		// Phase B: bump to next page size in the cycle.
		b.pageSize = nextPageSize(b.pageSize, +1)
		b.page = 0
		b.refresh()
		return nil
	case '-', '_':
		// Phase B: drop to previous page size in the cycle.
		b.pageSize = nextPageSize(b.pageSize, -1)
		b.page = 0
		b.refresh()
		return nil
	}
	switch ev.Key() {
	case tcell.KeyCtrlU:
		// Clear active filter (ctrl+u — matches readline "kill to start").
		if b.filter != "" {
			b.filter = ""
			b.refresh()
		}
		return nil
	case tcell.KeyTab, tcell.KeyRight:
		b.focusPreview()
		return nil
	case tcell.KeyEnter:
		b.activateEdgeOrPreview()
		return nil
	}
	// Edge triggers (single-char letters only — enter is reserved for
	// preview focus, never auto-bound to a "primary" drill edge).
	if r := ev.Rune(); r != 0 {
		for _, e := range b.spec.edges {
			if e.trigger == string(r) {
				b.followEdge(e)
				return nil
			}
		}
	}
	return ev
}

// previewKeyCapture lets the user return focus to the list via Tab/←/h.
func (b *browser) previewKeyCapture(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyTab, tcell.KeyLeft, tcell.KeyBacktab:
		b.focusList()
		return nil
	}
	if ev.Rune() == 'h' {
		b.focusList()
		return nil
	}
	return ev
}

func (b *browser) focusPreview() {
	b.app.tv.SetFocus(b.prev)
	b.list.SetBorderColor(tcell.ColorDodgerBlue)
	b.list.SetTitleColor(tcell.ColorDodgerBlue)
	b.prev.SetBorderColor(tcell.ColorOrange)
	b.prev.SetTitleColor(tcell.ColorOrange)
}

func (b *browser) focusList() {
	b.app.tv.SetFocus(b.list)
	b.prev.SetBorderColor(tcell.ColorDodgerBlue)
	b.prev.SetTitleColor(tcell.ColorYellow)
	b.list.SetBorderColor(tcell.ColorOrange)
	b.list.SetTitleColor(tcell.ColorOrange)
}

func (b *browser) activateEdgeOrPreview() {
	// Enter focuses the preview pane. To drill an edge, press its
	// single-char trigger (shown in the preview footer).
	b.focusPreview()
}

func (b *browser) followEdge(e anyEdge) {
	if len(b.rows) == 0 {
		return
	}
	r := b.rows[b.list.GetCurrentItem()]
	ctx, cancel := context.WithTimeout(b.app.ctx, 5*time.Second)
	defer cancel()
	switch e.kind {
	case EdgeUpward:
		if e.resolveUpward == nil {
			return
		}
		ref, err := e.resolveUpward(ctx, r.ID)
		if err != nil {
			b.updateStatus("edge error: " + err.Error())
			return
		}
		b.app.pushBrowser(ref.Kind, ref.ID)
	case EdgeDrill:
		if e.resolveDrill == nil {
			return
		}
		refs, err := e.resolveDrill(ctx, r.ID)
		if err != nil {
			b.updateStatus("edge error: " + err.Error())
			return
		}
		b.app.pushBrowserList(refs)
	}
}

func (b *browser) openFilter() {
	input := tview.NewInputField().
		SetLabel("/ ").
		SetText(b.filter).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldWidth(40)

	close := func() {
		b.app.pages.RemovePage("filter")
		b.app.tv.SetFocus(b.list)
	}

	input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			b.filter = input.GetText()
			close()
			b.refresh()
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

	b.app.pages.AddPage("filter", modal, true, true)
	b.app.tv.SetFocus(input)
}

func (b *browser) cycleSort() {
	// Cycle through column keys flagged Sortable (TODO: track sortable list).
	// v1: just toggle direction.
	if b.sortDir == Asc {
		b.sortDir = Desc
	} else {
		b.sortDir = Asc
	}
	b.refresh()
}

func (b *browser) updateStatus(msg string) {
	dir := "↑"
	if b.sortDir == Desc {
		dir = "↓"
	}
	pages := 0
	page := 0
	if b.pageSize > 0 && b.total > 0 {
		pages = (b.total + b.pageSize - 1) / b.pageSize
		page = b.page + 1
	}
	// Surface carried filters / sort stack in the status so the user
	// knows the table-view conditions are still active in this view.
	filter := b.filter
	if n := len(b.carriedFilters); n > 0 {
		extra := fmt.Sprintf("+%d cond", n)
		if filter == "" {
			filter = extra
		} else {
			filter = filter + " " + extra
		}
	}
	sortField := b.sortField
	if len(b.carriedSortStack) > 0 {
		sortField = formatSortStack(b.carriedSortStack)
		dir = ""
	}
	b.stat.SetText(renderStatus(statusData{
		Display:   b.spec.display,
		Count:     fmt.Sprintf("%d/%d", b.list.GetItemCount(), b.total),
		SortField: sortField,
		SortDir:   dir,
		Filter:    filter,
		Error:     msg,
		Page:      page,
		Pages:     pages,
		PageSize:  b.pageSize,
		CanEdit:     b.spec.update != nil && len(b.spec.formFields) > 0,
		CanCreate:   b.spec.create != nil && len(b.spec.formFields) > 0,
		CanDelete:   b.spec.deleteRow != nil,
		CanBulkCopy: b.spec.allowBulkCopy,
		CanExport:   b.spec.allowExport,
		SelCount:    b.selection.count(),
	}))
}

func (b *browser) setIDFilter(ids []string) {
	if len(ids) == 0 {
		b.idFilter = nil
		return
	}
	b.idFilter = make(map[string]bool, len(ids))
	for _, id := range ids {
		b.idFilter[id] = true
	}
	b.refresh()
}

// focusID moves the selection to the row with the given ID, if present.
func (b *browser) focusID(id string) {
	for i, r := range b.rows {
		if r.ID == id {
			b.list.SetCurrentItem(i)
			return
		}
	}
}

// colorChipFor renders a status string using the entity's first chip-bearing
// column matching that status.
func colorChipFor(spec *anySpec, status string) string {
	for _, c := range spec.columns {
		if c.chip == nil {
			continue
		}
		if _, ok := c.chip[status]; ok {
			return colorChip(c.chip, status)
		}
	}
	return status
}

func colorChip(chip map[string]string, value string) string {
	return fmt.Sprintf("[%s::b]%s[-:-:-]", toneColor(chip[value]), value)
}

// refreshDisplayOnly redraws the list labels without re-fetching. Used
// when toggling selection markers — the underlying rows haven't changed.
func (b *browser) refreshDisplayOnly() {
	cur := b.list.GetCurrentItem()
	b.list.Clear()
	for _, r := range b.rows {
		label := rowLabel(r)
		if b.selection.has(r.ID) {
			label = "[yellow]✓[-] " + label
		}
		b.list.AddItem(label, "", 0, nil)
	}
	if cur >= 0 && cur < len(b.rows) {
		b.list.SetCurrentItem(cur)
	}
	b.updateStatus("")
}

// openBulkCopy runs the format chooser against the current selection.
// In the browser, no column is "focused" — only the all-columns
// variants are offered. `focused` is plumbed for parity with table.
func (b *browser) openBulkCopy(focused string) {
	rows := b.selection.filteredRows(b.rows)
	if len(rows) == 0 {
		return
	}
	cols := b.visibleColumnsForExport()
	openFormatChooser(b.app, focused, func(choice formatChoice) {
		text, err := formatRows(rows, cols, focused, choice)
		if err != nil {
			b.updateStatus("format failed: " + err.Error())
			return
		}
		copyToClipboard(b.host(), text, fmt.Sprintf("%d rows as %s", len(rows), formatLabel(choice)))
	})
}

// openExport re-fetches every row matching the current filter+sort
// (capped at exportRowCap) and copies it as JSON or CSV.
func (b *browser) openExport() {
	opts := ListOpts{
		Filter:    b.filter,
		Filters:   b.carriedFilters,
		Sort:      b.carriedSortStack,
		SortField: b.sortField,
		SortDir:   b.sortDir,
		Scope:     b.app.Scope(),
	}
	runExport(b.host(), b.spec.fetch, opts, b.visibleColumnsForExport(), b.selection.filteredRows(b.rows))
}

// visibleColumnsForExport returns the columns to include in CSV/JSON
// output — every visible column on the spec.
func (b *browser) visibleColumnsForExport() []anyColumn {
	out := make([]anyColumn, 0, len(b.spec.columns))
	for _, c := range b.spec.columns {
		if c.hidden {
			continue
		}
		out = append(out, c)
	}
	return out
}
