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

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

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
func (t *tableView) openSortModal() {
	if len(t.sortStack) == 0 {
		t.updateStatus("sort stack empty — press s on a column first")
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
		for i, k := range t.sortStack {
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

	// Enter on a row flips direction in place.
	list.SetSelectedFunc(func(idx int, _, _ string, _ rune) {
		if idx < 0 || idx >= len(t.sortStack) {
			return
		}
		if t.sortStack[idx].Dir == Asc {
			t.sortStack[idx].Dir = Desc
		} else {
			t.sortStack[idx].Dir = Asc
		}
		rebuild()
		t.refresh()
	})

	// moveUp / moveDown encapsulate the reorder logic — bound to several
	// keys below so users with terminals that mishandle capital letters
	// or shift+arrow still have a working binding.
	moveUp := func() {
		idx := list.GetCurrentItem()
		if idx > 0 && idx < len(t.sortStack) {
			t.sortStack[idx], t.sortStack[idx-1] = t.sortStack[idx-1], t.sortStack[idx]
			list.SetCurrentItem(idx - 1)
			rebuild()
			t.refresh()
		}
	}
	moveDown := func() {
		idx := list.GetCurrentItem()
		if idx >= 0 && idx < len(t.sortStack)-1 {
			t.sortStack[idx], t.sortStack[idx+1] = t.sortStack[idx+1], t.sortStack[idx]
			list.SetCurrentItem(idx + 1)
			rebuild()
			t.refresh()
		}
	}

	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		idx := list.GetCurrentItem()
		// Reorder bindings: ctrl+↑/↓ (primary, universal), shift+↑/↓
		// (some terminals), and K/J as vim-style fallback.
		switch ev.Key() {
		case tcell.KeyEscape:
			t.app.pages.RemovePage("sort-modal")
			return nil
		case tcell.KeyCtrlK:
			moveUp()
			return nil
		case tcell.KeyCtrlJ:
			moveDown()
			return nil
		}
		// Shift / Ctrl + arrow detection.
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
			if idx >= 0 && idx < len(t.sortStack) {
				t.sortStack = append(t.sortStack[:idx], t.sortStack[idx+1:]...)
				if len(t.sortStack) == 0 {
					t.app.pages.RemovePage("sort-modal")
					t.refresh()
					return nil
				}
				rebuild()
				t.refresh()
			}
			return nil
		case 'c':
			t.sortStack = nil
			t.app.pages.RemovePage("sort-modal")
			t.refresh()
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

	t.app.pages.AddPage("sort-modal", centerModal(body, 60, 20), true, true)
	t.app.tv.SetFocus(list)
}

// --- Phase F: condition builder ---

// openConditionBuilder presents a modal of (column, operator, value) rows.
// Layout: scrollable list of conditions on top + explicit Add / Apply /
// Cancel buttons below in a Form. Tab cycles between the list and the
// buttons; ctrl+s = Apply from anywhere (escape hatch).
func (t *tableView) openConditionBuilder() {
	cols := t.filterableColumns()
	if len(cols) == 0 {
		t.updateStatus("no filterable columns")
		return
	}

	// Working copy so cancel reverts.
	work := append([]FilterCondition(nil), t.colFilters...)

	// Conditions list (display + delete).
	list := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDodgerBlue).
		SetSelectedTextColor(tcell.ColorBlack)

	apply := func() {
		t.colFilters = work
		t.page = 0
		t.app.pages.RemovePage("condition-builder")
		t.refresh()
	}
	cancel := func() {
		t.app.pages.RemovePage("condition-builder")
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
		t.openEditConditionRow(cols, &work, list, rebuild, idx)
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
				t.openEditConditionRow(cols, &work, list, rebuild, idx)
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
			t.openAddConditionRow(cols, &work, list, rebuild)
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
	t.app.pages.AddPage("condition-builder", page, true, true)
	t.app.tv.SetFocus(list)

	// Bind page-level hotkeys via the body flex's InputCapture so they
	// fire regardless of whether focus is on the list or the form buttons.
	addFn := func() { t.openAddConditionRow(cols, &work, list, rebuild) }
	body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			cancel()
			return nil
		case tcell.KeyTab:
			// Tab cycles list ↔ form buttons.
			if t.app.tv.GetFocus() == list {
				t.app.tv.SetFocus(form)
			} else {
				t.app.tv.SetFocus(list)
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
func (t *tableView) openAddConditionRow(cols []anyColumn, work *[]FilterCondition, parent *tview.List, rebuild func()) {
	t.openColumnPicker(cols, work, parent, rebuild, -1)
}

// openEditConditionRow opens the picker chain pre-filled with the
// current condition at `idx` so the user can change column / operator /
// value. Submitting REPLACES instead of appending.
func (t *tableView) openEditConditionRow(cols []anyColumn, work *[]FilterCondition, parent *tview.List, rebuild func(), idx int) {
	t.openColumnPicker(cols, work, parent, rebuild, idx)
}

// openColumnPicker is the shared implementation used by both add + edit
// flows. editIdx >= 0 means edit mode.
func (t *tableView) openColumnPicker(cols []anyColumn, work *[]FilterCondition, parent *tview.List, rebuild func(), editIdx int) {
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
		t.app.pages.RemovePage("cb-col")
		t.openPickOperator(col, work, parent, rebuild, editIdx)
	}
	cancel := func() { t.app.pages.RemovePage("cb-col") }

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

	t.app.pages.AddPage("cb-col", centerModal(body, 50, 22), true, true)
	t.app.tv.SetFocus(input)
}

// openPickOperator is step 2 — operator menu typed to the column kind.
// editIdx >= 0 means we're replacing (*work)[editIdx] on submit instead
// of appending. v1: a fixed operator list.
func (t *tableView) openPickOperator(col anyColumn, work *[]FilterCondition, parent *tview.List, rebuild func(), editIdx int) {
	t.app.pages.RemovePage("cb-col")

	ops := []FilterOp{OpEq, OpNeq, OpContains, OpLt, OpLte, OpGt, OpGte, OpIn, OpNotIn, OpIsNull, OpNotNull}
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
			t.app.pages.RemovePage("cb-op")
			rebuild()
			return
		}
		t.openEnterValue(col, op, work, rebuild, editIdx)
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
	t.app.pages.AddPage("cb-op", centerModal(body, 30, 18), true, true)
	t.app.tv.SetFocus(opSel)
	opSel.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			t.app.pages.RemovePage("cb-op")
			return nil
		}
		return ev
	})
	_ = parent
}

