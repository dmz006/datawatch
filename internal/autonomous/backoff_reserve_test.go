package autonomous

import (
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

type reserveTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *reserveTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *reserveTestClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestReserveRoleByRank(t *testing.T) {
	for _, r := range []capacity.Role{capacity.RoleVerifier, capacity.RolePlanner} {
		if !ReserveRole(r) {
			t.Errorf("role %v should take a reserve slot", r)
		}
	}
	for _, r := range []capacity.Role{capacity.RoleWorker, ""} {
		if ReserveRole(r) {
			t.Errorf("role %q must never take a reserve slot", r)
		}
	}
}

func TestBackoffReserve_AdmitRules(t *testing.T) {
	clk := &reserveTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	g := NewBackoffReserve(clk.Now, func() int { return 1 }, func() int64 { return 0 })

	// A review role gets the single slot while 0 of it are held.
	admit, _ := g.Admit("claude-code", capacity.RoleVerifier, 0)
	if !admit {
		t.Fatal("verifier should be admitted with a free reserve slot")
	}
	// The planner too — different sessions, same one slot, 0 held here.
	admit, _ = g.Admit("claude-code", capacity.RolePlanner, 0)
	if !admit {
		t.Fatal("planner should be admitted with a free reserve slot")
	}
	// A worker is refused no matter how empty the reserve is.
	if admit, reason := g.Admit("claude-code", capacity.RoleWorker, 0); admit {
		t.Error("worker must never be admitted through the reserve")
	} else if reason != BackoffReserveWaitReason {
		t.Errorf("worker refusal reason = %q, want %q", reason, BackoffReserveWaitReason)
	}
	// The reserve is held: the slot count is exhausted.
	if admit, reason := g.Admit("claude-code", capacity.RoleVerifier, 1); admit {
		t.Error("a held reserve slot admits nothing more")
	} else if reason != BackoffReserveWaitReason {
		t.Errorf("refusal reason = %q, want %q", reason, BackoffReserveWaitReason)
	}
	// Per-backend: another backend's reserve is independent.
	if admit, _ := g.Admit("pi-prompt", capacity.RoleVerifier, 0); !admit {
		t.Error("the reserve is per backend")
	}
}

func TestBackoffReserve_SlotsZero(t *testing.T) {
	g := NewBackoffReserve(time.Now, func() int { return 0 }, nil)
	if admit, _ := g.Admit("claude-code", capacity.RoleVerifier, 0); admit {
		t.Error("reserve_for_review = 0 refuses every request")
	}
}

func TestBackoffReserve_SlotsResolverLive(t *testing.T) {
	slots := 1
	g := NewBackoffReserve(time.Now, func() int { return slots }, nil)
	if admit, _ := g.Admit("cc", capacity.RoleVerifier, 0); !admit {
		t.Fatal("setup: first slot should admit")
	}
	slots = 0
	if admit, _ := g.Admit("cc", capacity.RoleVerifier, 0); admit {
		t.Error("a config lowering slots to 0 must be visible without rebuilding the gate")
	}
	slots = 2
	if admit, _ := g.Admit("cc", capacity.RoleVerifier, 1); !admit {
		t.Error("a config raising slots must be visible too")
	}
}

func TestBackoffReserve_RollingTokenCap(t *testing.T) {
	clk := &reserveTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	tokenCap := int64(1000)
	g := NewBackoffReserve(clk.Now, func() int { return 1 }, func() int64 { return tokenCap })

	// 900 of the 1000 already spent: the next session is still admitted.
	g.RecordTokens("cc", 900)
	if admit, _ := g.Admit("cc", capacity.RoleVerifier, 0); !admit {
		t.Error("900 of 1000 tokens must not yet refuse the reserve")
	}
	// One more 200 exhausts the budget: now refused.
	g.RecordTokens("cc", 200)
	if admit, _ := g.Admit("cc", capacity.RoleVerifier, 0); admit {
		t.Error("an exhausted token budget admits nothing")
	}
	// The window rolls: the same spend is out of the hour once the clock is
	// past it.
	clk.advance(time.Hour + time.Second)
	if admit, _ := g.Admit("cc", capacity.RoleVerifier, 0); !admit {
		t.Error("stamps outside the rolling hour stop counting")
	}
	// Per backend: another backend's exhausted budget is separate.
	g.RecordTokens("other", 1000000)
	if admit, _ := g.Admit("cc", capacity.RoleVerifier, 0); !admit {
		t.Error("the token budget is per backend")
	}
}

func TestBackoffReserve_RecordTokensSanity(t *testing.T) {
	g := NewBackoffReserve(time.Now, nil, nil)
	g.RecordTokens("cc", -1)
	g.RecordTokens("", 5)
	if got := g.UsedTokens("cc"); got != 0 {
		t.Errorf("non-positive and empty backend amounts must not count, got %d", got)
	}
}

func TestBackoffReserve_EventQueue(t *testing.T) {
	g := NewBackoffReserve(time.Now, func() int { return 1 }, nil)
	if got := g.DrainEvents(); len(got) != 0 {
		t.Fatalf("fresh gate has %d queued events, want 0", len(got))
	}
	g.Admit("cc", capacity.RoleVerifier, 0)
	g.Admit("cc", capacity.RoleWorker, 0)
	evs := g.DrainEvents()
	if len(evs) != 2 {
		t.Fatalf("two verdicts queued %d events, want 2", len(evs))
	}
	if !evs[0].Admitted || evs[0].Backend != "cc" || evs[0].Role != string(capacity.RoleVerifier) || evs[0].Slots != 1 || evs[0].UsedSlots != 0 {
		t.Errorf("first event = %+v", evs[0])
	}
	if evs[1].Admitted || evs[1].Role != string(capacity.RoleWorker) {
		t.Errorf("second event = %+v", evs[1])
	}
	if got := g.DrainEvents(); len(got) != 0 {
		t.Errorf("drain clears the queue, %d left", len(got))
	}
	// The queue is back-bounded.
	for i := 0; i < reserveEventCap+10; i++ {
		g.Admit("cc", capacity.RoleVerifier, 0)
	}
	if got := g.DrainEvents(); len(got) != reserveEventCap {
		t.Errorf("queue overflows past %d: got %d", reserveEventCap, len(got))
	}
}

func TestBackoffReserveWaitReasonClassifiesAsBackoff(t *testing.T) {
	// The legacy back-off wording still classifies.
	if got := ClassifyWaitReason("backend claude-code backed off until 12:17 UTC (resets in 7m)"); got != WaitCauseBackoff {
		t.Errorf("existing back-off reason = %s, want %s", got, WaitCauseBackoff)
	}
	if got := ClassifyWaitReason(BackoffReserveWaitReason); got != WaitCauseBackoff {
		t.Errorf("reserve reason = %s, want %s", got, WaitCauseBackoff)
	}
	if got := ClassifyWaitReason("pool llm:cc backpressure: back-off, review reserve in use"); got != WaitCauseBackoff {
		t.Errorf("ledger-wrapped reserve reason = %s, want %s", got, WaitCauseBackoff)
	}
}
