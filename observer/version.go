package observer

import (
	"runtime/debug"

	"ergo.services/ergo/gen"
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				Version.Commit = setting.Value
				break
			}
		}
	}
}

var (
	Version = gen.Version{
		Name:    "Observer Application",
		Release: "0.1.0",
		License: gen.LicenseMIT,
	}
)
