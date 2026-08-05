package notify

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/i18n"
	"github.com/yutat23/usagebat/internal/model"
)

func limitSettings(percents ...int) config.LimitThresholds {
	return config.LimitThresholds{Enabled: true, Percents: percents}
}

func gauge(remaining float64, resets time.Time) Gauge {
	return Gauge{
		Source: model.SourceClaudeCode,
		Name:   "Claude Code",
		Window: model.Window5h,
		Status: model.WindowStatus{
			Window: model.Window5h, Known: true,
			UsedPercent: 100 - remaining, ResetsAt: resets,
		},
	}
}

func TestLimitWarningFiresOncePerThreshold(t *testing.T) {
	e := newAt(filepath.Join(t.TempDir(), "state.json"))
	p := i18n.New("en")
	now := time.Now()
	resets := now.Add(2 * time.Hour)

	if got := e.DueLimits([]Gauge{gauge(80, resets)}, limitSettings(50, 20), p, now); len(got) != 0 {
		t.Fatalf("warned at 80%% remaining: %+v", got)
	}

	events := e.DueLimits([]Gauge{gauge(45, resets)}, limitSettings(50, 20), p, now)
	if len(events) != 1 {
		t.Fatalf("got %d events crossing 50%%, want 1", len(events))
	}
	if err := e.Mark(events[0], now); err != nil {
		t.Fatal(err)
	}

	// Still under 50 but nothing new has been crossed.
	if got := e.DueLimits([]Gauge{gauge(40, resets)}, limitSettings(50, 20), p, now); len(got) != 0 {
		t.Fatalf("warned again for a threshold already sent: %+v", got)
	}

	// Now past the second mark.
	events = e.DueLimits([]Gauge{gauge(15, resets)}, limitSettings(50, 20), p, now)
	if len(events) != 1 {
		t.Fatalf("got %d events crossing 20%%, want 1", len(events))
	}
	if err := e.Mark(events[0], now); err != nil {
		t.Fatal(err)
	}
	if got := e.DueLimits([]Gauge{gauge(5, resets)}, limitSettings(50, 20), p, now); len(got) != 0 {
		t.Fatalf("warned a third time: %+v", got)
	}
}

// A 5h window resets several times a day. Without the reset time in the key it
// would warn once and then stay silent for good.
func TestLimitWarningRearmsAfterAReset(t *testing.T) {
	e := newAt(filepath.Join(t.TempDir(), "state.json"))
	p := i18n.New("en")
	now := time.Now()

	first := now.Add(time.Hour)
	events := e.DueLimits([]Gauge{gauge(15, first)}, limitSettings(20), p, now)
	if len(events) != 1 {
		t.Fatalf("got %d events, want the first warning", len(events))
	}
	if err := e.Mark(events[0], now); err != nil {
		t.Fatal(err)
	}

	// The window rolled over: a new reset time, and headroom is low again.
	second := now.Add(6 * time.Hour)
	events = e.DueLimits([]Gauge{gauge(15, second)}, limitSettings(20), p, now.Add(5*time.Hour))
	if len(events) != 1 {
		t.Fatalf("got %d events after the reset, want a fresh warning", len(events))
	}
}

// Passing several marks between two refreshes should say the most urgent thing
// once, not queue one notification per mark.
func TestSeveralThresholdsAtOnceSendOneWarning(t *testing.T) {
	e := newAt(filepath.Join(t.TempDir(), "state.json"))
	p := i18n.New("en")
	now := time.Now()

	events := e.DueLimits([]Gauge{gauge(5, now.Add(time.Hour))}, limitSettings(50, 20), p, now)
	if len(events) != 1 {
		t.Fatalf("got %d events, want one covering both marks", len(events))
	}
	if err := e.Mark(events[0], now); err != nil {
		t.Fatal(err)
	}
	// Both marks were recorded, so neither fires again in this window.
	if got := e.DueLimits([]Gauge{gauge(3, now.Add(time.Hour))}, limitSettings(50, 20), p, now); len(got) != 0 {
		t.Fatalf("a recorded threshold fired again: %+v", got)
	}
}

func TestLimitWarningsRespectTheSetting(t *testing.T) {
	e := newAt(filepath.Join(t.TempDir(), "state.json"))
	p := i18n.New("en")
	now := time.Now()
	low := []Gauge{gauge(5, now.Add(time.Hour))}

	off := config.LimitThresholds{Enabled: false, Percents: []int{50, 20}}
	if got := e.DueLimits(low, off, p, now); len(got) != 0 {
		t.Errorf("warned while disabled: %+v", got)
	}
	empty := config.LimitThresholds{Enabled: true}
	if got := e.DueLimits(low, empty, p, now); len(got) != 0 {
		t.Errorf("warned with no thresholds configured: %+v", got)
	}
}

// A gauge the service says nothing about has no headroom to be low.
func TestUnknownGaugesAreSkipped(t *testing.T) {
	e := newAt(filepath.Join(t.TempDir(), "state.json"))
	unknown := Gauge{
		Source: model.SourceClaudeCode, Name: "Claude Code", Window: model.Window5h,
		Status: model.WindowStatus{Window: model.Window5h, Known: false},
	}
	if got := e.DueLimits([]Gauge{unknown}, limitSettings(50), i18n.New("en"), time.Now()); len(got) != 0 {
		t.Fatalf("warned about an unknown gauge: %+v", got)
	}
}
