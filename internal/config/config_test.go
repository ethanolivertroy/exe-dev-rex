package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func tmp(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestLoadMissing(t *testing.T) {
	tmp(t)
	c, err := Load()
	if err != nil || c != (Config{}) {
		t.Fatalf("got %+v, %v", c, err)
	}
	if !c.PaletteOn() || !c.LinksOn() {
		t.Fatal("palette and links default on")
	}
}

func TestRoundTrip(t *testing.T) {
	tmp(t)
	off := false
	c := Config{Install: Install{Bin: "~/bin", Palette: &off}, New: NewDefaults{CPU: 4, Memory: "8GB"}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(Path())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi.Mode(), err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.PaletteOn() || !got.LinksOn() || got.New.CPU != 4 || got.New.Memory != "8GB" || got.Install.Bin != "~/bin" {
		t.Fatalf("got %+v", got)
	}
	home, _ := os.UserHomeDir()
	if got.BinDir() != filepath.Join(home, "bin") {
		t.Fatalf("BinDir %s", got.BinDir())
	}
}

func TestUnknownKeysIgnored(t *testing.T) {
	tmp(t)
	os.MkdirAll(filepath.Dir(Path()), 0o755)
	os.WriteFile(Path(), []byte("future = 1\n[new]\ncpu = 2\nwhatever = \"x\"\n"), 0o600)
	c, err := Load()
	if err != nil || c.New.CPU != 2 {
		t.Fatalf("got %+v, %v", c, err)
	}
}

func TestNewArgsOverride(t *testing.T) {
	got := NewDefaults{CPU: 4, Memory: "8GB"}.Args([]string{"--cpu=8"})
	if !slices.Equal(got, []string{"--memory=8GB"}) {
		t.Fatalf("got %q", got)
	}
	got = NewDefaults{CPU: 4, Disk: "50GB"}.Args([]string{"--disk", "20GB"})
	if !slices.Equal(got, []string{"--cpu=4"}) {
		t.Fatalf("got %q", got)
	}
	if got := (NewDefaults{}).Args(nil); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestSet(t *testing.T) {
	var c Config
	if err := Set(&c, "new.cpu", "4"); err != nil || c.New.CPU != 4 {
		t.Fatal(err, c)
	}
	if err := Set(&c, "install.palette", "false"); err != nil || c.PaletteOn() {
		t.Fatal(err, c)
	}
	if err := Set(&c, "new.cpu", "lots"); err == nil {
		t.Fatal("want error for non-number")
	}
	if err := Set(&c, "tag", "x"); err == nil {
		t.Fatal("want error for unknown key")
	}
}

func TestSaveOmitsZero(t *testing.T) {
	tmp(t)
	if err := (Config{}).Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path())
	if strings.Contains(string(b), "cpu") {
		t.Fatalf("an unset cpu must not be written:\n%s", b)
	}
}
