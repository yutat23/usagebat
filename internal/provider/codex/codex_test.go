package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yutat23/usage-battery/internal/config"
	"github.com/yutat23/usage-battery/internal/model"
)

// writeRollout creates a session log under home for the given date.
func writeRollout(t *testing.T, home, date, name string, lines ...string) string {
	t.Helper()
	parts := []string{home, "sessions", date[:4], date[5:7], date[8:10]}
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	var body string
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// tokenCount builds the event shape Codex actually writes.
func tokenCount(ts string, primary, secondary string) string {
	sec := "null"
	if secondary != "" {
		sec = secondary
	}
	return `{"timestamp":"` + ts + `","type":"event_msg","payload":{` +
		`"type":"token_count",` +
		`"info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":800,` +
		`"cache_write_input_tokens":10,"output_tokens":50,"total_tokens":1060}},` +
		`"rate_limits":{"limit_id":"codex","primary":` + primary + `,"secondary":` + sec +
		`,"plan_type":"team"}}}`
}

func jsonBucket(usedPercent float64, windowMinutes int, resetsAt int64) string {
	return fmt.Sprintf(`{"used_percent":%g,"window_minutes":%d,"resets_at":%d}`,
		usedPercent, windowMinutes, resetsAt)
}

// newProvider builds the single provider for one explicit home.
func newProvider(t *testing.T, home string) *Provider {
	t.Helper()
	ps := Providers(&config.Codex{Enabled: true, Homes: []string{home}})
	if len(ps) != 1 {
		t.Fatalf("got %d providers, want 1", len(ps))
	}
	// Unit tests below exercise the rollout compatibility path. Live app-server
	// parsing has focused tests of its own.
	ps[0].live = nil
	return ps[0]
}

func TestWindowMappingByPeriodLength(t *testing.T) {
	home := t.TempDir()
	reset := time.Now().Add(2 * time.Hour).Unix()
	writeRollout(t, home, "2026-08-02", "rollout-a.jsonl",
		tokenCount("2026-08-02T05:00:00.000Z",
			jsonBucket(40, 300, reset),    // 5h
			jsonBucket(25, 10080, reset)), // weekly
	)

	st := newProvider(t, home).Collect(time.Now())
	if st.Err != "" {
		t.Fatalf("unexpected error: %s", st.Err)
	}
	if got := st.Windows[model.Window5h]; !got.Known || got.UsedPercent != 40 {
		t.Errorf("5h = %+v", got)
	}
	if got := st.Windows[model.WindowWeekly]; !got.Known || got.UsedPercent != 25 {
		t.Errorf("weekly = %+v", got)
	}
	if _, ok := st.Windows[model.WindowMonthly]; ok {
		t.Errorf("monthly should be absent when Codex reports no such bucket")
	}
}

func TestMonthlyWindowFromLongPeriod(t *testing.T) {
	home := t.TempDir()
	// 43800 minutes is the ~30.4 day bucket seen on team plans.
	writeRollout(t, home, "2026-08-02", "rollout-a.jsonl",
		tokenCount("2026-08-02T05:00:00.000Z", jsonBucket(0, 43800, 1788229667), ""))

	st := newProvider(t, home).Collect(time.Now())
	got, ok := st.Windows[model.WindowMonthly]
	if !ok || !got.Known {
		t.Fatalf("monthly missing: %+v (%s)", st.Windows, st.Err)
	}
	if got.Estimated {
		t.Error("Codex figures are reported, not estimated")
	}
	if want := time.Unix(1788229667, 0); !got.ResetsAt.Equal(want) {
		t.Errorf("ResetsAt = %v, want %v", got.ResetsAt, want)
	}
}

func TestNewestEventWins(t *testing.T) {
	home := t.TempDir()
	reset := time.Now().Add(time.Hour).Unix()
	// Two events in one file: the later one is the current state.
	writeRollout(t, home, "2026-08-02", "rollout-a.jsonl",
		tokenCount("2026-08-02T05:00:00.000Z", jsonBucket(10, 300, reset), ""),
		`{"timestamp":"2026-08-02T05:30:00.000Z","type":"response_item","payload":{"type":"message"}}`,
		tokenCount("2026-08-02T06:00:00.000Z", jsonBucket(72, 300, reset), ""),
	)

	st := newProvider(t, home).Collect(time.Now())
	if got := st.Windows[model.Window5h]; got.UsedPercent != 72 {
		t.Errorf("used = %v, want the newest event's 72", got.UsedPercent)
	}
}

func TestEachHomeStaysItsOwnSource(t *testing.T) {
	work, personal := t.TempDir(), t.TempDir()
	reset := time.Now().Add(time.Hour).Unix()
	writeRollout(t, work, "2026-07-01", "rollout-work.jsonl",
		tokenCount("2026-07-01T05:00:00.000Z", jsonBucket(5, 300, reset), ""))
	writeRollout(t, personal, "2026-08-02", "rollout-personal.jsonl",
		tokenCount("2026-08-02T05:00:00.000Z", jsonBucket(88, 300, reset), ""))

	ps := Providers(&config.Codex{Enabled: true, Homes: []string{work, personal}})
	if len(ps) != 2 {
		t.Fatalf("got %d providers, want one per home", len(ps))
	}

	// Separate homes are separate accounts. Merging them would report one
	// account's quota under the other's name.
	got := map[string]float64{}
	names := map[string]bool{}
	ids := map[string]bool{}
	for _, p := range ps {
		p.live = nil
		st := p.Collect(time.Now())
		got[st.Name] = st.Windows[model.Window5h].UsedPercent
		names[st.Name] = true
		ids[st.ID] = true
	}
	if len(names) != 2 || len(ids) != 2 {
		t.Fatalf("sources are not distinguishable: names=%v ids=%v", names, ids)
	}
	var used []float64
	for _, v := range got {
		used = append(used, v)
	}
	if !(contains(used, 5) && contains(used, 88)) {
		t.Errorf("got %v, want both 5 and 88 reported separately", got)
	}
}

func contains(xs []float64, want float64) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestAutoDoesNotAdoptUnnamedProfiles(t *testing.T) {
	// "auto" must resolve to the standard location only. Picking up a sibling
	// directory the user never named could put another account's quota on the
	// menu bar.
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	writeRollout(t, home, "2026-08-02", "rollout-a.jsonl",
		tokenCount("2026-08-02T05:00:00.000Z", jsonBucket(7, 300, time.Now().Unix()), ""))

	homes := resolveHomes(&config.Codex{Homes: []string{"auto"}})
	if len(homes) != 1 {
		t.Fatalf("auto resolved to %v, want exactly the configured CODEX_HOME", homes)
	}
	if got := labelFor(homes[0]); got != "Codex" {
		t.Errorf("the default home needs no qualifier, got %q", got)
	}
}

func TestNoSessionsIsReportedNotFabricated(t *testing.T) {
	st := newProvider(t, t.TempDir()).Collect(time.Now())
	if st.Err == "" {
		t.Fatal("expected an error when no sessions directory exists")
	}
	if len(st.Windows) != 0 {
		t.Errorf("no data must not produce windows: %+v", st.Windows)
	}
}

func TestResetsInSecondsFallback(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	writeRollout(t, home, "2026-08-02", "rollout-a.jsonl",
		`{"timestamp":"`+now.UTC().Format(time.RFC3339)+`","type":"event_msg","payload":{`+
			`"type":"token_count","info":{},`+
			`"rate_limits":{"primary":{"used_percent":50,"window_minutes":300,`+
			`"resets_in_seconds":3600}}}}`)

	st := newProvider(t, home).Collect(now)
	got := st.Windows[model.Window5h]
	if !got.Known {
		t.Fatalf("5h missing: %s", st.Err)
	}
	if d := got.ResetsAt.Sub(now); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("ResetsAt = %v (%v from now), want about an hour out", got.ResetsAt, d)
	}
}

