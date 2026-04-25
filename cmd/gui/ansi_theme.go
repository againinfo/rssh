package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

const (
	ansiFgDefault = fyne.ThemeColorName("ansi-fg-default")
	ansiFgBlack   = fyne.ThemeColorName("ansi-fg-black")
	ansiFgRed     = fyne.ThemeColorName("ansi-fg-red")
	ansiFgGreen   = fyne.ThemeColorName("ansi-fg-green")
	ansiFgYellow  = fyne.ThemeColorName("ansi-fg-yellow")
	ansiFgBlue    = fyne.ThemeColorName("ansi-fg-blue")
	ansiFgMagenta = fyne.ThemeColorName("ansi-fg-magenta")
	ansiFgCyan    = fyne.ThemeColorName("ansi-fg-cyan")
	ansiFgWhite   = fyne.ThemeColorName("ansi-fg-white")

	ansiFgBrightBlack   = fyne.ThemeColorName("ansi-fg-bright-black")
	ansiFgBrightRed     = fyne.ThemeColorName("ansi-fg-bright-red")
	ansiFgBrightGreen   = fyne.ThemeColorName("ansi-fg-bright-green")
	ansiFgBrightYellow  = fyne.ThemeColorName("ansi-fg-bright-yellow")
	ansiFgBrightBlue    = fyne.ThemeColorName("ansi-fg-bright-blue")
	ansiFgBrightMagenta = fyne.ThemeColorName("ansi-fg-bright-magenta")
	ansiFgBrightCyan    = fyne.ThemeColorName("ansi-fg-bright-cyan")
	ansiFgBrightWhite   = fyne.ThemeColorName("ansi-fg-bright-white")
)

type ansiTheme struct {
	base fyne.Theme
}

func newANSITheme(base fyne.Theme) fyne.Theme {
	return &ansiTheme{base: base}
}

func (t *ansiTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case ansiFgDefault:
		return t.base.Color(theme.ColorNameForeground, variant)

	case ansiFgBlack:
		return color.NRGBA{R: 15, G: 23, B: 42, A: 255}
	case ansiFgRed:
		return color.NRGBA{R: 220, G: 38, B: 38, A: 255}
	case ansiFgGreen:
		return color.NRGBA{R: 22, G: 163, B: 74, A: 255}
	case ansiFgYellow:
		return color.NRGBA{R: 202, G: 138, B: 4, A: 255}
	case ansiFgBlue:
		return color.NRGBA{R: 37, G: 99, B: 235, A: 255}
	case ansiFgMagenta:
		return color.NRGBA{R: 192, G: 38, B: 211, A: 255}
	case ansiFgCyan:
		return color.NRGBA{R: 8, G: 145, B: 178, A: 255}
	case ansiFgWhite:
		return color.NRGBA{R: 226, G: 232, B: 240, A: 255}

	case ansiFgBrightBlack:
		return color.NRGBA{R: 100, G: 116, B: 139, A: 255}
	case ansiFgBrightRed:
		return color.NRGBA{R: 248, G: 113, B: 113, A: 255}
	case ansiFgBrightGreen:
		return color.NRGBA{R: 74, G: 222, B: 128, A: 255}
	case ansiFgBrightYellow:
		return color.NRGBA{R: 250, G: 204, B: 21, A: 255}
	case ansiFgBrightBlue:
		return color.NRGBA{R: 96, G: 165, B: 250, A: 255}
	case ansiFgBrightMagenta:
		return color.NRGBA{R: 232, G: 121, B: 249, A: 255}
	case ansiFgBrightCyan:
		return color.NRGBA{R: 34, G: 211, B: 238, A: 255}
	case ansiFgBrightWhite:
		return color.NRGBA{R: 248, G: 250, B: 252, A: 255}
	}

	return t.base.Color(name, variant)
}

func (t *ansiTheme) Font(style fyne.TextStyle) fyne.Resource    { return t.base.Font(style) }
func (t *ansiTheme) Icon(name fyne.ThemeIconName) fyne.Resource { return t.base.Icon(name) }
func (t *ansiTheme) Size(name fyne.ThemeSizeName) float32       { return t.base.Size(name) }
