// Package webui serves usagebat's settings screen to the user's browser.
//
// The tray menu can only hold flat rows of text, and a native window would
// mean writing and maintaining one for each platform. A page served over the
// loopback interface is a single implementation that looks the same on macOS
// and Windows.
//
// Nothing here is reachable from outside the machine: the listener binds to
// 127.0.0.1, it only starts when the user asks for the settings screen, it
// stops again once the screen goes unused, and every request has to carry a
// token minted for that one session.
package webui

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Kind is how a row behaves.
type Kind int

const (
	// KindToggle is an independent on/off setting.
	KindToggle Kind = iota
	// KindRadio is one of a set where picking one clears the others.
	KindRadio
	// KindAction does something instead of holding a value.
	KindAction
	// KindText is explanatory copy.
	KindText
	// KindChart is a rendered chart, inlined as SVG.
	KindChart
)

// Row is one setting.
type Row struct {
	// ID is handed back to Activate. It is the same identifier the tray menu
	// uses, so a setting has one implementation rather than two.
	ID      string
	Label   string
	Detail  string
	Kind    Kind
	Checked bool
	// Group ties radios together; rows sharing a group are mutually exclusive.
	Group string
	// Href makes an action open a link rather than post back.
	Href string
	// Indent nests a row under the one above it.
	Indent int
	// SVG is the chart markup for a KindChart row. It is inlined rather than
	// escaped, so it must come from the renderer and never from user input.
	SVG template.HTML
}

// Section is a titled block of rows.
type Section struct {
	Title string
	Rows  []Row
	// Aside puts the section in the narrow column beside the charts. Settings
	// go there: they are what you occasionally come to change, next to the
	// numbers you came to look at.
	Aside bool
	// Grid flows the rows as tiles rather than a list, which is what lets
	// several charts share a row instead of each taking the full width.
	Grid bool
}

// Page is the screen. Everything is on one of them: the charts and the
// settings sit side by side, so changing a setting and seeing what it did does
// not mean navigating between two places.
type Page struct {
	Title    string
	Version  string
	Sections []Section
	// Footer is a closing line, typically the project link.
	Footer     string
	FooterHref string
}

// Main returns the sections that fill the wide column.
func (p Page) Main() []Section { return p.column(false) }

// Aside returns the sections that fill the narrow column.
func (p Page) Aside() []Section { return p.column(true) }

func (p Page) column(aside bool) []Section {
	var out []Section
	for _, section := range p.Sections {
		if section.Aside == aside {
			out = append(out, section)
		}
	}
	return out
}

// Server owns the loopback listener and the pages it serves.
type Server struct {
	// Render produces the page. It runs per request, so the screen can never
	// show a value the config no longer holds.
	Render func() Page
	// Activate applies the row the user clicked and returns once the change is
	// saved, so the redirect that follows renders the new state.
	Activate func(id string)
	// IdleTimeout stops the listener after this long with no requests. Zero
	// picks a sensible default.
	IdleTimeout time.Duration

	mu       sync.Mutex
	listener net.Listener
	token    string
	lastSeen time.Time
	stop     chan struct{}
}

const defaultIdleTimeout = 15 * time.Minute

// Open starts the server if it is not already running and returns the URL to
// point a browser at.
func (s *Server) Open() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		if err := s.startLocked(); err != nil {
			return "", err
		}
	}
	s.lastSeen = time.Now()
	// The token rides in the query only on this first hop; the page swaps it
	// for a cookie and redirects, so it never lingers in the address bar or
	// leaks through a Referer header.
	return fmt.Sprintf("http://%s/?t=%s", s.listener.Addr().String(), s.token), nil
}

func (s *Server) startLocked() error {
	// Port zero: the operating system hands back a free one, so usagebat can
	// never collide with whatever else the user is running.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	token, err := newToken()
	if err != nil {
		listener.Close()
		return err
	}
	s.listener, s.token, s.stop = listener, token, make(chan struct{})

	server := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	go s.watchIdle(server, s.stop)
	return nil
}

// Close stops serving. It is safe to call when nothing is running.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked()
}

func (s *Server) closeLocked() error {
	if s.listener == nil {
		return nil
	}
	close(s.stop)
	err := s.listener.Close()
	s.listener, s.token, s.stop = nil, "", nil
	return err
}

// Running reports whether the listener is up.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listener != nil
}

// Addr is the listening address, empty when stopped. Tests use it; callers
// should use the URL from Open.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Server) idleTimeout() time.Duration {
	if s.IdleTimeout > 0 {
		return s.IdleTimeout
	}
	return defaultIdleTimeout
}

// watchIdle closes the listener once the screen has gone unused. A tray app
// runs for weeks; holding a port open for all of it would be rude, and it is
// the kind of thing that shows up in someone's netstat and worries them.
func (s *Server) watchIdle(server *http.Server, stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			_ = server.Close()
			return
		case <-ticker.C:
			s.mu.Lock()
			idle := time.Since(s.lastSeen) > s.idleTimeout()
			if idle {
				_ = s.closeLocked()
			}
			s.mu.Unlock()
			if idle {
				_ = server.Close()
				return
			}
		}
	}
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

const cookieName = "usagebat_session"

var errUnauthorized = errors.New("unauthorized")

// authorize enforces everything that keeps this listener private, and reports
// whether the caller has already handled the response.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	token, addr := s.token, ""
	if s.listener != nil {
		addr = s.listener.Addr().String()
	}
	s.lastSeen = time.Now()
	s.mu.Unlock()

	if token == "" || addr == "" {
		http.Error(w, "closed", http.StatusServiceUnavailable)
		return false
	}
	// A page on the internet can point its own hostname at 127.0.0.1 and have
	// the browser reach this port. The browser still sends that hostname as
	// Host, so rejecting anything but our own address stops it.
	if r.Host != addr {
		http.Error(w, errUnauthorized.Error(), http.StatusForbidden)
		return false
	}
	if cookie, err := r.Cookie(cookieName); err == nil && subtleEqual(cookie.Value, token) {
		return true
	}
	// First arrival: trade the token in the query for a cookie, then send the
	// browser to the same path without it.
	if subtleEqual(r.URL.Query().Get("t"), token) {
		http.SetCookie(w, &http.Cookie{
			Name: cookieName, Value: token, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
		http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
		return false
	}
	http.Error(w, errUnauthorized.Error(), http.StatusForbidden)
	return false
}

// subtleEqual compares in constant time so a caller cannot learn the token one
// character at a time.
func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		// The pattern "/" also matches everything below it.
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		s.renderPage(w)
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		io.WriteString(w, appJS)
	})
	mux.HandleFunc("/apply", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "post only", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if id := strings.TrimSpace(r.PostForm.Get("id")); id != "" && s.Activate != nil {
			// Activate returns once the change is saved, so whichever answer
			// goes back below renders the new state rather than the old one.
			s.Activate(id)
		}
		if r.Header.Get("X-Requested-With") == "fetch" {
			// The script fetches the page itself afterwards; sending it a
			// redirect here would just make it fetch twice.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Post-redirect-get: reloading after a change must not repeat it. The
		// target is fixed rather than taken from the request, so a crafted
		// post cannot turn this into a way to send the browser elsewhere.
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	return mux
}
