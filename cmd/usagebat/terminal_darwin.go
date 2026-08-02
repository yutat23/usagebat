//go:build darwin

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func interactiveTerminal() bool {
	return isTerminal(os.Stdout) || isTerminal(os.Stderr)
}

func isTerminal(file *os.File) bool {
	_, err := unix.IoctlGetTermios(int(file.Fd()), unix.TIOCGETA)
	return err == nil
}
