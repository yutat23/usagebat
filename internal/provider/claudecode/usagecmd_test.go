package claudecode

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/model"
)

// sampleOutput reproduces the layout of `claude -p /usage --output-format json`
// on a subscription account. The wording and punctuation are what the parser
// keys off, so they are kept verbatim; the figures are made up.
const sampleOutput = `You are currently using your subscription to power your Claude Code usage

Current session: 51% used · resets Aug 2 at 7:20pm (Asia/Tokyo)
Current week (all models): 6% used · resets Aug 6 at 9am (Asia/Tokyo)

What's contributing to your limits usage?
Approximate, based on local sessions on this machine — does not include other devices or claude.ai. Behaviors are independent characteristics, not a breakdown.

Last 24h · 120 requests · 2 sessions
  57% of your usage was at >150k context

Last 7d · 480 requests · 9 sessions
  39% of your usage was at >150k context`

func TestParseUsageOutput(t *testing.T) {
	now := time.Date(2026, 8, 2, 15, 0, 0, 0, time.Local)
	got := parseUsage(sampleOutput, now)

	if len(got) != 2 {
		t.Fatalf("parsed %d windows, want 2: %+v", len(got), got)
	}
	session := got[model.Window5h]
	if !session.Known || session.UsedPercent != 51 {
		t.Errorf("session = %+v, want 51%% used", session)
	}
	if session.Estimated {
		t.Error("a reported figure must not be flagged as an estimate")
	}
	week := got[model.WindowWeekly]
	if !week.Known || week.UsedPercent != 6 {
		t.Errorf("weekly = %+v, want 6%% used", week)
	}
	if _, ok := got[model.WindowMonthly]; ok {
		t.Error("monthly is not reported here and must not be invented")
	}

	// "Last 24h · 100 requests" and the "57% of your usage" lines must not be
	// mistaken for limits.
	tz, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	wantReset := time.Date(2026, 8, 2, 19, 20, 0, 0, tz)
	if !session.ResetsAt.Equal(wantReset) {
		t.Errorf("session reset = %v, want %v", session.ResetsAt, wantReset)
	}
}

func TestParseUsageIgnoresUnrelatedPercentages(t *testing.T) {
	text := "Last 24h · 120 requests · 2 sessions\n  57% of your usage was at >150k context"
	if got := parseUsage(text, time.Now()); len(got) != 0 {
		t.Errorf("got %+v, want nothing recognised", got)
	}
}

func TestParseUsageUnknownFormatYieldsNothing(t *testing.T) {
	// Better to fall back to estimation than to guess at an unfamiliar layout.
	if got := parseUsage("Usage: 51 percent of session consumed", time.Now()); len(got) != 0 {
		t.Errorf("got %+v, want nothing recognised", got)
	}
}

func TestParseUsageTakesTheFullestBucketPerWindow(t *testing.T) {
	text := "Current week (all models): 6% used\nCurrent week (Opus): 42% used"
	got := parseUsage(text, time.Now())
	if w := got[model.WindowWeekly]; w.UsedPercent != 42 {
		t.Errorf("weekly = %v, want the binding 42", w.UsedPercent)
	}
}

