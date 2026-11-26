package render

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/history"
)

func testChartOptions(dark bool) ChartOptions {
	return ChartOptions{
		Palette:  PaletteFrom(config.Default().Colors, dark),
		Dark:     dark,
		Location: time.UTC,
		Label:    "test chart",
	}
}

// A day of work: headroom drains, the window resets, it drains again.
func sawtooth(start time.Time) []history.Point {
	var points []history.Point
	remaining := 100.0
	for i := 0; i < 96; i++ {
		remaining -= 3.5
		if remaining < 8 {
			remaining = 100
		}
		points = append(points, history.Point{
			At:    start.Add(time.Duration(i) * 15 * time.Minute),
			Value: remaining,
		})
	}
	return points
}

// The SVG is inlined into the settings page, so malformed markup would break
// the whole document rather than one image.
func TestChartsProduceWellFormedSVG(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	o := testChartOptions(false)
	o.Width, o.Height = 320, 120

	var heat history.Heatmap
	heat[time.Monday][9] = 30
	for name, chart := range map[string]Chart{
		"remaining": RemainingChart(sawtooth(start), o),
		"usage":     UsageChart(sawtooth(start), o),
		"activity":  ActivityChart(heat, o),
	} {
		if err := xml.Unmarshal([]byte(chart.SVG), new(struct{})); err != nil {
			t.Errorf("%s chart is not well-formed XML: %v", name, err)
		}
		if !strings.Contains(chart.SVG, `aria-label="test chart"`) {
			t.Errorf("%s chart carries no description", name)
		}
		if chart.Width <= 0 || chart.Height <= 0 {
			t.Errorf("%s chart viewBox = %vx%v", name, chart.Width, chart.Height)
		}
	}
}

// The label goes into an attribute; a quote in it would end the attribute and
// let the rest be read as markup.
func TestChartLabelIsEscaped(t *testing.T) {
	o := testChartOptions(false)
	o.Label = `Claude "5h" & <Codex>`
	chart := RemainingChart(sawtooth(time.Now()), o)
	if strings.Contains(chart.SVG, `aria-label="Claude "5h"`) {
		t.Fatalf("label was not escaped:\n%s", chart.SVG[:200])
	}
	if err := xml.Unmarshal([]byte(chart.SVG), new(struct{})); err != nil {
		t.Fatalf("escaping produced invalid XML: %v", err)
	}
}

// The whole chart is inlined into the page, so a line has to be one element
// rather than a rectangle per step.
func TestLineChartIsCompact(t *testing.T) {
	o := testChartOptions(false)
	chart := RemainingChart(sawtooth(time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)), o)
	if got := strings.Count(chart.SVG, "<polyline"); got == 0 {
		t.Fatal("the line was not drawn as a polyline")
	}
	if len(chart.SVG) > 8000 {
		t.Errorf("chart is %d bytes; it is inlined into every page load", len(chart.SVG))
	}
}

// Axis labels are real text now, which is the point: the dot font could not
// write anything but uppercase ASCII.
func TestChartsWriteRealText(t *testing.T) {
	o := testChartOptions(false)
	o.Weekdays = [7]string{"日", "月", "火", "水", "木", "金", "土"}
	var heat history.Heatmap
	heat[time.Monday][9] = 30

	chart := ActivityChart(heat, o)
	if !strings.Contains(chart.SVG, "<text") {
		t.Fatal("the heatmap has no text labels")
	}
	if !strings.Contains(chart.SVG, ">月<") {
		t.Errorf("localised weekday labels were not used:\n%s", chart.SVG)
	}
}

// A chart with one sample, or none, must still draw its axes rather than
// dividing by a zero time span.
func TestChartsSurviveTooLittleData(t *testing.T) {
	o := testChartOptions(false)
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	for _, points := range [][]history.Point{
		nil,
		{{At: now, Value: 50}},
		{{At: now, Value: 50}, {At: now, Value: 40}}, // no elapsed time
	} {
		for name, chart := range map[string]Chart{
			"remaining": RemainingChart(points, o),
			"usage":     UsageChart(points, o),
		} {
			if chart.SVG == "" {
				t.Fatalf("%s chart came out empty for %d points", name, len(points))
			}
			if err := xml.Unmarshal([]byte(chart.SVG), new(struct{})); err != nil {
				t.Fatalf("%s chart is malformed for %d points: %v", name, len(points), err)
			}
		}
	}
}

