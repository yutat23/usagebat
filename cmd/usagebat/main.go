// Command usagebat shows Claude Code and Codex limit headroom as a
// pixel-art battery in the macOS menu bar or the Windows tray.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yutat23/usagebat/internal/appbundle"
	"github.com/yutat23/usagebat/internal/autostart"
	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/history"
	"github.com/yutat23/usagebat/internal/i18n"
	"github.com/yutat23/usagebat/internal/model"
	notifyengine "github.com/yutat23/usagebat/internal/notify"
	"github.com/yutat23/usagebat/internal/provider"
	"github.com/yutat23/usagebat/internal/provider/claudecode"
	"github.com/yutat23/usagebat/internal/provider/codex"
	"github.com/yutat23/usagebat/internal/render"
	"github.com/yutat23/usagebat/internal/tray"
	"github.com/yutat23/usagebat/internal/update"
	"github.com/yutat23/usagebat/internal/version"
	"github.com/yutat23/usagebat/internal/webui"
)

func init() {
	// AppKit requires its run loop on the process's main thread.
	runtime.LockOSThread()
}

type app struct {
	backend tray.Backend

	// mu guards everything derived from the config, which the update loop may
	// replace when the file changes and click handlers may mutate. It is only
	// ever held for bookkeeping: collection runs outside it, because a provider
	// can sit in a subprocess for tens of seconds and menu clicks must not queue
	// up behind it.
	mu        sync.Mutex
	cfg       *config.Config
	providers []provider.Provider
	notifier  *notifyengine.Engine
	// notificationsUnavailable suppresses repeated attempts during a standalone
	// macOS launch. Such binaries have no application bundle identity, and
	// UserNotifications raises an Objective-C exception if called directly.
	notificationsUnavailable bool

	// settings serves the browser settings screen. It only listens while the
	// screen is in use.
	settings *webui.Server
	// recorder accumulates the samples the usage charts are drawn from, and
	// updates is the optional GitHub release check. Both are self-synchronised
	// and nil in tests.
	recorder *history.Recorder
	updates  *update.Checker
	latest   atomic.Pointer[update.Release]

	// authoritative asks the next collection to fetch live figures rather than
	// whatever each provider has cached. Click handlers run on their own
	// goroutines and must not touch providers, so they raise this instead and
	// the update loop acts on it.
	authoritative atomic.Bool
	// lastUpdate is when the previous collection ran, in wall-clock terms. It
	// is only ever touched on the update goroutine.
	lastUpdate time.Time

	refresh chan struct{}
	snap    atomic.Pointer[model.Snapshot]
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("usagebat: ")

	dump := flag.String("dump", "",
		"collect once, print the menu to stdout, write the icon to this path, and exit")
	notifyTest := flag.Bool("notify-test", false,
		"send one notification through the real path, print diagnostics, and exit")
	foreground := flag.Bool("foreground", false, "run in the foreground and keep the terminal attached")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: usagebat [options]\n       usagebat install-app\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	// Every mode below this point either prints and exits or hands the terminal
	// back; only they need a console, and only they may have to go find one.
	oneShot := *dump != "" || *notifyTest || *showVersion
	if oneShot {
		attachParentConsole()
	}
	if *showVersion {
		fmt.Println("usagebat " + version.String())
		return
	}
	if flag.NArg() > 0 {
		if flag.NArg() != 1 || flag.Arg(0) != "install-app" {
			flag.Usage()
			os.Exit(2)
		}
		path, err := appbundle.Install(version.String())
		if err != nil {
			log.Fatal(err)
		}
		if err := appbundle.Launch(path); err != nil {
			log.Fatal(err)
		}
		fmt.Println("usagebat installed and launched: " + path)
		return
	}
	if shouldDetach(*foreground, oneShot, interactiveTerminal()) {
		if err := launchDetached(); err != nil {
			log.Fatal(err)
		}
		fmt.Println("usagebat started in the background")
		return
	}
	cfg, err := config.Load()
	if err != nil {
		// A broken config must not keep the app off the menu bar; defaults apply.
		log.Printf("config: %v (using defaults)", err)
	}

	a := &app{
		cfg:      cfg,
		backend:  tray.New(),
		notifier: notifyengine.New(),
		settings: &webui.Server{},
		recorder: newRecorder(cfg),
		updates:  update.New(),
		refresh:  make(chan struct{}, 1),
	}
	// Wired after construction: both callbacks need the app itself.
	a.settings.Render = a.settingsPage
	a.settings.Activate = a.applySetting
	a.rebuild()

	if *dump != "" {
		if err := a.dumpOnce(*dump); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *notifyTest {
		if err := a.notifyOnce(); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := a.backend.Run(a.onReady, a.onClick); err != nil {
		log.Fatal(err)
	}
}

// shouldDetach is false for one-shot runs: those print to the terminal the user
// is standing in front of, so detaching would throw their output away.
func shouldDetach(foreground, oneShot, interactive bool) bool {
	return !foreground && !oneShot && interactive
}

// dumpOnce runs one collection without starting the tray. It exists so the
// data path can be inspected on a machine where the menu bar cannot be
// screenshotted, and so users can see what the app is reading.
func (a *app) dumpOnce(path string) error {
	now := time.Now()
	snap := &model.Snapshot{UpdatedAt: now}
	for _, p := range a.providers {
		snap.Sources = append(snap.Sources, p.Collect(now))
	}
	snap.AggregateSources(model.AllWindows, a.cfg.EnabledDisplaySources())

	p := i18n.New(a.cfg.LanguageSetting())
	for _, it := range buildMenu(a.cfg, snap, now, p) {
		if it.Separator {
			fmt.Println(strings.Repeat("-", 60))
			continue
		}
		mark := "  "
		if it.Checkable && it.Checked {
			mark = "* "
		}
		fmt.Printf("%s%s%s\n", strings.Repeat("    ", it.Indent), mark, it.Title)
	}

	icon := a.renderIcon(a.cfg, snap)
	if len(icon.Bytes) == 0 {
		return fmt.Errorf("icon render produced no bytes")
	}
	fmt.Printf("\nicon: %d bytes, logical %.0fx%.0f pt -> %s\n",
		len(icon.Bytes), icon.WidthPt, icon.HeightPt, path)
	return os.WriteFile(path, icon.Bytes, 0o644)
}

// notifyOnce sends one notification through the same backend and the same
// wording the banked-reset alert uses, without starting the tray. The real
// alert only fires when a credit is genuinely near expiry and then deduplicates
// itself, so there is otherwise no way to look at one on demand.
func (a *app) notifyOnce() error {
	now := time.Now()
	p := i18n.New(a.cfg.LanguageSetting())
	title, body := p.ResetNotification(1, now.Add(6*time.Hour), now)
	fmt.Printf("title: %s\nbody:  %s\n", title, body)

	err := a.backend.Notify(tray.Notification{Title: title, Body: body})
	// Print the platform state either way: when nothing appears despite a
	// successful send, the registration is where the answer is.
	for _, line := range tray.NotificationDiagnostics() {
		fmt.Println(line)
	}
	if err != nil {
		return err
	}
	// macOS hands the request to another queue and returns immediately, so an
	// exit here would cancel the delivery before it happens.
	time.Sleep(3 * time.Second)
	fmt.Println("sent")
	return nil
}

// rebuild recreates everything derived from the config.
func (a *app) rebuild() {
	a.providers = nil
	if a.cfg.Sources.ClaudeCode.Enabled && claudecode.Available(&a.cfg.Sources.ClaudeCode) {
		for _, p := range claudecode.Providers(&a.cfg.Sources.ClaudeCode) {
			a.providers = append(a.providers, p)
		}
	}
	if a.cfg.Sources.Codex.Enabled && codex.Available(&a.cfg.Sources.Codex) {
		// One provider per Codex home: separate homes are separate accounts.
		for _, p := range codex.Providers(&a.cfg.Sources.Codex) {
			a.providers = append(a.providers, p)
		}
	}
}

// currentConfig returns the live config pointer. Its own methods are
// self-synchronised, so callers outside the update loop use this rather than
// holding the app lock across file I/O.
func (a *app) currentConfig() *config.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

// mutateConfig applies a change to the live config under the app lock, so a
// concurrent reload cannot swap the config out from under the click handler and
// have it persist the pre-reload contents.
func (a *app) mutateConfig(change func(*config.Config) error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := change(a.cfg); err != nil {
		log.Printf("save config: %v", err)
	}
}

func (a *app) onReady() {
	ticker := time.NewTicker(a.refreshInterval())
	defer ticker.Stop()
	for {
		a.update()
		// Reset after the update, so the interval is measured from the end of a
		// refresh and a manual one pushes the next scheduled one back.
		ticker.Reset(a.refreshInterval())
		select {
		case <-ticker.C: // a tick that arrived during the update
		default:
		}
		select {
		case <-ticker.C:
		case <-a.refresh:
		}
	}
}

func (a *app) refreshInterval() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return time.Duration(a.cfg.RefreshSeconds) * time.Second
}

// requestRefresh nudges the update loop without blocking the caller. Click
// handlers run on their own goroutines and must not touch providers directly,
// because providers keep unsynchronised incremental-read state.
func (a *app) requestRefresh() {
	select {
	case a.refresh <- struct{}{}:
	default:
	}
}

// update collects from every provider and pushes fresh artwork and menu.
func (a *app) update() {
	// Providers keep unsynchronised incremental-read state and are only ever
	// touched here, so taking them under the lock and collecting outside it is
	// safe — and it keeps the lock off the subprocesses providers may run.
	a.mu.Lock()
	a.reloadConfigIfChanged()
	cfg, providers := a.cfg, a.providers
	a.mu.Unlock()

	a.noteSleep(time.Now(), time.Duration(cfg.RefreshSeconds)*time.Second)

	// Consumed here rather than in the handler: providers keep unsynchronised
	// state and are only ever touched on this goroutine.
	if a.authoritative.Swap(false) {
		for _, p := range providers {
			if live, ok := p.(provider.Authoritative); ok {
				live.RequestAuthoritative()
			}
		}
	}

	now := time.Now()
	snap := &model.Snapshot{UpdatedAt: now}
	for _, p := range providers {
		snap.Sources = append(snap.Sources, p.Collect(now))
	}
	snap.AggregateSources(model.AllWindows, cfg.EnabledDisplaySources())
	a.snap.Store(snap)
	p := i18n.New(cfg.LanguageSetting())

	a.recordHistory(cfg, snap, now)
	a.startUpdateCheck(cfg, now)

	a.backend.SetIcon(a.renderIcon(cfg, snap))
	a.backend.SetTooltip(tooltip(snap, now, p))
	a.backend.SetMenu(buildMenu(cfg, snap, now, p))
	if a.notifier == nil || a.notificationsUnavailable {
		return
	}
	events := a.notifier.Due(snap, cfg.BankedResetNotifications(), p, now)
	events = append(events, a.notifier.DueLimits(
		watchedGauges(cfg, snap, p), cfg.LimitThresholdSettings(), p, now)...)
	a.deliver(events, now)
}

// deliver sends the notifications and records the ones that arrived. An event
// is only marked as sent once the platform accepted it, so a launch from a
// package that cannot notify does not silently consume the alert.
func (a *app) deliver(events []notifyengine.Event, now time.Time) {
	for _, event := range events {
		if err := a.backend.Notify(tray.Notification{Title: event.Title, Body: event.Body}); err != nil {
			log.Printf("notification: %v", err)
			if errors.Is(err, tray.ErrNotificationsUnavailable) {
				a.notificationsUnavailable = true
				return
			}
			continue
		}
		if err := a.notifier.Mark(event, now); err != nil {
			log.Printf("notification state: %v", err)
		}
	}
}

// watchedGauges are the limits the icon is drawing, which is how the user has
// already said which ones they care about.
func watchedGauges(cfg *config.Config, snap *model.Snapshot, p i18n.Printer) []notifyengine.Gauge {
	var out []notifyengine.Gauge
	for _, cell := range displayCells(cfg, snap) {
		name := cell.Service
		switch name {
		case model.SourceClaudeCode:
			name = "Claude Code"
		case model.SourceCodex:
			name = "Codex"
		}
		out = append(out, notifyengine.Gauge{
			Source: cell.Service, Name: name,
			Window: cell.Status.Window, Status: cell.Status,
		})
	}
	return out
}

// sleepGap is how far past due a refresh has to be before the machine is
// assumed to have been asleep rather than merely busy.
const sleepGap = 2 * time.Minute

// noteSleep spots the gap a suspended machine leaves in the refresh loop.
//
// Waking up to figures from before the lid closed is the one time everything
// on screen is certainly wrong, and it is also the moment somebody looks at
// it. There is no platform code here: a timer that was itself suspended
// cannot report that it was, but the wall clock says so plainly.
func (a *app) noteSleep(now time.Time, interval time.Duration) {
	previous := a.lastUpdate
	a.lastUpdate = now
	if previous.IsZero() || interval <= 0 {
		return
	}
	if now.Sub(previous) > interval+sleepGap {
		a.authoritative.Store(true)
	}
}

// newRecorder builds the history recorder from the config's sampling settings.
// They are read once: changing them is rare, and a resident recorder that
// re-reads them every refresh would have to re-open its file each time.
func newRecorder(cfg *config.Config) *history.Recorder {
	settings := cfg.HistorySettings()
	return history.NewAt(history.DefaultPath(), history.Options{
		Interval:  time.Duration(settings.IntervalMinutes) * time.Minute,
		Retention: time.Duration(settings.RetentionDays) * 24 * time.Hour,
	})
}

// recordHistory samples the snapshot for the usage charts. A failure here must
// never interrupt a refresh: the tray is the product, the charts are extra.
func (a *app) recordHistory(cfg *config.Config, snap *model.Snapshot, now time.Time) {
	if a.recorder == nil || !cfg.HistorySettings().Enabled {
		return
	}
	if _, err := a.recorder.Observe(snap, now); err != nil {
		log.Printf("history: %v", err)
	}
}

// startUpdateCheck runs the check on its own goroutine. The refresh loop is
// already slow enough with providers in it, and a network request that hangs
// until its timeout must not hold the icon and the menu back.
func (a *app) startUpdateCheck(cfg *config.Config, now time.Time) {
	settings := cfg.UpdateCheckSettings()
	if a.updates == nil || !settings.Enabled {
		return
	}
	if !a.updates.Begin(now, time.Duration(settings.IntervalHours)*time.Hour) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		release, err := a.updates.Check(ctx, version.String())
		if err != nil {
			// Being offline is ordinary. The next check is a whole interval
			// away, so this cannot fill the log.
			log.Printf("update check: %v", err)
			return
		}
		if release == nil {
			return
		}
		a.latest.Store(release)
		a.requestRefresh()
	}()
}

