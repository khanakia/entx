package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Master-detail split: a two-pane page. Top = the master kind's table;
// bottom = a live child table of one of the master's DetailEdges. With
// more than one edge the detail pane is TABBED — one tab per edge, each
// a full tableView restricted (via idFilter) to the selected master
// row's children for that edge.
//
// Reuse over rebuild: every pane IS a *tableView (own keyCapture,
// modals, status). The split adds: vertical layout, a tab strip,
// Tab focus switching, ]/[ tab cycling, and the master→detail wiring.
// Child tableViews are built lazily — each edge can target a different
// child kind, learned from its first resolveDrill.

type mdTab struct {
	edgeName string
	edge     *anyEdge
	tv       *tableView // nil until first activated (lazy)
	built    bool
}

type masterDetailView struct {
	app    *App
	master *tableView
	tabs   []*mdTab
	active int
	root   *tview.Flex
	tabBar *tview.TextView
	slot   *tview.Flex // holds the active detail pane (swapped on tab change)
	focus  int         // 0 = master, 1 = detail
}

func (a *App) pushMasterDetail(masterSpec *anySpec) {
	if len(masterSpec.detailEdges) == 0 {
		return
	}
	md := &masterDetailView{app: a}
	md.master = newTableView(a, masterSpec)

	for _, name := range masterSpec.detailEdges {
		var e *anyEdge
		for i := range masterSpec.edges {
			if masterSpec.edges[i].name == name {
				e = &masterSpec.edges[i]
				break
			}
		}
		if e == nil || e.resolveDrill == nil {
			continue // unknown / non-drill edge — skip silently
		}
		md.tabs = append(md.tabs, &mdTab{edgeName: name, edge: e})
	}
	if len(md.tabs) == 0 {
		a.flash("no usable drill edges for master-detail on " + masterSpec.kind)
		return
	}

	md.tabBar = tview.NewTextView().SetDynamicColors(true)
	md.slot = tview.NewFlex()

	masterPane := paneWrap(md.master.root, " "+masterSpec.display+" (master) ", true)

	md.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(masterPane, 0, 1, true).
		AddItem(md.tabBar, 1, 0, false).
		AddItem(md.slot, 0, 1, false).
		AddItem(tview.NewTextView().
			SetDynamicColors(true).
			SetTextColor(tcell.ColorGray).
			SetText(" tab: switch pane · ] [ : detail tabs · m: exit · esc: back "), 1, 0, false)

	// Master cursor move → re-filter the active detail tab.
	md.master.table.SetSelectionChangedFunc(func(row, _ int) {
		md.syncActive(row)
	})

	md.activateTab(0) // builds tab 0 + primes it

	md.installTabSwitch()

	name := pageName("md", masterSpec.kind, "")
	a.pages.AddPage(name, md.root, true, true)
	a.stack = append(a.stack, pageEntry{name: name, title: masterSpec.display + " (master-detail)", kind: masterSpec.kind})
	a.registerInstance(name, md.master) // per-kind state cache keys off the master
	a.tv.SetFocus(md.master.table)
	a.syncSidebar()
}

// currentMasterID returns the master row currently under the cursor.
func (md *masterDetailView) currentMasterID() (string, bool) {
	row, _ := md.master.table.GetSelection()
	i := row - 1
	if i < 0 || i >= len(md.master.rows) {
		return "", false
	}
	return md.master.rows[i].ID, true
}

// activateTab builds the tab's child tableView on first use, swaps it
// into the slot, primes it with the current master row, repaints the
// tab strip.
func (md *masterDetailView) activateTab(idx int) {
	if idx < 0 || idx >= len(md.tabs) {
		return
	}
	md.active = idx
	t := md.tabs[idx]

	if !t.built {
		// Discover the child kind by resolving this edge for the
		// current master row (or row 0).
		mid, ok := md.currentMasterID()
		if !ok && len(md.master.rows) > 0 {
			mid, ok = md.master.rows[0].ID, true
		}
		childKind := ""
		if ok {
			ctx, cancel := context.WithTimeout(md.app.ctx, 5*time.Second)
			refs, err := t.edge.resolveDrill(ctx, mid)
			cancel()
			if err == nil {
				childKind = refs.Kind
			}
		}
		if cs := md.app.specs[childKind]; cs != nil {
			t.tv = newTableView(md.app, cs)
			t.tv.idFilter = map[string]bool{}
		}
		t.built = true
		if t.tv != nil {
			md.wrapKeys(t.tv)
		}
	}

	md.slot.Clear()
	if t.tv != nil {
		title := fmt.Sprintf(" %s — via %s ", t.tv.spec.display, t.edgeName)
		md.slot.AddItem(paneWrap(t.tv.root, title, md.focus == 1), 0, 1, true)
	} else {
		tvw := tview.NewTextView().
			SetDynamicColors(true).
			SetText("[gray](no child rows yet — move the master cursor / press r)[-]")
		md.slot.AddItem(paneWrap(tvw, " "+t.edgeName+" ", md.focus == 1), 0, 1, true)
	}
	md.repaintTabBar()
	if row, _ := md.master.table.GetSelection(); row >= 1 {
		md.syncActive(row)
	}
}

