package notify

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/i18n"
	"github.com/yutat23/usagebat/internal/model"
)

// Gauge is one limit worth watching, as the icon shows it.
//
// Only the limits already on the icon are watched. Somebody who asked to see
// their weekly Codex figure has said what they care about, and warning about
// every window of every service would mean a notification for a limit the
// user deliberately hid.
type Gauge struct {
	// Source is the stable identifier, used for deduplication.
	Source string
	// Name is what the notification calls it.
	Name   string
	Window model.Window
	Status model.WindowStatus
}

// DueLimits returns the warnings to send for gauges that have just dropped
// past a threshold.
//
// A window that resets is a new window, so crossing 20% again after a reset
// warns again, while a gauge sitting at 19% for an hour does not. Which window
// a warning belonged to is recorded alongside the thresholds rather than folded
// into the key: see limitKey.
func (e *Engine) DueLimits(gauges []Gauge, settings config.LimitThresholds,
	p i18n.Printer, now time.Time) []Event {

	if !settings.Enabled || len(settings.Percents) == 0 {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	var events []Event
	// One gauge can reach this list twice — the same service drawn as both a
	// family and an account, say — and two events with one key would be two
	// notifications, because nothing is recorded until each is delivered.
	emitted := map[string]bool{}
	for _, gauge := range gauges {
		if !gauge.Status.Known {
			continue
		}
		key := limitKey(gauge)
		if emitted[key] {
			continue
		}
		entry := e.state.Sent[key]
		if windowRolledOver(entry, gauge.Status.ResetsAt) {
			// Written back rather than only cleared locally, so Mark does not
			// union the retired window's thresholds back in.
			entry = stateEntry{LastSeen: now.Unix()}
			e.state.Sent[key] = entry
		}

		remaining := gauge.Status.RemainingPercent()
		crossed := crossedThresholds(settings.Percents, remaining)
		var unseen []int
		for _, threshold := range crossed {
			if !contains(entry.Thresholds, threshold) {
				unseen = append(unseen, threshold)
			}
		}
		if len(unseen) == 0 {
			continue
		}
		// Several thresholds can be passed at once — a long run between two
		// refreshes, or a first launch already deep into the window. Only the
		// most urgent is worth saying out loud, but all of them are recorded
		// so none of them fires later.
		title, body := p.LimitWarning(gauge.Name, gauge.Window, remaining)
		emitted[key] = true
		events = append(events, Event{Title: title, Body: body, key: key,
			thresholds: crossed, windowResetsAt: gauge.Status.ResetsAt})
	}
	return events
}

// windowRolloverTolerance absorbs disagreement between two readings of the same
// window instead of treating it as a new one.
//
// The same limit can be read from more than one place, and they do not describe
// the reset identically: Claude's usage cache carries RFC 3339 seconds while
// `/usage` prints whole minutes, and Codex derives one reading from a countdown.
// Any real rollover moves the reset by the window's whole length — five hours at
// the shortest — so a generous tolerance still cannot miss one.
const windowRolloverTolerance = 5 * time.Minute

// windowRolledOver reports whether the limit being read is a later window than
// the one already warned about.
//
// It compares the reported reset against the recorded one rather than against
// the clock, so a reading that is a few seconds off cannot look like a fresh
// window — which is exactly what re-sent the same warning every time usagebat
// switched between Claude's cache and `/usage`.
func windowRolledOver(entry stateEntry, reported time.Time) bool {
	if entry.WindowResetsAt <= 0 || reported.IsZero() {
		return false
	}
	return reported.Unix() > entry.WindowResetsAt+int64(windowRolloverTolerance/time.Second)
}

// crossedThresholds returns every threshold at or below which the gauge now
// sits, most urgent first.
func crossedThresholds(percents []int, remaining float64) []int {
	var out []int
	for _, percent := range percents {
		if remaining <= float64(percent) {
			out = append(out, percent)
		}
	}
	sort.Ints(out)
	return out
}

// limitKey identifies one window of one source, for all time.
//
// The reset time is deliberately not part of it. It used to be, so that a fresh
// window could warn again, but that made the identity of a limit depend on a
// value the providers do not report identically from one reading to the next: a
// 13:09:59 from Claude's cache and a 13:09:00 from `/usage` are the same window
// and were two different keys, so every switch between them re-sent the warning
// the user had already seen. Which window it is now lives in the entry, where a
// few seconds of disagreement can be tolerated — see windowRolledOver.
func limitKey(gauge Gauge) string {
	sum := sha256.Sum256([]byte("limit\x00" + gauge.Source + "\x00" + string(gauge.Window)))
	return hex.EncodeToString(sum[:16])
}
