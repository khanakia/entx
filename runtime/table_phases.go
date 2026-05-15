package runtime

// table_phases.go — implementations for Phases D/E/F/G on the table view.
//
// Phase D: multi-column sort. `s` on the focused column cycles a sort
// stack (append → desc → remove). `S` opens a modal listing the stack so
// the user can reorder or clear it.
//
// Phase E: per-column substring filter row inside the table header area.
// (The `/` overlay filter and the per-column filter compose as AND.)
//
// Phase F: condition builder modal — pick column → operator → value, add
// rows, AND-compose, apply.
//
// Phase G: column show/hide modal — checkbox list, toggle, apply.
//
// All modals operate against a `modalHost` (see definition below) so they
// can be invoked from either the table view OR the browser view — "a view
// is just a different layout, the filter/sort UI should work everywhere".

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// modalHost is the small surface every modal needs from the view that
// hosts it. Both *tableView and *browser construct one (see their host()
// methods) and pass it into the free functions below. Pointer fields
// give the modals mutable access to the host's filter / sort / overrides
// state; the function fields are callbacks back into the view.
type modalHost struct {
	app                *App
	specColumns        []anyColumn          // every column on the spec (for the show/hide modal)
	filterableColumns  []anyColumn          // subset Filterable()
	filtersPtr         *[]FilterCondition   // condition builder state
	sortStackPtr       *[]SortKey           // multi-sort stack
	overridesPtr       *map[string]bool     // column visibility overrides
	refresh            func()               // re-fetch and re-render
	resetPage          func()               // jump back to page 0 (called on filter apply)
	updateStatus       func(msg string)     // surface inline status messages
}

// --- Phase D: multi-column sort ---

// cycleSortOnFocused mutates the sort stack based on the column under the
// table cursor: append (asc) → flip (desc) → remove. If the focused
// column isn't Sortable, nothing happens.
func (t *tableView) cycleSortOnFocused() {
	_, col := t.table.GetSelection()
	cols := t.visibleColumns()
	if col < 0 || col >= len(cols) {
		return
	}
	c := cols[col]
	if !c.sortable {
		t.updateStatus("column not sortable")
		return
	}

	for i, k := range t.sortStack {
		if k.Field == c.key {
			switch k.Dir {
			case Asc:
				t.sortStack[i].Dir = Desc
			case Desc:
				t.sortStack = append(t.sortStack[:i], t.sortStack[i+1:]...)
			}
			t.refresh()
			return
		}
	}
	// Not in stack → append ascending. If MultiSort is disabled, replace.
	if !t.spec.multiSort {
		t.sortStack = []SortKey{{Field: c.key, Dir: Asc}}
	} else {
		t.sortStack = append(t.sortStack, SortKey{Field: c.key, Dir: Asc})
	}
	t.refresh()
}

// openSortModal lists the current stack with reorder shortcuts. Keys:
//   K / J     move focused entry up / down
//   d         delete focused entry
//   enter     flip direction (asc ↔ desc)
//   c         clear entire stack
//   esc       close
//
// Rebuild is local — never re-opens the modal recursively (the previous
// version stacked nested copies of itself on every keystroke).
func (t *tableView) openSortModal() { openSortModal(t.host()) }

