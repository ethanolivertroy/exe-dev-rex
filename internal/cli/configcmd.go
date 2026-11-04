package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ethanolivertroy/exe-dev-rex/internal/config"
)

// ConfigCmd shows or changes ~/.config/exe-rex/config.toml.
func ConfigCmd(_ context.Context, args []string) error {
	const use = "usage: exe-rex config [path | set KEY VALUE | unset KEY]   (keys: " // completed below
	usage := use + strings.Join(config.Keys, ", ") + ")"
	args, asJSON := takeJSON(args)
	c, err := config.Load()
	if asJSON {
		if len(args) > 0 && args[0] != "set" && args[0] != "unset" {
			return emit(nil, usageError("usage: exe-rex config --json [set KEY VALUE | unset KEY]"))
		}
		if err == nil && len(args) > 0 {
			if len(args) == 3 && args[0] == "set" {
				err = config.Set(&c, args[1], args[2])
			} else if len(args) == 2 && args[0] == "unset" {
				err = config.Set(&c, args[1], "")
			} else {
				err = usageError(usage)
			}
			if err == nil {
				err = c.Save()
			}
		}
		_, statErr := os.Stat(config.Path())
		return emit(map[string]any{"path": config.Path(), "exists": statErr == nil, "config": c,
			"bin_dir": c.BinDir(), "palette": c.PaletteOn(), "links": c.LinksOn()}, err)
	}
	if err != nil {
		return err
	}
	switch {
	case len(args) == 0:
		b, err := os.ReadFile(config.Path())
		if err != nil {
			fmt.Printf("%s\nNo config file yet; the defaults are in use. Change one with: exe-rex config set new.cpu 4\n", config.Path())
			return nil
		}
		fmt.Printf("# %s\n%s", config.Path(), b)
		return nil
	case args[0] == "path" && len(args) == 1:
		fmt.Println(config.Path())
		return nil
	case args[0] == "set" && len(args) == 3, args[0] == "unset" && len(args) == 2:
		v := ""
		if args[0] == "set" {
			v = args[2]
		}
		if err := config.Set(&c, args[1], v); err != nil {
			return usageError(err.Error())
		}
		if err := c.Save(); err != nil {
			return err
		}
		fmt.Printf("saved %s\n", config.Path())
		return nil
	}
	return usageError(usage)
}
