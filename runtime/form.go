package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Form modal for editing one row or creating a new one. Driven entirely
// by spec.formFields + spec.update / spec.create — the runtime knows
// nothing about specific entity shapes.
//
// Widget choice per field kind:
//   - string / stringPtr / time / scalar  → tview.InputField (text)
//   - enum / enumPtr                      → tview.DropDown over EnumValues
//
// The form collects everything as strings; the generated update/create
// closure parses each into the typed ent setter (handles SetNillable*,
// enum cast, time.Parse, strconv) and surfaces validation errors
// through the modal's status bar.

// openEditForm shows the edit modal pre-filled with the focused row's
// current column values. Closes with esc; saves with ctrl+s.
//
// Surfaces a visible "no editable fields" notice when the spec has no
// Editable() annotations — without this the keybinding looked broken
// (silent no-op). `onSaved` is invoked AFTER a successful save so the
// caller can refresh the row list — calling refresh BEFORE the modal
// closes was the bug (refresh ran while the modal was still open and
// no save had happened yet).
func openEditForm(app *App, spec *anySpec, row Row, notify func(string), onSaved func()) {
	if len(spec.formFields) == 0 || spec.update == nil {
		if notify != nil {
			notify("no editable fields — annotate with enttui.Editable() per field")
		}
		return
	}
	prefill := map[string]string{}
	for k, v := range row.Columns {
		prefill[k] = v
	}
	openForm(app, spec, prefill, "edit "+spec.kind+" / "+row.ID,
		func(vals map[string]string) error {
			ctx, cancel := context.WithTimeout(app.ctx, 10*time.Second)
			defer cancel()
			return spec.update(ctx, row.ID, vals)
		},
		onSaved,
	)
}

// openCreateForm shows the new-row modal. Empty pre-fill except for
// scope keys (e.g. tenant_id, project_id) — those are injected so the
// new row lands in the right scope without the user retyping them.
//
// `notify` surfaces a status hint when the entity hasn't opted into
// enttui.AllowCreate; without it the keybinding looked broken.
func openCreateForm(app *App, spec *anySpec, notify func(string), onSaved func()) {
	if spec.create == nil {
		if notify != nil {
			notify("create not enabled — add enttui.AllowCreate{} to the schema")
		}
		return
	}
	if len(spec.formFields) == 0 {
		if notify != nil {
			notify("no editable fields — annotate with enttui.Editable() per field")
		}
		return
	}
	prefill := map[string]string{}
	for k, v := range app.Scope() {
		prefill[k] = v
	}
	openForm(app, spec, prefill, "new "+spec.kind,
		func(vals map[string]string) error {
			ctx, cancel := context.WithTimeout(app.ctx, 10*time.Second)
			defer cancel()
			// Scope keys go in even if no form widget shows them — the
			// generated Create closure looks for them by key.
			for k, v := range app.Scope() {
				if _, ok := vals[k]; !ok {
					vals[k] = v
				}
			}
			_, err := spec.create(ctx, vals)
			return err
		},
		onSaved,
	)
}

