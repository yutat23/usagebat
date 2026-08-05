// Package provider defines the contract every usage source implements.
package provider

import (
	"time"

	"github.com/yutat23/usagebat/internal/model"
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

// Authoritative is implemented by providers that normally read something
// cheap and cached, but can go and ask the service directly when asked.
//
// A scheduled refresh stays cheap; a refresh the user asked for is worth a
// subprocess and a few seconds, because the reason to press it is doubting the
// number on screen.
type Authoritative interface {
	// RequestAuthoritative makes the next Collect fetch a live reading. It is
	// called from the same goroutine as Collect.
	RequestAuthoritative()
}