func openSortModal(h *modalHost) {
	if len(*h.sortStackPtr) == 0 {
		h.updateStatus("sort stack empty — press s on a column first")
		return
	}

	list := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDodgerBlue).
		SetSelectedTextColor(tcell.ColorBlack)

	rebuild := func() {
		// Preserve selection across rebuilds so move-up/down feels stable.
		cur := list.GetCurrentItem()
		list.Clear()
		for i, k := range *h.sortStackPtr {
			dir := "↑"
			if k.Dir == Desc {
				dir = "↓"
			}
			label := fmt.Sprintf("%d.  [orange::b]%s[-:-:-]  %s", i+1, k.Field, dir)
			list.AddItem(label, "", 0, nil)
		}
		if cur >= list.GetItemCount() {
			cur = list.GetItemCount() - 1
		}
		if cur < 0 {
			cur = 0
		}
		list.SetCurrentItem(cur)
	}
	rebuild()

	list.SetSelectedFunc(func(idx int, _, _ string, _ rune) {
		stack := *h.sortStackPtr
		if idx < 0 || idx >= len(stack) {
			return
		}
		if stack[idx].Dir == Asc {
			stack[idx].Dir = Desc
		} else {
			stack[idx].Dir = Asc
		}
		rebuild()
		h.refresh()
	})

	moveUp := func() {
		stack := *h.sortStackPtr
		idx := list.GetCurrentItem()
		if idx > 0 && idx < len(stack) {
			stack[idx], stack[idx-1] = stack[idx-1], stack[idx]
			list.SetCurrentItem(idx - 1)
			rebuild()
			h.refresh()
		}
	}
	moveDown := func() {
		stack := *h.sortStackPtr
		idx := list.GetCurrentItem()
		if idx >= 0 && idx < len(stack)-1 {
			stack[idx], stack[idx+1] = stack[idx+1], stack[idx]
			list.SetCurrentItem(idx + 1)
			rebuild()
			h.refresh()
		}
	}

	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		idx := list.GetCurrentItem()
		switch ev.Key() {
		case tcell.KeyEscape:
			h.app.pages.RemovePage("sort-modal")
			return nil
		case tcell.KeyCtrlK:
			moveUp()
			return nil
		case tcell.KeyCtrlJ:
			moveDown()
			return nil
		}
		if ev.Key() == tcell.KeyUp && (ev.Modifiers()&(tcell.ModCtrl|tcell.ModShift)) != 0 {
			moveUp()
			return nil
		}
		if ev.Key() == tcell.KeyDown && (ev.Modifiers()&(tcell.ModCtrl|tcell.ModShift)) != 0 {
			moveDown()
			return nil
		}
		switch ev.Rune() {
		case 'K':
			moveUp()
			return nil
		case 'J':
			moveDown()
			return nil
		case 'd':
			stack := *h.sortStackPtr
			if idx >= 0 && idx < len(stack) {
				*h.sortStackPtr = append(stack[:idx], stack[idx+1:]...)
				if len(*h.sortStackPtr) == 0 {
					h.app.pages.RemovePage("sort-modal")
					h.refresh()
					return nil
				}
				rebuild()
				h.refresh()
			}
			return nil
		case 'c':
			*h.sortStackPtr = nil
			h.app.pages.RemovePage("sort-modal")
			h.refresh()
			return nil
		}
		return ev
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(tview.NewTextView().
			SetText(" ↑↓ nav · ctrl+↑/↓ or K/J move · enter flip · d delete · c clear · esc close ").
			SetTextColor(tcell.ColorGray), 1, 0, false)
	body.SetBorder(true).
		SetTitle(" sort stack ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	h.app.pages.AddPage("sort-modal", centerModal(body, 60, 20), true, true)
	h.app.tv.SetFocus(list)
}

// --- Phase F: condition builder ---

// openConditionBuilder presents a modal of (column, operator, value) rows.
// Layout: scrollable list of conditions on top + explicit Add / Apply /
// Cancel buttons below in a Form. Tab cycles between the list and the
// buttons; ctrl+s = Apply from anywhere (escape hatch).
func (t *tableView) openConditionBuilder() { openConditionBuilder(t.host()) }

