package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/i18n"
	"github.com/yutat23/usagebat/internal/model"
	"github.com/yutat23/usagebat/internal/provider"
	"github.com/yutat23/usagebat/internal/render"
	"github.com/yutat23/usagebat/internal/tray"
	"github.com/yutat23/usagebat/internal/version"
	"github.com/yutat23/usagebat/internal/webui"
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
	menu := buildMenu(cfg, snap, time.Now(), i18n.New("en"))
	for _, item := range menu {
		if item.Title == "Claude Code" || item.Title == "Claude Code limits" {
			t.Fatalf("unavailable Claude should not be offered in menu: %+v", item)
		}
	}
}

func TestJapaneseMenuIncludesBankedResetAndLanguage(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.Local)
	cfg := config.Default()
	cfg.Language = "ja"
	snap := &model.Snapshot{Sources: []model.SourceStatus{{
		ID: model.SourceCodex, Name: "Codex", Windows: map[model.Window]model.WindowStatus{},
		RateLimitResets: &model.RateLimitResetCredits{AvailableCount: 1, Credits: []model.RateLimitResetCredit{{
			Status: "available", ExpiresAt: now.Add(10 * 24 * time.Hour),
		}}},
	}}}
	menu := buildMenu(cfg, snap, now, i18n.New("ja"))
	wants := []string{"Banked reset  1回利用可能 · あと10日に期限切れ", "banked resetの期限切れ通知", "今すぐ更新", "日本語"}
	for _, want := range wants {
		found := false
		for _, item := range menu {
			found = found || item.Title == want
		}
		if !found {
			t.Errorf("menu missing %q", want)
		}
	}
}

