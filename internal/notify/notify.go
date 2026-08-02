// Package notify decides when a banked-reset expiry deserves one user
// notification and persists deduplication state separately from user config.
package notify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/i18n"
	"github.com/yutat23/usagebat/internal/model"
)

type Event struct {
	Title, Body string
	key         string
	thresholds  []int
}

type stateEntry struct {
	Thresholds []int `json:"thresholdHours"`
	LastSeen   int64 `json:"lastSeen"`
}

type stateFile struct {
	Version int                   `json:"version"`
	Sent    map[string]stateEntry `json:"sent"`
}

type Engine struct {
	mu    sync.Mutex
	path  string
	state stateFile
}

func New() *Engine {
	dir, err := os.UserConfigDir()
	if err != nil {
		return newAt("")
	}
	return newAt(filepath.Join(dir, "usagebat", "state.json"))
}

func newAt(path string) *Engine {
	e := &Engine{path: path, state: stateFile{Version: 1, Sent: map[string]stateEntry{}}}
	data, err := os.ReadFile(path)
	if err == nil {
		var loaded stateFile
		if json.Unmarshal(data, &loaded) == nil && loaded.Sent != nil {
			e.state = loaded
		}
	}
	return e
}

// Due returns at most one event per Codex account: the earliest known expiry.
// On startup inside several thresholds, only the most urgent message is sent.
func (e *Engine) Due(snap *model.Snapshot, settings config.BankedResetExpiry, p i18n.Printer, now time.Time) []Event {
	if !settings.Enabled {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	var events []Event
	for _, src := range snap.Sources {
		resets := src.RateLimitResets
		if resets == nil || resets.AvailableCount <= 0 {
			continue
		}
		credit, ok := earliestAvailable(resets.Credits, now)
		if !ok {
			continue
		}
		key := eventKey(src.ID, credit)
		entry := e.state.Sent[key]
		matching := matchingThresholds(settings.ThresholdHours, credit.ExpiresAt.Sub(now))
		var unseen []int
		for _, threshold := range matching {
			if !contains(entry.Thresholds, threshold) {
				unseen = append(unseen, threshold)
			}
		}
		if len(unseen) == 0 {
			continue
		}
		expiring := expiringAt(resets.Credits, credit.ExpiresAt)
		title, body := p.ResetNotification(expiring, credit.ExpiresAt, now)
		events = append(events, Event{Title: title, Body: body, key: key, thresholds: matching})
	}
	return events
}

func expiringAt(credits []model.RateLimitResetCredit, expiry time.Time) int {
	count := 0
	for _, credit := range credits {
		if (credit.Status == "" || credit.Status == "available") && credit.ExpiresAt.Equal(expiry) {
			count++
		}
	}
	if count < 1 {
		return 1
	}
	return count
}

func earliestAvailable(credits []model.RateLimitResetCredit, now time.Time) (model.RateLimitResetCredit, bool) {
	var best model.RateLimitResetCredit
	found := false
	for _, credit := range credits {
		if credit.Status != "" && credit.Status != "available" {
			continue
		}
		if credit.ExpiresAt.IsZero() || !credit.ExpiresAt.After(now) {
			continue
		}
		if !found || credit.ExpiresAt.Before(best.ExpiresAt) {
			best, found = credit, true
		}
	}
	return best, found
}

func matchingThresholds(hours []int, remaining time.Duration) []int {
	var out []int
	for _, hour := range hours {
		if remaining <= time.Duration(hour)*time.Hour {
			out = append(out, hour)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

func eventKey(source string, credit model.RateLimitResetCredit) string {
	sum := sha256.Sum256([]byte(source + "\x00" + credit.ID + "\x00" + strconv.FormatInt(credit.ExpiresAt.Unix(), 10)))
	return hex.EncodeToString(sum[:16])
}

func contains(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// Mark records an event only after the OS notification backend accepted it.
func (e *Engine) Mark(event Event, now time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	entry := e.state.Sent[event.key]
	for _, threshold := range event.thresholds {
		if !contains(entry.Thresholds, threshold) {
			entry.Thresholds = append(entry.Thresholds, threshold)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(entry.Thresholds)))
	entry.LastSeen = now.Unix()
	e.state.Sent[event.key] = entry
	for key, old := range e.state.Sent {
		if old.LastSeen > 0 && now.Unix()-old.LastSeen > int64(90*24*time.Hour/time.Second) {
			delete(e.state.Sent, key)
		}
	}
	return e.save()
}

func (e *Engine) save() error {
	if e.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(e.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, e.path)
}
