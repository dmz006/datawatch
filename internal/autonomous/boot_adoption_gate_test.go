// Boot-resume adoption gate: a live session is adopted only once its agent has
// started.
//
//   - LIVE bare shell (agent never launched, a boot-time launch failure) → not
//     adopted: ended through the killer hook with a reason naming the launch
//     failure, the task reset to pending with SessionStartFailureReason, and
//     no retry counter moved.
//   - LIVE healthy session (agent started) → adopted exactly as before: kept
//     in_progress, lease re-taken, AdoptedSession set, never killed.
//   - LIVE session whose agent runs but has produced no first turn → adopted
//     here; the start-confirm bound handles it later.
//   - agent-started hook nil → the old behaviour: adopt on alive.
//
// Fakes come from adopt_live_session_test.go; the capacity fixture from
// capacity_test.go.
package autonomous

import (
	"strings"
	"testing"
)

// TestBootAdoptionGate_BareShellEndedAndReset: a live session whose agent never
// started is a boot-time launch failure. It is ended through the killer with a
// reason naming that failure, and its task goes back to pending carrying
// SessionStartFailureReason. RetryCount and StartFailures are left as they were,
// so no retry is counted.
func TestBootAdoptionGate_BareShellEndedAndReset(t *testing.T) {
	m, _, led := capFixture(t, 1)
	fs := newFakeSessions("sess-shell") // the pane is up, but only a bare shell runs in it
	fs.wire(m)
	var gotCaller, gotReason string
	m.SetSessionKillerFn(func(id, caller, reason string) error {
		gotCaller, gotReason = caller, reason
		return fs.killFn(id, caller, reason)
	})
	m.SetSessionAgentStartedFn(func(id string) bool { return id != "sess-shell" })

	cur := setupBootedAdoptState(t, m, "sess-shell")
	tk := &cur.Story[0].Tasks[0]
	tk.RetryCount = 2
	tk.StartFailures = 1
	if err := m.Store().SaveTask(tk); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	cur, _ = m.Store().GetPRD(cur.ID)

	m.resetInProgressTasksForResume(cur)

	got, _ := m.Store().GetPRD(cur.ID)
	tk2 := got.Story[0].Tasks[0]
	if tk2.Status != TaskPending {
		t.Fatalf("bare-shell task must reset to pending, got %s", tk2.Status)
	}
	if tk2.Error != SessionStartFailureReason {
		t.Fatalf("bare-shell task Error = %q, want %q", tk2.Error, SessionStartFailureReason)
	}
	if tk2.SessionID != "" {
		t.Fatalf("reset task must clear its session reference, got %q", tk2.SessionID)
	}
	if tk2.AdoptedSession {
		t.Fatal("bare-shell task must not carry AdoptedSession")
	}
	if killed := fs.killed(); len(killed) != 1 || killed[0] != "sess-shell" {
		t.Fatalf("kill calls = %v, want exactly [sess-shell]", killed)
	}
	if gotCaller != "autonomous.boot-resume.start-failed" {
		t.Fatalf("kill caller = %q, want autonomous.boot-resume.start-failed", gotCaller)
	}
	if !strings.Contains(gotReason, "boot-time launch failure") {
		t.Fatalf("kill reason must name the boot-time launch failure, got %q", gotReason)
	}
	if tk2.RetryCount != 2 {
		t.Fatalf("RetryCount moved on a boot-time launch failure: got %d, want 2", tk2.RetryCount)
	}
	if tk2.StartFailures != 1 {
		t.Fatalf("StartFailures moved on a boot-time launch failure: got %d, want 1", tk2.StartFailures)
	}
	if led.Held(tk2.ID) {
		t.Fatal("bare-shell task must not hold a capacity lease after reset")
	}
}

