// Package version exposes the application version injected by release builds.
package version

import (
	"runtime/debug"
	"strings"
)

// Value is replaced with -ldflags for tagged builds.
var Value = "dev"

// String returns the linker-injected release version, or the Go module version
// embedded by `go install module@version` when no release flags were supplied.
func String() string {
	if Value != "" && Value != "dev" {
		return Value
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}
