// Package render draws the tray artwork on a dot grid and scales it up, so the
// result reads as pixel art at any size.
package render

import (
	"fmt"
	"image"
	"math"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/model"
)

// Battery sprite geometry, in dots.
//
//	 0            14 1516
//	0 ##############        <- body outline, 15 wide
//	1 #............#
//	3 #............#  ##    <- terminal, 2x5
//	9 #............#
//	10##############
const (
	bodyW    = 15
	batteryH = 11
	tipW     = 2
	tipH     = 5
	batteryW = bodyW + tipW // 17
	innerX   = 1
	innerY   = 1
	innerW   = bodyW - 2 // 13
	innerH   = batteryH - 2
	labelH   = glyphH
	labelGap = 2
	cellGap  = 3
)

// Palette is the resolved colour set.
type Palette struct {
	Good, Warn, Critical  rgba
	Unknown               rgba
	Claude, Codex, Period rgba
	TextOnFill            rgba
	WarnBelow             float64
	CriticalBelow         float64
}

// PaletteFrom resolves configured hex strings, falling back to the defaults for
// anything unparseable.
func PaletteFrom(c config.Colors, dark bool) Palette {
	t := c.Light
	if dark {
		t = c.Dark
	}
	return Palette{
		Good:          parseHex(t.Good, rgba{0x15, 0x80, 0x3D, 0xFF}),
		Warn:          parseHex(t.Warn, rgba{0xA1, 0x62, 0x07, 0xFF}),
		Critical:      parseHex(t.Critical, rgba{0xBE, 0x12, 0x3C, 0xFF}),
		Unknown:       parseHex(t.Unknown, rgba{0x52, 0x52, 0x5B, 0xFF}),
		Claude:        parseHex(t.Claude, rgba{0xA9, 0x4F, 0x32, 0xFF}),
		Codex:         parseHex(t.Codex, rgba{0x08, 0x75, 0x67, 0xFF}),
		Period:        parseHex(t.Period, rgba{0x25, 0x27, 0x2B, 0xFF}),
		TextOnFill:    parseHex(t.TextOnFill, rgba{0xF8, 0xFA, 0xFC, 0xFF}),
		WarnBelow:     orDefault(c.WarnBelow, 50),
		CriticalBelow: orDefault(c.CriticalBelow, 20),
	}
}

func orDefault(v, d float64) float64 {
	if v <= 0 {
		return d
	}
	return v
}

// accent picks the colour for a remaining percentage.
func (p Palette) accent(st model.WindowStatus) rgba {
	if !st.Known {
		return p.Unknown
	}
	r := st.RemainingPercent()
	switch {
	case r < p.CriticalBelow:
		return p.Critical
	case r < p.WarnBelow:
		return p.Warn
	}
	return p.Good
}

// Options controls one render.
type Options struct {
	Mode    config.DisplayMode
	Palette Palette
	// Scale is how many bitmap pixels one dot becomes.
	Scale int
}

// Icon is a rendered tray image plus the dot dimensions it was laid out at,
// which the macOS backend needs to compute the logical point size.
type Icon struct {
	Image *image.RGBA
	DotsW int
	DotsH int
}

// Cell is one independently labelled limit in a tray icon. Label can include
// both a service and a period (for example "CL 5H"), unlike the legacy Window
// API which labels only the period.
type Cell struct {
	// Service identifies the provider family and selects the CL/CX label and
	// colour. Period is the independently coloured 5H/WK/MO suffix. Label is a
	// compatibility fallback for callers that provide one unsplit caption.
	Service string
	Period  string
	Label   string
	Status  model.WindowStatus
}

// gaugeText is what goes inside (or in place of) the battery.
//
// Both "100" and "87%" are 11 dots wide, so they centre identically inside the
// 13-dot gauge; "100%" would not fit, hence the split.
func gaugeText(st model.WindowStatus) string {
	if !st.Known {
		return "?"
	}
	r := int(math.Round(st.RemainingPercent()))
	if r >= 100 {
		return "100"
	}
	return fmt.Sprintf("%d%%", r)
}

// percentText is the standalone form used when no battery is drawn.
func percentText(st model.WindowStatus) string {
	if !st.Known {
		return "?"
	}
	return fmt.Sprintf("%d%%", int(math.Round(st.RemainingPercent())))
}

