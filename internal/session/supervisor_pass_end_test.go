package session

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A scheduled supervisor pass is a single turn. Claude Code's Stop hook says the
// turn is over; the one-shot reaper ends the session once the hook has fired after
// the task was delivered and nothing has happened for a grace period. It does not
// depend on the model printing a completion marker.

func startSupervisorPass(t *testing.T) (*Manager, *Session) {
	t.Helper()
	isolateMultiplexerEnv(t)
	mgr, fake := newPromptFamilyTestManager(t)
	started, err := mgr.Start(context.Background(), "supervisor pass", "", t.TempDir(), &StartOptions{
		Backend:  "pi-prompt",
		LaunchFn: stubPromptLaunch(t, fake),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	sess, ok := mgr.GetSession(started.FullID)
	if !ok || !sess.OneShot {
		t.Fatalf("precondition: a one-shot session, got ok=%v", ok)
	}
	sess.LaunchRole = launchRoleSupervisor
	delivered := time.Now().Add(-5 * time.Minute)
	sess.TaskDeliveredAt = &delivered
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	return mgr, sess
}

func setStopAge(mgr *Manager, full string, age time.Duration) {
	mgr.mu.Lock()
	mgr.hookStopAt[full] = time.Now().Add(-age)
	mgr.mu.Unlock()
}

func TestSupervisorPassEndsAfterTheStopHookAndTheGracePeriod(t *testing.T) {
	mgr, sess := startSupervisorPass(t)
	mgr.NoteHookBoardEvent(sess.FullID, "Stop")
	if got := mgr.FinishedOneShots(); len(got) != 0 {
		t.Fatalf("a pass whose Stop hook fired just now was listed as finished: %+v", got)
	}
	setStopAge(mgr, sess.FullID, time.Minute)
	got := mgr.FinishedOneShots()
	if len(got) != 1 || got[0].FullID != sess.FullID {
		t.Fatalf("FinishedOneShots = %+v, want exactly %s", got, sess.FullID)
	}
	reaped := mgr.ReapFinishedOneShots()
	if len(reaped) != 1 || reaped[0] != sess.FullID {
		t.Fatalf("ReapFinishedOneShots = %v, want [%s]", reaped, sess.FullID)
	}
	if cur, _ := mgr.GetSession(sess.FullID); cur.State != StateComplete {
		t.Fatalf("state = %s, want complete", cur.State)
	}
	if again := mgr.ReapFinishedOneShots(); len(again) != 0 {
		t.Fatalf("a second reap ended %v again", again)
	}
}

func TestNewActivityAfterTheStopHookKeepsThePassAlive(t *testing.T) {
	for _, ev := range []string{"UserPromptSubmit", "PreToolUse", "PostToolUse"} {
		mgr, sess := startSupervisorPass(t)
		mgr.NoteHookBoardEvent(sess.FullID, "Stop")
		setStopAge(mgr, sess.FullID, time.Minute)
		mgr.NoteHookBoardEvent(sess.FullID, ev)
		if got := mgr.FinishedOneShots(); len(got) != 0 {
			t.Errorf("after Stop then %s the pass was listed as finished: %+v", ev, got)
		}
	}
}

func TestASubagentStopIsNotTheEndOfThePass(t *testing.T) {
	mgr, sess := startSupervisorPass(t)
	mgr.NoteHookBoardEvent(sess.FullID, "SubagentStop")
	mgr.mu.Lock()
	_, recorded := mgr.hookStopAt[sess.FullID]
	mgr.mu.Unlock()
	if recorded {
		t.Fatal("a sub-agent stop was recorded as the end of the pass")
	}
}

func TestAStopBeforeTheTaskArrivedDoesNotEndThePass(t *testing.T) {
	mgr, sess := startSupervisorPass(t)
	mgr.NoteHookBoardEvent(sess.FullID, "Stop")
	setStopAge(mgr, sess.FullID, time.Minute)
	later := time.Now() // the task arrived after that stop
	cur, _ := mgr.GetSession(sess.FullID)
	cur.TaskDeliveredAt = &later
	if err := mgr.SaveSession(cur); err != nil {
		t.Fatal(err)
	}
	if got := mgr.FinishedOneShots(); len(got) != 0 {
		t.Fatalf("a stop from before the task was delivered ended the pass: %+v", got)
	}
}

func TestOnlySupervisorPassesEndOnTheStopHook(t *testing.T) {
	mgr, sess := startSupervisorPass(t)
	cur, _ := mgr.GetSession(sess.FullID)
	cur.LaunchRole = "" // an ordinary one-shot worker, not a supervisor pass
	if err := mgr.SaveSession(cur); err != nil {
		t.Fatal(err)
	}
	mgr.NoteHookBoardEvent(sess.FullID, "Stop")
	setStopAge(mgr, sess.FullID, time.Hour)
	if got := mgr.FinishedOneShots(); len(got) != 0 {
		t.Fatalf("a non-supervisor one-shot was ended by the Stop hook: %+v", got)
	}
}

func TestAPassThatRunsPastTheBackstopAgeIsEnded(t *testing.T) {
	mgr, sess := startSupervisorPass(t)
	cur, _ := mgr.GetSession(sess.FullID)
	cur.CreatedAt = time.Now().Add(-supervisorPassMaxAge - time.Minute)
	if err := mgr.SaveSession(cur); err != nil {
		t.Fatal(err)
	}
	got := mgr.FinishedOneShots()
	if len(got) != 1 || got[0].FullID != sess.FullID {
		t.Fatalf("FinishedOneShots = %+v, want the overdue pass %s", got, sess.FullID)
	}
	if reason := mgr.oneShotFinishReason(got[0]); !strings.Contains(reason, "without ending") {
		t.Errorf("reason = %q, want the backstop reason", reason)
	}
}
