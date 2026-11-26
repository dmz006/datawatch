// One proven case ends a claude-code session as a failed launch: datawatch
// declined a start-up dialog, the agent left, nothing but the shell remains, no
// task was delivered and nothing was sent through datawatch. Every other case
// behaves as it did without the rule.

package session

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const settingsErrorPane = `────────────────────────────────────────────────────────────────────────
 Settings Error

 /made-up/project/.claude/settings.json
  └ Invalid or malformed JSON (line 2, column 3)

 /made-up/very/long/folder/names/that/make/the/terminal/wrap/this/pa
th/project/.claude/settings.local.json
  └ Invalid or malformed JSON (line 2, column 3)

 Files with errors are skipped entirely, not just the invalid settings.

 ❯ 1. Fix with Claude
   2. Exit and fix manually
   3. Continue without these settings

 Enter to confirm · Esc to cancel`

const exitedPane = settingsErrorPane + `
DATAWATCH_COMPLETE: claude done
user@host:~/made-up/project$`

type stateLog struct {
	mu   sync.Mutex
	seen []State
}

func (l *stateLog) add(s *Session, _ State) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, s.State)
}

func (l *stateLog) list() []State {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]State(nil), l.seen...)
}

func (l *stateLog) count(st State) int {
	n := 0
	for _, s := range l.list() {
		if s == st {
			n++
		}
	}
	return n
}

// launch is a fresh channel session on a made-up terminal.
type launch struct {
	mgr   *Manager
	fake  *FakeTmux
	sess  *Session
	seen  *stateLog
	mu    sync.Mutex
	procs []paneProc // nil: the process table cannot be read
}

func newLaunch(t *testing.T, mutate func(*Session)) *launch {
	t.Helper()
	mgr, fake, sess := guardFixture(t, liveBox, paneAgent)
	l := &launch{mgr: mgr, fake: fake, sess: sess, seen: &stateLog{}, procs: paneAgent}
	mgr.paneProcsFn = func(string) (int, string, []paneProc, bool) {
		l.mu.Lock()
		defer l.mu.Unlock()
		return guardShellPID, guardTTY, l.procs, l.procs != nil
	}
	mgr.store.Mutate(func() {
		sess.State = StateRunning
		sess.Task = "Reply with the single word ready."
		if mutate != nil {
			mutate(sess)
		}
	})
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	mgr.SetStateChangeHandler(l.seen.add)
	return l
}

func (l *launch) screen(pane string, procs []paneProc) {
	l.fake.mu.Lock()
	l.fake.Pane[l.sess.TmuxSession] = pane
	l.fake.mu.Unlock()
	l.mu.Lock()
	l.procs = procs
	l.mu.Unlock()
}

// decline puts a dialog on screen and has datawatch decline it.
func (l *launch) decline(t *testing.T, dialog string) {
	t.Helper()
	l.screen(dialog, paneAgent)
	if err := l.mgr.DeclineStartupDialog(l.sess.FullID); err != nil {
		t.Fatal(err)
	}
	if got := l.fake.LastCall("send-keys-literal").Args; len(got) != 1 || got[0] != "\x1b" {
		t.Fatalf("the decline must stay a single Esc, sent %q", got)
	}
}

func (l *launch) snap() Session {
	s, _ := l.mgr.GetSession(l.sess.FullID)
	return l.mgr.store.Snapshot(s)
}

