// Package ops holds exe-rex's workflows: creating, listing and removing VMs,
// installing and checking the setup. The CLI and the TUI both drive them,
// reporting progress through a *ui.UI.
package ops

// InvalidError is a bad argument (a VM name, a tag). The CLI exits 2 for it.
type InvalidError struct{ msg string }

func (e *InvalidError) Error() string { return e.msg }

func invalid(msg string) error { return &InvalidError{msg} }
