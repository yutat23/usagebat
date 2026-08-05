package render

import (
	"fmt"
	"strings"
)

// Shared SVG helpers. The charts in chart.go are the only thing that emits
// SVG; the tray icon is a bitmap, because a status item wants pixels.

func hex(c rgba) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// escapeAttr keeps a caller-supplied string from breaking out of the attribute
// or element it is written into. Chart labels come from the app's own strings,
// but that is a property of today's callers rather than of this function.
func escapeAttr(s string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;",
	).Replace(s)
}
