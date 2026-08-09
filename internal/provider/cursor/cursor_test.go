package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/model"
)

func testProvider(fetch func(context.Context, string, string) (*usage, error)) *Provider {
	return &Provider{
		cfg: config.Cursor{
			Enabled: true, TimeoutSeconds: 5, MinIntervalSeconds: 30, StaleAfterSeconds: 900,
			APIKey: "test-token",
		},
		token: resolveToken,
		fetch: fetch,
	}
}

func TestCollectReportsTheBillingCycleAsMonthly(t *testing.T) {
	cycleEnd := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	p := testProvider(func(context.Context, string, string) (*usage, error) {
		return &usage{
			planName: "Pro",
			cycleEnd: cycleEnd,
			included: &included{totalPercent: 62, autoPercent: 40, apiPercent: 2},
			onDemand: &onDemand{kind: onDemandFixed, usedCents: 315, limitCents: 5000},
		}, nil
	})

	st := p.Collect(time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC))
	if st.Err != "" {
		t.Fatalf("unexpected error: %s", st.Err)
	}
	if st.ID != model.SourceCursor || st.Name != "Cursor" {
		t.Fatalf("source identity = %q / %q", st.ID, st.Name)
	}
	// Cursor accounts per billing cycle and reports nothing shorter, so the
	// figure has to land on monthly or the icon draws a 5h cell it never fills.
	if len(st.Windows) != 1 {
		t.Fatalf("windows = %+v, want only the monthly one", st.Windows)
	}
	monthly := st.Windows[model.WindowMonthly]
	if !monthly.Known || monthly.UsedPercent != 62 {
		t.Fatalf("monthly = %+v", monthly)
	}
	if !monthly.ResetsAt.Equal(cycleEnd) {
		t.Fatalf("monthly resets at %v, want the cycle end %v", monthly.ResetsAt, cycleEnd)
	}
	if !strings.Contains(monthly.Detail, "auto 40%") || !strings.Contains(monthly.Detail, "api 2%") {
		t.Fatalf("detail = %q, want the auto/api split", monthly.Detail)
	}
	for _, want := range []string{"live via Cursor", "plan: Pro", "on-demand $3.15 / $50.00"} {
		if !strings.Contains(st.Note, want) {
			t.Fatalf("note = %q, want it to mention %q", st.Note, want)
		}
	}
}

func TestCollectThrottlesRepeatedRefreshesButNotRequestedOnes(t *testing.T) {
	calls := 0
	p := testProvider(func(context.Context, string, string) (*usage, error) {
		calls++
		return &usage{included: &included{totalPercent: float64(calls)}}, nil
	})

	now := time.Now()
	p.Collect(now)
	p.Collect(now.Add(time.Second))
	p.Collect(now.Add(2 * time.Second))
	if calls != 1 {
		t.Fatalf("scheduled refreshes made %d requests, want 1", calls)
	}

	// A refresh the user asked for is one they want gone to the service for,
	// which is the whole reason the button exists.
	p.RequestAuthoritative()
	p.Collect(now.Add(3 * time.Second))
	if calls != 2 {
		t.Fatalf("a requested refresh made %d requests total, want 2", calls)
	}

	p.Collect(now.Add(time.Hour))
	if calls != 3 {
		t.Fatalf("after the throttle expired there were %d requests, want 3", calls)
	}
}

func TestCollectKeepsTheLastGoodReadingUntilItGoesStale(t *testing.T) {
	fail := false
	p := testProvider(func(context.Context, string, string) (*usage, error) {
		if fail {
			return nil, fmt.Errorf("network is down")
		}
		return &usage{included: &included{totalPercent: 25}}, nil
	})

	now := time.Now()
	p.Collect(now)
	fail = true

	// Still inside staleAfterSeconds: a flaky network must not blank a quota
	// that has not moved.
	aged := p.Collect(now.Add(5 * time.Minute))
	if aged.Err != "" || aged.Windows[model.WindowMonthly].UsedPercent != 25 {
		t.Fatalf("aged reading = %+v", aged)
	}
	if !strings.Contains(aged.Note, "last good reading") {
		t.Fatalf("note = %q, want it to say the figure is not fresh", aged.Note)
	}

	// Past it, the error is the honest answer.
	expired := p.Collect(now.Add(time.Hour))
	if expired.Err == "" || len(expired.Windows) != 0 {
		t.Fatalf("expired reading = %+v, want an error and no gauge", expired)
	}
}

func TestCollectExplainsAnAccountWithNoPercentage(t *testing.T) {
	p := testProvider(func(context.Context, string, string) (*usage, error) {
		return &usage{displayMessage: "Usage is billed to your organization"}, nil
	})
	st := p.Collect(time.Now())
	if len(st.Windows) != 0 {
		t.Fatalf("windows = %+v, want none", st.Windows)
	}
	if !strings.Contains(st.Err, "organization") {
		t.Fatalf("error = %q, want Cursor's own explanation", st.Err)
	}
}

