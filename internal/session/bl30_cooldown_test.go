// BL30 — global rate-limit cooldown tests.

package session

import (
	"errors"
	"testing"
	"time"
)

func TestBL30_CooldownStatus_InactiveByDefault(t *testing.T) {
	m := &Manager{}
	st := m.CooldownStatus()
	if st.Active {
		t.Errorf("default cooldown should be inactive: %+v", st)
	}
}

func TestBL30_SetGlobalCooldown_BecomesActive(t *testing.T) {
	m := &Manager{}
	until := time.Now().Add(30 * time.Second)
	m.SetGlobalCooldown(until, "anthropic-rate-limit")
	st := m.CooldownStatus()
	if !st.Active {
		t.Fatalf("cooldown should be active: %+v", st)
	}
	if st.Reason != "anthropic-rate-limit" {
		t.Errorf("reason=%q want anthropic-rate-limit", st.Reason)
	}
	if st.RemainingSeconds < 25 || st.RemainingSeconds > 31 {
		t.Errorf("remaining=%d not within 25..31", st.RemainingSeconds)
	}
}

func TestBL30_ClearCooldown_GoesInactive(t *testing.T) {
	m := &Manager{}
	m.SetGlobalCooldown(time.Now().Add(time.Minute), "x")
	m.ClearGlobalCooldown()
	if m.CooldownStatus().Active {
		t.Errorf("expected inactive after clear")
	}
}

func TestBL30_ExpiredCooldown_Inactive(t *testing.T) {
	m := &Manager{}
	m.SetGlobalCooldown(time.Now().Add(-1*time.Second), "stale")
	if m.CooldownStatus().Active {
		t.Errorf("expired cooldown should report inactive")
	}
}

func TestBL30_RateLimitGlobalPause_OptIn(t *testing.T) {
	m := &Manager{}
	m.SetRateLimitGlobalPause(true)
	if !m.RateLimitGlobalPause() {
		t.Errorf("setter not honoured")
	}
}

func TestBL30_ErrGlobalCooldown_Sentinel(t *testing.T) {
	if !errors.Is(ErrGlobalCooldown, ErrGlobalCooldown) {
		t.Error("sentinel error broken")
	}
}