// TestBootAdoptionGate_HealthyAdopted: a live session whose agent has started
// is adopted exactly as before. It stays in_progress with its session, keeps
// its capacity lease and AdoptedSession flag, and is never killed.
func TestBootAdoptionGate_HealthyAdopted(t *testing.T) {
	m, _, led := capFixture(t, 1)
	fs := newFakeSessions("sess-ok")
	fs.wire(m)
	m.SetSessionAgentStartedFn(func(id string) bool { return id == "sess-ok" })

	cur := setupBootedAdoptState(t, m, "sess-ok")
	m.resetInProgressTasksForResume(cur)

	got, _ := m.Store().GetPRD(cur.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskInProgress {
		t.Fatalf("healthy adopted task must stay in_progress, got %s", tk.Status)
	}
	if tk.SessionID != "sess-ok" {
		t.Fatalf("healthy adopted task must keep its session, got %q", tk.SessionID)
	}
	if !tk.AdoptedSession {
		t.Fatal("healthy adopted task must carry AdoptedSession")
	}
	if tk.Error != "" {
		t.Fatalf("healthy adopted task must not carry an Error, got %q", tk.Error)
	}
	if n := fs.killCount(); n != 0 {
		t.Fatalf("healthy session must not be killed on adopt, killed %v", fs.killed())
	}
	if !led.Held(tk.ID) {
		t.Fatal("healthy adopted task holds no capacity lease after boot-resume")
	}
	if n := len(led.Snapshot().Leases); n != 1 {
		t.Fatalf("want exactly one lease after boot, got %d", n)
	}
}

// TestBootAdoptionGate_NoFirstTurnAdopted: the agent process runs in the pane
// but no assistant turn has appeared yet (the pane shows only the prompt). The
// gate counts the agent as started, so the session is adopted and kept for the
// start-confirm bound to judge, not killed here.
func TestBootAdoptionGate_NoFirstTurnAdopted(t *testing.T) {
	m, _, led := capFixture(t, 1)
	fs := newFakeSessions("sess-warm")
	fs.recordOutput("sess-warm", "❯ write docs/x.md\n") // prompt shown, no assistant turn yet
	fs.wire(m)
	asked := 0
	m.SetSessionAgentStartedFn(func(id string) bool {
		asked++
		return id == "sess-warm" // the agent process is running in the pane
	})

	cur := setupBootedAdoptState(t, m, "sess-warm")
	m.resetInProgressTasksForResume(cur)

	if asked == 0 {
		t.Fatal("the agent-started hook was never consulted for a live session")
	}
	got, _ := m.Store().GetPRD(cur.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskInProgress || tk.SessionID != "sess-warm" || !tk.AdoptedSession {
		t.Fatalf("agent running without a first turn must be adopted: status=%s session=%q adopted=%v", tk.Status, tk.SessionID, tk.AdoptedSession)
	}
	if n := fs.killCount(); n != 0 {
		t.Fatalf("session with agent running must not be killed at boot, killed %v", fs.killed())
	}
	if !led.Held(tk.ID) {
		t.Fatal("adopted task holds no capacity lease")
	}
}

// TestBootAdoptionGate_NilHookAdoptsOnAlive: with no agent-started hook wired,
// the gate is off and a live session is adopted exactly as before, even one
// that looks like a bare shell.
func TestBootAdoptionGate_NilHookAdoptsOnAlive(t *testing.T) {
	m, _, led := capFixture(t, 1)
	fs := newFakeSessions("sess-live")
	fs.wire(m)

	cur := setupBootedAdoptState(t, m, "sess-live")
	m.resetInProgressTasksForResume(cur)

	got, _ := m.Store().GetPRD(cur.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskInProgress || tk.SessionID != "sess-live" || !tk.AdoptedSession {
		t.Fatalf("nil agent-started hook must adopt on alive: status=%s session=%q adopted=%v", tk.Status, tk.SessionID, tk.AdoptedSession)
	}
	if n := fs.killCount(); n != 0 {
		t.Fatalf("live session killed with the gate off: %v", fs.killed())
	}
	if !led.Held(tk.ID) {
		t.Fatal("adopted task holds no capacity lease")
	}
}
