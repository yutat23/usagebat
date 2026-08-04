//go:build !windows

package main

// attachParentConsole is a no-op: every other platform already hands a
// foreground process the terminal it was launched from.
func attachParentConsole() {}
