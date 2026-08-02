//go:build !darwin && !windows

package main

import "os"

func interactiveTerminal() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != ""
}
