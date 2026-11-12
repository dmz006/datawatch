package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// The post-turn wait's log lines name the session and the PRD/task, so an
// operator can follow one worker's nudge, stall and retry from the log alone.
// These tests drive the same fakes as post_turn_nudge_test.go and read the
// standard logger.

// newLogTestNudge builds a post-turn nudge over the fake session with the
// session idle limit set, as the verify wait does.
func newLogTestNudge(f *fakeClaudeSession, idleLimit time.Duration, task *autonomouspkg.Task) *postTurnNudge {
	deps := f.deps()
	deps.idleLimit = idleLimit
	return newPostTurnNudge(deps, autonomouspkg.Config{}, task)
}

func TestPostTurnNudgeLog_WaitStartNamesTheSessionAndWindows(t *testing.T) {
	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "claude-code")
	newLogTestNudge(f, 15*time.Minute, postTurnTaskFor(autonomouspkg.TaskInProgress))

	want := fmt.Sprintf("post-turn wait start: session %s task %s/%s: backend family claude-code, nudge applies=yes, nudge window 3m0s, stall window 10m0s, session idle limit 15m0s, persisted nudge=false",
		postTurnSessionID, postTurnPRD, postTurnTask)
	if !strings.Contains(logBuf.String(), want) {
		t.Fatalf("wait-start line missing %q; log:\n%s", want, logBuf.String())
	}
}

func TestPostTurnNudgeLog_WaitStartReportsAPersistedNudge(t *testing.T) {
	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "claude-code")
	task := postTurnTaskFor(autonomouspkg.TaskInProgress)
	task.NudgeEvents = []autonomouspkg.NudgeEvent{
		{At: postTurnT0.Add(-time.Minute), Count: 1, SessionID: postTurnSessionID, Outcome: autonomouspkg.NudgeOutcomeSent},
	}
	newLogTestNudge(f, 15*time.Minute, task)

	if !strings.Contains(logBuf.String(), "persisted nudge=true") {
		t.Fatalf("wait-start line does not report the persisted nudge; log:\n%s", logBuf.String())
	}
}

func TestPostTurnNudgeLog_FamilyUnknownLoggedOnceThenResolved(t *testing.T) {
	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "")
	nudge := newLogTestNudge(f, 15*time.Minute, postTurnTaskFor(autonomouspkg.TaskInProgress))

	if err := runPostTurnWait(clock, f, nudge, 1, nil); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	f.snap.BackendFamily = "claude-code"
	if err := runPostTurnWait(clock, f, nudge, 3, nil); err != nil {
		t.Fatalf("after the family resolved: %v", err)
	}

	log := logBuf.String()
	unknown := fmt.Sprintf("post-turn wait: session %s task %s/%s: backend family still unknown after the first tick",
		postTurnSessionID, postTurnPRD, postTurnTask)
	resolved := fmt.Sprintf("post-turn wait: session %s task %s/%s: backend family resolved to claude-code; nudge applies=yes, tracker seeded=false",
		postTurnSessionID, postTurnPRD, postTurnTask)
	if n := strings.Count(log, unknown); n != 1 {
		t.Errorf("unknown-family line logged %d times, want once; log:\n%s", n, log)
	}
	if n := strings.Count(log, resolved); n != 1 {
		t.Errorf("resolved-family line logged %d times, want once; log:\n%s", n, log)
	}
}

func TestPostTurnNudgeLog_WarnsOnceWhenNudgeWindowIsNotShorterThanIdleLimit(t *testing.T) {
	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "claude-code")
	newLogTestNudge(f, 3*time.Minute, postTurnTaskFor(autonomouspkg.TaskInProgress))

	want := fmt.Sprintf("post-turn wait warning: session %s task %s/%s: nudge window 3m0s is not smaller than session idle limit 3m0s",
		postTurnSessionID, postTurnPRD, postTurnTask)
	if n := strings.Count(logBuf.String(), want); n != 1 {
		t.Fatalf("warning logged %d times, want once; log:\n%s", n, logBuf.String())
	}
}

func TestPostTurnNudgeLog_NoWarningWhenNudgeWindowIsShorter(t *testing.T) {
	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "claude-code")
	newLogTestNudge(f, 15*time.Minute, postTurnTaskFor(autonomouspkg.TaskInProgress))

	if strings.Contains(logBuf.String(), "post-turn wait warning") {
		t.Fatalf("warning logged though the nudge fires first; log:\n%s", logBuf.String())
	}
}

func TestPostTurnNudgeLog_AnsweredNudgeNamesTheSessionAndTask(t *testing.T) {
	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "claude-code")
	nudge := newLogTestNudge(f, 15*time.Minute, postTurnTaskFor(autonomouspkg.TaskInProgress))

	answered := false
	onTick := func() {
		// The worker prints fresh output one tick after the nudge lands.
		if len(f.sent) == 1 && !answered {
			f.logMTime = clock.now.Add(-time.Second)
			answered = true
		}
	}
	if err := runPostTurnWait(clock, f, nudge, postTurnTicks, onTick); err != nil {
		t.Fatalf("unexpected stall: %v", err)
	}

	want := fmt.Sprintf("idle worker %s task %s/%s: nudge answered by new output", postTurnSessionID, postTurnPRD, postTurnTask)
	if !strings.Contains(logBuf.String(), want) {
		t.Fatalf("answered line missing %q; log:\n%s", want, logBuf.String())
	}
	if len(f.recorder.outcomes) != 1 || f.recorder.outcomes[0] != autonomouspkg.NudgeOutcomeAnswered {
		t.Fatalf("recorded outcomes = %v, want [answered]", f.recorder.outcomes)
	}
}

func TestPostTurnNudgeLog_StallNamesTheSessionTaskAndNudgeStall(t *testing.T) {
	logBuf := capturePostTurnLog(t)
	clock := &fakeClock{now: postTurnT0}
	f := newFakeClaudeSession(clock, "claude-code")
	nudge := newLogTestNudge(f, 15*time.Minute, postTurnTaskFor(autonomouspkg.TaskInProgress))

	// Silent throughout: nudged at 180s, stalled 600s later.
	err := runPostTurnWait(clock, f, nudge, 300, nil)
	if !errors.Is(err, autonomouspkg.ErrWorkerStalled) {
		t.Fatalf("wait error = %v, want a stall", err)
	}
	var stall autonomouspkg.NudgeStallError
	if !errors.As(err, &stall) || stall.SessionID != postTurnSessionID {
		t.Fatalf("wait error = %v, want a NudgeStallError for session %s", err, postTurnSessionID)
	}
	want := fmt.Sprintf("idle worker %s task %s/%s: still silent for ", postTurnSessionID, postTurnPRD, postTurnTask)
	if !strings.Contains(logBuf.String(), want) {
		t.Fatalf("stall line missing %q; log:\n%s", want, logBuf.String())
	}
	if autonomouspkg.StallRetryReason(err) != "after completion nudge" {
		t.Fatalf("retry reason = %q, want %q", autonomouspkg.StallRetryReason(err), "after completion nudge")
	}
}
