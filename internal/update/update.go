// Package update asks GitHub whether a newer usagebat release exists.
//
// This is the only outbound network request usagebat makes, so it stays off
// until the user turns it on, it never reports anything about the machine it
// runs on, and it only ever reads a release number.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultEndpoint returns the newest published release, excluding drafts and
// prereleases.
const DefaultEndpoint = "https://api.github.com/repos/yutat23/usagebat/releases/latest"

// Release is a published release newer than the running build.
type Release struct {
	Version string
	URL     string
}

// Checker remembers the newest release it has seen and paces its own requests.
// The zero value is not usable; call New.
type Checker struct {
	endpoint string
	client   *http.Client

	mu sync.Mutex
	// lastAttempt is when a check was last claimed, successful or not. A failed
	// check waits out the full interval rather than retrying: an unreachable
	// GitHub is not worth a request every refresh.
	lastAttempt time.Time
	latest      *Release
}

func New() *Checker {
	return &Checker{
		endpoint: DefaultEndpoint,
		// Long enough for a slow link, short enough that a hung connection
		// cannot pin a goroutine for the rest of the session.
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Begin reports whether a check is due and claims it in the same step, so two
// refreshes cannot put two requests in flight.
func (c *Checker) Begin(now time.Time, every time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.lastAttempt.IsZero() && now.Sub(c.lastAttempt) < every {
		return false
	}
	c.lastAttempt = now
	return true
}

// Latest is the newest release seen so far, or nil while the running build is
// the newest known one.
func (c *Checker) Latest() *Release {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.latest == nil {
		return nil
	}
	release := *c.latest
	return &release
}

// Check fetches the latest release and records it when it is newer than
// current. A current version that is not a release number — a `go run` build,
// or a pseudo-version from an untagged `go install` — never reports an update:
// there is nothing meaningful to compare against.
func (c *Checker) Check(ctx context.Context, current string) (*Release, error) {
	release, err := c.fetch(ctx)
	if err != nil {
		return nil, err
	}
	if !newer(release.Version, current) {
		return nil, nil
	}
	c.mu.Lock()
	c.latest = release
	c.mu.Unlock()
	return release, nil
}

func (c *Checker) fetch(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	// GitHub rejects requests without a User-Agent.
	req.Header.Set("User-Agent", "usagebat")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github returned %s", resp.Status)
	}
	// A malformed or hostile response must not be able to exhaust memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decoding the release: %w", err)
	}
	version := strings.TrimPrefix(strings.TrimSpace(payload.TagName), "v")
	if version == "" {
		return nil, fmt.Errorf("the release has no tag name")
	}
	url := payload.HTMLURL
	if url == "" {
		url = "https://github.com/yutat23/usagebat/releases/latest"
	}
	return &Release{Version: version, URL: url}, nil
}

// newer compares two dotted release numbers. Anything that is not one — "dev",
// a pseudo-version — makes the comparison false rather than true, so an
// unreleased build never nags its developer about an update.
func newer(candidate, current string) bool {
	a, ok := parse(candidate)
	if !ok {
		return false
	}
	b, ok := parse(current)
	if !ok {
		return false
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return len(a) > len(b)
}

func parse(version string) ([]int, bool) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" {
		return nil, false
	}
	// A prerelease or build suffix plays no part in the comparison; "0.6.0-rc1"
	// and "0.6.0" are close enough that offering the release is still right.
	if cut := strings.IndexAny(version, "-+"); cut >= 0 {
		version = version[:cut]
	}
	fields := strings.Split(version, ".")
	out := make([]int, 0, len(fields))
	for _, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
