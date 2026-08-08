package webui

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Server, string, *http.Client) {
	t.Helper()
	s := &Server{Render: func() Page {
		return Page{Title: "usagebat", Version: "0.6.0", OverviewLabel: "Overview",
			SettingsLabel: "Settings sections", SavedLabel: "Saved", SaveErrorLabel: "Could not save",
			Sections: []Section{
				{Title: "Headroom", Grid: true,
					Rows: []Row{{Label: "Claude Code · 5h", Kind: KindChart}}},
				{Title: "Icon", Aside: true, CategoryID: "general", CategoryTitle: "General", Rows: []Row{
					{ID: "mode:both", Label: "Battery + %", Kind: KindRadio, Checked: true},
					{ID: "history", Label: "Record history", Kind: KindToggle},
				}},
			}}
	}}
	t.Cleanup(func() { s.Close() })
	raw, err := s.Open()
	if err != nil {
		t.Fatal(err)
	}
	jar := &cookieJar{}
	return s, raw, &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// cookieJar is the smallest jar that keeps the session cookie across requests.
type cookieJar struct {
	mu      sync.Mutex
	cookies []*http.Cookie
}

func (j *cookieJar) SetCookies(_ *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cookies = append(j.cookies, cookies...)
}

func (j *cookieJar) Cookies(*url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cookies
}

func TestOpenPicksAFreePortAndBindsToLoopback(t *testing.T) {
	s, raw, _ := testServer(t)
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if host, _, _ := strings.Cut(parsed.Host, ":"); host != "127.0.0.1" {
		t.Fatalf("listening on %q; anything but loopback is reachable from the network", parsed.Host)
	}
	if parsed.Query().Get("t") == "" {
		t.Error("the first URL has to carry the session token")
	}
	if !s.Running() {
		t.Error("Open did not start the server")
	}

	// A second Open must reuse the running listener rather than leaking a port.
	again, err := s.Open()
	if err != nil {
		t.Fatal(err)
	}
	if u2, _ := url.Parse(again); u2.Host != parsed.Host {
		t.Errorf("second Open moved to %s, want the running %s", u2.Host, parsed.Host)
	}
}

// The token is traded for a cookie on the first hop, so it stops appearing in
// the address bar and cannot leak through a Referer header.
func TestFirstRequestExchangesTheTokenForACookie(t *testing.T) {
	s, raw, client := testServer(t)
	resp, err := client.Get(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect that drops the token", resp.StatusCode)
	}
	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == cookieName {
			found = true
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Errorf("session cookie = %+v, want HttpOnly and SameSite=Strict", c)
			}
		}
	}
	if !found {
		t.Fatal("no session cookie was set")
	}

	page := get(t, client, "http://"+s.Addr()+"/")
	if !strings.Contains(page, "Record history") {
		t.Errorf("page did not render the rows:\n%s", page)
	}
}

func TestRequestWithoutTheTokenIsRefused(t *testing.T) {
	_, raw, _ := testServer(t)
	base := strings.Split(raw, "?")[0]

	resp, err := http.Get(base)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a request with no token", resp.StatusCode)
	}

	resp2, err := http.Get(base + "?t=wrong-token")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a wrong token", resp2.StatusCode)
	}
}

// A page on the internet can resolve its own hostname to 127.0.0.1 and reach
// this port. The browser still sends that hostname, so the Host check is what
// stops it.
func TestForeignHostHeaderIsRefused(t *testing.T) {
	s, raw, client := testServer(t)
	// Establish the cookie first, so only the Host header is in question.
	client.Get(raw)

	req, err := http.NewRequest(http.MethodGet, "http://"+s.Addr()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a rebound hostname", resp.StatusCode)
	}
}

