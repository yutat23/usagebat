//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func interactiveTerminal() bool {
	return isTerminal(os.Stdout) || isTerminal(os.Stderr)
}

func isTerminal(file *os.File) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(file.Fd()), &mode) == nil
}
