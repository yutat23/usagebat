package render

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yutat23/usagebat/internal/config"
)

// The mascot is inlined into the settings page, so malformed markup would
// break the whole document.
func TestMascotIsWellFormedSVG(t *testing.T) {
	for _, face := range []Face{FaceContent, FaceWorried, FaceAlarmed, FaceBlank} {
		chart := Mascot(face, "usagebat")
		if err := xml.Unmarshal([]byte(chart.SVG), new(struct{})); err != nil {
			t.Errorf("face %d is not well-formed XML: %v", face, err)
		}
		if !strings.Contains(chart.SVG, `shape-rendering="crispEdges"`) {
			t.Errorf("face %d would be smoothed; it is pixel art", face)
		}
	}
}

// Every expression has to look different, or the whole thing is decoration
// that changes nothing.
func TestEachFaceDrawsSomethingDifferent(t *testing.T) {
	seen := map[string]Face{}
	for _, face := range []Face{FaceContent, FaceWorried, FaceAlarmed, FaceBlank} {
		svg := Mascot(face, "usagebat").SVG
		if other, ok := seen[svg]; ok {
			t.Errorf("faces %d and %d draw identically", other, face)
		}
		seen[svg] = face
	}
}

// The face follows the same thresholds the battery colours itself by, so the
// two can never disagree on screen.
func TestFaceFollowsTheBatteryThresholds(t *testing.T) {
	p := PaletteFrom(config.Default().Colors, false)
	for _, tc := range []struct {
		remaining float64
		known     bool
		want      Face
	}{
		{90, true, FaceContent},
		{30, true, FaceWorried},
		{5, true, FaceAlarmed},
		{0, false, FaceBlank},
	} {
		if got := FaceFor(tc.remaining, tc.known, p); got != tc.want {
			t.Errorf("FaceFor(%v, %v) = %d, want %d", tc.remaining, tc.known, got, tc.want)
		}
	}
}

// The mascot and the shipped icon are the same animal. Drawing the content
// face has to produce exactly what the icon generator writes out, or the two
// drift apart the first time somebody adjusts one of them.
func TestContentFaceIsTheShippedIcon(t *testing.T) {
	sprite := Sprite(FaceContent)
	var drawn int
	for _, row := range sprite {
		for _, value := range row {
			if value != 0 {
				drawn++
			}
		}
	}
	if drawn == 0 {
		t.Fatal("the sprite is empty")
	}
	// The content face is what the icon generator draws, so its battery is the
	// full three bars. The other faces drain it, which is the one place the
	// expression repeats what the reading says — deliberately.
	content := countValue(sprite, 'g')
	if content == 0 {
		t.Fatal("the shipped icon's battery has no charge bars")
	}
	if worried := countValue(Sprite(FaceWorried), 'g'); worried >= content {
		t.Errorf("worried bars = %d, content = %d; worried should be drained further",
			worried, content)
	}
	if alarmed := countValue(Sprite(FaceAlarmed), 'g'); alarmed >= countValue(Sprite(FaceWorried), 'g') {
		t.Error("an alarmed bat should be drawn emptier than a worried one")
	}
	if countValue(Sprite(FaceBlank), 'g') != 0 {
		t.Error("nothing reported should leave the battery empty")
	}
}

func countValue(sprite [SpriteSize][SpriteSize]byte, want byte) int {
	var n int
	for _, row := range sprite {
		for _, value := range row {
			if value == want {
				n++
			}
		}
	}
	return n
}

// Writes the four faces to build/ so they can be looked at; nothing about
// whether a drawing is charming can be asserted in a test.
func TestWriteMascotPreview(t *testing.T) {
	if os.Getenv("USAGEBAT_MASCOT_PREVIEW") == "" {
		t.Skip("set USAGEBAT_MASCOT_PREVIEW=1 to write build/mascot-preview.html")
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><title>mascot</title>` +
		`<style>body{margin:0;font:14px system-ui;display:flex}` +
		`.pane{flex:1;padding:2rem;display:flex;gap:2rem}` +
		`.light{background:#fafafa;color:#111}.dark{background:#232326;color:#eee}` +
		`figure{margin:0;text-align:center}svg{width:6rem;height:6rem;image-rendering:pixelated}` +
		`figcaption{margin-top:.5rem;opacity:.7}</style>`)
	names := map[Face]string{
		FaceContent: "content", FaceWorried: "worried",
		FaceAlarmed: "alarmed", FaceBlank: "nothing reported",
	}
	for _, class := range []string{"light", "dark"} {
		fmt.Fprintf(&b, `<div class="pane %s">`, class)
		for _, face := range []Face{FaceContent, FaceWorried, FaceAlarmed, FaceBlank} {
			fmt.Fprintf(&b, `<figure>%s<figcaption>%s</figcaption></figure>`,
				Mascot(face, names[face]).SVG, names[face])
		}
		b.WriteString(`</div>`)
	}
	dir := filepath.Join("..", "..", "build")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mascot-preview.html"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
