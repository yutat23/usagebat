//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

func launchDetached() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := append([]string{"--foreground"}, os.Args[1:]...)
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
