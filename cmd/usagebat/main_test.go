package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/model"
)

func TestDisplayCellsUseShortestLimitPerService(t *testing.T) {
	cfg := config.Default()
	a := &app{cfg: cfg}
	snap := &model.Snapshot{Sources: []model.SourceStatus{
		{ID: model.SourceClaudeCode, Windows: map[model.Window]model.WindowStatus{
			model.Window5h:     {Known: true, UsedPercent: 71},
			model.WindowWeekly: {Known: true, UsedPercent: 8},
		}},
		{ID: "codex:work", Windows: map[model.Window]model.WindowStatus{
			model.WindowMonthly: {Known: true, UsedPercent: 12},
		}},
	}}

	cells := a.displayCells(snap)
	if len(cells) != 2 {
		t.Fatalf("got %d cells, want one per service: %+v", len(cells), cells)
	}
	if cells[0].Service != model.SourceClaudeCode || cells[0].Period != "5H" || cells[0].Status.UsedPercent != 71 {
		t.Errorf("Claude cell = %+v", cells[0])
	}
	if cells[1].Service != model.SourceCodex || cells[1].Period != "MO" || cells[1].Status.UsedPercent != 12 {
		t.Errorf("Codex cell = %+v", cells[1])
	}
}

func TestDisplayCellsOmitUnavailableService(t *testing.T) {
	cfg := config.Default()
	a := &app{cfg: cfg}
	snap := &model.Snapshot{Sources: []model.SourceStatus{
		{ID: model.SourceCodex, Windows: map[model.Window]model.WindowStatus{
			model.WindowWeekly: {Known: true, UsedPercent: 6},
		}},
	}}

	cells := a.displayCells(snap)
	if len(cells) != 1 || cells[0].Service != model.SourceCodex || cells[0].Period != "WK" {
		t.Fatalf("only installed/present Codex should be drawn: %+v", cells)
	}
	menu := a.buildMenu(snap, time.Now())
	for _, item := range menu {
		if item.Title == "Claude Code" || item.Title == "Claude Code limits" {
			t.Fatalf("unavailable Claude should not be offered in menu: %+v", item)
		}
	}
}

func TestRebuildCreatesOnlyInstalledProviders(t *testing.T) {
	cfg := config.Default()
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.Sources.ClaudeCode.UsageCommand.Path = fakeClaude
	cfg.Sources.Codex.Path = filepath.Join(dir, "missing-codex")

	a := &app{cfg: cfg}
	a.rebuild()
	if len(a.providers) != 1 || a.providers[0].ID() != model.SourceClaudeCode {
		t.Fatalf("providers = %+v, want Claude only", a.providers)
	}
}

func TestShouldDetachOnlyInteractiveDefaultRun(t *testing.T) {
	if !shouldDetach(false, "", true) {
		t.Fatal("an interactive default run should detach")
	}
	for _, tc := range []struct {
		foreground  bool
		dump        string
		interactive bool
	}{
		{foreground: true, interactive: true},
		{dump: "icon.png", interactive: true},
		{},
	} {
		if shouldDetach(tc.foreground, tc.dump, tc.interactive) {
			t.Fatalf("unexpected detach for %+v", tc)
		}
	}
}

func TestDisplayCellsUseIndependentExplicitLimits(t *testing.T) {
	cfg := config.Default()
	cfg.DisplayLimits[model.SourceClaudeCode] = config.LimitDisplay{
		Windows: []string{"weekly"},
	}
	cfg.DisplayLimits[model.SourceCodex] = config.LimitDisplay{
		Windows: []string{"monthly"},
	}
	a := &app{cfg: cfg}
	snap := &model.Snapshot{Sources: []model.SourceStatus{
		{ID: model.SourceClaudeCode, Windows: map[model.Window]model.WindowStatus{
			model.Window5h: {Known: true}, model.WindowWeekly: {Known: true},
		}},
		{ID: model.SourceCodex, Windows: map[model.Window]model.WindowStatus{
			model.Window5h: {Known: true}, model.WindowMonthly: {Known: true},
		}},
	}}
	cells := a.displayCells(snap)
	if len(cells) != 2 || cells[0].Service != model.SourceClaudeCode || cells[0].Period != "WK" ||
		cells[1].Service != model.SourceCodex || cells[1].Period != "MO" {
		t.Fatalf("independent cells = %+v", cells)
	}
}
