package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testChecker(t *testing.T, handler http.HandlerFunc) *Checker {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := New()
	c.endpoint = server.URL
	return c
}

func TestCheckReportsANewerRelease(t *testing.T) {
	c := testChecker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("GitHub rejects requests without a User-Agent")
		}
		w.Write([]byte(`{"tag_name":"v0.6.0","html_url":"https://example.test/v0.6.0"}`))
	})

	release, err := c.Check(context.Background(), "0.5.2")
	if err != nil {
		t.Fatal(err)
	}
	if release == nil || release.Version != "0.6.0" {
		t.Fatalf("release = %+v, want 0.6.0", release)
	}
	if got := c.Latest(); got == nil || got.URL != "https://example.test/v0.6.0" {
		t.Fatalf("Latest() = %+v, want the release page", got)
	}
}

func TestCheckStaysQuietOnTheCurrentRelease(t *testing.T) {
	c := testChecker(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.5.2"}`))
	})

	release, err := c.Check(context.Background(), "0.5.2")
	if err != nil {
		t.Fatal(err)
	}
	if release != nil || c.Latest() != nil {
		t.Fatalf("running the newest release must not report an update: %+v", release)
	}
}

// A developer running `go run ./cmd/usagebat` has version "dev". Telling them
// every published release is an update would be noise.
func TestCheckStaysQuietOnAnUnreleasedBuild(t *testing.T) {
	c := testChecker(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.6.0"}`))
	})

	release, err := c.Check(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if release != nil {
		t.Fatalf("an unreleased build must not be told to update: %+v", release)
	}
}

func TestCheckSurfacesAFailedRequest(t *testing.T) {
	c := testChecker(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	})

	if _, err := c.Check(context.Background(), "0.5.2"); err == nil {
		t.Fatal("a 403 should be reported, not treated as up to date")
	}
}

// The refresh loop runs every minute; the check must not follow it.
func TestBeginPacesRequests(t *testing.T) {
	c := New()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	const every = 24 * time.Hour

	if !c.Begin(now, every) {
		t.Fatal("the first check is always due")
	}
	if c.Begin(now.Add(time.Minute), every) {
		t.Fatal("a second check a minute later must be refused")
	}
	if c.Begin(now.Add(23*time.Hour), every) {
		t.Fatal("a check inside the interval must be refused")
	}
	if !c.Begin(now.Add(25*time.Hour), every) {
		t.Fatal("a check past the interval is due")
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		candidate, current string
		want               bool
	}{
		{"0.6.0", "0.5.2", true},
		{"0.5.3", "0.5.2", true},
		{"1.0.0", "0.9.9", true},
		{"0.5.2", "0.5.2", false},
		{"0.5.1", "0.5.2", false},
		{"0.10.0", "0.9.0", true}, // not a string comparison
		{"v0.6.0", "0.5.2", true},
		{"0.6.0", "dev", false},
		{"0.6.0", "0.0.0-20260803120000-abcdef123456", true},
		{"garbage", "0.5.2", false},
		{"0.5.2.1", "0.5.2", true},
	} {
		if got := newer(tc.candidate, tc.current); got != tc.want {
			t.Errorf("newer(%q, %q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
		}
	}
}