func TestParseResetTime(t *testing.T) {
	now := time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skip("tzdata unavailable")
	}

	cases := []struct {
		in   string
		want time.Time
	}{
		{"Aug 2 at 7:20pm (Asia/Tokyo)", time.Date(2026, 8, 2, 19, 20, 0, 0, tokyo)},
		{"Aug 6 at 9am (Asia/Tokyo)", time.Date(2026, 8, 6, 9, 0, 0, 0, tokyo)},
		{"Aug 6 at 12am (Asia/Tokyo)", time.Date(2026, 8, 6, 0, 0, 0, 0, tokyo)},
		{"Aug 6 at 12pm (Asia/Tokyo)", time.Date(2026, 8, 6, 12, 0, 0, 0, tokyo)},
		// No zone: fall back to the local zone of `now`.
		{"Aug 6 at 9:30am", time.Date(2026, 8, 6, 9, 30, 0, 0, time.UTC)},
		// A month already behind us belongs to next year.
		{"Jan 3 at 9am", time.Date(2027, 1, 3, 9, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		got := parseResetTime(c.in, now)
		if !got.Equal(c.want) {
			t.Errorf("parseResetTime(%q) = %v, want %v", c.in, got, c.want)
		}
	}

	for _, bad := range []string{"", "soon", "next Tuesday", "Foo 2 at 7pm"} {
		if got := parseResetTime(bad, now); !got.IsZero() {
			t.Errorf("parseResetTime(%q) = %v, want zero", bad, got)
		}
	}
}

// providerWithReported builds a provider holding a cached /usage reading and
// throttled so Collect will not shell out.
func providerWithReported(t *testing.T, dir string, now time.Time, reported map[model.Window]model.WindowStatus) *Provider {
	t.Helper()
	c := config.Default().Sources.ClaudeCode
	c.ProjectsDir = dir
	c.UsageCacheFile = filepath.Join(dir, ".claude.json")
	c.UsageCommand.MinIntervalSeconds = 3600
	p := New(&c)
	p.lastAttempt = now
	p.reported = reported
	p.reportedAt = now
	return p
}

func TestReportedFiguresBeatEstimates(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	p := providerWithReported(t, dir, now, map[model.Window]model.WindowStatus{
		model.Window5h: {Window: model.Window5h, Known: true, UsedPercent: 51},
	})
	st := p.Collect(now)

	got := st.Windows[model.Window5h]
	if got.UsedPercent != 51 || got.Estimated {
		t.Errorf("5h = %+v, want the reported 51%% unflagged", got)
	}
	// Claude has no monthly subscription window, so one must not be invented.
	if _, ok := st.Windows[model.WindowMonthly]; ok {
		t.Errorf("monthly must be absent when /usage does not report it")
	}
	// Token tallies are real data and stay available for every window.
	if st.Tokens[model.Window5h].Output != 100 {
		t.Errorf("token tally lost: %+v", st.Tokens[model.Window5h])
	}
}

func TestStaleReportedReadingIsDiscarded(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	p := providerWithReported(t, dir, now, map[model.Window]model.WindowStatus{
		model.Window5h: {Window: model.Window5h, Known: true, UsedPercent: 51},
	})
	// Age the reading past the staleness limit.
	p.reportedAt = now.Add(-time.Duration(p.cfg.UsageCommand.StaleAfterSeconds+1) * time.Second)

	st := p.Collect(now)
	if got := st.Windows[model.Window5h]; !got.Estimated {
		t.Errorf("5h = %+v, want the stale reading dropped in favour of an estimate", got)
	}
}

func TestUsageCommandDisabledSkipsExec(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	c := config.Default().Sources.ClaudeCode
	c.ProjectsDir = dir
	c.UsageCacheFile = filepath.Join(dir, ".claude.json")
	c.UsageCommand.Enabled = false
	// Point at a binary that does not exist: if it were run, this would fail.
	c.UsageCommand.Path = filepath.Join(dir, "no-such-claude")

	p := New(&c)
	st := p.Collect(now)
	if !strings.Contains(st.Note, "usage cache unavailable") {
		t.Errorf("note = %q, want the missing cache explained", st.Note)
	}
}

func TestMissingBinaryFallsBackToEstimation(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	c := config.Default().Sources.ClaudeCode
	c.ProjectsDir = dir
	c.UsageCacheFile = filepath.Join(dir, ".claude.json")
	c.UsageCommand.Path = filepath.Join(dir, "definitely-not-here")

	st := New(&c).Collect(now)
	if got := st.Windows[model.Window5h]; !got.Estimated || !got.Known {
		t.Errorf("5h = %+v, want a usable estimate", got)
	}
	// The reason has to surface, or the user cannot tell a real figure from a
	// guess.
	if st.Note == "" || st.Note == "reported by /usage" {
		t.Errorf("note = %q, want it to explain the fallback", st.Note)
	}
}

func TestResolveBinaryRejectsNonExecutablePath(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveBinary(filepath.Join(dir, "nope")); err == nil {
		t.Error("expected an error for a missing explicit path")
	}
}
