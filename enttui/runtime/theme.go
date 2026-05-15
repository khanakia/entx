package runtime

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Theme is a semantic palette. Fields are roles, never raw colors, so the
// whole UI restyles by swapping one struct. JSON-ready (hex strings) for a
// future user-supplied theme file — for now two built-ins: Catppuccin
// Mocha (dark, default) and Latte (light).
type Theme struct {
	Name string

	Bg          tcell.Color // app background (base)
	Surface     tcell.Color // panels / modals / dropdowns (contrast bg)
	SurfaceAlt  tcell.Color // status bar / sidebar background
	Border      tcell.Color // unfocused borders / graphics
	BorderFocus tcell.Color // focused border (accent)
	Accent      tcell.Color // primary accent / links / triggers
	Accent2     tcell.Color // secondary accent (was aqua)
	Title       tcell.Color // box titles
	Text        tcell.Color // primary foreground
	Muted       tcell.Color // secondary / disabled / row index
	SelectionBg tcell.Color // selected-row background
	SelectionFg tcell.Color // selected-row foreground
	Inverse     tcell.Color // fg drawn on an accent fill
	Success     tcell.Color
	Warning     tcell.Color
	Danger      tcell.Color
}

func hexColor(h string) tcell.Color { return tcell.GetColor(h) }

// Catppuccin Mocha — dark, default.
var themeDark = Theme{
	Name:        "dark",
	Bg:          hexColor("#1e1e2e"),
	Surface:     hexColor("#313244"),
	SurfaceAlt:  hexColor("#181825"),
	Border:      hexColor("#585b70"),
	BorderFocus: hexColor("#89b4fa"),
	Accent:      hexColor("#89b4fa"),
	Accent2:     hexColor("#74c7ec"),
	Title:       hexColor("#cba6f7"),
	Text:        hexColor("#cdd6f4"),
	Muted:       hexColor("#6c7086"),
	SelectionBg: hexColor("#45475a"),
	SelectionFg: hexColor("#cdd6f4"),
	Inverse:     hexColor("#11111b"),
	Success:     hexColor("#a6e3a1"),
	Warning:     hexColor("#f9e2af"),
	Danger:      hexColor("#f38ba8"),
}

// Catppuccin Latte — light.
var themeLight = Theme{
	Name:        "light",
	Bg:          hexColor("#eff1f5"),
	Surface:     hexColor("#ffffff"),
	SurfaceAlt:  hexColor("#e6e9ef"),
	Border:      hexColor("#acb0be"),
	BorderFocus: hexColor("#1e66f5"),
	Accent:      hexColor("#1e66f5"),
	Accent2:     hexColor("#209fb5"),
	Title:       hexColor("#8839ef"),
	Text:        hexColor("#4c4f69"),
	Muted:       hexColor("#8c8fa1"),
	SelectionBg: hexColor("#bcc0cc"),
	SelectionFg: hexColor("#4c4f69"),
	Inverse:     hexColor("#eff1f5"),
	Success:     hexColor("#40a02b"),
	Warning:     hexColor("#df8e1d"),
	Danger:      hexColor("#d20f39"),
}

// theme is the live palette. Mutable: toggleTheme swaps it and re-applies.
var theme = themeDark

// selStyle is the canonical selected-row style — high-contrast, bold, so
// it never degrades to the invisible tcell default (the black-on-blue bug).
func selStyle() tcell.Style {
	return tcell.StyleDefault.
		Background(theme.SelectionBg).
		Foreground(theme.SelectionFg).
		Bold(true)
}

// applyTheme installs the live palette into tview's global Styles AND
// remaps tcell's named-color table. The remap is the key lever: every
// existing inline `[yellow]` / `[red]` / `[gray]` tview color tag resolves
// through tcell.ColorNames, so the whole app re-themes (and switches on
// toggle) without touching the hundreds of tag sites in the source.
func applyTheme() {
	t := theme
	tview.Styles = tview.Theme{
		PrimitiveBackgroundColor:    t.Bg,
		ContrastBackgroundColor:     t.Surface,
		MoreContrastBackgroundColor: t.SelectionBg,
		BorderColor:                 t.Border,
		TitleColor:                  t.Title,
		GraphicsColor:               t.Border,
		PrimaryTextColor:            t.Text,
		SecondaryTextColor:          t.Accent2,
		TertiaryTextColor:           t.Accent,
		InverseTextColor:            t.Inverse,
		ContrastSecondaryTextColor:  t.Text,
	}
	for name, c := range map[string]tcell.Color{
		"white":         t.Text,
		"black":         t.Inverse,
		"gray":          t.Muted,
		"grey":          t.Muted,
		"silver":        t.Muted,
		"red":           t.Danger,
		"maroon":        t.Danger,
		"green":         t.Success,
		"lime":          t.Success,
		"yellow":        t.Warning,
		"orange":        t.Warning,
		"olive":         t.Warning,
		"blue":          t.Accent,
		"navy":          t.Accent,
		"dodgerblue":    t.Accent,
		"aqua":          t.Accent2,
		"teal":          t.Accent2,
		"cyan":          t.Accent2,
		"fuchsia":       t.Title,
		"purple":        t.Title,
		"darkslategray": t.SelectionBg,
	} {
		tcell.ColorNames[name] = c
	}
}

// toneColor maps semantic tones to a tview color-tag name. The names are
// remapped by applyTheme, so the returned tag follows the live theme.
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

// --- persistence -----------------------------------------------------------

func themeFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "github.com/khanakia/entx/enttui", "theme")
}

// loadPersistedTheme reads the saved theme name (best-effort). Default dark.
func loadPersistedTheme() {
	p := themeFilePath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	if strings.TrimSpace(string(b)) == "light" {
		theme = themeLight
	}
}

func persistTheme() {
	p := themeFilePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(theme.Name), 0o644)
}
