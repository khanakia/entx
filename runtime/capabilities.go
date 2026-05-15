package runtime

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

// Capabilities matrix — `F` opens a searchable table of every
// registered kind × which features are enabled (edit / create / delete
// / bulk-copy / export), plus column + edge counts. Mirrors the help
// palette UX: type to filter, @-scope, ctrl+e to dump CSV, enter to
// jump straight to that kind.
//
// This is the at-a-glance answer to "which tables can I edit / export?"
// without opening each one to read the status chips.

type capRow struct {
	Kind    string
	Display string
	Group   string
	Edit    bool
	Create  bool
	Delete  bool
	Bulk    bool
	Export  bool
	Cols    int // visible (non-hidden) column count
	Filter  int // filterable column count
	Sort    int // sortable column count
	Edges   int
}

func (a *App) capRows() []capRow {
	specs := a.kindListSortedByDisplay()
	out := make([]capRow, 0, len(specs))
	for _, s := range specs {
		r := capRow{
			Kind:    s.kind,
			Display: s.display,
			Group:   s.group,
			Edit:    s.update != nil && len(s.formFields) > 0,
			Create:  s.create != nil && len(s.formFields) > 0,
			Delete:  s.deleteRow != nil,
			Bulk:    s.allowBulkCopy,
			Export:  s.allowExport,
			Edges:   len(s.edges),
		}
		for _, c := range s.columns {
			if !c.hidden {
				r.Cols++
			}
			if c.filterable {
				r.Filter++
			}
			if c.sortable {
				r.Sort++
			}
		}
		out = append(out, r)
	}
	return out
}

func tick(b bool) string {
	if b {
		return "[green]✓[-]"
	}
	return "[gray]·[-]"
}

// openKindInfo shows a compact card for ONE kind — the current view's
// capabilities, every flag listed (on AND off) with the annotation a
// user would add to enable each off one. Answers "what can I do on
// THIS table right now?" without scanning the global matrix.
func (a *App) openKindInfo(s *anySpec) {
	if s == nil {
		return
	}
	edit := s.update != nil && len(s.formFields) > 0
	create := s.create != nil && len(s.formFields) > 0
	del := s.deleteRow != nil
	cols, filt, sort := 0, 0, 0
	for _, c := range s.columns {
		if !c.hidden {
			cols++
		}
		if c.filterable {
			filt++
		}
		if c.sortable {
			sort++
		}
	}

	line := func(on bool, label, annot string) string {
		if on {
			return fmt.Sprintf("  [green]✓[-]  %-22s [gray]enabled[-]", label)
		}
		return fmt.Sprintf("  [gray]·[-]  %-22s [gray]off — add %s[-]", label, annot)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[yellow::b]%s[-:-:-]  ([aqua]%s[-], group [gray]%s[-])\n\n", s.display, s.kind, s.group)
	b.WriteString(line(edit, "Edit (e)", "enttui.Editable{} per field") + "\n")
	b.WriteString(line(create, "Create (N)", "enttui.AllowCreate{}") + "\n")
	b.WriteString(line(del, "Delete (D)", "enttui.AllowDelete{}") + "\n")
	b.WriteString(line(s.allowBulkCopy, "Bulk select/copy", "enttui.AllowBulkCopy{}") + "\n")
	b.WriteString(line(s.allowExport, "Export (X)", "enttui.AllowExport{}") + "\n")
	b.WriteString(line(s.showEdgeCounts, "Edge counts", "enttui.CountEdges{}") + "\n")
	fmt.Fprintf(&b, "\n  [gray]columns[-] %d visible · %d filterable · %d sortable · %d edges\n",
		cols, filt, sort, len(s.edges))

	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetText(b.String())

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tv, 0, 1, false).
		AddItem(tview.NewTextView().
			SetTextColor(tcell.ColorGray).
			SetText(" F : full matrix · esc : close "), 1, 0, false)
	body.SetBorder(true).
		SetTitle(" this view ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)
	body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch {
		case ev.Key() == tcell.KeyEscape, ev.Key() == tcell.KeyEnter:
			a.pages.RemovePage("kind-info")
			return nil
		case ev.Rune() == 'F':
			a.pages.RemovePage("kind-info")
			a.openCapabilities()
			return nil
		}
		return ev
	})

	a.pages.AddPage("kind-info", centerModal(body, 70, 16), true, true)
	a.tv.SetFocus(body)
}

