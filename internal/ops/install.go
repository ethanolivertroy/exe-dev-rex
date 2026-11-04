package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	exedevrex "github.com/ethanolivertroy/exe-dev-rex"
	"github.com/ethanolivertroy/exe-dev-rex/internal/buildinfo"
	"github.com/ethanolivertroy/exe-dev-rex/internal/config"
	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
	"github.com/ethanolivertroy/exe-dev-rex/internal/rexapp"
	"github.com/ethanolivertroy/exe-dev-rex/internal/run"
	"github.com/ethanolivertroy/exe-dev-rex/internal/tailnet"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

type installer struct {
	u                 *ui.UI
	dry, yes, palette bool
	links             bool
	authKeysOnly      bool
	vmTag             string
	persistent        bool
	binDir            string
	api               *tailnet.API
	token             string // from the TUI; memory only
	needToken         bool   // a dry run reached a step that needs a token
}

// InstallOptions are the installer's answers. The TUI fills them in from its
// wizard; the CLI from flags and the config file.
type InstallOptions struct {
	Dry, Yes       bool
	Palette, Links bool
	Persistent     bool
	AuthKeysOnly   bool
	VMTag, BinDir  string
	Token          string // Tailscale API token, used in memory only; "" = ask or TS_API_KEY
}

// RunInstall runs every install step with o, writing to u. needToken reports
// that a dry run without a token skipped work that needs one.
func RunInstall(ctx context.Context, u *ui.UI, o InstallOptions) (needToken bool, err error) {
	if !exedev.ValidTag(o.VMTag) {
		return false, invalid("VM tag: use lowercase letters, digits and dashes")
	}
	in := &installer{u: u, dry: o.Dry, yes: o.Yes, palette: o.Palette, links: o.Links,
		authKeysOnly: o.AuthKeysOnly, vmTag: o.VMTag, persistent: o.Persistent, binDir: o.BinDir, token: o.Token}
	if in.dry {
		u.Line(u.Dim(DryRunBanner))
	}
	for _, s := range []func(context.Context) error{in.prereqs, in.binary, in.tailscale, in.setupScript, in.paletteActions} {
		if err := s(ctx); err != nil {
			return in.needToken, err
		}
	}
	if !in.dry {
		if err := saveInstallChoices(o); err != nil {
			u.Warn("couldn't save your choices to %s: %v", config.Path(), err)
		}
	}
	return in.needToken, nil
}

// saveInstallChoices records the Mac-side answers, so a later install (or the
// TUI) starts from them instead of the defaults.
func saveInstallChoices(o InstallOptions) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	pal, lnk := o.Palette, o.Links
	c.Install.Palette, c.Install.Links = &pal, &lnk
	home, _ := os.UserHomeDir()
	if o.BinDir == filepath.Join(home, ".local", "bin") {
		c.Install.Bin = ""
	} else {
		c.Install.Bin = o.BinDir
	}
	return c.Save()
}

// DryRunBanner opens a dry run's output.
const DryRunBanner = "Dry run: nothing will be changed."

// ask is a yes/no question: --yes answers yes, --dry-run answers no.
func (in *installer) ask(q string) bool {
	if in.dry {
		in.u.WouldAsk(q)
		return false
	}
	return in.yes || ui.Confirm(q)
}

func (in *installer) prereqs(ctx context.Context) error {
	u := in.u
	u.Step("Checking prerequisites")
	out, _ := run.Output(ctx, "sw_vers", "-productVersion")
	if out == "" {
		return errors.New("this needs macOS (the Rex Mac app)")
	}
	u.OK("macOS %s", strings.TrimSpace(out))
	if m, _ := filepath.Glob("/Applications/Rex*.app"); len(m) == 0 {
		return errors.New("Rex for Mac isn't in /Applications; install it first")
	}
	u.OK("Rex app: %s", rexapp.AppName())
	for _, c := range []string{"rex", "tailscale", "ssh"} {
		if _, err := run.Output(ctx, "which", c); err != nil {
			return fmt.Errorf("%s isn't on your PATH (rex comes with the Rex app; Tailscale from tailscale.com)", c)
		}
	}
	u.OK("rex, tailscale, ssh")
	st, err := tailnet.Local(ctx)
	if err != nil || st.BackendState != "Running" {
		return errors.New("Tailscale isn't connected; connect it first")
	}
	u.OK("Tailscale connected, tailnet %s", st.MagicDNSSuffix)
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := exedev.Cmd(c, "whoami"); err != nil {
		return errors.New("can't reach exe.dev over SSH (ssh exe.dev whoami). Sign up at https://exe.dev and add your SSH key first")
	}
	u.OK("exe.dev SSH works")
	return nil
}

