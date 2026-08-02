// Package claudecode reports Claude Code limit usage from two sources.
//
// Current Claude Code releases cache the real percentages returned by the
// service in ~/.claude.json, so that cache is the primary source. Older CLI
// releases exposed the same figures through `claude -p /usage`; that remains a
// compatibility fallback.
//
// Whatever /usage does not report, if the user configured a corresponding
// budget, is estimated from the per-response token accounting in
// ~/.claude/projects/**/*.jsonl, compared against user-calibrated budgets.
// Estimated values are flagged as such; reported ones are not.
package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yutat23/usage-battery/internal/config"
	"github.com/yutat23/usage-battery/internal/model"
)

// lookback bounds both the file scan and the retained entry set. It covers the
// longest window (a calendar month) plus slack.
const lookback = 35 * 24 * time.Hour

// blockDuration is the length of Claude's session window.
const blockDuration = 5 * time.Hour

// Provider aggregates transcripts incrementally across refreshes.
type Provider struct {
	cfg *config.ClaudeCode

	// offsets tracks how far each transcript has been consumed. Transcripts are
	// append-only, so a steady-state refresh reads only the new bytes.
	offsets map[string]int64
	entries []entry
	seen    map[string]time.Time

	// Last successful /usage reading, kept so a failed run reuses it briefly
	// instead of making the battery jump to the estimate and back.
	reported     map[model.Window]model.WindowStatus
	reportedAt   time.Time
	lastAttempt  time.Time
	reportErr    string
	resolvedPath string
}

type entry struct {
	ts       time.Time
	weighted float64
	tokens   model.Tokens
}

// New builds a provider bound to the given config section.
func New(cfg *config.ClaudeCode) *Provider {
	return &Provider{
		cfg:     cfg,
		offsets: map[string]int64{},
		seen:    map[string]time.Time{},
	}
}

// Available reports whether Claude Code itself is installed. Configuration can
// remain enabled on machines where it is absent; the app then simply omits the
// provider instead of drawing a permanent unknown battery.
func Available(cfg *config.ClaudeCode) bool {
	_, err := resolveBinary(cfg.UsageCommand.Path)
	return err == nil
}

// ID implements provider.Provider.
func (p *Provider) ID() string { return "claude-code" }

// transcript line shape, narrowed to the fields we need.
type line struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// Collect implements provider.Provider.
func (p *Provider) Collect(now time.Time) model.SourceStatus {
	st := model.SourceStatus{
		ID:        "claude-code",
		Name:      "Claude Code",
		Windows:   map[model.Window]model.WindowStatus{},
		Tokens:    map[model.Window]model.Tokens{},
		UpdatedAt: now,
	}

	reported, cacheErr := p.collectCached(now)
	origin := "Claude usage cache"
	if len(reported) == 0 {
		reported = p.collectReported(now)
		origin = "/usage"
	}
	switch {
	case len(reported) > 0 && origin == "Claude usage cache":
		st.Note = "reported by Claude usage cache"
	case len(reported) > 0 && p.reportErr != "":
		st.Note = "/usage (last good reading)"
	case len(reported) > 0:
		st.Note = "reported by /usage"
	case cacheErr != nil && p.reportErr != "":
		st.Note = "estimated — usage cache unavailable: " + cacheErr.Error() + "; /usage: " + p.reportErr
	case cacheErr != nil:
		st.Note = "estimated — usage cache unavailable: " + cacheErr.Error()
	case p.reportErr != "":
		st.Note = "estimated — /usage unavailable: " + p.reportErr
	default:
		st.Note = "estimated from local transcripts"
	}

	dir := p.projectsDir()
	if dir == "" {
		st.Err = "cannot locate ~/.claude/projects"
	} else {
		if err := p.ingest(dir, now); err != nil {
			st.Err = err.Error()
		}
		p.prune(now)
		sort.Slice(p.entries, func(i, j int) bool { return p.entries[i].ts.Before(p.entries[j].ts) })
	}

	for _, w := range model.AllWindows {
		start, resets, label := p.windowBounds(w, now)
		tok, weighted := p.sum(start, now)
		st.Tokens[w] = tok

		// A real figure always beats an estimate; estimation only fills gaps.
		if r, ok := reported[w]; ok {
			st.Windows[w] = r
			continue
		}
		// Claude subscriptions expose session and weekly limits. Do not invent a
		// monthly quota from transcript totals. If Anthropic ever reports one via
		// /usage, the reported branch above will still display it.
		if w == model.WindowMonthly {
			continue
		}
		if estimated := p.estimate(w, label, resets, weighted); estimated.Known {
			st.Windows[w] = estimated
		}
	}

	if len(reported) == 0 && len(p.entries) == 0 && st.Err == "" {
		st.Err = "no usage data: /usage unavailable and no transcripts found"
	}
	return st
}

