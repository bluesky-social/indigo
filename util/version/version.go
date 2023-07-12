package version

import (
	"runtime/debug"
)

// Version holds the version number.
var Version string

func init() {
	if Version == "" {
		Version = "dev-" + revision()
	}
}

func revision() string {
	info, ok := debug.ReadBuildInfo()
	if ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				return setting.Value[:7]
			}
		}
	}
	return ""
}
