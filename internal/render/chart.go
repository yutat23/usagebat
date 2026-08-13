package render

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/yutat23/usagebat/internal/history"
)

// Charts are drawn straight to SVG rather than on the dot grid the tray icon
// uses. The icon is 17 dots wide and has to survive being drawn at menu-bar
// size; a chart in a browser has room for real text, which is what lets the
// axis labels be written in the reader's language instead of the handful of
// uppercase glyphs the icon's font carries.

// Chart is a rendered chart, ready to inline into a page.
type Chart struct {
	SVG string
	// Width and Height are the viewBox, which the page scales to its column.
	Width, Height float64
	// Hover is the pointer readout: times and values in viewBox coordinates,
	// kept beside the picture rather than as one SVG node per sample.
	Hover Hover
}

// Hover is the data the settings page uses to name the point under the cursor.
type Hover struct {
	// Kind is "line" for a time series and "heat" for the weekday grid.
	Kind string `json:"k"`
	// Plot is the data rectangle in viewBox units: x, y, width, height.
	Plot [4]float64 `json:"p"`
	// Cell is the heatmap pitch, including the gutter between squares.
	Cell float64 `json:"c,omitempty"`
	// Gap is subtracted from Cell to get the visible square.
	Gap float64      `json:"g,omitempty"`
	Pts []HoverPoint `json:"pts"`
}

// HoverPoint is one readable sample. X/Y are viewBox coordinates so the page
// can find the nearest point without knowing how the chart was scaled.
type HoverPoint struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Label string  `json:"l"`
}

// Layout, in viewBox units. The page renders the chart at roughly this size,
// so they are also approximately pixels.
const (
	chartPadLeft   = 30
	chartPadRight  = 8
	chartPadTop    = 10
	chartPadBottom = 18
	chartFontSize  = 10
	defaultChartW  = 340.0
	defaultChartH  = 120.0
)

// ChartOptions sizes, colours and localises a chart.
type ChartOptions struct {
	Palette Palette
	// Width and Height are the viewBox. Zero picks a size that suits the chart.
	Width, Height float64
	// Dark selects the surface the chart is drawn on, which decides the grid
	// and label ink.
	Dark bool
	// Location renders timestamps in the user's zone; nil means time.Local.
	Location *time.Location
	// Label describes the chart to a screen reader.
	Label string
	// Weekdays are the heatmap's row labels, Sunday first. Empty falls back to
	// English abbreviations.
	Weekdays [7]string
	// Empty is shown when there is nothing to plot.
	Empty string
	// FormatHover names a time-series sample for the pointer readout. Nil
	// skips the readout, which is what the tests that only care about ink do.
	FormatHover func(at time.Time, value float64) string
	// FormatHeat names one heatmap cell. Nil skips the readout.
	FormatHeat func(weekday, hour int, value float64) string
}

func (o ChartOptions) size(defaultW, defaultH float64) (float64, float64) {
	w, h := o.Width, o.Height
	if w <= 0 {
		w = defaultW
	}
	if h <= 0 {
		h = defaultH
	}
	return w, h
}

func (o ChartOptions) location() *time.Location {
	if o.Location == nil {
		return time.Local
	}
	return o.Location
}

func (o ChartOptions) empty() string {
	if o.Empty == "" {
		return "No data"
	}
	return o.Empty
}

