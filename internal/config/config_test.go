package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/model"
)

func TestDefaultShowsBothServicesAndShortestLimit(t *testing.T) {
	c := Default()
	for _, source := range []string{model.SourceClaudeCode, model.SourceCodex} {
		auto, got := c.LimitSelection(source)
		if !auto || len(got) != 1 || got[0] != model.Window5h {
			t.Fatalf("default %s limits = auto %v, %v", source, auto, got)
		}
	}
	if got := c.EnabledDisplaySources(); len(got) != 2 ||
		got[0] != model.SourceClaudeCode || got[1] != model.SourceCodex {
		t.Fatalf("default display sources = %v, want both services", got)
	}
	// Claude percentages come from the service; there is no budget to calibrate.
	if c.Sources.ClaudeCode.Weights.Output <= 0 {
		t.Fatal("token weighting is still needed for the tallies")
	}
}

func TestV1DefaultMigratesToShortestLimit(t *testing.T) {
	c := Default()
	c.Windows = []string{"5h", "weekly", "monthly"}
	if !c.migrate([]byte(`{"windows":["5h","weekly","monthly"]}`)) {
		t.Fatal("expected an unversioned config to migrate")
	}
	if auto, got := c.LimitSelection(model.SourceClaudeCode); !auto || len(got) != 1 || got[0] != model.Window5h {
		t.Fatalf("migrated Claude limits = auto %v, %v", auto, got)
	}
}

func TestV2MigratesFromFixed5hToAutomaticShortest(t *testing.T) {
	c := Default()
	c.AutoShortest = false
	if !c.migrate([]byte(`{"version":2,"windows":["5h"]}`)) {
		t.Fatal("expected v2 to migrate")
	}
	if auto, _ := c.LimitSelection(model.SourceCodex); !auto || c.Version != SchemaVersion {
		t.Fatalf("migrated config = auto %v version %d", auto, c.Version)
	}
}

func TestV4DefaultColorsMigrateToThemePalettes(t *testing.T) {
	c := Default()
	c.Colors.LegacyGood = "#3DDC64"
	c.Colors.LegacyWarn = "#FFC63D"
	c.Colors.LegacyCritical = "#FF4C4C"
	c.Colors.LegacyUnknown = "#8E8E93"
	c.Colors.LegacyLabel = "#8E8E93"
	c.Colors.LegacyTextOnFill = "#101010"
	if !c.migrate([]byte(`{"version":4}`)) {
		t.Fatal("expected v4 config to migrate")
	}
	if c.Version != SchemaVersion || c.Colors.Light.Good != "#15803D" || c.Colors.Dark.Good != "#4ADE80" {
		t.Fatalf("theme defaults not installed: %+v", c.Colors)
	}
}

func TestV4CustomColorsArePreservedForBothThemes(t *testing.T) {
	c := Default()
	c.Colors.LegacyGood = "#123456"
	c.Colors.LegacyWarn = "#654321"
	c.Colors.LegacyLabel = "#ABCDEF"
	c.migrate([]byte(`{"version":4}`))
	for name, theme := range map[string]ThemeColors{"light": c.Colors.Light, "dark": c.Colors.Dark} {
		if theme.Good != "#123456" || theme.Warn != "#654321" || theme.Period != "#ABCDEF" {
			t.Errorf("%s theme did not preserve custom colors: %+v", name, theme)
		}
	}
}

func TestV5MigratesToLocalizedNotifications(t *testing.T) {
	c := Default()
	if !c.migrate([]byte(`{"version":5}`)) {
		t.Fatal("expected v5 migration")
	}
	c.normalise()
	if c.Version != SchemaVersion || c.Language != "auto" {
		t.Fatalf("migration = version %d language %q", c.Version, c.Language)
	}
	settings := c.BankedResetNotifications()
	if !settings.Enabled || len(settings.ThresholdHours) != 2 || settings.ThresholdHours[0] != 168 || settings.ThresholdHours[1] != 24 {
		t.Fatalf("notification defaults = %+v", settings)
	}
}

func TestV1CustomizedWindowsArePreserved(t *testing.T) {
	c := Default()
	c.Windows = []string{"weekly", "monthly"}
	c.migrate([]byte(`{"windows":["weekly","monthly"]}`))
	_, got := c.LimitSelection(model.SourceClaudeCode)
	if len(got) != 2 || got[0] != model.WindowWeekly || got[1] != model.WindowMonthly {
		t.Fatalf("custom windows changed during migration: %v", got)
	}
}

