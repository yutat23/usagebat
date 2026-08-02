package config

import (
	"path/filepath"
	"testing"

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
	if _, ok := c.Sources.ClaudeCode.Limits[string(model.WindowMonthly)]; ok {
		t.Fatal("Claude must not have a fabricated monthly limit by default")
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
	if auto, _ := c.LimitSelection(model.SourceCodex); !auto || c.Version != 5 {
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
	if c.Version != 5 || c.Colors.Light.Good != "#15803D" || c.Colors.Dark.Good != "#4ADE80" {
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
