package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// fakeClock is the injectable clock the registry reads through Backoff.Clock.
type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time          { return f.t }
func (f *fakeClock) Advance(d time.Duration) { f.t = f.t.Add(d) }

func newTestBackoff() (*Backoff, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	b := NewBackoff()
	b.Clock = clk.Now
	return b, clk
}

func TestBackoff_SetBlockedThenSweepClears(t *testing.T) {
	b, clk := newTestBackoff()
	until := clk.Now().Add(30 * time.Minute)
	b.Set("claude-code", until, "rate-limit — reset 12:30", BackoffSourceRateLimit)

	blocked, reason := b.Blocked("claude-code")
	if !blocked {
		t.Fatal("expected claude-code blocked while until is in the future")
	}
	if !strings.Contains(reason, "12:30") {
		t.Errorf("reason %q should include the human-readable until", reason)
	}
	if ok, _ := b.Blocked("pi-prompt"); ok {
		t.Error("an untouched backend must not be blocked")
	}

	clk.Advance(31 * time.Minute)
	if ok, _ := b.Blocked("claude-code"); ok {
		t.Error("Blocked must be false once until has passed, even before Sweep")
	}
	cleared := b.Sweep(clk.Now())
	if len(cleared) != 1 || cleared[0] != "claude-code" {
		t.Fatalf("Sweep cleared %v, want [claude-code]", cleared)
	}
	if len(b.Active()) != 0 {
		t.Errorf("Active after Sweep = %v, want empty", b.Active())
	}
}

func TestBackoff_SweepKeepsFutureEntries(t *testing.T) {
	b, clk := newTestBackoff()
	b.Set("a", clk.Now().Add(time.Minute), "r", BackoffSourceRateLimit)
	b.Set("b", clk.Now().Add(time.Hour), "r", BackoffSourceOperator)
	clk.Advance(2 * time.Minute)
	if got := b.Sweep(clk.Now()); len(got) != 1 || got[0] != "a" {
		t.Fatalf("Sweep = %v, want [a]", got)
	}
	if ok, _ := b.Blocked("b"); !ok {
		t.Error("b should still be blocked")
	}
	// until == now counts as expired.
	clk.Advance(58 * time.Minute)
	if got := b.Sweep(clk.Now()); len(got) != 1 || got[0] != "b" {
		t.Fatalf("Sweep at until==now = %v, want [b]", got)
	}
}

func TestBackoff_ClearAndClearAll(t *testing.T) {
	b, clk := newTestBackoff()
	until := clk.Now().Add(time.Hour)
	b.Set("a", until, "r", BackoffSourceOperator)
	b.Set("b", until, "r", BackoffSourceOperator)
	b.Set("c", until, "r", BackoffSourceOperator)

	if !b.Clear("a") {
		t.Error("Clear(a) should report an entry existed")
	}
	if b.Clear("a") {
		t.Error("second Clear(a) should report nothing existed")
	}
	if n := b.ClearAll(); n != 2 {
		t.Errorf("ClearAll = %d, want 2", n)
	}
	if n := b.ClearAll(); n != 0 {
		t.Errorf("ClearAll on empty = %d, want 0", n)
	}
}

func TestBackoff_ActiveDeterministicOrder(t *testing.T) {
	b, clk := newTestBackoff()
	until := clk.Now().Add(time.Hour)
	for _, name := range []string{"pi-prompt", "claude-code", "opencode", "aider"} {
		b.Set(name, until, "r", BackoffSourceRateLimit)
	}
	var got []string
	for _, e := range b.Active() {
		got = append(got, e.Backend)
	}
	want := "aider,claude-code,opencode,pi-prompt"
	if strings.Join(got, ",") != want {
		t.Errorf("Active order = %v, want %s", got, want)
	}
}

func TestBackoff_SetReplacesEntry(t *testing.T) {
	b, clk := newTestBackoff()
	b.Set("a", clk.Now().Add(time.Minute), "first", BackoffSourceRateLimit)
	b.Set("a", clk.Now().Add(time.Hour), "second", BackoffSourceOperator)
	act := b.Active()
	if len(act) != 1 || act[0].Reason != "second" || act[0].Source != BackoffSourceOperator {
		t.Fatalf("Active = %+v, want the replaced entry", act)
	}
}

