package main

import (
	"runtime/debug"

	"github.com/allen0099/blacklist/cmd"
)

// version is set at release time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if bi, ok := debug.ReadBuildInfo(); ok && version == "dev" {
		version = bi.Main.Version // set by `go install ...@vX.Y.Z`
	}
	cmd.Execute(version)
}
