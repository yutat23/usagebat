// Package claudecode reports Claude Code limit usage.
//
// Every percentage it reports comes from the service. Current Claude Code
// releases cache the figures the service returned in ~/.claude.json, so that
// cache is the primary source; `claude -p /usage` asks for them directly,
// which is what a refresh the user asked for does, and what older CLI
// releases needed.
//
// A window the service does not report is left unknown rather than guessed
// at. Earlier versions estimated it from local token accounting against a
// budget the user calibrated by hand, which asked people to tune a number in
// order to be told something the service already knew.
//
// The per-response tallies in ~/.claude/projects/**/*.jsonl are still read,
// but only to report how many tokens went through each window; they never
// decide a percentage.
package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/model"
)

// lookback bounds both the file scan and the retained entry set. It covers the
// longest window (a calendar month) plus slack.
const lookback = 35 * 24 * time.Hour

// blockDuration is the length of Claude's session window.
const blockDuration = 5 * time.Hour

// Provider aggregates transcripts incrementally across refreshes.
type Provider struct {
	cfg            *config.ClaudeCode
	configDir      string
	usageCacheFile string
	projectsPath   string
	label          string
	short          string
	defaultProfile bool

	// offsets tracks how far each transcript has been consumed. Transcripts are
	// append-only, so a steady-state refresh reads only the new bytes.
	offsets map[string]int64
	entries []entry
	seen    map[string]time.Time

	// Last successful /usage reading, kept so a failed run reuses it briefly
	// instead of the battery dropping to "?" and back over one failed run.
	reported     map[model.Window]model.WindowStatus
	reportedAt   time.Time
	lastAttempt  time.Time
	reportErr    string
	resolvedPath string

	// authoritative makes the next Collect run /usage instead of trusting the
	// cache Claude Code left behind. Set by RequestAuthoritative and cleared
	// once the run has happened.
	authoritative bool
}

// RequestAuthoritative implements provider.Authoritative.
//
// The usage cache is whatever Claude Code wrote the last time it talked to the
// service, which can be an hour ago on a machine where nobody has run it
// since. Asking /usage costs a subprocess but returns what the account
// actually looks like now, which is what somebody pressing refresh wants.
func (p *Provider) RequestAuthoritative() { p.authoritative = true }

type entry struct {
	ts       time.Time
	weighted float64
	tokens   model.Tokens
}

// New builds the first configured provider. It remains for callers that only
// need the default profile; the app uses Providers to keep accounts separate.
func New(cfg *config.ClaudeCode) *Provider {
	profiles := Providers(cfg)
	if len(profiles) > 0 {
		return profiles[0]
	}
	return newProvider(cfg, config.Profile{Path: "auto"}, "", true, true)
}

// Providers builds one provider per distinct Claude configuration directory.
func Providers(cfg *config.ClaudeCode) []*Provider {
	profiles := cfg.Profiles
	if len(profiles) == 0 {
		profiles = []config.Profile{{Path: "auto"}}
	}
	seen := map[string]bool{}
	out := make([]*Provider, 0, len(profiles))
	for index, profile := range profiles {
		dir := resolveConfigDir(profile.Path)
		abs, err := filepath.Abs(dir)
		if err == nil {
			dir = abs
		}
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		standard := profile.Path == "auto" || profile.Path == "" || isStandardClaudeDir(dir)
		out = append(out, newProvider(cfg, profile, dir, index == 0, standard))
	}
	return out
}

func newProvider(cfg *config.ClaudeCode, profile config.Profile, dir string, first, defaultProfile bool) *Provider {
	label := profile.Label
	if label == "" {
		if defaultProfile {
			label = "Claude Code"
		} else {
			label = "Claude Code (" + shortenHome(dir) + ")"
		}
	}
	cache := dir + ".json"
	projects := filepath.Join(dir, "projects")
	// These pre-profile settings remain meaningful for the first account.
	if first && cfg.UsageCacheFile != "" {
		cache = expandHome(cfg.UsageCacheFile)
	}
	if first && cfg.ProjectsDir != "" {
		projects = expandHome(cfg.ProjectsDir)
	}
	return &Provider{
		cfg: cfg, configDir: dir, usageCacheFile: cache, projectsPath: projects,
		label: label, short: profile.Icon(), defaultProfile: defaultProfile,
		offsets: map[string]int64{}, seen: map[string]time.Time{},
	}
}

