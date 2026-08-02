//go:build windows

// Package hiddenprocess applies platform-specific settings to background
// subprocesses started by the tray application.
package hiddenprocess

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW prevents console programs such as codex and claude from
// opening a Command Prompt window during a background refresh.
const createNoWindow = 0x08000000

func Configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
}