func openConditionBuilder(h *modalHost) {
	cols := h.filterableColumns
	if len(cols) == 0 {
		h.updateStatus("no filterable columns")
		return
	}

	// Working copy so cancel reverts.
	work := append([]FilterCondition(nil), (*h.filtersPtr)...)

	// Conditions list (display + delete).
	list := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDodgerBlue).
		SetSelectedTextColor(tcell.ColorBlack)

	apply := func() {
		*h.filtersPtr = work
		h.resetPage()
		h.app.pages.RemovePage("condition-builder")
		h.refresh()
	}
	cancel := func() {
		h.app.pages.RemovePage("condition-builder")
	}

	var rebuild func()
	rebuild = func() {
		list.Clear()
		if len(work) == 0 {
			list.AddItem("[gray]no conditions yet — press 'a' to add[-]", "", 0, nil)
		}
		for i, c := range work {
			label := fmt.Sprintf("%d.  [orange::b]%s[-:-:-]  [aqua]%s[-]  %q", i+1, c.Field, c.Op, c.Value)
			list.AddItem(label, "", 0, nil)
		}
	}
	rebuild()

	// Enter on a condition row opens the picker chain in edit mode.
	list.SetSelectedFunc(func(idx int, _, _ string, _ rune) {
		if idx < 0 || idx >= len(work) {
			return
		}
		openColumnPicker(h, cols, &work, list, rebuild, idx)
	})

	// d = delete current condition, e = explicit edit (same as enter).
	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		idx := list.GetCurrentItem()
		switch ev.Rune() {
		case 'd':
			if idx < len(work) {
				work = append(work[:idx], work[idx+1:]...)
				rebuild()
			}
			return nil
		case 'e':
			if idx < len(work) {
				openColumnPicker(h, cols, &work, list, rebuild, idx)
			}
			return nil
		}
		return ev
	})

	// Form with explicit buttons. tview.Form gives us proper Tab nav +
	// visible button rendering, which the previous list-of-items approach
	// faked badly (apply/cancel were just text rows).
	form := tview.NewForm().
		AddButton("+ Add condition", func() {
			openColumnPicker(h, cols, &work, list, rebuild, -1)
		}).
		AddButton("Apply", apply).
		AddButton("Cancel", cancel)
	form.SetButtonsAlign(tview.AlignCenter).
		SetButtonBackgroundColor(tcell.ColorDarkSlateGray).
		SetButtonTextColor(tcell.ColorWhite)

	// Wrap the conditions list + form in a vertical flex.
	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(form, 3, 0, false).
		AddItem(tview.NewTextView().
			SetText(" a add · enter/e edit · d delete · s apply · esc cancel · tab cycle buttons ").
			SetTextColor(tcell.ColorGray), 1, 0, false)
	body.SetBorder(true).
		SetTitle(" filter — condition builder ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	// Page-wide hotkeys: ctrl+s = apply; esc = cancel; tab between regions.
	page := centerModal(body, 80, 24)
	h.app.pages.AddPage("condition-builder", page, true, true)
	h.app.tv.SetFocus(list)

	// Bind page-level hotkeys via the body flex's InputCapture so they
	// fire regardless of whether focus is on the list or the form buttons.
	addFn := func() { openColumnPicker(h, cols, &work, list, rebuild, -1) }
	body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			cancel()
			return nil
		case tcell.KeyTab:
			if h.app.tv.GetFocus() == list {
				h.app.tv.SetFocus(form)
			} else {
				h.app.tv.SetFocus(list)
			}
			return nil
		}
		switch ev.Rune() {
		case 'a', '+', 'n':
			// Direct shortcut: add a new condition without needing to
			// tab to the button. Works whether focus is on the list or
			// the buttons.
			addFn()
			return nil
		case 's':
			// Single-char Apply — faster than tabbing to the button.
			// 's' would collide with the table's "sort on focused column"
			// shortcut, but we're inside the modal so the outer table
			// never sees it.
			apply()
			return nil
		}
		return ev
	})
}

