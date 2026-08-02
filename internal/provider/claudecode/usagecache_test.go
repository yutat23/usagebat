package claudecode

import (
	"testing"
	"time"

	"github.com/yutat23/usage-battery/internal/model"
)

func TestParseCachedUsage(t *testing.T) {
	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	five, seven := 71.0, 8.0
	cache := &cachedUsage{}
	cache.Utilization.FiveHour = cachedBucket{
		Utilization: &five, ResetsAt: "2026-08-02T19:19:59.648177+09:00",
	}
	cache.Utilization.SevenDay = cachedBucket{
		Utilization: &seven, ResetsAt: "2026-08-06T08:59:59.648202+09:00",
	}
	got := parseCachedUsage(cache, now)
	if st := got[model.Window5h]; !st.Known || st.UsedPercent != 71 || st.Estimated {
		t.Errorf("5h = %+v, want reported 71%%", st)
	}
	if st := got[model.WindowWeekly]; !st.Known || st.UsedPercent != 8 {
		t.Errorf("weekly = %+v, want reported 8%%", st)
	}
	if _, ok := got[model.WindowMonthly]; ok {
		t.Error("monthly must not be invented from the extra-usage spend cap")
	}
}

func TestParseCachedUsageAcceptsExplicitMonthlyLimit(t *testing.T) {
	now := time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC)
	pct := 12.0
	cache := &cachedUsage{}
	cache.Utilization.Limits = append(cache.Utilization.Limits, struct {
		Group    string   `json:"group"`
		Percent  *float64 `json:"percent"`
		ResetsAt string   `json:"resets_at"`
	}{Group: "monthly", Percent: &pct, ResetsAt: "2026-09-01T00:00:00Z"})
	if st := parseCachedUsage(cache, now)[model.WindowMonthly]; !st.Known || st.UsedPercent != 12 {
		t.Errorf("monthly = %+v, want explicit reported limit", st)
	}
}
