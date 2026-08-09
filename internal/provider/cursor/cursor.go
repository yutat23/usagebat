// Package cursor reads plan usage from Cursor's dashboard API, using the
// credential the Cursor CLI already stored in the OS keychain.
//
// Unlike Claude Code and Codex, Cursor writes no usage figure anywhere on
// disk and ships no command that prints one, so there is nothing local to
// read: the request to Cursor is the only way to learn the number. Everything
// else in usagebat stays local, and the source only appears at all once a
// credential exists, so nobody who does not use Cursor makes this request.
package cursor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/model"
)

// Provider reads the account the stored credential belongs to.
//
// There is one credential per machine, so unlike Codex homes there is nothing
// to enumerate: a second Cursor account means logging the CLI in again, which
// replaces the first.
type Provider struct {
	cfg config.Cursor

	// token and fetch are indirected so tests can drive the whole collection
	// without a keychain or a network.
	token func(cfg config.Cursor) (string, error)
	fetch func(ctx context.Context, endpoint, token string) (*usage, error)

	// last is the newest good reading, kept so a failed request degrades to a
	// visibly aged figure rather than to nothing at all.
	last   *usage
	lastAt time.Time
	// nextAllowed is when the throttle lets another request through.
	nextAllowed time.Time
	// authoritative makes the next Collect ignore the throttle, because a
	// refresh the user asked for is one they want to go to the service.
	authoritative bool
}

// Providers builds the single Cursor source.
//
// It returns a slice for symmetry with the other provider packages, which do
// have several accounts each; main.go treats them all the same way.
func Providers(cfg *config.Cursor) []*Provider {
	return []*Provider{{cfg: *cfg, token: resolveToken, fetch: fetchUsage}}
}

// Available reports whether this machine has a Cursor account at all. It
// deliberately does not make the request: an account nobody has logged into
// should cost nothing and show nothing.
//
// Being signed in counts even when the credential itself cannot be found,
// because the two are separate lookups and only one of them is fragile. If the
// token turns out to be unreadable, Collect says so on a row the user can act
// on, which is worth more than a service that silently is not there.
func Available(cfg *config.Cursor) bool {
	if strings.TrimSpace(cfg.APIKey) != "" || envToken() != "" {
		return true
	}
	return credentialPresent() || signedIn()
}

// ID implements provider.Provider.
func (p *Provider) ID() string { return model.SourceCursor }

// RequestAuthoritative implements provider.Authoritative.
func (p *Provider) RequestAuthoritative() { p.authoritative = true }

// Collect implements provider.Provider.
func (p *Provider) Collect(now time.Time) model.SourceStatus {
	st := model.SourceStatus{
		ID:        model.SourceCursor,
		Name:      "Cursor",
		Windows:   map[model.Window]model.WindowStatus{},
		Tokens:    map[model.Window]model.Tokens{},
		UpdatedAt: now,
	}

	reading, fresh, err := p.reading(now)
	if reading == nil {
		st.Err = err.Error()
		return st
	}

	notes := []string{}
	if fresh {
		notes = append(notes, "live via Cursor")
	} else {
		notes = append(notes, "Cursor (last good reading)")
	}
	if reading.planName != "" {
		notes = append(notes, "plan: "+reading.planName)
	}
	if summary := reading.onDemand.summary(); summary != "" {
		notes = append(notes, summary)
	}
	st.Note = strings.Join(notes, " · ")

	// Cursor accounts for included usage over the billing cycle and reports
	// nothing shorter, so there is exactly one window and it is the monthly one.
	if reading.included != nil {
		detail := "reported by Cursor (billing cycle)"
		if extra := reading.included.breakdown(); extra != "" {
			detail += " · " + extra
		}
		st.Windows[model.WindowMonthly] = model.WindowStatus{
			Window:      model.WindowMonthly,
			Known:       true,
			UsedPercent: reading.included.totalPercent,
			ResetsAt:    reading.cycleEnd,
			Detail:      detail,
		}
		return st
	}

	// displayMessage is only used here, as the reason there is no gauge. It is
	// kept out of the note otherwise because it does not agree with the
	// percentage: an account reading 3% used carried "You've used 14% of your
	// included usage". Two different numbers on one menu line would read as a
	// bug in usagebat rather than as two figures Cursor keeps separately.
	//
	// A response without plan usage is not an error — an unlimited or
	// enterprise-billed account genuinely has no percentage — but there is no
	// gauge to draw, so say why rather than showing a silent blank.
	st.Err = "Cursor reported no plan usage for this account"
	if reading.displayMessage != "" {
		st.Err = "Cursor reported no percentage: " + reading.displayMessage
	}
	return st
}