func TestActivityChartDistinguishesUnusedHours(t *testing.T) {
	o := testChartOptions(true)
	_, grid, _ := o.surface()
	if got := o.Palette.heatInk(60, 60, grid); got == grid {
		t.Error("the peak bucket is indistinguishable from an unused one")
	}
	if got := o.Palette.heatInk(0, 60, grid); got != grid {
		t.Error("an unused hour must keep the grid colour")
	}
}

func TestChartsCarryHoverReadout(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	o := testChartOptions(false)
	o.FormatHover = func(at time.Time, v float64) string {
		return at.Format("1/2 15:04") + " · " + fmt.Sprintf("%.0f", v)
	}
	o.FormatHeat = func(day, hour int, v float64) string {
		return fmt.Sprintf("%d %02d:00 · %.0f", day, hour, v)
	}
	points := sawtooth(start)

	remaining := RemainingChart(points, o)
	if remaining.Hover.Kind != "line" || len(remaining.Hover.Pts) == 0 {
		t.Fatalf("remaining hover = %+v", remaining.Hover)
	}
	if !strings.Contains(remaining.Hover.Pts[0].Label, "8/3 00:00") {
		t.Errorf("remaining first label = %q", remaining.Hover.Pts[0].Label)
	}
	if strings.Contains(remaining.SVG, remaining.Hover.Pts[0].Label) {
		t.Error("the readout was inlined into the SVG; it belongs beside the picture")
	}

	usage := UsageChart(points, o)
	if usage.Hover.Kind != "bar" || len(usage.Hover.Pts) == 0 {
		t.Fatalf("usage hover = %+v", usage.Hover)
	}

	var heat history.Heatmap
	heat[time.Monday][9] = 30
	activity := ActivityChart(heat, o)
	if activity.Hover.Kind != "heat" || len(activity.Hover.Pts) != 7*24 {
		t.Fatalf("activity hover pts = %d, kind = %q", len(activity.Hover.Pts), activity.Hover.Kind)
	}
	mon9 := activity.Hover.Pts[int(time.Monday)*24+9]
	if !strings.Contains(mon9.Label, "09:00") || !strings.Contains(mon9.Label, "30") {
		t.Errorf("Monday 9:00 label = %q", mon9.Label)
	}
	if !strings.Contains(activity.SVG, "0%") || !strings.Contains(activity.SVG, "30%") {
		t.Errorf("heatmap has no percent legend:\n%s", activity.SVG)
	}
	// The 0% / peak% captions sit below the swatches. If Height does not
	// clear their baseline, the viewBox clips the bottom of the glyphs.
	if activity.Height < 134 {
		t.Errorf("heatmap height = %v; the percent legend would be clipped", activity.Height)
	}
}

func TestHoverLabelsRoundTripThroughJSON(t *testing.T) {
	o := testChartOptions(false)
	o.FormatHover = func(time.Time, float64) string { return `Claude "5h" & <x>` }
	chart := RemainingChart(sawtooth(time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)), o)
	raw, err := json.Marshal(chart.Hover)
	if err != nil {
		t.Fatal(err)
	}
	var parsed Hover
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Pts[0].Label != `Claude "5h" & <x>` {
		t.Errorf("round-trip = %q", parsed.Pts[0].Label)
	}
}

func TestUsageHoverUsesBarPositions(t *testing.T) {
	o := testChartOptions(false)
	o.FormatHover = func(at time.Time, count float64) string {
		return at.Format("15:04") + " · " + FormatCount(count)
	}
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	points := []history.Point{
		{At: start, Value: 50},
		{At: start.Add(time.Minute), Value: 900},
		{At: start.Add(time.Hour), Value: 90},
	}
	chart := UsageChart(points, o)
	for i, want := range []string{"00:00 · 50", "00:01 · 900", "01:00 · 90"} {
		got := chart.Hover.Pts[i]
		x := chart.Hover.Plot[0] + (float64(i)+0.5)*chart.Hover.Plot[2]/3
		if got.X != hoverCoord(x) || got.Label != want {
			t.Errorf("bar %d hover = %+v, want x=%.2f and %q", i, got, x, want)
		}
	}
	chart = UsageChart(points[:1], o)
	if len(chart.Hover.Pts) != 1 || chart.Hover.Pts[0].Label != "00:00 · 50" {
		t.Errorf("single bar hover = %+v", chart.Hover)
	}
}