// reloadConfigIfChanged picks up edits made in the config file, so calibrating
// the Claude Code limits does not need a restart. Saves the app made itself are
// not edits: the config stamps those as it writes them.
func (a *app) reloadConfigIfChanged() {
	if !a.cfg.ExternallyModified() {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		// Keep the config we already have rather than dropping the user's
		// settings over a typo, but accept the file so the next refresh does not
		// report the same broken edit again.
		log.Printf("config reload: %v", err)
		a.cfg.MarkSeen()
		return
	}
	a.cfg = cfg
	a.rebuild()
}

func (a *app) renderIcon(cfg *config.Config, snap *model.Snapshot) tray.IconData {
	cells := displayCells(cfg, snap)
	dark := a.backend.Appearance() == tray.AppearanceDark
	geometry := cfg.IconGeometry()
	opts := render.Options{
		Mode:    cfg.Mode(),
		Palette: render.PaletteFrom(cfg.Palette(), dark),
		Scale:   geometry.PixelScale,
	}

	if a.backend.Layout() == tray.LayoutStrip {
		icon := render.StripCells(cells, opts)
		data, err := render.PNG(icon.Image)
		if err != nil {
			log.Printf("render: %v", err)
			return tray.IconData{}
		}
		return tray.IconData{
			Bytes:    data,
			WidthPt:  float64(icon.DotsW) * geometry.DotSize,
			HeightPt: float64(icon.DotsH) * geometry.DotSize,
		}
	}

	square := capSquareCells(cells)
	data, err := render.ICO(func(size int) *image.RGBA {
		o := opts
		o.Scale = 1
		base := render.SquareCells(square, o, geometry.WindowsLayout).Image
		return render.ResizeNearest(base, size)
	})
	if err != nil {
		log.Printf("render: %v", err)
		return tray.IconData{}
	}
	return tray.IconData{Bytes: data}
}