// openEnterValue is step 3 — text input for the value. On enter, append
// (or replace if editIdx >= 0) and dismiss the chain of modals.
func (t *tableView) openEnterValue(col anyColumn, op FilterOp, work *[]FilterCondition, rebuild func(), editIdx int) {
	t.app.pages.RemovePage("cb-op")

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
			t.app.pages.RemovePage("cb-val")
			rebuild()
		case tcell.KeyEscape:
			t.app.pages.RemovePage("cb-val")
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
	t.app.pages.AddPage("cb-val", centerModal(body, 50, 6), true, true)
	t.app.tv.SetFocus(input)
}

// --- Phase G: column show/hide ---

// openColumnsModal lists every column with a checkbox indicating its
// visibility. Toggling is session-only (in-memory per kind).
func (t *tableView) openColumnsModal() {
	if t.columnOverrides == nil {
		t.columnOverrides = make(map[string]bool, len(t.spec.columns))
		for _, c := range t.spec.columns {
			t.columnOverrides[c.key] = !c.hidden
		}
	}

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
		for _, c := range t.spec.columns {
			if c.key == "body" {
				continue
			}
			// `✓` (green) = visible, `✗` (red) = hidden. Reads faster
			// than `[x]` / `[ ]` at a glance — the red ✗ pops on the
			// row that's been turned off.
			mark := "[green]✓[-]"
			if !t.columnOverrides[c.key] {
				mark = "[red]✗[-]"
			}
			cKey := c.key
			// Empty secondaryText — ShowSecondaryText(false) is set, so
			// passing c.key was unused and just noise.
			list.AddItem(mark+" "+c.label, "", 0, func() {
				t.columnOverrides[cKey] = !t.columnOverrides[cKey]
				cur := list.GetCurrentItem()
				rebuild()
				list.SetCurrentItem(cur)
			})
		}
		list.AddItem("[yellow]apply", "", 0, func() {
			t.app.pages.RemovePage("cols-modal")
			t.refresh()
		})
		list.AddItem("[gray]close", "", 0, func() {
			t.app.pages.RemovePage("cols-modal")
		})
	}
	rebuild()

	apply := func() {
		t.app.pages.RemovePage("cols-modal")
		t.refresh()
	}

	reset := func() {
		// Reset overrides to whatever spec.Hidden says — i.e. the
		// codegen-defined defaults. Keeps the modal open so the user
		// can see the result before committing with `s`.
		cur := list.GetCurrentItem()
		for _, c := range t.spec.columns {
			t.columnOverrides[c.key] = !c.hidden
		}
		rebuild()
		list.SetCurrentItem(cur)
	}

	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			t.app.pages.RemovePage("cols-modal")
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
			for _, c := range t.spec.columns {
				if c.key == "body" {
					continue
				}
				if j == i {
					t.columnOverrides[c.key] = !t.columnOverrides[c.key]
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

	t.app.pages.AddPage("cols-modal", centerModal(body, 40, 20), true, true)
	t.app.tv.SetFocus(list)
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
