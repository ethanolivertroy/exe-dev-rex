package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ops"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

// Rm tears down a VM made with New: its Rex hosts, its Tailscale device, and the VM.
func Rm(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	yes := fs.Bool("y", false, "don't ask for confirmation")
	asJSON := fs.Bool("json", false, "print the result as JSON (needs -y and a name)")
	if err := fs.Parse(args); err != nil {
		return usageError("usage: exe-rex rm [-y] [--json] <name>")
	}
	if *asJSON {
		if !*yes || fs.NArg() != 1 {
			return emit(nil, usageError("usage: exe-rex rm --json -y <name>"))
		}
		r, u := newRecorder()
		res, err := ops.RemoveVM(ctx, u, fs.Arg(0))
		return emit(struct {
			ops.RmResult
			Steps []Step `json:"steps"`
		}{res, r.actions()}, err)
	}
	u := ui.New()
	name := fs.Arg(0)
	all, err := exedev.ListVMs(ctx)
	if err != nil {
		return err
	}
	var vms []string
	for _, v := range all {
		vms = append(vms, v.Name)
	}
	if name == "" {
		u.Line("Your VMs: " + strings.Join(vms, ", "))
		u.Status("blocked", "Which VM to delete", -1, "question")
		if name, err = ui.Prompt("VM to delete (name without .exe.xyz): "); err != nil || name == "" {
			return usageError("usage: exe-rex rm [-y] <name>")
		}
	}
	if !slices.Contains(vms, name) {
		u.Status("error", "No exe.dev VM called "+name, -1, "")
		return fmt.Errorf("no exe.dev VM called %s", name)
	}
	if !*yes {
		u.Status("blocked", "Confirm deleting "+name, -1, "permission")
		a, err := ui.Prompt(fmt.Sprintf("Delete exe.dev VM %s, its Rex host and its Tailscale device? [y/N] ", name))
		if err != nil || !strings.HasPrefix(strings.ToLower(a), "y") {
			u.Status("idle", "Cancelled", -1, "")
			return errors.New("cancelled")
		}
	}
	_, err = ops.RemoveVM(ctx, u, name)
	return err
}
