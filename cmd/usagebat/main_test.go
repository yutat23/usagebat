package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/model"
	"github.com/yutat23/usagebat/internal/provider"
	"github.com/yutat23/usagebat/internal/tray"
)

func TestDisplayCellsUseShortestLimitPerService(t *testing.T) {
	cfg := config.Default()
	snap := &model.Snapshot{Sources: []model.SourceStatus{
		{ID: model.SourceClaudeCode, Windows: map[model.Window]model.WindowStatus{
			model.Window5h:     {Known: true, UsedPercent: 71},
			model.WindowWeekly: {Known: true, UsedPercent: 8},
		}},
		{ID: "codex:work", Windows: map[model.Window]model.WindowStatus{
			model.WindowMonthly: {Known: true, UsedPercent: 12},
		}},
	}}

	cells := displayCells(cfg, snap)
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
	snap := &model.Snapshot{Sources: []model.SourceStatus{
		{ID: model.SourceCodex, Windows: map[model.Window]model.WindowStatus{
			model.WindowWeekly: {Known: true, UsedPercent: 6},
		}},
	}}

	cells := displayCells(cfg, snap)
	if len(cells) != 1 || cells[0].Service != model.SourceCodex || cells[0].Period != "WK" {
		t.Fatalf("only installed/present Codex should be drawn: %+v", cells)
	}
	menu := buildMenu(cfg, snap, time.Now())
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
	snap := &model.Snapshot{Sources: []model.SourceStatus{
		{ID: model.SourceClaudeCode, Windows: map[model.Window]model.WindowStatus{
			model.Window5h: {Known: true}, model.WindowWeekly: {Known: true},
		}},
		{ID: model.SourceCodex, Windows: map[model.Window]model.WindowStatus{
			model.Window5h: {Known: true}, model.WindowMonthly: {Known: true},
		}},
	}}
	cells := displayCells(cfg, snap)
	if len(cells) != 2 || cells[0].Service != model.SourceClaudeCode || cells[0].Period != "WK" ||
		cells[1].Service != model.SourceCodex || cells[1].Period != "MO" {
		t.Fatalf("independent cells = %+v", cells)
	}
}

// stubBackend records what the tray was told to do.
type stubBackend struct {
	mu     sync.Mutex
	quits  int
	layout tray.Layout
}

func (b *stubBackend) Layout() tray.Layout            { return b.layout }
func (b *stubBackend) Appearance() tray.Appearance    { return tray.AppearanceLight }
func (b *stubBackend) Run(func(), func(string)) error { return nil }
func (b *stubBackend) SetIcon(tray.IconData)          {}
func (b *stubBackend) SetTooltip(string)              {}
func (b *stubBackend) SetMenu([]tray.Item)            {}
func (b *stubBackend) Quit() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.quits++
}

func (b *stubBackend) quitCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.quits
}

// slowProvider stands in for a `claude -p /usage` or `codex app-server` call
// that has gone unresponsive and is sitting out its timeout.
type slowProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (s *slowProvider) ID() string { return model.SourceClaudeCode }

func (s *slowProvider) Collect(now time.Time) model.SourceStatus {
	close(s.entered)
	<-s.release
	return model.SourceStatus{ID: model.SourceClaudeCode, Name: "Claude Code"}
}

// Collection can block for tens of seconds. The menu must stay responsive
// while it does: a Quit that waits for a hung subprocess reads as a hang.
func TestMenuRespondsWhileARefreshIsStuck(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	slow := &slowProvider{entered: make(chan struct{}), release: make(chan struct{})}
	backend := &stubBackend{}
	a := &app{
		cfg:       cfg,
		backend:   backend,
		providers: []provider.Provider{slow},
		refresh:   make(chan struct{}, 1),
	}

	done := make(chan struct{})
	go func() {
		a.update()
		close(done)
	}()
	<-slow.entered

	clicked := make(chan struct{})
	go func() {
		a.onClick(idQuit)
		a.onClick(idModePfx + string(config.ModePercent))
		close(clicked)
	}()
	select {
	case <-clicked:
	case <-time.After(5 * time.Second):
		t.Fatal("menu clicks blocked behind an in-flight refresh")
	}

	if backend.quitCount() != 1 {
		t.Errorf("quit count = %d, want 1", backend.quitCount())
	}
	if got := cfg.Mode(); got != config.ModePercent {
		t.Errorf("display mode = %q, want the clicked one", got)
	}
	close(slow.release)
	<-done
}

// A settings click and a config reload must not interleave. Reading the config
// pointer and mutating it as two separate steps lets a reload land in between,
// after which the click writes — and saves — the config the app has already
// thrown away, silently reverting the hand edit that triggered the reload.
func TestClickCannotInterleaveWithAConfigReload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	stale, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: stale, backend: &stubBackend{}, refresh: make(chan struct{}, 1)}

	// Hold the lock the way update() does while it swaps the config in.
	a.mu.Lock()
	clicked := make(chan struct{})
	go func() {
		a.onClick(idModePfx + string(config.ModeBattery))
		close(clicked)
	}()
	select {
	case <-clicked:
		a.mu.Unlock()
		t.Fatal("the click mutated a config the app was in the middle of replacing")
	case <-time.After(50 * time.Millisecond):
	}
	a.cfg = reloaded
	a.mu.Unlock()

	<-clicked
	if got := reloaded.Mode(); got != config.ModeBattery {
		t.Fatalf("reloaded config mode = %q, want the clicked one", got)
	}
	if got := stale.Mode(); got == config.ModeBattery {
		t.Fatal("the click landed on the discarded config")
	}
}
