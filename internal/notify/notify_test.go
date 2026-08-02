package notify

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/i18n"
	"github.com/yutat23/usagebat/internal/model"
)

func resetSnapshot(now time.Time, expires time.Duration) *model.Snapshot {
	return &model.Snapshot{Sources: []model.SourceStatus{{
		ID: model.SourceCodex,
		RateLimitResets: &model.RateLimitResetCredits{AvailableCount: 1, Credits: []model.RateLimitResetCredit{{
			ID: "credit-1", Status: "available", ExpiresAt: now.Add(expires),
		}}},
	}}}
}

func TestDueSendsSevenDayThenOneDayOnce(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	engine := newAt(filepath.Join(t.TempDir(), "state.json"))
	settings := config.BankedResetExpiry{Enabled: true, ThresholdHours: []int{168, 24}}
	snap := resetSnapshot(now, 6*24*time.Hour)
	events := engine.Due(snap, settings, i18n.New("en"), now)
	if len(events) != 1 {
		t.Fatalf("7d events = %d", len(events))
	}
	if err := engine.Mark(events[0], now); err != nil {
		t.Fatal(err)
	}
	if got := engine.Due(snap, settings, i18n.New("en"), now); len(got) != 0 {
		t.Fatalf("duplicate = %d", len(got))
	}

	withinDay := now.Add(5*24*time.Hour + time.Hour)
	events = engine.Due(snap, settings, i18n.New("en"), withinDay)
	if len(events) != 1 {
		t.Fatalf("24h events = %d", len(events))
	}
	if err := engine.Mark(events[0], withinDay); err != nil {
		t.Fatal(err)
	}
	if got := engine.Due(snap, settings, i18n.New("en"), withinDay); len(got) != 0 {
		t.Fatalf("24h duplicate = %d", len(got))
	}
}

func TestStartupInsideOneDayProducesOneEventAndMarksBothStages(t *testing.T) {
	now := time.Now()
	engine := newAt(filepath.Join(t.TempDir(), "state.json"))
	settings := config.BankedResetExpiry{Enabled: true, ThresholdHours: []int{168, 24}}
	snap := resetSnapshot(now, 12*time.Hour)
	events := engine.Due(snap, settings, i18n.New("ja"), now)
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	if err := engine.Mark(events[0], now); err != nil {
		t.Fatal(err)
	}
	if got := engine.Due(snap, settings, i18n.New("ja"), now); len(got) != 0 {
		t.Fatalf("catch-up duplicate = %d", len(got))
	}
}

func TestNoExpiryDetailMeansNoNotification(t *testing.T) {
	now := time.Now()
	snap := &model.Snapshot{Sources: []model.SourceStatus{{RateLimitResets: &model.RateLimitResetCredits{AvailableCount: 2}}}}
	got := newAt("").Due(snap, config.BankedResetExpiry{Enabled: true, ThresholdHours: []int{168}}, i18n.New("en"), now)
	if len(got) != 0 {
		t.Fatalf("events = %d", len(got))
	}
}
