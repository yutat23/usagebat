// Package i18n contains usagebat's small, typed English/Japanese message
// catalog. Configuration keys and diagnostic strings intentionally stay in
// English; this package is for user-facing tray and notification text.
package i18n

import (
	"fmt"
	"strings"
	"time"

	"github.com/yutat23/usagebat/internal/model"
)

const (
	Auto = "auto"
	EN   = "en"
	JA   = "ja"
)

type Printer struct{ lang string }

func New(setting string) Printer {
	lang := setting
	if lang == "" || lang == Auto {
		lang = systemLanguage()
	}
	if lang != JA {
		lang = EN
	}
	return Printer{lang: lang}
}

func (p Printer) Language() string { return p.lang }
func (p Printer) Japanese() bool   { return p.lang == JA }

var messages = map[string][2]string{
	"iconStyle":       {"Icon style", "アイコン表示"},
	"modeBoth":        {"Battery + %", "バッテリー + %"},
	"modeBattery":     {"Battery only", "バッテリーのみ"},
	"modePercent":     {"% only", "%のみ"},
	"servicesShown":   {"Services shown in icon", "アイコンに表示するサービス"},
	"claudeLimits":    {"Claude Code limits", "Claude Codeの制限"},
	"codexLimits":     {"Codex limits", "Codexの制限"},
	"shortest":        {"Shortest available", "最短の制限"},
	"launchAtStartup": {"Launch at startup", "OS起動時に自動起動"},
	"refreshNow":      {"Refresh now", "今すぐ更新"},
	"openConfig":      {"Open config file…", "設定ファイルを開く…"},
	"viewOnGitHub":    {"View on GitHub…", "GitHubで見る…"},
	"settings":        {"Settings…", "設定…"},
	"settingsTitle":   {"usagebat", "usagebat"},
	"checkForUpdates": {"Check for updates", "更新を確認する"},
	"upToDate":        {"Up to date", "最新です"},
	"chartRemaining":  {"Headroom over time", "残量の推移"},
	"chartTokens":     {"Token usage", "トークン消費量"},
	"chartActivity":   {"When you use it", "時間帯別の使用傾向"},
	"chartsPending":   {"Charts appear once usagebat has collected a few hours of history.", "数時間ぶん記録するとグラフが表示されます。"},
	"recordHistory":   {"Record usage history on this machine", "使用履歴をこの端末に記録する"},
	"historyDetail":   {"Kept locally and pruned automatically; nothing is uploaded.", "ローカルにのみ保存し、古いものは自動削除します。外部送信はありません。"},
	"updateDetail":    {"Reads a release number from GitHub. Nothing about you is sent.", "GitHubからリリース番号のみ取得します。利用者の情報は送信しません。"},
	"lastDays":        {"Last 7 days", "直近7日間"},
	"chartNoData":     {"No data", "データなし"},
	"quit":            {"Quit", "終了"},
	"language":        {"Language / 言語", "言語 / Language"},
	"systemDefault":   {"System default", "システム設定"},
	"english":         {"English", "English"},
	"japanese":        {"日本語", "日本語"},
	"notifications":   {"Banked reset expiry notifications", "banked resetの期限切れ通知"},
	"weekly":          {"Weekly", "週間"},
	"monthly":         {"Monthly", "月間"},
	"estimated":       {"(est)", "（推定）"},
	"noData":          {"usagebat: no data", "usagebat：データなし"},
	"latestSession":   {"latest session", "最新セッション"},
	"input":           {"in", "入力"},
	"output":          {"out", "出力"},
	"cache":           {"cache", "キャッシュ"},
	"weighted":        {"weighted", "加重"},
	"resetTitle":      {"Codex reset expires soon", "Codexのリセット期限が近づいています"},
}

func (p Printer) T(key string) string {
	pair, ok := messages[key]
	if !ok {
		return key
	}
	if p.Japanese() {
		return pair[1]
	}
	return pair[0]
}

func (p Printer) WindowTitle(w model.Window) string {
	switch w {
	case model.Window5h:
		return "5h"
	case model.WindowWeekly:
		return p.T("weekly")
	case model.WindowMonthly:
		return p.T("monthly")
	}
	return string(w)
}