// displayCells keeps services separate. In automatic mode each service gets
// its own shortest real limit, so a Claude 5h limit and a Codex monthly limit
// can coexist in the same icon.
func displayCells(cfg *config.Config, snap *model.Snapshot) []render.Cell {
	var cells []render.Cell
	for _, selected := range cfg.EnabledDisplaySources() {
		if !snapshotHasFamily(snap, selected) {
			continue
		}
		for _, target := range displayTargets(snap, selected) {
			// A target can name a whole service or one account of it. Naming an
			// account draws it on its own with the configured abbreviation.
			family, profile := target, ""
			if source, ok := findSource(snap, target); ok {
				family = sourceFamily(target)
				profile = profileAbbreviation(source)
			}
			aggregated := &model.Snapshot{Sources: snap.Sources}
			aggregated.AggregateSources(model.AllWindows, []string{target})
			auto, windows := cfg.LimitSelection(family)
			if auto {
				windows = nil
				for _, w := range model.AllWindows {
					if st := aggregated.Icon[w]; st.Known {
						windows = []model.Window{w}
						break
					}
				}
			}
			if len(windows) == 0 {
				windows = []model.Window{model.Window5h}
			}
			for _, w := range windows {
				st := aggregated.Icon[w]
				st.Window = w
				cells = append(cells, render.Cell{
					Service: family, Period: w.Label(), Profile: profile, Status: st,
				})
			}
		}
	}
	return cells
}