// openAddConditionRow is the column-picker sub-modal — step 1 of the
// (column → operator → value) chain. fzf-style filter input at the top
// drives the list below; pressing enter on a row advances to the
// operator picker.
//
// editIdx >= 0 puts the picker in EDIT mode: instead of appending the
// new condition to `work`, the result REPLACES `(*work)[editIdx]`.
// openColumnPicker is the shared implementation used by both add + edit
// flows. editIdx >= 0 means edit mode.
func openColumnPicker(h *modalHost, cols []anyColumn, work *[]FilterCondition, parent *tview.List, rebuild func(), editIdx int) {
	// fzf-style: input at top, filtered list below.
	input := tview.NewInputField().
		SetLabel("filter › ").
		SetLabelColor(tcell.ColorYellow).
		SetFieldWidth(40).
		SetFieldBackgroundColor(tcell.ColorDefault)

	list := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDodgerBlue).
		SetSelectedTextColor(tcell.ColorBlack)

	// `shown` is the currently-filtered subset; the SelectedFunc indexes
	// into it (not the full cols slice).
	shown := append([]anyColumn(nil), cols...)

	// Track which column should be pre-highlighted. In edit mode this is
	// the existing condition's column so the user lands on it directly.
	var preselectKey string
	if editIdx >= 0 && editIdx < len(*work) {
		preselectKey = (*work)[editIdx].Field
	}

	populate := func() {
		list.Clear()
		for _, c := range shown {
			list.AddItem(c.label, c.key, 0, nil)
		}
		if list.GetItemCount() > 0 {
			// Try to land on the preselected column (edit mode); else
			// just highlight the first row.
			start := 0
			if preselectKey != "" {
				for i, c := range shown {
					if c.key == preselectKey {
						start = i
						break
					}
				}
			}
			list.SetCurrentItem(start)
		}
	}
	populate()

	input.SetChangedFunc(func(text string) {
		q := strings.ToLower(strings.TrimSpace(text))
		if q == "" {
			shown = append([]anyColumn(nil), cols...)
		} else {
			shown = shown[:0]
			for _, c := range cols {
				hay := strings.ToLower(c.label + " " + c.key)
				if strings.Contains(hay, q) {
					shown = append(shown, c)
				}
			}
		}
		populate()
	})

	// Arrow keys from the input drive the list — fzf-like.
	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			if cur := list.GetCurrentItem() + 1; cur < list.GetItemCount() {
				list.SetCurrentItem(cur)
			}
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			if cur := list.GetCurrentItem() - 1; cur >= 0 {
				list.SetCurrentItem(cur)
			}
			return nil
		case tcell.KeyPgDn:
			list.SetCurrentItem(min(list.GetCurrentItem()+10, list.GetItemCount()-1))
			return nil
		case tcell.KeyPgUp:
			list.SetCurrentItem(max(list.GetCurrentItem()-10, 0))
			return nil
		}
		return ev
	})

	commit := func() {
		idx := list.GetCurrentItem()
		if idx < 0 || idx >= len(shown) {
			return
		}
		col := shown[idx]
		h.app.pages.RemovePage("cb-col")
		openPickOperator(h, col, work, parent, rebuild, editIdx)
	}
	cancel := func() { h.app.pages.RemovePage("cb-col") }

	input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			commit()
		case tcell.KeyEscape:
			cancel()
		}
	})
	list.SetSelectedFunc(func(int, string, string, rune) { commit() })
	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			cancel()
			return nil
		}
		return ev
	})

	title := " 1/3 column "
	if editIdx >= 0 {
		title = " edit — column (1/3) "
	}

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(list, 0, 1, false).
		AddItem(tview.NewTextView().
			SetText(" type to filter · ↑/↓ nav · enter pick · esc cancel ").
			SetTextColor(tcell.ColorGray), 1, 0, false)
	body.SetBorder(true).
		SetTitle(title).
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	h.app.pages.AddPage("cb-col", centerModal(body, 50, 22), true, true)
	h.app.tv.SetFocus(input)
}

// openPickOperator is step 2 — operator menu typed to the column kind.
// editIdx >= 0 means we're replacing (*work)[editIdx] on submit instead
// of appending. v1: a fixed operator list.
func openPickOperator(h *modalHost, col anyColumn, work *[]FilterCondition, parent *tview.List, rebuild func(), editIdx int) {
	h.app.pages.RemovePage("cb-col")

	// Operator menu is typed to the column: enum columns drop the
	// substring `contains` (meaningless for enums) and surface `in /
	// not_in` as the primary multi-select ops; string columns keep the
	// full list.
	var ops []FilterOp
	if len(col.enumValues) > 0 {
		ops = []FilterOp{OpEq, OpNeq, OpIn, OpNotIn, OpIsNull, OpNotNull}
	} else {
		ops = []FilterOp{OpEq, OpNeq, OpContains, OpLt, OpLte, OpGt, OpGte, OpIn, OpNotIn, OpIsNull, OpNotNull}
	}
	opSel := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDodgerBlue).
		SetSelectedTextColor(tcell.ColorBlack)

	for _, op := range ops {
		opSel.AddItem(string(op), "", 0, nil)
	}

	// Edit mode → preselect the operator that matches the existing
	// condition. New conditions land on OpEq (index 0).
	if editIdx >= 0 && editIdx < len(*work) {
		curOp := (*work)[editIdx].Op
		for i, op := range ops {
			if op == curOp {
				opSel.SetCurrentItem(i)
				break
			}
		}
	}

	opSel.SetSelectedFunc(func(idx int, _, _ string, _ rune) {
		if idx < 0 || idx >= len(ops) {
			return
		}
		op := ops[idx]
		if op == OpIsNull || op == OpNotNull {
			// No value needed — submit immediately.
			newCond := FilterCondition{Field: col.key, Op: op}
			if editIdx >= 0 && editIdx < len(*work) {
				(*work)[editIdx] = newCond
			} else {
				*work = append(*work, newCond)
			}
			h.app.pages.RemovePage("cb-op")
			rebuild()
			return
		}
		openEnterValue(h, col, op, work, rebuild, editIdx)
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(opSel, 0, 1, true).
		AddItem(tview.NewTextView().
			SetText(" enter pick · esc cancel ").
			SetTextColor(tcell.ColorGray), 1, 0, false)
	title := " 2/3 operator (" + col.label + ") "
	if editIdx >= 0 {
		title = " edit — operator (" + col.label + ") "
	}
	body.SetBorder(true).
		SetTitle(title).
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)
	h.app.pages.AddPage("cb-op", centerModal(body, 30, 18), true, true)
	h.app.tv.SetFocus(opSel)
	opSel.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			h.app.pages.RemovePage("cb-op")
			return nil
		}
		return ev
	})
	_ = parent
}

