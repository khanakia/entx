package runtime

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// sidebar is a left-rail kind switcher that stays visible while the user
// browses. Unlike the modal picker (k):
//   - lives in the root flex (no overlay)
//   - swaps the top stack page on every selection change (live preview)
//   - hidden by default; toggled with `b`
//   - syncs its highlight with whatever kind is on top of the stack
//
// Layout: [InputField (filter)] / [List (kinds)].
//
// Focus model:
//   - input focused on mount; typing filters in real time
//   - arrows inside the input forward to list-selection moves (via the
//     input's SetInputCapture) so the user can filter and navigate
//     without leaving the input
//   - Tab cycles input ↔ list
//   - `\` switches focus between sidebar and the body pane (the front
//     stack page). Sidebar stays open; great for pinning the kind list
//     while interacting with the table/list.
//   - esc / enter (in input) closes the sidebar; the live-preview swap
//     has already taken effect, so close = commit.
//
// IMPORTANT — re-entrance: the list's SetChangedFunc fires a page swap
// via app.replaceTopKind. The swap calls app.syncSidebar which used to
// re-populate the list (Clear + AddItem + SetCurrentItem) — that nested
// SetCurrentItem corrupted the outer SetCurrentItem's bookkeeping and
// caused the cursor to teleport to the last item on the first arrow
// press. The fix: syncSidebar now only moves the cursor (no re-populate),
// guarded by suppressChange so it doesn't cascade.
type sidebar struct {
	app    *App
	root   *tview.Flex
	body   *tview.Flex
	input  *tview.InputField
	list   *tview.List
	all    []*anySpec
	shown  []*anySpec
	filter string

	// suppressChange skips the ChangedFunc-driven page swap during
	// programmatic SetCurrentItem calls (initial mount, post-filter
	// re-selection, and the highlight nudge that fires *during* a swap).
	suppressChange bool
}

func newSidebar(app *App) *sidebar {
	s := &sidebar{app: app, all: app.kindListSortedByDisplay()}
	s.shown = append([]*anySpec(nil), s.all...)

	s.input = tview.NewInputField().
		SetLabel("/ ").
		SetLabelColor(tcell.ColorYellow).
		SetFieldBackgroundColor(tcell.ColorDefault)

	s.list = tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDodgerBlue).
		SetSelectedTextColor(tcell.ColorBlack)

	// Live preview: selection change → swap top stack page to that kind.
	s.list.SetChangedFunc(func(i int, _, _ string, _ rune) {
		if s.suppressChange {
			return
		}
		if i < 0 || i >= len(s.shown) {
			return
		}
		kind := s.shown[i].kind
		if app.currentKind() == kind {
			return
		}
		app.replaceTopKind(kind)
		// pushBrowser steals focus onto the new view; pull it back to
		// whichever sidebar widget the user was driving before the swap.
		if app.tv.GetFocus() != s.list {
			app.tv.SetFocus(s.input)
		} else {
			app.tv.SetFocus(s.list)
		}
	})

	// Filter typing.
	s.input.SetChangedFunc(func(text string) {
		s.filter = text
		s.refilter()
	})

	// Input: arrows forward to list move; Tab → focus list; `\` → body;
	// enter / esc close.
	s.input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			s.move(+1)
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			s.move(-1)
			return nil
		case tcell.KeyPgDn:
			s.move(+10)
			return nil
		case tcell.KeyPgUp:
			s.move(-10)
			return nil
		case tcell.KeyTab:
			app.tv.SetFocus(s.list)
			return nil
		case tcell.KeyEnter, tcell.KeyEscape:
			app.hideSidebar()
			return nil
		case tcell.KeyRune:
			if ev.Rune() == '\\' {
				app.focusBody()
				return nil
			}
		}
		return ev
	})

	// List: arrows native; Tab → focus input; `\` → body; esc closes.
	s.list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyTab:
			app.tv.SetFocus(s.input)
			return nil
		case tcell.KeyEscape, tcell.KeyEnter:
			app.hideSidebar()
			return nil
		case tcell.KeyRune:
			if ev.Rune() == '\\' {
				app.focusBody()
				return nil
			}
		}
		return ev
	})

	s.body = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.input, 1, 0, true).
		AddItem(s.list, 0, 1, false)
	s.body.SetBorder(true).
		SetTitle(" kinds (^b close · \\ body) ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	s.populate()

	s.root = s.body
	return s
}

// refilter recomputes s.shown from s.filter and repopulates the list.
// Called only from input.SetChangedFunc — never from a page swap.
func (s *sidebar) refilter() {
	q := strings.ToLower(strings.TrimSpace(s.filter))
	if q == "" {
		s.shown = append([]*anySpec(nil), s.all...)
	} else {
		s.shown = make([]*anySpec, 0, len(s.all))
		for _, sp := range s.all {
			name := strings.ToLower(sp.display)
			if name == "" {
				name = sp.kind
			}
			if strings.Contains(name, q) || strings.Contains(sp.kind, q) {
				s.shown = append(s.shown, sp)
			}
		}
	}
	s.populate()

	// While a filter is active the user is searching — put the cursor
	// on the FIRST match so results are visible from the top. Without
	// this, populate()'s highlightCurrent() pins the cursor to the
	// current page's kind, which can be scrolled far down and makes a
	// broad query like "task" look like it's missing "TaskLists".
	if q != "" && len(s.shown) > 0 {
		s.suppressChange = true
		s.list.SetCurrentItem(0)
		s.suppressChange = false
	}

	// If the user just narrowed the filter to a set that no longer
	// contains the current page's kind, auto-swap to whatever the cursor
	// is sitting on (shown[0] by default). Without this, filtering down
	// to a single non-matching entry would leave the body unchanged —
	// the user had no way to actually "open" the result.
	if len(s.shown) == 0 {
		return
	}
	cur := s.app.currentKind()
	for _, sp := range s.shown {
		if sp.kind == cur {
			return // current page is still in the filtered list — nothing to do
		}
	}
	i := s.list.GetCurrentItem()
	if i < 0 || i >= len(s.shown) {
		i = 0
	}
	s.app.replaceTopKind(s.shown[i].kind)
	// pushBrowser steals focus; pull it back to the input so the user
	// can keep refining the filter.
	s.app.tv.SetFocus(s.input)
}

func (s *sidebar) populate() {
	s.suppressChange = true
	s.list.Clear()
	for _, sp := range s.shown {
		label := sp.display
		if label == "" {
			label = sp.kind
		}
		if sp.icon != "" {
			label = sp.icon + " " + label
		}
		s.list.AddItem(label, "", 0, nil)
	}
	s.highlightCurrent()
	s.suppressChange = false
}

// highlightCurrent moves the cursor onto the row whose kind matches the
// front page. Always called inside suppressChange so it never triggers
// a page swap. This is the ONLY path syncSidebar uses — no Clear, no
// re-AddItem, so it can't corrupt an in-flight SetCurrentItem.
func (s *sidebar) highlightCurrent() {
	cur := s.app.currentKind()
	if cur == "" {
		return
	}
	for i, sp := range s.shown {
		if sp.kind == cur {
			s.list.SetCurrentItem(i)
			return
		}
	}
}

// move is the input's arrow-key shim: nudges the list's selection by
// `delta` rows. Lets the user navigate without leaving the filter input.
func (s *sidebar) move(delta int) {
	if len(s.shown) == 0 {
		return
	}
	i := s.list.GetCurrentItem() + delta
	if i < 0 {
		i = 0
	}
	if i >= len(s.shown) {
		i = len(s.shown) - 1
	}
	s.list.SetCurrentItem(i)
}
