package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/history"
	"github.com/yutat23/usagebat/internal/model"
	"github.com/yutat23/usagebat/internal/webui"
)

// recordedApp is an app with a week of history behind it, which is what the
// charts need before they have anything to draw.
func recordedApp(t *testing.T) *app {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.jsonl")
	recorder := history.NewAt(path, history.Options{Interval: time.Minute})

	start := time.Now().Add(-48 * time.Hour)
	remaining := 100.0
	for i := 0; i < 90; i++ {
		if remaining -= 4; remaining < 10 {
			remaining = 100
		}
		snap := &model.Snapshot{Sources: []model.SourceStatus{{
			ID: model.SourceClaudeCode, Name: "Claude Code",
			Windows: map[model.Window]model.WindowStatus{
				model.Window5h: {Known: true, UsedPercent: 100 - remaining},
			},
			Tokens: map[model.Window]model.Tokens{
				model.Window5h: {Input: int64(i) * 1000},
			},
		}}}
		if _, err := recorder.Observe(snap, start.Add(time.Duration(i)*30*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	a := &app{cfg: config.Default(), recorder: recorder, refresh: make(chan struct{}, 1)}
	a.snap.Store(&model.Snapshot{Sources: []model.SourceStatus{{
		ID: model.SourceClaudeCode, Name: "Claude Code",
		Windows: map[model.Window]model.WindowStatus{model.Window5h: {Known: true}},
	}}})
	return a
}

func TestPageDrawsChartsFromHistory(t *testing.T) {
	page := recordedApp(t).settingsPage()

	var charts []webui.Row
	for _, section := range page.Sections {
		for _, row := range section.Rows {
			if row.Kind == webui.KindChart {
				charts = append(charts, row)
			}
		}
	}
	if len(charts) == 0 {
		t.Fatalf("no charts on the page: %+v", page.Sections)
	}
	for _, chart := range charts {
		markup := string(chart.SVG)
		if !strings.Contains(markup, "<svg") || !strings.Contains(markup, "<rect") {
			t.Errorf("chart %q carries no drawing", chart.Label)
		}
		// Both themes are sent and the page's media query keeps one; a chart
		// with only one would be invisible in the other theme.
		if !strings.Contains(markup, "only-light") || !strings.Contains(markup, "only-dark") {
			t.Errorf("chart %q was not rendered for both themes", chart.Label)
		}
	}
}

// Turning recording off has to stop the charts as well, or the screen keeps
// showing data the user asked it to stop collecting.
func TestPageWithoutHistoryExplainsItself(t *testing.T) {
	a := recordedApp(t)
	a.cfg.History.Enabled = false

	for _, section := range a.settingsPage().Sections {
		for _, row := range section.Rows {
			if row.Kind == webui.KindChart {
				t.Fatalf("charts drawn while recording is off: %q", row.Label)
			}
		}
	}
}

// Charts and settings share one screen but remain separate groups: charts use
// the full-width area and settings flow as cards below them.
func TestChartsAndSettingsAreSeparateAreas(t *testing.T) {
	page := recordedApp(t).settingsPage()

	var charts, controls int
	for _, section := range page.Main() {
		for _, row := range section.Rows {
			if row.Kind == webui.KindChart {
				charts++
			}
			if row.ID != "" {
				t.Errorf("setting %q is in the chart column", row.ID)
			}
		}
		if !section.Grid {
			t.Errorf("chart section %q does not tile", section.Title)
		}
	}
	for _, section := range page.Aside() {
		for _, row := range section.Rows {
			if row.Kind == webui.KindChart {
				t.Errorf("chart %q is in the settings column", row.Label)
			}
			if row.ID != "" {
				controls++
			}
		}
	}
	if charts == 0 || controls == 0 {
		t.Fatalf("panes = %d charts, %d controls; want both filled", charts, controls)
	}
}

func TestSettingsPageUsesFocusedNavigationGroups(t *testing.T) {
	groups := recordedApp(t).settingsPage().SettingsGroups()
	want := []string{"general", "accounts", "alerts-data", "appearance"}
	if len(groups) != len(want) {
		t.Fatalf("groups = %+v, want %v", groups, want)
	}
	for i, id := range want {
		if groups[i].ID != id || groups[i].Title == "" || len(groups[i].Sections) == 0 {
			t.Errorf("group %d = %+v, want populated %q", i, groups[i], id)
		}
	}
}

func TestSettingsPageOffersProfileOrderingControls(t *testing.T) {
	a := recordedApp(t)
	a.cfg.Sources.Codex.Profiles = []config.Profile{
		{Path: "~/.codex-w", Label: "Work"},
		{Path: "~/.codex-p", Label: "Personal"},
	}
	ids := map[string]bool{}
	for _, row := range allRows(a.settingsPage()) {
		ids[row.ID] = true
	}
	for _, want := range []string{"profile:move:0:down", "profile:move:1:up"} {
		if !ids[want] {
			t.Errorf("settings page is missing %q", want)
		}
	}
	for _, impossible := range []string{"profile:move:0:up", "profile:move:1:down"} {
		if ids[impossible] {
			t.Errorf("settings page offered impossible move %q", impossible)
		}
	}
}

// A provider that reports no tokens used to get a chart-sized frame saying
// "no data", which costs as much room as a real chart and says less.
func TestChartsAreOmittedWhenThereIsNothingToPlot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	recorder := history.NewAt(path, history.Options{Interval: time.Minute})

	start := time.Now().Add(-6 * time.Hour)
	for i := 0; i < 20; i++ {
		// Codex reports headroom but no per-window token tally.
		snap := &model.Snapshot{Sources: []model.SourceStatus{{
			ID: model.SourceCodex, Name: "Codex",
			Windows: map[model.Window]model.WindowStatus{
				model.WindowMonthly: {Known: true, UsedPercent: float64(i)},
			},
		}}}
		if _, err := recorder.Observe(snap, start.Add(time.Duration(i)*10*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	a := &app{cfg: config.Default(), recorder: recorder, refresh: make(chan struct{}, 1)}
	a.snap.Store(&model.Snapshot{Sources: []model.SourceStatus{{
		ID: model.SourceCodex, Name: "Codex",
		Windows: map[model.Window]model.WindowStatus{model.WindowMonthly: {Known: true}},
	}}})

	page := a.settingsPage()
	var headroom, tokens int
	for _, section := range page.Main() {
		for _, row := range section.Rows {
			if row.Kind != webui.KindChart {
				continue
			}
			if strings.Contains(section.Title, "Token") || strings.Contains(section.Title, "トークン") {
				tokens++
			} else {
				headroom++
			}
		}
	}
	if headroom == 0 {
		t.Fatal("the headroom chart is missing; that series does have data")
	}
	if tokens != 0 {
		t.Errorf("drew %d token charts for a provider that reports no tokens", tokens)
	}
}

func allRows(page webui.Page) []webui.Row {
	var out []webui.Row
	for _, section := range page.Sections {
		out = append(out, section.Rows...)
	}
	return out
}

// The screen is the only place these can be reached, so every row it offers
// has to be a row the click handler knows.
func TestSettingsPageRowsAreAllHandled(t *testing.T) {
	known := map[string]bool{
		idConfig: true, idAutostart: true, idNotifications: true,
		idHistory: true, idUpdateCheck: true, idLimitAlerts: true,
		idRefresh: true,
	}
	prefixes := []string{idModePfx, idSourcePfx, idLimitPfx, idLanguagePfx, "setting:", "profile:", "claude-profile:"}

	for _, section := range recordedApp(t).settingsPage().Sections {
		for _, row := range section.Rows {
			if row.ID == "" {
				continue
			}
			if known[row.ID] {
				continue
			}
			handled := false
			for _, prefix := range prefixes {
				handled = handled || strings.HasPrefix(row.ID, prefix)
			}
			if !handled {
				t.Errorf("row %q has no handler", row.ID)
			}
		}
	}
}

// Nothing arriving over a socket should be able to close the app.
func TestApplySettingRefusesQuit(t *testing.T) {
	a := &app{cfg: config.Default(), backend: &stubBackend{}, refresh: make(chan struct{}, 1)}
	if err := a.applySetting(idQuit, ""); err != nil {
		t.Fatal(err)
	}
	if got := a.backend.(*stubBackend).quitCount(); got != 0 {
		t.Fatalf("quit count = %d; the settings page must not be able to quit", got)
	}
}
