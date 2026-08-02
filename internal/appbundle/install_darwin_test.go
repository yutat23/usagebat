//go:build darwin

package appbundle

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAtCreatesAndReplacesBundle(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte("new executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "Applications", appName)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "old"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	signed := false
	if err := installAt(source, destination, "0.5.1", func(path string) error {
		signed = strings.HasSuffix(path, appName)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !signed {
		t.Fatal("staged app was not signed")
	}
	got, err := os.ReadFile(filepath.Join(destination, "Contents", "MacOS", "usagebat"))
	if err != nil || string(got) != "new executable" {
		t.Fatalf("installed executable = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "old")); !os.IsNotExist(err) {
		t.Fatalf("old bundle survived replacement: %v", err)
	}
	plist, err := os.ReadFile(filepath.Join(destination, "Contents", "Info.plist"))
	if err != nil || !strings.Contains(string(plist), "<string>0.5.1</string>") {
		t.Fatalf("Info.plist = %q, %v", plist, err)
	}
	if got := len(icon); got == 0 {
		t.Fatal("embedded icon is empty")
	}
}

func TestInstallAtLeavesExistingBundleWhenSigningFails(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "usagebat.app")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(destination, "old")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := errors.New("sign failed")
	if err := installAt(source, destination, "dev", func(string) error { return want }); !errors.Is(err, want) {
		t.Fatalf("installAt error = %v, want %v", err, want)
	}
	if got, err := os.ReadFile(old); err != nil || string(got) != "old" {
		t.Fatalf("existing bundle changed: %q, %v", got, err)
	}
}

func TestPlistVersion(t *testing.T) {
	for input, want := range map[string]string{
		"v0.5.1": "0.5.1", "0.5.1-0.20260803-deadbeef": "0.5.1", "dev": "0.0.0",
	} {
		if got := plistVersion(input); got != want {
			t.Errorf("plistVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInstallAtProducesValidAdHocSignature(t *testing.T) {
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), appName)
	if err := installAt(source, destination, "0.5.1", signBundle); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", destination).CombinedOutput(); err != nil {
		t.Fatalf("codesign verification: %v: %s", err, output)
	}
}