// openRefPicker shows a searchable list of the target kind's rows
// (by rowLabel) and calls pick(id, label) with the chosen row. Used by
// ref FormFields so the user selects a parent instead of typing an
// opaque foreign-key id. v1: first 1000 rows + in-memory fzf filter,
// scope-aware — same shape as the enum/column pickers.
func openRefPicker(app *App, refKind string, pick func(id, label string)) {
	cs := app.specs[refKind]
	if cs == nil {
		app.flash("ref target kind '" + refKind + "' not registered")
		return
	}
	ctx, cancel := context.WithTimeout(app.ctx, 5*time.Second)
	rows, _, err := cs.fetch(ctx, ListOpts{Limit: 1000, Scope: app.Scope()})
	cancel()
	if err != nil {
		app.flash("ref load failed: " + err.Error())
		return
	}

	input := tview.NewInputField().
		SetLabel("pick " + cs.display + " › ").
		SetLabelColor(tcell.ColorYellow).
		SetFieldWidth(40).
		SetFieldBackgroundColor(tcell.ColorDefault)

	list := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDodgerBlue).
		SetSelectedTextColor(tcell.ColorBlack)

	shown := append([]Row(nil), rows...)
	repaint := func() {
		list.Clear()
		// First entry clears the FK (only meaningful for nullable, but
		// harmless otherwise — generated setter no-ops empty for
		// required fields).
		list.AddItem("[gray](clear)[-]", "", 0, nil)
		for _, r := range shown {
			list.AddItem(rowLabel(r), "", 0, nil)
		}
		list.SetCurrentItem(0)
	}
	repaint()

	close := func() { app.pages.RemovePage("ref-picker") }
	commit := func() {
		i := list.GetCurrentItem()
		if i == 0 {
			pick("", "(none)")
			close()
			return
		}
		r := shown[i-1]
		pick(r.ID, rowLabel(r))
		close()
	}

	input.SetChangedFunc(func(text string) {
		q := strings.ToLower(strings.TrimSpace(text))
		if q == "" {
			shown = append([]Row(nil), rows...)
		} else {
			shown = shown[:0]
			for _, r := range rows {
				if strings.Contains(strings.ToLower(rowLabel(r)+" "+r.ID), q) {
					shown = append(shown, r)
				}
			}
		}
		repaint()
	})
	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			if c := list.GetCurrentItem() + 1; c < list.GetItemCount() {
				list.SetCurrentItem(c)
			}
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			if c := list.GetCurrentItem() - 1; c >= 0 {
				list.SetCurrentItem(c)
			}
			return nil
		case tcell.KeyEnter:
			commit()
			return nil
		case tcell.KeyEscape:
			close()
			return nil
		}
		return ev
	})
	list.SetSelectedFunc(func(int, string, string, rune) { commit() })

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(list, 0, 1, false).
		AddItem(tview.NewTextView().
			SetTextColor(tcell.ColorGray).
			SetText(" type to filter · ↑/↓ · enter pick · (clear) unsets · esc cancel "), 1, 0, false)
	body.SetBorder(true).
		SetTitle(" pick " + cs.display + " ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)
	body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			close()
			return nil
		}
		return ev
	})

	app.pages.AddPage("ref-picker", centerModal(body, 70, 24), true, true)
	app.tv.SetFocus(input)
}

// openForm is the shared implementation. submit runs the typed save and
// returns an error; on success the modal closes and onSaved fires so
// the caller can refresh its row list AFTER the DB write completed.
func openForm(app *App, spec *anySpec, prefill map[string]string, title string, submit func(map[string]string) error, onSaved func()) {
	values := make(map[string]string, len(spec.formFields))
	for _, f := range spec.formFields {
		values[f.Key] = prefill[f.Key]
	}

	status := tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(tcell.ColorGray)

	form := tview.NewForm().
		SetButtonsAlign(tview.AlignCenter).
		SetButtonBackgroundColor(tcell.ColorDarkSlateGray).
		SetButtonTextColor(tcell.ColorWhite).
		SetLabelColor(tcell.ColorYellow)

	// Ref fields get a post-loop InputCapture wired to a picker; track
	// (label, key, RefKind) so we can fetch the *tview.InputField after
	// the form is built.
	type refWire struct{ label, key, refKind string }
	var refWires []refWire

	for _, f := range spec.formFields {
		key := f.Key
		switch f.Kind {
		case "ref":
			label := f.Label
			if f.Required {
				label += " *"
			}
			label += " ↵pick"
			// Display the current FK value (id) until the user picks;
			// the changed func is nil so typing can't corrupt the id —
			// only the picker writes values[key].
			form.AddInputField(label, values[key], 0, nil, nil)
			refWires = append(refWires, refWire{label: label, key: key, refKind: f.RefKind})
		case "enum", "enumPtr":
			opts := append([]string(nil), f.EnumValues...)
			// Prepend a blank option for enumPtr so the user can clear it.
			if f.Kind == "enumPtr" {
				opts = append([]string{""}, opts...)
			}
			initial := 0
			for i, v := range opts {
				if v == values[key] {
					initial = i
					break
				}
			}
			label := f.Label
			if f.Required {
				label += " *"
			}
			form.AddDropDown(label, opts, initial, func(option string, _ int) {
				values[key] = option
			})
		default:
			label := f.Label
			if f.Required {
				label += " *"
			}
			form.AddInputField(label, values[key], 0, nil, func(text string) {
				values[key] = text
			})
		}
	}

	cancelBtn := func() { app.pages.RemovePage("form-modal") }

	saveBtn := func() {
		// Front-end required check before round-tripping to the DB.
		for _, f := range spec.formFields {
			if f.Required && values[f.Key] == "" {
				status.SetText("[red]" + f.Label + " is required[-]")
				return
			}
		}
		status.SetText("[gray]saving…[-]")
		// Run save synchronously — the contexts have 10s timeouts so
		// the UI won't wedge on slow DBs.
		if err := submit(values); err != nil {
			status.SetText("[red]save failed: " + err.Error() + "[-]")
			return
		}
		app.pages.RemovePage("form-modal")
		if onSaved != nil {
			onSaved()
		}
	}

	// Wire each ref field: Enter on it opens a picker of the target
	// kind's rows; the chosen row's ID is stored, its label shown.
	for _, rw := range refWires {
		item := form.GetFormItemByLabel(rw.label)
		in, ok := item.(*tview.InputField)
		if !ok {
			continue
		}
		key, refKind, input := rw.key, rw.refKind, in
		in.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			if ev.Key() == tcell.KeyEnter {
				openRefPicker(app, refKind, func(id, label string) {
					values[key] = id
					input.SetText(label)
				})
				return nil
			}
			// Swallow text edits — the id is picker-managed only.
			if ev.Key() == tcell.KeyRune || ev.Key() == tcell.KeyBackspace || ev.Key() == tcell.KeyBackspace2 {
				return nil
			}
			return ev
		})
	}

	form.AddButton("Save", saveBtn).
		AddButton("Cancel", cancelBtn)

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true).
		AddItem(status, 1, 0, false).
		AddItem(tview.NewTextView().
			SetText(" tab next · ctrl+s save · esc cancel ").
			SetTextColor(tcell.ColorGray), 1, 0, false)
	body.SetBorder(true).
		SetTitle(" " + title + " ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDodgerBlue)

	body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			cancelBtn()
			return nil
		case tcell.KeyCtrlS:
			saveBtn()
			return nil
		}
		return ev
	})

	height := len(spec.formFields)*2 + 6
	if height > 32 {
		height = 32
	}
	app.pages.AddPage("form-modal", centerModal(body, 70, height), true, true)
	app.tv.SetFocus(form)
}

