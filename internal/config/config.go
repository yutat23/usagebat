// Package config loads, defaults and persists the user configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	DisplayMode    DisplayMode             `json:"displayMode"`
	DisplaySources []string                `json:"displaySources"`
	DisplayLimits  map[string]LimitDisplay `json:"displayLimits"`
	// AutoShortest and Windows are retained only to migrate v1-v3 configs.
	AutoShortest   bool     `json:"autoShortest,omitempty"`
	Windows        []string `json:"windows,omitempty"`
	RefreshSeconds int      `json:"refreshSeconds"`
	Icon           Icon     `json:"icon"`
	Colors         Colors   `json:"colors"`
	Sources        Sources  `json:"sources"`

	path string
	mu   sync.Mutex
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
	Good          string  `json:"good"`
	Warn          string  `json:"warn"`
	Critical      string  `json:"critical"`
	Unknown       string  `json:"unknown"`
	Label         string  `json:"label"`
	TextOnFill    string  `json:"textOnFill"`
	WarnBelow     float64 `json:"warnBelow"`
	CriticalBelow float64 `json:"criticalBelow"`
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
		Version:        4,
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
			Good:          "#3DDC64",
			Warn:          "#FFC63D",
			Critical:      "#FF4C4C",
			Unknown:       "#8E8E93",
			Label:         "#8E8E93",
			TextOnFill:    "#101010",
			WarnBelow:     50,
			CriticalBelow: 20,
		},
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
	path, err := Path()
	if err != nil {
		return nil, err
	}
	cfg := Default()
	cfg.path = path

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, cfg.Save()
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	cfg.path = path
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
// V4 makes the period selection independent per service and enlarges the
// default macOS artwork. Older global selections are copied to both services.
func (c *Config) migrate(data []byte) bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return false
	}
	version := 0
	if encoded, ok := raw["version"]; ok {
		_ = json.Unmarshal(encoded, &version)
	}
	if version >= 4 {
		return false
	}
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
	c.Version = 4
	return true
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
	if c.Colors.Good == "" {
		c.Colors = d.Colors
	}
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
	return os.Rename(tmp, c.path)
}

// FilePath is the resolved config path.
func (c *Config) FilePath() string { return c.path }