// The max back-off used when no reset time is parsed comes from
// config BackoffMaxWaitMinutes; the default is 60 and values < 1 clamp to it.
func TestBackoff_MaxBackoffDeadlineFromConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Autonomous.BackoffMaxWaitMinutes != 60 {
		t.Fatalf("default BackoffMaxWaitMinutes = %d, want 60", cfg.Autonomous.BackoffMaxWaitMinutes)
	}
	cfg.Autonomous.BackoffMaxWaitMinutes = 15

	b, clk := newTestBackoff()
	maxWait := time.Duration(cfg.Autonomous.BackoffMaxWaitMinutes) * time.Minute
	b.Set("claude-code", clk.Now().Add(maxWait), "rate-limit — no reset time (max back-off)", BackoffSourceRateLimit)

	clk.Advance(14 * time.Minute)
	if ok, _ := b.Blocked("claude-code"); !ok {
		t.Error("still blocked 14m into a 15m max back-off")
	}
	clk.Advance(time.Minute)
	if ok, _ := b.Blocked("claude-code"); ok {
		t.Error("max back-off must expire after BackoffMaxWaitMinutes")
	}
}

// TestBackoff_RungSerialisesOnHoldSetOnly covers the ladder-rung field:
// SetWithRung (the burn guard's hold_claude hold) puts the rung name and the
// reason text on the same entry, while SetWithReset (a rate-limit flag) and
// Set (operator) leave it out of the JSON entirely. The fixed clock sits in a
// non-UTC zone so a regression that compares zones or drops the zone survives
// in the serialised until.
func TestBackoff_RungSerialisesOnHoldSetOnly(t *testing.T) {
	zone := time.FixedZone("IST", 5*3600+30*60)
	now := time.Date(2026, 10, 22, 20, 0, 0, 0, zone)
	mk := func() *Backoff {
		b := NewBackoff()
		b.Clock = func() time.Time { return now }
		return b
	}

	until := now.Add(15 * time.Minute)

	hold := mk()
	hold.SetWithRung("claude-code", until, "burn guard: weekly burn projects over the pace line", "burn_guard", true, "hold_claude")
	rate := mk()
	rate.SetWithReset("claude-code", until, "rate-limit — reset 20:15", BackoffSourceRateLimit, true)
	op := mk()
	op.Set("claude-code", until, "paused by operator", BackoffSourceOperator)

	decodes := func(b *Backoff) map[string]any {
		raw, err := json.Marshal(b.Live())
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out []map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(out) != 1 {
			t.Fatalf("Live() = %v, want one entry", out)
		}
		return out[0]
	}

	if got := decodes(hold); got["rung"] != "hold_claude" || got["reason"] != "burn guard: weekly burn projects over the pace line" {
		t.Errorf("hold entry = %v, want rung hold_claude with its reason text", got)
	}
	for name, b := range map[string]*Backoff{"rate_limit": rate, "operator": op} {
		got := decodes(b)
		if _, present := got["rung"]; present {
			t.Errorf("%s entry = %v, want no rung key", name, got)
		}
	}

	// The entry struct keeps the deadline in the caller's (non-UTC) zone and
	// Blocked still respects it from the fixed clock. The Blocked message
	// renders the deadline in the entry's zone; the free-form reason text
	// itself lives on the entry (and in the serialised output above).
	if e := hold.Live()[0]; !e.Until.Equal(until) || e.Rung != "hold_claude" {
		t.Errorf("Live()[0] = %+v, want until %s with rung hold_claude", e, until)
	}
	if blocked, reason := hold.Blocked("claude-code"); !blocked || !strings.Contains(reason, "backed off until 20:15 IST") {
		t.Errorf("hold Blocked = (%v, %q), want blocked with the IST deadline", blocked, reason)
	}
	if ratelimit := rate.Live()[0]; ratelimit.Rung != "" {
		t.Errorf("rate-limit Entry.Rung = %q, want empty", ratelimit.Rung)
	}
	// Re-setting with SetWithReset (a later rate limit on the same backend)
	// must drop the rung, matching what the burn guard releases.
	hold.SetWithReset("claude-code", until.Add(time.Minute), "rate-limit — reset 20:30", BackoffSourceRateLimit, true)
	{
		got := decodes(hold)
		if _, present := got["rung"]; present {
			t.Errorf("entry after SetWithReset = %v, want no rung key", got)
		}
	}
}
