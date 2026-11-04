package cli

import (
	"context"
	"flag"

	"github.com/ethanolivertroy/exe-dev-rex/internal/config"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ops"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

// Doctor checks every piece exe-rex depends on and changes nothing.
// It exits non-zero when something needs fixing.
func Doctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	cfg, _ := config.Load()
	bin := fs.String("bin", cfg.BinDir(), "where exe-rex is installed")
	asJSON := fs.Bool("json", false, "print the checks as JSON")
	if err := fs.Parse(args); err != nil {
		return usageError("usage: exe-rex doctor [--bin DIR] [--json]   (set TS_API_KEY to also check the tailnet policy)")
	}
	if *asJSON {
		r, u := newRecorder()
		err := ops.RunDoctor(ctx, u, *bin)
		return emit(map[string]any{"checks": r.all(), "problems": r.with("fail"), "warnings": r.with("warn")}, err)
	}
	return ops.RunDoctor(ctx, ui.New(), *bin)
}
