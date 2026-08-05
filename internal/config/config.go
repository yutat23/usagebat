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

// SchemaVersion is the config layout this build writes. Bumping it makes
// migrate rewrite the file once, so an added setting appears with its default.
const SchemaVersion = 7

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
	UpdateCheck    UpdateCheck   `json:"updateCheck"`
	History        History       `json:"history"`
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
	LimitThresholds   LimitThresholds   `json:"limitThresholds"`
}

// LimitThresholds warns when headroom drops past a percentage. Only the limits
// shown in the icon are watched: choosing what the icon shows is how the user
// says which limits they care about.
type LimitThresholds struct {
	Enabled bool `json:"enabled"`
	// Percents are remaining-percentage marks, e.g. 50 and 20.
	Percents []int `json:"percents"`
}

type BankedResetExpiry struct {
	Enabled        bool  `json:"enabled"`
	ThresholdHours []int `json:"thresholdHours"`
}

// UpdateCheck governs usagebat's only outbound network request. It ships off:
// everything else the app does is local, and asking GitHub for a release
// number is a choice the user gets to make.
type UpdateCheck struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"intervalHours"`
}

// History governs the samples the usage charts are drawn from. It stays on
// this machine; the charts have nothing to show without it, so it ships on.
type History struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"intervalMinutes"`
	RetentionDays   int  `json:"retentionDays"`
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
// fallback, and the transcript scan behind the token tallies.
type ClaudeCode struct {
	Enabled      bool         `json:"enabled"`
	UsageCommand UsageCommand `json:"usageCommand"`
	// UsageCacheFile is Claude Code's locally cached service usage response.
	// Empty means ~/.claude.json.
	UsageCacheFile string `json:"usageCacheFile"`
	ProjectsDir    string `json:"projectsDir"`
	// WeeklyMode and MonthlyMode bound the periods the token tallies are summed
	// over. They no longer decide any percentage.
	WeeklyMode  string  `json:"weeklyMode"`  // rolling | calendar
	MonthlyMode string  `json:"monthlyMode"` // calendar | rolling
	Weights     Weights `json:"weights"`
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
	// Profiles lists the accounts to track. "auto" resolves to $CODEX_HOME, or
	// ~/.codex. Anything else is taken literally. Each one becomes its own
	// entry, because separate homes are separate accounts.
	Profiles []Profile `json:"profiles"`
	// Homes is the pre-v7 form, read once so an existing config migrates.
	Homes []string `json:"homes,omitempty"`
}

// Profile is one account, with the name it is shown under.
type Profile struct {
	// Path is the profile directory. "auto" means the standard location.
	Path string `json:"path"`
	// Label names it in menus, charts and notifications. Empty falls back to
	// the directory name, which is usually a hash nobody can read.
	Label string `json:"label"`
	// Short is the one or two characters the icon has room for. Empty derives
	// one from Label.
	Short string `json:"short"`
}

// Icon returns the abbreviation to draw, at most two characters.
func (p Profile) Icon() string {
	source := p.Short
	if source == "" {
		source = p.Label
	}
	runes := []rune(source)
	if len(runes) > 2 {
		runes = runes[:2]
	}
	return string(runes)
}

