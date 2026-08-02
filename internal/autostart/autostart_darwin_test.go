//go:build darwin

package autostart

import (
	"os"
	"strings"
	"testing"
)

func TestLaunchAgentPlistEscapesExecutablePath(t *testing.T) {
	plist := launchAgentPlist(`/Applications/A&B <Test>/Usage "Battery"`)
	for _, want := range []string{"A&amp;B", "&lt;Test&gt;", "&quot;Battery&quot;", "<key>RunAtLoad</key>"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q:\n%s", want, plist)
		}
	}
}

func TestSetEnablesAndDisablesLaunchAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Set(true); err != nil {
		t.Fatal(err)
	}
	if enabled, err := Enabled(); err != nil || !enabled {
		t.Fatalf("Enabled() = %v, %v after Set(true)", enabled, err)
	}
	path, err := launchAgentPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "ProgramArguments") {
		t.Fatalf("registered plist = %q, %v", data, err)
	}
	if err := Set(false); err != nil {
		t.Fatal(err)
	}
	if enabled, err := Enabled(); err != nil || enabled {
		t.Fatalf("Enabled() = %v, %v after Set(false)", enabled, err)
	}
}
