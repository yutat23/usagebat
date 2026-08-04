//go:build darwin

package tray

import (
	"fmt"
	"os"
	"path/filepath"
)

func notificationDiagnostics() []string {
	exe, err := os.Executable()
	if err != nil {
		return []string{fmt.Sprintf("executable: %v", err)}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	out := []string{"executable: " + exe}
	if isAppBundleExecutable(exe) {
		out = append(out, "bundle:     yes — UserNotifications can be used")
	} else {
		out = append(out, "bundle:     no — macOS only delivers from usagebat.app;",
			"            run `usagebat install-app`, then re-run this from",
			"            /Applications/usagebat.app/Contents/MacOS/usagebat")
	}
	return out
}