// displayTargets expands a family selection when its accounts need to remain
// distinguishable. A single unnamed default profile keeps the compact CX
// label; configured abbreviations and multiple accounts are shown separately.
func displayTargets(snap *model.Snapshot, selected string) []string {
	if _, exact := findSource(snap, selected); exact {
		return []string{selected}
	}
	var matches []model.SourceStatus
	for _, source := range snap.Sources {
		if sourceFamily(source.ID) == selected {
			matches = append(matches, source)
		}
	}
	if len(matches) == 1 && matches[0].Short == "" {
		return []string{selected}
	}
	if len(matches) == 0 {
		return []string{selected}
	}
	targets := make([]string, 0, len(matches))
	for _, source := range matches {
		targets = append(targets, source.ID)
	}
	return targets
}

// squareCellLimit is how many bars fit in a Windows tray icon and still read
// as bars. The grid is sixteen dots square, so a fourth bar leaves three dots
// each and the outlines disappear.
const squareCellLimit = 3

// capSquareCells keeps the Windows icon legible. Past the limit it shows the
// most constrained cells rather than shrinking every bar into a stripe:
// somebody watching four accounts is watching for the one in trouble.
func capSquareCells(cells []render.Cell) []render.Cell {
	if len(cells) <= squareCellLimit {
		return cells
	}
	ranked := append([]render.Cell(nil), cells...)
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Status.RemainingPercent() < ranked[j].Status.RemainingPercent()
	})
	return ranked[:squareCellLimit]
}

