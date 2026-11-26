package antigravity

import (
	"errors"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/model"
)

const usageResponse = `{
	"status": "SUCCESS",
	"command": {
		"name": "usage",
		"data": {
			"groups": [
				{"name":"Gemini Models","buckets":[
					{"id":"gemini-weekly","window":"weekly","remaining_fraction":0.875,"reset_time":"2026-12-04T00:00:00Z"},
					{"id":"gemini-5h","window":"5h","remaining_fraction":0,"reset_time":"2026-11-27T00:03:00Z"},
					{"id":"gemini-monthly","window":"monthly","remaining_fraction":null}
				]},
				{"name":"Claude and GPT models","buckets":[
					{"id":"3p-weekly","window":"weekly","remaining_fraction":0.25},
					{"id":"3p-monthly","window":"monthly","remaining_fraction":1}
				]}
			]
		}
	}
}`

func TestParseUsageUsesGeminiBuckets(t *testing.T) {
	windows, err := parseUsage([]byte(usageResponse))
	if err != nil {
		t.Fatal(err)
	}
	if got := windows[model.WindowWeekly]; !got.Known || got.RemainingPercent() != 87.5 ||
		!got.ResetsAt.Equal(time.Date(2026, 12, 4, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("Gemini weekly quota = %+v", got)
	}
	if got := windows[model.Window5h]; !got.Known || got.RemainingPercent() != 0 {
		t.Errorf("exhausted Gemini quota = %+v", got)
	}
	if len(windows) != 2 {
		t.Errorf("windows = %+v, want only reported Gemini windows", windows)
	}
}

func TestParseUsageNeedsAQuotaResponse(t *testing.T) {
	for _, raw := range []string{
		`{"status":"SUCCESS","response":"unstructured text"}`,
		`{"status":"ERROR","command":{"name":"usage"}}`,
		`{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[]}}}`,
	} {
		if windows, err := parseUsage([]byte(raw)); err == nil || len(windows) > 0 {
			t.Errorf("parseUsage(%q) = %+v, %v", raw, windows, err)
		}
	}
}

func TestCollectRefreshesAtResetAndOnDemand(t *testing.T) {
	start := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	calls := 0
	p := &Provider{query: func() ([]byte, error) {
		calls++
		if calls == 3 {
			return nil, errors.New("test failure")
		}
		return []byte(usageResponse), nil
	}}
	if status := p.Collect(start); status.Err != "" || status.ID != model.SourceAntigravity {
		t.Fatalf("first collect = %+v", status)
	}
	p.Collect(start.Add(time.Minute))
	if calls != 1 {
		t.Fatalf("quota was queried %d times before the reset", calls)
	}
	reset := start.Add(3 * time.Minute)
	p.Collect(reset)
	if calls != 2 {
		t.Fatalf("quota was queried %d times through the reset", calls)
	}
	p.RequestAuthoritative()
	if status := p.Collect(reset); status.Err == "" || len(status.Windows) != 0 || calls != 3 {
		t.Fatalf("manual refresh = %+v after %d calls", status, calls)
	}
	p.Collect(reset.Add(time.Second))
	if calls != 3 {
		t.Fatalf("quota was queried %d times before the retry interval", calls)
	}
	if status := p.Collect(reset.Add(time.Minute)); status.Err != "" || calls != 4 {
		t.Fatalf("retry = %+v after %d calls", status, calls)
	}
}
