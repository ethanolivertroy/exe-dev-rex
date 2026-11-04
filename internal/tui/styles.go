package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

var (
	cyan   = lipgloss.Color("6")
	green  = lipgloss.Color("2")
	yellow = lipgloss.Color("3")
	red    = lipgloss.Color("1")
	grey   = lipgloss.Color("8")

	title    = lipgloss.NewStyle().Bold(true).Foreground(cyan)
	bold     = lipgloss.NewStyle().Bold(true)
	dim      = lipgloss.NewStyle().Foreground(grey)
	okStyle  = lipgloss.NewStyle().Foreground(green)
	warn     = lipgloss.NewStyle().Foreground(yellow)
	bad      = lipgloss.NewStyle().Foreground(red)
	selected = lipgloss.NewStyle().Reverse(true)
	box      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(grey).Padding(0, 1)
)

// renderEvent styles one line of command output by its kind.
func renderEvent(e ui.Event) string {
	switch e.Kind {
	case ui.KindSection:
		return bold.Render(e.Text)
	case ui.KindOK:
		if e.Took > 0 { // a progress-bar stage: "✓ Created the VM     1.4s"
			return okStyle.Render("  ✓ ") + pad(e.Text, 34) + dim.Render(e.TookString())
		}
		return okStyle.Render("  ✓") + " " + e.Text
	case ui.KindWarn:
		return warn.Render("  !") + " " + e.Text
	case ui.KindFail:
		return bad.Render("  ✗") + " " + e.Text
	case ui.KindTodo: // a dry run's skipped question reads as the change it would make
		return warn.Render("  →") + " " + e.Text
	case ui.KindDetail:
		return "      " + e.Text
	}
	return e.Text
}
