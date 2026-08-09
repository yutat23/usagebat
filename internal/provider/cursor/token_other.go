//go:build !darwin && !windows

package cursor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// keychainTimeout bounds the helper. It is normally instant; a hang here would
// stall the whole refresh loop, which owns the icon.
const keychainTimeout = 5 * time.Second

// authFilePath is where the Linux CLI keeps the token. It follows the XDG
// config location rather than ~/.cursor, which holds its settings.
func authFilePath() (string, error) {
	if configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); configHome != "" {
		return filepath.Join(configHome, "cursor", "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cursor", "auth.json"), nil
}

func credentialPresent() bool {
	if hasAuthFileToken() {
		return true
	}
	token, err := secretToolToken()
	return err == nil && token != ""
}

// readCredential prefers the auth file, which is what the CLI writes here.
// secret-tool remains as a fallback for installs whose keychain binding did
// reach libsecret, and it is not always available: a machine without it simply
// never shows Cursor, the same outcome as never having logged in.
func readCredential() (string, error) {
	token, fileErr := authFileToken()
	if fileErr == nil && token != "" {
		return token, nil
	}
	if token, err := secretToolToken(); err == nil && token != "" {
		return token, nil
	}
	return "", fileErr
}

func secretToolToken() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "secret-tool",
		"lookup", "service", keychainService, "account", keychainAccount)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