// openDeleteConfirm shows a yes/no overlay; runs spec.deleteRow on Y.
// Surfaces a status hint when the entity hasn't opted in via
// enttui.AllowDelete{} — without this `D` looked broken.
func openDeleteConfirm(app *App, spec *anySpec, row Row, notify func(string), onDone func()) {
	if spec.deleteRow == nil {
		if notify != nil {
			notify("delete not enabled — add enttui.AllowDelete{} to the schema")
		}
		return
	}
	// Roll our own confirm dialog rather than tview.Modal. Reason: the
	// built-in modal's focused-button style is invisible in many color
	// schemes — users couldn't tell whether tab/arrows were doing
	// anything. A Form gives us proper DarkSlateGray button bg with
	// terminal default for the focused button.
	prompt := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText("Delete [yellow]" + spec.kind + " " + row.ID + "[-]?\nThis cannot be undone.")

	form := tview.NewForm().
		SetButtonsAlign(tview.AlignCenter).
		SetButtonBackgroundColor(tcell.ColorDarkSlateGray).
		SetButtonTextColor(tcell.ColorWhite)

	close := func() { app.pages.RemovePage("delete-modal") }

	form.AddButton("Cancel", close).
		AddButton("Delete", func() {
			ctx, cancelCtx := context.WithTimeout(app.ctx, 10*time.Second)
			defer cancelCtx()
			if err := spec.deleteRow(ctx, row.ID); err != nil {
				close()
				openDeleteError(app, err)
				return
			}
			close()
			if onDone != nil {
				onDone()
			}
		})

	hint := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetTextColor(tcell.ColorGray).
		SetText("← → / tab : switch · enter : confirm · esc : cancel")

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(prompt, 0, 1, false).
		AddItem(form, 3, 0, true).
		AddItem(hint, 1, 0, false)
	body.SetBorder(true).
		SetTitle(" confirm delete ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorRed)

	body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyLeft:
			// tview.Form only moves focus on Tab/Backtab by default —
			// remap arrows so ← / → feel natural between buttons.
			return tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
		case tcell.KeyRight:
			return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		}
		return ev
	})

	app.pages.AddPage("delete-modal", centerModal(body, 60, 9), true, true)
	app.tv.SetFocus(form)
}

func openDeleteError(app *App, err error) {
	close := func() { app.pages.RemovePage("delete-error") }
	msg := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetTextColor(tcell.ColorRed).
		SetText("Delete failed:\n" + err.Error())
	form := tview.NewForm().
		SetButtonsAlign(tview.AlignCenter).
		SetButtonBackgroundColor(tcell.ColorDarkSlateGray).
		SetButtonTextColor(tcell.ColorWhite).
		AddButton("OK", close)
	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(msg, 0, 1, false).
		AddItem(form, 3, 0, true)
	body.SetBorder(true).
		SetTitle(" error ").
		SetTitleColor(tcell.ColorRed).
		SetBorderColor(tcell.ColorRed)
	body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyEnter {
			close()
			return nil
		}
		return ev
	})
	app.pages.AddPage("delete-error", centerModal(body, 60, 9), true, true)
	app.tv.SetFocus(form)
}