// openEnterValue is step 3 — text input for the value. On enter, append
// (or replace if editIdx >= 0) and dismiss the chain of modals.
//
// Enum branch: when the column has declared EnumValues the runtime
// shows a value picker instead of a free-text input. For `=` / `!=`,
// it's a single-select list (enter commits). For `in` / `not_in`, it's
// a multi-select list (space toggles, `s` applies). The selected values
// are encoded into FilterCondition.Value as a `|`-joined string — the
// generated dispatch splits on `|` and feeds the typed `<Field>In(...)`
// predicate.
func openEnterValue(h *modalHost, col anyColumn, op FilterOp, work *[]FilterCondition, rebuild func(), editIdx int) {
	h.app.pages.RemovePage("cb-op")

	if len(col.enumValues) > 0 {
		openEnumValuePicker(h, col, op, work, rebuild, editIdx)
		return
	}

	input := tview.NewInputField().
		SetLabel(col.label + " " + string(op) + " ").
		SetLabelColor(tcell.ColorYellow).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldWidth(40)

	// Edit mode → pre-populate the input with the existing value.
	if editIdx >= 0 && editIdx < len(*work) {
		input.SetText((*work)[editIdx].Value)
	}

	input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			newCond := FilterCondition{Field: col.key, Op: op, Value: input.GetText()}
			if editIdx >= 0 && editIdx < len(*work) {
				(*work)[editIdx] = newCond
			} else {
				*work = append(*work, newCond)
			}
			h.app.pages.RemovePage("cb-val")
			rebuild()
		case tcell.KeyEscape:
			h.app.pages.RemovePage("cb-val")
		}
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(tview.NewTextView().
			SetText(" enter submit · esc cancel ").
			SetTextColor(tcell.ColorGray), 1, 0, false)
	title := " 3/3 value "
	if editIdx >= 0 {
		title = " edit — value "
	}
	body.SetBorder(true).
		SetTitle(title).
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)
	h.app.pages.AddPage("cb-val", centerModal(body, 50, 6), true, true)
	h.app.tv.SetFocus(input)
}

