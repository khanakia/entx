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

// openSortModal lists the current stack with reorder buttons. v1 keeps
// the UX minimal: each row is "field ↑/↓ [↑move ↓move ✕remove]".
func (t *tableView) openSortModal() {
	if len(t.sortStack) == 0 {
		t.updateStatus("sort stack empty — press s on a column first")
		return
	}

	list := tview.NewList().ShowSecondaryText(false)
	rebuild := func() {
		list.Clear()
		for i, k := range t.sortStack {
			dir := "↑"
			if k.Dir == Desc {
				dir = "↓"
			}
			label := fmt.Sprintf("%d. %s %s", i+1, k.Field, dir)
			idx := i
			list.AddItem(label, "", 0, func() {
				// Click cycles the dir; long-press equivalents on
				// key shortcuts below.
				if t.sortStack[idx].Dir == Asc {
					t.sortStack[idx].Dir = Desc
				} else {
					t.sortStack[idx].Dir = Asc
				}
				t.refresh()
				// Refresh modal too.
				t.openSortModal()
			})
		}
		list.AddItem("[red]clear all", "", 'X', func() {
			t.sortStack = nil
			t.app.pages.RemovePage("sort-modal")
			t.refresh()
		})
		list.AddItem("[gray]close", "", 0, func() {
			t.app.pages.RemovePage("sort-modal")
		})
	}
	rebuild()

	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		idx := list.GetCurrentItem()
		if idx < 0 || idx >= len(t.sortStack) {
			return ev
		}
		switch ev.Rune() {
		case 'K':
			// Move up.
			if idx > 0 {
				t.sortStack[idx], t.sortStack[idx-1] = t.sortStack[idx-1], t.sortStack[idx]
				rebuild()
				list.SetCurrentItem(idx - 1)
				t.refresh()
			}
			return nil
		case 'J':
			// Move down.
			if idx < len(t.sortStack)-1 {
				t.sortStack[idx], t.sortStack[idx+1] = t.sortStack[idx+1], t.sortStack[idx]
				rebuild()
				list.SetCurrentItem(idx + 1)
				t.refresh()
			}
			return nil
		case 'd':
			// Delete.
			t.sortStack = append(t.sortStack[:idx], t.sortStack[idx+1:]...)
			rebuild()
			t.refresh()
			return nil
		}
		if ev.Key() == tcell.KeyEscape {
			t.app.pages.RemovePage("sort-modal")
			return nil
		}
		return ev
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(tview.NewTextView().
			SetText(" K up · J down · d delete · enter flip dir · esc close ").
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
// Add / edit / delete rows; apply builds a flat []FilterCondition list
// AND-composed at the top level. (Nested groups planned but not in v1.)
func (t *tableView) openConditionBuilder() {
	cols := t.filterableColumns()
	if len(cols) == 0 {
		t.updateStatus("no filterable columns")
		return
	}

	// Working copy so cancel reverts.
	work := append([]FilterCondition(nil), t.colFilters...)

	list := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true)
	var rebuild func() // forward declaration so the closure can reference itself
	rebuild = func() {
		list.Clear()
		for i, c := range work {
			label := fmt.Sprintf("%d. %s %s %q", i+1, c.Field, c.Op, c.Value)
			list.AddItem(label, "", 0, nil)
		}
		list.AddItem("[green]+ add condition", "", 'a', func() {
			t.openAddConditionRow(cols, &work, list, rebuild)
		})
		list.AddItem("[yellow]apply", "", 0, func() {
			t.colFilters = work
			t.page = 0
			t.app.pages.RemovePage("condition-builder")
			t.refresh()
		})
		list.AddItem("[gray]cancel", "", 0, func() {
			t.app.pages.RemovePage("condition-builder")
		})
	}
	rebuild()

	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		idx := list.GetCurrentItem()
		switch ev.Key() {
		case tcell.KeyEscape:
			t.app.pages.RemovePage("condition-builder")
			return nil
		}
		if ev.Rune() == 'd' && idx >= 0 && idx < len(work) {
			work = append(work[:idx], work[idx+1:]...)
			rebuild()
			return nil
		}
		return ev
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(tview.NewTextView().
			SetText(" a add · d delete · enter pick · esc cancel ").
			SetTextColor(tcell.ColorGray), 1, 0, false)
	body.SetBorder(true).
		SetTitle(" filter — condition builder ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	t.app.pages.AddPage("condition-builder", centerModal(body, 70, 22), true, true)
	t.app.tv.SetFocus(list)
}

// openAddConditionRow is a sub-modal: pick a column, pick an operator,
// type a value. On submit, appends to `work` and refreshes the parent.
func (t *tableView) openAddConditionRow(cols []anyColumn, work *[]FilterCondition, parent *tview.List, rebuild func()) {
	colSel := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true)
	for _, c := range cols {
		c := c
		colSel.AddItem(c.label, c.key, 0, func() {
			t.openPickOperator(c, work, parent, rebuild)
		})
	}
	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(colSel, 0, 1, true)
	body.SetBorder(true).
		SetTitle(" 1/3 column ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)
	t.app.pages.AddPage("cb-col", centerModal(body, 40, 16), true, true)
	t.app.tv.SetFocus(colSel)
	colSel.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			t.app.pages.RemovePage("cb-col")
			return nil
		}
		return ev
	})
}

