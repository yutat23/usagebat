package tray

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/url"
	"path/filepath"
)

// toastIconURI is the file:// URI of the icon the toast payload points at. It
// stays empty until the Windows backend has written the icon out.
var toastIconURI string

// toastPayload builds the Windows notification XML.
//
// The icon has to be named here. The IconUri in the AppUserModelId registration
// only feeds the attribution line in the Action Center, which Windows does not
// draw for an unpackaged app: what puts a picture on the toast itself is an
// appLogoOverride image inside the payload.
//
// This lives outside the windows-only files so it can be tested anywhere; the
// payload is the part that is easy to get subtly and invisibly wrong.
func toastPayload(title, body string) string {
	return fmt.Sprintf(
		`<toast><visual><binding template="ToastGeneric">%s<text>%s</text><text>%s</text></binding></visual><audio src="ms-winsoundevent:Notification.Default"/></toast>`,
		toastImageElement(toastIconURI), escapeXML(title), escapeXML(body))
}

func toastImageElement(iconURI string) string {
	if iconURI == "" {
		return ""
	}
	return fmt.Sprintf(`<image placement="appLogoOverride" src="%s"/>`, escapeXML(iconURI))
}

// fileURI renders a Windows path as the file:// URI the toast schema expects.
// A bare path mostly works, but one holding a space — a user name such as
// "Yuta Tanaka" — does not.
func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(path)}).String()
}

func escapeXML(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return ""
	}
	return buf.String()
}
