// Package history records what the providers reported over time.
//
// Providers only ever answer "how much is left right now", so a chart of the
// past has to be built from samples usagebat keeps itself. Everything stays on
// disk next to the rest of usagebat's state; nothing here leaves the machine.
package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/yutat23/usagebat/internal/model"
)

// Sample is one recorded refresh, flattened to the values a chart needs.
type Sample struct {
	// At is unix seconds. A tray app writes this file for months, so the
	// on-disk form stays deliberately small.
	At      int64   `json:"t"`
	Entries []Entry `json:"e"`
}

func (s Sample) Time() time.Time { return time.Unix(s.At, 0) }

// Entry is one source and accounting period at that moment.
type Entry struct {
	Source string       `json:"s"`
	Window model.Window `json:"w"`
	// Remaining is the percentage the battery would have shown.
	Remaining float64 `json:"r"`
	// Tokens are cumulative within the window, as the provider reported them,
	// and are absent for providers that do not count tokens.
	Tokens *Tokens `json:"k,omitempty"`
}

type Tokens struct {
	Input         int64   `json:"i,omitempty"`
	Output        int64   `json:"o,omitempty"`
	CacheCreation int64   `json:"cc,omitempty"`
	CacheRead     int64   `json:"cr,omitempty"`
	Weighted      float64 `json:"w,omitempty"`
}

func (t Tokens) Total() int64 {
	return t.Input + t.Output + t.CacheCreation + t.CacheRead
}

// Options tune how much history is kept and how finely.
type Options struct {
	// Interval is the shortest gap between two stored samples. The refresh loop
	// runs every minute by default, which is far more detail than any chart of
	// a week can show.
	Interval time.Duration
	// Retention is how far back samples are kept.
	Retention time.Duration
}

func (o Options) withDefaults() Options {
	if o.Interval <= 0 {
		o.Interval = 5 * time.Minute
	}
	if o.Retention <= 0 {
		o.Retention = 30 * 24 * time.Hour
	}
	return o
}

// Recorder appends samples to a file and prunes it. It deliberately keeps no
// samples in memory: this runs in a process that stays resident for weeks, and
// the charts are the only thing that ever needs the full series.
type Recorder struct {
	mu   sync.Mutex
	path string
	opts Options

	lastSample time.Time
	lastPrune  time.Time
}

// DefaultPath is where samples are kept, beside the rest of usagebat's state.
// It is empty when the config directory cannot be resolved, which disables
// recording rather than failing.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "usagebat", "history.jsonl")
}

// New returns a recorder writing to DefaultPath with default sampling.
func New() *Recorder { return NewAt(DefaultPath(), Options{}) }

// NewAt is New with an explicit path. An empty path disables recording, which
// is what happens when the config directory cannot be resolved.
func NewAt(path string, opts Options) *Recorder {
	return &Recorder{path: path, opts: opts.withDefaults()}
}

// Path is where samples are stored, empty when recording is disabled.
func (r *Recorder) Path() string { return r.path }

// Observe records a snapshot, unless one was recorded too recently. It reports
// whether anything was written so callers can tell a skipped sample from a
// failure.
func (r *Recorder) Observe(snap *model.Snapshot, now time.Time) (bool, error) {
	if r.path == "" || snap == nil {
		return false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.lastSample.IsZero() && now.Sub(r.lastSample) < r.opts.Interval {
		return false, nil
	}
	sample := build(snap, now)
	if len(sample.Entries) == 0 {
		// Nothing known yet: a row of holes would only add gaps to the charts.
		return false, nil
	}
	r.lastSample = now
	if err := r.append(sample); err != nil {
		return false, err
	}
	return true, r.pruneLocked(now)
}

func build(snap *model.Snapshot, now time.Time) Sample {
	sample := Sample{At: now.Unix()}
	for _, src := range snap.Sources {
		for _, window := range model.AllWindows {
			status, ok := src.Windows[window]
			if !ok || !status.Known {
				continue
			}
			entry := Entry{
				Source:    src.ID,
				Window:    window,
				Remaining: status.RemainingPercent(),
			}
			if tokens, ok := src.Tokens[window]; ok && (tokens.Total() > 0 || tokens.Weighted > 0) {
				entry.Tokens = &Tokens{
					Input:         tokens.Input,
					Output:        tokens.Output,
					CacheCreation: tokens.CacheCreation,
					CacheRead:     tokens.CacheRead,
					Weighted:      tokens.Weighted,
				}
			}
			sample.Entries = append(sample.Entries, entry)
		}
	}
	return sample
}

func (r *Recorder) append(sample Sample) error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Load returns the samples in [from, to], oldest first. A zero from or to is
// unbounded. A malformed line is skipped rather than failing the whole read:
// a truncated final write must not cost the user their history.
func (r *Recorder) Load(from, to time.Time) ([]Sample, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadLocked(from, to)
}

func (r *Recorder) loadLocked(from, to time.Time) ([]Sample, error) {
	if r.path == "" {
		return nil, nil
	}
	f, err := os.Open(r.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var samples []Sample
	scanner := bufio.NewScanner(f)
	// Samples are small, but a corrupted file must not be able to allocate
	// without bound.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var sample Sample
		if json.Unmarshal(line, &sample) != nil {
			continue
		}
		at := sample.Time()
		if !from.IsZero() && at.Before(from) {
			continue
		}
		if !to.IsZero() && at.After(to) {
			continue
		}
		samples = append(samples, sample)
	}
	if err := scanner.Err(); err != nil {
		return samples, err
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].At < samples[j].At })
	return samples, nil
}

// pruneLocked drops samples past the retention window, at most once a day. The
// rewrite is cheap but pointless to repeat: retention is measured in weeks.
func (r *Recorder) pruneLocked(now time.Time) error {
	if !r.lastPrune.IsZero() && now.Sub(r.lastPrune) < 24*time.Hour {
		return nil
	}
	r.lastPrune = now
	cutoff := now.Add(-r.opts.Retention)

	samples, err := r.loadLocked(time.Time{}, time.Time{})
	if err != nil {
		return err
	}
	kept := samples[:0]
	for _, sample := range samples {
		if sample.Time().Before(cutoff) {
			continue
		}
		kept = append(kept, sample)
	}
	if len(kept) == len(samples) {
		return nil
	}
	return r.rewrite(kept)
}

func (r *Recorder) rewrite(samples []Sample) error {
	tmp := r.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, sample := range samples {
		line, err := json.Marshal(sample)
		if err != nil {
			continue
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, r.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