func resolveConfigDir(path string) string {
	if path != "" && path != "auto" {
		return expandHome(path)
	}
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return expandHome(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func isStandardClaudeDir(dir string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	standard, err := filepath.Abs(filepath.Join(home, ".claude"))
	if err != nil {
		return false
	}
	return filepath.Clean(dir) == filepath.Clean(standard)
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && (path == home || strings.HasPrefix(path, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// Available reports whether Claude Code itself is installed. Configuration can
// remain enabled on machines where it is absent; the app then simply omits the
// provider instead of drawing a permanent unknown battery.
func Available(cfg *config.ClaudeCode) bool {
	_, err := resolveBinary(cfg.UsageCommand.Path)
	return err == nil
}

// ID implements provider.Provider.
func (p *Provider) ID() string {
	if p.defaultProfile {
		return model.SourceClaudeCode
	}
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(p.configDir))
	return fmt.Sprintf("%s:%s-%08x", model.SourceClaudeCode, filepath.Base(p.configDir), sum.Sum32())
}

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
		ID:        p.ID(),
		Name:      p.label,
		Short:     p.short,
		Windows:   map[model.Window]model.WindowStatus{},
		Tokens:    map[model.Window]model.Tokens{},
		UpdatedAt: now,
	}

	var reported map[model.Window]model.WindowStatus
	cached, cacheAge, cacheResetExpired, cacheErr := p.collectCached(now)
	maxCacheAge := time.Duration(p.cfg.UsageCommand.StaleAfterSeconds) * time.Second
	cacheFresh := len(cached) > 0 && !cacheResetExpired &&
		(maxCacheAge <= 0 || cacheAge <= maxCacheAge)
	origin := "Claude usage cache"

	// Fresh cache data is the cheap normal path. Once it ages out, ask /usage
	// for a current reading; if that fails, retain the still-active cached
	// buckets until their reset time instead of replacing useful data with '?'.
	if p.authoritative || !cacheFresh {
		p.authoritative = false
		reported, origin = p.collectReported(now), "/usage"
	}
	if len(reported) == 0 && len(cached) > 0 {
		reported, origin = cached, "Claude usage cache"
	}
	switch {
	case len(reported) > 0 && origin == "Claude usage cache" && !cacheFresh && p.reportErr != "":
		st.Note = fmt.Sprintf("Claude usage cache (%s old); /usage: %s",
			cacheAge.Round(time.Minute), p.reportErr)
	case len(reported) > 0 && origin == "Claude usage cache" && !cacheFresh:
		st.Note = fmt.Sprintf("Claude usage cache (%s old)", cacheAge.Round(time.Minute))
	case len(reported) > 0 && origin == "Claude usage cache":
		st.Note = "reported by Claude usage cache"
	case len(reported) > 0 && p.reportErr != "":
		st.Note = "/usage (last good reading)"
	case len(reported) > 0:
		st.Note = "reported by /usage"
	case cacheErr != nil && p.reportErr != "":
		st.Note = "no figures: usage cache " + cacheErr.Error() + "; /usage: " + p.reportErr
	case cacheErr != nil:
		st.Note = "no figures: usage cache " + cacheErr.Error()
	case p.reportErr != "":
		st.Note = "no figures: /usage " + p.reportErr
	default:
		st.Note = "no figures reported"
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
		start, _, _ := p.windowBounds(w, now)
		tok, _ := p.sum(start, now)
		st.Tokens[w] = tok

		// Only what the service reported. A window it says nothing about stays
		// absent, and the battery draws "?" for it, which is the truth.
		if r, ok := reported[w]; ok {
			st.Windows[w] = r
		}
	}

	if len(reported) == 0 && st.Err == "" {
		st.Err = "no usage data: the usage cache and /usage are both unavailable"
	}
	return st
}

// collectReported polls /usage, subject to throttling, and returns the figures
// to use this cycle — either fresh or a recent cached reading.
func (p *Provider) collectReported(now time.Time) map[model.Window]model.WindowStatus {
	uc := p.cfg.UsageCommand
	if !uc.Enabled {
		p.reported, p.reportErr = nil, ""
		return nil
	}

	// The throttle applies to authoritative requests too: pressing refresh
	// twice must not start two subprocesses, and the second press returns the
	// live reading the first one fetched seconds earlier.
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
		// Too old to stand behind. Reporting nothing is better than showing a
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

	// An auto profile must behave exactly like invoking `claude` in the user's
	// shell. In particular, do not turn the implicit standard location into an
	// explicit CLAUDE_CONFIG_DIR: Claude Code can treat those authentication
	// contexts differently. Explicit profiles still need an isolated env.
	text, err := runUsage(ctx, p.resolvedPath, p.usageCommandConfigDir())
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

func (p *Provider) usageCommandConfigDir() string {
	if p.defaultProfile {
		return ""
	}
	return p.configDir
}

func (p *Provider) projectsDir() string {
	return p.projectsPath
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
	// Transcripts get deleted and whole projects get archived; without this the
	// offset table would keep an entry for every file the app ever saw.
	live := make(map[string]bool, len(p.offsets))
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
		live[path] = true
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
	for path := range p.offsets {
		if !live[path] {
			delete(p.offsets, path)
		}
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

// startOfHour rounds down to the top of the hour the caller is living in.
// Transcripts stamp UTC and time.Truncate rounds against absolute time, so
// neither is enough on its own: in a zone offset by a half or quarter hour
// (Asia/Kolkata, Asia/Kathmandu) both land mid-hour rather than on the hour
// the block actually opened at.
func startOfHour(t time.Time) time.Time {
	local := t.Local()
	return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, local.Location())
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
			blockStart = startOfHour(e.ts)
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
