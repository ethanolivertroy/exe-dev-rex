package ui

import (
	"fmt"
	"time"
)

// Kind is what an output line means. The terminal, --json and the TUI each
// render Events their own way; none of them parses formatted text.
type Kind string

const (
	KindSection Kind = "section" // "==> Setting up Tailscale"
	KindOK      Kind = "ok"      // ✓
	KindTodo    Kind = "todo"    // → a change (to make, or a dry run would make)
	KindWarn    Kind = "warn"    // !
	KindFail    Kind = "fail"    // ✗
	KindDetail  Kind = "detail"  // a continuation of the step before it
	KindInfo    Kind = "info"    // anything else: summary cards, notes, blank lines
)

// Event is one line of a command's output.
type Event struct {
	Kind Kind
	// Text is the message without its mark or indent. For KindInfo it is the
	// whole line as printed, indent included.
	Text string
	// Took is set on the KindOK line that ends a progress-bar stage.
	Took time.Duration
	// Stage marks a KindSection started by Bar.Begin (a progress-bar stage).
	Stage bool
	// WouldAsk marks a KindTodo for a question a dry run skipped; Text is the question.
	WouldAsk bool
}

// TookString formats Took the way every renderer shows it.
func (e Event) TookString() string { return fmt.Sprintf("%.1fs", e.Took.Seconds()) }

// render formats e for the terminal.
func (u *UI) render(e Event) string {
	switch e.Kind {
	case KindSection:
		if e.Stage {
			return "==> " + e.Text + "\n"
		}
		return "\n" + u.Bold("==> "+e.Text) + "\n"
	case KindOK:
		if e.Took > 0 {
			if u.TTY {
				return fmt.Sprintf("\r\x1b[2K  %s %-32s %s\n", u.Green("✓"), e.Text, u.Dim(fmt.Sprintf("%5s", e.TookString())))
			}
			return fmt.Sprintf("    done: %s (%s)\n", e.Text, e.TookString())
		}
		return "  " + u.Green("✓") + " " + e.Text + "\n"
	case KindTodo:
		if e.WouldAsk {
			return "  " + u.Yellow("→") + " " + u.Dim("(dry run) would ask: "+e.Text) + "\n"
		}
		return "  " + u.Yellow("→") + " " + e.Text + "\n"
	case KindWarn:
		return "  " + u.Yellow("!") + " " + e.Text + "\n"
	case KindFail:
		return "  " + u.Red("✗") + " " + e.Text + "\n"
	case KindDetail:
		return "      " + e.Text + "\n"
	default:
		return e.Text + "\n"
	}
}

// Emit sends e to the UI's sink, if any, and prints it on Out, if any.
func (u *UI) Emit(e Event) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.sink != nil {
		u.sink(e)
	}
	if u.Out != nil {
		fmt.Fprint(u.Out, u.render(e))
	}
}
