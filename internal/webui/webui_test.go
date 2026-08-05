package webui

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Server, string, *http.Client) {
	t.Helper()
	s := &Server{Render: func() Page {
		return Page{Title: "usagebat", Version: "0.6.0", Sections: []Section{
			{Title: "Headroom", Grid: true,
				Rows: []Row{{Label: "Claude Code · 5h", Kind: KindChart}}},
			{Title: "Icon", Aside: true, Rows: []Row{
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
	s.Activate = func(id string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, id)
	}
	client.Get(raw)

	resp, err := client.PostForm("http://"+s.Addr()+"/apply", url.Values{"id": {"history"}})
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
	if len(got) != 1 || got[0] != "history" {
		t.Fatalf("activated %v, want [history]", got)
	}
}

// One screen, two columns: the charts go in the wide one and the settings
// beside them, which is what the layout hangs off.
func TestPageSplitsMainFromAside(t *testing.T) {
	s, raw, client := testServer(t)
	client.Get(raw)
	page := get(t, client, "http://"+s.Addr()+"/")

	for _, want := range []string{`class="col main"`, `class="col side"`, `class="rows grid"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %s", want)
		}
	}
}

// Whatever a form claims, the change lands back on the one screen there is.
func TestApplyAlwaysReturnsToTheScreen(t *testing.T) {
	s, raw, client := testServer(t)
	s.Activate = func(string) {}
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

// The page carries no script, so the policy can forbid one outright.
func TestPageForbidsScripts(t *testing.T) {
	_, raw, client := testServer(t)
	client.Get(raw)
	resp, err := client.Get(strings.Split(raw, "?")[0])
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	policy := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(policy, "default-src 'none'") {
		t.Errorf("content-security-policy = %q", policy)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "<script") || strings.Contains(string(body), "onchange=") {
		t.Error("the page grew a script; the policy above would block it")
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
