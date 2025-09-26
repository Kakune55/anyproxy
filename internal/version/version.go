package version

import "runtime/debug"

var (
	Version   = "1.1.0-rc"
	GitCommit = ""
	BuildInfo = ""
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		BuildInfo = info.Main.Version
	}
}
