package ui

// NewSink returns a UI that hands each Event to fn instead of printing it:
// no terminal, no escape codes, no notifications. The TUI runs the same
// commands as the CLI through it.
func NewSink(fn func(Event)) *UI {
	return &UI{App: "exe-dev", NoNotify: true, sink: fn}
}

// Tee returns a copy of u that also hands each Event to fn. --json uses it
// to print plain progress on stderr while keeping the steps.
func Tee(u *UI, fn func(Event)) *UI {
	return &UI{TTY: u.TTY, Out: u.Out, App: u.App, NoNotify: u.NoNotify, sink: fn}
}
