package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Judge resume at boot: a guardrail judgment that was in flight for a unit
// when the daemon restarted is resumed, not re-run.
//   - a live guardrail-judge session (adopted at boot) is re-attached to —
//     a waiter waits on its verdict, and no second judge spawns;
//   - a surviving, still-unrecorded result file is consumed as the
//     verdict — no judge spawns;
//   - with neither present, the judge runs once, as usual.

// withJudgeResumeBudget shortens the manager's wait on a re-attached
// judge's verdict (default: the full judge budget + 90s) for one test.
func withJudgeResumeBudget(t *testing.T, d time.Duration) {
	t.Helper()
	orig := judgeResumeWaitBudget
	t.Cleanup(func() { judgeResumeWaitBudget = orig })
	judgeResumeWaitBudget = func(int) time.Duration { return d }
}

func judgeResumeManager(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	m, prd := verifyingBootManager(t, []Story{
		{Title: "story", Tasks: []Task{{Title: "task", Status: TaskPending}}},
	})
	return m, prd
}

func judgeResumeInvocation(prd *PRD, unitID, guardrail string) GuardrailInvocation {
	return GuardrailInvocation{PRDID: prd.ID, Level: "task", UnitID: unitID, Guardrail: guardrail}
}

func TestResolveJudgeResumeDecisionOrder(t *testing.T) {
	file := func() (*GuardrailVerdict, bool) { return &GuardrailVerdict{Outcome: "pass"}, true }
	noneFile := func() (*GuardrailVerdict, bool) { return nil, false }
	emptyFile := func() (*GuardrailVerdict, bool) { return nil, true }
	live := func(sid string) bool { return sid == "jsess" }
	dead := func(sid string) bool { return false }

	cases := []struct {
		name      string
		sessionID string
		alive     func(string) bool
		verdict   func() (*GuardrailVerdict, bool)
		want      JudgeResume
	}{
		{"live session outranks surviving file", "jsess", live, file, JudgeResumeReattach},
		{"dead session with surviving file consumes", "jsess", dead, file, JudgeResumeConsume},
		{"no session with surviving file consumes", "", nil, file, JudgeResumeConsume},
		{"live session, unreadable file, still re-attaches", "jsess", live, noneFile, JudgeResumeReattach},
		{"surviving file that is not a verdict falls through", "", nil, emptyFile, JudgeResumeRunOnce},
		{"nothing live, nothing to consume runs once", "", nil, noneFile, JudgeResumeRunOnce},
		{"tracked session with no liveness hook runs once", "jsess", nil, noneFile, JudgeResumeRunOnce},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveJudgeResume(tc.sessionID, tc.alive, tc.verdict); got != tc.want {
				t.Fatalf("ResolveJudgeResume = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestJudgeResumeReattach(t *testing.T) {
	withJudgeResumeBudget(t, 5*time.Second)
	m, prd := judgeResumeManager(t)
	unit := prd.Story[0].Tasks[0]

	// A live guardrail-judge session survives the restart; boot adoption
	// tracks it.
	m.ReconcileJudges(&fakeJudgeHost{live: []JudgeSession{{ID: "jsess-1", UnitID: unit.ID, Started: time.Now()}}})
	if got := m.judgeAdopts[unit.ID]; got != "jsess-1" {
		t.Fatalf("boot adoption: judgeAdopts[%q] = %q, want jsess-1", unit.ID, got)
	}

	var attachMu sync.Mutex
	attachCalls := 0
	m.SetJudgeReattachFn(func(prdID string, inv GuardrailInvocation, sessionID string) {
		attachMu.Lock()
		attachCalls++
		attachMu.Unlock()
		if prdID != prd.ID || inv.UnitID != unit.ID || inv.Guardrail != "rules" || sessionID != "jsess-1" {
			t.Errorf("attach args = (%q, %q, %q, %q), want (%q, %q, rules, jsess-1)", prdID, inv.UnitID, inv.Guardrail, sessionID, prd.ID, unit.ID)
		}
		// The daemon's background waiter: delivers the surviving session's
		// verdict once it comes in.
		go func() {
			time.Sleep(20 * time.Millisecond)
			if !m.ConsumeJudgeResult(prdID, unit.ID, &GuardrailVerdict{
				Outcome: "pass", Summary: "from the surviving judge", JudgeSessionID: sessionID,
			}) {
				t.Errorf("ConsumeJudgeResult = false; the waiting guardrail flow should take the verdict")
			}
		}()
	})

	fnCalls := 0
	fn := func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		fnCalls++
		return GuardrailVerdict{Outcome: "pass", Summary: "a fresh judge ran"}, nil
	}

	v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, judgeResumeInvocation(prd, unit.ID, "rules"), fn)
	if err != nil {
		t.Fatalf("runGuardrailWithInfraRetry: %v", err)
	}
	if v.Summary != "from the surviving judge" {
		t.Errorf("summary = %q, want the surviving judge's verdict, not a fresh run", v.Summary)
	}
	if v.JudgeSessionID != "jsess-1" {
		t.Errorf("JudgeSessionID = %q, want the surviving session jsess-1", v.JudgeSessionID)
	}
	attachMu.Lock()
	after := attachCalls
	attachMu.Unlock()
	if after != 1 {
		t.Errorf("attach calls = %d, want exactly 1", after)
	}
	if fnCalls != 0 {
		t.Errorf("fresh judge ran %d time(s); a re-attached session must not be re-spawned", fnCalls)
	}
}

func TestJudgeResumeConsumeResult(t *testing.T) {
	m, prd := judgeResumeManager(t)
	unit := prd.Story[0].Tasks[0]

	m.SetJudgeResultReader(func(prdID string, inv GuardrailInvocation) (*GuardrailVerdict, bool) {
		if prdID != prd.ID || inv.UnitID != unit.ID || inv.Guardrail != "rules" {
			t.Errorf("reader args = (%q, %q, %q), want (%q, %q, rules)", prdID, inv.UnitID, inv.Guardrail, prd.ID, unit.ID)
		}
		return &GuardrailVerdict{
			Outcome: "warn", Summary: "recorded from the surviving file",
			Issues: []string{"note"}, JudgeOutputPath: "/tmp/judge-out.json",
		}, true
	})

	fnCalls := 0
	fn := func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		fnCalls++
		return GuardrailVerdict{Outcome: "pass", Summary: "a fresh judge ran"}, nil
	}

	v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, judgeResumeInvocation(prd, unit.ID, "rules"), fn)
	if err != nil {
		t.Fatalf("runGuardrailWithInfraRetry: %v", err)
	}
	if v.Outcome != "warn" || v.Summary != "recorded from the surviving file" {
		t.Errorf("got %+v, want the consumed surviving verdict", v)
	}
	if v.JudgeOutputPath != "/tmp/judge-out.json" {
		t.Errorf("JudgeOutputPath = %q, want the surviving file path carried through for cleanup", v.JudgeOutputPath)
	}
	if fnCalls != 0 {
		t.Errorf("fresh judge ran %d time(s); a consumed result file must not re-judge", fnCalls)
	}
}

