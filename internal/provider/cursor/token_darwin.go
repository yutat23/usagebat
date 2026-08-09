package cursor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// keychainTimeout bounds the helper. It is normally instant; a hang here would
// stall the whole refresh loop, which owns the icon.
const keychainTimeout = 5 * time.Second

// authFilePath is the CLI's file fallback on macOS. The keychain is what it
// normally uses here, so this is only consulted when that comes up empty.
func authFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor", "auth.json"), nil
}

// credentialPresent asks only whether a credential exists. The keychain half
// runs `security` without -w, so it returns the item's attributes and never its
// secret and cannot raise the "wants to use your confidential information"
// prompt — which matters, because this runs at startup for everybody, including
// people who do not use Cursor.
func credentialPresent() bool {
	return keychainPresent() || hasAuthFileToken()
}

// readCredential prefers the keychain, which is where the macOS CLI puts the
// token, and falls back to the file the other platforms use in case a future
// version stops reaching for the keychain here too.
func readCredential() (string, error) {
	if token, err := keychainToken(); err == nil && token != "" {
		return token, nil
	}
	token, err := authFileToken()
	if err != nil {
		return "", fmt.Errorf("no Cursor credential in the login keychain; run `agent login`" +
			apiKeyFallbackHint)
	}
	return token, nil
}

func keychainPresent() bool {
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security",
		"find-generic-password", "-s", keychainService, "-a", keychainAccount)
	return cmd.Run() == nil
}

func keychainToken() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security",
		"find-generic-password", "-s", keychainService, "-a", keychainAccount, "-w")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
