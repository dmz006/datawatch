package autonomous

import (
	"errors"
	"testing"
)

func withLimits(t *testing.T, l *BurnGuardLimits) {
	t.Helper()
	SetBurnGuardLimits(l)
	t.Cleanup(func() { SetBurnGuardLimits(nil) })
}

func defaultLimits() *BurnGuardLimits {
	return &BurnGuardLimits{
		Enabled: true,
		EffortCeiling: map[string]string{
			"docs": "low", "parity": "low", "live_check": "low", "final_gate": "low",
		},
		RetryLimitClaude: 2,
	}
}

func TestEffortCeiling_AppliesPerKind(t *testing.T) {
	withLimits(t, defaultLimits())
	cases := []struct {
		name string
		task Task
		want Effort
	}{
		{"docs title", Task{Title: "Update the docs"}, EffortLow},
		{"parity title", Task{Title: "PWA parity issue"}, EffortLow},
		{"live-check title", Task{Title: "Run the live-check"}, EffortLow},
		{"final gate kind", Task{Title: "x", Kind: "final_gate"}, EffortLow},
		{"final-gate kind spelling", Task{Title: "x", Kind: "final-gate"}, EffortLow},
		{"ordinary task", Task{Title: "Implement the ladder"}, EffortHigh},
	}
	for _, c := range cases {
		got, _, _ := ApplyEffortCeiling(&c.task, nil, EffortHigh, "")
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestEffortCeiling_ExplicitLowerWins_NeverRaised(t *testing.T) {
	l := defaultLimits()
	l.EffortCeiling["docs"] = "medium"
	withLimits(t, l)
	task := &Task{Title: "docs"}
	if got, th, _ := ApplyEffortCeiling(task, nil, EffortLow, "off"); got != EffortLow || th != "off" {
		t.Fatalf("explicit lower raised: effort=%q thinking=%q", got, th)
	}
	if got, th, _ := ApplyEffortCeiling(task, nil, EffortMax, "high"); got != EffortMedium || th != "medium" {
		t.Fatalf("higher not lowered: effort=%q thinking=%q", got, th)
	}
}

func TestEffortCeiling_TaskAndAutomatonOverride(t *testing.T) {
	withLimits(t, defaultLimits())
	auto := &Automation{EffortCeiling: map[string]string{"docs": "medium"}}
	task := &Task{Title: "docs"}
	if got, _, _ := ApplyEffortCeiling(task, auto, EffortHigh, ""); got != EffortMedium {
		t.Fatalf("automaton ceiling: got %q", got)
	}
	task.EffortCeiling = "high"
	if got, _, _ := ApplyEffortCeiling(task, auto, EffortHigh, ""); got != EffortHigh {
		t.Fatalf("task ceiling: got %q", got)
	}
}

func TestEffortCeiling_CheaperBackend(t *testing.T) {
	l := defaultLimits()
	l.EffortCeiling["docs"] = CeilingCheapBackend
	withLimits(t, l)
	got, _, cheaper := ApplyEffortCeiling(&Task{Title: "docs"}, nil, EffortHigh, "")
	if !cheaper || got != EffortHigh {
		t.Fatalf("cheaper=%v effort=%q", cheaper, got)
	}
}

func TestEffortCeiling_DisabledIsNoop(t *testing.T) {
	l := defaultLimits()
	l.Enabled = false
	withLimits(t, l)
	got, th, cheaper := ApplyEffortCeiling(&Task{Title: "docs"}, nil, EffortHigh, "high")
	if got != EffortHigh || th != "high" || cheaper {
		t.Fatalf("disabled changed: %q %q %v", got, th, cheaper)
	}
	SetBurnGuardLimits(nil)
	if got, _, _ := ApplyEffortCeiling(&Task{Title: "docs"}, nil, EffortHigh, ""); got != EffortHigh {
		t.Fatalf("unset changed: %q", got)
	}
}

func TestClaudeRetry_LimitBoundary(t *testing.T) {
	cases := []struct {
		retries int
		want    ClaudeRetryAction
	}{{0, ClaudeRetryAllow}, {1, ClaudeRetryAllow}, {2, ClaudeRetryRoute}, {3, ClaudeRetryRoute}}
	for _, c := range cases {
		d := DecideClaudeRetry("claude-code", true, c.retries, 2, "pi-prompt")
		if d.Action != c.want {
			t.Errorf("retries=%d: got %s want %s", c.retries, d.Action, c.want)
		}
	}
	if d := DecideClaudeRetry("claude-code", true, 2, 2, ""); d.Action != ClaudeRetryHold {
		t.Errorf("no fallback: got %s", d.Action)
	}
	if d := DecideClaudeRetry("claude-code", false, 9, 2, "pi-prompt"); d.Action != ClaudeRetryAllow {
		t.Errorf("first attempt must be allowed")
	}
	if d := DecideClaudeRetry("pi-prompt", true, 9, 2, "x"); d.Action != ClaudeRetryAllow {
		t.Errorf("non-claude must be allowed")
	}
	if d := DecideClaudeRetry("claude-code", true, 9, 0, "x"); d.Action != ClaudeRetryAllow {
		t.Errorf("limit 0 must be a no-op")
	}
}

func TestClaudeRetry_CounterPerTask(t *testing.T) {
	withLimits(t, defaultLimits())
	a, b := &Task{ID: "a"}, &Task{ID: "b"}
	for i := 0; i < 2; i++ {
		if be, err := ApplyClaudeRetryLimit(a, "claude-code", true); be != "claude-code" || err != nil {
			t.Fatalf("retry %d: %q %v", i, be, err)
		}
	}
	if a.ClaudeRetries != 2 || b.ClaudeRetries != 0 {
		t.Fatalf("counters a=%d b=%d", a.ClaudeRetries, b.ClaudeRetries)
	}
	be, err := ApplyClaudeRetryLimit(a, "claude-code", true)
	if be != "pi-prompt" || err != nil || a.ClaudeRetries != 2 {
		t.Fatalf("over limit: %q %v count=%d", be, err, a.ClaudeRetries)
	}
	if be, _ := ApplyClaudeRetryLimit(b, "claude-code", true); be != "claude-code" {
		t.Fatalf("other task affected: %q", be)
	}
}

func TestClaudeRetry_HoldErrorWrapsStall(t *testing.T) {
	err := ClaudeRetryLimitError("t1", "limit")
	if !errors.Is(err, ErrClaudeRetryLimit) || !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("err = %v", err)
	}
}

func TestClaudeRetry_DisabledIsNoop(t *testing.T) {
	l := defaultLimits()
	l.Enabled = false
	withLimits(t, l)
	task := &Task{ID: "a", ClaudeRetries: 50}
	if be, err := ApplyClaudeRetryLimit(task, "claude-code", true); be != "claude-code" || err != nil || task.ClaudeRetries != 50 {
		t.Fatalf("disabled changed: %q %v %d", be, err, task.ClaudeRetries)
	}
}

func TestEffortCeiling_XHighLoweredAndUnsetTakesCeiling(t *testing.T) {
	for _, c := range []string{CeilingLow, CeilingMedium, CeilingHigh} {
		if got := LowerThinkingLevel("xhigh", c); got != c {
			t.Errorf("xhigh under %s = %q, want %q", c, got, c)
		}
	}
	if got := LowerThinkingLevel("", CeilingHigh); got != CeilingHigh {
		t.Errorf("unset thinking = %q, want %q", got, CeilingHigh)
	}
	if got := LowerEffort("", CeilingLow); got != Effort(CeilingLow) {
		t.Errorf("unset effort = %q, want %q", got, CeilingLow)
	}
}
