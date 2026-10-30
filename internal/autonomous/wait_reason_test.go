package autonomous

import (
	"testing"
	"time"
)

// waitReasonClock is the fixed clock every wait-reason test reads: 12:00 UTC.
func waitReasonClock() func() time.Time {
	clk := &fakeClock{t: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	return clk.Now
}

func assertWaitText(t *testing.T, got, want string, cause WaitCause) {
	t.Helper()
	if got != want {
		t.Errorf("text = %q\nwant   %q", got, want)
	}
	if c := ClassifyWaitReason(got); c != cause {
		t.Errorf("ClassifyWaitReason(%q) = %q, want %q", got, c, cause)
	}
}

func TestWaitReason_PoolFullNamesHolder(t *testing.T) {
	got := PoolFullWaitText("llm:pi-prompt", 2, 2, []string{"task-a", "task-b"})
	assertWaitText(t, got, "pool llm:pi-prompt full (2/2, held by task-a,task-b)", WaitCausePoolFull)

	got = PoolFullWaitText("llm:pi-prompt", 1, 1, nil)
	assertWaitText(t, got, "pool llm:pi-prompt full (1/1, held by other sessions)", WaitCausePoolFull)

	// A pool back-pressured by a gate is classified by that gate.
	bp := "pool llm:claude-code backpressure: " + BackoffWaitText(waitReasonClock(), "claude-code",
		time.Date(2026, 10, 22, 12, 30, 0, 0, time.UTC), true, "")
	if c := ClassifyWaitReason(bp); c != WaitCauseBackoff {
		t.Errorf("backpressure reason classified %q, want %q", c, WaitCauseBackoff)
	}
}

func TestWaitReason_NodeBusyNamesHolder(t *testing.T) {
	got := NodeBusyWaitText("gpu-box", 1, 1, []string{"task-c"})
	assertWaitText(t, got, "waiting for node gpu-box: 1/1 engines in use, held by task-c", WaitCauseNodeBusy)
}

func TestWaitReason_BackoffWithResetTime(t *testing.T) {
	until := time.Date(2026, 10, 22, 12, 42, 0, 0, time.UTC)
	got := BackoffWaitText(waitReasonClock(), "claude-code", until, true, "")
	assertWaitText(t, got, "backend claude-code backed off until 12:42 UTC (resets in 42m)", WaitCauseBackoff)

	got = BackoffWaitText(waitReasonClock(), "claude-code", time.Date(2026, 10, 22, 13, 5, 0, 0, time.UTC), true, "weekly limit")
	assertWaitText(t, got, "backend claude-code backed off until 13:05 UTC (resets in 1h05m) — weekly limit", WaitCauseBackoff)

	// The registry words its Blocked reason through the same builder, and an
	// operator-set flag counts as a known reset.
	b, clk := newTestBackoff()
	b.Set("pi-prompt", clk.Now().Add(42*time.Minute), "maintenance", BackoffSourceOperator)
	_, reason := b.Blocked("pi-prompt")
	assertWaitText(t, reason, "backend pi-prompt backed off until 12:42 UTC (resets in 42m) — maintenance", WaitCauseBackoff)
}

func TestWaitReason_BackoffResetUnknown(t *testing.T) {
	until := time.Date(2026, 10, 22, 12, 15, 0, 0, time.UTC)
	got := BackoffWaitText(waitReasonClock(), "claude-code", until, false, "")
	assertWaitText(t, got,
		"backend claude-code backed off until 12:15 UTC (reset time unknown; next attempt allowed then, in 15m)",
		WaitCauseBackoff)

	b, clk := newTestBackoff()
	b.SetWithReset("claude-code", clk.Now().Add(15*time.Minute), "rate-limit — no reset time (max back-off)", BackoffSourceRateLimit, false)
	_, reason := b.Blocked("claude-code")
	assertWaitText(t, reason,
		"backend claude-code backed off until 12:15 UTC (reset time unknown; next attempt allowed then, in 15m)",
		WaitCauseBackoff)
	if live := b.Live(); len(live) != 1 || live[0].ResetKnown {
		t.Errorf("Live() = %+v, want one entry with ResetKnown=false", live)
	}
}

func TestWaitReason_BudgetWindowFullShowsSpendAndRollover(t *testing.T) {
	rolls := time.Date(2026, 10, 22, 14, 5, 0, 0, time.UTC)
	got := BudgetWaitText(waitReasonClock(), "claude-code", 20, 0, 20, &rolls)
	assertWaitText(t, got,
		"budget window full: claude-code spent $20.00 of $20.00 ceiling; window rolls over at 2026-10-22T14:05:00Z (in 2h05m)",
		WaitCauseBudget)

	got = BudgetWaitText(waitReasonClock(), "claude-code", 15, 5.5, 20, &rolls)
	assertWaitText(t, got,
		"budget window full: claude-code spent $20.50 (local $15.00, peers $5.50) of $20.00 ceiling; window rolls over at 2026-10-22T14:05:00Z (in 2h05m)",
		WaitCauseBudget)

	got = BudgetWaitText(waitReasonClock(), "claude-code", 0, 21, 20, nil)
	assertWaitText(t, got,
		"budget window full: claude-code spent $21.00 (local $0.00, peers $21.00) of $20.00 ceiling; window rolls over at when peer spend ages out",
		WaitCauseBudget)
}

func TestWaitReason_GlobalCooldown(t *testing.T) {
	until := time.Date(2026, 10, 22, 12, 10, 0, 0, time.UTC)
	got := GlobalCooldownWaitText(waitReasonClock(), until)
	assertWaitText(t, got, "global cooldown until 12:10 UTC (in 10m)", WaitCauseGlobalCooldown)

	got = GlobalCooldownWaitText(waitReasonClock(), time.Time{})
	assertWaitText(t, got, "global cooldown active", WaitCauseGlobalCooldown)
}

func TestWaitReason_AwaitExternal(t *testing.T) {
	got := AwaitExternalWaitText("CI run 42 to finish")
	assertWaitText(t, got, "await_external: CI run 42 to finish", WaitCauseAwaitExternal)
	if got != AwaitExternalWaitReasonFor("CI run 42 to finish") {
		t.Errorf("AwaitExternalWaitReasonFor diverged from AwaitExternalWaitText: %q", AwaitExternalWaitReasonFor("CI run 42 to finish"))
	}

	assertWaitText(t, AwaitExternalWaitText("  "), "await_external", WaitCauseAwaitExternal)

	// The tree-lock text is the remaining cause; it names the holder.
	tl := TreeLockWaitText("/work/repo", "t1", "prd9", time.Date(2026, 10, 22, 11, 55, 0, 0, time.UTC).Unix())
	assertWaitText(t, tl, "tree lock: /work/repo held by task t1 (PRD prd9) since 2026-10-22T11:55:00Z", WaitCauseTreeLock)
	assertWaitText(t, TreeLockWaitText("/work/repo", "", "", 0),
		"tree lock: /work/repo held by unknown holder (record unreadable)", WaitCauseTreeLock)

	if c := ClassifyWaitReason("something else entirely"); c != "" {
		t.Errorf("unknown reason classified %q, want empty", c)
	}
}

// readableHolderNames is the test holder resolver: a handful of well-known
// ids map to their operator-readable names, everything else maps to "".
// ("Verifying: …" is how a verifying holder is worded.)
func readableHolderNames(id string) string {
	switch id {
	case "task-a":
		return "Fix crash / Handle panics (c3e70011)"
	case "task-b":
		return "Verifying: Handle panics (49890003)"
	case "task-c":
		return "Deploy / Wire API (a1b2c3d4)"
	case "task-d":
		return "Docs / Write README (deadbeef)"
	}
	return ""
}

func TestReadableWaitText_PoolFullNamesHolders(t *testing.T) {
	reason := PoolFullWaitText("llm:pi-prompt", 2, 2, []string{"task-a", "task-b"})
	got := ReadableWaitText(reason, WaitReasonHolders(reason), readableHolderNames)
	want := "pool llm:pi-prompt full (2/2), held by Fix crash / Handle panics (c3e70011) and Verifying: Handle panics (49890003)"
	if got != want {
		t.Errorf("readable = %q\nwant    %q", got, want)
	}
	// The raw sentence is untouched and still classifies the same way.
	if ClassifyWaitReason(reason) != WaitCausePoolFull {
		t.Errorf("raw reason reclassified: %q", reason)
	}
	if ReadableWaitText(reason, []string{"task-a", "task-b"}, nil) !=
		"pool llm:pi-prompt full (2/2), held by task-a and task-b" {
		t.Errorf("no resolver: want raw ids in the readable shape")
	}

	// Three holders list as "A, B and C".
	three := PoolFullWaitText("llm:pi-prompt", 3, 3, []string{"task-a", "task-b", "task-d"})
	got = ReadableWaitText(three, WaitReasonHolders(three), readableHolderNames)
	want = "pool llm:pi-prompt full (3/3), held by Fix crash / Handle panics (c3e70011), Verifying: Handle panics (49890003) and Docs / Write README (deadbeef)"
	if got != want {
		t.Errorf("three holders readable = %q\nwant          %q", got, want)
	}
}

func TestReadableWaitText_NodeBusyNamesHolders(t *testing.T) {
	reason := NodeBusyWaitText("gpu-box", 1, 1, []string{"task-c"})
	if got, want := ReadableWaitText(reason, WaitReasonHolders(reason), readableHolderNames),
		"waiting for node gpu-box: 1/1 engines in use, held by Deploy / Wire API (a1b2c3d4)"; got != want {
		t.Errorf("readable = %q\nwant    %q", got, want)
	}
}

func TestReadableWaitText_NoHoldersOrUnknownReasonsPassThrough(t *testing.T) {
	// A full pool with no known holder keeps "other sessions".
	none := PoolFullWaitText("llm:pi-prompt", 1, 1, nil)
	if got, want := ReadableWaitText(none, WaitReasonHolders(none), readableHolderNames),
		"pool llm:pi-prompt full (1/1), held by other sessions"; got != want {
		t.Errorf("no holders readable = %q, want %q", got, want)
	}

	// Reasons that name no holders come back unchanged.
	until := time.Date(2026, 10, 22, 12, 42, 0, 0, time.UTC)
	bo := BackoffWaitText(waitReasonClock(), "claude-code", until, true, "")
	if ReadableWaitText(bo, WaitReasonHolders(bo), readableHolderNames) != bo {
		t.Errorf("backoff reason changed: %q", bo)
	}
	tlock := TreeLockWaitText("/work/repo", "t1", "prd9", time.Now().Unix())
	if ReadableWaitText(tlock, WaitReasonHolders(tlock), readableHolderNames) != tlock {
		t.Errorf("tree-lock reason changed: %q", tlock)
	}
	if ReadableWaitText("unknown reason", nil, readableHolderNames) != "unknown reason" {
		t.Errorf("unknown reason changed")
	}
	// Malformed pool text is returned unchanged rather than re-worded half-wrong.
	if ReadableWaitText("pool x full (bad)", nil, readableHolderNames) != "pool x full (bad)" {
		t.Errorf("malformed pool reason changed")
	}
	if ReadableWaitText("waiting for node x: bad engines in use", nil, readableHolderNames) != "waiting for node x: bad engines in use" {
		t.Errorf("malformed node reason changed")
	}
	if ReadableWaitText("", nil, readableHolderNames) != "" {
		t.Errorf("empty reason not preserved")
	}
	// A blank resolved name falls back to the raw holder id.
	mixed := PoolFullWaitText("llm:pi-prompt", 2, 2, []string{"task-a", "ghost"})
	got := ReadableWaitText(mixed, WaitReasonHolders(mixed), readableHolderNames)
	if want := "pool llm:pi-prompt full (2/2), held by Fix crash / Handle panics (c3e70011) and ghost"; got != want {
		t.Errorf("mixed holders readable = %q, want %q", got, want)
	}
}

func TestWaitReasonHolders_ExtractsRawIds(t *testing.T) {
	if got := WaitReasonHolders(PoolFullWaitText("llm:pi-prompt", 2, 2, []string{"task-a", "task-b"})); len(got) != 2 || got[0] != "task-a" || got[1] != "task-b" {
		t.Errorf("pool holders = %v, want [task-a task-b]", got)
	}
	if got := WaitReasonHolders(NodeBusyWaitText("gpu-box", 1, 1, []string{"task-c"})); len(got) != 1 || got[0] != "task-c" {
		t.Errorf("node holders = %v, want [task-c]", got)
	}
	if got := WaitReasonHolders(PoolFullWaitText("llm:pi-prompt", 1, 1, nil)); got != nil {
		t.Errorf("no holders = %v, want nil", got)
	}
	if got := WaitReasonHolders(BackoffWaitText(waitReasonClock(), "claude-code",
		time.Date(2026, 10, 22, 12, 42, 0, 0, time.UTC), true, "")); got != nil {
		t.Errorf("backoff holders = %v, want nil", got)
	}
	if got := WaitReasonHolders("mystery"); got != nil {
		t.Errorf("unknown holders = %v, want nil", got)
	}
}

func TestWaitReason_AwaitDecisionTextAndPredicate(t *testing.T) {
	got := AwaitDecisionWaitText("q-1")
	assertWaitText(t, got, "await_decision: q-1", WaitCauseAwaitDecision)
	assertWaitText(t, AwaitDecisionWaitText("  "), "await_decision", WaitCauseAwaitDecision)

	for _, c := range []struct {
		reason string
		want   bool
	}{
		{"await_decision", true},
		{"await_decision: q-1", true},
		{"  await_decision: q-1  ", true},
		{"await_decision — pool llm:x full (1/1)", true},
		{"await_external: CI run #42", false},
		{"await_decisions", false},
		{"tree lock: t1", false},
		{"", false},
	} {
		if got := IsAwaitDecisionWaitReason(c.reason); got != c.want {
			t.Errorf("IsAwaitDecisionWaitReason(%q) = %v; want %v", c.reason, got, c.want)
		}
	}
	// The two await sub-reasons stay distinct.
	if IsAwaitExternalWaitReason("await_decision: q-1") {
		t.Error("an await_decision reason must not read as await_external")
	}
}
