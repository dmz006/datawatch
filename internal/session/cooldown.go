// BL30 — global rate-limit cooldown.
//
// Manager.SetGlobalCooldown(until) marks all backends as paused until
// the given time. New session starts return ErrGlobalCooldown while
// active. CooldownStatus() returns the end time + remaining seconds
// for ops surfaces. The actual auto-resume happens when callers next
// observe the deadline elapsed; no goroutine is needed.

package session

import (
	"errors"
	"sync"
	"time"
)

// ErrGlobalCooldown is returned by Start when a global cooldown is
// active. Callers should display the cooldown end-time to the user
// and either retry after, or fall back to a different backend.
var ErrGlobalCooldown = errors.New("global rate-limit cooldown is active")

// CooldownState is the manager's view of the current rate-limit pause.
type CooldownState struct {
	Active           bool      `json:"active"`
	Until            time.Time `json:"until,omitempty"`
	RemainingSeconds int       `json:"remaining_seconds,omitempty"`
	Reason           string    `json:"reason,omitempty"`
}

type cooldownTracker struct {
	mu     sync.Mutex
	until  time.Time
	reason string
}

// SetGlobalCooldown sets the manager-wide rate-limit pause until the
// given absolute time. Reason is a human-readable string (e.g. the
// backend name + retry-after header) surfaced via CooldownStatus.
// Passing a zero time clears the cooldown.
func (m *Manager) SetGlobalCooldown(until time.Time, reason string) {
	if m.cooldown == nil {
		m.cooldown = &cooldownTracker{}
	}
	m.cooldown.mu.Lock()
	defer m.cooldown.mu.Unlock()
	m.cooldown.until = until
	m.cooldown.reason = reason
}

// ClearGlobalCooldown removes any active cooldown immediately.
func (m *Manager) ClearGlobalCooldown() {
	m.SetGlobalCooldown(time.Time{}, "")
}

// CooldownStatus returns the current cooldown state.
func (m *Manager) CooldownStatus() CooldownState {
	if m.cooldown == nil {
		return CooldownState{}
	}
	m.cooldown.mu.Lock()
	defer m.cooldown.mu.Unlock()
	now := time.Now()
	if m.cooldown.until.IsZero() || !m.cooldown.until.After(now) {
		return CooldownState{}
	}
	return CooldownState{
		Active:           true,
		Until:            m.cooldown.until,
		RemainingSeconds: int(m.cooldown.until.Sub(now).Seconds()),
		Reason:           m.cooldown.reason,
	}
}

// inGlobalCooldown is the internal check called from Start when the
// rate_limit_global_pause config flag is enabled.
func (m *Manager) inGlobalCooldown() bool {
	if m.cooldown == nil {
		return false
	}
	m.cooldown.mu.Lock()
	defer m.cooldown.mu.Unlock()
	return !m.cooldown.until.IsZero() && m.cooldown.until.After(time.Now())
}