func TestLimitSelectionsAreIndependent(t *testing.T) {
	c := Default()
	c.path = filepath.Join(t.TempDir(), "config.json")
	if err := c.ToggleWindow(model.SourceClaudeCode, model.WindowWeekly); err != nil {
		t.Fatal(err)
	}
	claudeAuto, claude := c.LimitSelection(model.SourceClaudeCode)
	codexAuto, codex := c.LimitSelection(model.SourceCodex)
	if claudeAuto || len(claude) != 1 || claude[0] != model.WindowWeekly {
		t.Fatalf("Claude selection = auto %v, %v", claudeAuto, claude)
	}
	if !codexAuto || len(codex) != 1 || codex[0] != model.Window5h {
		t.Fatalf("Codex selection was changed: auto %v, %v", codexAuto, codex)
	}
}

func TestToggleDisplaySourcePersistsAndKeepsOne(t *testing.T) {
	c := Default()
	c.path = filepath.Join(t.TempDir(), "config.json")
	if err := c.ToggleDisplaySource(model.SourceClaudeCode); err != nil {
		t.Fatal(err)
	}
	if got := c.EnabledDisplaySources(); len(got) != 1 || got[0] != model.SourceCodex {
		t.Fatalf("sources after disabling Claude = %v", got)
	}
	if err := c.ToggleDisplaySource(model.SourceCodex); err != nil {
		t.Fatal(err)
	}
	if got := c.EnabledDisplaySources(); len(got) != 1 || got[0] != model.SourceCodex {
		t.Fatalf("last source should stay selected, got %v", got)
	}
}

// A hand-edited file can be valid JSON with a wrong type somewhere. Unmarshal
// applies everything it parsed before failing and normalise() never runs on
// that path, so Load must not hand back the half-applied struct: a zero refresh
// interval reaches time.NewTicker and takes the whole app down.
func TestMalformedConfigStillYieldsUsableValues(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	broken := `{"version":5,"refreshSeconds":0,"icon":{"pixelScale":0},"displayMode":123}`
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err == nil {
		t.Fatal("a malformed config should still be reported")
	}
	if cfg == nil {
		t.Fatal("a malformed config must not leave the caller without one")
	}
	if cfg.RefreshSeconds < 5 || cfg.Icon.PixelScale < 1 || !cfg.DisplayMode.Valid() {
		t.Fatalf("unusable values survived: refresh=%d scale=%d mode=%q",
			cfg.RefreshSeconds, cfg.Icon.PixelScale, cfg.DisplayMode)
	}
}

// The config path is unresolvable when the environment has no home directory.
// Losing persistence is acceptable; handing the caller a nil config is not.
func TestLoadWithoutAHomeStillReturnsAConfig(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	cfg, err := Load()
	if cfg == nil {
		t.Fatalf("Load returned no config (err=%v)", err)
	}
	if cfg.RefreshSeconds < 5 || !cfg.DisplayMode.Valid() {
		t.Fatalf("fallback config is not usable: %+v", cfg)
	}
}

// Saving must not look like somebody editing the file: a menu click that was
// mistaken for a hand edit reloads the config and rebuilds every provider,
// throwing away their incremental read state and usage-command throttle.
func TestOwnSaveIsNotAnExternalEdit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if err := cfg.SetDisplayMode(ModePercent); err != nil {
			t.Fatal(err)
		}
		if cfg.ExternallyModified() {
			t.Fatalf("our own save was reported as an external edit (iteration %d)", i)
		}
	}
}

func TestHandEditIsDetectedAndAcknowledged(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// Filesystem timestamps are coarse; make the edit unambiguously newer.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(cfg.FilePath(), later, later); err != nil {
		t.Fatal(err)
	}
	if !cfg.ExternallyModified() {
		t.Fatal("an edit made behind the app's back should be picked up")
	}
	cfg.MarkSeen()
	if cfg.ExternallyModified() {
		t.Fatal("MarkSeen should stop the same edit being reported again")
	}
}

