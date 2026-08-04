//go:build windows

package tray

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
	"golang.org/x/sys/windows/registry"
)

const (
	notificationAppID = "usagebat"
	// The icon is drawn on top of this color, so a transparent background lets
	// the toast surface show through the icon's rounded edges.
	notificationIconBackground = "#00000000"
	notificationAppKey         = `SOFTWARE\Classes\AppUserModelId\` + notificationAppID
	// toastIconName sits beside the executable, so it carries the program's own
	// name rather than a generic one: GOPATH\bin is a shared directory.
	toastIconName = "usagebat-toast-icon.png"
)

// Windows reads the toast icon from an image file on disk; it does not pull
// icon resources out of the executable. The tray therefore ships its own copy.
//
//go:embed toasticon.png
var toastIcon []byte

var (
	notifySetup sync.Once
	notifyErr   error
)

func notifyNative(n Notification) error {
	notifySetup.Do(func() { notifyErr = registerToastApp() })
	if notifyErr != nil {
		return notifyErr
	}
	// Call the COM path directly. The high-level package enables a PowerShell
	// fallback; usagebat deliberately never opens or spawns a shell for alerts.
	return wintoast.Push(notificationAppID, toastPayload(n.Title, n.Body))
}

// notificationDiagnostics reads back what Windows will actually use, rather
// than what this process tried to register: a stale IconUri left by an older
// build is the difference between a toast with an icon and one without.
func notificationDiagnostics() []string {
	out := []string{"payload:      " + toastPayload("<title>", "<body>")}
	if toastIconURI == "" {
		out = append(out, "toast icon:   <unregistered; sending a notification writes it>")
	}
	out = append(out, `registration: HKCU\`+notificationAppKey)
	key, err := registry.OpenKey(registry.CURRENT_USER, notificationAppKey, registry.READ)
	if err != nil {
		return append(out, fmt.Sprintf("  <unregistered until the first notification> (%v)", err))
	}
	defer key.Close()
	for _, name := range []string{"DisplayName", "IconUri", "IconBackgroundColor", "CustomActivator"} {
		value, _, err := key.GetStringValue(name)
		if err != nil {
			out = append(out, fmt.Sprintf("  %-19s <unset>", name+":"))
			continue
		}
		line := fmt.Sprintf("  %-19s %s", name+":", value)
		if name == "IconUri" {
			switch info, err := os.Stat(value); {
			case err != nil:
				line += fmt.Sprintf("  [unreadable: %v]", err)
			case !strings.EqualFold(filepath.Ext(value), ".png") && !strings.EqualFold(filepath.Ext(value), ".ico"):
				line += "  [not an image; Windows will show no icon]"
			default:
				line += fmt.Sprintf("  [%d bytes]", info.Size())
			}
		}
		out = append(out, line)
	}
	return out
}

func registerToastApp() error {
	iconPath, err := writeToastIcon()
	if err != nil {
		return err
	}
	toastIconURI = fileURI(iconPath)
	if err := wintoast.SetAppData(wintoast.AppData{
		AppID: notificationAppID, IconPath: iconPath, IconBackgroundColor: notificationIconBackground,
	}); err != nil {
		return err
	}
	return registerToastIcon(iconPath)
}

// writeToastIcon materializes the embedded icon and returns its absolute path.
func writeToastIcon() (string, error) {
	path, err := installToastIcon()
	if err != nil {
		return "", err
	}
	if legacy := legacyToastIconPath(); legacy != "" && legacy != path {
		// Best effort: an earlier build put the icon under %APPDATA%, and
		// uninstalling usagebat would never have reached it there.
		os.Remove(legacy)
	}
	return path, nil
}

// installToastIcon writes the icon beside the executable, so that deleting the
// program directory — or the binary `go install` dropped in GOPATH\bin — takes
// the icon with it and leaves nothing behind. An install directory the user
// cannot write to still deserves an icon, so the config directory stands in.
func installToastIcon() (string, error) {
	var lastErr error
	for _, path := range toastIconCandidates() {
		if err := writeFileIfChanged(path, toastIcon); err != nil {
			lastErr = err
			continue
		}
		return path, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no candidate location for the toast icon")
	}
	return "", fmt.Errorf("writing the toast icon: %w", lastErr)
}

func toastIconCandidates() []string {
	var paths []string
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		paths = append(paths, filepath.Join(filepath.Dir(exe), toastIconName))
	}
	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "usagebat", toastIconName))
	}
	return paths
}

// legacyToastIconPath is where builds before this one wrote the icon.
func legacyToastIconPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "usagebat", "toast-icon.png")
}

func writeFileIfChanged(path string, data []byte) error {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// registerToastIcon overwrites the registry values SetAppData leaves alone.
// SetAppData skips any value that is already present, so installs upgraded from
// a build that registered the executable path would otherwise keep pointing at
// a file Windows cannot decode, and show no icon at all.
func registerToastIcon(iconPath string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, notificationAppKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("opening %s: %w", notificationAppKey, err)
	}
	defer key.Close()
	if err := key.SetStringValue("IconUri", iconPath); err != nil {
		return fmt.Errorf("setting IconUri: %w", err)
	}
	if err := key.SetStringValue("IconBackgroundColor", notificationIconBackground); err != nil {
		return fmt.Errorf("setting IconBackgroundColor: %w", err)
	}
	return nil
}
