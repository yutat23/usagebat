package cursor

import "testing"

// The enumeration matches on the entry's name, and the name is written by
// whichever keychain binding the installed CLI was built against. Matching too
// loosely would send somebody's saved git password to Cursor's API, which is a
// worse failure than not finding the credential at all.
func TestLooksLikeCursorTokenNeedsBothWhoseItIsAndThatItIsAToken(t *testing.T) {
	for _, name := range []string{
		"cursor-access-token",
		"cursor-access-token/cursor-user",
		"LegacyGeneric:target=cursor-access-token",
		"Cursor/AccessToken",
		"cursor.com:accessToken",
	} {
		if !looksLikeCursorToken(name) {
			t.Errorf("%q should be recognised as the Cursor token", name)
		}
	}
	for _, name := range []string{
		"git:https://github.com/someone/cursor-plugin",
		"cursor-user",
		"MicrosoftAccount:target=SSO_POP_Device",
		"LegacyGeneric:target=git:https://gitlab.com",
		"",
	} {
		if looksLikeCursorToken(name) {
			t.Errorf("%q should not be mistaken for the Cursor token", name)
		}
	}
}

func TestDecodeBlobReadsBothEncodingsTheBindingsWrite(t *testing.T) {
	utf8Blob := []byte("tok_abc123")
	if got := decodeBlob(utf8Blob); got != "tok_abc123" {
		t.Errorf("UTF-8 blob decoded to %q", got)
	}

	utf16Blob := make([]byte, 0, len(utf8Blob)*2)
	for _, b := range utf8Blob {
		utf16Blob = append(utf16Blob, b, 0)
	}
	if got := decodeBlob(utf16Blob); got != "tok_abc123" {
		t.Errorf("UTF-16 blob decoded to %q", got)
	}

	// A trailing NUL is part of the buffer, not part of the token.
	if got := decodeBlob(append(utf8Blob, 0)); got != "tok_abc123" {
		t.Errorf("NUL-terminated blob decoded to %q", got)
	}
}
