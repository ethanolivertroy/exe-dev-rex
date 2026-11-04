package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

// removeDeviceScript runs on the VM and removes its Tailscale device.
//
// Ephemeral nodes (the default): Tailscale removes them as soon as they log
// out. The rejoin timer is stopped first, or it would put the VM back on the
// tailnet before exe.dev deletes it.
//
// Persistent nodes don't go away on logout, so the VM trades its exe.dev
// identity token for a Tailscale API token and deletes its own device. That
// needs the devices:core scope on the trust credential.
const removeDeviceScript = `set -euo pipefail
if [[ "$(cat /etc/exe-rex/tailscale-ephemeral 2>/dev/null)" == true ]]; then
  sudo systemctl disable --now exe-rex-tailscale-rejoin.timer >/dev/null 2>&1 || true
  sudo tailscale logout
  echo EXE-REX-LOGGED-OUT
  exit 0
fi
W=https://tailscale-wif.int.exe.xyz
CID=$(curl -fsS "$W/metadata" | jq -er .client_id)
JWT=$(curl -fsS "$W/token" | jq -er .token)
TOK=$(curl -fsS -X POST https://api.tailscale.com/api/v2/oauth/token-exchange \
        -d "client_id=$CID" -d "jwt=$JWT" | jq -er .access_token)
DEV=$(tailscale status --json | jq -er .Self.ID)
# The node can't outlive its own deletion, so fire and check the status code.
code=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE \
        "https://api.tailscale.com/api/v2/device/$DEV" -H "Authorization: Bearer $TOK")
[[ "$code" == 200 ]] || { echo "Tailscale API said HTTP $code" >&2; exit 1; }
echo EXE-REX-DELETED
`

// RmResult is what RemoveVM did.
type RmResult struct {
	Name string `json:"name"`
	// Tailscale is what happened to its device: logged-out (ephemeral, gone),
	// logged-out-pending (Tailscale lists it for up to an hour), deleted
	// (persistent, via the API), or left (delete it in the console).
	Tailscale string  `json:"tailscale,omitempty"`
	Sidebar   bool    `json:"removed_from_sidebar"`
	Seconds   float64 `json:"seconds"`
}

// RemoveVM is exe-rex rm without the questions: it removes name from the Rex
// sidebar and the tailnet, then deletes the VM. All output goes to u.
func RemoveVM(ctx context.Context, u *ui.UI, name string) (RmResult, error) {
	return Default().RemoveVM(ctx, u, name)
}

// RemoveVM is RemoveVM in e.
func (e *Env) RemoveVM(ctx context.Context, u *ui.UI, name string) (res RmResult, err error) {
	res = RmResult{Name: name}
	all, err := e.ExeDev.ListVMs(ctx)
	if err != nil {
		return res, err
	}
	dest := map[string]string{}
	for _, v := range all {
		dest[v.Name] = v.SSHDest
	}
	if _, ok := dest[name]; !ok {
		return res, fmt.Errorf("no exe.dev VM called %s", name)
	}
	st, err := e.Tailnet.Local(ctx)
	if err != nil {
		return res, err
	}
	ep := name + "." + st.MagicDNSSuffix

	started := time.Now()
	u.Header("removing " + u.Bold(name))
	u.Status("working", "Removing "+name+" from Rex", -1, "")
	gone, err := e.Rex.RemoveHost(ctx, ep, true)
	switch {
	case err != nil:
		u.Warn("couldn't update Rex's host lists: %v", err)
	case len(gone) > 0:
		res.Sidebar = true
		u.OK("Removed from the Rex sidebar")
	default:
		u.OK("Not in the Rex sidebar")
	}

	u.Status("working", "Deleting "+name+"'s Tailscale device", -1, "")
	tsOK := false
	res.Tailscale = "left"
	if vm, err := e.ExeDev.VM(name, dest[name]); err == nil {
		c, cancel := context.WithTimeout(ctx, 60*time.Second)
		out, err := vm.Script(c, removeDeviceScript)
		cancel()
		vm.Close()
		switch {
		case err == nil && strings.Contains(out, "EXE-REX-LOGGED-OUT"):
			// Tailscale removes an ephemeral node on logout; check it really went.
			if tsOK = e.deviceGone(ctx, name); tsOK {
				res.Tailscale = "logged-out"
				u.OK("Removed its Tailscale device (logged out)")
			} else {
				u.Warn("Logged it out of Tailscale, but it's still listed; Tailscale removes ephemeral devices within an hour")
				tsOK, res.Tailscale = true, "logged-out-pending" // nothing for the user to do
			}
		case err == nil:
			tsOK, res.Tailscale = true, "deleted"
			u.OK("Deleted its Tailscale device")
		case strings.Contains(err.Error(), "403"):
			u.Warn("Couldn't delete its Tailscale device: it's a persistent node and the trust credential has auth keys only")
		default:
			u.Warn("Couldn't delete its Tailscale device: %v %s", err, strings.TrimSpace(out))
		}
	}

	u.Status("working", "Deleting VM "+name, -1, "")
	if err := e.ExeDev.Delete(ctx, name); err != nil {
		u.Status("error", "exe.dev couldn't delete "+name, -1, "")
		return res, fmt.Errorf("exe.dev couldn't delete %s: %w", name, err)
	}
	u.OK("Deleted the VM")
	e.ExeDev.ForgetHostKey(name)
	u.Rule()
	u.Line(fmt.Sprintf("  %s %s is gone %s", u.Green("●"), u.Bold(name), u.Dim(fmt.Sprintf("in %.0fs", time.Since(started).Seconds()))))

	if tsOK {
		u.Status("done", name+" removed", -1, "")
	} else {
		u.Line("")
		u.Line(fmt.Sprintf("    %s delete %q under Machines in the Tailscale console", u.Yellow("Left to do:"), name))
		u.Status("done", name+" removed; delete its Tailscale device in the console", -1, "")
	}
	res.Seconds = time.Since(started).Seconds()
	return res, nil
}

// deviceGone waits up to 10s for this Mac's tailscaled to stop listing name.
func (e *Env) deviceGone(ctx context.Context, name string) bool {
	for i := 0; i < 20; i++ {
		if st, err := e.Tailnet.Local(ctx); err == nil {
			if _, ok := st.Find(name); !ok {
				return true
			}
		}
		e.Sleep(ctx, 500*time.Millisecond)
	}
	return false
}