func (md *masterDetailView) repaintTabBar() {
	parts := make([]string, len(md.tabs))
	for i, t := range md.tabs {
		label := t.edgeName
		if i == md.active {
			parts[i] = "[black:dodgerblue:b] " + label + " [-:-:-]"
		} else {
			parts[i] = "[gray] " + label + " [-]"
		}
	}
	md.tabBar.SetText(" " + strings.Join(parts, " "))
}

// syncActive re-resolves the active tab's edge for the master row at
// table-row `row` (1-based) and points its detail table at the kids.
func (md *masterDetailView) syncActive(row int) {
	if md.active >= len(md.tabs) {
		return
	}
	t := md.tabs[md.active]
	if t.tv == nil {
		return
	}
	i := row - 1
	if i < 0 || i >= len(md.master.rows) {
		return
	}
	ctx, cancel := context.WithTimeout(md.app.ctx, 5*time.Second)
	refs, err := t.edge.resolveDrill(ctx, md.master.rows[i].ID)
	cancel()
	set := map[string]bool{}
	if err == nil {
		for _, id := range refs.IDs {
			set[id] = true
		}
	}
	t.tv.idFilter = set
	t.tv.refresh()
}

func (md *masterDetailView) installTabSwitch() {
	md.wrapKeys(md.master)
}

// wrapKeys layers split-level shortcuts (tab focus, ]/[ tab cycle, m
// exit) on top of a tableView's own keyCapture.
func (md *masterDetailView) wrapKeys(t *tableView) {
	orig := t.keyCapture
	t.table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyTab, tcell.KeyBacktab:
			md.toggleFocus()
			return nil
		}
		switch ev.Rune() {
		case ']':
			md.activateTab((md.active + 1) % len(md.tabs))
			md.app.tv.SetFocus(md.activeFocusPrimitive())
			return nil
		case '[':
			md.activateTab((md.active - 1 + len(md.tabs)) % len(md.tabs))
			md.app.tv.SetFocus(md.activeFocusPrimitive())
			return nil
		case 'm':
			md.app.popPage()
			md.app.pushBrowser(md.master.spec.kind, "")
			return nil
		}
		return orig(ev)
	})
}

func (md *masterDetailView) activeFocusPrimitive() tview.Primitive {
	if md.focus == 1 && md.active < len(md.tabs) && md.tabs[md.active].tv != nil {
		return md.tabs[md.active].tv.table
	}
	return md.master.table
}

func (md *masterDetailView) toggleFocus() {
	if md.focus == 0 {
		t := md.tabs[md.active]
		if t.tv == nil {
			return // nothing to focus in the detail pane yet
		}
		md.focus = 1
	} else {
		md.focus = 0
	}
	// Re-wrap so a freshly-focused detail table gets the split keys,
	// and repaint borders to show the active pane.
	md.activateTab(md.active)
	md.app.tv.SetFocus(md.activeFocusPrimitive())
}

// paneWrap puts a titled border around a pane; orange = focused.
func paneWrap(p tview.Primitive, title string, focused bool) tview.Primitive {
	f := tview.NewFlex().AddItem(p, 0, 1, true)
	f.SetBorder(true).SetTitle(title).SetTitleColor(tcell.ColorYellow)
	if focused {
		f.SetBorderColor(tcell.ColorOrange)
	} else {
		f.SetBorderColor(tcell.ColorDodgerBlue)
	}
	return f
}
