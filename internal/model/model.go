// Package model holds the types shared between providers, renderer and tray.
package model

import (
	"fmt"
	"strings"
	"time"
)

// Window is a usage-limit accounting period.
type Window string

const (
	Window5h      Window = "5h"
	WindowWeekly  Window = "weekly"
	WindowMonthly Window = "monthly"
)

// AllWindows is the canonical display order.
var AllWindows = []Window{Window5h, WindowWeekly, WindowMonthly}

// Label is the two-character label drawn above the battery.
func (w Window) Label() string {
	switch w {
	case Window5h:
		return "5H"
	case WindowWeekly:
		return "WK"
	case WindowMonthly:
		return "MO"
	}
	return "??"
}

// Title is the human-readable name used in menus.
func (w Window) Title() string {
	switch w {
	case Window5h:
		return "5h"
	case WindowWeekly:
		return "Weekly"
	case WindowMonthly:
		return "Monthly"
	}
	return string(w)
}

// ParseWindow maps a config string to a Window, reporting whether it is known.
func ParseWindow(s string) (Window, bool) {
	for _, w := range AllWindows {
		if string(w) == s {
			return w, true
		}
	}
	return "", false
}

// WindowStatus is the state of one accounting period for one source.
type WindowStatus struct {
	Window Window
	// Known is false when the provider cannot determine a percentage at all
	// (no data, or no configured limit). The battery renders "?" in that case.
	Known bool
	// UsedPercent is 0..100.
	UsedPercent float64
	// ResetsAt is the zero time when unknown.
	ResetsAt time.Time
	// Detail is a short menu line explaining where the number came from.
	Detail string
}

// RemainingPercent is what the battery gauge shows.
func (s WindowStatus) RemainingPercent() float64 {
	r := 100 - s.UsedPercent
	if r < 0 {
		return 0
	}
	if r > 100 {
		return 100
	}
	return r
}

// Tokens is a raw token tally, shown in the menu.
type Tokens struct {
	Input         int64
	Output        int64
	CacheCreation int64
	CacheRead     int64
	// Weighted is the normalised figure a provider compares against a limit.
	// Zero when the provider does not estimate.
	Weighted float64
}

// RateLimitResetCredit is one earned Codex rate-limit reset. Detail rows are
// optional in the app-server response even when AvailableCount is known.
type RateLimitResetCredit struct {
	ID        string
	Status    string
	GrantedAt time.Time
	ExpiresAt time.Time
	Title     string
}

// RateLimitResetCredits is the authoritative available count plus any detail
// rows the service chose to return.
type RateLimitResetCredits struct {
	AvailableCount int
	Credits        []RateLimitResetCredit
}

// Add accumulates another tally.
func (t *Tokens) Add(o Tokens) {
	t.Input += o.Input
	t.Output += o.Output
	t.CacheCreation += o.CacheCreation
	t.CacheRead += o.CacheRead
	t.Weighted += o.Weighted
}

// Total is the unweighted sum.
func (t Tokens) Total() int64 {
	return t.Input + t.Output + t.CacheCreation + t.CacheRead
}

// SourceStatus is everything one provider knows right now.
type SourceStatus struct {
	ID   string // "claude-code" / "codex"
	Name string // display name, may include the directory a value came from
	// Err is non-empty when the provider failed; Windows may still be partly filled.
	Err     string
	Windows map[Window]WindowStatus
	Tokens  map[Window]Tokens
	// TokensNote qualifies what the token tallies cover when they are not a
	// per-window total, e.g. Codex reports per session.
	TokensNote string
	Note       string // e.g. plan type
	// RateLimitResets is nil when this source cannot fetch live reset-credit
	// data. AvailableCount remains authoritative when Credits is incomplete.
	RateLimitResets *RateLimitResetCredits
	UpdatedAt       time.Time
}

// Snapshot is one full refresh across all enabled sources.
type Snapshot struct {
	Sources []SourceStatus
	// Icon holds the aggregated per-window value actually drawn in the tray.
	Icon      map[Window]WindowStatus
	UpdatedAt time.Time
}

const (
	SourceClaudeCode = "claude-code"
	SourceCodex      = "codex"
)

// Aggregate fills Snapshot.Icon by taking, per window, the source with the
// least remaining capacity. The most constrained limit is the one that decides
// what you can still do, so that is what the battery should show.
func (s *Snapshot) Aggregate(windows []Window) {
	s.AggregateSources(windows, nil)
}

// AggregateSources fills Snapshot.Icon from only the selected provider
// families. An empty sources slice means all sources, preserving the original
// Aggregate behaviour for callers that do not need filtering. Codex profiles
// have IDs such as "codex:work", so the family prefix is matched as well.
func (s *Snapshot) AggregateSources(windows []Window, sources []string) {
	s.Icon = make(map[Window]WindowStatus, len(windows))
	for _, w := range windows {
		best := WindowStatus{Window: w}
		found := false
		for _, src := range s.Sources {
			if !sourceSelected(src.ID, sources) {
				continue
			}
			st, ok := src.Windows[w]
			if !ok || !st.Known {
				continue
			}
			if !found || st.UsedPercent > best.UsedPercent {
				best, found = st, true
			}
		}
		best.Window = w
		s.Icon[w] = best
	}
}

func sourceSelected(id string, selected []string) bool {
	if len(selected) == 0 {
		return true
	}
	for _, family := range selected {
		if id == family || strings.HasPrefix(id, family+":") {
			return true
		}
	}
	return false
}

// FormatCount renders a token count compactly for menu lines.
func FormatCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

// FormatReset renders a whole phrase describing when a window rolls over.
// Nearby resets get both the countdown and the wall-clock time, which is what
// you actually want when deciding whether to wait it out; distant ones get the
// date alone.
func FormatReset(t time.Time, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := t.Sub(now)
	local := t.Local()
	switch {
	case d < 0:
		return "resetting now"
	case d < time.Hour:
		return fmt.Sprintf("resets in %dm (%s)", int(d.Minutes()), local.Format("15:04"))
	case d < 24*time.Hour:
		return fmt.Sprintf("resets in %dh%02dm (%s)",
			int(d.Hours()), int(d.Minutes())%60, local.Format("15:04"))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("resets in %dd%dh (%s)",
			int(d.Hours()/24), int(d.Hours())%24, local.Format("Mon 15:04"))
	}
	return "resets " + local.Format("Jan 2 15:04")
}