func TestNormaliseReadsProtobufStringNumbers(t *testing.T) {
	// 64-bit protobuf fields arrive as JSON strings while smaller ones arrive
	// as numbers, and which is which varies by field, so both have to parse.
	var response usageResponse
	body := `{
	  "billingCycleEnd": "1788229667000",
	  "planUsage": {"totalPercentUsed": "71.5", "autoPercentUsed": 12, "apiPercentUsed": "0"},
	  "spendLimitUsage": {"individualUsed": "1250", "individualLimit": "2000"}
	}`
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatal(err)
	}
	got, err := response.normalise("Ultra")
	if err != nil {
		t.Fatal(err)
	}
	if got.included == nil || got.included.totalPercent != 71.5 || got.included.autoPercent != 12 {
		t.Fatalf("included = %+v", got.included)
	}
	if got.cycleEnd.UnixMilli() != 1788229667000 {
		t.Fatalf("cycle end = %v", got.cycleEnd)
	}
	if got.onDemand.kind != onDemandFixed || got.onDemand.summary() != "on-demand $12.50 / $20.00" {
		t.Fatalf("on-demand = %+v (%s)", got.onDemand, got.onDemand.summary())
	}
}

// The spend amounts are a different scale from the percentage: this is a real
// response, in which the percentage says 2.9% and the amounts are a 50% ratio.
// Falling back from one to the other would put a fifty-point error on the
// battery, so a response carrying only the amounts has to yield no gauge.
func TestNormaliseNeverDerivesThePercentageFromSpend(t *testing.T) {
	var response usageResponse
	body := `{"planUsage": {"totalSpend": 1008, "includedSpend": 1008,
	          "remaining": 992, "limit": 2000}, "displayMessage": "You've used 14%"}`
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatal(err)
	}
	got, err := response.normalise("")
	if err != nil {
		t.Fatal(err)
	}
	if got.included != nil {
		t.Fatalf("included = %+v, want no percentage at all", got.included)
	}
}

// A live response, kept whole so a field rename is caught by a test rather
// than by a blank battery.
func TestNormaliseReadsALiveResponse(t *testing.T) {
	body := `{
	  "billingCycleStart": "1786081182000",
	  "billingCycleEnd": "1788759582000",
	  "planUsage": {
	    "totalSpend": 1008, "includedSpend": 1008, "remaining": 992, "limit": 2000,
	    "remainingBonus": false,
	    "autoPercentUsed": 3.36, "apiPercentUsed": 0,
	    "totalPercentUsed": 2.9217391304347826
	  },
	  "spendLimitUsage": {"individualLimit": 5000, "individualRemaining": 5000, "limitType": "user"},
	  "displayThreshold": 200,
	  "enabled": true,
	  "displayMessage": "You've used 14% of your included usage"
	}`
	var response usageResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatal(err)
	}
	got, err := response.normalise("Pro")
	if err != nil {
		t.Fatal(err)
	}
	if got.included == nil || got.included.totalPercent != 2.9217391304347826 {
		t.Fatalf("included = %+v", got.included)
	}
	// individualUsed is absent when nothing has been spent on demand, which is
	// not the same as an absent limit.
	if got.onDemand.kind != onDemandFixed || got.onDemand.summary() != "on-demand $0.00 / $50.00" {
		t.Fatalf("on-demand = %+v (%s)", got.onDemand, got.onDemand.summary())
	}
	if got.cycleEnd.UnixMilli() != 1788759582000 {
		t.Fatalf("cycle end = %v", got.cycleEnd)
	}
}

func TestNormaliseSeparatesDisabledOnDemandFromUnlimited(t *testing.T) {
	// Zero and absent look the same to a plain float, but they are opposite
	// answers: spending switched off versus no ceiling at all.
	for _, tc := range []struct {
		name string
		body string
		want onDemandKind
	}{
		{"disabled", `{"spendLimitUsage":{"individualUsed":0,"individualLimit":0}}`, onDemandDisabled},
		{"unlimited", `{"spendLimitUsage":{"individualUsed":900}}`, onDemandUnlimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var response usageResponse
			if err := json.Unmarshal([]byte(tc.body), &response); err != nil {
				t.Fatal(err)
			}
			got, err := response.normalise("")
			if err != nil {
				t.Fatal(err)
			}
			if got.onDemand.kind != tc.want {
				t.Fatalf("kind = %v, want %v", got.onDemand.kind, tc.want)
			}
		})
	}
}

