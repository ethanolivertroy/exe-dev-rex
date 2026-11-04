package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethanolivertroy/exe-dev-rex/internal/ops"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

// New creates an exe.dev VM and adds it to the Rex app's sidebar.
//
// It waits until this Mac's tailscaled lists the VM before connecting, and
// connects by tailnet IP: asking DNS for the name before the VM exists got
// the "no such name" answer cached, which blocked connections for ~57s.
func New(ctx context.Context, args []string) error {
	args, asJSON := takeJSON(args)
	if asJSON {
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return emit(nil, usageError("usage: exe-rex new --json <name> [ssh exe.dev new args...]"))
		}
		r, u := newRecorder()
		res, err := ops.CreateVM(ctx, u, args[0], args[1:])
		return emit(struct {
			ops.NewResult
			Steps []Step `json:"steps"`
		}{res, r.actions()}, err)
	}
	u := ui.New()
	name, extra := "", []string(nil)
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, extra = args[0], args[1:]
	} else {
		extra = args
	}
	if name == "" {
		def := "vm-" + time.Now().Format("0102-1504")
		u.Status("blocked", "Name for the new VM", -1, "question")
		a, err := ui.Prompt(fmt.Sprintf("Name for the new exe.dev VM [%s]: ", def))
		if err != nil {
			return usageError("usage: exe-rex new <name> [ssh exe.dev new args...]")
		}
		if name = a; name == "" {
			name = def
		}
	}
	_, err := ops.CreateVM(ctx, u, name, extra)
	return err
}
