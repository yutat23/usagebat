// Package provider defines the contract every usage source implements.
package provider

import (
	"time"

	"github.com/yutat23/usage-battery/internal/model"
)

// Provider collects the current usage state for one tool.
//
// Implementations are called from a single goroutine on a timer and may keep
// caches between calls; they must not block for long.
type Provider interface {
	// ID is a stable identifier ("claude-code", "codex").
	ID() string
	// Collect returns the state as of now.
	Collect(now time.Time) model.SourceStatus
}