func TestApplyPostsTheRowIdAndRedirects(t *testing.T) {
	s, raw, client := testServer(t)
	var mu sync.Mutex
	var got []string
	s.Activate = func(id, value string) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, id+"="+value)
		return nil
	}
	client.Get(raw)

	resp, err := client.PostForm("http://"+s.Addr()+"/apply", url.Values{
		"id": {"setting:refreshSeconds"}, "value": {"90"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Post-redirect-get: a reload must not repeat the change.
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "setting:refreshSeconds=90" {
		t.Fatalf("activated %v, want the field id and value", got)
	}
}

// Charts and settings keep separate containers so CSS can give charts the
// full width and lay settings out as responsive cards below.
func TestPageSplitsChartsFromSettings(t *testing.T) {
	s, raw, client := testServer(t)
	client.Get(raw)
	page := get(t, client, "http://"+s.Addr()+"/")

	for _, want := range []string{
		`class="col main"`, `class="tabbar"`, `class="settings-grid"`, `class="rows grid"`,
		`data-tab-panel="overview"`, `max-width: 72rem`,
		`columns: 18rem`, `break-inside: avoid`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %s", want)
		}
	}
}

func TestSettingsGroupsPreserveCategoryAndSectionOrder(t *testing.T) {
	page := Page{Sections: []Section{
		{Title: "Chart"},
		{Title: "Icon", Aside: true, CategoryID: "general", CategoryTitle: "General"},
		{Title: "Language", Aside: true, CategoryID: "general", CategoryTitle: "General"},
		{Title: "Colors", Aside: true, CategoryID: "appearance", CategoryTitle: "Appearance"},
	}}
	groups := page.SettingsGroups()
	if len(groups) != 2 || groups[0].ID != "general" || groups[1].ID != "appearance" {
		t.Fatalf("groups = %+v", groups)
	}
	if len(groups[0].Sections) != 2 || groups[0].Sections[0].Title != "Icon" ||
		groups[0].Sections[1].Title != "Language" {
		t.Fatalf("general sections = %+v", groups[0].Sections)
	}
}

func TestPageRendersEditableInputsAndSelects(t *testing.T) {
	s := &Server{Render: func() Page {
		return Page{Title: "usagebat", Sections: []Section{{Aside: true, Rows: []Row{
			{ID: "setting:refreshSeconds", Label: "Refresh interval", Kind: KindInput,
				InputType: "number", Value: "90", Min: "5", Max: "3600", SubmitLabel: "Save"},
			{ID: "setting:icon.windowsLayout", Label: "Windows layout", Kind: KindSelect,
				Value: "single", SubmitLabel: "Save", Options: []Option{
					{Value: "stack", Label: "Stack"}, {Value: "single", Label: "Single"},
				}},
		}}}}
	}}
	recorder := httptest.NewRecorder()
	s.renderPage(recorder)
	body := recorder.Body.String()
	for _, want := range []string{
		`name="value" type="number" value="90"`, `min="5"`, `max="3600"`,
		`<option value="single" selected>Single</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("editable settings page is missing %q:\n%s", want, body)
		}
	}
}

// Whatever a form claims, the change lands back on the one screen there is.
func TestApplyAlwaysReturnsToTheScreen(t *testing.T) {
	s, raw, client := testServer(t)
	s.Activate = func(string, string) error { return nil }
	client.Get(raw)

	for _, from := range []string{"", "/settings", "https://example.com/"} {
		resp, err := client.PostForm("http://"+s.Addr()+"/apply",
			url.Values{"id": {"history"}, "from": {from}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Location"); got != "/" {
			t.Errorf("from %q redirected to %q, want /", from, got)
		}
	}
}

func TestApplyRejectsGet(t *testing.T) {
	s, raw, client := testServer(t)
	client.Get(raw)
	resp, err := client.Get("http://" + s.Addr() + "/apply")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405: a link must not be able to change a setting", resp.StatusCode)
	}
}

func TestApplyReportsValidationErrors(t *testing.T) {
	s, raw, client := testServer(t)
	s.Activate = func(string, string) error { return errors.New("invalid setting value") }
	client.Get(raw)

	resp, err := client.PostForm("http://"+s.Addr()+"/apply", url.Values{
		"id": {"setting:refreshSeconds"}, "value": {"0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "invalid setting value") {
		t.Fatalf("validation error was not returned: %s", body)
	}
}

// The only script the page may run is the one this server serves. Nothing
// inline, nothing from anywhere else, and no network beyond the loopback
// origin the page was fetched from.
func TestPageAllowsOnlyItsOwnScript(t *testing.T) {
	_, raw, client := testServer(t)
	client.Get(raw)
	resp, err := client.Get(strings.Split(raw, "?")[0])
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	policy := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "connect-src 'self'"} {
		if !strings.Contains(policy, want) {
			t.Errorf("content-security-policy = %q, missing %q", policy, want)
		}
	}
	if strings.Contains(policy, "script-src 'unsafe-inline'") {
		t.Error("inline scripts are allowed; the page does not need them")
	}
	body, _ := io.ReadAll(resp.Body)
	for _, forbidden := range []string{"onchange=", "onclick=", "onsubmit="} {
		if strings.Contains(string(body), forbidden) {
			t.Errorf("page carries an inline handler (%s), which the policy blocks", forbidden)
		}
	}
}

// The script is only useful if the forms it enhances still work without it.
func TestFormsWorkWithoutTheScript(t *testing.T) {
	s, raw, client := testServer(t)
	s.Activate = func(string, string) error { return nil }
	client.Get(raw)

	page := get(t, client, "http://"+s.Addr()+"/")
	if !strings.Contains(page, `action="/apply"`) || !strings.Contains(page, "data-async") {
		t.Fatal("the rows are no longer plain forms marked for enhancement")
	}
	// A browser without the script posts the form and follows a redirect.
	resp, err := client.PostForm("http://"+s.Addr()+"/apply", url.Values{"id": {"history"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect for a plain form post", resp.StatusCode)
	}
}

// The script fetches the page itself afterwards; a redirect would make it
// fetch twice.
func TestFetchCallersGetNoContent(t *testing.T) {
	s, raw, client := testServer(t)
	s.Activate = func(string, string) error { return nil }
	client.Get(raw)

	req, err := http.NewRequest(http.MethodPost, "http://"+s.Addr()+"/apply",
		strings.NewReader("id=history"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "fetch")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for a fetch caller", resp.StatusCode)
	}
}

// The script is served from the same guarded origin as the page.
func TestScriptNeedsTheSession(t *testing.T) {
	s, raw, client := testServer(t)

	resp, err := http.Get("http://" + s.Addr() + "/app.js")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 without a session", resp.StatusCode)
	}

	client.Get(raw)
	body := get(t, client, "http://"+s.Addr()+"/app.js")
	for _, want := range []string{"data-async", "data-tab-target", "reportValidity", "window.location.hash"} {
		if !strings.Contains(body, want) {
			t.Errorf("served script is missing %q:\n%s", want, body)
		}
	}
}

func TestCloseStopsListening(t *testing.T) {
	s, raw, _ := testServer(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.Running() {
		t.Fatal("Running() is true after Close")
	}
	if _, err := http.Get(strings.Split(raw, "?")[0]); err == nil {
		t.Fatal("the port still answers after Close")
	}
	// Closing twice must not panic on the already-closed stop channel.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIdleTimeoutIsRespected(t *testing.T) {
	s := &Server{IdleTimeout: time.Hour}
	if got := s.idleTimeout(); got != time.Hour {
		t.Errorf("idleTimeout() = %v, want the configured hour", got)
	}
	s.IdleTimeout = 0
	if got := s.idleTimeout(); got != defaultIdleTimeout {
		t.Errorf("idleTimeout() = %v, want the default", got)
	}
}

func get(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