func (p Printer) FormatReset(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := t.Sub(now)
	local := t.Local()
	if !p.Japanese() {
		switch {
		case d < 0:
			return "resetting now"
		case d < time.Hour:
			return fmt.Sprintf("resets in %dm (%s)", int(d.Minutes()), local.Format("15:04"))
		case d < 24*time.Hour:
			return fmt.Sprintf("resets in %dh%02dm (%s)", int(d.Hours()), int(d.Minutes())%60, local.Format("15:04"))
		case d < 7*24*time.Hour:
			return fmt.Sprintf("resets in %dd%dh (%s)", int(d.Hours()/24), int(d.Hours())%24, local.Format("Mon 15:04"))
		default:
			return "resets " + local.Format("Jan 2 15:04")
		}
	}
	switch {
	case d < 0:
		return "まもなくリセット"
	case d < time.Hour:
		return fmt.Sprintf("あと%d分でリセット（%s）", int(d.Minutes()), local.Format("15:04"))
	case d < 24*time.Hour:
		return fmt.Sprintf("あと%d時間%02d分でリセット（%s）", int(d.Hours()), int(d.Minutes())%60, local.Format("15:04"))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("あと%d日%d時間でリセット（%s）", int(d.Hours()/24), int(d.Hours())%24, japaneseWeekday(local))
	default:
		return fmt.Sprintf("%d月%d日 %sにリセット", local.Month(), local.Day(), local.Format("15:04"))
	}
}

func japaneseWeekday(t time.Time) string {
	days := [...]string{"日", "月", "火", "水", "木", "金", "土"}
	return fmt.Sprintf("%s %s", days[t.Weekday()], t.Format("15:04"))
}

func (p Printer) BankedResets(count int, expires time.Time, now time.Time) string {
	if !p.Japanese() {
		label := "reset"
		if count != 1 {
			label = "resets"
		}
		line := fmt.Sprintf("Banked %s  %d available", label, count)
		if !expires.IsZero() {
			line += " · expires " + p.relativeExpiry(expires, now)
		}
		return line
	}
	line := fmt.Sprintf("Banked reset  %d回利用可能", count)
	if !expires.IsZero() {
		line += " · " + p.relativeExpiry(expires, now) + "に期限切れ"
	}
	return line
}

func (p Printer) relativeExpiry(expires, now time.Time) string {
	d := expires.Sub(now)
	if d <= 0 {
		if p.Japanese() {
			return "期限切れ"
		}
		return "now"
	}
	if d < 48*time.Hour {
		h := int(d.Hours())
		if p.Japanese() {
			return fmt.Sprintf("あと%d時間", h)
		}
		return fmt.Sprintf("in %dh", h)
	}
	days := int(d.Hours() / 24)
	if p.Japanese() {
		return fmt.Sprintf("あと%d日", days)
	}
	return fmt.Sprintf("in %dd", days)
}

func (p Printer) ResetNotification(count int, expires, now time.Time) (string, string) {
	remaining := p.relativeExpiry(expires, now)
	when := expires.Local()
	if p.Japanese() {
		body := fmt.Sprintf("banked reset %d回分が%sで期限切れになります\n期限：%d月%d日 %s",
			count, remaining, when.Month(), when.Day(), when.Format("15:04"))
		return p.T("resetTitle"), body
	}
	unit, verb := "reset", "expires"
	if count != 1 {
		unit, verb = "resets", "expire"
	}
	body := fmt.Sprintf("%d banked %s %s %s\nExpires %s", count, unit, verb, remaining,
		when.Format("Jan 2 at 15:04"))
	return p.T("resetTitle"), body
}

// Weekdays are the heatmap's row labels, Sunday first.
func (p Printer) Weekdays() [7]string {
	if p.Japanese() {
		return [7]string{"日", "月", "火", "水", "木", "金", "土"}
	}
	return [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
}

// UpdateAvailable labels the row that opens a newer release.
func (p Printer) UpdateAvailable(version string) string {
	if p.Japanese() {
		return fmt.Sprintf("バージョン %s があります…", version)
	}
	return fmt.Sprintf("Version %s available…", version)
}

func (p Printer) TranslateNote(note string) string {
	if !p.Japanese() {
		return note
	}
	replacements := []struct{ from, to string }{
		{"latest session", p.T("latestSession")},
		{"reported by Claude usage cache", "Claude使用量キャッシュから取得"},
		{"/usage (last good reading)", "/usage（前回の取得値）"},
		{"reported by /usage", "/usageから取得"},
		{"estimated from local transcripts", "ローカル履歴から推定"},
		{"estimated — ", "推定 — "},
		{"live via Codex", "Codexから取得"},
		{"rollout fallback", "ログから取得"},
		{"plan: ", "プラン: "},
		{"unlimited credits", "クレジット無制限"},
	}
	for _, replacement := range replacements {
		note = strings.ReplaceAll(note, replacement.from, replacement.to)
	}
	return note
}
