//go:build !windows

// Package hiddenprocess applies platform-specific settings to background
// subprocesses started by the tray application.
package hiddenprocess

import "os/exec"

// Configure is a no-op outside Windows.
func Configure(_ *exec.Cmd) {}
