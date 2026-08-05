package webui

import (
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/history"
	"github.com/yutat23/usagebat/internal/render"
)

// previewChart draws a real chart, both themes, exactly as the app does. The
// dependency on render is test-only; the package itself never imports it.
func previewChart() template.HTML {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	var points []history.Point
	remaining := 100.0
	for i := 0; i < 96; i++ {
		if remaining -= 3.5; remaining < 8 {
			remaining = 100
		}
		points = append(points, history.Point{
			At: start.Add(time.Duration(i) * 15 * time.Minute), Value: remaining,
		})
	}

	var markup strings.Builder
	for _, dark := range []bool{false, true} {
		class := "only-light"
		if dark {
			class = "only-dark"
		}
		chart := render.RemainingChart(points, render.ChartOptions{
			Palette: render.PaletteFrom(config.Default().Colors, dark),
			Dark:    dark, Width: 340, Height: 120, Location: time.UTC,
			Weekdays: [7]string{"日", "月", "火", "水", "木", "金", "土"},
			Label:    "Claude Code 5h headroom",
		})
		markup.WriteString(`<span class="` + class + `">` + chart.SVG + `</span>`)
	}
	return template.HTML(markup.String())
}

// samplePage stands in for a machine with both services configured, so the
// preview shows every kind of row the real screen can produce.
func samplePage() Page {
	return Page{
		Title:      "usagebat の設定",
		Version:    "0.6.0",
		Footer:     "GitHubで見る…",
		FooterHref: "https://github.com/yutat23/usagebat",
		Sections: []Section{
			{Title: "残量の推移 — 直近7日間", Grid: true, Rows: []Row{
				{Label: "Claude Code · 5h", Kind: KindChart, SVG: previewChart()},
				{Label: "Codex · 月間", Kind: KindChart, SVG: previewChart()},
			}},
			// One chart on its own has to come out the same size as one of two.
			{Title: "トークン消費量", Grid: true, Rows: []Row{
				{Label: "Claude Code · 5h", Kind: KindChart, SVG: previewChart()},
			}},
			{Title: "アイコン表示", Aside: true, Rows: []Row{
				{ID: "mode:both", Label: "バッテリー + %", Kind: KindRadio, Group: "mode", Checked: true},
				{ID: "mode:battery", Label: "バッテリーのみ", Kind: KindRadio, Group: "mode"},
				{ID: "mode:percent", Label: "%のみ", Kind: KindRadio, Group: "mode"},
			}},
			{Title: "アイコンに表示するサービス", Aside: true, Rows: []Row{
				{ID: "source:claude-code", Label: "Claude Code", Kind: KindToggle, Checked: true},
				{ID: "source:codex", Label: "Codex", Kind: KindToggle, Checked: true},
			}},
			{Title: "Claude Codeの制限", Aside: true, Rows: []Row{
				{ID: "limit:claude-code:auto", Label: "最短の制限", Kind: KindToggle, Checked: true},
				{ID: "limit:claude-code:5h", Label: "5h", Kind: KindToggle, Indent: 1},
				{ID: "limit:claude-code:weekly", Label: "週間", Kind: KindToggle, Indent: 1},
				{ID: "limit:claude-code:monthly", Label: "月間", Kind: KindToggle, Indent: 1},
			}},
			{Title: "言語 / Language", Aside: true, Rows: []Row{
				{ID: "language:auto", Label: "システム設定", Kind: KindRadio, Group: "lang", Checked: true},
				{ID: "language:en", Label: "English", Kind: KindRadio, Group: "lang"},
				{ID: "language:ja", Label: "日本語", Kind: KindRadio, Group: "lang"},
			}},
			{Aside: true, Rows: []Row{
				{ID: "notifications:banked-reset", Label: "banked resetの期限切れ通知",
					Kind: KindToggle, Checked: true},
				{ID: "autostart", Label: "OS起動時に自動起動", Kind: KindToggle, Checked: true},
				{ID: "history", Label: "使用履歴をこの端末に記録する", Kind: KindToggle, Checked: true,
					Detail: "ローカルにのみ保存し、古いものは自動削除します。外部送信はありません。"},
				{ID: "config", Label: "設定ファイルを開く…", Kind: KindAction},
			}},
		},
	}
}

// Writes the screen to build/ so its appearance can be reviewed in a
// browser. Nothing about how a page looks can be asserted in a test.
func TestWritePagePreview(t *testing.T) {
	if os.Getenv("USAGEBAT_PAGE_PREVIEW") == "" {
		t.Skip("set USAGEBAT_PAGE_PREVIEW=1 to write build/settings-preview.html")
	}
	s := &Server{Render: samplePage}
	t.Cleanup(func() { s.Close() })
	raw, err := s.Open()
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(raw)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	token := ""
	for _, c := range resp.Cookies() {
		if c.Name == cookieName {
			token = c.Value
		}
	}

	dir := filepath.Join("..", "..", "build")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	func() {
		req, _ := http.NewRequest(http.MethodGet, "http://"+s.Addr()+"/", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		page, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(page.Body)
		page.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		// The forms post back to a server that is gone by the time anyone
		// opens these files, and the tabs link to paths no file server will
		// answer, so the preview is for looks only.
		// The forms post back to a server that is gone by the time anyone
		// opens this file, so the preview is for looks only.
		out := strings.ReplaceAll(string(body), `action="/apply"`, `action="#"`)
		if err := os.WriteFile(filepath.Join(dir, "settings-preview.html"), []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}()
}
