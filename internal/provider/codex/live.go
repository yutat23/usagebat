package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yutat23/usage-battery/internal/config"
	"github.com/yutat23/usage-battery/internal/version"
)

// liveRateLimits asks Codex's own app server for the same account snapshot the
// CLI uses for /status. Rollout files are intentionally only a fallback: the
// newest file can belong to a session that stopped before a limit reset.
func liveRateLimits(ctx context.Context, bin, home string) (*rateLimits, error) {
	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio")
	cmd.Dir = os.TempDir()
	cmd.Env = withEnv(os.Environ(), "CODEX_HOME", home)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	enc := json.NewEncoder(stdin)
	dec := json.NewDecoder(bufio.NewReader(stdout))
	if err := enc.Encode(map[string]any{
		"id": 1, "method": "initialize",
		"params": map[string]any{
			"clientInfo": map[string]string{
				"name": "usage-battery", "title": "usage-battery", "version": version.Value,
			},
			"capabilities": map[string]any{},
		},
	}); err != nil {
		return nil, err
	}
	if _, err := waitResponse(dec, 1); err != nil {
		return nil, appServerError(err, stderr.String())
	}
	if err := enc.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	if err := enc.Encode(map[string]any{
		"id": 2, "method": "account/rateLimits/read", "params": nil,
	}); err != nil {
		return nil, err
	}
	result, err := waitResponse(dec, 2)
	if err != nil {
		return nil, appServerError(err, stderr.String())
	}
	return parseLiveRateLimits(result)
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func waitResponse(dec *json.Decoder, want int) (json.RawMessage, error) {
	for {
		var msg rpcMessage
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("Codex app server closed before response %d", want)
			}
			return nil, err
		}
		if strings.TrimSpace(string(msg.ID)) != fmt.Sprint(want) {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("Codex app server: %s (%d)", msg.Error.Message, msg.Error.Code)
		}
		if len(msg.Result) == 0 || string(msg.Result) == "null" {
			return nil, fmt.Errorf("Codex app server returned no result")
		}
		return msg.Result, nil
	}
}

type liveWindow struct {
	UsedPercent       float64 `json:"usedPercent"`
	WindowDurationMin int     `json:"windowDurationMins"`
	ResetsAt          *int64  `json:"resetsAt"`
}

type liveSnapshot struct {
	LimitID   string      `json:"limitId"`
	PlanType  string      `json:"planType"`
	Primary   *liveWindow `json:"primary"`
	Secondary *liveWindow `json:"secondary"`
}

func parseLiveRateLimits(data []byte) (*rateLimits, error) {
	var response struct {
		RateLimits          *liveSnapshot            `json:"rateLimits"`
		RateLimitsByLimitID map[string]*liveSnapshot `json:"rateLimitsByLimitId"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("parsing Codex rate limits: %w", err)
	}
	snapshot := response.RateLimits
	if byID := response.RateLimitsByLimitID["codex"]; byID != nil {
		snapshot = byID
	}
	if snapshot == nil {
		return nil, fmt.Errorf("Codex returned no account rate limits")
	}
	convert := func(w *liveWindow) *bucket {
		if w == nil {
			return nil
		}
		return &bucket{
			UsedPercent: w.UsedPercent, WindowMinutes: w.WindowDurationMin, ResetsAt: w.ResetsAt,
		}
	}
	return &rateLimits{
		LimitID: snapshot.LimitID, PlanType: snapshot.PlanType,
		Primary: convert(snapshot.Primary), Secondary: convert(snapshot.Secondary),
	}, nil
}

func appServerError(err error, stderr string) error {
	if detail := strings.TrimSpace(stderr); detail != "" {
		// Stderr can contain several warning lines; the first one is enough for
		// diagnostics and keeps the tray menu readable.
		if i := strings.IndexByte(detail, '\n'); i >= 0 {
			detail = detail[:i]
		}
		return fmt.Errorf("%w: %s", err, detail)
	}
	return err
}

func withEnv(env []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), prefix) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, key+"="+value)
}

// resolveBinary also checks common GUI-invisible locations because tray apps
// usually inherit a smaller PATH than an interactive terminal.
func resolveBinary(configured string) (string, error) {
	if configured != "" {
		if strings.ContainsAny(configured, `/\\`) {
			if isExecutable(configured) {
				return configured, nil
			}
			return "", fmt.Errorf("%s is not executable", configured)
		}
		if p, err := exec.LookPath(configured); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("codex"); err == nil {
		return p, nil
	}
	for _, candidate := range wellKnownPaths() {
		if isExecutable(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("codex CLI not found; set sources.codex.path")
}

// Available reports whether the Codex CLI needed for live account readings is
// installed. Old rollout files alone do not make an uninstalled service appear
// in the tray.
func Available(cfg *config.Codex) bool {
	_, err := resolveBinary(cfg.Path)
	return err == nil
}

func wellKnownPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		return []string{
			filepath.Join(home, ".local", "bin", "codex.exe"),
			filepath.Join(home, "AppData", "Roaming", "npm", "codex.cmd"),
			filepath.Join(home, "AppData", "Local", "Programs", "codex", "codex.exe"),
		}
	}
	return []string{
		filepath.Join(home, ".local", "bin", "codex"),
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
	}
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || fi.Mode()&0o111 != 0
}

func liveTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = 15
	}
	return time.Duration(seconds) * time.Second
}
