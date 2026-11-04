// Package buildinfo holds the exe-rex version.
package buildinfo

import "runtime/debug"

// Version is set at build time with -ldflags "-X .../buildinfo.Version=...".
var Version = "dev"

func init() {
	if Version != "dev" {
		return
	}
	// A plain go build or go install stamps the git commit.
	if bi, ok := debug.ReadBuildInfo(); ok {
		rev, dirty := "", false
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if len(rev) >= 7 {
			Version = rev[:7]
			if dirty {
				Version += "-dirty"
			}
		} else if v := bi.Main.Version; v != "" && v != "(devel)" {
			Version = v
		}
	}
}
