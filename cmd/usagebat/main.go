// Command usagebat shows Claude Code and Codex limit headroom as a
// pixel-art battery in the macOS menu bar or the Windows tray.
package main

import (
	"flag"
	"fmt"
	"image"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yutat23/usagebat/internal/autostart"
	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/model"
	"github.com/yutat23/usagebat/internal/provider"
	"github.com/yutat23/usagebat/internal/provider/claudecode"
	"github.com/yutat23/usagebat/internal/provider/codex"
	"github.com/yutat23/usagebat/internal/render"
	"github.com/yutat23/usagebat/internal/tray"
	"github.com/yutat23/usagebat/internal/version"
)

func init() {
	// AppKit requires its run loop on the process's main thread.
	runtime.LockOSThread()
}

type app struct {
	backend tray.Backend

	// mu guards everything derived from the config, which the update loop may
	// replace when the file changes and click handlers may mutate.
	mu        sync.Mutex
	cfg       *config.Config
	cfgMod    time.Time
	providers []provider.Provider
	palette   render.Palette

	refresh chan struct{}
	snap    atomic.Pointer[model.Snapshot]
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("usagebat: ")

	dump := flag.String("dump", "",
		"collect once, print the menu to stdout, write the icon to this path, and exit")
	foreground := flag.Bool("foreground", false, "run in the foreground and keep the terminal attached")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("usagebat " + version.String())
		return
	}
	if shouldDetach(*foreground, *dump, interactiveTerminal()) {
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
		cfg:     cfg,
		backend: tray.New(),
		refresh: make(chan struct{}, 1),
	}
	a.rebuild()
	a.stampConfig()

	if *dump != "" {
		if err := a.dumpOnce(*dump); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := a.backend.Run(a.onReady, a.onClick); err != nil {
		log.Fatal(err)
	}
}

