package runtime

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// applyTheme installs a k9s-inspired palette. Backgrounds stay terminal-
// native (ColorDefault) so the app blends into whatever scheme the user
// already runs; only foreground / accent colors are set.
func applyTheme() {
	tview.Styles = tview.Theme{
		PrimitiveBackgroundColor:    tcell.ColorDefault, // terminal bg
		ContrastBackgroundColor:     tcell.ColorBlue,    // selection bg
		MoreContrastBackgroundColor: tcell.ColorDarkSlateGray,
		BorderColor:                 tcell.ColorDodgerBlue,
		TitleColor:                  tcell.ColorYellow,
		GraphicsColor:               tcell.ColorDodgerBlue,
		PrimaryTextColor:            tcell.ColorDefault, // terminal fg
		SecondaryTextColor:          tcell.ColorAqua,
		TertiaryTextColor:           tcell.ColorOrange,
		InverseTextColor:            tcell.ColorBlack,
		ContrastSecondaryTextColor:  tcell.ColorBlack,
	}
}

// toneColor maps semantic tones to readable tcell color names usable inside
// `[name]…[-]` tview color tags.
func toneColor(tone string) string {
	switch tone {
	case "success":
		return "green"
	case "warn":
		return "orange"
	case "danger":
		return "red"
	case "info":
		return "dodgerblue"
	case "muted":
		return "gray"
	default:
		return "white"
	}
}