func sameFile(a, b string) bool {
	x, err1 := os.ReadFile(a)
	y, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

func (in *installer) binary(ctx context.Context) error {
	u := in.u
	u.Step("Installing exe-rex into " + in.binDir)
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	dst := filepath.Join(in.binDir, "exe-rex")
	switch {
	case self == dst:
		u.OK("running the installed exe-rex")
	case sameFile(self, dst):
		u.OK("exe-rex is up to date")
	case in.dry:
		u.Todo("would copy %s to %s", self, dst)
	default:
		if err := os.MkdirAll(in.binDir, 0o755); err != nil {
			return err
		}
		if err := copyFile(self, dst+".new", 0o755); err != nil {
			return err
		}
		if err := os.Rename(dst+".new", dst); err != nil {
			return err
		}
		u.OK("installed exe-rex %s", buildinfo.Version)
	}
	if !in.links {
		for _, n := range []string{"exe-new", "exe-rm", "rex-app-host"} {
			p := filepath.Join(in.binDir, n)
			if t, err := os.Readlink(p); err != nil || t != "exe-rex" {
				continue
			}
			if in.dry {
				u.Todo("would remove the %s link", n)
			} else if err := os.Remove(p); err != nil {
				return err
			} else {
				u.OK("removed the %s link (exe-rex %s works the same)", n, strings.TrimPrefix(strings.TrimPrefix(n, "exe-"), "rex-"))
			}
		}
	}
	if in.links {
		backup := filepath.Join(in.binDir, ".exe-rex-replaced")
		for _, n := range []string{"exe-new", "exe-rm", "rex-app-host"} {
			p := filepath.Join(in.binDir, n)
			if t, err := os.Readlink(p); err == nil && t == "exe-rex" {
				u.OK("%s → exe-rex", n)
				continue
			}
			if in.dry {
				u.Todo("would link %s → exe-rex (saving any existing script)", n)
				continue
			}
			if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
				_ = os.MkdirAll(backup, 0o755)
				saved := filepath.Join(backup, n+"."+time.Now().Format("20060102-150405"))
				if err := os.Rename(p, saved); err != nil {
					return err
				}
				u.OK("saved the old %s script as %s", n, saved)
			} else {
				_ = os.Remove(p)
			}
			if err := os.Symlink("exe-rex", p); err != nil {
				return err
			}
			u.OK("%s → exe-rex", n)
		}
	}
	if !strings.Contains(":"+os.Getenv("PATH")+":", ":"+in.binDir+":") {
		u.Warn("%s isn't on your PATH. Add to ~/.zshrc:  export PATH=\"%s:$PATH\"", in.binDir, in.binDir)
	} else {
		u.OK("%s is on your PATH", in.binDir)
	}
	if _, err := rexapp.Read(ctx); err != nil {
		u.Warn("can't read this Rex version's host list (%v); new VMs won't be added to the sidebar", err)
	} else {
		u.OK("the Rex app's host list is in the format exe-rex knows")
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	r, err := os.Open(src)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

var apiToken = regexp.MustCompile(`^tskey-api-[A-Za-z0-9-]+$`)

// tsAPI returns a Tailscale API client, asking for a token the first time.
// The token stays in memory.
func (in *installer) tsAPI(ctx context.Context, why string) (*tailnet.API, error) {
	if in.api != nil {
		return in.api, nil
	}
	tok := strings.TrimSpace(in.token)
	if tok == "" {
		tok = strings.TrimSpace(os.Getenv("TS_API_KEY"))
	}
	if tok == "" {
		if in.dry {
			in.needToken = true
			in.u.Todo("would ask for a Tailscale API access token to %s", why)
			return nil, nil
		}
		in.u.Todo("To %s, exe-rex needs a Tailscale API access token. In the admin console:", why)
		in.u.Detail("https://login.tailscale.com/admin/settings/keys → Generate access token…")
		in.u.Detail("(any description; 1 day expiry is plenty). It's used once, in memory, and never saved.")
		var err error
		if tok, err = ui.Secret("  Paste the token (tskey-api-…, not shown): "); err != nil {
			return nil, err
		}
	}
	if !apiToken.MatchString(tok) {
		return nil, errors.New("that isn't a Tailscale API access token (they start with tskey-api-)")
	}
	api := &tailnet.API{Token: tok}
	if _, _, err := api.GetPolicy(ctx); err != nil {
		return nil, fmt.Errorf("the token doesn't work: %w", err)
	}
	in.api = api
	return api, nil
}

func (in *installer) hasToken() bool {
	return strings.TrimSpace(in.token) != "" || os.Getenv("TS_API_KEY") != ""
}

func (in *installer) tailscale(ctx context.Context) error {
	u := in.u
	u.Step("Tailscale identity for the VMs")
	state, err := exedev.TailscaleIntegration(ctx)
	if err != nil {
		return err
	}
	ready := state.Exists && !state.Pending
	switch {
	case ready:
		u.OK("exe.dev integration %q is set up", exedev.Integration)
		if err := in.scopeToTag(ctx, state); err != nil {
			return err
		}
		if err := in.nodeMode(ctx, state); err != nil {
			return err
		}
	case state.Pending:
		u.Todo("%q exists but was never connected to a Tailscale trust credential", exedev.Integration)
	default:
		u.Todo("no %q integration yet", exedev.Integration)
	}

	// The policy: checked whenever a token is available, required when setting up.
	var api *tailnet.API
	if !ready || in.hasToken() {
		if api, err = in.tsAPI(ctx, "set up the tailnet policy and trust credential"); err != nil {
			return err
		}
	}
	if api != nil {
		if err := in.policy(ctx, api); err != nil {
			return err
		}
	} else if ready {
		u.OK("tailnet policy not checked (set TS_API_KEY to check it)")
		if state.Ephemeral == in.persistent {
			// The node type changes, so the credential's devices:core scope may
			// need to as well, and only the Tailscale API can see or change it.
			if in.dry {
				in.needToken = true
				u.Todo("would ask for a Tailscale API token to match the trust credential's scopes to the new node type")
			} else if api, err = in.tsAPI(ctx, "match the trust credential's scopes to the new node type"); err != nil {
				return err
			}
		}
	}
	if ready {
		if api != nil {
			return in.reviewScope(ctx, api, state.Audience)
		}
		return nil
	}

	if state.Pending {
		u.Warn("exe.dev only shows the integration's Issuer and Subject when it's created, so it has to be recreated")
		if !in.ask("Remove and recreate the " + exedev.Integration + " integration?") {
			u.Warn("skipped; new VMs can't join the tailnet until this is done")
			return nil
		}
		if err := exedev.RemoveTailscaleIntegration(ctx); err != nil {
			return err
		}
	}
	scopes := in.chooseScopes()
	if !state.Pending && !in.ask("Create the exe.dev integration and the Tailscale trust credential?") {
		u.Warn("skipped; new VMs can't join the tailnet until this is done")
		return nil
	}
	if in.dry {
		return nil
	}
	issuer, subject, err := exedev.CreateTailscaleIntegration(ctx, "tag:"+in.vmTag)
	if err != nil {
		return err
	}
	u.OK("created the exe.dev integration (issuer %s)", issuer)
	key, err := api.CreateFederatedIdentity(ctx, "exe-dev VMs for Rex", issuer, subject,
		scopes, []string{tailnet.ExeTag})
	if err != nil {
		return fmt.Errorf("creating the Tailscale trust credential: %w (the exe.dev integration exists; run install again)", err)
	}
	u.OK("created the Tailscale trust credential %s (scopes %s; tag:exe)", key.ID, strings.Join(scopes, ", "))
	// Tailscale documents the generated audience as api.tailscale.com/<client ID>,
	// so the Client ID comes from there, not from the key's id.
	if key.Audience == "" {
		if k, err := api.FederatedIdentity(ctx, key.ID); err == nil {
			key = k
		}
	}
	clientID, ok := strings.CutPrefix(key.Audience, "api.tailscale.com/")
	if !ok || clientID == "" {
		return fmt.Errorf("Tailscale created trust credential %s but returned no audience (%q); copy its Client ID from https://login.tailscale.com/admin/settings/trust-credentials and run: ssh exe.dev integrations edit %s --audience api.tailscale.com/<CLIENT_ID> --metadata=client_id=<CLIENT_ID> --metadata=tags=tag:exe --metadata=ephemeral=%t", key.ID, key.Audience, exedev.Integration, !in.persistent)
	}
	if err := exedev.SetTailscaleClient(ctx, key.Audience, clientID, !in.persistent); err != nil {
		return err
	}
	u.OK("connected the two: new VMs join your tailnet as tag:exe")
	if os.Getenv("TS_API_KEY") == "" {
		u.Todo("You can revoke the API token now: https://login.tailscale.com/admin/settings/keys")
	}
	return nil
}

// devicesCoreWarning is printed whenever a trust credential gets, or has, the
// devices:core scope. Tailscale's docs: it can "read the list of all devices in
// the tailnet, authorize or remove machines, and manipulate tags on devices";
// the credential's tags limit only the auth keys it creates.
func (in *installer) devicesCoreWarning() {
	u := in.u
	u.Warn("Automatic cleanup needs the Tailscale scope devices:core. Be aware what that allows:")
	for _, l := range []string{
		"Any process on any of your exe.dev VMs (including an agent you run there) can trade",
		"the VM's identity token for a Tailscale API token that can list, remove, expire and",
		"retag ANY machine on your tailnet, not just tag:exe VMs. Tailscale's docs:",
		"https://tailscale.com/docs/reference/trust-credentials#scopes",
		"Without it, exe-rex rm can't delete the VM's Tailscale device; you delete it under",
		"Machines in the Tailscale admin console instead.",
	} {
		u.Detail("%s", l)
	}
}

// chooseScopes decides the trust credential's scopes: auth_keys always, and
// devices:core (automatic device cleanup in exe-rex rm) unless declined.
func (in *installer) chooseScopes() []string {
	if !in.persistent {
		in.u.OK("scope auth_keys only: ephemeral VMs remove themselves by logging out")
		return []string{"auth_keys"}
	}
	if in.authKeysOnly {
		in.u.OK("auth keys only (--auth-keys-only): exe-rex rm will ask you to delete Tailscale devices yourself")
		return []string{"auth_keys"}
	}
	in.devicesCoreWarning()
	if in.dry {
		in.u.WouldAsk("Let exe-rex rm delete Tailscale devices automatically (devices:core)?")
		return []string{"auth_keys", "devices:core"}
	}
	if in.yes || ui.Confirm("Let exe-rex rm delete Tailscale devices automatically (devices:core)?") {
		return []string{"auth_keys", "devices:core"}
	}
	in.u.OK("auth keys only: exe-rex rm will ask you to delete Tailscale devices yourself")
	return []string{"auth_keys"}
}

// nodeMode makes the integration's ephemeral setting match --persistent.
// It applies to VMs created from now on; existing ones keep how they joined.
func (in *installer) nodeMode(ctx context.Context, state exedev.IntegrationState) error {
	u := in.u
	want := !in.persistent
	describe := func(eph bool) string {
		if eph {
			return "ephemeral nodes: exe-rex rm removes them by logging out, and Tailscale removes a VM that's gone for good within an hour (--persistent to keep them)"
		}
		return "persistent nodes: kept while a VM is off; exe-rex rm deletes them with devices:core (default is ephemeral)"
	}
	if state.Ephemeral == want {
		u.OK("new VMs join as %s", describe(want))
		return nil
	}
	u.Todo("new VMs would join as %s", describe(want))
	if in.dry {
		return nil
	}
	if err := exedev.SetTailscaleClient(ctx, state.Audience, state.ClientID, want); err != nil {
		return err
	}
	u.OK("updated; VMs you already have keep how they joined")
	if want && !in.hasToken() {
		u.Line(u.Dim("    Ephemeral VMs don't need the devices:core scope. If your trust credential has it,"))
		u.Line(u.Dim("    TS_API_KEY=<token> exe-rex install removes it (or remove Devices: Core in the console)."))
	}
	return nil
}

// reviewScope makes the existing trust credential's devices:core scope match
// the choice: persistent nodes with automatic cleanup need it, everything else
// doesn't, so it is offered for adding or removing.
func (in *installer) reviewScope(ctx context.Context, api *tailnet.API, audience string) error {
	if audience == "" {
		return nil
	}
	ids, err := api.FederatedIdentities(ctx)
	if err != nil {
		return nil
	}
	want := in.persistent && !in.authKeysOnly
	for _, k := range ids {
		if k.Audience != audience {
			continue
		}
		has := slices.Contains(k.Scopes, "devices:core")
		switch {
		case has == want && want:
			in.devicesCoreWarning()
			in.u.OK("the trust credential has devices:core, so exe-rex rm deletes persistent devices")
			return nil
		case has == want:
			in.u.OK("the trust credential has %s", strings.Join(k.Scopes, ", "))
			return nil
		}
		next := slices.DeleteFunc(slices.Clone(k.Scopes), func(s string) bool { return s == "devices:core" })
		q := "Remove devices:core from the trust credential?"
		if want {
			next = append(next, "devices:core")
			q = "Add devices:core to the trust credential, so exe-rex rm deletes persistent devices?"
			in.u.Todo("the trust credential lacks devices:core, which persistent nodes need for automatic cleanup")
			in.devicesCoreWarning()
		} else {
			in.u.Todo("the trust credential has devices:core, which isn't needed for this setup")
			in.devicesCoreWarning()
			in.u.Detail("%s", in.u.Dim("Persistent VMs you already have would then need deleting in the console on exe-rex rm."))
		}
		if !in.ask(q) {
			return nil
		}
		if err := api.SetFederatedIdentityScopes(ctx, k.ID, next); err != nil {
			return fmt.Errorf("changing the trust credential's scopes: %w", err)
		}
		in.u.OK("trust credential now has: %s", strings.Join(next, ", "))
		return nil
	}
	return nil
}

// scopeToTag makes tailscale-wif reachable only from VMs tagged in.vmTag.
// Setups from before this attached it to auto:all (every VM); those are
// migrated: VMs already on the tailnet as tag:exe get the exe.dev tag first,
// then the tag is attached, and only then is auto:all detached, so no VM
// loses access in between.
func (in *installer) scopeToTag(ctx context.Context, state exedev.IntegrationState) error {
	u := in.u
	want := "tag:" + in.vmTag
	var others []string
	for _, a := range state.Attachments {
		if a != want {
			others = append(others, a)
		}
	}
	if slices.Contains(state.Attachments, want) && len(others) == 0 {
		u.OK("only VMs tagged %s can get the Tailscale identity", in.vmTag)
		return nil
	}
	u.Todo("%s is attached to %s; exe-rex now limits it to VMs tagged %s",
		exedev.Integration, strings.Join(state.Attachments, ", "), in.vmTag)
	u.Line(u.Dim("    Afterwards, VMs made elsewhere (the web UI, other tools) can't get its Tailscale token."))
	// VMs that already joined with exe-setup.sh are tag:exe peers on the tailnet.
	vms, err := exedev.ListVMs(ctx)
	if err != nil {
		return err
	}
	var tagThese []string
	if st, err := tailnet.Local(ctx); err == nil {
		for _, v := range vms {
			if p, ok := st.Find(v.Name); ok && p.HasTag(tailnet.ExeTag) && !slices.Contains(v.Tags, in.vmTag) {
				tagThese = append(tagThese, v.Name)
			}
		}
	}
	if len(tagThese) > 0 {
		u.Line(fmt.Sprintf("    Already on your tailnet, so they get the tag %s: %s", in.vmTag, strings.Join(tagThese, ", ")))
	}
	if !in.ask("Limit " + exedev.Integration + " to VMs tagged " + in.vmTag + "?") {
		u.Warn("skipped; every VM you create can still get the Tailscale token")
		return nil
	}
	for _, v := range tagThese {
		if err := exedev.TagVM(ctx, v, in.vmTag); err != nil {
			return fmt.Errorf("tagging %s: %w", v, err)
		}
		u.OK("tagged %s %s", v, in.vmTag)
	}
	if !slices.Contains(state.Attachments, want) {
		if err := exedev.AttachIntegration(ctx, want); err != nil {
			return err
		}
		u.OK("attached %s to %s", exedev.Integration, want)
	}
	for _, a := range others {
		if err := exedev.DetachIntegration(ctx, a); err != nil {
			return err
		}
		u.OK("detached %s from %s", exedev.Integration, a)
	}
	return nil
}

func (in *installer) policy(ctx context.Context, api *tailnet.API) error {
	u := in.u
	cur, etag, err := api.GetPolicy(ctx)
	if err != nil {
		return err
	}
	next, changes, err := tailnet.EnsureExePolicy(cur)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		u.OK("tailnet policy has tag:exe and lets admins reach Rex on it (tcp:7297)")
		return nil
	}
	if err := api.ValidatePolicy(ctx, next); err != nil {
		return err
	}
	u.Todo("the tailnet policy needs:")
	for _, c := range changes {
		u.Detail("- %s", c)
	}
	u.Line(u.Dim("    Rex on the VMs admits anyone this grant lets through, so it's admins only."))
	u.Line(u.Dim("    Tailscale validated the edit. The diff (your comments are kept):"))
	for _, l := range strings.Split(strings.TrimRight(tailnet.Diff(cur, next), "\n"), "\n") {
		u.Line("    " + l)
	}
	if !in.ask("Apply this to your tailnet policy?") {
		u.Warn("skipped; VMs can't be reached until the tag and grant exist")
		return nil
	}
	if err := api.SetPolicy(ctx, next, etag); err != nil {
		var ae *tailnet.APIError
		if errors.As(err, &ae) && ae.Status == 412 {
			return errors.New("the policy changed while we were editing it; run install again")
		}
		return err
	}
	u.OK("updated the tailnet policy")
	return nil
}

// setupScript retires the account-wide default first-boot script. exe-rex new
// passes exe-setup.sh to each VM it creates (new --setup-script), so other VMs
// don't run it, and there's no question which of the two exe.dev would use.
func (in *installer) setupScript(ctx context.Context) error {
	u := in.u
	u.Step("First-boot script")
	cur, err := exedev.DefaultSetupScript(ctx)
	if err != nil {
		return err
	}
	switch {
	case cur == "":
		u.OK("exe-rex new passes exe-setup.sh to each VM it creates")
		return nil
	case !strings.Contains(cur, "tailscale-wif.int.exe.xyz"):
		u.OK("your account-wide default setup script isn't exe-rex's; leaving it alone")
		u.Warn("exe.dev doesn't document which script wins when exe-rex passes its own; if new VMs misbehave, check it")
		return nil
	}
	u.Todo("exe-setup.sh is your account-wide default, so every new VM runs it; exe-rex new now passes it per VM")
	if !in.ask("Remove the account-wide default setup script?") {
		u.Warn("kept; VMs made outside exe-rex will run it too")
		return nil
	}
	if err := exedev.DeleteDefaultSetupScript(ctx); err != nil {
		return err
	}
	u.OK("removed the account-wide default")
	return nil
}

var requireExe = regexp.MustCompile(`require\s*\(?\s*["']exe-dev["']`)

func rexConfigDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "rex")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rex")
}

