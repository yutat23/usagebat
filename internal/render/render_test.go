package render

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"testing"

	"github.com/yutat23/usage-battery/internal/config"
	"github.com/yutat23/usage-battery/internal/model"
)

func testPalette() Palette { return PaletteFrom(config.Default().Colors) }

func known(used float64) model.WindowStatus {
	return model.WindowStatus{Known: true, UsedPercent: used}
}

func TestGaugeTextFitsInside(t *testing.T) {
	// Every gauge string must fit the 13-dot interior, otherwise digits spill
	// over the battery outline.
	for _, used := range []float64{0, 0.4, 13, 50, 87.5, 99.6, 100} {
		txt := gaugeText(known(used))
		if w := textWidth(txt); w > innerW {
			t.Errorf("used=%v text %q is %d dots, interior is %d", used, txt, w, innerW)
		}
	}
	if w := textWidth(gaugeText(model.WindowStatus{})); w > innerW {
		t.Errorf("unknown marker too wide")
	}
}

func TestFilledColumns(t *testing.T) {
	cases := []struct {
		used float64
		want int
	}{
		{0, innerW},   // nothing used: full gauge
		{100, 0},      // exhausted: empty
		{50, 7},       // 6.5 rounds up
		{99.9, 1},     // a sliver must remain visible
		{99.99999, 1}, //
	}
	for _, c := range cases {
		if got := filledColumns(known(c.used), innerW); got != c.want {
			t.Errorf("used=%v: got %d columns, want %d", c.used, got, c.want)
		}
	}
	if got := filledColumns(model.WindowStatus{}, innerW); got != 0 {
		t.Errorf("unknown status must not fill the gauge, got %d", got)
	}
}

func TestStripGeometry(t *testing.T) {
	windows := model.AllWindows
	st := map[model.Window]model.WindowStatus{
		model.Window5h:      known(13),
		model.WindowWeekly:  known(38),
		model.WindowMonthly: known(59),
	}
	icon := Strip(windows, st, Options{Mode: config.ModeBoth, Palette: testPalette(), Scale: 2})

	wantW := batteryW*3 + cellGap*2
	wantH := labelH + labelGap + batteryH
	if icon.DotsW != wantW || icon.DotsH != wantH {
		t.Fatalf("dots = %dx%d, want %dx%d", icon.DotsW, icon.DotsH, wantW, wantH)
	}
	if b := icon.Image.Bounds(); b.Dx() != wantW*2 || b.Dy() != wantH*2 {
		t.Fatalf("image = %v, want %dx%d", b, wantW*2, wantH*2)
	}
	// The menu bar is 22pt tall, so at the default 1pt per dot the art has to
	// stay under that.
	if wantH > 22 {
		t.Fatalf("strip is %d dots tall; too tall for the menu bar", wantH)
	}
}

func TestStripPercentModeIsNarrower(t *testing.T) {
	windows := []model.Window{model.Window5h}
	st := map[model.Window]model.WindowStatus{model.Window5h: known(13)}
	p := testPalette()
	both := Strip(windows, st, Options{Mode: config.ModeBoth, Palette: p, Scale: 1})
	pct := Strip(windows, st, Options{Mode: config.ModePercent, Palette: p, Scale: 1})
	if pct.DotsH >= both.DotsH {
		t.Errorf("percent-only mode should be shorter: %d vs %d", pct.DotsH, both.DotsH)
	}
}

func TestSquareIsAlwaysSixteenDots(t *testing.T) {
	st := map[model.Window]model.WindowStatus{
		model.Window5h:      known(13),
		model.WindowWeekly:  known(38),
		model.WindowMonthly: {},
	}
	for _, layout := range []string{"stack", "single"} {
		for _, n := range []int{1, 2, 3} {
			icon := Square(model.AllWindows[:n], st, Options{Mode: config.ModeBoth, Palette: testPalette(), Scale: 2}, layout)
			if icon.DotsW != squareDots || icon.DotsH != squareDots {
				t.Errorf("%s/%d: dots = %dx%d", layout, n, icon.DotsW, icon.DotsH)
			}
		}
	}
}

func TestICOStructure(t *testing.T) {
	data, err := ICO(func(size int) *image.RGBA {
		return Square(model.AllWindows, map[model.Window]model.WindowStatus{
			model.Window5h: known(20),
		}, Options{Mode: config.ModeBoth, Palette: testPalette(), Scale: size / 16}, "stack").Image
	})
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(data)
	var reserved, typ, count uint16
	binary.Read(r, binary.LittleEndian, &reserved)
	binary.Read(r, binary.LittleEndian, &typ)
	binary.Read(r, binary.LittleEndian, &count)
	if reserved != 0 || typ != 1 || int(count) != len(icoSizes) {
		t.Fatalf("bad ICONDIR: reserved=%d type=%d count=%d", reserved, typ, count)
	}
	for i := 0; i < int(count); i++ {
		var e struct {
			W, H, Colors, Reserved uint8
			Planes, Bits           uint16
			Size, Offset           uint32
		}
		if err := binary.Read(r, binary.LittleEndian, &e); err != nil {
			t.Fatal(err)
		}
		want := icoSizes[i]
		if int(e.W) != want || int(e.H) != want {
			t.Errorf("entry %d: %dx%d, want %dx%d", i, e.W, e.H, want, want)
		}
		// BITMAPINFOHEADER + BGRA pixels + AND mask.
		wantLen := 40 + want*want*4 + ((want+31)/32)*4*want
		if int(e.Size) != wantLen {
			t.Errorf("entry %d: size %d, want %d", i, e.Size, wantLen)
		}
		if int(e.Offset)+int(e.Size) > len(data) {
			t.Errorf("entry %d runs past the end of the file", i)
		}
	}
}