// estimate derives a window status from weighted token accounting.
func (p *Provider) estimate(w model.Window, label string, resets time.Time, weighted float64) model.WindowStatus {
	ws := model.WindowStatus{Window: w, Estimated: true, ResetsAt: resets}
	limit := p.cfg.Limits[string(w)]
	if limit <= 0 {
		ws.Detail = fmt.Sprintf("%s · no limit configured (weighted %s)",
			label, model.FormatCount(int64(weighted)))
		return ws
	}
	ws.Known = true
	ws.UsedPercent = weighted / float64(limit) * 100
	if ws.UsedPercent > 100 {
		ws.UsedPercent = 100
	}
	ws.Detail = fmt.Sprintf("%s · weighted %s / %s",
		label, model.FormatCount(int64(weighted)), model.FormatCount(limit))
	return ws
}

// collectReported polls /usage, subject to throttling, and returns the figures
// to use this cycle — either fresh or a recent cached reading.
func (p *Provider) collectReported(now time.Time) map[model.Window]model.WindowStatus {
	uc := p.cfg.UsageCommand
	if !uc.Enabled {
		p.reported, p.reportErr = nil, ""
		return nil
	}

	throttled := !p.lastAttempt.IsZero() &&
		now.Sub(p.lastAttempt) < time.Duration(uc.MinIntervalSeconds)*time.Second
	if !throttled {
		p.lastAttempt = now
		if err := p.runAndStore(now); err != nil {
			p.reportErr = err.Error()
		} else {
			p.reportErr = ""
		}
	}

	if len(p.reported) == 0 {
		return nil
	}
	if age := now.Sub(p.reportedAt); age > time.Duration(uc.StaleAfterSeconds)*time.Second {
		// Too old to stand behind; fall back to estimation rather than showing a
		// stale number as if it were current.
		p.reported = nil
		return nil
	}
	return p.reported
}

func (p *Provider) runAndStore(now time.Time) error {
	if p.resolvedPath == "" {
		path, err := resolveBinary(p.cfg.UsageCommand.Path)
		if err != nil {
			return err
		}
		p.resolvedPath = path
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(p.cfg.UsageCommand.TimeoutSeconds)*time.Second)
	defer cancel()

	text, err := runUsage(ctx, p.resolvedPath)
	if err != nil {
		// The path may have gone stale (an update moved the binary); re-resolve
		// on the next attempt.
		p.resolvedPath = ""
		return err
	}
	parsed := parseUsage(text, now)
	if len(parsed) == 0 {
		return fmt.Errorf("no limits recognised in /usage output")
	}
	p.reported, p.reportedAt = parsed, now
	return nil
}

func (p *Provider) projectsDir() string {
	if p.cfg.ProjectsDir != "" {
		return expandHome(p.cfg.ProjectsDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

// ingest walks the transcript tree and consumes anything appended since the
// last refresh.
func (p *Provider) ingest(dir string, now time.Time) error {
	cutoff := now.Add(-lookback)
	var firstErr error
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip rather than abort the refresh
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			return nil
		}
		off := p.offsets[path]
		if info.Size() == off {
			return nil
		}
		if info.Size() < off {
			off = 0 // truncated or replaced; re-read from the start
		}
		next, err := p.readFrom(path, off, cutoff)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		p.offsets[path] = next
		return nil
	})
	if err != nil {
		return err
	}
	return firstErr
}

// readFrom parses appended lines and returns the new offset. The offset only
// advances past complete lines so a transcript captured mid-write is re-read.
func (p *Provider) readFrom(path string, off int64, cutoff time.Time) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return off, err
	}
	defer f.Close()

	if off > 0 {
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			return off, err
		}
	}
	r := bufio.NewReaderSize(f, 256*1024)
	consumed := off
	for {
		raw, err := r.ReadBytes('\n')
		if err != nil {
			// A trailing chunk without a newline is an incomplete write: leave the
			// offset before it so the next refresh sees the whole line.
			break
		}
		consumed += int64(len(raw))
		p.parseLine(raw, cutoff)
	}
	return consumed, nil
}