func TestJudgeResumeNeitherRunsJudgeOnce(t *testing.T) {
	m, prd := judgeResumeManager(t)
	unit := prd.Story[0].Tasks[0]

	m.SetJudgeResultReader(func(prdID string, inv GuardrailInvocation) (*GuardrailVerdict, bool) {
		// Nothing survived the restart.
		return nil, false
	})

	fnCalls := 0
	fn := func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		fnCalls++
		return GuardrailVerdict{Outcome: "pass", Summary: "fresh judgment"}, nil
	}

	v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, judgeResumeInvocation(prd, unit.ID, "rules"), fn)
	if err != nil {
		t.Fatalf("runGuardrailWithInfraRetry: %v", err)
	}
	if v.Summary != "fresh judgment" {
		t.Errorf("summary = %q, want the fresh judgment", v.Summary)
	}
	if fnCalls != 1 {
		t.Errorf("fresh judge ran %d time(s), want exactly 1", fnCalls)
	}

	// A later judgment for the same unit is a normal run again — the resume
	// decision is per guardrail invocation, not sticky.
	v2, err := m.runGuardrailWithInfraRetry(context.Background(), prd, judgeResumeInvocation(prd, unit.ID, "rules"), fn)
	if err != nil || v2.Summary != "fresh judgment" {
		t.Fatalf("second run = (%q, %v), want the fresh judgment again", v2.Summary, err)
	}
	if fnCalls != 2 {
		t.Errorf("fresh judge ran %d time(s) in total, want 2", fnCalls)
	}
}

