package model

import (
	"strings"
	"testing"
	"time"
)

func src(id string, windows map[Window]WindowStatus) SourceStatus {
	return SourceStatus{ID: id, Name: id, Windows: windows}
}

func known(used float64) WindowStatus { return WindowStatus{Known: true, UsedPercent: used} }

func TestAggregateTakesTheMostConstrainedSource(t *testing.T) {
	s := Snapshot{Sources: []SourceStatus{
		src("claude-code", map[Window]WindowStatus{
			Window5h:     known(20),
			WindowWeekly: known(90),
		}),
		src("codex", map[Window]WindowStatus{
			Window5h:     known(75),
			WindowWeekly: known(10),
		}),
	}}
	s.Aggregate([]Window{Window5h, WindowWeekly})

	if got := s.Icon[Window5h]; got.UsedPercent != 75 {
		t.Errorf("5h used = %v, want codex's 75", got.UsedPercent)
	}
	if got := s.Icon[WindowWeekly]; got.UsedPercent != 90 {
		t.Errorf("weekly used = %v, want claude-code's 90", got.UsedPercent)
	}
}

func TestAggregateSourcesFiltersProviderFamilies(t *testing.T) {
	s := Snapshot{Sources: []SourceStatus{
		src("claude-code", map[Window]WindowStatus{Window5h: known(20)}),
		src("codex:work", map[Window]WindowStatus{Window5h: known(75)}),
	}}
	s.AggregateSources([]Window{Window5h}, []string{SourceClaudeCode})
	if got := s.Icon[Window5h].UsedPercent; got != 20 {
		t.Errorf("Claude-only aggregate = %v, want 20", got)
	}

	s.AggregateSources([]Window{Window5h}, []string{SourceCodex})
	if got := s.Icon[Window5h].UsedPercent; got != 75 {
		t.Errorf("Codex-family aggregate = %v, want 75", got)
	}
}

func TestAggregateIgnoresUnknownSources(t *testing.T) {
	s := Snapshot{Sources: []SourceStatus{
		src("a", map[Window]WindowStatus{Window5h: {Known: false, UsedPercent: 99}}),
		src("b", map[Window]WindowStatus{Window5h: known(30)}),
	}}
	s.Aggregate([]Window{Window5h})

	got := s.Icon[Window5h]
	if !got.Known || got.UsedPercent != 30 {
		t.Errorf("got %+v, want the known 30%% reading", got)
	}
}

func TestAggregateWithNoDataStaysUnknown(t *testing.T) {
	s := Snapshot{Sources: []SourceStatus{src("a", map[Window]WindowStatus{})}}
	s.Aggregate([]Window{Window5h, WindowMonthly})

	for _, w := range []Window{Window5h, WindowMonthly} {
		got, ok := s.Icon[w]
		if !ok {
			t.Fatalf("%s missing from the icon set", w)
		}
		if got.Known {
			t.Errorf("%s should be unknown, got %+v", w, got)
		}
		if got.Window != w {
			t.Errorf("window tag = %q, want %q", got.Window, w)
		}
	}
}

func TestRemainingPercentIsClamped(t *testing.T) {
	if got := (WindowStatus{UsedPercent: 130}).RemainingPercent(); got != 0 {
		t.Errorf("over-used remaining = %v, want 0", got)
	}
	if got := (WindowStatus{UsedPercent: -5}).RemainingPercent(); got != 100 {
		t.Errorf("negative usage remaining = %v, want 100", got)
	}
}

func TestFormatCount(t *testing.T) {
	cases := map[int64]string{
		0: "0", 999: "999", 1500: "1.5K", 1_260_000: "1.3M", 2_000_000_000: "2.0B",
	}
	for in, want := range cases {
		if got := FormatCount(in); got != want {
			t.Errorf("FormatCount(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatReset(t *testing.T) {
	now := time.Date(2026, 8, 2, 14, 0, 0, 0, time.Local)
	if got := FormatReset(time.Time{}, now); got != "" {
		t.Errorf("zero time should render nothing, got %q", got)
	}
	if got := FormatReset(now.Add(-time.Minute), now); got != "resetting now" {
		t.Errorf("past reset = %q", got)
	}
	// Resets within a day carry the wall-clock time, which is what you need to
	// decide whether to wait.
	if got := FormatReset(now.Add(90*time.Minute), now); !strings.Contains(got, "15:30") {
		t.Errorf("same-day reset %q should include the clock time", got)
	}
	if got := FormatReset(now.Add(40*24*time.Hour), now); !strings.HasPrefix(got, "resets Sep") {
		t.Errorf("distant reset = %q, want a date", got)
	}
}

func TestParseWindow(t *testing.T) {
	if w, ok := ParseWindow("weekly"); !ok || w != WindowWeekly {
		t.Errorf("got %q/%v", w, ok)
	}
	if _, ok := ParseWindow("daily"); ok {
		t.Error("unknown windows must be rejected")
	}
}
