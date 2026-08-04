//go:build windows

package main

import (
	"log"
	"os"

	"golang.org/x/sys/windows"
)

// attachParentProcess asks AttachConsole for the console of the parent process.
const attachParentProcess = ^uintptr(0) // (DWORD)-1

// attachParentConsole reconnects stdout and stderr to the console the user
// typed into.
//
// The tray binary is linked with -H windowsgui so that no console window
// flashes up behind the tray icon. That also leaves one-shot runs such as
// -dump and -notify-test with no console at all, and everything they print
// would go nowhere. AttachConsole never creates a console, so a launch from
// Explorer or from a login item still stays silent.
func attachParentConsole() {
	attach := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	if ret, _, _ := attach.Call(attachParentProcess); ret == 0 {
		return
	}
	// A stream the shell redirected already has a usable handle. Only the ones
	// Windows left null get pointed at the console, so `-dump > out.txt` still
	// writes to the file.
	if needsConsole(windows.STD_OUTPUT_HANDLE) {
		if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stdout = out
		}
	}
	if needsConsole(windows.STD_ERROR_HANDLE) {
		if errOut, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stderr = errOut
		}
	}
	// The log package captured os.Stderr at init, so pointing it at the console
	// takes a second call; log.Fatal is how one-shot runs report failure.
	log.SetOutput(os.Stderr)
}

// needsConsole reports whether the standard handle is one the caller must
// supply itself. A GUI-subsystem process inherits a real handle only when the
// shell redirected the stream; otherwise Windows hands it a null one.
func needsConsole(std uint32) bool {
	handle, err := windows.GetStdHandle(std)
	return err != nil || handle == 0 || handle == windows.InvalidHandle
}
