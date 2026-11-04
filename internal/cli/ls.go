package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/ethanolivertroy/exe-dev-rex/internal/ops"
)

// Ls prints the VM table.
func Ls(ctx context.Context, args []string) error {
	args, asJSON := takeJSON(args)
	if len(args) > 0 {
		return usageError("usage: exe-rex ls [--json]")
	}
	ov, err := ops.ListRows(ctx, true)
	if asJSON {
		if ov.Rows == nil {
			ov.Rows = []ops.VMRow{}
		}
		if ov.Warnings == nil {
			ov.Warnings = []string{}
		}
		return emit(ov, err)
	}
	if err != nil {
		return err
	}
	for _, w := range ov.Warnings {
		fmt.Println("! " + w)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tREX\tLINK\tNODE\tAGE")
	for _, r := range ov.Rows {
		age := "—"
		if !r.Created.IsZero() {
			age = ops.AgeString(time.Since(r.Created))
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Name, dash(r.Rex), dash(r.Link), dash(r.Node), age)
	}
	tw.Flush()
	if len(ov.Rows) == 0 {
		fmt.Println("(no VMs yet: exe-rex new <name>)")
	}
	if ov.Others > 0 {
		fmt.Printf("%d other exe.dev VM(s) without the %s tag not shown\n", ov.Others, ov.Tag)
	}
	return nil
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
