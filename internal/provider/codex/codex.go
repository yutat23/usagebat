// Package codex reads live rate-limit figures from Codex CLI, with its session
// rollout logs as a compatibility fallback.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/model"
)

// tailBytes bounds how much of a rollout file is scanned. Rate-limit events are
// written on every turn, so the newest one is always near the end.
const tailBytes = 1 << 20

// scanFiles bounds how many recent rollout files are opened per refresh.
const scanFiles = 8

// Provider reads one CODEX_HOME directory.
//
// One instance per home, rather than one instance merging several: separate
// homes are separate accounts with separate quotas, and collapsing them into a
// single number would show a limit that belongs to whichever account happened
// to be used last.
type Provider struct {
	home           string
	label          string
	path           string
	timeoutSeconds int
	live           func(context.Context, string, string) (*rateLimits, error)
	// hint explains what to configure when no home was found at all.
	hint string
}

// Providers builds one provider per configured home. When nothing resolves it
// still returns a single provider, so the menu can say why instead of silently
// omitting Codex.
func Providers(cfg *config.Codex) []*Provider {
	homes := resolveHomes(cfg)
	if len(homes) == 0 {
		return []*Provider{{label: "Codex", hint: unconfiguredHint()}}
	}
	out := make([]*Provider, 0, len(homes))
	for _, h := range homes {
		out = append(out, &Provider{
			home: h, label: labelFor(h), path: cfg.Path,
			timeoutSeconds: cfg.TimeoutSeconds, live: liveRateLimits,
		})
	}
	return out
}

// labelFor names a source. The default home needs no qualifier; anything else
// is shown with its path so two profiles are never confused for each other.
func labelFor(home string) string {
	if def, err := defaultHome(); err == nil && home == def {
		return "Codex"
	}
	return "Codex (" + shortenHome(home) + ")"
}

// ID implements provider.Provider.
func (p *Provider) ID() string {
	if p.home == "" {
		return "codex"
	}
	return "codex:" + filepath.Base(p.home)
}

