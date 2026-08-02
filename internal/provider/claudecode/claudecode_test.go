package claudecode

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yutat23/usage-battery/internal/config"
	"github.com/yutat23/usage-battery/internal/model"
)

// assistantLine builds the transcript shape Claude Code writes.
func assistantLine(ts time.Time, id, reqID, modelName string, in, out, cacheCreate, cacheRead int64) string {
	return fmt.Sprintf(
		`{"type":"assistant","timestamp":%q,"requestId":%q,"message":{"id":%q,"model":%q,`+
			`"usage":{"input_tokens":%d,"output_tokens":%d,`+
			`"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d}}}`,
		ts.UTC().Format(time.RFC3339), reqID, id, modelName, in, out, cacheCreate, cacheRead)
}

func writeTranscript(t *testing.T, dir, name string, lines ...string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

// testConfig exercises the estimator alone. The /usage path is switched off so
// tests never shell out to the real CLI.
func testConfig(dir string, limits map[string]int64) *config.ClaudeCode {
	c := config.Default().Sources.ClaudeCode
	c.ProjectsDir = dir
	c.UsageCacheFile = filepath.Join(dir, ".claude.json")
	c.UsageCommand.Enabled = false
	if limits != nil {
		c.Limits = limits
	}
	return &c
}

func TestWeightingUsesModelAndTokenKind(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	// 1000 in + 100 out + 1000 cache-create + 10000 cache-read, on sonnet (x1):
	// 1000 + 5*100 + 1.25*1000 + 0.1*10000 = 3750
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 1000, 100, 1000, 10000))

	st := New(testConfig(dir, nil)).Collect(now)
	tok := st.Tokens[model.Window5h]
	if got := tok.Weighted; got != 3750 {
		t.Errorf("weighted = %v, want 3750", got)
	}
	if tok.Input != 1000 || tok.Output != 100 || tok.CacheCreation != 1000 || tok.CacheRead != 10000 {
		t.Errorf("raw tallies wrong: %+v", tok)
	}

	// The same response on opus carries a 5x model weight.
	dir2 := t.TempDir()
	writeTranscript(t, filepath.Join(dir2, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-opus-5", 1000, 100, 1000, 10000))
	st2 := New(testConfig(dir2, nil)).Collect(now)
	if got := st2.Tokens[model.Window5h].Weighted; got != 3750*5 {
		t.Errorf("opus weighted = %v, want %v", got, 3750*5)
	}
}

func TestDeduplicatesResponsesAcrossTranscripts(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	line := assistantLine(now.Add(-time.Minute), "msg_1", "req_1", "claude-sonnet-5", 0, 100, 0, 0)
	// A resumed session copies earlier responses into the new transcript.
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl", line)
	writeTranscript(t, filepath.Join(dir, "proj"), "b.jsonl", line)

	st := New(testConfig(dir, nil)).Collect(now)
	if got := st.Tokens[model.Window5h].Output; got != 100 {
		t.Errorf("output = %d, want 100 counted once", got)
	}
}

func TestIncrementalReadDoesNotDoubleCount(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	path := writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-2*time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	p := New(testConfig(dir, nil))
	if got := p.Collect(now).Tokens[model.Window5h].Output; got != 100 {
		t.Fatalf("first pass output = %d, want 100", got)
	}
	// A second refresh with no new bytes must not re-count the file.
	if got := p.Collect(now).Tokens[model.Window5h].Output; got != 100 {
		t.Errorf("unchanged file re-counted: output = %d, want 100", got)
	}

	appendLines(t, path,
		assistantLine(now.Add(-time.Minute), "m2", "r2", "claude-sonnet-5", 0, 50, 0, 0))
	if got := p.Collect(now).Tokens[model.Window5h].Output; got != 150 {
		t.Errorf("appended line missed: output = %d, want 150", got)
	}
}

func TestPartialLineIsNotConsumedUntilComplete(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	projDir := filepath.Join(dir, "proj")
	path := writeTranscript(t, projDir, "a.jsonl",
		assistantLine(now.Add(-2*time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	p := New(testConfig(dir, nil))
	p.Collect(now)

	// Simulate catching Claude Code mid-write: a line without its newline yet.
	full := assistantLine(now.Add(-time.Minute), "m2", "r2", "claude-sonnet-5", 0, 50, 0, 0)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(full[:20]); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if got := p.Collect(now).Tokens[model.Window5h].Output; got != 100 {
		t.Errorf("partial line counted: output = %d, want 100", got)
	}

	appendLines(t, path, full[20:])
	if got := p.Collect(now).Tokens[model.Window5h].Output; got != 150 {
		t.Errorf("completed line missed: output = %d, want 150", got)
	}
}

func TestActiveBlockExcludesOlderSessions(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		// Yesterday's work: a different, long-closed block.
		assistantLine(now.Add(-26*time.Hour), "m0", "r0", "claude-sonnet-5", 0, 999, 0, 0),
		// The current block, opened an hour ago.
		assistantLine(now.Add(-time.Hour), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0),
		assistantLine(now.Add(-time.Minute), "m2", "r2", "claude-sonnet-5", 0, 50, 0, 0),
	)

	st := New(testConfig(dir, nil)).Collect(now)
	if got := st.Tokens[model.Window5h].Output; got != 150 {
		t.Errorf("5h output = %d, want 150 (yesterday's block excluded)", got)
	}
	// Weekly rolls over seven days, so it sees everything.
	if got := st.Tokens[model.WindowWeekly].Output; got != 1149 {
		t.Errorf("weekly output = %d, want 1149", got)
	}
	if r := st.Windows[model.Window5h].ResetsAt; r.Before(now) || r.After(now.Add(blockDuration)) {
		t.Errorf("5h reset %v is not within the next five hours", r)
	}
}

func TestNoLimitConfiguredYieldsUnknownNotZero(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	st := New(testConfig(dir, map[string]int64{"5h": 0, "weekly": 0, "monthly": 0})).Collect(now)
	for _, w := range model.AllWindows {
		if st.Windows[w].Known {
			t.Errorf("%s should be unknown without a configured limit", w)
		}
	}
	// The weighted figure still has to be reported, since that is what the user
	// calibrates the limit against.
	if st.Tokens[model.Window5h].Weighted == 0 {
		t.Error("weighted tokens missing")
	}
}

func TestUsageIsCappedAtFull(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 0, 1_000_000, 0, 0))

	st := New(testConfig(dir, map[string]int64{"5h": 1000})).Collect(now)
	got := st.Windows[model.Window5h]
	if !got.Known || got.UsedPercent != 100 {
		t.Errorf("used = %v, want it clamped to 100", got.UsedPercent)
	}
	if got.RemainingPercent() != 0 {
		t.Errorf("remaining = %v, want 0", got.RemainingPercent())
	}
}

func TestEstimatedValuesAreFlagged(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	st := New(testConfig(dir, nil)).Collect(now)
	for _, w := range []model.Window{model.Window5h, model.WindowWeekly} {
		if !st.Windows[w].Estimated {
			t.Errorf("%s must be flagged as an estimate", w)
		}
	}
	if _, ok := st.Windows[model.WindowMonthly]; ok {
		t.Error("Claude monthly usage must not be estimated")
	}
}

func TestNonAssistantLinesAreIgnored(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		`{"type":"user","timestamp":"`+now.UTC().Format(time.RFC3339)+`","message":{"role":"user"}}`,
		`{"type":"summary","summary":"usage of the tool"}`,
		`not json at all`,
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0),
	)

	st := New(testConfig(dir, nil)).Collect(now)
	if got := st.Tokens[model.Window5h].Output; got != 100 {
		t.Errorf("output = %d, want 100", got)
	}
	if st.Err != "" {
		t.Errorf("unexpected error: %s", st.Err)
	}
}

func TestModelWeightPrefersLongestMatch(t *testing.T) {
	weights := map[string]float64{"opus": 5, "claude-opus-5": 2}
	if got := modelWeight(weights, "claude-opus-5"); got != 2 {
		t.Errorf("got %v, want the more specific 2", got)
	}
	if got := modelWeight(weights, "claude-opus-4-8"); got != 5 {
		t.Errorf("got %v, want 5", got)
	}
	if got := modelWeight(weights, "something-else"); got != 1 {
		t.Errorf("unknown model should weigh 1, got %v", got)
	}
}
