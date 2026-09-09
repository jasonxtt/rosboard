// Package buildinfo describes the running executable, never a working-tree file.
package buildinfo

import (
	"runtime"
	"runtime/debug"
)

var Version = "dev"
var Commit = "unknown"
var BuiltAt = ""
var Flavor = ""

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"builtAt"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

func Current() Info {
	arch := Flavor
	if arch == "" {
		arch = runtime.GOARCH
		if b, ok := debug.ReadBuildInfo(); ok {
			for _, s := range b.Settings {
				if s.Key == "GOARM" && (s.Value == "7" || s.Value == "7,hardfloat") {
					arch = "armv7"
				}
				if s.Key == "GOAMD64" && s.Value == "v3" {
					arch = "amd64-v3"
				}
			}
		}
	}
	return Info{Version, Commit, BuiltAt, runtime.GOOS, arch}
}
