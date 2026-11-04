package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethanolivertroy/exe-dev-rex/internal/rexapp"
)

const appHostUsage = `usage: exe-rex app-host [--via ADDR] [--no-restart] add <label> <endpoint> [icon]
       exe-rex app-host [--no-restart] remove <endpoint>
       exe-rex app-host list

  --via ADDR     reach the host at ADDR (e.g. 100.x.y.z:7297) for its server.status,
                 instead of resolving <endpoint>
  --no-restart   write the list without quitting Rex; the change shows the next time
                 Rex starts (a running Rex doesn't reload it, and doesn't overwrite it on quit)`

// AppHost adds, removes and lists hosts in the Rex app's sidebar.
func AppHost(ctx context.Context, args []string) error {
	via, restart := "", true
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--via":
			if len(args) < 2 {
				return usageError(appHostUsage)
			}
			via, args = args[1], args[2:]
		case "--no-restart":
			restart, args = false, args[1:]
		default:
			return usageError(appHostUsage)
		}
	}
	if len(args) == 0 {
		return usageError(appHostUsage)
	}
	switch args[0] {
	case "list":
		l, err := rexapp.Read(ctx)
		if err != nil {
			return err
		}
		for _, h := range l.Hosts() {
			fmt.Printf("%-30s %s\n", h.Name, strings.Join(h.Locators, " "))
		}
		return nil
	case "add":
		if len(args) < 3 {
			return usageError(appHostUsage)
		}
		icon := ""
		if len(args) > 3 {
			icon = args[3]
		}
		if err := rexapp.AddHost(ctx, rexapp.AddOptions{Label: args[1], Endpoint: args[2], Icon: icon, Via: via, Restart: restart}); err != nil {
			return err
		}
		suffix := ""
		if !restart {
			suffix = "; it shows the next time Rex starts"
		}
		fmt.Printf("Added %q (%s) to the Rex app%s\n", args[1], args[2], suffix)
		return nil
	case "remove":
		if len(args) < 2 {
			return usageError(appHostUsage)
		}
		gone, err := rexapp.RemoveHost(ctx, args[1], restart)
		if err != nil {
			return err
		}
		if len(gone) == 0 {
			return fmt.Errorf("no app host with endpoint %s (removed any server host entries)", args[1])
		}
		fmt.Printf("Removed %s (%s) from the Rex app\n", args[1], strings.Join(gone, ","))
		return nil
	}
	return usageError(appHostUsage)
}