// openCapabilities renders the matrix. Reuses the help-palette layout:
// filter input on top, table below, mutable footer.
func (a *App) openCapabilities() {
	all := a.capRows()

	input := tview.NewInputField().
		SetLabel("filter › ").
		SetLabelColor(tcell.ColorYellow).
		SetFieldWidth(50).
		SetFieldBackgroundColor(tcell.ColorDefault)

	tbl := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0).
		SetSeparator(' ').
		SetSelectedStyle(tcell.StyleDefault.
			Background(tcell.ColorDodgerBlue).
			Foreground(tcell.ColorBlack).
			Attributes(tcell.AttrBold))

	headers := []string{"KIND", "DISPLAY", "GROUP", "EDIT", "NEW", "DEL", "BULK", "EXPORT", "COLS", "FILT", "SORT", "EDGES"}

	var shown []capRow
	populate := func() {
		tbl.Clear()
		for c, h := range headers {
			tbl.SetCell(0, c, tview.NewTableCell(h).
				SetTextColor(tcell.ColorYellow).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false).
				SetExpansion(1))
		}
		for i, r := range shown {
			row := i + 1
			set := func(col int, txt string, color tcell.Color) {
				tbl.SetCell(row, col, tview.NewTableCell(txt).
					SetTextColor(color).SetExpansion(1))
			}
			set(0, r.Kind, tcell.ColorAqua)
			set(1, r.Display, tcell.ColorWhite)
			set(2, r.Group, tcell.ColorGray)
			set(3, tick(r.Edit), tcell.ColorWhite)
			set(4, tick(r.Create), tcell.ColorWhite)
			set(5, tick(r.Delete), tcell.ColorWhite)
			set(6, tick(r.Bulk), tcell.ColorWhite)
			set(7, tick(r.Export), tcell.ColorWhite)
			set(8, fmt.Sprintf("%d", r.Cols), tcell.ColorWhite)
			set(9, fmt.Sprintf("%d", r.Filter), tcell.ColorWhite)
			set(10, fmt.Sprintf("%d", r.Sort), tcell.ColorWhite)
			set(11, fmt.Sprintf("%d", r.Edges), tcell.ColorWhite)
		}
		if len(shown) > 0 {
			tbl.Select(1, 0)
		}
	}

	applyFilter := func(text string) {
		q := strings.ToLower(strings.TrimSpace(text))
		if q == "" {
			shown = all
			populate()
			return
		}
		// `cap:` tokens isolate one capability column. Recognized:
		// edit, new/create, del/delete, bulk, export. Anything else →
		// full-text on kind/display/group.
		shown = make([]capRow, 0, len(all))
		for _, r := range all {
			match := false
			switch q {
			case "cap:edit", "@edit":
				match = r.Edit
			case "cap:new", "cap:create", "@create":
				match = r.Create
			case "cap:del", "cap:delete", "@delete":
				match = r.Delete
			case "cap:bulk", "@bulk":
				match = r.Bulk
			case "cap:export", "@export":
				match = r.Export
			default:
				hay := strings.ToLower(r.Kind + " " + r.Display + " " + r.Group)
				match = strings.Contains(hay, q)
			}
			if match {
				shown = append(shown, r)
			}
		}
		populate()
	}
	applyFilter("")

	footer := tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(tcell.ColorGray).
		SetText(" type to filter · [yellow]cap:edit|new|del|bulk|export[-] · [yellow]ctrl+e[-] CSV · enter open · esc close ")

	exportCSV := func() {
		cwd, _ := os.Getwd()
		path := filepath.Join(cwd, "enttui-capabilities-"+time.Now().Format("20060102-150405")+".csv")
		f, err := os.Create(path)
		if err != nil {
			footer.SetText(" [red]export failed: " + err.Error() + "[-]")
			return
		}
		w := csv.NewWriter(f)
		_ = w.Write([]string{"kind", "display", "group", "edit", "create", "delete", "bulk_copy", "export", "cols", "filterable", "sortable", "edges"})
		yn := func(b bool) string {
			if b {
				return "yes"
			}
			return "no"
		}
		for _, r := range shown {
			_ = w.Write([]string{
				r.Kind, r.Display, r.Group,
				yn(r.Edit), yn(r.Create), yn(r.Delete), yn(r.Bulk), yn(r.Export),
				fmt.Sprintf("%d", r.Cols), fmt.Sprintf("%d", r.Filter),
				fmt.Sprintf("%d", r.Sort), fmt.Sprintf("%d", r.Edges),
			})
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

	closeFn := func() { a.pages.RemovePage("capabilities") }

	openSelected := func() {
		r, _ := tbl.GetSelection()
		idx := r - 1
		if idx < 0 || idx >= len(shown) {
			return
		}
		kind := shown[idx].Kind
		closeFn()
		a.pushBrowser(kind, "")
	}

	input.SetChangedFunc(applyFilter)
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
			if r > 1 {
				tbl.Select(r-1, 0)
			}
			return nil
		case tcell.KeyPgDn:
			r, _ := tbl.GetSelection()
			tbl.Select(min(r+10, tbl.GetRowCount()-1), 0)
			return nil
		case tcell.KeyPgUp:
			r, _ := tbl.GetSelection()
			tbl.Select(max(r-10, 1), 0)
			return nil
		case tcell.KeyEnter:
			openSelected()
			return nil
		case tcell.KeyEscape:
			closeFn()
			return nil
		case tcell.KeyCtrlE:
			exportCSV()
			return nil
		}
		return ev
	})

	tbl.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEnter:
			openSelected()
			return nil
		case tcell.KeyEscape:
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
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(tbl, 0, 1, false).
		AddItem(footer, 1, 0, false)
	body.SetBorder(true).
		SetTitle(" capabilities — kinds × features ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	a.pages.AddPage("capabilities", centerModal(body, 100, 34), true, true)
	a.tv.SetFocus(input)
}
