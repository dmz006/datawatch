// A claude-code session whose agent exits before its task was delivered ends
// failed, with the reason, and stays failed. It is never reported complete.

package session

import (
	"context"
	"os"
	"path/filepath"
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

const plainExitPane = `made-up agent: starting
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

// launchFixture is a live claude-code session on a fake terminal whose pane
// runs the agent until exitAgent is called.
func launchFixture(t *testing.T, mutate func(*Session)) (*Manager, *FakeTmux, *Session, *stateLog) {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	mgr.paneChildFn = func(string) bool { return false }
	dir := t.TempDir()
	logFile := filepath.Join(dir, "output.log")
	if err := os.WriteFile(logFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sess := &Session{
		ID: "lf01", FullID: "testhost-lf01", Hostname: "testhost",
		TmuxSession: "cs-testhost-lf01", BackendFamily: "claude-code",
		Task: "Reply with the single word ready.", ProjectDir: dir, LogFile: logFile,
		State: StateRunning, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		DaemonDeliversTask: true,
	}
	if mutate != nil {
		mutate(sess)
	}
	if err := fake.NewSessionWithSize(sess.TmuxSession, 120, 40); err != nil {
		t.Fatal(err)
	}
	fake.PaneCmd[sess.TmuxSession] = "claude"
	mgr.paneProcsFn = fakeProcs(fake)
	// A first start whose task this daemon is still to type.
	mgr.deliveryArmed = map[string]bool{sess.FullID: sess.DaemonDeliversTask}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	seen := &stateLog{}
	mgr.SetStateChangeHandler(seen.add)
	t.Cleanup(mgr.WaitBackground)
	return mgr, fake, sess, seen
}

const (
	fakeShellPID = 100
	fakeTTY      = "pts/99"
)

// fakeProcs is a pane's process table: its shell, and the pane's foreground
// command as the shell's child when that is not the shell itself.
func fakeProcs(fake *FakeTmux, extra ...paneProc) func(string) (int, string, []paneProc, bool) {
	return func(name string) (int, string, []paneProc, bool) {
		procs := []paneProc{{pid: fakeShellPID, ppid: 1, tty: fakeTTY, comm: "bash"}}
		if cmd := fake.PaneCommand(name); cmd != "" && cmd != "bash" {
			procs = append(procs, paneProc{pid: 200, ppid: fakeShellPID, tty: fakeTTY, comm: cmd})
		}
		return fakeShellPID, fakeTTY, append(procs, extra...), true
	}
}

// watchScreen runs the screen watcher until the session ends or d passes.
func watchScreen(mgr *Manager, fullID string, d time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	mgr.StartScreenCapture(ctx, fullID, 20)
	awaitState(mgr, fullID, StateFailed, d)
	cancel()
	mgr.bg.Wait()
}

func setLaunchPane(fake *FakeTmux, name, pane, cmd string) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.Pane[name] = pane
	fake.PaneCmd[name] = cmd
}

func declineAndExit(t *testing.T, mgr *Manager, fake *FakeTmux, sess *Session) {
	t.Helper()
	setLaunchPane(fake, sess.TmuxSession, settingsErrorPane, "claude")
	if err := mgr.DeclineStartupDialog(sess.FullID); err != nil {
		t.Fatal(err)
	}
	if got := fake.LastCall("send-keys-literal").Args; len(got) != 1 || got[0] != "\x1b" {
		t.Fatalf("the decline must stay a single Esc, sent %q", got)
	}
	setLaunchPane(fake, sess.TmuxSession, exitedPane, "bash")
}

func feedLine(mgr *Manager, sess *Session, line string) {
	var last, match time.Time
	var pending []string
	mgr.processOutputLine(context.Background(), sess, NewProjectGit(sess.ProjectDir), line+"\n",
		&last, &pending, &match, func() *Tracker { return nil })
}

func awaitState(mgr *Manager, fullID string, want State, within time.Duration) State {
	deadline := time.Now().Add(within)
	var got State
	for time.Now().Before(deadline) {
		if s, ok := mgr.GetSession(fullID); ok {
			if got = mgr.store.Snapshot(s).State; got == want {
				return got
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return got
}

func assertFailedOnce(t *testing.T, mgr *Manager, fake *FakeTmux, fullID string, seen *stateLog, wantInNote ...string) {
	t.Helper()
	s, ok := mgr.GetSession(fullID)
	if !ok {
		t.Fatal("session gone")
	}
	snap := mgr.store.Snapshot(s)
	if snap.State != StateFailed {
		t.Fatalf("state = %s (note %q), want failed; transitions %v", snap.State, snap.EndNote, seen.list())
	}
	for _, w := range wantInNote {
		if !strings.Contains(snap.EndNote, w) {
			t.Errorf("end note %q does not name %q", snap.EndNote, w)
		}
	}
	ends := 0
	for _, st := range seen.list() {
		if st == StateComplete {
			t.Errorf("the session was reported complete: %v", seen.list())
		}
		if st.IsTerminal() {
			ends++
		}
	}
	if ends != 1 {
		t.Errorf("terminal transitions = %d (%v), want exactly 1", ends, seen.list())
	}
	if fake.SessionExists(snap.TmuxSession) {
		t.Error("the leftover shell pane was not closed")
	}
	if mgr.TaskDelivered(fullID) {
		t.Error("the task reads as delivered")
	}
}

// The live failure: an interactive session, the log watcher and the reconciler.
func TestDeclinedSettingsDialogEndsFailedAndStaysFailed(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, nil)
	declineAndExit(t, mgr, fake, sess)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.goBG(func() { mgr.monitorOutput(ctx, sess, NewProjectGit(sess.ProjectDir)) })
	awaitState(mgr, sess.FullID, StateFailed, 6*time.Second)
	mgr.probeClaudeAliveFunc = func(*Session) bool { return false }
	mgr.reconcileSessions(ctx)
	mgr.reconcileSessions(ctx)
	time.Sleep(700 * time.Millisecond)

	assertFailedOnce(t, mgr, fake, sess.FullID, seen, "Settings Error",
		"/made-up/project/.claude/settings.json",
		"/made-up/very/long/folder/names/that/make/the/terminal/wrap/this/path/project/.claude/settings.local.json",
		"before the task was delivered")
}

// A task on the command line has no delivery step: the declined dialog that
// ends the agent is the evidence.
func TestDeclinedDialogFailsACommandLineOneShot(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, func(s *Session) { s.DaemonDeliversTask = false; s.OneShot = true })
	declineAndExit(t, mgr, fake, sess)
	feedLine(mgr, sess, "DATAWATCH_COMPLETE: claude done")
	watchScreen(mgr, sess.FullID, 3*time.Second)
	assertFailedOnce(t, mgr, fake, sess.FullID, seen, "Settings Error", "settings.json")
}

// A session with no task at all still did not launch.
func TestDeclinedDialogFailsASessionWithoutATask(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, func(s *Session) { s.DaemonDeliversTask = false; s.Task = "" })
	declineAndExit(t, mgr, fake, sess)
	feedLine(mgr, sess, "DATAWATCH_COMPLETE: claude done")
	watchScreen(mgr, sess.FullID, 3*time.Second)
	assertFailedOnce(t, mgr, fake, sess.FullID, seen, "Settings Error")
}

func TestAgentExitDuringDeliveryIsFailedByTheScreenWatcher(t *testing.T) {
	for name, oneShot := range map[string]bool{"interactive": false, "one-shot": true} {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess, seen := launchFixture(t, func(s *Session) { s.OneShot = oneShot })
			if !mgr.ClaimTaskDelivery(sess.FullID) {
				t.Fatal("claim")
			}
			setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			mgr.StartScreenCapture(ctx, sess.FullID, 20)
			awaitState(mgr, sess.FullID, StateFailed, 3*time.Second)
			time.Sleep(200 * time.Millisecond)
			assertFailedOnce(t, mgr, fake, sess.FullID, seen, "before its task was delivered")
			if mgr.ClaimTaskDelivery(sess.FullID) {
				t.Error("a late channel ready could still claim the task of a failed launch")
			}
			if mgr.tryTransitionToWaiting(sess.FullID, "user@host$", "", nil, true) {
				t.Error("a prompt match moved a failed launch back to waiting_input")
			}
		})
	}
}

func TestReaperDoesNotCompleteAnUndeliveredOneShot(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, func(s *Session) { s.OneShot = true })
	setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
	if got := mgr.ReapFinishedOneShots(); len(got) != 0 {
		t.Errorf("reaped as complete: %v", got)
	}
	assertFailedOnce(t, mgr, fake, sess.FullID, seen, "before its task was delivered")
}

func TestTerminalGoneBeforeDeliveryIsFailed(t *testing.T) {
	ends := map[string]func(*Manager){
		"reconciler": func(m *Manager) { m.reconcileSessions(context.Background()) },
		"finaliser":  func(m *Manager) { m.FinaliseGoneSessions(time.Now()) },
	}
	for name, run := range ends {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess, seen := launchFixture(t, func(s *Session) {
				s.TrackingDir = s.ProjectDir
			})
			if err := os.WriteFile(sess.LogFile, []byte("starting\nDATAWATCH_COMPLETE: claude done\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := fake.KillSession(sess.TmuxSession); err != nil {
				t.Fatal(err)
			}
			run(mgr)
			assertFailedOnce(t, mgr, fake, sess.FullID, seen, "before its task was delivered")
		})
	}
}

// After a daemon restart only a recorded declined dialog is evidence.
func TestADeclinedDialogStillCountsAfterADaemonRestart(t *testing.T) {
	ends := map[string]func(*Manager){
		"boot":      func(m *Manager) { m.ResumeMonitors(context.Background()) },
		"reconnect": func(m *Manager) { m.ReconnectBackends(func(*Session, *BackendState) {}) },
	}
	for name, run := range ends {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess, seen := launchFixture(t, func(s *Session) { s.TrackingDir = s.ProjectDir })
			declineAndExit(t, mgr, fake, sess)
			mgr.deliveryArmed = nil // a new daemon process
			if err := fake.KillSession(sess.TmuxSession); err != nil {
				t.Fatal(err)
			}
			run(mgr)
			assertFailedOnce(t, mgr, fake, sess.FullID, seen, "Settings Error")
		})
	}
}

// If closing the pane fails, neither the reconciler nor a restart of the daemon
// may bring a failed launch back.
func TestAFailedLaunchWithALivePaneIsNeverRevived(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, nil)
	declineAndExit(t, mgr, fake, sess)
	fake.mu.Lock()
	fake.FailNextOp = map[string]int{"kill-session": 1}
	fake.mu.Unlock()
	watchScreen(mgr, sess.FullID, 3*time.Second)
	if got := awaitState(mgr, sess.FullID, StateFailed, time.Second); got != StateFailed {
		t.Fatalf("state = %s", got)
	}
	// The pane outlived the end.
	_ = fake.NewSessionWithSize(sess.TmuxSession, 120, 40)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.ResumeMonitors(ctx)
	mgr.reconcileSessions(ctx)
	time.Sleep(700 * time.Millisecond)
	assertFailedOnce(t, mgr, fake, sess.FullID, seen, "Settings Error")
}

func TestARestartFollowsTheSameRule(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, nil)
	declineAndExit(t, mgr, fake, sess)
	watchScreen(mgr, sess.FullID, 3*time.Second)
	assertFailedOnce(t, mgr, fake, sess.FullID, seen, "Settings Error")

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := mgr.Restart(ctx, sess.FullID); err != nil {
		t.Fatal(err)
	}
	cancel()
	mgr.WaitBackground()
	s, _ := mgr.GetSession(sess.FullID)
	if got := mgr.store.Snapshot(s); got.State != StateRunning || got.EndNote != "" || got.LaunchFailure != "" || got.StartupDecline != nil {
		t.Fatalf("after restart: %+v", got)
	}
	// The file is unchanged, so the relaunch meets the same dialog.
	again := &stateLog{}
	mgr.SetStateChangeHandler(again.add)
	declineAndExit(t, mgr, fake, sess)
	watchScreen(mgr, sess.FullID, 3*time.Second)
	assertFailedOnce(t, mgr, fake, sess.FullID, again, "Settings Error")
}

// The delivery loop sees the launch fail: it must not record the task as
// delivered, and the reason stays the launch's reason.
func TestDeliveryNeverMarksAFailedLaunchDelivered(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, nil)
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("claim")
	}
	declineAndExit(t, mgr, fake, sess)
	var failed []string
	mgr.SetOnTaskDeliveryFailed(func(_, reason string) { failed = append(failed, reason) })
	err := mgr.DeliverOneShotTask(sess.FullID, sess.Task, 1, time.Second, time.Millisecond)
	if err == nil {
		t.Fatal("delivery reported success")
	}
	assertFailedOnce(t, mgr, fake, sess.FullID, seen, "Settings Error")
	if got := mgr.TaskDeliveryFailure(sess.FullID); !strings.Contains(got, "Settings Error") {
		t.Errorf("delivery failure reason = %q", got)
	}
	if len(failed) != 1 || !strings.Contains(failed[0], "Settings Error") {
		t.Errorf("delivery failure callbacks = %q, want one naming the dialog", failed)
	}
}

// The marker on screen while the agent still runs is neither a completion nor
// an exit.
func TestExitLineWithTheAgentStillRunningChangesNothing(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, func(s *Session) { s.OneShot = true })
	setLaunchPane(fake, sess.TmuxSession, plainExitPane, "claude")
	feedLine(mgr, sess, "DATAWATCH_COMPLETE: claude done")
	if got := mgr.ReapFinishedOneShots(); len(got) != 0 {
		t.Errorf("reaped: %v", got)
	}
	s, _ := mgr.GetSession(sess.FullID)
	if got := mgr.store.Snapshot(s).State; got != StateRunning {
		t.Fatalf("state = %s, transitions %v", got, seen.list())
	}
}

// Controls: a delivered task, and a declined dialog the agent got past, end as
// before.
func TestADeliveredTaskStillCompletes(t *testing.T) {
	mgr, fake, sess, _ := launchFixture(t, func(s *Session) { s.OneShot = true })
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("claim")
	}
	mgr.MarkTaskDelivered(sess.FullID)
	mgr.mu.Lock()
	old := time.Now().Add(-time.Hour)
	mgr.taskDeliveredAt[sess.FullID] = old
	mgr.mu.Unlock()
	setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
	feedLine(mgr, sess, "DATAWATCH_COMPLETE: claude done")
	if got := awaitState(mgr, sess.FullID, StateComplete, time.Second); got != StateComplete {
		t.Fatalf("state = %s, want complete", got)
	}
}

func TestADeclinedDialogTheAgentGotPastDoesNotFailItsLaterExit(t *testing.T) {
	mgr, fake, sess, _ := launchFixture(t, func(s *Session) { s.DaemonDeliversTask = false; s.OneShot = true })
	setLaunchPane(fake, sess.TmuxSession, "──────────\n Update the made-up settings?\n\n ❯ 1. Update\n   2. Not now\n\n Enter to confirm · Esc to cancel", "claude")
	if err := mgr.DeclineStartupDialog(sess.FullID); err != nil {
		t.Fatal(err)
	}
	if s, _ := mgr.GetSession(sess.FullID); mgr.store.Snapshot(s).StartupDecline == nil {
		t.Fatal("the dialog was not recorded")
	}
	// Esc closed the dialog and the agent reached its input box.
	setLaunchPane(fake, sess.TmuxSession, "──────────\n❯ \n──────────", "claude")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.StartScreenCapture(ctx, sess.FullID, 20)
	time.Sleep(200 * time.Millisecond)
	cancel()
	mgr.bg.Wait() // the watcher has stopped before the log line arrives
	setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
	feedLine(mgr, sess, "DATAWATCH_COMPLETE: claude done")
	if got := awaitState(mgr, sess.FullID, StateComplete, time.Second); got != StateComplete {
		t.Fatalf("state = %s, want complete", got)
	}
	if s, _ := mgr.GetSession(sess.FullID); mgr.store.Snapshot(s).StartupDecline != nil {
		t.Error("the record of a dialog the agent got past was kept")
	}
}

// A ❯ that is not the agent's framed input box does not show the agent got past
// a declined dialog.
func TestAShellPromptOrChoiceRowIsNotTheInputBox(t *testing.T) {
	for name, screen := range map[string]string{
		"shell prompt":       "$ clear\n❯ cd '/made-up' && claude",
		"unnumbered choices": "──────────\n Reset your settings?\n❯ Yes\n  No\n Enter to confirm · Esc to cancel",
	} {
		if showsFramedInputBox(screen) {
			t.Errorf("%s read as the input box", name)
		}
	}
	if !showsFramedInputBox("──── name ─\n❯ \n──────────\n ⏵⏵ bypass permissions on") {
		t.Error("the agent's input box was not recognised")
	}
}

// The reason an operator reads names the dialog, the files and what to do.
func TestStartupDialogDetails(t *testing.T) {
	dialog, files := startupDialogDetails(settingsErrorPane)
	if dialog != "Settings Error" || len(files) != 2 || !strings.HasSuffix(files[1], "/this/path/project/.claude/settings.local.json") {
		t.Errorf("dialog %q files %q", dialog, files)
	}
	if dialog, files = startupDialogDetails(" Update available! Update now?"); dialog != "Update available! Update now?" || files != nil {
		t.Errorf("update prompt: dialog %q files %q", dialog, files)
	}
}