// filledColumns converts a remaining percentage into gauge columns. A non-zero
// remainder always keeps at least one column lit so "nearly empty" stays
// visually distinct from "empty".
func filledColumns(st model.WindowStatus, width int) int {
	if !st.Known {
		return 0
	}
	r := st.RemainingPercent()
	n := int(math.Round(r / 100 * float64(width)))
	if n < 1 && r > 0 {
		n = 1
	}
	if n > width {
		n = width
	}
	return n
}

// drawBattery paints the sprite with its top-left at (x, y).
func drawBattery(c *canvas, x, y int, st model.WindowStatus, o Options, withText bool) {
	accent := o.Palette.accent(st)
	c.strokeRect(x, y, bodyW, batteryH, accent)
	c.fillRect(x+bodyW, y+(batteryH-tipH)/2, tipW, tipH, accent)

	// The whole interior is painted — spent capacity in a pale tint, remaining
	// capacity in the accent — so the digits always sit on a known backdrop.
	filled := filledColumns(st, innerW)
	c.fillRect(x+innerX, y+innerY, innerW, innerH, pale(accent))
	c.fillRect(x+innerX, y+innerY, filled, innerH, accent)

	if !withText {
		return
	}
	txt := gaugeText(st)
	tx := x + innerX + (innerW-textWidth(txt))/2
	ty := y + innerY + (innerH-glyphH)/2
	drawText(c, tx, ty, txt, solid(o.Palette.TextOnFill))
}

// Strip renders the wide layout used on macOS: one cell per window, each a
// battery with its label above it.
func Strip(windows []model.Window, st map[model.Window]model.WindowStatus, o Options) *Icon {
	cells := make([]Cell, 0, len(windows))
	for _, w := range windows {
		s := st[w]
		s.Window = w
		cells = append(cells, Cell{Period: w.Label(), Status: s})
	}
	return StripCells(cells, o)
}

// StripCells renders explicitly labelled provider/limit cells on macOS.
func StripCells(items []Cell, o Options) *Icon {
	if len(items) == 0 {
		return &Icon{Image: image.NewRGBA(image.Rect(0, 0, 1, 1)), DotsW: 1, DotsH: 1}
	}

	cells := make([]*canvas, 0, len(items))
	for _, item := range items {
		cells = append(cells, stripCellCanvas(item, o))
	}

	totalW, totalH := 0, 0
	for i, cell := range cells {
		if i > 0 {
			totalW += cellGap
		}
		totalW += cell.w
		if cell.h > totalH {
			totalH = cell.h
		}
	}

	out := newCanvas(totalW, totalH)
	x := 0
	for i, cell := range cells {
		if i > 0 {
			x += cellGap
		}
		out.blit(cell, x, 0)
		x += cell.w
	}
	return &Icon{Image: out.toImage(o.Scale), DotsW: out.w, DotsH: out.h}
}

func stripCellCanvas(item Cell, o Options) *canvas {
	service, period := cellLabels(item)
	label := service + period
	st := item.Status
	labelW := textWidth(label)

	var artW, artH int
	if o.Mode == config.ModePercent {
		artW, artH = textWidth(percentText(st)), glyphH
	} else {
		artW, artH = batteryW, batteryH
	}

	cellW := artW
	if labelW > cellW {
		cellW = labelW
	}
	c := newCanvas(cellW, labelH+labelGap+artH)

	drawCellLabel(c, (cellW-labelW)/2, service, period, item.Service, o.Palette)

	ax, ay := (cellW-artW)/2, labelH+labelGap
	if o.Mode == config.ModePercent {
		// No painted backdrop here, so the accent is darkened to stay legible on
		// a light menu bar without disappearing on a dark one.
		drawText(c, ax, ay, percentText(st), solid(ink(o.Palette.accent(st))))
	} else {
		drawBattery(c, ax, ay, st, o, o.Mode == config.ModeBoth)
	}
	return c
}

func cellLabels(item Cell) (string, string) {
	if item.Period == "" {
		return "", item.Label
	}
	switch item.Service {
	case model.SourceClaudeCode:
		return "CL", item.Period
	case model.SourceCodex:
		return "CX", item.Period
	default:
		return "", item.Period
	}
}

func drawCellLabel(c *canvas, x int, service, period, source string, p Palette) {
	if service != "" {
		col := p.Period
		if source == model.SourceClaudeCode {
			col = p.Claude
		} else if source == model.SourceCodex {
			col = p.Codex
		}
		drawText(c, x, 0, service, solid(col))
		x += textWidth(service) + glyphGap
	}
	if period != "" {
		drawText(c, x, 0, period, solid(p.Period))
	}
}

