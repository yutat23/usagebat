// Package config loads, defaults and persists the user configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/yutat23/usagebat/internal/model"
)

// DisplayMode selects what the tray icon draws.
type DisplayMode string

const (
	ModeBoth    DisplayMode = "both"    // battery outline + fill + percent overlay
	ModeBattery DisplayMode = "battery" // battery only
	ModePercent DisplayMode = "percent" // digits only
)

// Valid reports whether m is a known mode.
func (m DisplayMode) Valid() bool {
	return m == ModeBoth || m == ModeBattery || m == ModePercent
}

// Title is the menu label for the mode.
func (m DisplayMode) Title() string {
	switch m {
	case ModeBoth:
		return "Battery + %"
	case ModeBattery:
		return "Battery only"
	case ModePercent:
		return "% only"
	}
	return string(m)
}

// Config is the on-disk configuration.
type Config struct {
	Version        int                     `json:"version"`
	Language       string                  `json:"language"`
	DisplayMode    DisplayMode             `json:"displayMode"`
	DisplaySources []string                `json:"displaySources"`
	DisplayLimits  map[string]LimitDisplay `json:"displayLimits"`
	// AutoShortest and Windows are retained only to migrate v1-v3 configs.
	AutoShortest   bool          `json:"autoShortest,omitempty"`
	Windows        []string      `json:"windows,omitempty"`
	RefreshSeconds int           `json:"refreshSeconds"`
	Icon           Icon          `json:"icon"`
	Colors         Colors        `json:"colors"`
	Notifications  Notifications `json:"notifications"`
	Sources        Sources       `json:"sources"`

	path string
	mu   sync.Mutex
	// stamp is the file's modification time as of the last read or write we did
	// ourselves. It is guarded by mu together with Save, so a write in progress
	// can never be observed as somebody else's edit.
	stamp time.Time
}

type Notifications struct {
	BankedResetExpiry BankedResetExpiry `json:"bankedResetExpiry"`
}

type BankedResetExpiry struct {
	Enabled        bool  `json:"enabled"`
	ThresholdHours []int `json:"thresholdHours"`
}

// LimitDisplay independently selects periods for one service.
type LimitDisplay struct {
	AutoShortest bool     `json:"autoShortest"`
	Windows      []string `json:"windows"`
}

// Icon holds rendering geometry.
type Icon struct {
	// DotSize is the logical size in points of one art dot (macOS).
	DotSize float64 `json:"dotSize"`
	// PixelScale is how many bitmap pixels one art dot becomes.
	PixelScale int `json:"pixelScale"`
	// WindowsLayout is "stack" (three mini bars) or "single" (one battery).
	WindowsLayout string `json:"windowsLayout"`
}

// Colors holds the palette and the thresholds that pick between them.
type Colors struct {
	Light         ThemeColors `json:"light"`
	Dark          ThemeColors `json:"dark"`
	WarnBelow     float64     `json:"warnBelow"`
	CriticalBelow float64     `json:"criticalBelow"`

	// V1-V4 stored one palette for every system appearance. Keep these fields
	// readable so customized configs can be migrated to both theme palettes.
	LegacyGood       string `json:"good,omitempty"`
	LegacyWarn       string `json:"warn,omitempty"`
	LegacyCritical   string `json:"critical,omitempty"`
	LegacyUnknown    string `json:"unknown,omitempty"`
	LegacyLabel      string `json:"label,omitempty"`
	LegacyTextOnFill string `json:"textOnFill,omitempty"`
}

// ThemeColors is one complete palette for a light or dark system bar.
type ThemeColors struct {
	Good       string `json:"good"`
	Warn       string `json:"warn"`
	Critical   string `json:"critical"`
	Unknown    string `json:"unknown"`
	Claude     string `json:"claude"`
	Codex      string `json:"codex"`
	Period     string `json:"period"`
	TextOnFill string `json:"textOnFill"`
}

