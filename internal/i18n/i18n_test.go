package i18n

import (
	"strings"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/model"
)

func TestExplicitLanguages(t *testing.T) {
	if got := New("en").T("refreshNow"); got != "Refresh now" {
		t.Fatalf("en = %q", got)
	}
	if got := New("ja").T("refreshNow"); got != "今すぐ更新" {
		t.Fatalf("ja = %q", got)
	}
	if got := New("unknown").Language(); got != EN {
		t.Fatalf("fallback = %q", got)
	}
}

func TestJapaneseResetAndNotificationFormatting(t *testing.T) {
	now := time.Date(2026, 8, 2, 14, 0, 0, 0, time.Local)
	expires := now.Add(23*time.Hour + 20*time.Minute)
	p := New("ja")
	if got := p.WindowTitle(model.WindowWeekly); got != "週間" {
		t.Fatalf("window = %q", got)
	}
	if got := p.FormatReset(now.Add(2*time.Hour), now); !strings.Contains(got, "あと2時間") {
		t.Fatalf("reset = %q", got)
	}
	title, body := p.ResetNotification(1, expires, now)
	if title != "Codexのリセット期限が近づいています" || !strings.Contains(body, "あと23時間") {
		t.Fatalf("notification = %q / %q", title, body)
	}
}

func TestChartHoverFormatting(t *testing.T) {
	at := time.Date(2026, 8, 13, 22, 5, 0, 0, time.UTC)
	if got := New("ja").ChartRemainingHover(at, 51); got != "8/13 22:05 · 残り51%" {
		t.Errorf("ja remaining = %q", got)
	}
	if got := New("en").ChartRemainingHover(at, 51); got != "8/13 22:05 · 51% left" {
		t.Errorf("en remaining = %q", got)
	}
	if got := New("ja").ChartHeatHover("月", 9, 0); got != "月 9時 · なし" {
		t.Errorf("ja empty heat = %q", got)
	}
	if got := New("ja").ChartHeatHover("月", 9, 8); got != "月 9時 · 8%消費" {
		t.Errorf("ja heat = %q", got)
	}
	if got := New("en").ChartHeatHover("Mon", 9, 8); got != "Mon 09:00 · 8% used" {
		t.Errorf("en heat = %q", got)
	}
	if got := New("en").T("chartActivityUnit"); got != "% of the limit used that hour" {
		t.Errorf("activity unit = %q", got)
	}
	if got := New("en").ChartTokensHover(at, "12K"); got != "8/13 22:05 · 12K" {
		t.Errorf("en tokens = %q", got)
	}
}

func TestEnglishPluralResetFormatting(t *testing.T) {
	now := time.Now()
	_, body := New("en").ResetNotification(2, now.Add(20*time.Hour), now)
	if !strings.Contains(body, "2 banked resets expire") {
		t.Fatalf("body = %q", body)
	}
	_, body = New("en").ResetNotification(1, now.Add(20*time.Hour), now)
	if !strings.Contains(body, "1 banked reset expires") {
		t.Fatalf("singular body = %q", body)
	}
}
