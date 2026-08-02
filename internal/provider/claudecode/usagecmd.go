package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/yutat23/usagebat/internal/model"
)

// Older Claude Code releases made /usage report the real limit percentages in
// print mode. It runs with zero turns and zero tokens, so it remains a cheap
// compatibility fallback when the newer local usage cache is unavailable.
//
// Sample output (the `result` field of --output-format json):
//
//	You are currently using your subscription to power your Claude Code usage
//
//	Current session: 51% used · resets Aug 2 at 7:20pm (Asia/Tokyo)
//	Current week (all models): 6% used · resets Aug 6 at 9am (Asia/Tokyo)
//	...

// usageLine matches one reported limit. The label is non-greedy so that a
// qualified name like "Current week (all models)" is captured whole.
var usageLine = regexp.MustCompile(
	`^(.*?):\s*([0-9]+(?:\.[0-9]+)?)%\s+used(?:\s*[·・]\s*resets\s+(.*?))?\s*$`)

// resetPattern matches "Aug 2 at 7:20pm (Asia/Tokyo)", with the minutes, the
// meridiem and the zone all optional.
var resetPattern = regexp.MustCompile(
	`^([A-Za-z]{3,})\s+(\d{1,2})\s+at\s+(\d{1,2})(?::(\d{2}))?\s*([AaPp][Mm])?\s*(?:\(([^)]+)\))?$`)

// runUsage executes the slash command and returns its rendered text.
func runUsage(ctx context.Context, bin string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "-p", "/usage", "--output-format", "json")
	// Run somewhere neutral: the command inherits the working directory as its
	// project context, and the app's own directory is not meaningful here.
	cmd.Dir = os.TempDir()
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%s: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}

	var payload struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
		Subtype string `json:"subtype"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return "", fmt.Errorf("parsing /usage output: %w", err)
	}
	if payload.IsError {
		return "", fmt.Errorf("/usage reported an error: %s", payload.Subtype)
	}
	return payload.Result, nil
}

// parseUsage turns the rendered text into window statuses. Unrecognised lines
// are skipped rather than guessed at, so an output format we do not understand
// degrades to "no data" instead of to a wrong number.
func parseUsage(text string, now time.Time) map[model.Window]model.WindowStatus {
	out := map[model.Window]model.WindowStatus{}
	for _, raw := range strings.Split(text, "\n") {
		m := usageLine.FindStringSubmatch(strings.TrimSpace(raw))
		if m == nil {
			continue
		}
		w, ok := windowForLabel(m[1])
		if !ok {
			continue
		}
		pct, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		st := model.WindowStatus{
			Window:      w,
			Known:       true,
			UsedPercent: pct,
			ResetsAt:    parseResetTime(strings.TrimSpace(m[3]), now),
			Detail:      "reported by /usage — " + strings.TrimSpace(m[1]),
		}
		// A plan can report several buckets against one window, e.g. an
		// all-models weekly limit alongside a per-model one. The binding
		// constraint is the fullest bucket.
		if prev, seen := out[w]; seen && prev.UsedPercent >= st.UsedPercent {
			continue
		}
		out[w] = st
	}
	return out
}

// windowForLabel maps a reported limit name onto an accounting window.
func windowForLabel(label string) (model.Window, bool) {
	l := strings.ToLower(label)
	switch {
	case strings.Contains(l, "session"):
		return model.Window5h, true
	case strings.Contains(l, "week"):
		return model.WindowWeekly, true
	case strings.Contains(l, "month"):
		return model.WindowMonthly, true
	}
	return "", false
}

var monthNames = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

// parseResetTime reads the human-formatted reset stamp. The year is absent from
// the text, so it is chosen as the one that puts the reset in the future.
func parseResetTime(s string, now time.Time) time.Time {
	m := resetPattern.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}
	}
	month, ok := monthNames[strings.ToLower(m[1])[:3]]
	if !ok {
		return time.Time{}
	}
	day, err := strconv.Atoi(m[2])
	if err != nil {
		return time.Time{}
	}
	hour, err := strconv.Atoi(m[3])
	if err != nil {
		return time.Time{}
	}
	minute := 0
	if m[4] != "" {
		if minute, err = strconv.Atoi(m[4]); err != nil {
			return time.Time{}
		}
	}
	switch strings.ToLower(m[5]) {
	case "pm":
		if hour < 12 {
			hour += 12
		}
	case "am":
		if hour == 12 {
			hour = 0
		}
	}

	loc := now.Location()
	if m[6] != "" {
		if l, err := time.LoadLocation(m[6]); err == nil {
			loc = l
		}
	}

	t := time.Date(now.Year(), month, day, hour, minute, 0, 0, loc)
	// A reset already well in the past means the text wrapped into next year.
	if t.Before(now.Add(-24 * time.Hour)) {
		t = t.AddDate(1, 0, 0)
	}
	return t
}

// resolveBinary finds the Claude Code CLI.
//
// PATH cannot be relied on: a menu-bar app launched from Finder inherits a
// minimal environment that usually lacks ~/.local/bin, so the usual install
// locations are checked explicitly.
func resolveBinary(configured string) (string, error) {
	if configured != "" {
		if strings.ContainsRune(configured, os.PathSeparator) {
			if isExecutable(configured) {
				return configured, nil
			}
			return "", fmt.Errorf("%s is not executable", configured)
		}
		if p, err := exec.LookPath(configured); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}
	for _, c := range wellKnownPaths() {
		if isExecutable(c) {
			return c, nil
		}
	}
	return "", fmt.Errorf("claude CLI not found; set sources.claudeCode.usageCommand.path")
}

func wellKnownPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		return []string{
			filepath.Join(home, ".local", "bin", "claude.exe"),
			filepath.Join(home, "AppData", "Local", "Programs", "claude", "claude.exe"),
		}
	}
	return []string{
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".claude", "local", "claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	}
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return fi.Mode()&0o111 != 0
}
