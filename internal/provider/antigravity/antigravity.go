// Package antigravity reads Gemini quotas from the signed-in Antigravity CLI.
package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yutat23/usagebat/internal/hiddenprocess"
	"github.com/yutat23/usagebat/internal/model"
)

const (
	name            = "Antigravity (Gemini)"
	refreshInterval = 5 * time.Minute
	retryInterval   = time.Minute
)

// Provider caches recent readings between scheduled refreshes.
type Provider struct {
	query func() ([]byte, error)
	last  model.SourceStatus
	next  time.Time
}

// Detect returns a provider if the Antigravity CLI is installed.
func Detect() *Provider {
	path, err := exec.LookPath("agy")
	if err != nil {
		// Finder and Explorer may not have the PATH used by the shell.
		var installed string
		if runtime.GOOS == "windows" {
			if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
				installed = filepath.Join(dir, "agy", "bin", "agy.exe")
			}
		} else if home, err := os.UserHomeDir(); err == nil {
			installed = filepath.Join(home, ".local", "bin", "agy")
		}
		if installed == "" {
			return nil
		}
		path, err = exec.LookPath(installed)
		if err != nil {
			return nil
		}
	}
	return &Provider{query: func() ([]byte, error) { return runUsage(path) }}
}

func (p *Provider) ID() string { return model.SourceAntigravity }

func (p *Provider) RequestAuthoritative() { p.next = time.Time{} }

func (p *Provider) Collect(now time.Time) model.SourceStatus {
	if now.Before(p.next) {
		return p.last
	}
	status := model.SourceStatus{ID: p.ID(), Name: name, UpdatedAt: now}
	raw, err := p.query()
	if err == nil {
		status.Windows, err = parseUsage(raw)
	}
	if err != nil {
		status.Err = err.Error()
		p.next = now.Add(retryInterval)
	} else {
		status.Note = "live via Antigravity CLI"
		p.next = now.Add(refreshInterval)
		for _, window := range status.Windows {
			if window.ResetsAt.After(now) && window.ResetsAt.Before(p.next) {
				p.next = window.ResetsAt
			}
		}
	}
	p.last = status
	return status
}

func runUsage(path string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "usagebat-agy-")
	if err != nil {
		return nil, errors.New("could not create a temporary directory for agy")
	}
	defer os.RemoveAll(dir)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-p", "/usage", "--output-format", "json", "--print-timeout", "12s")
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	hiddenprocess.Configure(cmd)
	raw, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, errors.New("agy /usage timed out")
	}
	if err != nil {
		return nil, errors.New("agy /usage failed; check the CLI version and sign-in")
	}
	return raw, nil
}

func parseUsage(raw []byte) (map[model.Window]model.WindowStatus, error) {
	var response struct {
		Status  string
		Command struct {
			Name string
			Data struct {
				Groups []struct {
					Buckets []struct {
						ID                string
						Window            string
						RemainingFraction *float64 `json:"remaining_fraction"`
						ResetTime         string   `json:"reset_time"`
					}
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Status != "SUCCESS" || response.Command.Name != "usage" {
		return nil, errors.New("agy did not return usage data; check the CLI version and sign-in")
	}

	windows := make(map[model.Window]model.WindowStatus)
	for _, group := range response.Command.Data.Groups {
		for _, bucket := range group.Buckets {
			window, ok := model.ParseWindow(bucket.Window)
			if !strings.HasPrefix(bucket.ID, "gemini-") || !ok || bucket.RemainingFraction == nil {
				continue
			}
			remaining := *bucket.RemainingFraction
			if remaining < 0 || remaining > 1 {
				continue
			}
			reset, _ := time.Parse(time.RFC3339, bucket.ResetTime)
			if !reset.After(time.Unix(0, 0)) {
				reset = time.Time{}
			}
			status := model.WindowStatus{Window: window, Known: true, UsedPercent: 100 - remaining*100, ResetsAt: reset}
			if existing, ok := windows[window]; !ok || status.UsedPercent > existing.UsedPercent {
				windows[window] = status
			}
		}
	}
	if len(windows) == 0 {
		return nil, errors.New("agy reported no Gemini quota for this account")
	}
	return windows, nil
}