func TestEditableSettingsPersistValidatedValues(t *testing.T) {
	c := Default()
	c.path = filepath.Join(t.TempDir(), "config.json")

	changes := map[string]string{
		"refreshSeconds":                                 "90",
		"icon.windowsLayout":                             "single",
		"history.intervalMinutes":                        "10",
		"history.retentionDays":                          "60",
		"updateCheck.intervalHours":                      "12",
		"notifications.bankedResetExpiry.thresholdHours": "336, 48, 12",
		"notifications.limitThresholds.percents":         "40, 10",
		"colors.dark.codex":                              "#123ABC",
	}
	for key, value := range changes {
		if err := c.SetEditableSetting(key, value); err != nil {
			t.Fatalf("SetEditableSetting(%q): %v", key, err)
		}
	}

	got := c.EditableSettings()
	if got.RefreshSeconds != 90 || got.Icon.WindowsLayout != "single" ||
		got.History.IntervalMinutes != 10 || got.History.RetentionDays != 60 ||
		got.UpdateCheck.IntervalHours != 12 || got.Colors.Dark.Codex != "#123ABC" {
		t.Fatalf("editable settings were not applied: %+v", got)
	}
	if want := []int{336, 48, 12}; !sameInts(got.Notifications.BankedResetExpiry.ThresholdHours, want) {
		t.Fatalf("expiry thresholds = %v, want %v", got.Notifications.BankedResetExpiry.ThresholdHours, want)
	}
	if _, err := os.Stat(c.path); err != nil {
		t.Fatalf("settings were not persisted: %v", err)
	}
}

func TestInvalidEditableSettingDoesNotMutateConfig(t *testing.T) {
	c := Default()
	c.path = filepath.Join(t.TempDir(), "config.json")
	before := c.RefreshSeconds
	if err := c.SetEditableSetting("refreshSeconds", "0"); err == nil {
		t.Fatal("invalid refresh interval was accepted")
	}
	if c.RefreshSeconds != before {
		t.Fatalf("invalid value changed refresh interval to %d", c.RefreshSeconds)
	}
	if err := c.SetEditableSetting("colors.light.good", "red"); err == nil {
		t.Fatal("non-hex color was accepted")
	}
}

func TestCodexProfilesCanBeEditedButNotAllRemoved(t *testing.T) {
	c := Default()
	c.path = filepath.Join(t.TempDir(), "config.json")
	if err := c.SetEditableSetting("codex.profiles.0.label", "Work"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditableSetting("codex.profiles.0.short", "WK"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddCodexProfile(); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditableSetting("codex.profiles.1.path", "/tmp/personal-codex"); err != nil {
		t.Fatal(err)
	}
	if err := c.MoveCodexProfile(1, -1); err != nil {
		t.Fatal(err)
	}
	profiles := c.EditableSettings().CodexProfiles
	if profiles[0].Path != "/tmp/personal-codex" || profiles[1].Label != "Work" {
		t.Fatalf("profiles were not reordered: %+v", profiles)
	}
	if err := c.RemoveCodexProfile(0); err != nil {
		t.Fatal(err)
	}
	profiles = c.EditableSettings().CodexProfiles
	if len(profiles) != 1 || profiles[0].Label != "Work" {
		t.Fatalf("profiles = %+v", profiles)
	}
	if err := c.RemoveCodexProfile(0); err == nil {
		t.Fatal("removed the final Codex profile")
	}
}

func TestCodexProfileMoveRejectsGoingPastAnEnd(t *testing.T) {
	c := Default()
	c.path = filepath.Join(t.TempDir(), "config.json")
	if err := c.MoveCodexProfile(0, -1); err == nil {
		t.Fatal("moved the first profile before the start")
	}
	if got := c.EditableSettings().CodexProfiles; len(got) != 1 || got[0].Path != "auto" {
		t.Fatalf("failed move changed profiles: %+v", got)
	}
}

func TestClaudeProfilesCanBeEditedReorderedAndRetainOne(t *testing.T) {
	c := Default()
	c.path = filepath.Join(t.TempDir(), "config.json")
	if err := c.SetEditableSetting("claude.profiles.0.label", "Work"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditableSetting("claude.profiles.0.short", "CW"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddClaudeProfile(); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditableSetting("claude.profiles.1.path", "~/.claude-personal"); err != nil {
		t.Fatal(err)
	}
	if err := c.MoveClaudeProfile(1, -1); err != nil {
		t.Fatal(err)
	}
	profiles := c.EditableSettings().ClaudeProfiles
	if profiles[0].Path != "~/.claude-personal" || profiles[1].Label != "Work" {
		t.Fatalf("Claude profiles were not reordered: %+v", profiles)
	}
	if err := c.RemoveClaudeProfile(0); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveClaudeProfile(0); err == nil {
		t.Fatal("removed the final Claude profile")
	}
}

func TestV7ConfigGainsDefaultClaudeProfile(t *testing.T) {
	c := Default()
	c.Sources.ClaudeCode.Profiles = nil
	if !c.migrate([]byte(`{"version":7}`)) {
		t.Fatal("expected v7 config to migrate")
	}
	if got := c.Sources.ClaudeCode.Profiles; len(got) != 1 || got[0].Path != "auto" {
		t.Fatalf("migrated Claude profiles = %+v", got)
	}
}

func sameInts(a, b []int) bool {
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
