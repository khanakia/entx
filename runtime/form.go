package runtime

import (
	"context"
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

	for _, f := range spec.formFields {
		key := f.Key
		switch f.Kind {
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
func openDeleteConfirm(app *App, spec *anySpec, row Row, onDone func()) {
	if spec.deleteRow == nil {
		return
	}
	modal := tview.NewModal().
		SetText("Delete " + spec.kind + " " + row.ID + "?\nThis cannot be undone.").
		AddButtons([]string{"Cancel", "Delete"}).
		SetDoneFunc(func(idx int, label string) {
			if label == "Delete" {
				ctx, cancel := context.WithTimeout(app.ctx, 10*time.Second)
				defer cancel()
				if err := spec.deleteRow(ctx, row.ID); err != nil {
					// Re-render with the error in place of the prompt.
					app.pages.RemovePage("delete-modal")
					openDeleteError(app, err)
					return
				}
				app.pages.RemovePage("delete-modal")
				if onDone != nil {
					onDone()
				}
				return
			}
			app.pages.RemovePage("delete-modal")
		})
	app.pages.AddPage("delete-modal", modal, true, true)
}

func openDeleteError(app *App, err error) {
	m := tview.NewModal().
		SetText("Delete failed:\n" + err.Error()).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(int, string) { app.pages.RemovePage("delete-error") })
	app.pages.AddPage("delete-error", m, true, true)
}