// Sources groups the per-provider settings.
type Sources struct {
	ClaudeCode ClaudeCode `json:"claudeCode"`
	Codex      Codex      `json:"codex"`
}

// ClaudeCode configures the local service-usage cache, the legacy /usage
// fallback, and the transcript estimator that fills remaining gaps.
type ClaudeCode struct {
	Enabled      bool         `json:"enabled"`
	UsageCommand UsageCommand `json:"usageCommand"`
	// UsageCacheFile is Claude Code's locally cached service usage response.
	// Empty means ~/.claude.json.
	UsageCacheFile string           `json:"usageCacheFile"`
	ProjectsDir    string           `json:"projectsDir"`
	WeeklyMode     string           `json:"weeklyMode"`  // rolling | calendar
	MonthlyMode    string           `json:"monthlyMode"` // calendar | rolling
	Weights        Weights          `json:"weights"`
	Limits         map[string]int64 `json:"limits"`
}

// UsageCommand configures polling of `claude -p /usage`.
type UsageCommand struct {
	Enabled bool `json:"enabled"`
	// Path is the CLI to run. Empty means auto-detect.
	Path           string `json:"path"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	// MinIntervalSeconds throttles the subprocess independently of the refresh
	// interval, so repeated manual refreshes cannot spawn a process per click.
	MinIntervalSeconds int `json:"minIntervalSeconds"`
	// StaleAfterSeconds is how long a previous good reading stays usable when a
	// run fails, so a transient error does not make the battery jump.
	StaleAfterSeconds int `json:"staleAfterSeconds"`
}

// Weights normalises raw token counts into a single comparable figure.
type Weights struct {
	Output        float64            `json:"output"`
	CacheCreation float64            `json:"cacheCreation"`
	CacheRead     float64            `json:"cacheRead"`
	Models        map[string]float64 `json:"models"`
}

// Codex configures the live Codex reader and rollout-log fallback.
type Codex struct {
	Enabled bool `json:"enabled"`
	// Path is the Codex CLI to run. Empty means auto-detect.
	Path           string `json:"path"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	// Homes lists CODEX_HOME directories. "auto" resolves to $CODEX_HOME, or
	// ~/.codex. Anything else is taken literally. Each home becomes its own
	// entry in the menu, because separate homes are separate accounts.
	Homes []string `json:"homes"`
}

// Default returns the shipped configuration.
func Default() *Config {
	return &Config{
		Version:        6,
		Language:       "auto",
		DisplayMode:    ModeBoth,
		DisplaySources: []string{model.SourceClaudeCode, model.SourceCodex},
		DisplayLimits: map[string]LimitDisplay{
			model.SourceClaudeCode: {AutoShortest: true, Windows: []string{"5h"}},
			model.SourceCodex:      {AutoShortest: true, Windows: []string{"5h"}},
		},
		RefreshSeconds: 60,
		Icon: Icon{
			DotSize:       1.2,
			PixelScale:    2,
			WindowsLayout: "stack",
		},
		Colors: Colors{
			Light: ThemeColors{
				Good: "#15803D", Warn: "#A16207", Critical: "#BE123C",
				Unknown: "#52525B", Claude: "#A94F32", Codex: "#087567",
				Period: "#25272B", TextOnFill: "#F8FAFC",
			},
			Dark: ThemeColors{
				Good: "#4ADE80", Warn: "#FACC15", Critical: "#FB7185",
				Unknown: "#A1A1AA", Claude: "#E58A68", Codex: "#52C7B8",
				Period: "#F2F2F2", TextOnFill: "#101010",
			},
			WarnBelow:     50,
			CriticalBelow: 20,
		},
		Notifications: Notifications{BankedResetExpiry: BankedResetExpiry{
			Enabled: true, ThresholdHours: []int{168, 24},
		}},
		Sources: Sources{
			ClaudeCode: ClaudeCode{
				Enabled: true,
				UsageCommand: UsageCommand{
					Enabled:            true,
					TimeoutSeconds:     20,
					MinIntervalSeconds: 30,
					StaleAfterSeconds:  900,
				},
				WeeklyMode:  "rolling",
				MonthlyMode: "calendar",
				Weights: Weights{
					Output:        5,
					CacheCreation: 1.25,
					CacheRead:     0.1,
					Models: map[string]float64{
						"opus":   5,
						"sonnet": 1,
						"haiku":  0.2,
					},
				},
				// Weighted-token budgets. Anthropic does not publish real limits and
				// does not expose remaining quota locally, so these are calibration
				// seeds: set them by comparing the weighted figures in the menu
				// against what /usage reports. 0 disables estimation for a window.
				Limits: map[string]int64{
					"5h":     10_000_000,
					"weekly": 60_000_000,
				},
			},
			Codex: Codex{
				Enabled:        true,
				TimeoutSeconds: 15,
				Homes:          []string{"auto"},
			},
		},
	}
}

