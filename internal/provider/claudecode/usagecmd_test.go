package claudecode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/model"
)

func TestClaudeConfigDirReplacesInheritedValue(t *testing.T) {
	got := withClaudeConfigDir([]string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/old", "LANG=C"}, "/new")
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "CLAUDE_CONFIG_DIR=/old") || !strings.Contains(joined, "CLAUDE_CONFIG_DIR=/new") {
		t.Fatalf("environment = %v", got)
	}
}

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

func TestDecodeAndParseRealUsageJSONWithZeroPercent(t *testing.T) {
	raw := []byte(`{"is_error":false,"num_turns":0,"result":"You are currently using your subscription to power your Claude Code usage\n\nCurrent session: 0% used · resets Aug 9 at 3am (Asia/Tokyo)\nCurrent week (all models): 5% used · resets Aug 13 at 9am (Asia/Tokyo)"}`)
	text, err := decodeUsageJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := parseUsage(text, time.Date(2026, 8, 9, 0, 0, 0, 0, time.Local))
	if session := got[model.Window5h]; !session.Known || session.UsedPercent != 0 {
		t.Fatalf("session = %+v, want known 0%% used", session)
	}
	if week := got[model.WindowWeekly]; !week.Known || week.UsedPercent != 5 {
		t.Fatalf("week = %+v, want known 5%% used", week)
	}
}

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
	// Better to report nothing than to guess at an unfamiliar layout.
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

