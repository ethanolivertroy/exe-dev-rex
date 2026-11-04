// Package config reads and writes ~/.config/exe-rex/config.toml: the
// Mac-side choices (install folder, palette actions, links) and the default
// size for new VMs.
//
// Account-level settings (the VM tag, ephemeral or persistent nodes, the
// trust credential's scopes) are deliberately not here: they live in exe.dev
// and Tailscale, are shared by all your Macs, and are read from there.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/ethanolivertroy/exe-dev-rex/internal/resources"
)

type Config struct {
	Install Install     `toml:"install" json:"install"`
	New     NewDefaults `toml:"new" json:"new"`
}

// Install holds the installer's choices. A nil bool means "never chosen"
// (the default, on); false means the user declined, and re-runs respect it.
type Install struct {
	Bin     string `toml:"bin,omitempty" json:"bin,omitempty"`
	Palette *bool  `toml:"palette,omitempty" json:"palette,omitempty"`
	Links   *bool  `toml:"links,omitempty" json:"links,omitempty"`
}

// NewDefaults are passed to ssh exe.dev new. Empty fields use exe.dev's defaults.
type NewDefaults struct {
	CPU    int    `toml:"cpu,omitzero" json:"cpu,omitempty"`
	Memory string `toml:"memory,omitempty" json:"memory,omitempty"`
	Disk   string `toml:"disk,omitempty" json:"disk,omitempty"`
}

// Path is $XDG_CONFIG_HOME/exe-rex/config.toml, or ~/.config/exe-rex/config.toml.
func Path() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "exe-rex", "config.toml")
}

// Load reads the config. A missing file is not an error; unknown keys are ignored.
func Load() (Config, error) {
	var c Config
	b, err := os.ReadFile(Path())
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if _, err := toml.Decode(string(b), &c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", Path(), err)
	}
	return c, nil
}

// Save writes the config atomically, readable only by you.
func (c Config) Save() error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# exe-rex settings. `exe-rex` (Settings) and `exe-rex config set` write this file.\n")
	b.WriteString("# The VM tag and node type live in your exe.dev account, not here.\n\n")
	if err := toml.NewEncoder(&b).Encode(c); err != nil {
		return err
	}
	tmp := p + ".new"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func on(b *bool) bool { return b == nil || *b }

func (c Config) PaletteOn() bool { return on(c.Install.Palette) }
func (c Config) LinksOn() bool   { return on(c.Install.Links) }

// BinDir is where exe-rex is installed: the configured folder (install
// records it), else ~/.local/bin.
func (c Config) BinDir() string {
	home, _ := os.UserHomeDir()
	if b := c.Install.Bin; b != "" {
		if rest, ok := strings.CutPrefix(b, "~"); ok {
			return filepath.Join(home, rest)
		}
		return b
	}
	return filepath.Join(home, ".local", "bin")
}

// Args turns the defaults into ssh exe.dev new flags, leaving out any flag
// the user passed explicitly.
func (n NewDefaults) Args(explicit []string) []string {
	has := func(flag string) bool {
		return slices.ContainsFunc(explicit, func(a string) bool {
			return a == flag || strings.HasPrefix(a, flag+"=")
		})
	}
	var out []string
	if n.CPU > 0 && !has("--cpu") {
		out = append(out, "--cpu="+strconv.Itoa(n.CPU))
	}
	if n.Memory != "" && !has("--memory") {
		out = append(out, "--memory="+n.Memory)
	}
	if n.Disk != "" && !has("--disk") {
		out = append(out, "--disk="+n.Disk)
	}
	return out
}

// Keys lists the settings Set accepts.
var Keys = []string{"install.bin", "install.palette", "install.links", "new.cpu", "new.memory", "new.disk"}

// Set changes one setting by its dotted key. An empty value clears it.
func Set(c *Config, key, value string) error {
	boolp := func() (*bool, error) {
		if value == "" {
			return nil, nil
		}
		v, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("%s: want true or false", key)
		}
		return &v, nil
	}
	var err error
	switch key {
	case "install.bin":
		c.Install.Bin = value
	case "install.palette":
		c.Install.Palette, err = boolp()
	case "install.links":
		c.Install.Links, err = boolp()
	case "new.cpu":
		if value == "" {
			c.New.CPU = 0
		} else if c.New.CPU, err = strconv.Atoi(value); err != nil || c.New.CPU < 1 {
			c.New.CPU, err = 0, fmt.Errorf("new.cpu: want a whole number of CPUs")
		}
	case "new.memory":
		if err := resources.ValidateSize(value); err != nil {
			return fmt.Errorf("new.memory: %w", err)
		}
		c.New.Memory = resources.SizeArgument(value)
	case "new.disk":
		if err := resources.ValidateSize(value); err != nil {
			return fmt.Errorf("new.disk: %w", err)
		}
		c.New.Disk = resources.SizeArgument(value)
	default:
		return fmt.Errorf("unknown setting %q (settings: %s)", key, strings.Join(Keys, ", "))
	}
	return err
}