// Path returns the config file location for this OS.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "usagebat", "config.json"), nil
}

// Load reads the config, creating it with defaults when absent. A malformed
// file is reported but does not stop startup: defaults are used instead.
func Load() (*Config, error) {
	cfg := Default()
	path, err := Path()
	if err != nil {
		// Callers keep running on a usable config; only persistence is lost.
		return cfg, err
	}
	cfg.path = path

	// Stamped before the read, so a file rewritten in between is picked up on
	// the next refresh rather than silently kept at its old contents.
	if fi, err := os.Stat(path); err == nil {
		cfg.stamp = fi.ModTime()
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, cfg.Save()
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		// Unmarshal applies every field it got through before failing, and the
		// normalisation below never runs on this path. A half-applied struct can
		// hold values no part of the app is prepared for — a zero refresh
		// interval, an unrenderable icon scale — so start over from the defaults.
		fresh := Default()
		fresh.path, fresh.stamp = path, cfg.stamp
		return fresh, fmt.Errorf("%s: %w", path, err)
	}
	migrated := cfg.migrate(data)
	cfg.normalise()
	if migrated {
		if err := cfg.Save(); err != nil {
			return cfg, fmt.Errorf("saving migrated config: %w", err)
		}
	}
	return cfg, nil
}

// migrate updates only values that are known to be an old shipped default.
// V4 made period selection independent per service. V5 added separate light
// and dark palettes. V6 added localization and reset-expiry notifications.
func (c *Config) migrate(data []byte) bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return false
	}
	version := 0
	if encoded, ok := raw["version"]; ok {
		_ = json.Unmarshal(encoded, &version)
	}
	if version >= 6 {
		return false
	}
	if version < 4 {
		if version == 0 && sameStrings(c.Windows, []string{"5h", "weekly", "monthly"}) {
			c.Windows = []string{"5h"}
		}
		if version < 3 {
			c.AutoShortest = true
		}
		c.DisplayLimits = map[string]LimitDisplay{}
		for _, source := range allDisplaySources {
			c.DisplayLimits[source] = LimitDisplay{
				AutoShortest: c.AutoShortest,
				Windows:      append([]string(nil), c.Windows...),
			}
		}
		if c.Icon.DotSize <= 1.0 {
			c.Icon.DotSize = 1.2
		}
	}
	if version < 5 {
		c.migrateLegacyColors()
	}
	c.Version = 6
	return true
}

