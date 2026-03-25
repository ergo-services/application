package observer

import (
	"runtime/debug"
	"strings"

	"ergo.services/ergo/gen"
)

var Version = gen.Version{
	Name:    "Observer Application",
	Release: "0.2.0",
	License: gen.LicenseMIT,
}

func init() {
	info, ok := debug.ReadBuildInfo()
	if ok == false {
		return
	}
	for _, dep := range info.Deps {
		if dep.Path == "ergo.services/application/observer" {
			v := dep.Version
			if dep.Replace != nil {
				v = dep.Replace.Version
			}
			// skip empty or local replace markers
			if v == "" || v == "(devel)" {
				return
			}
			// pseudo-version: "v0.0.0-20260324112233-abc1234def56" → extract hash
			if parts := strings.Split(v, "-"); len(parts) == 3 {
				Version.Commit = parts[2]
			} else {
				Version.Commit = v
			}
			return
		}
	}
}