// openEnumValuePicker is step 3 for enum columns — a picker of the
// declared values. Single-select for OpEq/OpNeq, multi-select for
// OpIn/OpNotIn. Encodes selection back into FilterCondition.Value as a
// "|"-joined string the generated dispatch knows how to split.
func openEnumValuePicker(h *modalHost, col anyColumn, op FilterOp, work *[]FilterCondition, rebuild func(), editIdx int) {
	multi := op == OpIn || op == OpNotIn

	// Pre-select from the existing value when editing.
	selected := make(map[string]bool, len(col.enumValues))
	if editIdx >= 0 && editIdx < len(*work) {
		for _, v := range strings.Split((*work)[editIdx].Value, "|") {
			if v != "" {
				selected[v] = true
			}
		}
	}

	list := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDodgerBlue).
		SetSelectedTextColor(tcell.ColorBlack)

	var rebuildList func()
	rebuildList = func() {
		cur := list.GetCurrentItem()
		list.Clear()
		for _, v := range col.enumValues {
			label := v
			if multi {
				mark := "[red]✗[-]"
				if selected[v] {
					mark = "[green]✓[-]"
				}
				label = mark + " " + v
			}
			vv := v
			list.AddItem(label, "", 0, func() {
				if multi {
					selected[vv] = !selected[vv]
					i := list.GetCurrentItem()
					rebuildList()
					list.SetCurrentItem(i)
				} else {
					// Single-select: commit immediately.
					commitEnumPicker(h, col, op, work, rebuild, editIdx, []string{vv})
				}
			})
		}
		if cur < 0 {
			cur = 0
		}
		if cur >= list.GetItemCount() {
			cur = list.GetItemCount() - 1
		}
		list.SetCurrentItem(cur)
	}
	rebuildList()

	apply := func() {
		vals := make([]string, 0, len(col.enumValues))
		for _, v := range col.enumValues {
			if selected[v] {
				vals = append(vals, v)
			}
		}
		commitEnumPicker(h, col, op, work, rebuild, editIdx, vals)
	}

	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			h.app.pages.RemovePage("cb-val")
			return nil
		case tcell.KeyCtrlS:
			if multi {
				apply()
			}
			return nil
		}
		if multi {
			switch ev.Rune() {
			case 's':
				apply()
				return nil
			case ' ':
				i := list.GetCurrentItem()
				if i >= 0 && i < len(col.enumValues) {
					v := col.enumValues[i]
					selected[v] = !selected[v]
					rebuildList()
					list.SetCurrentItem(i)
				}
				return nil
			}
		}
		return ev
	})

	hint := " enter pick · esc cancel "
	if multi {
		hint = " space toggle · s apply · esc cancel "
	}
	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(tview.NewTextView().
			SetText(hint).
			SetTextColor(tcell.ColorGray), 1, 0, false)
	title := " 3/3 value (" + col.label + " " + string(op) + ") "
	if editIdx >= 0 {
		title = " edit — value (" + col.label + " " + string(op) + ") "
	}
	body.SetBorder(true).
		SetTitle(title).
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)
	h.app.pages.AddPage("cb-val", centerModal(body, 40, min(len(col.enumValues)+4, 20)), true, true)
	h.app.tv.SetFocus(list)
}

// commitEnumPicker stores the resolved values back into work[editIdx]
// or appends a new condition, then dismisses the picker.
func commitEnumPicker(h *modalHost, col anyColumn, op FilterOp, work *[]FilterCondition, rebuild func(), editIdx int, vals []string) {
	value := strings.Join(vals, "|")
	newCond := FilterCondition{Field: col.key, Op: op, Value: value}
	if editIdx >= 0 && editIdx < len(*work) {
		(*work)[editIdx] = newCond
	} else {
		*work = append(*work, newCond)
	}
	h.app.pages.RemovePage("cb-val")
	rebuild()
}

// --- Phase G: column show/hide ---

// openColumnsModal lists every column with a checkbox indicating its
// visibility. Toggling is session-only (in-memory per kind).
func (t *tableView) openColumnsModal() { openColumnsModal(t.host()) }

