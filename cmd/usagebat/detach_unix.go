//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func launchDetached() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := append([]string{"--foreground"}, os.Args[1:]...)
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
