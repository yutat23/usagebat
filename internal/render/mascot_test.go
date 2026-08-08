package render

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The mascot is inlined into the settings page, so malformed markup would
// break the whole document.
func TestMascotIsWellFormedSVG(t *testing.T) {
	chart := Mascot("usagebat")
	if err := xml.Unmarshal([]byte(chart.SVG), new(struct{})); err != nil {
		t.Fatalf("mascot is not well-formed XML: %v", err)
	}
	if !strings.Contains(chart.SVG, `shape-rendering="crispEdges"`) {
		t.Error("mascot would be smoothed; it is pixel art")
	}
}

// The mascot and shipped icon share exactly one sprite. There is deliberately
// no usage-dependent face or charge state in this API.
func TestMascotUsesTheStableShippedSprite(t *testing.T) {
	sprite := Sprite()
	var drawn, charge int
	for _, row := range sprite {
		for _, value := range row {
			if value != 0 {
				drawn++
			}
			if value == 'g' {
				charge++
			}
		}
	}
	if drawn == 0 || charge == 0 {
		t.Fatalf("stable sprite is incomplete: drawn=%d charge=%d", drawn, charge)
	}
}

// Writes the one stable mascot to build/ for optional visual review.
func TestWriteMascotPreview(t *testing.T) {
	if os.Getenv("USAGEBAT_MASCOT_PREVIEW") == "" {
		t.Skip("set USAGEBAT_MASCOT_PREVIEW=1 to write build/mascot-preview.html")
	}
	markup := `<!doctype html><meta charset="utf-8"><title>mascot</title>` +
		`<style>body{margin:0;font:14px system-ui;display:flex}` +
		`.pane{flex:1;padding:2rem}.light{background:#fafafa;color:#111}` +
		`.dark{background:#232326;color:#eee}figure{margin:0;text-align:center}` +
		`svg{width:6rem;height:6rem;image-rendering:pixelated}` +
		`figcaption{margin-top:.5rem;opacity:.7}</style>` +
		`<div class="pane light"><figure>` + Mascot("usagebat").SVG +
		`<figcaption>stable mascot</figcaption></figure></div>` +
		`<div class="pane dark"><figure>` + Mascot("usagebat").SVG +
		`<figcaption>stable mascot</figcaption></figure></div>`
	dir := filepath.Join("..", "..", "build")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mascot-preview.html"), []byte(markup), 0o644); err != nil {
		t.Fatal(err)
	}
}
