package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
	"github.com/ethanolivertroy/exe-dev-rex/internal/rexapp"
	"github.com/ethanolivertroy/exe-dev-rex/internal/tailnet"
)

// VMRow is one exe-rex VM as `exe-rex ls` and the TUI show it.
type VMRow struct {
	Name     string    `json:"name"`
	Rex      string    `json:"rex"`  // ok, paused, unreachable, off-tailnet, or "" (not checked yet)
	Link     string    `json:"link"` // direct, relay, or ""
	Node     string    `json:"node"` // ephemeral, persistent, ? or ""
	Created  time.Time `json:"created_at"`
	HTTPSURL string    `json:"https_url,omitempty"`
	SSH      string    `json:"ssh"`      // ssh destination
	Endpoint string    `json:"endpoint"` // the Rex host address, <name>.<tailnet>
	Paused   bool      `json:"paused"`
}

// Overview is everything the VM table needs.
type Overview struct {
	Rows      []VMRow  `json:"vms"`
	Tag       string   `json:"tag"`       // exe.dev tag the integration is attached to
	Ephemeral bool     `json:"ephemeral"` // what new VMs join as
	Tailnet   string   `json:"tailnet"`   // MagicDNS suffix, "" if Tailscale is down
	Others    int      `json:"other_vms"` // VMs without the tag (not shown)
	Warnings  []string `json:"warnings"`
}

// ListRows gathers the VM table. The fast pass reads exe.dev's VM list and
// this Mac's tailscaled; deep also checks Rex, the connection type and the
// node type on each VM, in parallel.
func ListRows(ctx context.Context, deep bool) (Overview, error) {
	var ov Overview
	var (
		vms  []exedev.VMInfo
		ig   exedev.IntegrationState
		st   *tailnet.Status
		errV error
		errI error
		wg   sync.WaitGroup
	)
	wg.Add(3)
	go func() { defer wg.Done(); vms, errV = exedev.ListVMs(ctx) }()
	go func() { defer wg.Done(); ig, errI = exedev.TailscaleIntegration(ctx) }()
	go func() { defer wg.Done(); st, _ = tailnet.Local(ctx) }()
	wg.Wait()
	if errV != nil {
		return ov, errV
	}
	if errI == nil {
		ov.Tag, ov.Ephemeral = ig.VMTag(), ig.Ephemeral
		switch {
		case !ig.Exists || ig.Pending:
			ov.Warnings = append(ov.Warnings, "Tailscale identity isn't set up; new VMs can't join (open Settings)")
		case ig.AttachedAll:
			ov.Warnings = append(ov.Warnings, exedev.Integration+" is attached to every VM (auto:all); Settings limits it to the tag")
		}
	}
	if st != nil && st.BackendState == "Running" {
		ov.Tailnet = st.MagicDNSSuffix
	} else {
		ov.Warnings = append(ov.Warnings, "Tailscale isn't connected on this Mac")
	}
	have := map[string]bool{}
	for _, v := range vms {
		have[v.Name] = true
		if ov.Tag == "" || !slices.Contains(v.Tags, ov.Tag) {
			ov.Others++
			continue
		}
		r := VMRow{Name: v.Name, Created: v.CreatedAt, HTTPSURL: v.HTTPSURL, SSH: v.SSHDest,
			Paused: v.Status != "" && v.Status != "running"}
		if r.SSH == "" {
			r.SSH = v.Name + ".exe.xyz"
		}
		if ov.Tailnet != "" {
			r.Endpoint = v.Name + "." + ov.Tailnet
		}
		switch {
		case r.Paused:
			r.Rex = "paused"
		case st != nil:
			if p, ok := st.Find(v.Name); !ok {
				r.Rex = "off-tailnet"
			} else if !p.Online {
				r.Rex = "unreachable"
			}
		}
		ov.Rows = append(ov.Rows, r)
	}
	if st != nil {
		for _, p := range st.Peer {
			if p.HasTag(tailnet.ExeTag) && !have[p.HostName] {
				ov.Warnings = append(ov.Warnings, fmt.Sprintf("Tailscale device %s has no exe.dev VM; delete it under Machines in the Tailscale console", p.HostName))
			}
		}
	}
	if deep {
		var wg sync.WaitGroup
		for i := range ov.Rows {
			r := &ov.Rows[i]
			if r.Rex != "" || st == nil {
				continue
			}
			p, _ := st.Find(r.Name)
			wg.Add(3)
			go func() {
				defer wg.Done()
				if rexapp.Reachable(ctx, p.IPv4()+":7297", 2*time.Second) == nil {
					r.Rex = "ok"
				} else {
					r.Rex = "unreachable"
				}
			}()
			go func() {
				defer wg.Done()
				c, cancel := context.WithTimeout(ctx, 8*time.Second)
				defer cancel()
				if pr, err := tailnet.Ping(c, r.Name); err == nil {
					r.Link = map[bool]string{true: "direct", false: "relay"}[pr.Direct]
				}
			}()
			go func() {
				defer wg.Done()
				r.Node = nodeType(ctx, r.Name, r.SSH)
			}()
		}
		wg.Wait()
	}
	return ov, nil
}

var (
	nodeMu    sync.Mutex
	nodeCache = map[string]string{}
)

// nodeType reads how a VM joined the tailnet (exe-setup.sh records it). It
// doesn't change after the VM is created, so it's cached for the process.
func nodeType(ctx context.Context, name, dest string) string {
	nodeMu.Lock()
	t, ok := nodeCache[name]
	nodeMu.Unlock()
	if ok {
		return t
	}
	vm, err := exedev.NewVM(name)
	if err != nil {
		return "?"
	}
	vm.Dest = dest
	defer vm.Close()
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out, err := vm.Run(c, "cat /etc/exe-rex/tailscale-ephemeral 2>/dev/null || echo none")
	if err != nil {
		return "?"
	}
	t = "persistent" // VMs from before ephemeral have no file
	if strings.TrimSpace(out) == "true" {
		t = "ephemeral"
	}
	nodeMu.Lock()
	nodeCache[name] = t
	nodeMu.Unlock()
	return t
}

// AgeString is a short age: 45s, 12m, 3h, 2d.
func AgeString(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