// TestICOPixelsRoundTrip decodes an entry back out of the DIB. Windows cannot be
// exercised from here, so the bytes themselves have to be checked: a bottom-up
// row order or a BGRA channel swap would otherwise ship unnoticed.
func TestICOPixelsRoundTrip(t *testing.T) {
	st := map[model.Window]model.WindowStatus{
		model.Window5h:      known(20),
		model.WindowWeekly:  known(50),
		model.WindowMonthly: known(90),
	}
	build := func(size int) *image.RGBA {
		return Square(model.AllWindows, st,
			Options{Mode: config.ModeBoth, Palette: testPalette(), Scale: size / 16}, "stack").Image
	}
	data, err := ICO(build)
	if err != nil {
		t.Fatal(err)
	}

	const size = 16
	// ICONDIR is 6 bytes, then one 16-byte entry per image; the first entry's
	// pixels start after its 40-byte BITMAPINFOHEADER.
	offset := 6 + 16*len(icoSizes) + 40
	want := build(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Rows are stored bottom-up.
			i := offset + ((size-1-y)*size+x)*4
			gotB, gotG, gotR, gotA := data[i], data[i+1], data[i+2], data[i+3]
			w := want.RGBAAt(x, y)
			if gotR != w.R || gotG != w.G || gotB != w.B || gotA != w.A {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y,
					[4]uint8{gotR, gotG, gotB, gotA}, [4]uint8{w.R, w.G, w.B, w.A})
			}
		}
	}
}

func TestParseHex(t *testing.T) {
	fb := rgba{1, 2, 3, 4}
	if got := parseHex("#3DDC64", fb); got != (rgba{0x3D, 0xDC, 0x64, 0xFF}) {
		t.Errorf("rrggbb: %v", got)
	}
	if got := parseHex("3DDC6480", fb); got != (rgba{0x3D, 0xDC, 0x64, 0x80}) {
		t.Errorf("rrggbbaa: %v", got)
	}
	if got := parseHex("nope", fb); got != fb {
		t.Errorf("fallback: %v", got)
	}
}

// TestWritePreview dumps the artwork so the layouts can be eyeballed. It is a
// no-op unless USAGE_BATTERY_PREVIEW_DIR is set.
func TestWritePreview(t *testing.T) {
	dir := os.Getenv("USAGE_BATTERY_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set USAGE_BATTERY_PREVIEW_DIR to dump preview images")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := map[model.Window]model.WindowStatus{
		model.Window5h:      known(13),
		model.WindowWeekly:  known(38),
		model.WindowMonthly: known(59),
	}
	full := map[model.Window]model.WindowStatus{
		model.Window5h:      known(0),
		model.WindowWeekly:  known(0),
		model.WindowMonthly: known(0),
	}
	low := map[model.Window]model.WindowStatus{
		model.Window5h:      known(96),
		model.WindowWeekly:  known(85),
		model.WindowMonthly: {},
	}
	p := testPalette()

	var sheet []*image.RGBA
	dump := func(name string, icon *Icon) {
		data, err := PNG(icon.Image)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		sheet = append(sheet, icon.Image)
	}
	for _, m := range []config.DisplayMode{config.ModeBoth, config.ModeBattery, config.ModePercent} {
		o := Options{Mode: m, Palette: p, Scale: 8}
		dump("strip-"+string(m)+".png", Strip(model.AllWindows, st, o))
	}
	dump("strip-full.png", Strip(model.AllWindows, full, Options{Mode: config.ModeBoth, Palette: p, Scale: 8}))
	dump("strip-low.png", Strip(model.AllWindows, low, Options{Mode: config.ModeBoth, Palette: p, Scale: 8}))
	dump("square-stack.png", Square(model.AllWindows, st, Options{Mode: config.ModeBoth, Palette: p, Scale: 8}, "stack"))
	dump("square-single.png", Square(model.AllWindows, st, Options{Mode: config.ModeBoth, Palette: p, Scale: 8}, "single"))
	providerCells := []Cell{
		{Label: "CL5H", Status: known(71)},
		{Label: "CXMO", Status: known(0)},
	}
	dump("strip-providers.png", StripCells(providerCells, Options{Mode: config.ModeBoth, Palette: p, Scale: 8}))
	dump("square-providers.png", SquareCells(providerCells, Options{Mode: config.ModeBoth, Palette: p, Scale: 8}, "stack"))

	// The art has to hold up on both a light and a dark menu bar, so composite
	// contact sheets over each.
	for _, bg := range []struct {
		name string
		col  color.RGBA
	}{
		{"sheet-dark.png", color.RGBA{0x1E, 0x1E, 0x20, 0xFF}},
		{"sheet-light.png", color.RGBA{0xF5, 0xF5, 0xF7, 0xFF}},
	} {
		const pad = 16
		w, h := 0, pad
		for _, img := range sheet {
			if img.Bounds().Dx() > w {
				w = img.Bounds().Dx()
			}
			h += img.Bounds().Dy() + pad
		}
		out := image.NewRGBA(image.Rect(0, 0, w+pad*2, h))
		draw.Draw(out, out.Bounds(), &image.Uniform{bg.col}, image.Point{}, draw.Src)
		y := pad
		for _, img := range sheet {
			r := img.Bounds().Add(image.Pt(pad, y))
			draw.Draw(out, r, img, image.Point{}, draw.Over)
			y += img.Bounds().Dy() + pad
		}
		data, err := PNG(out)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, bg.name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
