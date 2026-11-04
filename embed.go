// Package exedevrex holds the files exe-rex installs: the VM's first-boot
// script and the Rex command-palette actions.
package exedevrex

import _ "embed"

// SetupScript is exe-setup.sh, exe.dev's first-boot script for new VMs.
//
//go:embed exe-setup.sh
var SetupScript string

// PaletteLua is rex/exe-dev.lua, the Rex command-palette actions.
//
//go:embed rex/exe-dev.lua
var PaletteLua string