// findSource looks for a selection that names one account exactly.
func findSource(snap *model.Snapshot, id string) (model.SourceStatus, bool) {
	for _, src := range snap.Sources {
		if src.ID == id {
			return src, true
		}
	}
	return model.SourceStatus{}, false
}

// sourceFamily strips the account off a source id: "codex:work-1a2b" is Codex.
func sourceFamily(id string) string {
	if family, _, found := strings.Cut(id, ":"); found {
		return family
	}
	return id
}

// profileAbbreviation is the one or two characters the icon has room for.
//
// A configured short name wins. Failing that the account's own name is
// trimmed down, which is the part of a path that distinguishes it:
// "Codex (~/.codex-work)" abbreviates to WO, not to CO, because CO is what
// every Codex directory would give.
func profileAbbreviation(src model.SourceStatus) string {
	if src.Short != "" {
		return firstRunes(src.Short, 2)
	}
	name := src.Name
	if open := strings.IndexRune(name, '('); open >= 0 {
		if close := strings.IndexRune(name[open:], ')'); close > 0 {
			name = name[open+1 : open+close]
		}
	}
	name = filepath.Base(name)
	name = strings.TrimPrefix(name, ".")
	// Directories are conventionally named for the tool and then the account,
	// so whatever follows the first dash is the distinguishing part.
	if dash := strings.IndexRune(name, '-'); dash >= 0 && dash+1 < len(name) {
		name = name[dash+1:]
	}
	return firstRunes(name, 2)
}

func firstRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		runes = runes[:n]
	}
	return string(runes)
}

func snapshotHasFamily(snap *model.Snapshot, family string) bool {
	for _, src := range snap.Sources {
		if src.ID == family || strings.HasPrefix(src.ID, family+":") {
			return true
		}
	}
	return false
}

// Menu item identifiers.
const (
	idRefresh       = "refresh"
	idConfig        = "config"
	idAutostart     = "autostart"
	idQuit          = "quit"
	idModePfx       = "mode:"
	idSourcePfx     = "source:"
	idLimitPfx      = "limit:"
	idLanguagePfx   = "language:"
	idNotifications = "notifications:banked-reset"
	idHomepage      = "homepage"
	idSettings      = "settings"
	idHistory       = "history"
	idUpdateCheck   = "update-check"
	idLimitAlerts   = "limit-alerts"
)

// homepageURL is where the about section sends anyone looking for the source,
// the README, or somewhere to report a problem.
const homepageURL = "https://github.com/yutat23/usagebat"

func buildMenu(cfg *config.Config, snap *model.Snapshot, now time.Time, p i18n.Printer) []tray.Item {
	var items []tray.Item

	for _, src := range snap.Sources {
		name := src.Name
		if src.Note != "" {
			name += "  —  " + p.TranslateNote(src.Note)
		}
		items = append(items, tray.Item{Title: name, Disabled: true})

		if src.Err != "" {
			items = append(items, tray.Item{Title: "⚠ " + src.Err, Disabled: true, Indent: 1})
		}
		// The detail menu shows every limit the provider actually has. The
		// window checkboxes below control only what is drawn in the icon.
		for _, w := range model.AllWindows {
			st, ok := src.Windows[w]
			if !ok {
				continue
			}
			items = append(items, tray.Item{Title: windowLine(w, st, now, p), Disabled: true, Indent: 1})
			if tok, ok := src.Tokens[w]; ok && tok.Total() > 0 {
				items = append(items, tray.Item{
					Title:    tokenLine(tok, src.TokensNote, p),
					Disabled: true,
					Indent:   2,
				})
			}
		}
		if src.RateLimitResets != nil {
			expires := earliestResetExpiry(src.RateLimitResets.Credits, now)
			items = append(items, tray.Item{
				Title:    p.BankedResets(src.RateLimitResets.AvailableCount, expires, now),
				Disabled: true, Indent: 1,
			})
		}
		items = append(items, tray.Item{Separator: true})
	}

	items = append(items, tray.Item{Title: p.T("iconStyle"), Disabled: true})
	mode := cfg.Mode()
	for _, m := range []config.DisplayMode{config.ModeBoth, config.ModeBattery, config.ModePercent} {
		items = append(items, tray.Item{
			ID:        idModePfx + string(m),
			Title:     modeTitle(m, p),
			Indent:    1,
			Checkable: true,
			Checked:   mode == m,
		})
	}

	items = append(items, tray.Item{Title: p.T("servicesShown"), Disabled: true})
	selectedSources := map[string]bool{}
	for _, id := range cfg.EnabledDisplaySources() {
		selectedSources[id] = true
	}
	for _, source := range []struct{ id, title string }{
		{model.SourceClaudeCode, "Claude Code"},
		{model.SourceCodex, "Codex"},
	} {
		if !snapshotHasFamily(snap, source.id) {
			continue
		}
		items = append(items, tray.Item{
			ID:        idSourcePfx + source.id,
			Title:     source.title,
			Indent:    1,
			Checkable: true,
			Checked:   selectedSources[source.id],
		})
	}

	for _, source := range []struct{ id, title string }{
		{model.SourceClaudeCode, p.T("claudeLimits")},
		{model.SourceCodex, p.T("codexLimits")},
	} {
		if !snapshotHasFamily(snap, source.id) {
			continue
		}
		items = append(items, tray.Item{Title: source.title, Disabled: true})
		auto, windows := cfg.LimitSelection(source.id)
		items = append(items, tray.Item{
			ID:        idLimitPfx + source.id + ":auto",
			Title:     p.T("shortest"),
			Indent:    1,
			Checkable: true,
			Checked:   auto,
		})
		enabled := map[model.Window]bool{}
		for _, w := range windows {
			enabled[w] = true
		}
		for _, w := range model.AllWindows {
			items = append(items, tray.Item{
				ID:        idLimitPfx + source.id + ":" + string(w),
				Title:     p.WindowTitle(w),
				Indent:    1,
				Checkable: true,
				Checked:   !auto && enabled[w],
			})
		}
	}

	if snapshotHasFamily(snap, model.SourceCodex) {
		ns := cfg.BankedResetNotifications()
		items = append(items, tray.Item{
			ID: idNotifications, Title: p.T("notifications"), Checkable: true, Checked: ns.Enabled,
		})
	}

	items = append(items, tray.Item{Title: p.T("language"), Disabled: true})
	language := cfg.LanguageSetting()
	for _, choice := range []struct{ id, title string }{
		{i18n.Auto, p.T("systemDefault")}, {i18n.EN, p.T("english")}, {i18n.JA, p.T("japanese")},
	} {
		items = append(items, tray.Item{
			ID: idLanguagePfx + choice.id, Title: choice.title, Indent: 1,
			Checkable: true, Checked: language == choice.id,
		})
	}

	if autostart.Supported() {
		enabled, err := autostart.Enabled()
		item := tray.Item{
			ID: idAutostart, Title: p.T("launchAtStartup"), Checkable: true, Checked: enabled,
		}
		if err != nil {
			item.Tooltip = err.Error()
		}
		items = append(items, item)
	}

	items = append(items,
		tray.Item{Separator: true},
		tray.Item{ID: idRefresh, Title: p.T("refreshNow")},
		tray.Item{ID: idSettings, Title: p.T("settings")},
		// The version doubles as the heading of the about section: a tray menu
		// has no room for a row that only says "About".
		tray.Item{Title: "usagebat " + version.String(), Disabled: true},
		tray.Item{ID: idHomepage, Title: p.T("viewOnGitHub"), Indent: 1, Tooltip: homepageURL},
		tray.Item{ID: idQuit, Title: p.T("quit")},
	)
	return items
}

