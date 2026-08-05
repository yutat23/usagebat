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

// testConfig exercises the transcript tallies alone. The /usage path is
// switched off so tests never shell out to the real CLI, and the cache file
// does not exist, so nothing reports a percentage.
func testConfig(dir string) *config.ClaudeCode {
	c := config.Default().Sources.ClaudeCode
	c.ProjectsDir = dir
	c.UsageCacheFile = filepath.Join(dir, ".claude.json")
	c.UsageCommand.Enabled = false
	return &c
}

func TestWeightingUsesModelAndTokenKind(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	// 1000 in + 100 out + 1000 cache-create + 10000 cache-read, on sonnet (x1):
	// 1000 + 5*100 + 1.25*1000 + 0.1*10000 = 3750
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 1000, 100, 1000, 10000))

	st := New(testConfig(dir)).Collect(now)
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
	st2 := New(testConfig(dir2)).Collect(now)
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

	st := New(testConfig(dir)).Collect(now)
	if got := st.Tokens[model.Window5h].Output; got != 100 {
		t.Errorf("output = %d, want 100 counted once", got)
	}
}

func TestIncrementalReadDoesNotDoubleCount(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	path := writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-2*time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	p := New(testConfig(dir))
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

	p := New(testConfig(dir))
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

	st := New(testConfig(dir)).Collect(now)
	if got := st.Tokens[model.Window5h].Output; got != 150 {
		t.Errorf("5h output = %d, want 150 (yesterday's block excluded)", got)
	}
	// Weekly rolls over seven days, so it sees everything.
	if got := st.Tokens[model.WindowWeekly].Output; got != 1149 {
		t.Errorf("weekly output = %d, want 1149", got)
	}
}

// Every percentage comes from the service. Transcripts say how many tokens
// went through, which is not the same thing as how much of a limit is left,
// and guessing one from the other asked the user to calibrate a budget by hand.
func TestUnreportedWindowsStayUnknown(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 0, 1_000_000, 0, 0))

	st := New(testConfig(dir)).Collect(now)
	for _, w := range model.AllWindows {
		if _, ok := st.Windows[w]; ok {
			t.Errorf("%s has a figure the service never reported: %+v", w, st.Windows[w])
		}
	}
	// The tallies are real data and stay, because they are measured rather
	// than inferred.
	if st.Tokens[model.Window5h].Output != 1_000_000 {
		t.Errorf("token tally lost: %+v", st.Tokens[model.Window5h])
	}
	if !strings.Contains(st.Note, "no figures") {
		t.Errorf("note = %q, want it to say nothing was reported", st.Note)
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

	st := New(testConfig(dir)).Collect(now)
	if got := st.Tokens[model.Window5h].Output; got != 100 {
		t.Errorf("output = %d, want 100", got)
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

// The offset table indexes every transcript ever seen. Sessions get deleted and
// whole projects get archived, so entries that no longer exist have to go or the
// map grows for the life of the process.
func TestDeletedTranscriptsLeaveNoOffsetBehind(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	path := writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(now.Add(-time.Minute), "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	p := New(testConfig(dir))
	p.Collect(now)
	if _, ok := p.offsets[path]; !ok {
		t.Fatal("the transcript was never recorded")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	p.Collect(now)
	if _, ok := p.offsets[path]; ok {
		t.Fatalf("offset for a deleted transcript survived: %v", p.offsets)
	}
}

// Claude's session block opens at the top of the hour of the first message.
// time.Truncate rounds against absolute time, which lands mid-hour in zones
// offset by a half or quarter hour.
func TestBlockStartsOnTheLocalHourInOffsetZones(t *testing.T) {
	kolkata, err := time.LoadLocation("Asia/Kolkata") // UTC+05:30
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	// Transcripts stamp UTC; what matters is the zone the user reads the menu in.
	saved := time.Local
	time.Local = kolkata
	t.Cleanup(func() { time.Local = saved })

	dir := t.TempDir()
	first := time.Date(2026, 8, 2, 14, 20, 0, 0, kolkata)
	now := first.Add(90 * time.Minute)
	writeTranscript(t, filepath.Join(dir, "proj"), "a.jsonl",
		assistantLine(first, "m1", "r1", "claude-sonnet-5", 0, 100, 0, 0))

	p := New(testConfig(dir))
	p.Collect(now)
	start, resets := p.activeBlock(now)
	if got := start.In(kolkata); got.Minute() != 0 || got.Hour() != 14 {
		t.Errorf("block start = %s, want 14:00 local", got.Format(time.RFC3339))
	}
	if want := start.Add(blockDuration); !resets.Equal(want) {
		t.Errorf("reset = %s, want %s", resets, want)
	}
}
