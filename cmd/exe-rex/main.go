// Command exe-rex creates exe.dev VMs and adds them to Rex as remote hosts.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethanolivertroy/exe-dev-rex/internal/cli"
	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := cli.Main(ctx, os.Args)
	exedev.CloseShared()
	switch {
	case err == nil:
	case cli.IsReported(err): // already printed as JSON
		os.Exit(1)
	case cli.IsUsage(err):
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "✗", err)
		os.Exit(1)
	}
}