func openColumnsModal(h *modalHost) {
	if *h.overridesPtr == nil {
		*h.overridesPtr = make(map[string]bool, len(h.specColumns))
		for _, c := range h.specColumns {
			(*h.overridesPtr)[c.key] = !c.hidden
		}
	}
	// Local alias for the override map (re-fetched after lazy init above).
	overrides := *h.overridesPtr

	// Selected bg/fg must be set explicitly — without them tview.List's
	// "selected" style is the same as the surrounding rows, so the user
	// can't tell the cursor is moving and it looks like arrow keys don't
	// work. Matches the sort modal + sidebar.
	list := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDodgerBlue).
		SetSelectedTextColor(tcell.ColorBlack)
	var rebuild func()
	rebuild = func() {
		list.Clear()
		for _, c := range h.specColumns {
			if c.key == "body" {
				continue
			}
			// `✓` (green) = visible, `✗` (red) = hidden. Reads faster
			// than `[x]` / `[ ]` at a glance — the red ✗ pops on the
			// row that's been turned off.
			mark := "[green]✓[-]"
			if !overrides[c.key] {
				mark = "[red]✗[-]"
			}
			cKey := c.key
			// Empty secondaryText — ShowSecondaryText(false) is set, so
			// passing c.key was unused and just noise.
			list.AddItem(mark+" "+c.label, "", 0, func() {
				overrides[cKey] = !overrides[cKey]
				cur := list.GetCurrentItem()
				rebuild()
				list.SetCurrentItem(cur)
			})
		}
		list.AddItem("[yellow]apply", "", 0, func() {
			h.app.pages.RemovePage("cols-modal")
			h.refresh()
		})
		list.AddItem("[gray]close", "", 0, func() {
			h.app.pages.RemovePage("cols-modal")
		})
	}
	rebuild()

	apply := func() {
		h.app.pages.RemovePage("cols-modal")
		h.refresh()
	}

	reset := func() {
		// Reset overrides to whatever spec.Hidden says — i.e. the
		// codegen-defined defaults. Keeps the modal open so the user
		// can see the result before committing with `s`.
		cur := list.GetCurrentItem()
		for _, c := range h.specColumns {
			overrides[c.key] = !c.hidden
		}
		rebuild()
		list.SetCurrentItem(cur)
	}

	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			h.app.pages.RemovePage("cols-modal")
			return nil
		case tcell.KeyCtrlS:
			apply()
			return nil
		case tcell.KeyCtrlR:
			reset()
			return nil
		}
		switch ev.Rune() {
		case 's':
			// Single-key apply — same convention as the condition
			// builder. `space` is the vim-style toggle for the row,
			// keeping enter free for "default" semantics.
			apply()
			return nil
		case 'r':
			// Reset to schema defaults (whatever the codegen marked
			// with enttui.Hidden / the convention rules).
			reset()
			return nil
		case ' ':
			// Space toggles the focused row — same as enter on a
			// non-button row, just without leaving the cursor.
			i := list.GetCurrentItem()
			// Compute the column matching this row index (rebuild
			// iterates spec.columns and skips "body").
			j := 0
			for _, c := range h.specColumns {
				if c.key == "body" {
					continue
				}
				if j == i {
					overrides[c.key] = !overrides[c.key]
					rebuild()
					list.SetCurrentItem(i)
					break
				}
				j++
			}
			return nil
		}
		return ev
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(tview.NewTextView().
			SetText(" enter / space toggle · s apply · r reset · esc close ").
			SetTextColor(tcell.ColorGray), 1, 0, false)
	body.SetBorder(true).
		SetTitle(" columns ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	h.app.pages.AddPage("cols-modal", centerModal(body, 40, 20), true, true)
	h.app.tv.SetFocus(list)
}

// --- helpers ---

// visibleColumns returns the columns the table should render right now,
// honoring user overrides from openColumnsModal first, then spec.Hidden,
// always dropping the body column.
func (t *tableView) visibleColumns() []anyColumn {
	out := make([]anyColumn, 0, len(t.spec.columns))
	for _, c := range t.spec.columns {
		if c.key == "body" {
			continue
		}
		if t.columnOverrides != nil {
			if !t.columnOverrides[c.key] {
				continue
			}
		} else if c.hidden {
			continue
		}
		out = append(out, c)
	}
	return out
}

// filterableColumns returns the subset usable by the condition builder.
func (t *tableView) filterableColumns() []anyColumn {
	out := make([]anyColumn, 0, len(t.spec.columns))
	for _, c := range t.spec.columns {
		if c.filterable {
			out = append(out, c)
		}
	}
	return out
}

// centerModal wraps a primitive in a centered flex of given (cols, rows).
func centerModal(p tview.Primitive, cols, rows int) tview.Primitive {
	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(
			tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(p, rows, 0, true).
				AddItem(nil, 0, 1, false),
			cols, 0, true,
		).
		AddItem(nil, 0, 1, false)
}

// formatSortStack returns a compact text representation for the status bar.
//
//	[status↑ created_at↓ priority↑]  →  "status↑ created_at↓ priority↑"
func formatSortStack(stack []SortKey) string {
	if len(stack) == 0 {
		return ""
	}
	parts := make([]string, len(stack))
	for i, k := range stack {
		dir := "↑"
		if k.Dir == Desc {
			dir = "↓"
		}
		parts[i] = k.Field + dir
	}
	return strings.Join(parts, " ")
}

// parseInt is a tiny helper for ListOpts numeric values in tests / debug.
func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
