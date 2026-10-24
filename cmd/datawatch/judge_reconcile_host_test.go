// judgeReconcileHost is the host-side half of the judge reconcile: it is the
// only place in cmd/datawatch that turns PlanJudgeReconcile's intent into a
// manager kill or a registry/ledger bind. Every method is tested against the
// real session.Manager (temp data dir, no tmux pane), the real capacity
// ledger, and a judges verifierRegistry.
package main

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

func newSessionTestManager(t *testing.T) *session.Manager {
	t.Helper()
	mgr, err := session.NewManager("h", t.TempDir(), "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

func saveJudgeSession(t *testing.T, mgr *session.Manager, id, name string, state session.State, createdAt time.Time) {
	t.Helper()
	if err := mgr.SaveSession(&session.Session{
		ID: id, FullID: "h-" + id, Name: name, State: state,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}
}

// LiveJudges returns exactly the guardrail-judge sessions that are still
// unfinished (running or waiting_input), mapped back to their unit id and
// start time; finished and non-judge sessions are excluded.
func TestJudgeReconcileHost_LiveJudges_OnlyUnfinishedJudgeSessions(t *testing.T) {
	mgr := newSessionTestManager(t)
	jname := func(unit string) string { return judgeSessionNamePrefix + ":" + unit }
	start := time.Date(2026, 5, 3, 1, 2, 3, 0, time.UTC)
	// live
	saveJudgeSession(t, mgr, "aaaa", jname("u1"), session.StateRunning, start)
	saveJudgeSession(t, mgr, "bbbb", jname("u2"), session.StateWaitingInput, start.Add(10*time.Second))
	// finished — must not be listed
	saveJudgeSession(t, mgr, "cccc", jname("u3"), session.StateComplete, start)
	saveJudgeSession(t, mgr, "dddd", jname("u4"), session.StateFailed, start)
	saveJudgeSession(t, mgr, "eeee", jname("u5"), session.StateKilled, start)
	// not a judge — must not be listed even though running
	saveJudgeSession(t, mgr, "ffff", "autonomous:verify:t1", session.StateRunning, start)
	saveJudgeSession(t, mgr, "gggg", "custom-name", session.StateRunning, start)

	host := &judgeReconcileHost{mgr: mgr}
	live := host.LiveJudges()
	if len(live) != 2 {
		t.Fatalf("LiveJudges = %+v, want exactly the two unfinished judge sessions", live)
	}
	byUnit := map[string]autonomous.JudgeSession{}
	for _, js := range live {
		byUnit[js.UnitID] = js
	}
	for unit, wantID := range map[string]string{"u1": "h-aaaa", "u2": "h-bbbb"} {
		js, ok := byUnit[unit]
		if !ok {
			t.Fatalf("missing unit %q in %+v", unit, live)
		}
		if js.ID != wantID {
			t.Errorf("unit %q id = %q, want %q", unit, js.ID, wantID)
		}
		if !js.Started.Equal(start) && !js.Started.Equal(start.Add(10*time.Second)) {
			t.Errorf("unit %q started = %v", unit, js.Started)
		}
	}
}

// End kills the session through the manager; after it, the session is no
// longer live for reconcile. End of an unknown id is an error from the
// manager, propagated.
func TestJudgeReconcileHost_End_KillsSessionAndUnlistsIt(t *testing.T) {
	mgr := newSessionTestManager(t)
	host := &judgeReconcileHost{mgr: mgr, reg: newVerifierRegistry()}
	jname := func(unit string) string { return judgeSessionNamePrefix + ":" + unit }
	saveJudgeSession(t, mgr, "aaaa", jname("u1"), session.StateRunning, time.Now())

	if got := host.LiveJudges(); len(got) != 1 || got[0].ID != "h-aaaa" {
		t.Fatalf("LiveJudges before End = %+v", got)
	}
	if err := host.End("h-aaaa", "unit finished"); err != nil {
		t.Fatalf("End: %v", err)
	}
	if got := host.LiveJudges(); len(got) != 0 {
		t.Fatalf("LiveJudges after End = %+v, want none", got)
	}
	sess, ok := mgr.GetSession("h-aaaa")
	if !ok {
		t.Fatal("session missing after End")
	}
	if sess.State != session.StateKilled {
		t.Errorf("state after End = %s, want killed", sess.State)
	}

	if err := host.End("h-zzzz", "unknown"); err == nil {
		t.Error("End of an unknown session id should be an error")
	}
}

// BindLease records the judge session in BOTH the judges registry (the
// reaper's holderAlive view) and the capacity ledger (the lease's bound
// session), so the pre-existing lease now follows the session's liveness
// exactly like a verifier lease does.
func TestJudgeReconcileHost_BindLease_BindsRegistryAndLedger(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 2)
	holder := autonomous.JudgeHolder("u7")
	led.Adopt(reapReq(holder), "")

	host := &judgeReconcileHost{mgr: newSessionTestManager(t), led: led, reg: newVerifierRegistry()}
	host.BindLease(holder, "h-jj01")

	if sid := host.reg.sessionOf(holder); sid != "h-jj01" {
		t.Fatalf("registry sessionOf(%s) = %q, want the bound id", holder, sid)
	}
	if ls := led.LeaseOf(holder); ls == nil || ls.SessionID != "h-jj01" {
		t.Fatalf("ledger lease = %+v, want the bound session id", ls)
	}

	// The bound-session rule now applies: a live session keeps the lease
	// past spawn-grace, and a dead session lets the leak be reaped even
	// while the holder is still registered as active — the verifier rule.
	now := time.Now().Add(30 * time.Minute)
	beginEnd := host.reg.begin(holder)
	defer beginEnd()
	if got := reapPass(led, host.reg, map[string]bool{"h-jj01": true}, now); len(got) != 0 {
		t.Fatalf("reaped %v with a live bound judge session", got)
	}
	if got := reapPass(led, host.reg, map[string]bool{"h-jj01": false}, now); len(got) != 1 {
		t.Fatalf("reaped %v, want the lease of the dead judge session (registration must not keep it alive)", got)
	}
	if led.Held(holder) {
		t.Fatal("lease of the dead judge session must not be held")
	}
}