func TestReportedFiguresAreShownUnflagged(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	p := providerWithReported(t, dir, now, map[model.Window]model.WindowStatus{
		model.Window5h: {Window: model.Window5h, Known: true, UsedPercent: 51},
	})
	st := p.Collect(now)

	got := st.Windows[model.Window5h]
	if got.UsedPercent != 51 {
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

// writeUsageCache plants the file Claude Code writes after it talks to the
// service, which is what a scheduled refresh reads.
func writeUsageCache(t *testing.T, path string, percent float64, fetched time.Time) {
	t.Helper()
	body := fmt.Sprintf(`{"cachedUsageUtilization":{"fetchedAtMs":%d,`+
		`"utilization":{"five_hour":{"utilization":%v,"resets_at":%q}}}}`,
		fetched.UnixMilli(), percent, fetched.Add(time.Hour).Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A scheduled refresh reads the cache Claude Code left behind, which costs
// nothing but can be minutes old.
func TestScheduledRefreshUsesTheCache(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeUsageCache(t, filepath.Join(dir, ".claude.json"), 20, now.Add(-time.Minute))

	p := providerWithReported(t, dir, now, map[model.Window]model.WindowStatus{
		model.Window5h: {Window: model.Window5h, Known: true, UsedPercent: 51},
	})
	st := p.Collect(now)

	if got := st.Windows[model.Window5h].UsedPercent; got != 20 {
		t.Errorf("5h = %v%%, want the cached 20%%", got)
	}
	if !strings.Contains(st.Note, "Claude usage cache") {
		t.Errorf("note = %q, want the cache named", st.Note)
	}
}

// A refresh the user asked for goes to /usage instead, because the cache is
// only as fresh as the last time Claude Code itself ran.
func TestRequestedRefreshPrefersUsageOverTheCache(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeUsageCache(t, filepath.Join(dir, ".claude.json"), 20, now.Add(-time.Minute))

	p := providerWithReported(t, dir, now, map[model.Window]model.WindowStatus{
		model.Window5h: {Window: model.Window5h, Known: true, UsedPercent: 51},
	})
	p.RequestAuthoritative()
	st := p.Collect(now)

	if got := st.Windows[model.Window5h].UsedPercent; got != 51 {
		t.Errorf("5h = %v%%, want the /usage reading of 51%%", got)
	}
	if !strings.Contains(st.Note, "/usage") {
		t.Errorf("note = %q, want /usage named as the source", st.Note)
	}

	// The request is spent: the next scheduled refresh is cheap again.
	if got := p.Collect(now).Windows[model.Window5h].UsedPercent; got != 20 {
		t.Errorf("5h = %v%% on the next refresh, want the cache back at 20%%", got)
	}
}

// When the CLI cannot be run, an authoritative request must not lose the
// reading that is available.
func TestRequestedRefreshFallsBackToTheCache(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeUsageCache(t, filepath.Join(dir, ".claude.json"), 20, now.Add(-time.Minute))

	c := config.Default().Sources.ClaudeCode
	c.ProjectsDir = dir
	c.UsageCacheFile = filepath.Join(dir, ".claude.json")
	c.UsageCommand.Path = filepath.Join(dir, "no-such-claude")
	p := New(&c)
	p.RequestAuthoritative()

	st := p.Collect(now)
	if got := st.Windows[model.Window5h].UsedPercent; got != 20 {
		t.Errorf("5h = %v%%, want the cache used when /usage cannot run", got)
	}
}

func TestStaleActiveCacheFallsBackWhenUsageFails(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fetched := now.Add(-17 * time.Minute)
	writeUsageCache(t, filepath.Join(dir, ".claude.json"), 20, fetched)

	c := config.Default().Sources.ClaudeCode
	c.ProjectsDir = dir
	c.UsageCacheFile = filepath.Join(dir, ".claude.json")
	c.UsageCommand.Path = filepath.Join(dir, "no-such-claude")
	p := New(&c)

	st := p.Collect(now)
	if got := st.Windows[model.Window5h]; !got.Known || got.UsedPercent != 20 {
		t.Fatalf("5h = %+v, want stale but active cache value", got)
	}
	if !strings.Contains(st.Note, "17m0s old") || !strings.Contains(st.Note, "/usage") {
		t.Fatalf("note = %q, want cache age and /usage failure", st.Note)
	}
}

func TestStaleCachePastItsResetIsNotUsed(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeUsageCache(t, filepath.Join(dir, ".claude.json"), 20, now.Add(-2*time.Hour))

	c := config.Default().Sources.ClaudeCode
	c.ProjectsDir = dir
	c.UsageCacheFile = filepath.Join(dir, ".claude.json")
	c.UsageCommand.Path = filepath.Join(dir, "no-such-claude")
	st := New(&c).Collect(now)
	if got, ok := st.Windows[model.Window5h]; ok {
		t.Fatalf("5h = %+v, want expired cache omitted", got)
	}
}

func TestPassedResetRefreshesEvenAYoungCache(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	body := fmt.Sprintf(`{"cachedUsageUtilization":{"fetchedAtMs":%d,"utilization":{`+
		`"five_hour":{"utilization":20,"resets_at":%q},`+
		`"seven_day":{"utilization":5,"resets_at":%q}}}}`,
		now.Add(-2*time.Minute).UnixMilli(), now.Add(-time.Minute).Format(time.RFC3339),
		now.Add(24*time.Hour).Format(time.RFC3339))
	cachePath := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(cachePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	c := config.Default().Sources.ClaudeCode
	c.ProjectsDir = dir
	c.UsageCacheFile = cachePath
	p := New(&c)
	p.lastAttempt = now // Keep the test from starting a real subprocess.
	p.reportedAt = now
	p.reported = map[model.Window]model.WindowStatus{
		model.Window5h: {Window: model.Window5h, Known: true, UsedPercent: 1},
	}

	st := p.Collect(now)
	if got := st.Windows[model.Window5h]; !got.Known || got.UsedPercent != 1 {
		t.Fatalf("5h = %+v, want /usage reading after cached reset", got)
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
	if got, ok := st.Windows[model.Window5h]; ok {
		t.Errorf("5h = %+v, want the stale reading dropped rather than shown", got)
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
	if !strings.Contains(st.Note, "usage cache") {
		t.Errorf("note = %q, want the missing cache explained", st.Note)
	}
}

// With no cache and no CLI there is nothing to report, and the app says so
// rather than filling the gap with a number it made up.
func TestMissingBinaryLeavesTheWindowsUnknown(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	c := config.Default().Sources.ClaudeCode
	c.ProjectsDir = dir
	c.UsageCacheFile = filepath.Join(dir, ".claude.json")
	c.UsageCommand.Path = filepath.Join(dir, "definitely-not-here")

	st := New(&c).Collect(now)
	if got, ok := st.Windows[model.Window5h]; ok {
		t.Errorf("5h = %+v, want no figure at all", got)
	}
	// The reason has to surface, and the measured tallies still stand.
	if !strings.Contains(st.Note, "no figures") {
		t.Errorf("note = %q, want it to explain that nothing was reported", st.Note)
	}
	if st.Tokens[model.Window5h].Output != 100 {
		t.Errorf("token tally lost: %+v", st.Tokens[model.Window5h])
	}
}

func TestResolveBinaryRejectsNonExecutablePath(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveBinary(filepath.Join(dir, "nope")); err == nil {
		t.Error("expected an error for a missing explicit path")
	}
}