func TestJudgeResumeNothingWiredRunsJudgeOnce(t *testing.T) {
	m, prd := judgeResumeManager(t)
	unit := prd.Story[0].Tasks[0]

	// No resume hooks at all (a bare manager, e.g. before daemon-side
	// wiring): the guardrail flow behaves exactly as pre-resume.
	fnCalls := 0
	fn := func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		fnCalls++
		return GuardrailVerdict{Outcome: "pass", Summary: "fresh judgment"}, nil
	}
	v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, judgeResumeInvocation(prd, unit.ID, "rules"), fn)
	if err != nil || v.Summary != "fresh judgment" || fnCalls != 1 {
		t.Fatalf("got (%+v, %v, fnCalls=%d), want the fresh judgment run once", v, err, fnCalls)
	}
}

func TestJudgeResumeReattachBudgetFallsThroughToJudgeRun(t *testing.T) {
	withJudgeResumeBudget(t, 150*time.Millisecond)
	m, prd := judgeResumeManager(t)
	unit := prd.Story[0].Tasks[0]

	m.ReconcileJudges(&fakeJudgeHost{live: []JudgeSession{{ID: "jsess-2", UnitID: unit.ID, Started: time.Now()}}})
	m.SetJudgeReattachFn(func(prdID string, inv GuardrailInvocation, sessionID string) {
		// Attached, but the surviving judge never delivers within budget.
	})

	fnCalls := 0
	fn := func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		fnCalls++
		return GuardrailVerdict{Outcome: "pass", Summary: "fresh judgment after the wait budget"}, nil
	}

	v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, judgeResumeInvocation(prd, unit.ID, "rules"), fn)
	if err != nil {
		t.Fatalf("runGuardrailWithInfraRetry: %v", err)
	}
	if v.Summary != "fresh judgment after the wait budget" {
		t.Errorf("summary = %q, want the fall-through fresh judgment", v.Summary)
	}
	if fnCalls != 1 {
		t.Errorf("fresh judge ran %d time(s), want exactly 1 (its daemon-side adoption re-waits on the live session)", fnCalls)
	}
	// The waiter registration is cleaned up after the budget expires — a
	// late delivery is a no-op, not a crash.
	if !m.ConsumeJudgeResult(prd.ID, unit.ID, &GuardrailVerdict{Outcome: "pass"}) {
		// expected: nobody waits any more
	}
}

func TestJudgeResumeConsumeWithoutWaiterIsNoop(t *testing.T) {
	m, _ := judgeResumeManager(t)
	v := GuardrailVerdict{Outcome: "pass"}
	if m.ConsumeJudgeResult("prd-x", "unit-y", &v) {
		t.Error("ConsumeJudgeResult must report false when no guardrail flow waits on that (PRD, unit) pair")
	}
	if m.ConsumeJudgeResult("prd-x", "unit-y", nil) {
		t.Error("a nil (giveup) delivery must also report false with no waiter present")
	}
}