func (c *Config) migrateLegacyColors() {
	legacy := ThemeColors{
		Good: c.Colors.LegacyGood, Warn: c.Colors.LegacyWarn,
		Critical: c.Colors.LegacyCritical, Unknown: c.Colors.LegacyUnknown,
		Period: c.Colors.LegacyLabel, TextOnFill: c.Colors.LegacyTextOnFill,
	}
	// Preserve a user's custom V1-V4 palette. The old shipped palette is
	// replaced by the more legible theme-aware defaults.
	oldDefault := ThemeColors{
		Good: "#3DDC64", Warn: "#FFC63D", Critical: "#FF4C4C",
		Unknown: "#8E8E93", Period: "#8E8E93", TextOnFill: "#101010",
	}
	if legacy.Good != "" && legacy != oldDefault {
		c.Colors.Light = overlayTheme(c.Colors.Light, legacy)
		c.Colors.Dark = overlayTheme(c.Colors.Dark, legacy)
	}
	c.Colors.LegacyGood = ""
	c.Colors.LegacyWarn = ""
	c.Colors.LegacyCritical = ""
	c.Colors.LegacyUnknown = ""
	c.Colors.LegacyLabel = ""
	c.Colors.LegacyTextOnFill = ""
}

func overlayTheme(dst, src ThemeColors) ThemeColors {
	if src.Good != "" {
		dst.Good = src.Good
	}
	if src.Warn != "" {
		dst.Warn = src.Warn
	}
	if src.Critical != "" {
		dst.Critical = src.Critical
	}
	if src.Unknown != "" {
		dst.Unknown = src.Unknown
	}
	if src.Period != "" {
		dst.Period = src.Period
	}
	if src.TextOnFill != "" {
		dst.TextOnFill = src.TextOnFill
	}
	return dst
}

