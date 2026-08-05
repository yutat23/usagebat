package main

import (
	"html/template"
	"log"
	"strings"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/history"
	"github.com/yutat23/usagebat/internal/i18n"
	"github.com/yutat23/usagebat/internal/model"
	"github.com/yutat23/usagebat/internal/render"
	"github.com/yutat23/usagebat/internal/webui"
)

// chartWindow is how far back the charts look.
const chartWindow = 7 * 24 * time.Hour

// Chart geometry, in viewBox units. The page scales these to the column, so
// they set the proportion; two charts sit side by side at this shape.
const (
	chartW = 340
	chartH = 120
)

// chartSections builds the usage charts from recorded history.
//
// Reading the file happens per page load rather than on a timer: the settings
// screen is opened occasionally, and holding a week of samples in memory for a
// process that runs for weeks would cost more than the read.
func (a *app) chartSections(cfg *config.Config, snap *model.Snapshot, p i18n.Printer) []webui.Section {
	heading := p.T("chartRemaining") + " — " + p.T("lastDays")
	pending := []webui.Section{{
		Title: heading,
		Rows:  []webui.Row{{Label: p.T("chartsPending"), Kind: webui.KindText}},
	}}
	if a.recorder == nil || !cfg.HistorySettings().Enabled {
		return pending
	}
	now := time.Now()
	samples, err := a.recorder.Load(now.Add(-chartWindow), now)
	if err != nil {
		log.Printf("history: %v", err)
	}
	if len(samples) < 2 {
		return pending
	}

	// The page follows the browser's theme, so the charts have to as well. A
	// page served to a dark browser with light charts looks broken, and the
	// server cannot know which it is — so both are drawn and CSS picks.
	remaining := webui.Section{Title: heading}
	var tokens, activity webui.Section
	tokens.Title = p.T("chartTokens")
	activity.Title = p.T("chartActivity")

	for _, series := range chartSeries(cfg, snap, samples) {
		label := seriesLabel(series, snap, p)
		if points := history.Remaining(samples, series); len(points) > 1 {
			remaining.Rows = append(remaining.Rows, chartRow(label, func(o render.ChartOptions) render.Chart {
				return render.RemainingChart(points, o)
			}, cfg, p, label))
		}
		// A provider that reports no tokens would otherwise get an empty frame
		// saying so, which takes as much room as a real chart and says less.
		if points := history.TokenUsage(samples, series); consumed(points) {
			tokens.Rows = append(tokens.Rows, chartRow(label, func(o render.ChartOptions) render.Chart {
				return render.UsageChart(points, o)
			}, cfg, p, label))
		}
		if heat := history.Activity(samples, series, time.Local); heat.Max() > 0 {
			activity.Rows = append(activity.Rows, chartRow(label, func(o render.ChartOptions) render.Chart {
				o.Width, o.Height = 0, 0 // the heatmap sizes itself
				return render.ActivityChart(heat, o)
			}, cfg, p, label))
		}
	}

	sections := []webui.Section{}
	for _, section := range []webui.Section{remaining, tokens, activity} {
		if len(section.Rows) > 0 {
			section.Grid = true
			sections = append(sections, section)
		}
	}
	if len(sections) == 0 {
		return pending
	}
	return sections
}

// consumed reports whether a series has anything worth plotting.
func consumed(points []history.Point) bool {
	if len(points) < 2 {
		return false
	}
	for _, p := range points {
		if p.Value > 0 {
			return true
		}
	}
	return false
}

// chartRow renders one chart for both themes. Only one is ever visible: the
// page's media query hides the other, which is how a server that cannot see
// the browser's theme still matches it.
func chartRow(caption string, draw func(render.ChartOptions) render.Chart,
	cfg *config.Config, p i18n.Printer, label string) webui.Row {

	var markup strings.Builder
	for _, dark := range []bool{false, true} {
		class := "only-light"
		if dark {
			class = "only-dark"
		}
		chart := draw(render.ChartOptions{
			Palette:  render.PaletteFrom(cfg.Palette(), dark),
			Dark:     dark,
			Width:    chartW,
			Height:   chartH,
			Label:    label,
			Weekdays: p.Weekdays(),
			Empty:    p.T("chartNoData"),
		})
		markup.WriteString(`<span class="` + class + `">` + chart.SVG + `</span>`)
	}
	return webui.Row{
		Label: caption,
		Kind:  webui.KindChart,
		// The markup is built here from rendered charts; nothing in it comes
		// from anywhere a user could reach.
		SVG: template.HTML(markup.String()),
	}
}

// chartSeries picks what to plot: the limits the icon draws, restricted to the
// ones history actually holds.
func chartSeries(cfg *config.Config, snap *model.Snapshot, samples []history.Sample) []history.Series {
	recorded := map[history.Series]bool{}
	for _, series := range history.Available(samples) {
		recorded[series] = true
	}
	var out []history.Series
	seen := map[history.Series]bool{}
	for _, cell := range displayCells(cfg, snap) {
		for series := range recorded {
			if seen[series] || series.Window != cell.Status.Window {
				continue
			}
			// Codex profiles carry IDs such as "codex:work"; the icon cell only
			// names the family, so every profile under it is plotted.
			if series.Source != cell.Service && !strings.HasPrefix(series.Source, cell.Service+":") {
				continue
			}
			seen[series] = true
			out = append(out, series)
		}
	}
	return out
}

// seriesLabel prefers the name the provider reports, which is what the menu
// shows; the raw source id carries a directory hash nobody can read.
func seriesLabel(series history.Series, snap *model.Snapshot, p i18n.Printer) string {
	name := series.Source
	for _, src := range snap.Sources {
		if src.ID == series.Source && src.Name != "" {
			name = src.Name
			break
		}
	}
	switch name {
	case model.SourceClaudeCode:
		name = "Claude Code"
	case model.SourceCodex:
		name = "Codex"
	}
	return name + " · " + p.WindowTitle(series.Window)
}
