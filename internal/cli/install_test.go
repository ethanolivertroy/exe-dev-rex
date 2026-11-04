package cli

import (
	"flag"
	"testing"

	"github.com/ethanolivertroy/exe-dev-rex/internal/config"
)

func parse(t *testing.T, args ...string) (*flag.FlagSet, *bool, *bool) {
	t.Helper()
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	np := fs.Bool("no-palette", false, "")
	nl := fs.Bool("no-links", false, "")
	fs.String("bin", "/default", "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs, np, nl
}

func TestInstallOptionsFromConfig(t *testing.T) {
	off := false
	cfg := config.Config{Install: config.Install{Palette: &off, Bin: "/cfg/bin"}}

	fs, np, nl := parse(t)
	pal, links, bin := installOptions(fs, cfg, *np, *nl)
	if pal || !links || bin != "/cfg/bin" {
		t.Fatalf("no flags: palette=%v links=%v bin=%s; want the config's choices", pal, links, bin)
	}

	on := true
	cfg.Install.Palette = &on
	fs, np, nl = parse(t, "--no-palette", "--bin", "/flag")
	pal, _, bin = installOptions(fs, cfg, *np, *nl)
	if pal || bin != "/flag" {
		t.Fatalf("flags must win: palette=%v bin=%s", pal, bin)
	}

	fs, np, nl = parse(t, "--no-palette=false")
	cfg.Install.Palette = &off
	if pal, _, _ = installOptions(fs, cfg, *np, *nl); !pal {
		t.Fatal("--no-palette=false must turn the palette back on")
	}
}
