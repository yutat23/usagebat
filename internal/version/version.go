// Package version exposes the application version injected by release builds.
package version

// Value is replaced with -ldflags for tagged builds.
var Value = "dev"
