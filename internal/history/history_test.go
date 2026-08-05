package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/model"
)

func snapshotAt(remaining float64, tokens int64) *model.Snapshot {
	src := model.SourceStatus{
		ID: model.SourceClaudeCode,
		Windows: map[model.Window]model.WindowStatus{
			model.Window5h: {Known: true, UsedPercent: 100 - remaining},
		},
		Tokens: map[model.Window]model.Tokens{
			model.Window5h: {Input: tokens},
		},
	}
	return &model.Snapshot{Sources: []model.SourceStatus{src}}
}

func TestObserveRecordsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	r := NewAt(path, Options{Interval: time.Minute})
	start := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

	for i, remaining := range []float64{100, 80, 55} {
		wrote, err := r.Observe(snapshotAt(remaining, int64(i)*1000), start.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if !wrote {
			t.Fatalf("sample %d was not recorded", i)
		}
	}

	samples, err := r.Load(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 3 {
		t.Fatalf("got %d samples, want 3", len(samples))
	}
	if got := samples[2].Entries[0].Remaining; got != 55 {
		t.Errorf("last remaining = %v, want 55", got)
	}
	if samples[0].At >= samples[1].At {
		t.Error("samples must come back oldest first")
	}
}

// The refresh loop runs every minute. Recording every one of those would make
// the file forty times bigger for detail no chart of a week can show.
func TestObserveHonoursTheInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	r := NewAt(path, Options{Interval: 5 * time.Minute})
	start := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

	if wrote, _ := r.Observe(snapshotAt(90, 0), start); !wrote {
		t.Fatal("the first sample is always recorded")
	}
	if wrote, _ := r.Observe(snapshotAt(89, 0), start.Add(time.Minute)); wrote {
		t.Fatal("a sample one minute later must be skipped")
	}
	if wrote, _ := r.Observe(snapshotAt(80, 0), start.Add(6*time.Minute)); !wrote {
		t.Fatal("a sample past the interval must be recorded")
	}
}

func TestObserveSkipsSnapshotsWithNothingKnown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	r := NewAt(path, Options{})
	snap := &model.Snapshot{Sources: []model.SourceStatus{{
		ID:      model.SourceCodex,
		Windows: map[model.Window]model.WindowStatus{model.Window5h: {Known: false}},
	}}}

	wrote, err := r.Observe(snap, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Fatal("a snapshot with no known values must not be recorded")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("nothing to record should not create the file")
	}
}

func TestPruneDropsSamplesPastRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	r := NewAt(path, Options{Interval: time.Minute, Retention: 48 * time.Hour})
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

	// Written directly: Observe would refuse timestamps this far apart only
	// after the first, and the point here is what pruning keeps.
	old := Sample{At: now.Add(-72 * time.Hour).Unix(), Entries: []Entry{{
		Source: model.SourceClaudeCode, Window: model.Window5h, Remaining: 50,
	}}}
	recent := Sample{At: now.Add(-time.Hour).Unix(), Entries: []Entry{{
		Source: model.SourceClaudeCode, Window: model.Window5h, Remaining: 60,
	}}}
	if err := r.rewrite([]Sample{old, recent}); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Observe(snapshotAt(70, 0), now); err != nil {
		t.Fatal(err)
	}
	samples, err := r.Load(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want the recent one plus the new one", len(samples))
	}
	for _, sample := range samples {
		if sample.Time().Before(now.Add(-48 * time.Hour)) {
			t.Errorf("sample from %s survived pruning", sample.Time())
		}
	}
}

// A crash mid-write leaves a partial final line. Losing weeks of history to it
// would be worse than ignoring it.
func TestLoadSkipsACorruptLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	content := `{"t":1754208000,"e":[{"s":"claude-code","w":"5h","r":90}]}
{"t":1754208300,"e":[{"s":"claude-co
{"t":1754208600,"e":[{"s":"claude-code","w":"5h","r":70}]}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	samples, err := NewAt(path, Options{}).Load(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want the two intact ones", len(samples))
	}
}

func TestLoadFiltersByRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	r := NewAt(path, Options{Interval: time.Minute})
	start := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if _, err := r.Observe(snapshotAt(float64(90-i), 0), start.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	samples, err := r.Load(start.Add(time.Minute), start.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 3 {
		t.Fatalf("got %d samples, want the three inside the range", len(samples))
	}
}

// Recording is disabled when the config directory cannot be resolved. That
// must be quiet, not an error on every refresh.
func TestDisabledRecorderIsInert(t *testing.T) {
	r := NewAt("", Options{})
	wrote, err := r.Observe(snapshotAt(50, 0), time.Now())
	if wrote || err != nil {
		t.Fatalf("wrote %v, err %v; want a silent no-op", wrote, err)
	}
	samples, err := r.Load(time.Time{}, time.Time{})
	if len(samples) != 0 || err != nil {
		t.Fatalf("samples %v, err %v; want nothing", samples, err)
	}
}
