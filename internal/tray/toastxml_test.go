package tray

import (
	"encoding/xml"
	"strings"
	"testing"
)

// The toast is drawn from this payload alone. A missing appLogoOverride is why
// notifications arrived without an icon even though the AppUserModelId
// registration named one.
func TestToastPayloadCarriesTheIcon(t *testing.T) {
	original := toastIconURI
	t.Cleanup(func() { toastIconURI = original })
	toastIconURI = "file:///C:/Users/yuta/AppData/Roaming/usagebat/toast-icon.png"

	payload := toastPayload("Codex reset expires soon", "1 banked reset expires in 6h")
	want := `<image placement="appLogoOverride" src="file:///C:/Users/yuta/AppData/Roaming/usagebat/toast-icon.png"/>`
	if !strings.Contains(payload, want) {
		t.Fatalf("payload has no icon:\n%s", payload)
	}
	if err := xml.Unmarshal([]byte(payload), new(struct{})); err != nil {
		t.Fatalf("payload is not well-formed XML: %v\n%s", err, payload)
	}
}

func TestToastPayloadOmitsTheImageBeforeRegistration(t *testing.T) {
	original := toastIconURI
	t.Cleanup(func() { toastIconURI = original })
	toastIconURI = ""

	payload := toastPayload("title", "body")
	if strings.Contains(payload, "<image") {
		t.Fatalf("an unregistered icon must not leave a dangling image element:\n%s", payload)
	}
	if err := xml.Unmarshal([]byte(payload), new(struct{})); err != nil {
		t.Fatalf("payload is not well-formed XML: %v\n%s", err, payload)
	}
}

// Windows rejects the whole payload when an attribute is malformed, so a
// notification body holding an ampersand would silently deliver nothing.
func TestToastPayloadEscapesText(t *testing.T) {
	original := toastIconURI
	t.Cleanup(func() { toastIconURI = original })
	toastIconURI = ""

	payload := toastPayload(`Claude & Codex`, `"5h" <90% left>`)
	if strings.Contains(payload, "Claude & Codex") {
		t.Fatalf("ampersand was not escaped:\n%s", payload)
	}
	if err := xml.Unmarshal([]byte(payload), new(struct{})); err != nil {
		t.Fatalf("payload is not well-formed XML: %v\n%s", err, payload)
	}
}

func TestFileURIEscapesSpaces(t *testing.T) {
	// Forward slashes so the expectation holds on any host: filepath.ToSlash
	// only has work to do on Windows.
	got := fileURI("C:/Users/Yuta Tanaka/AppData/Roaming/usagebat/toast-icon.png")
	want := "file:///C:/Users/Yuta%20Tanaka/AppData/Roaming/usagebat/toast-icon.png"
	if got != want {
		t.Fatalf("fileURI = %q, want %q", got, want)
	}
}