func fillTheme(dst, defaults ThemeColors) ThemeColors {
	if dst.Good == "" {
		dst.Good = defaults.Good
	}
	if dst.Warn == "" {
		dst.Warn = defaults.Warn
	}
	if dst.Critical == "" {
		dst.Critical = defaults.Critical
	}
	if dst.Unknown == "" {
		dst.Unknown = defaults.Unknown
	}
	if dst.Claude == "" {
		dst.Claude = defaults.Claude
	}
	if dst.Codex == "" {
		dst.Codex = defaults.Codex
	}
	if dst.Period == "" {
		dst.Period = defaults.Period
	}
	if dst.TextOnFill == "" {
		dst.TextOnFill = defaults.TextOnFill
	}
	return dst
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// normalise repairs values that would otherwise break rendering.
func (c *Config) normalise() {
	if c.Language != "auto" && c.Language != "en" && c.Language != "ja" {
		c.Language = "auto"
	}
	if !c.DisplayMode.Valid() {
		c.DisplayMode = ModeBoth
	}
	if c.RefreshSeconds < 5 {
		c.RefreshSeconds = 5
	}
	if c.Icon.DotSize <= 0 {
		c.Icon.DotSize = 1.2
	}
	if c.Icon.PixelScale < 1 {
		c.Icon.PixelScale = 2
	}
	if c.Icon.WindowsLayout != "stack" && c.Icon.WindowsLayout != "single" {
		c.Icon.WindowsLayout = "stack"
	}
	if len(c.EnabledDisplaySources()) == 0 {
		c.DisplaySources = append([]string(nil), Default().DisplaySources...)
	}
	d := Default()
	if c.DisplayLimits == nil {
		c.DisplayLimits = map[string]LimitDisplay{}
	}
	for _, source := range allDisplaySources {
		selection, ok := c.DisplayLimits[source]
		if !ok {
			c.DisplayLimits[source] = d.DisplayLimits[source]
			continue
		}
		if len(enabledWindows(selection.Windows)) == 0 {
			selection.Windows = []string{"5h"}
			c.DisplayLimits[source] = selection
		}
	}
	c.Colors.Light = fillTheme(c.Colors.Light, d.Colors.Light)
	c.Colors.Dark = fillTheme(c.Colors.Dark, d.Colors.Dark)
	if c.Colors.WarnBelow <= 0 {
		c.Colors.WarnBelow = d.Colors.WarnBelow
	}
	if c.Colors.CriticalBelow <= 0 {
		c.Colors.CriticalBelow = d.Colors.CriticalBelow
	}
	c.Notifications.BankedResetExpiry.ThresholdHours = normaliseThresholds(
		c.Notifications.BankedResetExpiry.ThresholdHours,
		d.Notifications.BankedResetExpiry.ThresholdHours,
	)
	if c.Sources.ClaudeCode.Weights.Output == 0 {
		c.Sources.ClaudeCode.Weights = d.Sources.ClaudeCode.Weights
	}
	uc := &c.Sources.ClaudeCode.UsageCommand
	if uc.TimeoutSeconds <= 0 {
		uc.TimeoutSeconds = d.Sources.ClaudeCode.UsageCommand.TimeoutSeconds
	}
	if uc.MinIntervalSeconds < 0 {
		uc.MinIntervalSeconds = d.Sources.ClaudeCode.UsageCommand.MinIntervalSeconds
	}
	if uc.StaleAfterSeconds <= 0 {
		uc.StaleAfterSeconds = d.Sources.ClaudeCode.UsageCommand.StaleAfterSeconds
	}
	if c.Sources.Codex.TimeoutSeconds <= 0 {
		c.Sources.Codex.TimeoutSeconds = d.Sources.Codex.TimeoutSeconds
	}
}

func normaliseThresholds(values, defaults []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	if len(out) == 0 {
		out = append(out, defaults...)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

var allDisplaySources = []string{model.SourceClaudeCode, model.SourceCodex}

// EnabledDisplaySources returns the provider families included in the tray
// icon. Collection and menu details remain enabled independently under Sources.
func (c *Config) EnabledDisplaySources() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabledDisplaySourcesLocked()
}

func (c *Config) enabledDisplaySourcesLocked() []string {
	set := map[string]bool{}
	for _, id := range c.DisplaySources {
		set[id] = true
	}
	out := make([]string, 0, len(set))
	for _, id := range allDisplaySources {
		if set[id] {
			out = append(out, id)
		}
	}
	return out
}

// ToggleDisplaySource includes or excludes a provider family from the icon,
// refusing to remove the last one so the icon always has a data source.
func (c *Config) ToggleDisplaySource(id string) error {
	c.mu.Lock()
	known := false
	for _, candidate := range allDisplaySources {
		known = known || candidate == id
	}
	if !known {
		c.mu.Unlock()
		return nil
	}
	cur := c.enabledDisplaySourcesLocked()
	on := false
	for _, candidate := range cur {
		on = on || candidate == id
	}
	if on && len(cur) == 1 {
		c.mu.Unlock()
		return nil
	}
	next := make([]string, 0, len(allDisplaySources))
	for _, candidate := range allDisplaySources {
		keep := false
		for _, selected := range cur {
			keep = keep || selected == candidate
		}
		if candidate == id {
			keep = !on
		}
		if keep {
			next = append(next, candidate)
		}
	}
	c.DisplaySources = next
	c.mu.Unlock()
	return c.Save()
}

// LimitSelection returns one service's automatic flag and explicit periods.
func (c *Config) LimitSelection(source string) (bool, []model.Window) {
	c.mu.Lock()
	defer c.mu.Unlock()
	selection, ok := c.DisplayLimits[source]
	if !ok {
		selection = LimitDisplay{AutoShortest: true, Windows: []string{"5h"}}
	}
	return selection.AutoShortest, enabledWindows(selection.Windows)
}

func enabledWindows(values []string) []model.Window {
	set := map[model.Window]bool{}
	for _, s := range values {
		if w, ok := model.ParseWindow(s); ok {
			set[w] = true
		}
	}
	out := make([]model.Window, 0, len(set))
	for _, w := range model.AllWindows {
		if set[w] {
			out = append(out, w)
		}
	}
	return out
}

// SetDisplayMode persists a new display mode.
func (c *Config) SetDisplayMode(m DisplayMode) error {
	c.mu.Lock()
	c.DisplayMode = m
	c.mu.Unlock()
	return c.Save()
}

// ToggleWindow enables or disables one explicit period for one service.
func (c *Config) ToggleWindow(source string, w model.Window) error {
	c.mu.Lock()
	selection, ok := c.DisplayLimits[source]
	if !ok {
		selection = LimitDisplay{AutoShortest: true, Windows: []string{"5h"}}
	}
	if selection.AutoShortest {
		selection.AutoShortest = false
		selection.Windows = []string{string(w)}
		c.DisplayLimits[source] = selection
		c.mu.Unlock()
		return c.Save()
	}
	selection.AutoShortest = false
	cur := enabledWindows(selection.Windows)
	on := false
	for _, x := range cur {
		if x == w {
			on = true
		}
	}
	if on && len(cur) == 1 {
		c.mu.Unlock()
		return nil
	}
	next := make([]string, 0, len(model.AllWindows))
	for _, x := range model.AllWindows {
		keep := false
		for _, y := range cur {
			if x == y {
				keep = true
			}
		}
		if x == w {
			keep = !on
		}
		if keep {
			next = append(next, string(x))
		}
	}
	selection.Windows = next
	c.DisplayLimits[source] = selection
	c.mu.Unlock()
	return c.Save()
}

// SetAutoShortest switches one service to automatic limit selection.
func (c *Config) SetAutoShortest(source string, enabled bool) error {
	c.mu.Lock()
	selection, ok := c.DisplayLimits[source]
	if !ok {
		selection = LimitDisplay{Windows: []string{"5h"}}
	}
	selection.AutoShortest = enabled
	c.DisplayLimits[source] = selection
	c.mu.Unlock()
	return c.Save()
}

// Save writes the config atomically.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" {
		p, err := Path()
		if err != nil {
			return err
		}
		c.path = p
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return err
	}
	// Recorded under the same lock as the write itself: ExternallyModified can
	// then never catch the file between the rename and the stamp and mistake our
	// own save for a hand edit.
	if fi, err := os.Stat(c.path); err == nil {
		c.stamp = fi.ModTime()
	}
	return nil
}