func TestNormaliseRejectsAnUnrecognisableResponse(t *testing.T) {
	var response usageResponse
	if err := json.Unmarshal([]byte(`{"somethingElse": true}`), &response); err != nil {
		t.Fatal(err)
	}
	// Showing a confident zero built out of absent fields would be worse than
	// showing nothing at all.
	if _, err := response.normalise(""); err == nil {
		t.Fatal("expected a response with no usage fields to be rejected")
	}
}

func TestFetchUsageSendsTheConnectHeadersAndSurvivesAMissingPlanName(t *testing.T) {
	var seenAuth, seenProtocol string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/GetCurrentPeriodUsage"):
			seenAuth = r.Header.Get("Authorization")
			seenProtocol = r.Header.Get("Connect-Protocol-Version")
			fmt.Fprint(w, `{"planUsage":{"totalPercentUsed":10}}`)
		default:
			http.Error(w, "nope", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	got, err := fetchUsage(context.Background(), server.URL, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if seenAuth != "Bearer secret" || seenProtocol != "1" {
		t.Fatalf("headers = %q / %q", seenAuth, seenProtocol)
	}
	// The plan name is decoration; losing it must not lose the percentage.
	if got.planName != "" || got.included.totalPercent != 10 {
		t.Fatalf("usage = %+v", got)
	}
}

func TestFetchUsageNamesAnExpiredSignIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := fetchUsage(context.Background(), server.URL, "stale")
	if err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatalf("error = %v, want it to say how to sign in again", err)
	}
}

func TestResolveTokenPrefersTheConfiguredKey(t *testing.T) {
	t.Setenv("CURSOR_API_KEY", "from-env")
	got, err := resolveToken(config.Cursor{APIKey: " from-config "})
	if err != nil || got != "from-config" {
		t.Fatalf("token = %q, err = %v", got, err)
	}
	got, err = resolveToken(config.Cursor{})
	if err != nil || got != "from-env" {
		t.Fatalf("token = %q, err = %v", got, err)
	}
}

// The CLI only reaches for a keychain on macOS; on Windows and Linux it writes
// the token to auth.json. Looking for a Credential Manager entry there finds
// nothing however it is named, which is what made a signed-in Windows machine
// report "no Cursor credential".
func TestAuthFileTokenIsReadFromWhereTheCliWritesIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))

	if _, err := authFileToken(); err == nil {
		t.Fatal("a machine with no auth file has no auth-file token")
	}
	if hasAuthFileToken() {
		t.Fatal("hasAuthFileToken must agree with authFileToken")
	}

	// The path is asked for rather than hardcoded, so this covers whichever
	// platform layout the test happens to run on.
	path, err := authFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path,
		[]byte(`{"accessToken":" tok_abc ","refreshToken":"r"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := authFileToken()
	if err != nil || got != "tok_abc" {
		t.Fatalf("token = %q, err = %v", got, err)
	}

	// A file that exists but carries no access token is a different problem
	// from a missing one, and has to say so rather than yield an empty token.
	if err := os.WriteFile(path, []byte(`{"refreshToken":"r"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := authFileToken(); err == nil || !strings.Contains(err.Error(), "accessToken") {
		t.Fatalf("error = %v, want it to name the missing field", err)
	}
}

func TestSignedInReadsTheCliMarkerWithoutATokenBeingPresent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	if signedIn() {
		t.Fatal("a machine with no Cursor config is not signed in")
	}

	cursorDir := filepath.Join(dir, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(cursorDir, "cli-config.json")
	if err := os.WriteFile(config, []byte(`{"model":"auto"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if signedIn() {
		t.Fatal("a config without an account marker is not signed in")
	}

	if err := os.WriteFile(config, []byte(`{"authId":"abc","authInfo":{"email":"a@b"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !signedIn() {
		t.Fatal("authId marks the CLI as signed in")
	}
}

// The credential store's entry name is chosen by whichever keychain binding
// the installed CLI was built against, and on Windows it has been seen under
// none of the names usagebat knows. Losing the token must not lose the source:
// a row saying the credential is missing is something the user can act on,
// where a service that is silently absent is not.
func TestAvailableSurvivesAnUnreadableCredential(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("CURSOR_API_KEY", "")

	cursorDir := filepath.Join(dir, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "cli-config.json"),
		[]byte(`{"authId":"abc"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Available(&config.Cursor{Enabled: true}) {
		t.Fatal("a signed-in CLI makes Cursor available even without a readable credential")
	}
}

func TestEndpointFallsBackToCursorsOwnHost(t *testing.T) {
	t.Setenv("CURSOR_API_ENDPOINT", "")
	p := testProvider(nil)
	if got := p.endpoint(); got != "https://api2.cursor.sh" {
		t.Fatalf("endpoint = %q", got)
	}
	p.cfg.Endpoint = "https://proxy.example/"
	if got := p.endpoint(); got != "https://proxy.example" {
		t.Fatalf("configured endpoint = %q, want the trailing slash gone", got)
	}
}
