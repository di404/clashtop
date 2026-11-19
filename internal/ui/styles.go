package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
)

// Low-contrast colors (muted text, axis, borders, selection) are derived from the
// terminal's real foreground/background, so they stay readable on any theme
// instead of assuming a near-black background.
var (
	cAccent, cDown, cUp, cMuted, cFaint, cText, cSelBg, cErr, cWarn, cOK, cDebug lipgloss.Color

	sTitle, sMuted, sFaint, sText, sDown, sUp, sErr, sWarn, sOK, sDebug,
	sHeader, sSel, sTabOn, sTabOff, sKey, sBox, sBoxTitle lipgloss.Style
)

func init() { applyPalette(defaultFg, defaultBg) }

var (
	defaultFg, _ = colorful.Hex("#d8dbe2")
	defaultBg, _ = colorful.Hex("#16181d")
)

// detectPalette queries the terminal (OSC 10/11) for its colors. Call it before
// the TUI takes over the terminal.
func detectPalette() {
	out := termenv.NewOutput(os.Stdout)
	fg, bg := defaultFg, defaultBg
	if c, ok := out.ForegroundColor().(termenv.RGBColor); ok {
		fg = termenv.ConvertToRGB(c)
	}
	if c, ok := out.BackgroundColor().(termenv.RGBColor); ok {
		bg = termenv.ConvertToRGB(c)
	}
	applyPalette(fg, bg)
	lipgloss.SetHasDarkBackground(isDark(bg))
}

func isDark(c colorful.Color) bool {
	_, _, l := c.Hsl()
	return l < 0.5
}

func hex(c colorful.Color) lipgloss.Color { return lipgloss.Color(c.Clamped().Hex()) }

// mix returns a color t of the way from bg to fg.
func mix(bg, fg colorful.Color, t float64) lipgloss.Color { return hex(bg.BlendLab(fg, t)) }

func applyPalette(fg, bg colorful.Color) {
	pick := func(light, dark string) colorful.Color {
		s := dark
		if !isDark(bg) {
			s = light
		}
		c, _ := colorful.Hex(s)
		return c
	}
	accent := pick("#0b7fab", "#7dcfff")
	cAccent = hex(accent)
	cDown = hex(pick("#178a5b", "#5fd7a7"))
	cUp = hex(pick("#b2650b", "#f0b35a"))
	cErr = hex(pick("#c62828", "#ff6b6b"))
	cWarn = hex(pick("#a66a00", "#ffcb6b"))
	cOK = hex(pick("#178a5b", "#7bd88f"))
	cDebug = hex(pick("#8a63d2", "#b392f0"))
	cText = hex(fg)
	cMuted = mix(bg, fg, 0.62)
	cFaint = mix(bg, fg, 0.40)
	cSelBg = mix(bg, accent, 0.25)

	sTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sMuted = lipgloss.NewStyle().Foreground(cMuted)
	sFaint = lipgloss.NewStyle().Foreground(cFaint)
	sText = lipgloss.NewStyle().Foreground(cText)
	sDown = lipgloss.NewStyle().Foreground(cDown)
	sUp = lipgloss.NewStyle().Foreground(cUp)
	sErr = lipgloss.NewStyle().Foreground(cErr)
	sWarn = lipgloss.NewStyle().Foreground(cWarn)
	sOK = lipgloss.NewStyle().Foreground(cOK)
	sDebug = lipgloss.NewStyle().Foreground(cDebug)
	sHeader = lipgloss.NewStyle().Bold(true).Foreground(cMuted)
	sSel = lipgloss.NewStyle().Background(cSelBg)
	sTabOn = lipgloss.NewStyle().Bold(true).Foreground(cAccent).Underline(true)
	sTabOff = lipgloss.NewStyle().Foreground(cMuted)
	sKey = lipgloss.NewStyle().Foreground(cAccent)
	sBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cFaint).Padding(0, 1)
	sBoxTitle = lipgloss.NewStyle().Foreground(cMuted)
}

func levelStyle(level string) lipgloss.Style {
	switch level {
	case "error":
		return sErr
	case "warning", "warn":
		return sWarn
	case "info":
		return sOK
	case "debug":
		return sDebug
	}
	return sMuted
}
