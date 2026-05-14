package runtime

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type picker struct {
	app   *App
	root  *tview.Flex
	input *tview.InputField
	list  *tview.List
	all   []*anySpec
	shown []*anySpec
}

func newPicker(app *App) *picker {
	p := &picker{app: app, all: app.kindListSortedByDisplay()}
	p.shown = p.all

	p.input = tview.NewInputField().
		SetLabel("kind › ").
		SetLabelColor(tcell.ColorYellow).
		SetFieldWidth(40).
		SetFieldBackgroundColor(tcell.ColorDefault)

	p.list = tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDodgerBlue).
		SetSelectedTextColor(tcell.ColorBlack)
	p.populate()

	p.input.SetChangedFunc(func(text string) {
		q := strings.ToLower(strings.TrimSpace(text))
		// Always allocate a fresh slice — never alias p.all and then
		// truncate it, that would mutate the master list.
		if q == "" {
			p.shown = append([]*anySpec(nil), p.all...)
		} else {
			p.shown = make([]*anySpec, 0, len(p.all))
			for _, s := range p.all {
				name := strings.ToLower(s.display)
				if name == "" {
					name = s.kind
				}
				if strings.Contains(name, q) || strings.Contains(s.kind, q) {
					p.shown = append(p.shown, s)
				}
			}
		}
		p.populate()
	})

	p.input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			p.choose()
		case tcell.KeyEscape:
			app.closePicker()
		}
	})

	// While the input has focus, arrow keys + ctrl+n/p drive the list
	// without losing the input — feels like fzf.
	p.input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			next := p.list.GetCurrentItem() + 1
			if next >= len(p.shown) {
				next = len(p.shown) - 1
			}
			if next < 0 {
				next = 0
			}
			p.list.SetCurrentItem(next)
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			prev := p.list.GetCurrentItem() - 1
			if prev < 0 {
				prev = 0
			}
			p.list.SetCurrentItem(prev)
			return nil
		case tcell.KeyPgDn:
			p.list.SetCurrentItem(min(p.list.GetCurrentItem()+10, len(p.shown)-1))
			return nil
		case tcell.KeyPgUp:
			p.list.SetCurrentItem(max(p.list.GetCurrentItem()-10, 0))
			return nil
		}
		return ev
	})

	p.list.SetSelectedFunc(func(int, string, string, rune) { p.choose() })
	p.list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			app.closePicker()
			return nil
		}
		return ev
	})

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.input, 1, 0, true).
		AddItem(p.list, 0, 1, false)
	body.SetBorder(true).
		SetTitle(" pick a kind ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	// Center the modal-ish body inside the page.
	p.root = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(
			tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(body, 20, 0, true).
				AddItem(nil, 0, 1, false),
			60, 0, true,
		).
		AddItem(nil, 0, 1, false)

	return p
}

func (p *picker) populate() {
	p.list.Clear()
	for _, s := range p.shown {
		label := s.display
		if label == "" {
			label = s.kind
		}
		if s.icon != "" {
			label = s.icon + " " + label
		}
		p.list.AddItem(label, "", 0, nil)
	}
}

func (p *picker) choose() {
	if len(p.shown) == 0 {
		return
	}
	i := p.list.GetCurrentItem()
	if i < 0 || i >= len(p.shown) {
		return
	}
	kind := p.shown[i].kind
	p.app.closePicker()
	p.app.pushBrowser(kind, "")
}