// openPickOperator is step 2 — operator menu typed to the column kind.
// v1: a fixed list. Future: type-aware via codegen metadata.
func (t *tableView) openPickOperator(col anyColumn, work *[]FilterCondition, parent *tview.List, rebuild func()) {
	t.app.pages.RemovePage("cb-col")

	ops := []FilterOp{OpEq, OpNeq, OpContains, OpLt, OpLte, OpGt, OpGte, OpIn, OpNotIn, OpIsNull, OpNotNull}
	opSel := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true)
	for _, op := range ops {
		op := op
		opSel.AddItem(string(op), col.key, 0, func() {
			if op == OpIsNull || op == OpNotNull {
				// No value needed — submit immediately.
				*work = append(*work, FilterCondition{Field: col.key, Op: op})
				t.app.pages.RemovePage("cb-op")
				rebuild()
				return
			}
			t.openEnterValue(col, op, work, rebuild)
		})
	}
	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(opSel, 0, 1, true)
	body.SetBorder(true).
		SetTitle(" 2/3 operator (" + col.label + ") ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)
	t.app.pages.AddPage("cb-op", centerModal(body, 30, 16), true, true)
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
// the condition and dismiss the chain of modals.
func (t *tableView) openEnterValue(col anyColumn, op FilterOp, work *[]FilterCondition, rebuild func()) {
	t.app.pages.RemovePage("cb-op")

	input := tview.NewInputField().
		SetLabel(col.label + " " + string(op) + " ").
		SetLabelColor(tcell.ColorYellow).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldWidth(40)
	input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			*work = append(*work, FilterCondition{Field: col.key, Op: op, Value: input.GetText()})
			t.app.pages.RemovePage("cb-val")
			rebuild()
		case tcell.KeyEscape:
			t.app.pages.RemovePage("cb-val")
		}
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true)
	body.SetBorder(true).
		SetTitle(" 3/3 value ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)
	t.app.pages.AddPage("cb-val", centerModal(body, 50, 5), true, true)
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

	list := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true)
	var rebuild func()
	rebuild = func() {
		list.Clear()
		for _, c := range t.spec.columns {
			if c.key == "body" {
				continue
			}
			mark := "[ ]"
			if t.columnOverrides[c.key] {
				mark = "[x]"
			}
			cKey := c.key
			list.AddItem(mark+" "+c.label, c.key, 0, func() {
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

	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			t.app.pages.RemovePage("cols-modal")
			return nil
		}
		return ev
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(tview.NewTextView().
			SetText(" enter toggle · esc close ").
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
