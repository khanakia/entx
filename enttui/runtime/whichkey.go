package runtime

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// which-key: the `,` leader popup. Frequent keys stay top-level and
// vim-faithful; the long tail of verbs/toggles lives here so they're
// DISCOVERED, not memorized. A wkItem's Fn may itself call openWhichKey
// to nest a submenu (e.g. `,t` → toggles).
type wkItem struct {
	Key   rune
	Label string
	Fn    func()
}

// openWhichKey renders a centered popup of "key  label" rows and waits
// for the next keypress: a matching Key runs its Fn (after closing the
// popup), esc cancels. Unmatched keys are ignored (popup stays).
func openWhichKey(app *App, title string, items []wkItem) {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)

	var b string
	for _, it := range items {
		b += "  [yellow::b]" + string(it.Key) + "[-:-:-]  " + it.Label + "\n"
	}
	tv.SetText(b)

	close := func() { app.pages.RemovePage("which-key") }

	tv.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			close()
			return nil
		}
		r := ev.Rune()
		for _, it := range items {
			if it.Key == r {
				close()
				if it.Fn != nil {
					it.Fn()
				}
				return nil
			}
		}
		return nil // swallow; leader menu is modal until esc or a hit
	})

	// Height = rows + border + title; capped.
	h := len(items) + 2
	if h > 22 {
		h = 22
	}
	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tv, 0, 1, true).
		AddItem(tview.NewTextView().
			SetTextColor(theme.Muted).
			SetText(" press a key · esc cancel "), 1, 0, false)
	body.SetBorder(true).
		SetTitle(" " + title + " ").
		SetTitleColor(theme.Title).
		SetBorderColor(theme.Border)

	app.pages.AddPage("which-key", centerModal(body, 56, h+2), true, true)
	app.tv.SetFocus(tv)
}