func TestJudgeResumeReattachDeadSessionNoResultFallsThrough(t *testing.T) {
	// A generous budget on purpose: the daemon-side give-up signal (nil)
	// must short-circuit the fall-through immediately — the waiting flow
	// must not burn the wait budget when the session ended with no result.
	withJudgeResumeBudget(t, 5*time.Second)
	m, prd := judgeResumeManager(t)
	unit := prd.Story[0].Tasks[0]

	m.ReconcileJudges(&fakeJudgeHost{live: []JudgeSession{{ID: "jsess-4", UnitID: unit.ID, Started: time.Now()}}})
	m.SetJudgeReattachFn(func(prdID string, inv GuardrailInvocation, sessionID string) {
		// Daemon-side waiter: the session is already dead and never wrote
		// a result file — signal nil (nothing to recover).
		go func() {
			time.Sleep(20 * time.Millisecond)
			if !m.ConsumeJudgeResult(prdID, unit.ID, nil) {
				t.Errorf("giveup delivery should reach the waiting flow")
			}
		}()
	})

	fnCalls := 0
	fn := func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		fnCalls++
		return GuardrailVerdict{Outcome: "pass", Summary: "fresh judgment after the give-up"}, nil
	}

	start := time.Now()
	v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, judgeResumeInvocation(prd, unit.ID, "rules"), fn)
	took := time.Since(start)
	if err != nil {
		t.Fatalf("runGuardrailWithInfraRetry: %v", err)
	}
	if v.Summary != "fresh judgment after the give-up" {
		t.Errorf("summary = %q, want the fall-through fresh judgment", v.Summary)
	}
	if fnCalls != 1 {
		t.Errorf("fresh judge ran %d time(s), want exactly 1", fnCalls)
	}
	if took > time.Second {
		t.Errorf("fall-through took %v; a nil give-up delivery must not burn the wait budget", took)
	}
}

func TestJudgeResumeConsumeBeatsFallbackButNotLiveSession(t *testing.T) {
	// A tracked-but-dead session plus a surviving file consumes the file;
	// a live session plus the same file re-attaches, not consumes.
	t.Run("dead session consumes file", func(t *testing.T) {
		m, prd := judgeResumeManager(t)
		unit := prd.Story[0].Tasks[0]
		m.ReconcileJudges(&fakeJudgeHost{live: nil})
		m.setAdoptedJudge(unit.ID, "jsess-dead")
		m.SetJudgeResultReader(func(prdID string, inv GuardrailInvocation) (*GuardrailVerdict, bool) {
			return &GuardrailVerdict{Outcome: "block", Summary: "from the file"}, true
		})
		v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, judgeResumeInvocation(prd, unit.ID, "rules"), func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
			t.Error("fresh judge must not run")
			return GuardrailVerdict{Outcome: "pass"}, nil
		})
		if err != nil || v.Summary != "from the file" {
			t.Fatalf("got (%+v, %v), want the consumed file verdict", v, err)
		}
	})

	t.Run("live session beats file", func(t *testing.T) {
		withJudgeResumeBudget(t, 5*time.Second)
		m, prd := judgeResumeManager(t)
		unit := prd.Story[0].Tasks[0]
		m.ReconcileJudges(&fakeJudgeHost{live: []JudgeSession{{ID: "jsess-3", UnitID: unit.ID, Started: time.Now()}}})
		m.SetJudgeResultReader(func(prdID string, inv GuardrailInvocation) (*GuardrailVerdict, bool) {
			return &GuardrailVerdict{Outcome: "warn", Summary: "stale file"}, true
		})
		m.SetJudgeReattachFn(func(prdID string, inv GuardrailInvocation, sessionID string) {
			go func() {
				time.Sleep(10 * time.Millisecond)
				m.ConsumeJudgeResult(prdID, unit.ID, &GuardrailVerdict{Outcome: "pass", Summary: "live session verdict"})
			}()
		})
		v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, judgeResumeInvocation(prd, unit.ID, "rules"), func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
			t.Error("fresh judge must not run")
			return GuardrailVerdict{Outcome: "pass"}, nil
		})
		if err != nil || v.Summary != "live session verdict" {
			t.Fatalf("got (%+v, %v), want the live session's verdict, not the stale file", v, err)
		}
	})
}