func (p *Provider) parseLine(raw []byte, cutoff time.Time) {
	// Cheap pre-filter: only assistant responses carry usage.
	if !strings.Contains(string(raw), `"usage"`) {
		return
	}
	var l line
	if err := json.Unmarshal(raw, &l); err != nil {
		return
	}
	if l.Type != "assistant" {
		return
	}
	u := l.Message.Usage
	if u.InputTokens == 0 && u.OutputTokens == 0 &&
		u.CacheCreationInputTokens == 0 && u.CacheReadInputTokens == 0 {
		return
	}
	ts, err := time.Parse(time.RFC3339, l.Timestamp)
	if err != nil || ts.Before(cutoff) {
		return
	}
	// The same response appears in several transcripts when a session is
	// resumed or forked, so count each (message, request) pair once.
	key := l.Message.ID + "|" + l.RequestID
	if key == "|" {
		key = fmt.Sprintf("%s|%d", l.Timestamp, u.OutputTokens)
	}
	if _, dup := p.seen[key]; dup {
		return
	}
	p.seen[key] = ts

	tok := model.Tokens{
		Input:         u.InputTokens,
		Output:        u.OutputTokens,
		CacheCreation: u.CacheCreationInputTokens,
		CacheRead:     u.CacheReadInputTokens,
	}
	w := p.weigh(l.Message.Model, tok)
	tok.Weighted = w
	p.entries = append(p.entries, entry{ts: ts, weighted: w, tokens: tok})
}

// weigh normalises a response's tokens into one comparable figure. The
// coefficients are configurable because the real ones are not published.
func (p *Provider) weigh(modelName string, t model.Tokens) float64 {
	w := p.cfg.Weights
	base := float64(t.Input) +
		w.Output*float64(t.Output) +
		w.CacheCreation*float64(t.CacheCreation) +
		w.CacheRead*float64(t.CacheRead)
	return base * modelWeight(w.Models, modelName)
}

// modelWeight picks the most specific matching model coefficient.
func modelWeight(models map[string]float64, name string) float64 {
	name = strings.ToLower(name)
	best, bestLen := 1.0, -1
	for k, v := range models {
		k = strings.ToLower(k)
		if strings.Contains(name, k) && len(k) > bestLen {
			best, bestLen = v, len(k)
		}
	}
	return best
}

// prune drops entries and dedup keys that have aged out of every window.
func (p *Provider) prune(now time.Time) {
	cutoff := now.Add(-lookback)
	kept := p.entries[:0]
	for _, e := range p.entries {
		if e.ts.After(cutoff) {
			kept = append(kept, e)
		}
	}
	p.entries = kept
	for k, ts := range p.seen {
		if ts.Before(cutoff) {
			delete(p.seen, k)
		}
	}
}

func (p *Provider) sum(start, end time.Time) (model.Tokens, float64) {
	var tok model.Tokens
	var weighted float64
	for _, e := range p.entries {
		if e.ts.Before(start) || e.ts.After(end) {
			continue
		}
		tok.Add(e.tokens)
		weighted += e.weighted
	}
	return tok, weighted
}

// windowBounds returns the start of the current window, when it resets, and a
// label describing the rule used.
func (p *Provider) windowBounds(w model.Window, now time.Time) (start, resets time.Time, label string) {
	switch w {
	case model.Window5h:
		s, r := p.activeBlock(now)
		return s, r, "5h block"
	case model.WindowWeekly:
		if p.cfg.WeeklyMode == "calendar" {
			s := startOfWeek(now)
			return s, s.AddDate(0, 0, 7), "calendar week"
		}
		s := now.Add(-7 * 24 * time.Hour)
		return s, p.rollingReset(s, 7*24*time.Hour), "rolling 7d"
	case model.WindowMonthly:
		if p.cfg.MonthlyMode == "rolling" {
			s := now.AddDate(0, 0, -30)
			return s, p.rollingReset(s, 30*24*time.Hour), "rolling 30d"
		}
		s := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return s, s.AddDate(0, 1, 0), "calendar month"
	}
	return now, time.Time{}, ""
}

// rollingReset is when the oldest counted activity leaves the window, i.e. the
// next moment the figure drops on its own.
func (p *Provider) rollingReset(start time.Time, d time.Duration) time.Time {
	for _, e := range p.entries {
		if e.ts.After(start) {
			return e.ts.Add(d)
		}
	}
	return time.Time{}
}

func startOfWeek(now time.Time) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return day.AddDate(0, 0, -int(day.Weekday()))
}

// activeBlock reproduces Claude Code's session-window behaviour: a block opens
// at the top of the hour of the first activity, and a new one opens after five
// hours or after five hours of silence.
func (p *Provider) activeBlock(now time.Time) (start, resets time.Time) {
	var blockStart, last time.Time
	for _, e := range p.entries {
		if blockStart.IsZero() ||
			e.ts.Sub(blockStart) >= blockDuration ||
			e.ts.Sub(last) >= blockDuration {
			blockStart = e.ts.Truncate(time.Hour)
		}
		last = e.ts
	}
	if blockStart.IsZero() || now.Sub(blockStart) >= blockDuration {
		// No block is open: the window is fully available and starts fresh on the
		// next message, so count nothing.
		return now, time.Time{}
	}
	return blockStart, blockStart.Add(blockDuration)
}
