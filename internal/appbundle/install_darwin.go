//go:build darwin

// Package appbundle installs the current standalone executable as a minimal
// macOS application bundle so APIs that require an application identity, such
// as UserNotifications, can be used safely.
package appbundle

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const appName = "usagebat.app"

//go:embed usagebat.icns
var icon []byte

var numericVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){0,2}`)

// Install copies the running executable into ~/Applications/usagebat.app,
// writes its identity and icon, and applies an ad-hoc signature. Replacement
// is staged on the same volume and rolls back if the final rename fails.
func Install(version string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	source, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(source); err == nil {
		source = resolved
	}
	destination := filepath.Join(home, "Applications", appName)
	if err := installAt(source, destination, version, signBundle); err != nil {
		return "", err
	}
	return destination, nil
}

// Launch asks Launch Services to register and start the installed bundle.
func Launch(path string) error {
	if output, err := exec.Command("/usr/bin/open", path).CombinedOutput(); err != nil {
		return fmt.Errorf("launching %s: %w: %s", path, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func installAt(source, destination, version string, sign func(string) error) error {
	applicationsDir := filepath.Dir(destination)
	if err := os.MkdirAll(applicationsDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", applicationsDir, err)
	}
	stageRoot, err := os.MkdirTemp(applicationsDir, ".usagebat-install-*")
	if err != nil {
		return fmt.Errorf("creating install staging directory: %w", err)
	}
	defer os.RemoveAll(stageRoot)

	stagedApp := filepath.Join(stageRoot, appName)
	macOSDir := filepath.Join(stagedApp, "Contents", "MacOS")
	resourcesDir := filepath.Join(stagedApp, "Contents", "Resources")
	if err := os.MkdirAll(macOSDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
		return err
	}
	if err := copyExecutable(source, filepath.Join(macOSDir, "usagebat")); err != nil {
		return fmt.Errorf("copying usagebat executable: %w", err)
	}
	if err := os.WriteFile(filepath.Join(resourcesDir, "usagebat.icns"), icon, 0o644); err != nil {
		return fmt.Errorf("writing application icon: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stagedApp, "Contents", "Info.plist"),
		[]byte(infoPlist(plistVersion(version))), 0o644); err != nil {
		return fmt.Errorf("writing Info.plist: %w", err)
	}
	if err := sign(stagedApp); err != nil {
		return err
	}
	return replace(destination, stagedApp)
}

func copyExecutable(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func signBundle(path string) error {
	output, err := exec.Command("/usr/bin/codesign", "--force", "--deep", "--sign", "-", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("signing application bundle: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func replace(destination, staged string) error {
	_, err := os.Lstat(destination)
	if os.IsNotExist(err) {
		return os.Rename(staged, destination)
	}
	if err != nil {
		return err
	}
	backup, err := os.MkdirTemp(filepath.Dir(destination), ".usagebat-backup-*")
	if err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil {
		return err
	}
	if err := os.Rename(destination, backup); err != nil {
		return fmt.Errorf("preserving existing application: %w", err)
	}
	if err := os.Rename(staged, destination); err != nil {
		if restoreErr := os.Rename(backup, destination); restoreErr != nil {
			return fmt.Errorf("installing application: %w (also failed to restore previous app: %v)", err, restoreErr)
		}
		return fmt.Errorf("installing application: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("removing previous application backup %s: %w", backup, err)
	}
	return nil
}

func plistVersion(version string) string {
	match := numericVersion.FindString(strings.TrimPrefix(version, "v"))
	if match == "" {
		return "0.0.0"
	}
	return match
}

func infoPlist(version string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "https://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>usagebat</string>
  <key>CFBundleDisplayName</key><string>usagebat</string>
  <key>CFBundleIdentifier</key><string>dev.yutat23.usagebat</string>
  <key>CFBundleExecutable</key><string>usagebat</string>
  <key>CFBundleIconFile</key><string>usagebat.icns</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>` + version + `</string>
  <key>CFBundleVersion</key><string>` + version + `</string>
  <key>NSHumanReadableCopyright</key><string>Copyright © 2026 yutat23</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>LSUIElement</key><true/>
</dict>
</plist>
`
}