// reading returns the figures to render, whether they were just fetched, and
// the reason there is nothing when it returns nil.
//
// The throttle is what keeps a burst of manual refreshes from becoming a burst
// of requests; going stale is what keeps a flaky network from blanking the
// icon of somebody whose quota has not moved.
func (p *Provider) reading(now time.Time) (*usage, bool, error) {
	authoritative := p.authoritative
	p.authoritative = false
	if !authoritative && p.last != nil && now.Before(p.nextAllowed) {
		return p.last, false, nil
	}

	token, err := p.token(p.cfg)
	if err != nil {
		return p.stale(now, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.timeout())
	defer cancel()
	fresh, err := p.fetch(ctx, p.endpoint(), token)
	if err != nil {
		return p.stale(now, err)
	}

	p.last, p.lastAt = fresh, now
	p.nextAllowed = now.Add(time.Duration(p.cfg.MinIntervalSeconds) * time.Second)
	return fresh, true, nil
}

// stale offers the previous reading in place of a failed one, but only for as
// long as it can still be believed. Past that the error is the honest answer.
func (p *Provider) stale(now time.Time, err error) (*usage, bool, error) {
	// A failure still costs a request, so it also spends the throttle: a
	// service that is down should not be asked once per second.
	p.nextAllowed = now.Add(time.Duration(p.cfg.MinIntervalSeconds) * time.Second)
	if p.last == nil {
		return nil, false, err
	}
	if now.Sub(p.lastAt) > time.Duration(p.cfg.StaleAfterSeconds)*time.Second {
		p.last = nil
		return nil, false, err
	}
	return p.last, false, nil
}

func (p *Provider) timeout() time.Duration {
	if p.cfg.TimeoutSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(p.cfg.TimeoutSeconds) * time.Second
}

func (p *Provider) endpoint() string {
	if configured := strings.TrimSpace(p.cfg.Endpoint); configured != "" {
		return strings.TrimSuffix(configured, "/")
	}
	return defaultEndpoint()
}

// breakdown names the two halves Cursor splits included usage into, which is
// the part of the number a user can actually act on: auto-model usage and
// bring-your-own-API usage are throttled by different decisions.
func (i *included) breakdown() string {
	var parts []string
	if i.autoPercent > 0 {
		parts = append(parts, fmt.Sprintf("auto %s", formatPercent(i.autoPercent)))
	}
	if i.apiPercent > 0 {
		parts = append(parts, fmt.Sprintf("api %s", formatPercent(i.apiPercent)))
	}
	return strings.Join(parts, " · ")
}

// summary is the menu note for pay-as-you-go spending, which sits outside the
// included quota and so cannot be folded into the battery.
func (o *onDemand) summary() string {
	if o == nil {
		return ""
	}
	switch o.kind {
	case onDemandDisabled:
		return "on-demand off"
	case onDemandFixed:
		return fmt.Sprintf("on-demand %s / %s", formatCents(o.usedCents), formatCents(o.limitCents))
	}
	return fmt.Sprintf("on-demand %s", formatCents(o.usedCents))
}

// formatPercent matches the Cursor CLI's own rounding: anything above zero but
// below one percent shows as 1%, so a started quota never reads as untouched.
func formatPercent(percent float64) string {
	if percent > 0 && percent < 1 {
		return "1%"
	}
	return fmt.Sprintf("%.0f%%", percent)
}

func formatCents(cents float64) string {
	return fmt.Sprintf("$%.2f", cents/100)
}
