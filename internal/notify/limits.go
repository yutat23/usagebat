package notify

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
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
// A window that resets is a new window: the deduplication key carries the
// reset time, so crossing 20% again after a reset warns again, while a gauge
// sitting at 19% for an hour does not.
func (e *Engine) DueLimits(gauges []Gauge, settings config.LimitThresholds,
	p i18n.Printer, now time.Time) []Event {

	if !settings.Enabled || len(settings.Percents) == 0 {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	var events []Event
	for _, gauge := range gauges {
		if !gauge.Status.Known {
			continue
		}
		remaining := gauge.Status.RemainingPercent()
		key := limitKey(gauge)
		entry := e.state.Sent[key]

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
		events = append(events, Event{Title: title, Body: body, key: key, thresholds: crossed})
	}
	return events
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

// limitKey identifies one window of one source between two resets. Without the
// reset time a 5h limit would warn once and then stay quiet for good; with it,
// and only it, each fresh window warns exactly once per threshold.
func limitKey(gauge Gauge) string {
	sum := sha256.Sum256([]byte("limit\x00" + gauge.Source + "\x00" + string(gauge.Window) +
		"\x00" + strconv.FormatInt(gauge.Status.ResetsAt.Unix(), 10)))
	return hex.EncodeToString(sum[:16])
}