func modeTitle(mode config.DisplayMode, p i18n.Printer) string {
	switch mode {
	case config.ModeBoth:
		return p.T("modeBoth")
	case config.ModeBattery:
		return p.T("modeBattery")
	case config.ModePercent:
		return p.T("modePercent")
	}
	return string(mode)
}

func earliestResetExpiry(credits []model.RateLimitResetCredit, now time.Time) time.Time {
	var earliest time.Time
	for _, credit := range credits {
		if credit.Status != "" && credit.Status != "available" {
			continue
		}
		if !credit.ExpiresAt.After(now) {
			continue
		}
		if earliest.IsZero() || credit.ExpiresAt.Before(earliest) {
			earliest = credit.ExpiresAt
		}
	}
	return earliest
}

func windowLine(w model.Window, st model.WindowStatus, now time.Time, p i18n.Printer) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-8s ", p.WindowTitle(w))
	if st.Known {
		if p.Japanese() {
			fmt.Fprintf(&b, "残り%3.0f%%", st.RemainingPercent())
		} else {
			fmt.Fprintf(&b, "%3.0f%% left", st.RemainingPercent())
		}
	} else {
		if p.Japanese() {
			b.WriteString("残り ?")
		} else {
			b.WriteString("  ?  left")
		}
	}
	if r := p.FormatReset(st.ResetsAt, now); r != "" {
		b.WriteString("  ·  " + r)
	}
	return b.String()
}

func tokenLine(t model.Tokens, note string, p i18n.Printer) string {
	parts := []string{
		p.T("input") + " " + model.FormatCount(t.Input),
		p.T("output") + " " + model.FormatCount(t.Output),
	}
	if c := t.CacheRead + t.CacheCreation; c > 0 {
		parts = append(parts, p.T("cache")+" "+model.FormatCount(c))
	}
	if t.Weighted > 0 {
		parts = append(parts, p.T("weighted")+" "+model.FormatCount(int64(t.Weighted)))
	}
	line := strings.Join(parts, " · ")
	if note != "" {
		line = p.TranslateNote(note) + ": " + line
	}
	return line
}