func TestExpiredRolloutIsNotShownAsCurrent(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	writeRollout(t, home, "2026-08-02", "rollout-a.jsonl",
		tokenCount(now.Add(-2*time.Hour).UTC().Format(time.RFC3339),
			jsonBucket(78, 10080, now.Add(-time.Hour).Unix()), ""))

	st := newProvider(t, home).Collect(now)
	if _, ok := st.Windows[model.WindowWeekly]; ok {
		t.Fatalf("expired weekly snapshot must not be displayed: %+v", st.Windows)
	}
	if st.Err == "" {
		t.Fatal("expired-only data should explain why no limit is displayed")
	}
}

func TestParseLiveRateLimitsUsesCodexSnapshot(t *testing.T) {
	reset5h := time.Now().Add(2 * time.Hour).Unix()
	resetWeek := time.Now().Add(4 * 24 * time.Hour).Unix()
	payload := fmt.Sprintf(`{
		"rateLimits":{"limitId":"other","primary":{"usedPercent":99,"windowDurationMins":300}},
		"rateLimitsByLimitId":{"codex":{"limitId":"codex","planType":"plus",
		"primary":{"usedPercent":6,"windowDurationMins":300,"resetsAt":%d},
		"secondary":{"usedPercent":6,"windowDurationMins":10080,"resetsAt":%d}}}}`,
		reset5h, resetWeek)

	rl, err := parseLiveRateLimits([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if rl.PlanType != "plus" || rl.Primary.UsedPercent != 6 || rl.Secondary.UsedPercent != 6 {
		t.Fatalf("unexpected live limits: %+v", rl)
	}
	if rl.Primary.WindowMinutes != 300 || rl.Secondary.WindowMinutes != 10080 {
		t.Fatalf("live window durations were not mapped: %+v", rl)
	}
}
