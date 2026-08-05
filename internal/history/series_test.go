package history

import (
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/model"
)

var claude5h = Series{Source: model.SourceClaudeCode, Window: model.Window5h}

func sample(at time.Time, remaining float64, tokens int64) Sample {
	entry := Entry{Source: claude5h.Source, Window: claude5h.Window, Remaining: remaining}
	if tokens >= 0 {
		entry.Tokens = &Tokens{Input: tokens}
	}
	return Sample{At: at.Unix(), Entries: []Entry{entry}}
}

func TestRemainingFollowsTheSeries(t *testing.T) {
	start := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	samples := []Sample{
		sample(start, 100, 0),
		sample(start.Add(time.Hour), 60, 5000),
		{At: start.Add(2 * time.Hour).Unix(), Entries: []Entry{
			{Source: model.SourceCodex, Window: model.Window5h, Remaining: 10},
		}},
	}

	points := Remaining(samples, claude5h)
	if len(points) != 2 {
		t.Fatalf("got %d points, want only the Claude ones: %+v", len(points), points)
	}
	if points[1].Value != 60 {
		t.Errorf("second point = %v, want 60", points[1].Value)
	}
}

// Token totals restart when the window rolls over. Subtracting across that
// boundary would plot a large negative spike.
func TestTokenUsageHandlesAWindowReset(t *testing.T) {
	start := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	samples := []Sample{
		sample(start, 100, 1000),
		sample(start.Add(time.Hour), 70, 4000),
		sample(start.Add(2*time.Hour), 100, 500), // reset: totals restarted
		sample(start.Add(3*time.Hour), 90, 900),
	}

	points := TokenUsage(samples, claude5h)
	want := []float64{3000, 500, 400}
	if len(points) != len(want) {
		t.Fatalf("got %d points, want %d: %+v", len(points), len(want), points)
	}
	for i, value := range want {
		if points[i].Value != value {
			t.Errorf("point %d = %v, want %v", i, points[i].Value, value)
		}
	}
}

// The first sample has nothing to subtract from; reporting its running total
// as consumption would invent a spike at the left edge of every chart.
func TestTokenUsageSkipsTheFirstSample(t *testing.T) {
	start := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	points := TokenUsage([]Sample{sample(start, 100, 50000)}, claude5h)
	if len(points) != 0 {
		t.Fatalf("a lone sample cannot describe consumption: %+v", points)
	}
}

func TestActivityBucketsConsumptionByLocalHour(t *testing.T) {
	loc := time.UTC
	monday := time.Date(2026, 8, 3, 9, 0, 0, 0, loc) // a Monday
	samples := []Sample{
		sample(monday, 100, -1),
		sample(monday.Add(30*time.Minute), 90, -1),   // 10 points, still 09:00
		sample(monday.Add(90*time.Minute), 75, -1),   // 15 points at 10:30
		sample(monday.Add(150*time.Minute), 100, -1), // reset, not usage
	}

	heat := Activity(samples, claude5h, loc)
	if got := heat[time.Monday][9]; got != 10 {
		t.Errorf("Monday 09:00 = %v, want 10", got)
	}
	if got := heat[time.Monday][10]; got != 15 {
		t.Errorf("Monday 10:00 = %v, want 15", got)
	}
	if got := heat[time.Monday][11]; got != 0 {
		t.Errorf("a reset must not count as usage, got %v", got)
	}
	if heat.Max() != 15 {
		t.Errorf("Max() = %v, want 15", heat.Max())
	}
}

func TestActivityIsEmptyWithoutConsumption(t *testing.T) {
	start := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	heat := Activity([]Sample{sample(start, 100, -1)}, claude5h, time.UTC)
	if heat.Max() != 0 {
		t.Errorf("Max() = %v, want 0", heat.Max())
	}
}

func TestAvailableListsSeriesInDisplayOrder(t *testing.T) {
	start := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	samples := []Sample{{At: start.Unix(), Entries: []Entry{
		{Source: model.SourceCodex, Window: model.WindowMonthly},
		{Source: model.SourceClaudeCode, Window: model.WindowWeekly},
		{Source: model.SourceClaudeCode, Window: model.Window5h},
		{Source: model.SourceCodex, Window: model.Window5h},
		{Source: model.SourceClaudeCode, Window: model.Window5h}, // duplicate
	}}}

	got := Available(samples)
	want := []Series{
		{model.SourceClaudeCode, model.Window5h},
		{model.SourceClaudeCode, model.WindowWeekly},
		{model.SourceCodex, model.Window5h},
		{model.SourceCodex, model.WindowMonthly},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
}