// rollout event shapes we care about.
type event struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type string `json:"type"`
		Info struct {
			TotalTokenUsage struct {
				InputTokens           int64 `json:"input_tokens"`
				CachedInputTokens     int64 `json:"cached_input_tokens"`
				CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
				OutputTokens          int64 `json:"output_tokens"`
				TotalTokens           int64 `json:"total_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
		RateLimits *rateLimits `json:"rate_limits"`
	} `json:"payload"`
}

type rateLimits struct {
	LimitID   string  `json:"limit_id"`
	Primary   *bucket `json:"primary"`
	Secondary *bucket `json:"secondary"`
	PlanType  string  `json:"plan_type"`
	Credits   *struct {
		HasCredits bool     `json:"has_credits"`
		Unlimited  bool     `json:"unlimited"`
		Balance    *float64 `json:"balance"`
	} `json:"credits"`
}

type bucket struct {
	UsedPercent    float64  `json:"used_percent"`
	WindowMinutes  int      `json:"window_minutes"`
	ResetsAt       *int64   `json:"resets_at"`
	ResetsInSecond *float64 `json:"resets_in_seconds"`
}

// window maps a bucket's period length onto our accounting windows. Which
// bucket carries which period depends on the plan, so the length decides.
func (b *bucket) window() (model.Window, bool) {
	switch {
	case b.WindowMinutes <= 0:
		return "", false
	case b.WindowMinutes <= 720: // up to 12h -> the short session window
		return model.Window5h, true
	case b.WindowMinutes <= 20160: // up to 14d -> weekly
		return model.WindowWeekly, true
	}
	return model.WindowMonthly, true
}

func (b *bucket) resetsAt(observedAt time.Time) time.Time {
	if b.ResetsAt != nil && *b.ResetsAt > 0 {
		return time.Unix(*b.ResetsAt, 0)
	}
	if b.ResetsInSecond != nil && *b.ResetsInSecond > 0 {
		return observedAt.Add(time.Duration(*b.ResetsInSecond) * time.Second)
	}
	return time.Time{}
}

// Collect implements provider.Provider.
func (p *Provider) Collect(now time.Time) model.SourceStatus {
	st := model.SourceStatus{
		ID:        p.ID(),
		Name:      p.label,
		Windows:   map[model.Window]model.WindowStatus{},
		Tokens:    map[model.Window]model.Tokens{},
		UpdatedAt: now,
	}

	if p.home == "" {
		st.Err = p.hint
		return st
	}

	// Keep reading the newest rollout for the optional per-session token tally,
	// but never let it override a live account snapshot.
	best, observedAt, logErr := latestRateLimitEvent(p.home)
	var rl *rateLimits
	liveUsed := false
	var liveErr error
	if p.live != nil {
		bin, err := resolveBinary(p.path)
		if err != nil {
			liveErr = err
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), liveTimeout(p.timeoutSeconds))
			rl, liveErr = p.live(ctx, bin, p.home)
			cancel()
			liveUsed = liveErr == nil && rl != nil
		}
	}
	if !liveUsed {
		if logErr != nil {
			st.Err = logErr.Error()
			if liveErr != nil {
				st.Err = liveErr.Error() + " · " + st.Err
			}
			return st
		}
		if best == nil {
			st.Err = "no Codex rate-limit data in " + shortenHome(p.home)
			if liveErr != nil {
				st.Err = liveErr.Error() + " · " + st.Err
			}
			return st
		}
		rl = best.Payload.RateLimits
		if liveErr != nil {
			st.Note = "rollout fallback"
		}
	} else {
		observedAt = now
		st.Note = "live via Codex"
	}

	if rl.PlanType != "" {
		if st.Note != "" {
			st.Note += " · "
		}
		st.Note += "plan: " + rl.PlanType
	}
	if rl.Credits != nil && rl.Credits.Unlimited {
		st.Note += " · unlimited credits"
	} else if rl.Credits != nil && rl.Credits.Balance != nil {
		st.Note += fmt.Sprintf(" · credits %.2f", *rl.Credits.Balance)
	}

	// Codex reports cumulative tokens for the session that produced the newest
	// event, not per limit window, so the tally is labelled accordingly.
	var tok model.Tokens
	if best != nil {
		st.TokensNote = "latest session"
		tok = model.Tokens{
			Input:         best.Payload.Info.TotalTokenUsage.InputTokens,
			Output:        best.Payload.Info.TotalTokenUsage.OutputTokens,
			CacheRead:     best.Payload.Info.TotalTokenUsage.CachedInputTokens,
			CacheCreation: best.Payload.Info.TotalTokenUsage.CacheWriteInputTokens,
		}
	}

	for _, b := range []*bucket{rl.Primary, rl.Secondary} {
		if b == nil {
			continue
		}
		w, ok := b.window()
		if !ok {
			continue
		}
		reset := b.resetsAt(observedAt)
		// An expired rollout is historical evidence, not the current limit. This
		// is the bug that previously rendered stale percentages as "resetting now".
		if !liveUsed && !reset.IsZero() && !reset.After(now) {
			continue
		}
		detail := fmt.Sprintf("reported by Codex (%s window)", humanMinutes(b.WindowMinutes))
		if liveUsed {
			detail = fmt.Sprintf("reported live by Codex (%s window)", humanMinutes(b.WindowMinutes))
		}
		st.Windows[w] = model.WindowStatus{
			Window:      w,
			Known:       true,
			UsedPercent: b.UsedPercent,
			ResetsAt:    reset,
			Detail:      detail,
		}
		if best != nil {
			st.Tokens[w] = tok
		}
	}
	if len(st.Windows) == 0 {
		if liveUsed {
			st.Err = "Codex reported no usable rate-limit windows"
		} else {
			st.Err = "Codex rollout rate-limit data has expired; waiting for live data"
		}
	}
	return st
}

func humanMinutes(m int) string {
	switch {
	case m%(60*24) == 0 && m >= 60*24:
		return fmt.Sprintf("%dd", m/(60*24))
	case m%60 == 0:
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dm", m)
}

func shortenHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// defaultHome is where a stock Codex install keeps its data.
func defaultHome() (string, error) {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return filepath.Abs(expandHome(h))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// resolveHomes turns the configured list into absolute directories that hold
// session logs.
//
// "auto" means the standard location only — $CODEX_HOME, or ~/.codex. It
// deliberately does not go hunting for other directories: a second home is a
// second account, and adopting one the user never named would put someone
// else's quota on their menu bar.
func resolveHomes(cfg *config.Codex) []string {
	var out []string
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" {
			return
		}
		abs, err := filepath.Abs(expandHome(dir))
		if err != nil || seen[abs] {
			return
		}
		if !hasSessions(abs) {
			return
		}
		seen[abs] = true
		out = append(out, abs)
	}

	for _, h := range cfg.Homes {
		if h == "auto" {
			if def, err := defaultHome(); err == nil {
				add(def)
			}
			continue
		}
		add(h)
	}
	return out
}

func hasSessions(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, "sessions"))
	return err == nil && fi.IsDir()
}

// unconfiguredHint is shown when no home resolved. Some people split Codex by
// profile (work and personal in separate directories); naming what is actually
// on disk turns a dead end into something the user can act on.
func unconfiguredHint() string {
	def, err := defaultHome()
	if err != nil {
		return "no Codex session directory found"
	}
	base := "no session logs in " + shortenHome(def)

	home, err := os.UserHomeDir()
	if err != nil {
		return base
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return base
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), ".codex-") {
			continue
		}
		if hasSessions(filepath.Join(home, e.Name())) {
			found = append(found, "~/"+e.Name())
		}
	}
	if len(found) == 0 {
		return base
	}
	return base + " — add " + strings.Join(found, ", ") + " to sources.codex.homes"
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

type fileInfo struct {
	path string
	mod  time.Time
}

// latestRateLimitEvent returns the newest token_count event carrying rate
// limits under home/sessions.
func latestRateLimitEvent(home string) (*event, time.Time, error) {
	files, err := recentRollouts(filepath.Join(home, "sessions"))
	if err != nil {
		return nil, time.Time{}, err
	}
	for _, f := range files {
		ev, ts, err := scanFile(f.path)
		if err != nil || ev == nil {
			continue
		}
		// Files are visited newest-first and events within a file are ordered,
		// so the first hit is the newest.
		return ev, ts, nil
	}
	return nil, time.Time{}, nil
}

// recentRollouts lists rollout files newest-first, capped at scanFiles. It
// walks the date-partitioned directories from the newest day backwards so a
// long history costs no more than a short one.
func recentRollouts(root string) ([]fileInfo, error) {
	years, err := sortedDirsDesc(root)
	if err != nil {
		return nil, err
	}
	var out []fileInfo
	for _, y := range years {
		months, err := sortedDirsDesc(filepath.Join(root, y))
		if err != nil {
			continue
		}
		for _, m := range months {
			days, err := sortedDirsDesc(filepath.Join(root, y, m))
			if err != nil {
				continue
			}
			for _, d := range days {
				dir := filepath.Join(root, y, m, d)
				entries, err := os.ReadDir(dir)
				if err != nil {
					continue
				}
				var batch []fileInfo
				for _, e := range entries {
					if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
						continue
					}
					info, err := e.Info()
					if err != nil {
						continue
					}
					batch = append(batch, fileInfo{filepath.Join(dir, e.Name()), info.ModTime()})
				}
				sort.Slice(batch, func(i, j int) bool { return batch[i].mod.After(batch[j].mod) })
				out = append(out, batch...)
				if len(out) >= scanFiles {
					return out[:scanFiles], nil
				}
			}
		}
	}
	return out, nil
}

func sortedDirsDesc(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// scanFile returns the last rate-limit-bearing event in a rollout file.
func scanFile(path string) (*event, time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, time.Time{}, err
	}
	var r io.Reader = f
	if fi.Size() > tailBytes {
		if _, err := f.Seek(fi.Size()-tailBytes, io.SeekStart); err != nil {
			return nil, time.Time{}, err
		}
		br := bufio.NewReader(f)
		// Drop the partial first line produced by seeking into the middle.
		if _, err := br.ReadString('\n'); err != nil {
			return nil, time.Time{}, nil
		}
		r = br
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var last *event
	var lastTS time.Time
	for sc.Scan() {
		line := sc.Bytes()
		// Cheap pre-filter: most lines are conversation items.
		if !strings.Contains(string(line), `"rate_limits"`) {
			continue
		}
		var ev event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Payload.Type != "token_count" || ev.Payload.RateLimits == nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, ev.Timestamp)
		if err != nil {
			ts = fi.ModTime()
		}
		e := ev
		last, lastTS = &e, ts
	}
	return last, lastTS, sc.Err()
}
