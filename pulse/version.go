package pulse

import (
	_ "embed"
	"runtime/debug"
	"strings"

	"ergo.services/ergo/gen"
)

//go:embed VERSION
var version string

var Version = gen.Version{
	Name:    "Pulse Application",
	Release: strings.TrimPrefix(strings.TrimSpace(version), "v"),
	License: gen.LicenseMIT,
}

func init() {
	info, ok := debug.ReadBuildInfo()
	if ok == false {
		return
	}
	for _, dep := range info.Deps {
		if dep.Path == "ergo.services/application/pulse" {
			v := dep.Version
			if dep.Replace != nil {
				v = dep.Replace.Version
			}
			if v == "" || v == "(devel)" {
				return
			}
			if parts := strings.Split(v, "-"); len(parts) == 3 {
				Version.Commit = parts[2]
			} else {
				Version.Commit = v
			}
			return
		}
	}
}