// Square geometry for Windows, whose tray icons are a fixed square with no room
// for a caption.
const squareDots = 16

// Square renders the layout used on Windows. "stack" shows one thin bar per
// window; "single" shows the most constrained window as one battery.
func Square(windows []model.Window, st map[model.Window]model.WindowStatus, o Options, layout string) *Icon {
	items := make([]Cell, 0, len(windows))
	for _, w := range windows {
		s := st[w]
		s.Window = w
		items = append(items, Cell{Label: w.Label(), Status: s})
	}
	return SquareCells(items, o, layout)
}

// SquareCells renders one bar per provider/limit cell in the Windows tray.
func SquareCells(items []Cell, o Options, layout string) *Icon {
	c := newCanvas(squareDots, squareDots)
	if len(items) == 0 {
		return &Icon{Image: c.toImage(o.Scale), DotsW: squareDots, DotsH: squareDots}
	}
	if layout == "single" || len(items) == 1 {
		drawSquareSingle(c, items, o)
	} else {
		drawSquareStack(c, items, o)
	}
	return &Icon{Image: c.toImage(o.Scale), DotsW: squareDots, DotsH: squareDots}
}

func drawSquareStack(c *canvas, items []Cell, o Options) {
	n := len(items)
	gap := 2
	if n > 3 {
		gap = 1
	}
	barH := (squareDots - gap*(n-1)) / n
	if barH < 1 {
		barH = 1
	}
	totalH := n*barH + gap*(n-1)
	y := (squareDots - totalH) / 2

	for _, item := range items {
		s := item.Status
		accent := o.Palette.accent(s)
		if barH < 3 {
			c.fillRect(0, y, squareDots, barH, pale(accent))
			c.fillRect(0, y, filledColumns(s, squareDots), barH, accent)
			y += barH + gap
			continue
		}
		c.strokeRect(0, y, squareDots, barH, accent)
		// The bar is the only channel available at this size; there is no room
		// for digits, so the display mode does not change what is drawn.
		c.fillRect(1, y+1, squareDots-2, barH-2, pale(accent))
		c.fillRect(1, y+1, filledColumns(s, squareDots-2), barH-2, accent)
		y += barH + gap
	}
}

// squareBattery geometry: 14-wide body + 2-wide terminal fills the 16-dot grid.
const (
	sqBodyW  = 14
	sqInnerW = sqBodyW - 2
)

func drawSquareSingle(c *canvas, items []Cell, o Options) {
	s := mostConstrainedCells(items)
	accent := o.Palette.accent(s)
	y := (squareDots - batteryH) / 2

	c.strokeRect(0, y, sqBodyW, batteryH, accent)
	c.fillRect(sqBodyW, y+(batteryH-tipH)/2, tipW, tipH, accent)

	c.fillRect(1, y+1, sqInnerW, innerH, pale(accent))
	c.fillRect(1, y+1, filledColumns(s, sqInnerW), innerH, accent)

	if o.Mode == config.ModeBattery {
		return
	}
	// Only two digits fit; a full battery needs no number.
	txt := ""
	switch {
	case !s.Known:
		txt = "?"
	case s.RemainingPercent() < 99.5:
		txt = fmt.Sprintf("%02d", int(math.Round(s.RemainingPercent())))
	}
	if txt == "" {
		return
	}
	tx := 1 + (sqInnerW-textWidth(txt))/2
	ty := y + 1 + (innerH-glyphH)/2
	drawText(c, tx, ty, txt, solid(o.Palette.TextOnFill))
}

// mostConstrained returns the window with the least remaining capacity, which
// is the one worth showing when there is only room for one.
func mostConstrained(windows []model.Window, st map[model.Window]model.WindowStatus) model.WindowStatus {
	items := make([]Cell, 0, len(windows))
	for _, w := range windows {
		s := st[w]
		s.Window = w
		items = append(items, Cell{Status: s})
	}
	return mostConstrainedCells(items)
}

func mostConstrainedCells(items []Cell) model.WindowStatus {
	var best model.WindowStatus
	found := false
	for _, item := range items {
		s := item.Status
		if !s.Known {
			continue
		}
		if !found || s.UsedPercent > best.UsedPercent {
			best, found = s, true
		}
	}
	if !found && len(items) > 0 {
		return items[0].Status
	}
	return best
}
