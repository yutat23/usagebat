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
	if auto, _ := c.LimitSelection(model.SourceCodex); !auto || c.Version != 4 {
		t.Fatalf("migrated config = auto %v version %d", auto, c.Version)
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