// Default returns the shipped configuration.
func Default() *Config {
	return &Config{
		Version:        SchemaVersion,
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
		Notifications: Notifications{
			BankedResetExpiry: BankedResetExpiry{
				Enabled: true, ThresholdHours: []int{168, 24},
			},
			LimitThresholds: LimitThresholds{
				Enabled: true, Percents: []int{50, 20},
			},
		},
		UpdateCheck: UpdateCheck{Enabled: false, IntervalHours: 24},
		History:     History{Enabled: true, IntervalMinutes: 5, RetentionDays: 30},
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
			},
			Codex: Codex{
				Enabled:        true,
				TimeoutSeconds: 15,
				Profiles:       []Profile{{Path: "auto"}},
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
// V7 added the update check, which needs no conversion: an absent key leaves
// the shipped default, and that default is off.
func (c *Config) migrate(data []byte) bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return false
	}
	version := 0
	if encoded, ok := raw["version"]; ok {
		_ = json.Unmarshal(encoded, &version)
	}
	if version >= SchemaVersion {
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
	c.migrateCodexHomes()
	c.Version = SchemaVersion
	return true
}

// migrateCodexHomes turns the v6 list of directories into profiles. The label
// is left empty so the provider keeps naming them the way it always has;
// somebody who wants a readable name now has somewhere to put one.
func (c *Config) migrateCodexHomes() {
	if len(c.Sources.Codex.Profiles) > 0 || len(c.Sources.Codex.Homes) == 0 {
		return
	}
	for _, home := range c.Sources.Codex.Homes {
		c.Sources.Codex.Profiles = append(c.Sources.Codex.Profiles, Profile{Path: home})
	}
	c.Sources.Codex.Homes = nil
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
	// A check every few minutes would be pointless and rude to GitHub.
	if c.UpdateCheck.IntervalHours < 1 {
		c.UpdateCheck.IntervalHours = 24
	}
	// One sample a minute is more detail than any chart can show, and it would
	// make the history file forty times bigger.
	if c.History.IntervalMinutes < 1 {
		c.History.IntervalMinutes = 5
	}
	if c.History.RetentionDays < 1 {
		c.History.RetentionDays = 30
	}
	// A config that names no profile still tracks the standard location; an
	// empty list would silently drop Codex off the icon.
	c.migrateCodexHomes()
	if len(c.Sources.Codex.Profiles) == 0 {
		c.Sources.Codex.Profiles = []Profile{{Path: "auto"}}
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
	// A remaining percentage above 100 can never be crossed and one at zero
	// fires when the limit is already spent, so neither is worth keeping.
	c.Notifications.LimitThresholds.Percents = normaliseThresholds(
		clampPercents(c.Notifications.LimitThresholds.Percents),
		d.Notifications.LimitThresholds.Percents,
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

func clampPercents(values []int) []int {
	var out []int
	for _, value := range values {
		if value > 0 && value <= 100 {
			out = append(out, value)
		}
	}
	return out
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

// enabledDisplaySourcesLocked returns the selections in a stable order: the
// known families first, in their canonical order, then any account named
// explicitly. An entry naming one account draws that account on its own
// instead of folding it into its family's figure.
func (c *Config) enabledDisplaySourcesLocked() []string {
	set := map[string]bool{}
	for _, id := range c.DisplaySources {
		set[id] = true
	}
	out := make([]string, 0, len(set))
	for _, id := range allDisplaySources {
		if set[id] {
			out = append(out, id)
			delete(set, id)
		}
	}
	// Whatever is left names an account. Config order decides theirs, since
	// there is no canonical order to fall back on.
	for _, id := range c.DisplaySources {
		if set[id] {
			out = append(out, id)
			delete(set, id)
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

func (c *Config) LimitThresholdSettings() LimitThresholds {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.Notifications.LimitThresholds
	out.Percents = append([]int(nil), out.Percents...)
	return out
}

func (c *Config) ToggleLimitThresholds() error {
	c.mu.Lock()
	c.Notifications.LimitThresholds.Enabled = !c.Notifications.LimitThresholds.Enabled
	c.mu.Unlock()
	return c.Save()
}

func (c *Config) UpdateCheckSettings() UpdateCheck {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.UpdateCheck
}

func (c *Config) HistorySettings() History {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.History
}

func (c *Config) ToggleHistory() error {
	c.mu.Lock()
	c.History.Enabled = !c.History.Enabled
	c.mu.Unlock()
	return c.Save()
}

func (c *Config) ToggleUpdateCheck() error {
	c.mu.Lock()
	c.UpdateCheck.Enabled = !c.UpdateCheck.Enabled
	c.mu.Unlock()
	return c.Save()
}

// FilePath is the resolved config path.
func (c *Config) FilePath() string { return c.path }
