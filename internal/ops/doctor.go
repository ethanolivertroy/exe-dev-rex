package ops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ethanolivertroy/exe-dev-rex/internal/buildinfo"
	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
	"github.com/ethanolivertroy/exe-dev-rex/internal/rexapp"
	"github.com/ethanolivertroy/exe-dev-rex/internal/run"
	"github.com/ethanolivertroy/exe-dev-rex/internal/tailnet"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

// RunDoctor is exe-rex doctor writing to u.
func RunDoctor(ctx context.Context, u *ui.UI, binDir string) error {
	bin := &binDir
	problems := 0
	bad := func(format string, a ...any) { problems++; u.Fail(format, a...) }

	u.Step("exe-rex")
	if fi, err := os.Stat(filepath.Join(*bin, "exe-rex")); err != nil || fi.Mode()&0o111 == 0 {
		bad("exe-rex isn't installed in %s (run: exe-rex install)", *bin)
	} else {
		u.OK("installed in %s (this is %s)", *bin, buildinfo.Version)
	}
	for _, n := range []string{"exe-new", "exe-rm", "rex-app-host"} {
		p := filepath.Join(*bin, n)
		if t, err := os.Readlink(p); err == nil && t == "exe-rex" {
			u.OK("%s → exe-rex", n)
		} else if _, err := os.Stat(p); err == nil {
			u.Warn("%s is the older bash script; exe-rex install replaces it", n)
		} else {
			u.Warn("%s isn't installed (optional; exe-rex %s works the same)", n, strings.TrimPrefix(strings.TrimPrefix(n, "exe-"), "rex-"))
		}
	}

	u.Step("Rex")
	if _, err := run.Output(ctx, "which", "rex"); err != nil {
		bad("the rex CLI isn't on your PATH")
	}
	list, err := rexapp.Read(ctx)
	if err != nil {
		bad("can't read the Rex app's host list: %v", err)
	} else {
		u.OK("%s; host list readable (%d hosts)", rexapp.AppName(), len(list.Hosts()))
	}

	u.Step("Tailscale")
	st, err := tailnet.Local(ctx)
	if err != nil || st.BackendState != "Running" {
		bad("Tailscale isn't connected")
		st = nil
	} else {
		u.OK("connected, tailnet %s", st.MagicDNSSuffix)
	}

	u.Step("exe.dev")
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	_, err = exedev.Cmd(c, "whoami")
	cancel()
	var vms []string
	var vmInfo []exedev.VMInfo
	tag := ""
	if err != nil {
		bad("ssh exe.dev doesn't work: %v", err)
	} else {
		u.OK("SSH works")
		if vmInfo, err = exedev.ListVMs(ctx); err != nil {
			bad("can't list VMs: %v", err)
		}
		for _, v := range vmInfo {
			vms = append(vms, v.Name)
		}
		if s, err := exedev.TailscaleIntegration(ctx); err != nil {
			bad("can't read integrations: %v", err)
		} else if !s.Exists {
			bad("no %s integration (run: exe-rex install)", exedev.Integration)
		} else if s.Pending {
			bad("%s was never connected to a Tailscale trust credential (run: exe-rex install)", exedev.Integration)
		} else if tag = s.VMTag(); tag == "" {
			bad("%s isn't attached to a VM tag (run: exe-rex install)", exedev.Integration)
		} else if s.AttachedAll {
			u.Warn("%s is attached to auto:all as well as tag:%s, so every VM can get its Tailscale token (exe-rex install fixes it)", exedev.Integration, tag)
		} else {
			u.OK("%s set up; only VMs tagged %s can get its Tailscale token", exedev.Integration, tag)
		}
		if cur, err := exedev.DefaultSetupScript(ctx); err != nil {
			bad("can't read the account-wide setup script: %v", err)
		} else if strings.Contains(cur, "tailscale-wif.int.exe.xyz") {
			u.Warn("exe-setup.sh is still your account-wide default, so every new VM runs it (exe-rex install removes it)")
		} else {
			u.OK("exe-rex new passes exe-setup.sh to each VM it creates")
		}
	}

	u.Step("Rex palette actions")
	cur, _ := os.ReadFile(filepath.Join(rexConfigDir(), "init.lua"))
	switch {
	case bytes.Contains(cur, []byte(`"exe_new"`)):
		u.OK("defined in init.lua")
	case requireExe.Match(cur):
		if _, err := os.Stat(filepath.Join(rexConfigDir(), "exe-dev.lua")); err != nil {
			bad(`init.lua requires "exe-dev" but exe-dev.lua is missing`)
		} else {
			u.OK(`init.lua loads exe-dev.lua`)
		}
	default:
		u.Warn("not set up (optional; exe-rex install adds them)")
	}
	if out, err := run.Combined(ctx, "rex", "config", "check"); err != nil {
		bad("rex config check: %s", strings.TrimSpace(out))
	}

	u.Step("Leftovers")
	if st != nil && vms != nil {
		have := map[string]bool{}
		for _, v := range vms {
			have[v] = true
		}
		clean := true
		suffix := "." + st.MagicDNSSuffix
		if list != nil {
			for _, h := range list.Hosts() {
				for _, l := range h.Locators {
					if n, ok := strings.CutSuffix(l, suffix); ok && strings.HasSuffix(h.Name, " - exe.dev") && !have[n] {
						clean = false
						u.Warn("sidebar host %q points at %s, which isn't an exe.dev VM any more (exe-rex app-host remove %s)", h.Name, l, l)
					}
				}
			}
		}
		for _, p := range st.Peer {
			if p.HasTag(tailnet.ExeTag) && !have[p.HostName] {
				clean = false
				u.Warn("Tailscale device %s (tag:exe) has no exe.dev VM; delete it under Machines in the console", p.HostName)
			}
		}
		if tag != "" {
			for _, v := range vmInfo {
				if p, ok := st.Find(v.Name); ok && p.HasTag(tailnet.ExeTag) && !slices.Contains(v.Tags, tag) {
					clean = false
					u.Warn("VM %s is on your tailnet but not tagged %s, so exe-rex rm can't delete its device (ssh exe.dev tag %s %s)", v.Name, tag, v.Name, tag)
				}
			}
		}
		if clean {
			u.OK("no orphaned Rex hosts or tag:exe devices")
		}
	} else {
		u.OK("skipped (needs Tailscale and exe.dev)")
	}

	u.Step("Connections")
	if st != nil {
		var peers []string
		for _, p := range st.Peer {
			if p.HasTag(tailnet.ExeTag) && p.Online {
				peers = append(peers, p.HostName)
			}
		}
		res := make([]tailnet.PingResult, len(peers))
		errs := make([]error, len(peers))
		var wg sync.WaitGroup
		for i, h := range peers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c, cancel := context.WithTimeout(ctx, 20*time.Second)
				defer cancel()
				res[i], errs[i] = tailnet.Ping(c, h)
			}()
		}
		wg.Wait()
		for i, h := range peers {
			switch {
			case errs[i] != nil:
				u.Warn("%s: %v", h, errs[i])
			case !res[i].Direct:
				u.Warn("%s is reached through Tailscale's relay %s (%s), not directly, so Rex will feel slower; a strict firewall or NAT is the usual cause", h, res[i].Via, res[i].Latency)
			default:
				u.OK("%s: direct, %s", h, res[i].Latency)
			}
		}
		if len(peers) == 0 {
			u.OK("no exe.dev VMs online to check")
		}
	}

	u.Step("Tailnet policy")
	if tok := os.Getenv("TS_API_KEY"); tok == "" {
		u.OK("not checked (set TS_API_KEY to check it)")
	} else {
		api := &tailnet.API{Token: tok}
		if pol, _, err := api.GetPolicy(ctx); err != nil {
			bad("can't read the policy: %v", err)
		} else if _, changes, err := tailnet.EnsureExePolicy(pol); err != nil {
			bad("%v", err)
		} else if len(changes) > 0 {
			for _, ch := range changes {
				bad("policy needs: %s (run: exe-rex install)", ch)
			}
		} else {
			u.OK("tag:exe and the Rex grant are in place")
		}
	}

	u.Line("")
	if problems > 0 {
		return fmt.Errorf("%d problem(s) found", problems)
	}
	u.Line(u.Green("Everything checks out."))
	return nil
}