func (o ChartOptions) weekdays() [7]string {
	if o.Weekdays[0] == "" {
		return [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	}
	return o.Weekdays
}

// surface returns the background, grid and label colours.
func (o ChartOptions) surface() (bg, grid, label rgba) {
	if o.Dark {
		return rgba{0x23, 0x23, 0x26, 0xFF}, rgba{0x3A, 0x3A, 0x3C, 0xFF}, rgba{0x9A, 0x9A, 0xA0, 0xFF}
	}
	return rgba{0xFA, 0xFA, 0xFA, 0xFF}, rgba{0xE3, 0xE3, 0xE6, 0xFF}, rgba{0x6B, 0x6B, 0x70, 0xFF}
}

// plot is the region inside the axis labels.
type plot struct{ x, y, w, h float64 }

func newPlot(w, h float64) plot {
	return plot{
		x: chartPadLeft,
		y: chartPadTop,
		w: w - chartPadLeft - chartPadRight,
		h: h - chartPadTop - chartPadBottom,
	}
}

func (p plot) valid() bool { return p.w > 0 && p.h > 0 }

// builder accumulates SVG. Every chart opens with the frame and closes with
// the tag, so that lives here rather than in each of them.
type builder struct {
	b     strings.Builder
	o     ChartOptions
	w, h  float64
	hover Hover
}

func newBuilder(o ChartOptions, w, h float64) *builder {
	c := &builder{o: o, w: w, h: h}
	bg, _, _ := o.surface()
	fmt.Fprintf(&c.b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" `+
		`width="100%%" preserveAspectRatio="xMidYMid meet" role="img" aria-label="%s">`,
		num(w), num(h), escapeAttr(o.Label))
	fmt.Fprintf(&c.b, `<rect width="%s" height="%s" rx="6" fill="%s"/>`, num(w), num(h), hex(bg))
	return c
}

func (c *builder) done() Chart {
	c.b.WriteString("</svg>")
	return Chart{SVG: c.b.String(), Width: c.w, Height: c.h, Hover: c.hover}
}

func (c *builder) rect(x, y, w, h float64, fill rgba, radius float64) {
	if w <= 0 || h <= 0 {
		return
	}
	if radius > 0 {
		fmt.Fprintf(&c.b, `<rect x="%s" y="%s" width="%s" height="%s" rx="%s" fill="%s"/>`,
			num(x), num(y), num(w), num(h), num(radius), hex(fill))
		return
	}
	fmt.Fprintf(&c.b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`,
		num(x), num(y), num(w), num(h), hex(fill))
}

func (c *builder) line(x1, y1, x2, y2 float64, stroke rgba, width float64) {
	fmt.Fprintf(&c.b, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-width="%s"/>`,
		num(x1), num(y1), num(x2), num(y2), hex(stroke), num(width))
}

// polyline draws one run of the series. Runs exist because the line changes
// colour as the headroom crosses the same thresholds the battery uses.
func (c *builder) polyline(points []point, stroke rgba) {
	if len(points) < 2 {
		return
	}
	var coords strings.Builder
	for i, p := range points {
		if i > 0 {
			coords.WriteByte(' ')
		}
		fmt.Fprintf(&coords, "%s,%s", num(p.x), num(p.y))
	}
	fmt.Fprintf(&c.b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="1.75" `+
		`stroke-linejoin="round" stroke-linecap="round"/>`, coords.String(), hex(stroke))
}

// text writes a label. anchor is start, middle or end.
func (c *builder) text(x, y float64, s, anchor string, fill rgba) {
	fmt.Fprintf(&c.b, `<text x="%s" y="%s" font-size="%d" text-anchor="%s" fill="%s">%s</text>`,
		num(x), num(y), chartFontSize, anchor, hex(fill), escapeAttr(s))
}

func (c *builder) centred(p plot, s string) {
	_, _, label := c.o.surface()
	c.text(p.x+p.w/2, p.y+p.h/2+chartFontSize/3, s, "middle", label)
}

type point struct{ x, y float64 }

// RemainingChart plots the headroom line: what the battery showed, over time.
// It sawtooths, climbing back up whenever a window resets.
func RemainingChart(points []history.Point, o ChartOptions) Chart {
	w, h := o.size(defaultChartW, defaultChartH)
	c := newBuilder(o, w, h)
	area := newPlot(w, h)
	if !area.valid() {
		return c.done()
	}
	_, grid, label := o.surface()

	// Headroom is a percentage, so the axis is always the full range: a chart
	// that rescaled itself would make a quiet day look like a crisis.
	for _, tick := range []float64{100, 50, 0} {
		y := area.y + (100-tick)/100*area.h
		c.line(area.x, y, area.x+area.w, y, grid, 1)
		c.text(area.x-6, y+chartFontSize/3, fmt.Sprintf("%d", int(tick)), "end", label)
	}

	if len(points) < 2 {
		c.centred(area, o.empty())
		return c.done()
	}
	first, last := points[0].At, points[len(points)-1].At
	span := last.Sub(first).Seconds()
	if span <= 0 {
		c.centred(area, o.empty())
		return c.done()
	}

	// The line is split where its colour changes, so a dip into the warning
	// band reads the same here as it does on the battery.
	var run []point
	var runInk rgba
	flush := func() {
		c.polyline(run, runInk)
		if len(run) > 0 {
			run = run[len(run)-1:]
		}
	}
	for i, p := range points {
		at := point{
			x: area.x + p.At.Sub(first).Seconds()/span*area.w,
			y: area.y + (100-clampPercent(p.Value))/100*area.h,
		}
		ink := o.Palette.remainingInk(p.Value)
		switch {
		case i == 0:
			runInk = ink
		case ink != runInk:
			run = append(run, at)
			flush()
			runInk = ink
		}
		run = append(run, at)
	}
	c.polyline(run, runInk)

	c.hover = lineHover(points, area, first, last, o.location(), func(v float64) float64 {
		return area.y + (100-clampPercent(v))/100*area.h
	}, o.FormatHover)
	drawTimeAxis(c, area, first, last, label, o.location())
	return c.done()
}

// remainingInk colours the line by the same thresholds as the battery.
func (p Palette) remainingInk(remaining float64) rgba {
	switch {
	case remaining <= p.CriticalBelow:
		return p.Critical
	case remaining <= p.WarnBelow:
		return p.Warn
	default:
		return p.Good
	}
}

// UsageChart plots what was consumed between samples as columns.
func UsageChart(points []history.Point, o ChartOptions) Chart {
	w, h := o.size(defaultChartW, defaultChartH)
	c := newBuilder(o, w, h)
	area := newPlot(w, h)
	if !area.valid() {
		return c.done()
	}
	_, grid, label := o.surface()

	peak := 0.0
	for _, p := range points {
		if p.Value > peak {
			peak = p.Value
		}
	}
	c.line(area.x, area.y+area.h, area.x+area.w, area.y+area.h, grid, 1)
	if len(points) == 0 || peak <= 0 {
		c.centred(area, o.empty())
		return c.done()
	}

	// Only the top of the scale is labelled: the interesting comparison is
	// between columns, not against an absolute number.
	c.text(area.x-6, area.y+chartFontSize, formatCount(peak), "end", label)

	slot := area.w / float64(len(points))
	width := math.Max(slot*0.7, 1)
	for i, p := range points {
		height := p.Value / peak * area.h
		if height <= 0 {
			continue
		}
		x := area.x + float64(i)*slot + (slot-width)/2
		c.rect(x, area.y+area.h-height, width, height, o.Palette.Codex, math.Min(1.5, width/2))
	}
	c.hover = lineHover(points, area, points[0].At, points[len(points)-1].At, o.location(), func(v float64) float64 {
		return area.y + area.h - v/peak*area.h
	}, o.FormatHover)
	drawTimeAxis(c, area, points[0].At, points[len(points)-1].At, label, o.location())
	return c.done()
}

// ActivityChart draws the weekday-by-hour heatmap. Each cell is shaded by how
// much of the limit was consumed in that hour across the whole period.
func ActivityChart(heat history.Heatmap, o ChartOptions) Chart {
	const (
		gutter = 26.0 // room for the weekday labels
		cell   = 11.0
		gap    = 1.5
		// Hour labels plus a colour-ramp legend under them. The percent
		// labels sit on a baseline below the swatches; this padding has to
		// clear that baseline plus a descender, or the 0% / peak% captions
		// are clipped by the viewBox.
		heatPadBottom = 48.0
	)
	w, h := o.size(gutter+24*cell+chartPadRight, 7*cell+chartPadTop+heatPadBottom)
	c := newBuilder(o, w, h)
	_, grid, label := o.surface()

	peak := heat.Max()
	days := o.weekdays()
	for day := 0; day < 7; day++ {
		y := chartPadTop + float64(day)*cell
		c.text(gutter-6, y+cell/2+chartFontSize/3, days[day], "end", label)
		for hour := 0; hour < 24; hour++ {
			c.rect(gutter+float64(hour)*cell, y, cell-gap, cell-gap,
				o.Palette.heatInk(heat[day][hour], peak, grid), 2)
		}
	}
	c.hover = heatHover(heat, gutter, cell, gap, o.FormatHeat)
	// Every third hour is labelled; more would collide at this cell size.
	hourY := 7*cell + chartPadTop + chartFontSize + 2
	for hour := 0; hour < 24; hour += 3 {
		c.text(gutter+float64(hour)*cell+(cell-gap)/2, hourY,
			fmt.Sprintf("%d", hour), "middle", label)
	}
	drawHeatLegend(c, gutter, hourY+8, peak, o, grid, label)
	return c.done()
}

const heatLegendStops = 5

func drawHeatLegend(c *builder, x, y, peak float64, o ChartOptions, grid, ink rgba) {
	const swatch, gap = 9.0, 1.5
	for i := 0; i < heatLegendStops; i++ {
		t := float64(i) / float64(heatLegendStops-1)
		c.rect(x+float64(i)*(swatch+gap), y, swatch, swatch,
			o.Palette.heatInk(t*peak, peak, grid), 1.5)
	}
	c.text(x, y+swatch+chartFontSize, "0%", "start", ink)
	c.text(x+float64(heatLegendStops)*(swatch+gap)-gap, y+swatch+chartFontSize,
		fmt.Sprintf("%.0f%%", peak), "end", ink)
}

// heatInk ramps an empty cell towards the accent colour. An hour with no
// recorded usage keeps the grid colour rather than a washed-out accent, so
// "never used" is distinguishable from "barely used".
func (p Palette) heatInk(value, peak float64, empty rgba) rgba {
	if peak <= 0 || value <= 0 {
		return empty
	}
	return mix(empty, p.Claude, math.Min(1, value/peak))
}

// drawTimeAxis labels the ends of the range.
func drawTimeAxis(c *builder, area plot, from, to time.Time, label rgba, loc *time.Location) {
	y := area.y + area.h + chartFontSize + 5
	c.text(area.x, y, from.In(loc).Format("1/2 15:04"), "start", label)
	c.text(area.x+area.w, y, to.In(loc).Format("1/2 15:04"), "end", label)
}

// lineHover keeps one sample per viewBox pixel. A week of five-minute history
// is thousands of points, and the picture is only a few hundred units across,
// so a node per sample would dwarf the polyline it is describing.
func lineHover(points []history.Point, area plot, first, last time.Time, loc *time.Location, yOf func(float64) float64, format func(time.Time, float64) string) Hover {
	if format == nil || len(points) == 0 {
		return Hover{}
	}
	span := last.Sub(first).Seconds()
	if span <= 0 {
		return Hover{}
	}
	type sample struct {
		at          time.Time
		value, x, y float64
	}
	best := map[int]sample{}
	keys := make([]int, 0, int(area.w)+1)
	for _, p := range points {
		x := area.x + p.At.Sub(first).Seconds()/span*area.w
		key := int(math.Round(x))
		if _, ok := best[key]; !ok {
			keys = append(keys, key)
		}
		best[key] = sample{at: p.At, value: p.Value, x: x, y: yOf(p.Value)}
	}
	out := Hover{Kind: "line", Plot: [4]float64{hoverCoord(area.x), hoverCoord(area.y), hoverCoord(area.w), hoverCoord(area.h)}}
	for _, key := range keys {
		s := best[key]
		out.Pts = append(out.Pts, HoverPoint{
			X: hoverCoord(s.x), Y: hoverCoord(s.y), Label: format(s.at.In(loc), s.value),
		})
	}
	return out
}

func heatHover(heat history.Heatmap, gutter, cell, gap float64, format func(weekday, hour int, value float64) string) Hover {
	if format == nil {
		return Hover{}
	}
	out := Hover{
		Kind: "heat",
		Plot: [4]float64{gutter, chartPadTop, 24 * cell, 7 * cell},
		Cell: cell,
		Gap:  gap,
	}
	for day := 0; day < 7; day++ {
		y := chartPadTop + float64(day)*cell
		for hour := 0; hour < 24; hour++ {
			out.Pts = append(out.Pts, HoverPoint{
				X:     hoverCoord(gutter + float64(hour)*cell),
				Y:     hoverCoord(y),
				Label: format(day, hour, heat[day][hour]),
			})
		}
	}
	return out
}

func hoverCoord(v float64) float64 { return math.Round(v*10) / 10 }

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// num formats a coordinate without the trailing zeros that would otherwise
// double the size of every path in the document.
func num(v float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

// FormatCount abbreviates a token count to something that fits an axis or a
// hover readout.
func FormatCount(v float64) string { return formatCount(v) }

// formatCount abbreviates a token count to something that fits an axis.
func formatCount(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.0fG", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.0fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.0fK", v/1e3)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}