func (in *installer) paletteActions(ctx context.Context) error {
	u := in.u
	u.Step("Rex command-palette actions")
	if !in.palette {
		return in.removePalette(ctx)
	}
	dir := rexConfigDir()
	initLua := filepath.Join(dir, "init.lua")
	cur, _ := os.ReadFile(initLua)
	if bytes.Contains(cur, []byte(`"exe_new"`)) {
		u.OK("your init.lua already defines exe_new / exe_rm; leaving it alone")
		return nil
	}
	lua := filepath.Join(dir, "exe-dev.lua")
	if old, err := os.ReadFile(lua); err == nil && string(old) == exedevrex.PaletteLua {
		u.OK("exe-dev.lua is up to date")
	} else if in.dry {
		u.Todo("would write %s", lua)
	} else {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(lua, []byte(exedevrex.PaletteLua), 0o644); err != nil {
			return err
		}
		u.OK("wrote exe-dev.lua")
	}
	if requireExe.Match(cur) {
		u.OK(`init.lua already has require("exe-dev")`)
	} else if in.ask(`Add require("exe-dev") to ` + initLua + "?") {
		f, err := os.OpenFile(initLua, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = f.WriteString(requireBlock)
		f.Close()
		if err != nil {
			return err
		}
		u.OK(`added require("exe-dev")`)
	}
	if in.dry {
		return nil
	}
	if out, err := run.Combined(ctx, "rex", "config", "check"); err != nil {
		u.Warn("rex config check reports problems:\n%s", out)
		return nil
	}
	if _, err := run.Combined(ctx, "rex", "config", "reload"); err != nil {
		u.Warn("couldn't reload the Rex config; run: rex config reload")
		return nil
	}
	u.OK("Rex config reloaded: New exe.dev VM… and Remove exe.dev VM… are in the palette")
	return nil
}

// requireBlock is what paletteActions appends to init.lua.
const requireBlock = "\n-- exe.dev VMs as Rex hosts (New exe.dev VM… / Remove exe.dev VM…)\nrequire(\"exe-dev\")\n"

// withoutRequire removes the lines paletteActions added to init.lua and
// nothing else. ok is false when they aren't there.
func withoutRequire(initLua string) (string, bool) {
	if i := strings.Index(initLua, requireBlock); i >= 0 {
		return initLua[:i] + initLua[i+len(requireBlock):], true
	}
	return initLua, false
}

// removePalette undoes paletteActions: our exe-dev.lua (only if unchanged) and
// our require block. A hand-written require or exe_new is left alone.
func (in *installer) removePalette(ctx context.Context) error {
	u := in.u
	dir := rexConfigDir()
	initLua := filepath.Join(dir, "init.lua")
	lua := filepath.Join(dir, "exe-dev.lua")
	cur, _ := os.ReadFile(initLua)
	next, hadBlock := withoutRequire(string(cur))
	old, err := os.ReadFile(lua)
	ours := err == nil && string(old) == exedevrex.PaletteLua
	if !hadBlock && !ours {
		if bytes.Contains(cur, []byte(`"exe_new"`)) {
			u.Warn("your init.lua defines exe_new / exe_rm itself; leaving them (remove them there to turn them off)")
		} else if requireExe.Match(cur) {
			u.Warn(`init.lua still has a require("exe-dev") that exe-rex didn't add; leaving it`)
		} else {
			u.OK("not installed (palette actions are off)")
		}
		return nil
	}
	if in.dry {
		u.Todo("would remove the New/Remove exe.dev VM… palette actions")
		return nil
	}
	if hadBlock {
		if err := os.WriteFile(initLua, []byte(next), 0o644); err != nil {
			return err
		}
	}
	if ours && !requireExe.MatchString(next) {
		if err := os.Remove(lua); err != nil {
			return err
		}
	}
	if _, err := run.Combined(ctx, "rex", "config", "reload"); err != nil {
		u.Warn("couldn't reload the Rex config; run: rex config reload")
	}
	u.OK("removed the palette actions")
	return nil
}
