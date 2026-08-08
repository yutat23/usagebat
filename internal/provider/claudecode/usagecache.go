package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yutat23/usagebat/internal/model"
)

// cachedUsage is written by current Claude Code releases after fetching the
// account's real subscription utilization. It replaces the older behaviour
// where `claude -p /usage` rendered subscription limits in print mode.
type cachedUsage struct {
	FetchedAtMs int64 `json:"fetchedAtMs"`
	Utilization struct {
		FiveHour cachedBucket `json:"five_hour"`
		SevenDay cachedBucket `json:"seven_day"`
		Limits   []struct {
			Group    string   `json:"group"`
			Percent  *float64 `json:"percent"`
			ResetsAt string   `json:"resets_at"`
		} `json:"limits"`
	} `json:"utilization"`
}

type cachedBucket struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

func (p *Provider) collectCached(now time.Time) (map[model.Window]model.WindowStatus, time.Duration, bool, error) {
	path := p.usageCacheFile
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, false, err
	}
	var root struct {
		Cache *cachedUsage `json:"cachedUsageUtilization"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, 0, false, fmt.Errorf("parsing %s: %w", path, err)
	}
	if root.Cache == nil || root.Cache.FetchedAtMs <= 0 {
		return nil, 0, false, fmt.Errorf("no cached usage in %s", path)
	}
	fetched := time.UnixMilli(root.Cache.FetchedAtMs)
	age := now.Sub(fetched)
	if age < 0 {
		age = 0
	}
	parsed := parseCachedUsage(root.Cache, now)
	resetPassed := cachedResetPassed(root.Cache, now)
	if len(parsed) == 0 {
		return nil, age, resetPassed, fmt.Errorf("cached usage has no active limits")
	}
	return parsed, age, resetPassed, nil
}

func cachedResetPassed(cache *cachedUsage, now time.Time) bool {
	passed := func(percent *float64, reset string) bool {
		if percent == nil || reset == "" {
			return false
		}
		at, err := time.Parse(time.RFC3339Nano, reset)
		return err == nil && !at.After(now)
	}
	if passed(cache.Utilization.FiveHour.Utilization, cache.Utilization.FiveHour.ResetsAt) ||
		passed(cache.Utilization.SevenDay.Utilization, cache.Utilization.SevenDay.ResetsAt) {
		return true
	}
	for _, limit := range cache.Utilization.Limits {
		if passed(limit.Percent, limit.ResetsAt) {
			return true
		}
	}
	return false
}

func parseCachedUsage(cache *cachedUsage, now time.Time) map[model.Window]model.WindowStatus {
	out := map[model.Window]model.WindowStatus{}
	add := func(w model.Window, percent *float64, reset, detail string) {
		if percent == nil {
			return
		}
		resetsAt, _ := time.Parse(time.RFC3339Nano, reset)
		if !resetsAt.IsZero() && !resetsAt.After(now) {
			return
		}
		st := model.WindowStatus{Window: w, Known: true, UsedPercent: *percent,
			ResetsAt: resetsAt, Detail: detail}
		if prev, ok := out[w]; !ok || st.UsedPercent > prev.UsedPercent {
			out[w] = st
		}
	}
	add(model.Window5h, cache.Utilization.FiveHour.Utilization,
		cache.Utilization.FiveHour.ResetsAt, "reported by Claude cache — five_hour")
	add(model.WindowWeekly, cache.Utilization.SevenDay.Utilization,
		cache.Utilization.SevenDay.ResetsAt, "reported by Claude cache — seven_day")
	for _, limit := range cache.Utilization.Limits {
		var w model.Window
		switch strings.ToLower(limit.Group) {
		case "session":
			w = model.Window5h
		case "weekly":
			w = model.WindowWeekly
		case "monthly":
			w = model.WindowMonthly
		default:
			continue
		}
		add(w, limit.Percent, limit.ResetsAt, "reported by Claude cache — "+limit.Group)
	}
	return out
}
