package autonomous

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

func reserveIntPtr(v int) *int { return &v }

func newReserveTestClock() *reserveTestClock {
	return &reserveTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
}

// a) While the backend is in back-off, the reserve admits exactly one
// review session for it; the next must wait.
func TestReserveAdmitsVerifierWhileBackendBackedOff(t *testing.T) {
	clk := newReserveTestClock()
	bo := NewBackoff()
	bo.Clock = clk.Now
	bo.Set("anthropic", clk.Now().Add(time.Hour), "rate limited", "test")
	if blocked, why := bo.Blocked("anthropic"); !blocked || why == "" {
		t.Fatalf("backend should read as backed off: blocked=%v why=%q", blocked, why)
	}

	g := NewBackoffReserve(clk.Now, func() int { return 1 }, func() int64 { return 0 })
	if ok, reason := g.Admit("anthropic", capacity.RoleVerifier, 0); !ok {
		t.Fatalf("first review session should be admitted through the reserve: %v", reason)
	}
	if ok, _ := g.Admit("anthropic", capacity.RoleVerifier, 1); ok {
		t.Fatal("a second review session should be refused while the first holds the only reserve slot")
	}
}

// c) reserve_for_review 0 refuses both review roles outright.
func TestReserveRefusesReviewRolesWhenEmpty(t *testing.T) {
	clk := newReserveTestClock()
	g := NewBackoffReserve(clk.Now, func() int { return 0 }, func() int64 { return 0 })
	for _, role := range []capacity.Role{capacity.RoleVerifier, capacity.RolePlanner} {
		if ok, reason := g.Admit("anthropic", role, 0); ok {
			t.Fatalf("reserve 0: %s should be refused", role)
		} else if reason != BackoffReserveWaitReason {
			t.Fatalf("reserve 0: %s refusal reason = %q, want %q", role, reason, BackoffReserveWaitReason)
		}
	}
}

// f) Once the hour's tokens are spent the reserve refuses admission even
// with free slots; the fake clock advancing one hour rolls the window and
// the same request is admitted again.
func TestReserveTokenCapRefusesThenAdmitsAfterHour(t *testing.T) {
	clk := newReserveTestClock()
	g := NewBackoffReserve(clk.Now, func() int { return 1 }, func() int64 { return 100 })
	g.RecordTokens("anthropic", 60)
	g.RecordTokens("anthropic", 40) // the hour's 100 tokens are fully spent
	if ok, reason := g.Admit("anthropic", capacity.RoleVerifier, 0); ok {
		t.Fatalf("should be refused once the hour's tokens are spent: %v", reason)
	} else if reason != BackoffReserveWaitReason {
		t.Fatalf("refusal reason = %q, want %q", reason, BackoffReserveWaitReason)
	}
	clk.advance(time.Hour + time.Second) // stamps drop out of the one-hour window
	if ok, reason := g.Admit("anthropic", capacity.RoleVerifier, 0); !ok {
		t.Fatalf("the one-hour window should have rolled, admission should be admitted again: %v", reason)
	}
}

// g) The review roles that use the reserve: the guardrail judges and
// verifiers run as capacity.RoleVerifier, and the decompose/planning
// sessions and repair helpers as capacity.RolePlanner. Ordinary workers
// never take the reserve.
func TestReserveRolesCoverJudgeDecomposeAndRepair(t *testing.T) {
	clk := newReserveTestClock()
	g := NewBackoffReserve(clk.Now, func() int { return 2 }, func() int64 { return 0 })
	for _, tc := range []struct {
		role capacity.Role
		what string
	}{
		{capacity.RoleVerifier, "guardrail judge/verifier"},
		{capacity.RolePlanner, "decompose/planning"},
		{capacity.RolePlanner, "repair helper"},
	} {
		if !ReserveRole(tc.role) {
			t.Fatalf("%s should use the review reserve", tc.what)
		}
		if ok, reason := g.Admit("anthropic", tc.role, 0); !ok {
			t.Fatalf("%s should be admitted through the reserve: %v", tc.what, reason)
		}
	}
	if ReserveRole(capacity.RoleWorker) {
		t.Fatal("ordinary workers must not use the review reserve")
	}
}

// h) reserve_for_review normalization: unset reads the default of one slot,
// an explicit zero turns the reserve off, a negative clamps to zero, and an
// operator-set positive value is passed through.
func TestReserveForReviewConfigNormalization(t *testing.T) {
	cases := []struct {
		v    *int
		want int
	}{
		{nil, 1},
		{reserveIntPtr(0), 0},
		{reserveIntPtr(-3), 0},
		{reserveIntPtr(2), 2},
	}
	for _, tc := range cases {
		if got := config.ReserveForReviewSlots(tc.v); got != tc.want {
			t.Fatalf("ReserveForReviewSlots(%v) = %d, want %d", tc.v, got, tc.want)
		}
	}
	if got := config.ReserveForReviewSlots(nil); got != config.DefaultBackoffReserveForReview {
		t.Fatalf("unset reserve = %d, want the default %d", got, config.DefaultBackoffReserveForReview)
	}
	if got := config.ReserveTokensPerHour(-5); got != 0 {
		t.Fatalf("negative token cap = %d, want 0 (uncapped)", got)
	}
}