// watch runs the screen watcher for d, or until the pane is closed. It waits on
// the fake terminal: older watcher paths write session fields without the
// store's lock.
func (l *launch) watch(d time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	l.mgr.StartScreenCapture(ctx, l.sess.FullID, 20)
	for end := time.Now().Add(d); time.Now().Before(end) && l.fake.SessionExists(l.sess.TmuxSession); {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	l.mgr.bg.Wait()
}

func (l *launch) monitor(d time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	l.mgr.goBG(func() { l.mgr.monitorOutput(ctx, l.sess, NewProjectGit(l.sess.ProjectDir)) })
	time.Sleep(d)
	cancel()
	l.mgr.WaitBackground()
}

func (l *launch) reconcile() {
	ctx, cancel := context.WithCancel(context.Background())
	l.mgr.reconcileSessions(ctx)
	cancel()
	l.mgr.WaitBackground()
}

func (l *launch) logLine(line string) {
	var last, match time.Time
	var pending []string
	l.mgr.processOutputLine(context.Background(), l.sess, NewProjectGit(l.sess.ProjectDir), line+"\n",
		&last, &pending, &match, func() *Tracker { return nil })
}

func (l *launch) assertFailedOnce(t *testing.T) {
	t.Helper()
	got := l.snap()
	if got.State != StateFailed {
		t.Fatalf("state = %s (%q), want failed; transitions %v", got.State, got.EndNote, l.seen.list())
	}
	for _, want := range []string{"Settings Error", "/made-up/project/.claude/settings.json",
		"/made-up/very/long/folder/names/that/make/the/terminal/wrap/this/path/project/.claude/settings.local.json"} {
		if !strings.Contains(got.EndNote, want) {
			t.Errorf("end note %q does not name %q", got.EndNote, want)
		}
	}
	if l.seen.count(StateComplete) != 0 || l.seen.count(StateFailed) != 1 {
		t.Errorf("transitions %v, want failed once and never complete", l.seen.list())
	}
	if l.fake.SessionExists(got.TmuxSession) {
		t.Error("the leftover shell pane was not closed")
	}
	if l.mgr.TaskDelivered(got.FullID) || l.mgr.ClaimTaskDelivery(got.FullID) {
		t.Error("the task reads as delivered, or can still be claimed")
	}
}

// The live failure, through each watcher that would have completed it.
func TestTheProvenLaunchFailureEndsFailedOnce(t *testing.T) {
	watchers := map[string]func(*launch){
		"pane monitor":   func(l *launch) { l.monitor(2500 * time.Millisecond) },
		"screen watcher": func(l *launch) { l.watch(2 * time.Second) },
		"log line":       func(l *launch) { l.logLine("DATAWATCH_COMPLETE: claude done") },
		"reaper":         func(l *launch) { l.mgr.ReapFinishedOneShots() },
	}
	for name, run := range watchers {
		for kind, oneShot := range map[string]bool{"interactive": false, "one-shot": true} {
			if name == "reaper" && !oneShot {
				continue // it looks at one-shots only
			}
			t.Run(name+"/"+kind, func(t *testing.T) {
				l := newLaunch(t, func(s *Session) { s.OneShot = oneShot })
				l.decline(t, settingsErrorPane)
				l.screen(exitedPane, paneBareShell)
				run(l)
				l.reconcile()
				l.reconcile()
				if l.mgr.tryTransitionToWaiting(l.sess.FullID, "user@host$", "", nil, true) {
					t.Error("a prompt match moved it back to waiting_input")
				}
				l.assertFailedOnce(t)
			})
		}
	}
}

// A claim taken before the exit, with nothing typed, is still no delivery.
func TestAClaimWithNothingTypedIsStillTheProvenCase(t *testing.T) {
	l := newLaunch(t, nil)
	if !l.mgr.ClaimTaskDelivery(l.sess.FullID) {
		t.Fatal("claim")
	}
	l.decline(t, settingsErrorPane)
	l.screen(exitedPane, paneBareShell)
	var reasons []string
	l.mgr.SetOnTaskDeliveryFailed(func(_, r string) { reasons = append(reasons, r) })
	if err := l.mgr.DeliverOneShotTask(l.sess.FullID, l.sess.Task, 1, time.Second, time.Millisecond); err == nil {
		t.Fatal("delivery reported success")
	}
	if typedContains(l.fake, "Reply with the single word ready.") {
		t.Fatalf("typed: %q", typed(l.fake))
	}
	l.assertFailedOnce(t)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "Settings Error") ||
		!strings.Contains(l.mgr.TaskDeliveryFailure(l.sess.FullID), "Settings Error") {
		t.Errorf("delivery failure reasons = %q", reasons)
	}
}

// A closed terminal that tmux confirms, also after a daemon restart: the
// evidence is on the saved record.
func TestTheProvenCaseWithAClosedTerminal(t *testing.T) {
	ends := map[string]func(*launch){
		"reconciler": func(l *launch) { l.reconcile() },
		"finaliser":  func(l *launch) { l.mgr.FinaliseGoneSessions(time.Now()) },
		"boot":       func(l *launch) { l.mgr.ResumeMonitors(context.Background()) },
		"reconnect":  func(l *launch) { l.mgr.ReconnectBackends(func(*Session, *BackendState) {}) },
	}
	for name, run := range ends {
		t.Run(name, func(t *testing.T) {
			l := newLaunch(t, func(s *Session) { s.TrackingDir = s.ProjectDir })
			l.decline(t, settingsErrorPane)
			if err := os.WriteFile(l.sess.LogFile, []byte("DATAWATCH_COMPLETE: claude done\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := l.fake.KillSession(l.sess.TmuxSession); err != nil {
				t.Fatal(err)
			}
			run(l)
			l.assertFailedOnce(t)
		})
	}
}

func TestAFailedLaunchIsNeverRevivedAndARestartIsJudgedAfresh(t *testing.T) {
	l := newLaunch(t, nil)
	l.decline(t, settingsErrorPane)
	l.screen(exitedPane, paneBareShell)
	l.fake.mu.Lock()
	l.fake.FailNextOp = map[string]int{"kill-session": 1} // the pane outlives the end
	l.fake.mu.Unlock()
	l.watch(time.Second)
	_ = l.fake.NewSessionWithSize(l.sess.TmuxSession, 120, 40)
	ctx, cancel := context.WithCancel(context.Background())
	l.mgr.ResumeMonitors(ctx)
	cancel()
	l.mgr.WaitBackground()
	l.reconcile()
	l.assertFailedOnce(t)

	ctx, cancel = context.WithCancel(context.Background())
	if _, err := l.mgr.Restart(ctx, l.sess.FullID); err != nil {
		t.Fatal(err)
	}
	cancel()
	l.mgr.WaitBackground()
	if got := l.snap(); got.State != StateRunning || got.EndNote != "" {
		t.Fatalf("after restart: state %s note %q", got.State, got.EndNote)
	}
	l.seen = &stateLog{}
	l.mgr.SetStateChangeHandler(l.seen.add)
	l.decline(t, settingsErrorPane) // the file is unchanged
	l.screen(exitedPane, paneBareShell)
	l.watch(2 * time.Second)
	l.assertFailedOnce(t)
}