func shouldDetach(foreground bool, dump string, interactive bool) bool {
	return !foreground && dump == "" && interactive
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

	for _, it := range a.buildMenu(snap, now) {
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

	icon := a.renderIcon(snap)
	if len(icon.Bytes) == 0 {
		return fmt.Errorf("icon render produced no bytes")
	}
	fmt.Printf("\nicon: %d bytes, logical %.0fx%.0f pt -> %s\n",
		len(icon.Bytes), icon.WidthPt, icon.HeightPt, path)
	return os.WriteFile(path, icon.Bytes, 0o644)
}

// rebuild recreates everything derived from the config.
func (a *app) rebuild() {
	a.palette = render.PaletteFrom(a.cfg.Colors)
	a.providers = nil
	if a.cfg.Sources.ClaudeCode.Enabled && claudecode.Available(&a.cfg.Sources.ClaudeCode) {
		a.providers = append(a.providers, claudecode.New(&a.cfg.Sources.ClaudeCode))
	}
	if a.cfg.Sources.Codex.Enabled && codex.Available(&a.cfg.Sources.Codex) {
		// One provider per Codex home: separate homes are separate accounts.
		for _, p := range codex.Providers(&a.cfg.Sources.Codex) {
			a.providers = append(a.providers, p)
		}
	}
}

// stampConfig records the config file's current mtime so our own writes are not
// mistaken for an external edit on the next update.
func (a *app) stampConfig() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if fi, err := os.Stat(a.cfg.FilePath()); err == nil {
		a.cfgMod = fi.ModTime()
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

func (a *app) onReady() {
	interval := a.refreshInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		a.update()
		if next := a.refreshInterval(); next != interval {
			interval = next
			ticker.Reset(interval)
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
	a.mu.Lock()
	defer a.mu.Unlock()

	a.reloadConfigIfChanged()

	now := time.Now()
	snap := &model.Snapshot{UpdatedAt: now}
	for _, p := range a.providers {
		snap.Sources = append(snap.Sources, p.Collect(now))
	}
	snap.AggregateSources(model.AllWindows, a.cfg.EnabledDisplaySources())
	a.snap.Store(snap)

	a.backend.SetIcon(a.renderIcon(snap))
	a.backend.SetTooltip(tooltip(snap, now))
	a.backend.SetMenu(a.buildMenu(snap, now))
}

// reloadConfigIfChanged picks up edits made in the config file, so calibrating
// the Claude Code limits does not need a restart.
func (a *app) reloadConfigIfChanged() {
	path := a.cfg.FilePath()
	if path == "" {
		return
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.ModTime().After(a.cfgMod) {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		log.Printf("config reload: %v", err)
		a.cfgMod = fi.ModTime()
		return
	}
	a.cfg = cfg
	a.cfgMod = fi.ModTime()
	a.rebuild()
}

func (a *app) renderIcon(snap *model.Snapshot) tray.IconData {
	cells := a.displayCells(snap)
	opts := render.Options{
		Mode:    a.cfg.DisplayMode,
		Palette: a.palette,
		Scale:   a.cfg.Icon.PixelScale,
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
			WidthPt:  float64(icon.DotsW) * a.cfg.Icon.DotSize,
			HeightPt: float64(icon.DotsH) * a.cfg.Icon.DotSize,
		}
	}

	data, err := render.ICO(func(size int) *image.RGBA {
		o := opts
		o.Scale = 1
		base := render.SquareCells(cells, o, a.cfg.Icon.WindowsLayout).Image
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
func (a *app) displayCells(snap *model.Snapshot) []render.Cell {
	var cells []render.Cell
	for _, family := range a.cfg.EnabledDisplaySources() {
		if !snapshotHasFamily(snap, family) {
			continue
		}
		aggregated := &model.Snapshot{Sources: snap.Sources}
		aggregated.AggregateSources(model.AllWindows, []string{family})
		auto, windows := a.cfg.LimitSelection(family)
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
		prefix := "CL"
		if family == model.SourceCodex {
			prefix = "CX"
		}
		for _, w := range windows {
			st := aggregated.Icon[w]
			st.Window = w
			cells = append(cells, render.Cell{Label: prefix + w.Label(), Status: st})
		}
	}
	return cells
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
	idRefresh   = "refresh"
	idConfig    = "config"
	idAutostart = "autostart"
	idQuit      = "quit"
	idModePfx   = "mode:"
	idSourcePfx = "source:"
	idLimitPfx  = "limit:"
)

func (a *app) buildMenu(snap *model.Snapshot, now time.Time) []tray.Item {
	var items []tray.Item

	for _, src := range snap.Sources {
		name := src.Name
		if src.Note != "" {
			name += "  —  " + src.Note
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
			items = append(items, tray.Item{Title: windowLine(w, st, now), Disabled: true, Indent: 1})
			if tok, ok := src.Tokens[w]; ok && tok.Total() > 0 {
				items = append(items, tray.Item{
					Title:    tokenLine(tok, src.TokensNote),
					Disabled: true,
					Indent:   2,
				})
			}
		}
		items = append(items, tray.Item{Separator: true})
	}

	items = append(items, tray.Item{Title: "Icon style", Disabled: true})
	for _, m := range []config.DisplayMode{config.ModeBoth, config.ModeBattery, config.ModePercent} {
		items = append(items, tray.Item{
			ID:        idModePfx + string(m),
			Title:     m.Title(),
			Indent:    1,
			Checkable: true,
			Checked:   a.cfg.DisplayMode == m,
		})
	}

	items = append(items, tray.Item{Title: "Services shown in icon", Disabled: true})
	selectedSources := map[string]bool{}
	for _, id := range a.cfg.EnabledDisplaySources() {
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
		{model.SourceClaudeCode, "Claude Code limits"},
		{model.SourceCodex, "Codex limits"},
	} {
		if !snapshotHasFamily(snap, source.id) {
			continue
		}
		items = append(items, tray.Item{Title: source.title, Disabled: true})
		auto, windows := a.cfg.LimitSelection(source.id)
		items = append(items, tray.Item{
			ID:        idLimitPfx + source.id + ":auto",
			Title:     "Shortest available",
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
				Title:     w.Title(),
				Indent:    1,
				Checkable: true,
				Checked:   !auto && enabled[w],
			})
		}
	}

	if autostart.Supported() {
		enabled, err := autostart.Enabled()
		item := tray.Item{
			ID: idAutostart, Title: "Launch at startup", Checkable: true, Checked: enabled,
		}
		if err != nil {
			item.Tooltip = err.Error()
		}
		items = append(items, item)
	}

	items = append(items,
		tray.Item{Separator: true},
		tray.Item{ID: idRefresh, Title: "Refresh now"},
		tray.Item{ID: idConfig, Title: "Open config file…"},
		tray.Item{ID: idQuit, Title: "Quit"},
	)
	return items
}

func windowLine(w model.Window, st model.WindowStatus, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-8s ", w.Title())
	if st.Known {
		fmt.Fprintf(&b, "%3.0f%% left", st.RemainingPercent())
	} else {
		b.WriteString("  ?  left")
	}
	if r := model.FormatReset(st.ResetsAt, now); r != "" {
		b.WriteString("  ·  " + r)
	}
	if st.Estimated {
		b.WriteString("  (est)")
	}
	return b.String()
}

func tokenLine(t model.Tokens, note string) string {
	parts := []string{
		"in " + model.FormatCount(t.Input),
		"out " + model.FormatCount(t.Output),
	}
	if c := t.CacheRead + t.CacheCreation; c > 0 {
		parts = append(parts, "cache "+model.FormatCount(c))
	}
	if t.Weighted > 0 {
		parts = append(parts, "weighted "+model.FormatCount(int64(t.Weighted)))
	}
	line := strings.Join(parts, " · ")
	if note != "" {
		line = note + ": " + line
	}
	return line
}

func tooltip(snap *model.Snapshot, now time.Time) string {
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
	}
	if len(lines) == 0 {
		return "usagebat: no data"
	}
	return strings.Join(lines, "\n")
}

func (a *app) onClick(id string) {
	cfg := a.currentConfig()
	switch {
	case id == idQuit:
		a.backend.Quit()
		return
	case id == idRefresh:
		a.requestRefresh()
		return
	case id == idConfig:
		if err := openPath(cfg.FilePath()); err != nil {
			log.Printf("open config: %v", err)
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
	case strings.HasPrefix(id, idModePfx):
		m := config.DisplayMode(strings.TrimPrefix(id, idModePfx))
		if !m.Valid() {
			return
		}
		if err := cfg.SetDisplayMode(m); err != nil {
			log.Printf("save config: %v", err)
		}
	case strings.HasPrefix(id, idLimitPfx):
		parts := strings.Split(strings.TrimPrefix(id, idLimitPfx), ":")
		if len(parts) != 2 {
			return
		}
		source, choice := parts[0], parts[1]
		if choice == "auto" {
			if err := cfg.SetAutoShortest(source, true); err != nil {
				log.Printf("save config: %v", err)
			}
			break
		}
		w, ok := model.ParseWindow(choice)
		if !ok {
			return
		}
		if err := cfg.ToggleWindow(source, w); err != nil {
			log.Printf("save config: %v", err)
		}
	case strings.HasPrefix(id, idSourcePfx):
		source := strings.TrimPrefix(id, idSourcePfx)
		if err := cfg.ToggleDisplaySource(source); err != nil {
			log.Printf("save config: %v", err)
		}
	default:
		return
	}
	// The write above is ours, so do not let it look like an external edit and
	// trigger a config reload.
	a.stampConfig()
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
