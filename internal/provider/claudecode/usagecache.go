package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func (p *Provider) collectCached(now time.Time) (map[model.Window]model.WindowStatus, error) {
	path := p.cfg.UsageCacheFile
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".claude.json")
	} else {
		path = expandHome(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root struct {
		Cache *cachedUsage `json:"cachedUsageUtilization"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if root.Cache == nil || root.Cache.FetchedAtMs <= 0 {
		return nil, fmt.Errorf("no cached usage in %s", path)
	}
	fetched := time.UnixMilli(root.Cache.FetchedAtMs)
	maxAge := time.Duration(p.cfg.UsageCommand.StaleAfterSeconds) * time.Second
	if maxAge > 0 && now.Sub(fetched) > maxAge {
		return nil, fmt.Errorf("cached usage is %s old", now.Sub(fetched).Round(time.Minute))
	}
	return parseCachedUsage(root.Cache, now), nil
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