func tooltip(snap *model.Snapshot, now time.Time, p i18n.Printer) string {
	var lines []string
	for _, src := range snap.Sources {
		if src.Err != "" {
			lines = append(lines, src.Name+": "+src.Err)
			continue
		}
		var parts []string
		for _, w := range model.AllWindows {
			st, ok := src.Windows[w]
			if !ok || !st.Known {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s %.0f%%", w.Label(), st.RemainingPercent()))
		}
		if len(parts) > 0 {
			lines = append(lines, src.Name+": "+strings.Join(parts, "  "))
		}
		if src.RateLimitResets != nil && src.RateLimitResets.AvailableCount > 0 {
			lines = append(lines, src.Name+": "+p.BankedResets(src.RateLimitResets.AvailableCount,
				earliestResetExpiry(src.RateLimitResets.Credits, now), now))
		}
	}
	if len(lines) == 0 {
		return p.T("noData")
	}
	return strings.Join(lines, "\n")
}

func (a *app) onClick(id string) {
	// Nothing here takes the app lock before dispatching: quitting must not have
	// to wait for a refresh that is currently blocked in a provider subprocess.
	switch {
	case id == idQuit:
		a.backend.Quit()
		return
	case id == idRefresh:
		// A refresh somebody asked for is worth going to the service for; the
		// scheduled ones stay cheap.
		a.authoritative.Store(true)
		a.requestRefresh()
		return
	case id == idSettings:
		a.openSettings()
		return
	case id == idConfig:
		if err := openPath(a.currentConfig().FilePath()); err != nil {
			log.Printf("open config: %v", err)
		}
		return
	case id == idHomepage:
		if err := openPath(homepageURL); err != nil {
			log.Printf("open homepage: %v", err)
		}
		return
	case id == idAutostart:
		enabled, err := autostart.Enabled()
		if err == nil {
			err = autostart.Set(!enabled)
		}
		if err != nil {
			log.Printf("launch at startup: %v", err)
		}
		a.requestRefresh()
		return
	case id == idNotifications:
		a.mutateConfig(func(c *config.Config) error { return c.ToggleBankedResetNotifications() })
	case id == idHistory:
		a.mutateConfig(func(c *config.Config) error { return c.ToggleHistory() })
	case id == idUpdateCheck:
		a.mutateConfig(func(c *config.Config) error { return c.ToggleUpdateCheck() })
	case id == idLimitAlerts:
		a.mutateConfig(func(c *config.Config) error { return c.ToggleLimitThresholds() })
	case strings.HasPrefix(id, idLanguagePfx):
		language := strings.TrimPrefix(id, idLanguagePfx)
		a.mutateConfig(func(c *config.Config) error { return c.SetLanguage(language) })
	case strings.HasPrefix(id, idModePfx):
		m := config.DisplayMode(strings.TrimPrefix(id, idModePfx))
		if !m.Valid() {
			return
		}
		a.mutateConfig(func(c *config.Config) error { return c.SetDisplayMode(m) })
	case strings.HasPrefix(id, idLimitPfx):
		parts := strings.Split(strings.TrimPrefix(id, idLimitPfx), ":")
		if len(parts) != 2 {
			return
		}
		source, choice := parts[0], parts[1]
		if choice == "auto" {
			a.mutateConfig(func(c *config.Config) error { return c.SetAutoShortest(source, true) })
			break
		}
		w, ok := model.ParseWindow(choice)
		if !ok {
			return
		}
		a.mutateConfig(func(c *config.Config) error { return c.ToggleWindow(source, w) })
	case strings.HasPrefix(id, idSourcePfx):
		source := strings.TrimPrefix(id, idSourcePfx)
		a.mutateConfig(func(c *config.Config) error { return c.ToggleDisplaySource(source) })
	default:
		return
	}
	a.requestRefresh()
}

func openPath(path string) error {
	if path == "" {
		return fmt.Errorf("no config path")
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}