func TestRemainingHoverKeepsAdjacentExtremes(t *testing.T) {
	o := testChartOptions(false)
	o.FormatHover = func(_ time.Time, v float64) string { return fmt.Sprintf("%g", v) }
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	chart := RemainingChart([]history.Point{
		{At: start, Value: 90},
		{At: start.Add(3 * time.Minute), Value: 5},
		{At: start.Add(4 * time.Minute), Value: 100},
		{At: start.Add(7 * 24 * time.Hour), Value: 87},
	}, o)
	if got := chart.Hover.Pts; len(got) != 4 || got[1].Label != "5" || got[2].Label != "100" {
		t.Errorf("hover samples = %+v", got)
	}
}

func TestActivityLegendKeepsSmallPeaksDistinctFromZero(t *testing.T) {
	for _, tc := range []struct {
		peak float64
		want string
	}{{0.4, "0.4%"}, {0.03, "&lt;0.1%"}} {
		var heat history.Heatmap
		heat[time.Monday][9] = tc.peak
		chart := ActivityChart(heat, testChartOptions(false))
		if !strings.Contains(chart.SVG, ">"+tc.want+"</text>") {
			t.Errorf("peak of %g has no %q label", tc.peak, tc.want)
		}
	}
}

func TestRemainingInkFollowsTheBatteryThresholds(t *testing.T) {
	p := PaletteFrom(config.Default().Colors, false)
	if p.remainingInk(90) != p.Good {
		t.Error("plenty of headroom should read as good")
	}
	if p.remainingInk(30) != p.Warn {
		t.Error("below the warn threshold should read as warning")
	}
	if p.remainingInk(5) != p.Critical {
		t.Error("below the critical threshold should read as critical")
	}
}

func TestFormatCount(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{{0, "0"}, {950, "950"}, {1500, "2K"}, {2_400_000, "2M"}, {3e9, "3G"}} {
		if got := FormatCount(tc.in); got != tc.want {
			t.Errorf("FormatCount(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Writes the charts to build/ so their appearance can be reviewed in a
// browser; nothing about a chart's looks can be asserted in a test.
func TestWriteChartPreviews(t *testing.T) {
	if os.Getenv("USAGEBAT_CHART_PREVIEW") == "" {
		t.Skip("set USAGEBAT_CHART_PREVIEW=1 to write build/chart-preview.html")
	}
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	points := sawtooth(start)

	var usage []history.Point
	for i, point := range points {
		usage = append(usage, history.Point{At: point.At, Value: float64((i%7)*1800 + 400)})
	}
	var heat history.Heatmap
	for day := 0; day < 7; day++ {
		for hour := 9; hour < 19; hour++ {
			heat[day][hour] = float64((day*hour)%13) * 4
		}
	}

	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><title>chart preview</title>` +
		`<style>body{margin:0;font:14px system-ui}` +
		`.pane{padding:2rem}.light{background:#fff;color:#111}.dark{background:#1c1c1e;color:#eee}` +
		`figure{margin:0 0 2rem;max-width:40rem}figcaption{margin-bottom:.4rem;opacity:.7}</style>`)
	for _, dark := range []bool{false, true} {
		o := testChartOptions(dark)
		o.Width, o.Height = 340, 120
		class := "light"
		if dark {
			class = "dark"
		}
		fmt.Fprintf(&b, `<div class="pane %s">`, class)
		for _, c := range []struct {
			name  string
			chart Chart
		}{
			{"remaining", RemainingChart(points, o)},
			{"token usage", UsageChart(usage, o)},
			{"activity", ActivityChart(heat, ChartOptions{
				Palette: o.Palette, Dark: dark, Location: time.UTC, Label: "activity",
			})},
		} {
			fmt.Fprintf(&b, `<figure><figcaption>%s</figcaption>%s</figure>`, c.name, c.chart.SVG)
		}
		b.WriteString(`</div>`)
	}

	dir := filepath.Join("..", "..", "build")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chart-preview.html"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
