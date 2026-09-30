package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	cAccent = lipgloss.AdaptiveColor{Light: "#d20f39", Dark: "#f38ba8"}
	cBlue   = lipgloss.AdaptiveColor{Light: "#1e66f5", Dark: "#89b4fa"}
	cGreen  = lipgloss.AdaptiveColor{Light: "#40a02b", Dark: "#a6e3a1"}
	cYellow = lipgloss.AdaptiveColor{Light: "#df8e1d", Dark: "#f9e2af"}
	cRed    = lipgloss.AdaptiveColor{Light: "#d20f39", Dark: "#f38ba8"}
	cMauve  = lipgloss.AdaptiveColor{Light: "#8839ef", Dark: "#cba6f7"}
	cTeal   = lipgloss.AdaptiveColor{Light: "#179299", Dark: "#94e2d5"}
	cText   = lipgloss.AdaptiveColor{Light: "#4c4f69", Dark: "#cdd6f4"}
	cDim    = lipgloss.AdaptiveColor{Light: "#8c8fa1", Dark: "#7f849c"}
	cFaint  = lipgloss.AdaptiveColor{Light: "#ccd0da", Dark: "#313244"}
	cSelBg  = lipgloss.AdaptiveColor{Light: "#dce0e8", Dark: "#313244"}

	sLogo     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1e1e2e")).Background(cAccent).Padding(0, 1)
	sTab      = lipgloss.NewStyle().Foreground(cDim).Padding(0, 1)
	sTabOn    = lipgloss.NewStyle().Foreground(cText).Bold(true).Underline(true).Padding(0, 1)
	sDim      = lipgloss.NewStyle().Foreground(cDim)
	sText     = lipgloss.NewStyle().Foreground(cText)
	sBold     = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sHead     = lipgloss.NewStyle().Foreground(cDim).Bold(true)
	sSel      = lipgloss.NewStyle().Background(cSelBg)
	sTitle    = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	sMoney    = lipgloss.NewStyle().Foreground(cGreen)
	sWarn     = lipgloss.NewStyle().Foreground(cYellow)
	sErr      = lipgloss.NewStyle().Foreground(cRed)
	sKey      = lipgloss.NewStyle().Foreground(cYellow).Bold(true)
	sBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cFaint).Padding(0, 1)
	sModal    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBlue).Padding(1, 2)
	sUser     = lipgloss.NewStyle().Foreground(cTeal).Bold(true)
	sAgentMsg = lipgloss.NewStyle().Foreground(cMauve).Bold(true)
)

var agentColors = map[string]lipgloss.AdaptiveColor{
	"claude":       {Light: "#fe640b", Dark: "#fab387"},
	"codex":        {Light: "#40a02b", Dark: "#a6e3a1"},
	"opencode":     {Light: "#1e66f5", Dark: "#89b4fa"},
	"gemini":       {Light: "#8839ef", Dark: "#cba6f7"},
	"cursor-agent": {Light: "#179299", Dark: "#94e2d5"},
}

func agentStyle(a string) lipgloss.Style {
	c, ok := agentColors[a]
	if !ok {
		c = cText
	}
	return lipgloss.NewStyle().Foreground(c)
}

// cell fits s into exactly w columns.
func cell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// rcell right-aligns s in w columns.
func rcell(s string, w int) string {
	if lipgloss.Width(s) > w {
		return ansi.Truncate(s, w, "…")
	}
	return strings.Repeat(" ", w-lipgloss.Width(s)) + s
}

func bar(frac float64, width int, col lipgloss.TerminalColor) string {
	if width <= 0 {
		return ""
	}
	if frac < 0 {
		frac = 0
	}
	n := int(frac*float64(width) + 0.5)
	if n > width {
		n = width
	}
	return lipgloss.NewStyle().Foreground(col).Render(strings.Repeat("━", n)) + lipgloss.NewStyle().Foreground(cFaint).Render(strings.Repeat("━", width-n))
}

func pctColor(f float64) lipgloss.TerminalColor {
	switch {
	case f >= 0.9:
		return cRed
	case f >= 0.6:
		return cYellow
	}
	return cGreen
}

// fitLines cuts a block to h lines of at most w columns each.
func fitLines(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		lines[i] = cell(l, w)
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", max(w, 0)))
	}
	return strings.Join(lines, "\n")
}
