package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ethanolivertroy/exe-dev-rex/internal/config"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ops"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

// installOptions resolves the CLI's answers: flags given on the command line
// win, then the config file, then the defaults.
func installOptions(fs *flag.FlagSet, cfg config.Config, noPalette, noLinks bool) (palette, links bool, bin string) {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	palette, links = cfg.PaletteOn(), cfg.LinksOn()
	if set["no-palette"] {
		palette = !noPalette
	}
	if set["no-links"] {
		links = !noLinks
	}
	bin = cfg.BinDir()
	if set["bin"] {
		bin = fs.Lookup("bin").Value.String()
	}
	return palette, links, bin
}

// Install sets up (or updates) everything: the binary, Tailscale, exe.dev, and
// the Rex palette actions. Every step checks what's there and skips it.
func Install(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "show what would change, change nothing")
	yes := fs.Bool("yes", false, "don't ask (still asks for a Tailscale API token if one is needed)")
	noPalette := fs.Bool("no-palette", false, "remove (or don't add) the Rex command-palette actions; remembered")
	vmTag := fs.String("vm-tag", "rex-host", "exe.dev tag for the VMs exe-rex creates; only VMs with it get the Tailscale identity")
	persistent := fs.Bool("persistent", false, "VMs join as persistent Tailscale nodes instead of ephemeral ones (kept while a VM is off for long; exe-rex rm then needs devices:core to delete them)")
	authOnly := fs.Bool("auth-keys-only", false, "with --persistent: don't give VMs the devices:core scope; exe-rex rm then can't delete Tailscale devices")
	noLinks := fs.Bool("no-links", false, "remove (or don't add) the exe-new, exe-rm and rex-app-host links; remembered")
	test := fs.Bool("test", false, "finish by creating and deleting a test VM (Rex restarts twice)")
	home, _ := os.UserHomeDir()
	fs.String("bin", filepath.Join(home, ".local", "bin"), "where to install exe-rex; remembered")
	asJSON := fs.Bool("json", false, "print what it did (or would do) as JSON; needs --yes or --dry-run")
	if err := fs.Parse(args); err != nil {
		return usageError("usage: exe-rex install [--dry-run] [--yes] [--no-palette] [--no-links] [--persistent] [--auth-keys-only] [--vm-tag NAME] [--test] [--bin DIR]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	palette, links, bin := installOptions(fs, cfg, *noPalette, *noLinks)
	if *asJSON {
		if !*yes && !*dry {
			return emit(nil, usageError("exe-rex install --json needs --yes or --dry-run (it can't ask questions)"))
		}
		ui.DisablePrompts() // a missing token fails instead of asking
		r, u := newRecorder()
		need, err := ops.RunInstall(ctx, u, ops.InstallOptions{Dry: *dry, Yes: *yes, Palette: palette, Links: links,
			Persistent: *persistent, AuthKeysOnly: *authOnly, VMTag: *vmTag, BinDir: bin})
		return emit(map[string]any{"dry_run": *dry, "needs_token": need, "changes": r.with("todo"),
			"warnings": r.with("warn"), "steps": r.all()}, err)
	}
	u := ui.New()
	u.Line(u.Bold("exe.dev VMs as Rex remote hosts: installer"))
	if _, err := ops.RunInstall(ctx, u, ops.InstallOptions{Dry: *dry, Yes: *yes, Palette: palette, Links: links,
		Persistent: *persistent, AuthKeysOnly: *authOnly, VMTag: *vmTag, BinDir: bin}); err != nil {
		return err
	}
	u.Step("Test")
	if *test && !*dry {
		name := "exe-install-test-" + time.Now().Format("150405")
		u.Todo("creating and deleting %s (Rex restarts twice; your sessions survive)", name)
		if err := New(ctx, []string{name}); err != nil {
			return err
		}
		if err := Rm(ctx, []string{"-y", name}); err != nil {
			return err
		}
		u.OK("test VM created, reached and removed")
	} else {
		u.OK("to try it:  exe-rex new test-vm   then   exe-rex rm test-vm   (or exe-rex install --test)")
	}
	u.Line("")
	u.Line(fmt.Sprintf("%s New VMs: %s, or %s in the Rex palette. Check everything: %s",
		u.Green("Done."), u.Bold("exe-rex new <name>"), u.Bold("New exe.dev VM…"), u.Bold("exe-rex doctor")))
	return nil
}
