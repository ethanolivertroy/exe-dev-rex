// Package cli implements exe-rex's subcommands.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethanolivertroy/exe-dev-rex/internal/buildinfo"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ops"
	"github.com/ethanolivertroy/exe-dev-rex/internal/tui"
	"golang.org/x/term"
)

// UsageError means bad arguments; main exits 2 for it.
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }
func usageError(msg string) error   { return &UsageError{msg} }

// IsUsage reports whether err is a UsageError or an argument ops rejected.
func IsUsage(err error) bool {
	var u *UsageError
	var inv *ops.InvalidError
	return errors.As(err, &u) || errors.As(err, &inv)
}

const usage = `exe-rex: exe.dev VMs as Rex remote hosts

Usage:
  exe-rex                                          open the TUI (setup wizard, then your VMs)
  exe-rex ls                                       your exe.dev Rex VMs and how they're doing
  exe-rex new [<name>] [ssh exe.dev new args...]   create a VM and add it to the Rex sidebar
  exe-rex rm [-y] [<name>]                         delete a VM, its Tailscale device and Rex host
  exe-rex app-host [--via ADDR] [--no-restart] add <label> <endpoint> [icon]
  exe-rex app-host [--no-restart] remove <endpoint>
  exe-rex app-host list                            the Rex app's sidebar host list
  exe-rex install [--dry-run] [--yes] [--persistent] [--auth-keys-only] [--vm-tag NAME]
                  [--no-palette] [--no-links] [--bin DIR] [--test]   set up or change settings
  exe-rex doctor                                   check every piece, change nothing
  exe-rex config [path | set KEY VALUE | unset KEY] settings file (install choices, default VM size)
  exe-rex version

--json (ls, new, rm, install, doctor, config, version): one JSON object on stdout with
"ok", progress on stderr, never a prompt. rm needs -y, install --yes or --dry-run.

Also runs as exe-new, exe-rm and rex-app-host (symlinks), like the old scripts.
`

// Main dispatches on the program name (multi-call) or the first argument.
func Main(ctx context.Context, argv []string) error {
	prog := filepath.Base(argv[0])
	args := argv[1:]
	switch prog {
	case "exe-new":
		return New(ctx, args)
	case "exe-rm":
		return Rm(ctx, args)
	case "rex-app-host":
		return AppHost(ctx, args)
	}
	if len(args) == 0 {
		if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
			return tui.Run(ctx)
		}
		fmt.Print(usage)
		return usageError("")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "new":
		return New(ctx, rest)
	case "rm":
		return Rm(ctx, rest)
	case "app-host":
		return AppHost(ctx, rest)
	case "install":
		return Install(ctx, rest)
	case "doctor":
		return Doctor(ctx, rest)
	case "ls":
		return Ls(ctx, rest)
	case "config":
		return ConfigCmd(ctx, rest)
	case "version", "--version":
		if _, j := takeJSON(rest); j {
			return emit(map[string]string{"version": buildinfo.Version}, nil)
		}
		fmt.Println("exe-rex", buildinfo.Version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return usageError("unknown command " + strings.TrimSpace(cmd))
}
