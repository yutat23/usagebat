package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yutat23/usagebat/internal/version"
)

// dashboardPath is the Connect service the Cursor CLI itself calls for the
// figures its TUI shows.
const dashboardPath = "/aiserver.v1.DashboardService"

// maxResponseBytes bounds what is read from a response. The real payload is a
// few hundred bytes; anything vastly larger is a proxy's error page, not usage.
const maxResponseBytes = 1 << 20

func defaultEndpoint() string {
	if fromEnv := strings.TrimSpace(os.Getenv("CURSOR_API_ENDPOINT")); fromEnv != "" {
		return strings.TrimSuffix(fromEnv, "/")
	}
	return "https://api2.cursor.sh"
}

// usage is one reading, already normalised into the shapes the menu needs.
type usage struct {
	displayMessage string
	planName       string
	cycleEnd       time.Time
	included       *included
	onDemand       *onDemand
}

// included is the quota the plan comes with, as percentages of the allowance.
type included struct {
	totalPercent float64
	autoPercent  float64
	apiPercent   float64
}

type onDemandKind int

const (
	onDemandUnlimited onDemandKind = iota
	onDemandFixed
	onDemandDisabled
)

// onDemand is spending beyond the included quota, in cents.
type onDemand struct {
	kind      onDemandKind
	usedCents float64
	// limitCents is meaningful only for onDemandFixed.
	limitCents float64
}

// fetchUsage asks for the current period and, as optional enrichment, the plan
// name. A missing plan name only costs a word in the menu, so its failure is
// never allowed to fail the reading.
func fetchUsage(ctx context.Context, endpoint, token string) (*usage, error) {
	client := &http.Client{}

	body, err := post(ctx, client, endpoint+dashboardPath+"/GetCurrentPeriodUsage", token)
	if err != nil {
		return nil, err
	}
	var response usageResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parsing Cursor usage: %w", err)
	}

	planName := ""
	if planBody, err := post(ctx, client, endpoint+dashboardPath+"/GetPlanInfo", token); err == nil {
		var plan struct {
			PlanInfo struct {
				PlanName string `json:"planName"`
			} `json:"planInfo"`
		}
		if json.Unmarshal(planBody, &plan) == nil {
			planName = strings.TrimSpace(plan.PlanInfo.PlanName)
		}
	}

	return response.normalise(planName)
}

func post(ctx context.Context, client *http.Client, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	// Connect's own version header; without it the service answers with a
	// protocol error rather than the payload.
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("User-Agent", "usagebat/"+version.String())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Cursor request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("Cursor sign-in expired; run `agent login`")
	default:
		return nil, fmt.Errorf("Cursor API returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
}

// usageResponse is the subset of GetCurrentPeriodUsage usagebat reads.
//
// Every number is a `num` because the service is protobuf-backed: 64-bit
// fields arrive as JSON strings, and the same field can switch representation
// between accounts.
type usageResponse struct {
	DisplayMessage    string          `json:"displayMessage"`
	BillingCycleStart json.RawMessage `json:"billingCycleStart"`
	BillingCycleEnd   json.RawMessage `json:"billingCycleEnd"`

	PlanUsage *struct {
		TotalPercentUsed num `json:"totalPercentUsed"`
		AutoPercentUsed  num `json:"autoPercentUsed"`
		APIPercentUsed   num `json:"apiPercentUsed"`
	} `json:"planUsage"`

	SpendLimitUsage *struct {
		IndividualUsed      num `json:"individualUsed"`
		IndividualLimit     num `json:"individualLimit"`
		IndividualRemaining num `json:"individualRemaining"`
	} `json:"spendLimitUsage"`
}

func (r usageResponse) normalise(planName string) (*usage, error) {
	out := &usage{
		displayMessage: strings.TrimSpace(r.DisplayMessage),
		planName:       planName,
		cycleEnd:       parseTimestamp(r.BillingCycleEnd),
	}

	// Only totalPercentUsed is read, and it is deliberately never reconstructed
	// from includedSpend and limit. Those are a different scale: a live account
	// reporting totalPercentUsed 2.9 carried includedSpend 1008 against a limit
	// of 2000, which as a ratio is 50%. Whatever the amounts describe, it is not
	// the quota this percentage measures, so a response without the percentage
	// has no percentage — inventing one from the amounts would be off by fifty
	// points rather than gracefully approximate.
	if u := r.PlanUsage; u != nil && u.TotalPercentUsed.set {
		out.included = &included{
			totalPercent: clampPercent(u.TotalPercentUsed.value),
			autoPercent:  clampPercent(u.AutoPercentUsed.value),
			apiPercent:   clampPercent(u.APIPercentUsed.value),
		}
	}

	if s := r.SpendLimitUsage; s != nil {
		used := s.IndividualUsed.value
		switch {
		case s.IndividualLimit.set && s.IndividualLimit.value > 0:
			out.onDemand = &onDemand{
				kind: onDemandFixed, usedCents: used, limitCents: s.IndividualLimit.value,
			}
		case s.IndividualLimit.set:
			out.onDemand = &onDemand{kind: onDemandDisabled, usedCents: used}
		default:
			out.onDemand = &onDemand{kind: onDemandUnlimited, usedCents: used}
		}
	}

	if out.included == nil && out.onDemand == nil && out.displayMessage == "" {
		// Nothing recognisable came back. Reporting that is better than showing
		// a confident zero built out of absent fields.
		return nil, fmt.Errorf("Cursor usage response had no recognisable plan data")
	}
	return out, nil
}

func clampPercent(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 100:
		return 100
	}
	return value
}

// num is a number that may arrive as a JSON number or as a JSON string, and
// remembers whether it was present at all — the difference between an
// on-demand limit of zero (spending disabled) and no limit (unlimited).
type num struct {
	set   bool
	value float64
}

func (n *num) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if text == "" || text == "null" {
		return nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		// One unreadable field must not discard the whole response; it stays
		// unset and the caller falls back the same way it would for a gap.
		return nil
	}
	n.set, n.value = true, value
	return nil
}

// parseTimestamp accepts the three shapes the field has been seen in: epoch
// milliseconds as a number, the same as a string, and RFC 3339.
func parseTimestamp(raw json.RawMessage) time.Time {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		return time.Time{}
	}
	if ms, err := strconv.ParseInt(text, 10, 64); err == nil {
		if ms <= 0 {
			return time.Time{}
		}
		return time.UnixMilli(ms)
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return parsed
	}
	return time.Time{}
}