// ExternallyModified reports whether the file changed since this config last
// read or wrote it, i.e. whether somebody edited it behind the app's back.
func (c *Config) ExternallyModified() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" {
		return false
	}
	fi, err := os.Stat(c.path)
	if err != nil {
		return false
	}
	return fi.ModTime().After(c.stamp)
}

// MarkSeen accepts the file's current state without adopting its contents. It
// is what keeps an unparseable edit from being re-read, and re-reported, on
// every refresh.
func (c *Config) MarkSeen() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" {
		return
	}
	if fi, err := os.Stat(c.path); err == nil {
		c.stamp = fi.ModTime()
	}
}

// Mode, Palette and IconGeometry read the fields the renderer needs under the
// same lock the menu's click handlers write them with.
func (c *Config) Mode() DisplayMode {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.DisplayMode
}

func (c *Config) Palette() Colors {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Colors
}

func (c *Config) IconGeometry() Icon {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Icon
}

func (c *Config) LanguageSetting() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Language
}

func (c *Config) SetLanguage(language string) error {
	if language != "auto" && language != "en" && language != "ja" {
		return nil
	}
	c.mu.Lock()
	c.Language = language
	c.mu.Unlock()
	return c.Save()
}

func (c *Config) BankedResetNotifications() BankedResetExpiry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.Notifications.BankedResetExpiry
	out.ThresholdHours = append([]int(nil), out.ThresholdHours...)
	return out
}

func (c *Config) ToggleBankedResetNotifications() error {
	c.mu.Lock()
	c.Notifications.BankedResetExpiry.Enabled = !c.Notifications.BankedResetExpiry.Enabled
	c.mu.Unlock()
	return c.Save()
}

// FilePath is the resolved config path.
func (c *Config) FilePath() string { return c.path }
