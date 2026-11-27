// A one-shot shell script that exits with an error ends failed, once. Nothing
// else on the screen is read as that verdict.

package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The two lines the shell backend's typed command prints. See the shell
// package's tests, which run that command in a real shell.
const (
	shellOKOut     = "DATAWATCH_COMPLETE: shell done"
	shellFailedOut = "DATAWATCH_FAILED: shell script failed"
)

func newShellSession(t *testing.T, mgr *Manager, f *FakeTmux, id string, oneShot bool) (*Session, *atomic.Int32) {
	t.Helper()
	sess := &Session{
		ID: id, FullID: "testhost-" + id, Hostname: "testhost", TmuxSession: "cs-" + id,
		BackendFamily: "shell", OneShot: oneShot, State: StateRunning,
		Task: "rebuild the nightly index", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	f.mu.Lock()
	f.Pane[sess.TmuxSession] = "datawatch:~$ \n"
	f.mu.Unlock()
	var ends atomic.Int32
	mgr.SetOnSessionEnd(func(*Session) { ends.Add(1) })
	return sess, &ends
}

func setShellPane(f *FakeTmux, sess *Session, text string) {
	f.mu.Lock()
	f.Pane[sess.TmuxSession] = text
	f.mu.Unlock()
}

// logLine hands one output line to the log watcher.
func logLine(t *testing.T, mgr *Manager, sess *Session, line string) {
	t.Helper()
	var last, match time.Time
	var pending []string
	mgr.processOutputLine(context.Background(), sess, NewProjectGit(t.TempDir()), line+"\r\n", &last, &pending, &match, func() *Tracker { return nil })
}

func TestShellScriptVerdictOnScreen(t *testing.T) {
	for label, tc := range map[string]struct {
		line string
		want State
	}{"failing": {shellFailedOut, StateFailed}, "succeeding": {shellOKOut, StateComplete}} {
		t.Run(label, func(t *testing.T) {
			mgr, f := newTestManagerWithFake(t)
			sess, ends := newShellSession(t, mgr, f, "sh01", true)
			setShellPane(f, sess, "indexing\n"+tc.line+"\ndatawatch:~$ \n")
			stop := startCapture(t, mgr, sess)
			pollUntil(t, 10*time.Second, func() bool { return mgr.store.Snapshot(sess).State.IsTerminal() },
				"the verdict line did not end the session")
			stop()
			got := mgr.store.Snapshot(sess)
			if got.State != tc.want || ends.Load() != 1 || got.EndedAt == nil {
				t.Fatalf("state=%s ends=%d ended_at=%v, want %s once", got.State, ends.Load(), got.EndedAt, tc.want)
			}
		})
	}
}

func TestShellScriptVerdictInTheLog(t *testing.T) {
	for label, tc := range map[string]struct {
		line string
		want State
	}{"failing": {shellFailedOut, StateFailed}, "succeeding": {shellOKOut, StateComplete}} {
		t.Run(label, func(t *testing.T) {
			mgr, f := newTestManagerWithFake(t)
			sess, ends := newShellSession(t, mgr, f, "sh02", true)
			// Terminal control codes sit around real output in the log.
			logLine(t, mgr, sess, "\x1b[?2004l\r"+tc.line)
			logLine(t, mgr, sess, tc.line)
			if got := storeState(t, mgr, sess.FullID); got.State != tc.want || ends.Load() != 1 {
				t.Fatalf("state=%s ends=%d, want %s once", got.State, ends.Load(), tc.want)
			}
		})
	}
}

// More than one watcher can see the line at once: the log watcher, the pane
// watcher and the reaper all end a session through the same step. It ends once.
func TestShellFailureSeenByManyWatchersEndsOnce(t *testing.T) {
	for i := 0; i < 5; i++ {
		mgr, f := newTestManagerWithFake(t)
		sess, ends := newShellSession(t, mgr, f, "sh03", true)
		var changes atomic.Int32
		mgr.SetStateChangeHandler(func(*Session, State) { changes.Add(1) })
		setShellPane(f, sess, shellFailedOut+"\n")
		start := make(chan struct{})
		var wg sync.WaitGroup
		for j := 0; j < 12; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				switch j % 3 {
				case 0:
					mgr.failMissedShellScripts()
				case 1:
					mgr.failShellScript(sess.FullID)
				default:
					logLine(t, mgr, sess, shellFailedOut)
				}
			}()
		}
		close(start)
		wg.Wait()
		got := mgr.store.Snapshot(sess)
		if got.State != StateFailed || ends.Load() != 1 || changes.Load() != 1 {
			t.Fatalf("state=%s ends=%d state changes=%d, want failed once", got.State, ends.Load(), changes.Load())
		}
	}
}

// The end is on record with the reason, once, whoever sees the line next.
func TestShellFailureIsRecordedOnce(t *testing.T) {
	mgr, f := newTestManagerWithFake(t)
	sess, ends := newShellSession(t, mgr, f, "sh08", true)
	tracker, err := NewTracker(mgr.dataDir, sess)
	if err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	mgr.trackers[sess.FullID] = tracker
	mgr.mu.Unlock()
	setShellPane(f, sess, shellFailedOut+"\n")
	logLine(t, mgr, sess, shellFailedOut)
	logLine(t, mgr, sess, shellFailedOut)
	mgr.ReapFinishedOneShots()
	got := mgr.store.Snapshot(sess)
	if got.State != StateFailed || ends.Load() != 1 || !strings.Contains(got.EndNote, "script exited with an error") {
		t.Fatalf("state=%s ends=%d end note=%q", got.State, ends.Load(), got.EndNote)
	}
	timeline, _ := os.ReadFile(filepath.Join(mgr.dataDir, "sessions", sess.FullID, "timeline.md"))
	if n := strings.Count(string(timeline), "script exited with an error"); n != 1 {
		t.Errorf("%d end lines in timeline.md, want 1:\n%s", n, timeline)
	}
}