// The about section is the only place the running version is visible, so a
// bug report can name it without the reporter digging for a terminal.
func TestMenuShowsVersionAndHomepage(t *testing.T) {
	menu := buildMenu(config.Default(), &model.Snapshot{}, time.Now(), i18n.New("en"))

	var versionRow, homepage *tray.Item
	for i, item := range menu {
		switch {
		case strings.HasPrefix(item.Title, "usagebat "):
			versionRow = &menu[i]
		case item.ID == idHomepage:
			homepage = &menu[i]
		}
	}
	if versionRow == nil {
		t.Fatalf("menu has no version row: %+v", menu)
	}
	if !versionRow.Disabled {
		t.Error("the version row is a heading, not something to click")
	}
	if versionRow.Title != "usagebat "+version.String() {
		t.Errorf("version row = %q, want the running version", versionRow.Title)
	}
	if homepage == nil {
		t.Fatalf("menu has no homepage item: %+v", menu)
	}
	if homepage.Tooltip != homepageURL || homepage.Disabled {
		t.Errorf("homepage item = %+v, want a clickable link to %s", *homepage, homepageURL)
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
	if !shouldDetach(false, false, true) {
		t.Fatal("an interactive default run should detach")
	}
	for _, tc := range []struct {
		name        string
		foreground  bool
		oneShot     bool
		interactive bool
	}{
		{name: "foreground", foreground: true, interactive: true},
		{name: "one-shot (-dump/-notify-test)", oneShot: true, interactive: true},
		{name: "already detached"},
	} {
		if shouldDetach(tc.foreground, tc.oneShot, tc.interactive) {
			t.Fatalf("unexpected detach for %s", tc.name)
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

// twoCodexProfiles is a machine signed in to two Codex accounts.
func twoCodexProfiles() *model.Snapshot {
	return &model.Snapshot{Sources: []model.SourceStatus{
		{ID: "codex:work-1a2b", Name: "codex-w", Short: "CW", Windows: map[model.Window]model.WindowStatus{
			model.WindowMonthly: {Known: true, UsedPercent: 30},
		}},
		{ID: "codex:home-3c4d", Name: "codex-p", Short: "CP", Windows: map[model.Window]model.WindowStatus{
			model.WindowMonthly: {Known: true, UsedPercent: 80},
		}},
	}}
}

func twoClaudeProfiles() *model.Snapshot {
	return &model.Snapshot{Sources: []model.SourceStatus{
		{ID: "claude-code:work-1a2b", Name: "Claude work", Short: "CW", Windows: map[model.Window]model.WindowStatus{
			model.Window5h: {Known: true, UsedPercent: 22},
		}},
		{ID: "claude-code:personal-3c4d", Name: "Claude personal", Short: "CP", Windows: map[model.Window]model.WindowStatus{
			model.Window5h: {Known: true, UsedPercent: 67},
		}},
	}}
}

func TestClaudeProfilesGetSeparateCellsAndMenuEntries(t *testing.T) {
	cfg := config.Default()
	cfg.DisplaySources = []string{model.SourceClaudeCode}
	cfg.DisplayLimits[model.SourceClaudeCode] = config.LimitDisplay{Windows: []string{"5h"}}
	cells := displayCells(cfg, twoClaudeProfiles())
	if len(cells) != 2 || cells[0].Profile != "CW" || cells[1].Profile != "CP" {
		t.Fatalf("Claude cells = %+v, want separate CW and CP cells", cells)
	}
	menu := buildMenu(cfg, twoClaudeProfiles(), time.Now(), i18n.New("en"))
	found := map[string]bool{}
	for _, item := range menu {
		if item.Title == "Claude work" || item.Title == "Claude personal" {
			found[item.Title] = true
		}
	}
	if !found["Claude work"] || !found["Claude personal"] {
		t.Fatalf("Claude menu headings = %v", found)
	}
}

// Selecting Codex expands configured accounts so their CW/CP abbreviations do
// not disappear into one generic CX cell.
func TestServiceSelectionShowsEachConfiguredProfile(t *testing.T) {
	cfg := config.Default()
	cfg.DisplaySources = []string{model.SourceCodex}
	cfg.DisplayLimits[model.SourceCodex] = config.LimitDisplay{Windows: []string{"monthly"}}

	cells := displayCells(cfg, twoCodexProfiles())
	if len(cells) != 2 {
		t.Fatalf("got %d cells, want one for each configured account", len(cells))
	}
	if cells[0].Profile != "CW" || cells[1].Profile != "CP" {
		t.Fatalf("profiles = %q, %q; want CW and CP", cells[0].Profile, cells[1].Profile)
	}
	if cells[0].Status.UsedPercent != 30 || cells[1].Status.UsedPercent != 80 {
		t.Errorf("account readings were combined: %+v", cells)
	}
}

func TestMenuListsEachConfiguredCodexProfile(t *testing.T) {
	menu := buildMenu(config.Default(), twoCodexProfiles(), time.Now(), i18n.New("en"))
	found := map[string]bool{}
	for _, item := range menu {
		if item.Title == "codex-w" || item.Title == "codex-p" {
			found[item.Title] = true
		}
	}
	if !found["codex-w"] || !found["codex-p"] {
		t.Fatalf("menu profile headings = %v, want codex-w and codex-p", found)
	}
}

func TestSingleUnnamedCodexProfileKeepsCompactServiceCell(t *testing.T) {
	cfg := config.Default()
	cfg.DisplaySources = []string{model.SourceCodex}
	snap := &model.Snapshot{Sources: []model.SourceStatus{{
		ID: "codex:default-1a2b", Name: "Codex",
		Windows: map[model.Window]model.WindowStatus{model.Window5h: {Known: true}},
	}}}
	cells := displayCells(cfg, snap)
	if len(cells) != 1 || cells[0].Profile != "" {
		t.Fatalf("default Codex cell = %+v, want one compact CX cell", cells)
	}
}

// Naming the accounts draws them separately, which is the point of tracking
// more than one.
func TestProfileSelectionDrawsEachAccount(t *testing.T) {
	cfg := config.Default()
	cfg.DisplaySources = []string{"codex:work-1a2b", "codex:home-3c4d"}
	cfg.DisplayLimits[model.SourceCodex] = config.LimitDisplay{Windows: []string{"monthly"}}

	cells := displayCells(cfg, twoCodexProfiles())
	if len(cells) != 2 {
		t.Fatalf("got %d cells, want one per named account", len(cells))
	}
	for _, cell := range cells {
		if cell.Service != model.SourceCodex {
			t.Errorf("cell service = %q, want the family so the colour still applies", cell.Service)
		}
		if cell.Profile == "" {
			t.Errorf("cell for %q has nothing to tell it apart", cell.Period)
		}
	}
	if cells[0].Profile == cells[1].Profile {
		t.Errorf("both accounts abbreviate to %q", cells[0].Profile)
	}
}

// A sixteen-dot square cannot hold four bars and still look like bars.
func TestWindowsIconCapsTheNumberOfBars(t *testing.T) {
	var cells []render.Cell
	for _, remaining := range []float64{90, 10, 70, 40, 55} {
		cells = append(cells, render.Cell{
			Service: model.SourceCodex,
			Status:  model.WindowStatus{Known: true, UsedPercent: 100 - remaining},
		})
	}

	got := capSquareCells(cells)
	if len(got) != squareCellLimit {
		t.Fatalf("got %d bars, want at most %d", len(got), squareCellLimit)
	}
	// The ones kept are the ones worth watching.
	if r := got[0].Status.RemainingPercent(); r != 10 {
		t.Errorf("first kept cell has %v%% left, want the most constrained 10%%", r)
	}
	if r := got[len(got)-1].Status.RemainingPercent(); r > 55 {
		t.Errorf("kept a cell with %v%% left over one with less", r)
	}
	// Under the limit nothing is dropped or reordered.
	few := cells[:2]
	if kept := capSquareCells(few); len(kept) != 2 || kept[0] != few[0] {
		t.Errorf("a short list was disturbed: %+v", kept)
	}
}

// countingProvider records whether it was asked for a live reading.
type countingProvider struct {
	mu       sync.Mutex
	collects int
	live     int
}

func (p *countingProvider) ID() string { return model.SourceClaudeCode }

func (p *countingProvider) Collect(time.Time) model.SourceStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.collects++
	return model.SourceStatus{ID: model.SourceClaudeCode, Name: "Claude Code"}
}

func (p *countingProvider) RequestAuthoritative() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.live++
}

func (p *countingProvider) counts() (collects, live int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.collects, p.live
}

// Refresh exists because somebody doubts the number on screen, so it has to go
// past whatever each provider has cached. The scheduled refreshes stay cheap.
func TestRefreshAsksProvidersForLiveFigures(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	live := &countingProvider{}
	a := &app{
		cfg:       cfg,
		backend:   &stubBackend{},
		providers: []provider.Provider{live},
		settings:  &webui.Server{},
		refresh:   make(chan struct{}, 1),
	}

	a.update()
	if _, asked := live.counts(); asked != 0 {
		t.Fatalf("a scheduled refresh asked for live figures %d times", asked)
	}

	a.onClick(idRefresh)
	a.update()
	collects, asked := live.counts()
	if asked != 1 {
		t.Fatalf("asked for live figures %d times after refresh, want 1", asked)
	}
	if collects != 2 {
		t.Fatalf("collected %d times, want one per update", collects)
	}

	// The request is spent by the collection that used it.
	a.update()
	if _, asked := live.counts(); asked != 1 {
		t.Fatalf("asked %d times; the request outlived its refresh", asked)
	}
}

// Waking a laptop is the one moment everything on screen is certainly stale,
// and also the moment somebody looks at it.
func TestSleepGapAsksForLiveFigures(t *testing.T) {
	const interval = time.Minute
	a := &app{}

	now := time.Now()
	a.noteSleep(now, interval)
	if a.authoritative.Load() {
		t.Fatal("the first collection has no previous one to compare against")
	}

	a.noteSleep(now.Add(interval+time.Second), interval)
	if a.authoritative.Load() {
		t.Fatal("a refresh running slightly late is not a suspended machine")
	}

	a.noteSleep(now.Add(2*time.Hour), interval)
	if !a.authoritative.Load() {
		t.Fatal("a two-hour gap should be treated as a sleep and fetch live figures")
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
func (b *stubBackend) Notify(tray.Notification) error { return nil }
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
