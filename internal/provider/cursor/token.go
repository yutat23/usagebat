package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yutat23/usagebat/internal/config"
)

// keychainService and keychainAccount are where `agent login` puts the access
// token on the platforms that have a keychain. The CLI is installed as `agent`;
// `cursor-agent` is an alias left by the versioned install directory, and both
// are the same binary.
const (
	keychainService = "cursor-access-token"
	keychainAccount = "cursor-user"
)

// authFileToken reads the token out of the CLI's auth.json.
//
// This, not the credential store, is where the token lives on Windows and
// Linux: the CLI only reaches for a keychain on macOS. Looking for a Credential
// Manager entry there finds nothing however it is named, which is why usagebat
// previously reported a signed-in Windows machine as having no credential.
func authFileToken() (string, error) {
	path, err := authFilePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no Cursor auth file at %s; run `agent login`"+apiKeyFallbackHint,
				shortenHome(path))
		}
		return "", fmt.Errorf("reading %s: %w", shortenHome(path), err)
	}
	var auth struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(data, &auth); err != nil {
		return "", fmt.Errorf("%s is not readable JSON; run `agent login`", shortenHome(path))
	}
	token := strings.TrimSpace(auth.AccessToken)
	if token == "" {
		return "", fmt.Errorf("no accessToken in %s; run `agent login`", shortenHome(path))
	}
	return token, nil
}

func hasAuthFileToken() bool {
	token, err := authFileToken()
	return err == nil && token != ""
}

// shortenHome keeps diagnostics in a tray menu to a readable width.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + strings.TrimPrefix(path, home)
}

// resolveToken finds the credential to authenticate with, preferring anything
// the user named explicitly over what happens to be in the keychain.
//
// usagebat only ever reads it: it is not copied into the config file, the
// history file or the log, and it goes nowhere except Cursor's own host.
func resolveToken(cfg config.Cursor) (string, error) {
	if configured := strings.TrimSpace(cfg.APIKey); configured != "" {
		return configured, nil
	}
	if fromEnv := envToken(); fromEnv != "" {
		return fromEnv, nil
	}
	token, err := readCredential()
	if err != nil {
		// Returned unwrapped: the platform lookup already names both what it
		// searched and what to do, and prefixing it produced menu rows reading
		// "no Cursor credential: no Cursor credential in ...".
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("no Cursor credential; run `agent login`" + apiKeyFallbackHint)
	}
	return token, nil
}

// apiKeyFallbackHint is appended wherever the credential cannot be read.
//
// Where the CLI puts its token is undocumented and varies by platform, so this
// lookup can fail on a machine that is genuinely signed in. An API key is a
// supported way in that does not depend on finding it, which makes it the one
// thing the user can always do about this message.
const apiKeyFallbackHint = ", or set sources.cursor.apiKey"

func envToken() string {
	return strings.TrimSpace(os.Getenv("CURSOR_API_KEY"))
}

// signedIn reports whether the CLI has an account on this machine, by looking
// for the marker it writes beside its own settings.
//
// This is a second, independent signal from the credential store, and it is
// what keeps Cursor visible on a machine where the token cannot be found: the
// store's entry name is chosen by whichever keychain binding the installed CLI
// was built against, and on Windows it has been seen under none of the names
// usagebat knows. Vanishing from the menu gives the user nothing to act on,
// whereas appearing with "no Cursor credential" says exactly what to fix.
//
// The file holds no secret — only `authId`, `authInfo` and a cache key — so
// this never reads a token from disk.
func signedIn() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(home, ".cursor", "cli-config.json"))
	if err != nil {
		return false
	}
	var marker struct {
		AuthID   json.RawMessage `json:"authId"`
		AuthInfo json.RawMessage `json:"authInfo"`
	}
	if json.Unmarshal(data, &marker) != nil {
		return false
	}
	return len(marker.AuthID) > 0 || len(marker.AuthInfo) > 0
}