// An interactive session stays usable after a script, as it does after a
// completion line. (A terminal state would also be undone by the reconciler
// while the pane is open.)
func TestShellFailureDoesNotEndAnInteractiveSession(t *testing.T) {
	mgr, f := newTestManagerWithFake(t)
	sess, ends := newShellSession(t, mgr, f, "sh04", false)
	setShellPane(f, sess, shellFailedOut+"\ndatawatch:~$ \n")
	stop := startCapture(t, mgr, sess)
	scanTicks(t, sess, f, 3)
	stop()
	logLine(t, mgr, sess, shellFailedOut)
	if got := storeState(t, mgr, sess.FullID); got.State.IsTerminal() || ends.Load() != 0 {
		t.Fatalf("state=%s ends=%d", got.State, ends.Load())
	}
}

func TestNothingElseReadsAsAShellFailure(t *testing.T) {
	for label, tc := range map[string]struct{ backend, line string }{
		"another backend prints the line": {"claude-code", shellFailedOut},
		"pi prints the line":              {"pi", shellFailedOut},
		"a script's own words":            {"shell", "DATAWATCH_FAILED: disk is full"},
		"more text after it":              {"shell", shellFailedOut + "' || true"},
		"quoted in a sentence":            {"shell", "on error it prints " + shellFailedOut},
		"the typed command":               {"shell", "datawatch:~$ './job.sh' 'x' '/p' && echo 'DATAWATCH_COMPLETE'': shell done' || echo 'DATAWATCH_FAILED'': shell script failed'"},
	} {
		t.Run(label, func(t *testing.T) {
			mgr, f := newTestManagerWithFake(t)
			sess, ends := newShellSession(t, mgr, f, "sh05", true)
			mgr.store.Mutate(func() { sess.BackendFamily = tc.backend })
			setShellPane(f, sess, tc.line+"\n")
			stop := startCapture(t, mgr, sess)
			scanTicks(t, sess, f, 3)
			stop()
			logLine(t, mgr, sess, tc.line)
			if got := storeState(t, mgr, sess.FullID); got.State.IsTerminal() || ends.Load() != 0 {
				t.Fatalf("state=%s ends=%d", got.State, ends.Load())
			}
		})
	}
}

// Every wrapped fragment of the typed command, at every terminal width.
func TestWrappedShellCommandNeverReadsAsAVerdict(t *testing.T) {
	cmd := "datawatch:~$ '/opt/jobs/job.sh' 'rebuild' '/p' && echo 'DATAWATCH_COMPLETE'': shell done' || echo 'DATAWATCH_FAILED'': shell script failed'"
	for width := 10; width <= len(cmd); width++ {
		for at := 0; at < len(cmd); at += width {
			frag := strings.TrimSpace(cmd[at:min(at+width, len(cmd))])
			if isRealCompletionLine(frag, "DATAWATCH_COMPLETE:") || matchesCompletionPattern(frag, "DATAWATCH_COMPLETE:") || isShellScriptFailureLine(frag) {
				t.Fatalf("width %d: %q reads as a verdict", width, frag)
			}
		}
	}
}

// A script can print a completion line and then fail. The reaper must not
// complete such a session.
func TestReaperDoesNotCompleteAFailedShellScript(t *testing.T) {
	mgr, f := newTestManagerWithFake(t)
	sess, _ := newShellSession(t, mgr, f, "sh06", true)
	setShellPane(f, sess, "DATAWATCH_COMPLETE: step one\n"+shellFailedOut+"\ndatawatch:~$ \n")
	if got := mgr.FinishedOneShots(); len(got) != 0 {
		t.Fatalf("listed as finished: %s", got[0].FullID)
	}
	setShellPane(f, sess, shellOKOut+"\ndatawatch:~$ \n")
	if got := mgr.FinishedOneShots(); len(got) != 1 {
		t.Fatalf("a finished script is not listed: %v", got)
	}
}

// A script that failed while nothing watched its pane (the daemon was down, or
// only the log watcher was resumed) is ended by the one-shot reaper.
func TestReaperFailsAShellScriptNoWatcherSaw(t *testing.T) {
	mgr, f := newTestManagerWithFake(t)
	sess, ends := newShellSession(t, mgr, f, "sh07", true)
	setShellPane(f, sess, shellFailedOut+"\ndatawatch:~$ \n")
	for i := 0; i < 2; i++ {
		if reaped := mgr.ReapFinishedOneShots(); len(reaped) != 0 {
			t.Fatalf("completed %v", reaped)
		}
	}
	if got := storeState(t, mgr, sess.FullID); got.State != StateFailed || ends.Load() != 1 {
		t.Fatalf("state=%s ends=%d, want failed once", got.State, ends.Load())
	}
}
