package cursor

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Credential Manager is read through advapi32 directly rather than by shelling
// out to PowerShell: a tray app that spawns a console window every refresh is
// worse than one that writes twenty lines of syscall.
var (
	advapi32          = windows.NewLazySystemDLL("advapi32.dll")
	procCredRead      = advapi32.NewProc("CredReadW")
	procCredEnumerate = advapi32.NewProc("CredEnumerateW")
	procCredFree      = advapi32.NewProc("CredFree")
)

const (
	// credTypeGeneric is CRED_TYPE_GENERIC, the type keychain-style helpers
	// store application secrets under.
	credTypeGeneric = 1
	// credEnumerateAllCredentials is CRED_ENUMERATE_ALL_CREDENTIALS, which
	// returns every credential of the current user regardless of namespace.
	// CredEnumerateW's own filter only supports a prefix followed by an
	// asterisk, so it cannot find a name whose "cursor" is not at the front.
	credEnumerateAllCredentials = 0x1
)

// credentialW mirrors the CREDENTIALW layout. Go's field alignment matches the
// C struct's on both 386 and amd64, so the fields can be read in place.
type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// credentialTargets are the names the credential has been seen under. They are
// tried first because a direct read is one call, but they are only a fast path:
// the CLI's Windows build has been observed storing the token under none of
// them, which is what the enumeration below exists to survive.
func credentialTargets() []string {
	return []string{
		keychainService,
		keychainService + "/" + keychainAccount,
		"LegacyGeneric:target=" + keychainService,
		keychainAccount + "@" + keychainService,
	}
}

// authFilePath is where the Windows CLI keeps the token: %APPDATA%\Cursor.
// The folder is the credential domain "cursor", title-cased, which is what the
// CLI's own path helper produces.
func authFilePath() (string, error) {
	appData := strings.TrimSpace(os.Getenv("APPDATA"))
	if appData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, "Cursor", "auth.json"), nil
}

// credentialPresent checks the file first, which is a stat rather than an
// enumeration of every credential the user has.
func credentialPresent() bool {
	return hasAuthFileToken() || credentialStoreToken() != ""
}

// readCredential prefers the auth file, because that is where the Windows CLI
// actually writes the token — it only reaches for a keychain on macOS. The
// Credential Manager lookup stays as a fallback: it costs nothing when the file
// is there, and it covers an install that did store one.
func readCredential() (string, error) {
	token, fileErr := authFileToken()
	if fileErr == nil && token != "" {
		return token, nil
	}
	if token := credentialStoreToken(); token != "" {
		return token, nil
	}
	return "", fileErr
}

func credentialStoreToken() string {
	for _, target := range credentialTargets() {
		if token, ok := readCredentialTarget(target); ok {
			return token
		}
	}
	if token, ok := findCursorCredential(); ok {
		return token
	}
	return ""
}

// findCursorCredential looks for the token under whatever name the installed
// CLI chose.
//
// Credential Manager entries are named by whichever keychain binding the CLI
// was built against, and that name is neither documented nor stable across
// platforms, so guessing a list of them is a losing game. Enumerating and
// matching on the name is not.
func findCursorCredential() (string, bool) {
	var count uint32
	var list **credentialW
	ret, _, _ := procCredEnumerate.Call(0, credEnumerateAllCredentials,
		uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&list)))
	if ret == 0 || list == nil || count == 0 {
		return "", false
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(list)))

	for _, cred := range unsafe.Slice(list, count) {
		if cred == nil || cred.TargetName == nil {
			continue
		}
		if !looksLikeCursorToken(windows.UTF16PtrToString(cred.TargetName)) {
			continue
		}
		if cred.CredentialBlob == nil || cred.CredentialBlobSize == 0 {
			continue
		}
		if token := decodeBlob(unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)); token != "" {
			return token, true
		}
	}
	return "", false
}

// looksLikeCursorToken requires the name to say both whose it is and that it is
// a token. Matching "cursor" alone would happily pick up a saved git password
// for a repository with cursor in its URL and send it to Cursor's API, which is
// a worse failure than not finding the credential at all.
func looksLikeCursorToken(name string) bool {
	lower := strings.ToLower(name)
	if !strings.Contains(lower, "cursor") {
		return false
	}
	return strings.Contains(lower, "token") || strings.Contains(lower, "access")
}

func readCredentialTarget(target string) (string, bool) {
	name, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", false
	}
	// The credential is written back as a pointer rather than as an integer so
	// it never spends a moment as a bare uintptr, which nothing keeps alive.
	var cred *credentialW
	ret, _, _ := procCredRead.Call(
		uintptr(unsafe.Pointer(name)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&cred)))
	if ret == 0 || cred == nil {
		return "", false
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))

	if cred.CredentialBlob == nil || cred.CredentialBlobSize == 0 {
		return "", false
	}
	blob := unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)
	token := decodeBlob(blob)
	return token, token != ""
}

// decodeBlob reads the secret as whichever encoding it was written in. .NET
// helpers store UTF-16 and Node ones store UTF-8, and which one wrote this
// credential depends on the CLI version that logged in, so the shape of the
// bytes decides rather than a guess.
func decodeBlob(blob []byte) string {
	if len(blob)%2 == 0 && looksUTF16(blob) {
		units := make([]uint16, 0, len(blob)/2)
		for i := 0; i+1 < len(blob); i += 2 {
			units = append(units, uint16(blob[i])|uint16(blob[i+1])<<8)
		}
		return trimNul(string(utf16.Decode(units)))
	}
	return trimNul(string(blob))
}

// looksUTF16 is true when every second byte is zero, which an ASCII token
// encoded as UTF-16LE always is and a UTF-8 one of any length never is.
func looksUTF16(blob []byte) bool {
	if len(blob) < 2 {
		return false
	}
	for i := 1; i < len(blob); i += 2 {
		if blob[i] != 0 {
			return false
		}
	}
	return true
}

func trimNul(s string) string {
	for len(s) > 0 && (s[len(s)-1] == 0 || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
