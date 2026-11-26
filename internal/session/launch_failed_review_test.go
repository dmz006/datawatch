// No healthy session may be failed or closed by the failed-launch rule.

package session

import (
	"context"
	"strings"
	"testing"
	"time"
)

const (
	agentBox      = "──────────\n❯ \n──────────\n  ⏵⏵ bypass permissions on"
	workedAndLeft = "❯ please make a made-up change\n● made-up answer\n\n❯ /exit\nDATAWATCH_COMPLETE: claude done\nuser@host:~/made-up/project$"
	updateWords   = "Do you want me to install the new version? [y/N]"
	oldDialog     = " Quick safety check: is this a project you trust?\n\n ❯ 1. Yes, I trust this folder\n   2. No, exit\n\n Enter to confirm · Esc to cancel\n"
)

// runWatchers drives every watcher of a live session in turn: the pane
// monitor, the screen watcher, the reaper and the reconciler. One at a time:
// older watcher paths write session fields without the store's lock.
func runWatchers(t *testing.T, mgr *Manager, fullID string) {
	t.Helper()
	s, ok := mgr.GetSession(fullID)
	if !ok {
		t.Fatal("session gone")
	}
	ctx, cancel := context.WithCancel(context.Background())
	mgr.goBG(func() { mgr.monitorOutput(ctx, s, NewProjectGit(s.ProjectDir)) })
	time.Sleep(4500 * time.Millisecond) // two of its 2 s pane reads
	cancel()
	mgr.WaitBackground()

	ctx, cancel = context.WithCancel(context.Background())
	mgr.StartScreenCapture(ctx, fullID, 20)
	time.Sleep(500 * time.Millisecond)
	cancel()
	mgr.bg.Wait()

	mgr.ReapFinishedOneShots()
	for i := 0; i < 2; i++ {
		ctx, cancel = context.WithCancel(context.Background())
		mgr.reconcileSessions(ctx)
		cancel()
		mgr.WaitBackground() // a monitor the reconciler restarted
	}
}

// assertLeftAlone: not failed, its pane open, and no complete/running flapping.
func assertLeftAlone(t *testing.T, mgr *Manager, fake *FakeTmux, fullID string, seen *stateLog) {
	t.Helper()
	s, ok := mgr.GetSession(fullID)
	if !ok {
		t.Fatal("session gone")
	}
	snap := mgr.store.Snapshot(s)
	if snap.State != StateRunning && snap.State != StateWaitingInput {
		t.Errorf("state = %s (%q), want still live; transitions %v", snap.State, snap.EndNote, seen.list())
	}
	if !fake.SessionExists(snap.TmuxSession) {
		t.Error("the pane was closed")
	}
	for _, st := range seen.list() {
		if st == StateComplete || st == StateFailed {
			t.Errorf("transitions %v: want no failed and no complete", seen.list())
			break
		}
	}
}

// Case A: a session that had its task is restarted, the channel bridge never
// reports ready, a person uses it and leaves.
func TestARestartedSessionAPersonUsedIsNotFailed(t *testing.T) {
	for name, after := range map[string]string{"work on screen": workedAndLeft, "no work on screen": plainExitPane} {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess, _ := launchFixture(t, nil)
			if !mgr.ClaimTaskDelivery(sess.FullID) {
				t.Fatal("claim")
			}
			mgr.MarkTaskDelivered(sess.FullID)
			if err := mgr.EndSession(sess.FullID, EndReason{Caller: "test", Reason: "kill"}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			if _, err := mgr.Restart(ctx, sess.FullID); err != nil {
				t.Fatal(err)
			}
			cancel()
			mgr.WaitBackground()
			seen := &stateLog{}
			mgr.SetStateChangeHandler(seen.add)
			setLaunchPane(fake, sess.TmuxSession, after, "bash")
			runWatchers(t, mgr, sess.FullID)
			assertLeftAlone(t, mgr, fake, sess.FullID, seen)
		})
	}
}

// A daemon that restarted is no longer typing the task, and cannot know who has
// used the session since.
func TestAfterADaemonRestartAnUndeliveredSessionIsNotFailed(t *testing.T) {
	for name, after := range map[string]string{"work on screen": workedAndLeft, "no work on screen": plainExitPane} {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess, seen := launchFixture(t, nil)
			mgr.deliveryArmed = nil // a new daemon process
			setLaunchPane(fake, sess.TmuxSession, after, "bash")
			runWatchers(t, mgr, sess.FullID)
			assertLeftAlone(t, mgr, fake, sess.FullID, seen)
		})
	}
}

// An undelivered session seen working is not failed and does not flap.
func TestAnUndeliveredSessionSeenWorkingNeitherFailsNorFlaps(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, nil)
	setLaunchPane(fake, sess.TmuxSession, "● made-up answer\n"+agentBox, "claude")
	watchScreen(mgr, sess.FullID, 300*time.Millisecond)
	setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
	runWatchers(t, mgr, sess.FullID)
	assertLeftAlone(t, mgr, fake, sess.FullID, seen)
}

// Input a person sends through datawatch before the task is typed: the session
// is theirs, and its exit is not judged.
func TestASessionAPersonTypedIntoIsNotFailedForItsTask(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, nil)
	setLaunchPane(fake, sess.TmuxSession, agentBox, "claude")
	if err := mgr.SendInput(sess.FullID, "/help", "web"); err != nil {
		t.Fatal(err)
	}
	setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
	runWatchers(t, mgr, sess.FullID)
	assertLeftAlone(t, mgr, fake, sess.FullID, seen)
}

// Cases B: ordinary text that reads like a dialog is not recorded as one, so a
// later exit of a command-line session ends as it always did.
func TestOrdinaryTextIsNeverRecordedAsADeclinedDialog(t *testing.T) {
	screens := map[string]string{
		"update words in output":     "⏺ I checked.\n" + updateWords,
		"update words, no markers":   updateWords,
		"dialog quoted mid-screen":   settingsErrorPane + "\n  made-up trailing output\n" + agentBox,
		"footer without choices":     " Update the made-up settings?\n Enter to confirm · Esc to cancel",
		"quoted by a working agent":  "⏺ the dialog reads:\n" + settingsErrorPane,
		"footer inside a sentence":   " made-up settings notes\n ❯ 1. one\n   2. two\n Then press Enter to confirm · Esc to cancel, it said",
		"the shell, after an exit":   exitedPane,
		"a dialog above an open box": settingsErrorPane + "\n" + agentBox,
		"unnumbered choices":         "──────────\n Update the made-up settings?\n ❯ Yes\n   No\n Enter to confirm · Esc to cancel",
		"boxed choices":              "╭──────╮\n│ Update the made-up settings? │\n│ ❯ 1. Yes │\n│   2. No │\n│ Enter to confirm · Esc to cancel │\n╰──────╯",
	}
	for name, screen := range screens {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess, seen := launchFixture(t, func(s *Session) { s.DaemonDeliversTask = false; s.OneShot = true })
			setLaunchPane(fake, sess.TmuxSession, screen, "claude")
			if err := mgr.DeclineStartupDialog(sess.FullID); err != nil {
				t.Fatal(err)
			}
			if got := fake.LastCall("send-keys-literal").Args; len(got) != 1 || got[0] != "\x1b" {
				t.Fatalf("the keys must not change, sent %q", got)
			}
			setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
			watchScreen(mgr, sess.FullID, 400*time.Millisecond)
			for _, st := range seen.list() {
				if st == StateFailed {
					t.Fatalf("failed: %v", seen.list())
				}
			}
		})
	}
}

// Case C: a start dialog still in the scrollback, a false match later, a normal
// exit.
func TestOldDialogTextInScrollbackDecidesNothing(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, func(s *Session) { s.DaemonDeliversTask = false })
	fake.History = map[string]string{sess.TmuxSession: oldDialog}
	setLaunchPane(fake, sess.TmuxSession, updateWords+"\n"+agentBox, "claude")
	if err := mgr.DeclineStartupDialog(sess.FullID); err != nil {
		t.Fatal(err)
	}
	// The pane monitor's own capture holds the whole scrollback.
	s, _ := mgr.GetSession(sess.FullID)
	ctx, cancel := context.WithCancel(context.Background())
	mgr.goBG(func() { mgr.monitorOutput(ctx, s, NewProjectGit(s.ProjectDir)) })
	time.Sleep(4500 * time.Millisecond)
	cancel()
	mgr.WaitBackground()
	setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
	watchScreen(mgr, sess.FullID, 400*time.Millisecond)
	for _, st := range seen.list() {
		if st == StateFailed {
			t.Fatalf("failed: %v", seen.list())
		}
	}
}

// An exit line that scrolled off the live screen is not this screen's.
func TestAnOldExitLineInScrollbackIsNotAnExit(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, nil)
	fake.History = map[string]string{sess.TmuxSession: plainExitPane + "\n"}
	setLaunchPane(fake, sess.TmuxSession, "made-up shell output\nuser@host$", "bash")
	runWatchers(t, mgr, sess.FullID)
	for _, st := range seen.list() {
		if st == StateFailed {
			t.Fatalf("failed: %v", seen.list())
		}
	}
	if !fake.SessionExists(sess.TmuxSession) {
		t.Error("the pane was closed")
	}
}

// Case D and its kin: the pane's foreground reads as a shell while something is
// still on the terminal or under the pane's shell.
func TestAnExitLineWithAProcessLeftClosesNothing(t *testing.T) {
	cases := map[string][]paneProc{
		"non-exec wrapper and its agent": {
			{pid: 201, ppid: fakeShellPID, tty: fakeTTY, comm: "bash"},
			{pid: 202, ppid: 201, tty: fakeTTY, comm: "claude"},
		},
		"wrapper alone":                   {{pid: 201, ppid: fakeShellPID, tty: fakeTTY, comm: "sh"}},
		"orphan still on the terminal":    {{pid: 301, ppid: 1, tty: fakeTTY, comm: "node"}},
		"child that left the terminal":    {{pid: 302, ppid: fakeShellPID, tty: "?", comm: "claude"}},
		"grandchild of a gone-quiet tree": {{pid: 303, ppid: fakeShellPID, tty: "?", comm: "sh"}, {pid: 304, ppid: 303, tty: "?", comm: "node"}},
	}
	for name, left := range cases {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess, seen := launchFixture(t, nil)
			mgr.paneProcsFn = fakeProcs(fake, left...)
			mgr.paneChildFn = func(string) bool { return true }
			// A fast clock: waiting longer is never proof.
			start := time.Now()
			mgr.clock = func() time.Time { return start.Add(20 * time.Since(start)) }
			setLaunchPane(fake, sess.TmuxSession, "DATAWATCH_COMPLETE: claude done\n", "bash")
			runWatchers(t, mgr, sess.FullID)
			assertLeftAlone(t, mgr, fake, sess.FullID, seen)
		})
	}
}

// A process table that cannot be read proves nothing.
func TestAnUnreadableProcessTableClosesNothing(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, nil)
	mgr.paneProcsFn = func(string) (int, string, []paneProc, bool) { return 0, "", nil, false }
	setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
	runWatchers(t, mgr, sess.FullID)
	assertLeftAlone(t, mgr, fake, sess.FullID, seen)
}

// Time alone is never proof: a process left on the terminal long after the exit
// line appeared still closes nothing.
func TestWaitingLongerIsNotProof(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, nil)
	mgr.paneProcsFn = fakeProcs(fake, paneProc{pid: 301, ppid: 1, tty: fakeTTY, comm: "node"})
	setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
	now := time.Now()
	for i := 0; i < 4; i++ {
		later := now.Add(time.Duration(i) * time.Hour)
		mgr.clock = func() time.Time { return later }
		if !mgr.guardEarlyExit(sess.FullID) {
			t.Fatal("completion was not held")
		}
	}
	assertLeftAlone(t, mgr, fake, sess.FullID, seen)
}

// The rows a real process listing gives, on a host pane at work and after it.
func TestOnlyShellLeft(t *testing.T) {
	atWork := "  100     1 pts/9    bash\n  200   100 pts/9    claude\n  300   200 pts/9    datawatch-chann\n  900     1 ?        systemd\n"
	if onlyShellLeft(100, "/dev/pts/9", parseProcessTable(atWork)) {
		t.Error("a pane with claude on it read as gone")
	}
	if !onlyShellLeft(100, "/dev/pts/9", parseProcessTable("  100     1 pts/9    -bash\n  900     1 ?        systemd\n  901     1 pts/3    node\n")) {
		t.Error("a pane with only its shell read as not gone")
	}
	for name, table := range map[string]string{
		"no shell row":            "  900     1 ?        systemd\n",
		"the pane is not a shell": "  100     1 pts/9    claude\n",
		"another tty":             "  100     1 pts/8    bash\n",
	} {
		if onlyShellLeft(100, "/dev/pts/9", parseProcessTable(table)) {
			t.Errorf("%s read as gone", name)
		}
	}
}

// A tmux error is not a closed terminal.
func TestATmuxErrorIsNotAGoneTerminal(t *testing.T) {
	ends := map[string]func(*Manager){
		"reconciler": func(m *Manager) { m.reconcileSessions(context.Background()) },
		"finaliser":  func(m *Manager) { m.FinaliseGoneSessions(time.Now()) },
		"reconnect":  func(m *Manager) { m.ReconnectBackends(func(*Session, *BackendState) {}) },
	}
	for name, run := range ends {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess, seen := launchFixture(t, func(s *Session) { s.TrackingDir = s.ProjectDir })
			fake.mu.Lock()
			fake.Unanswerable = map[string]bool{sess.TmuxSession: true}
			fake.mu.Unlock()
			run(mgr)
			s, _ := mgr.GetSession(sess.FullID)
			if got := mgr.store.Snapshot(s).State; got != StateRunning {
				t.Errorf("state = %s, transitions %v", got, seen.list())
			}
			if fake.Count("kill-session") != 0 {
				t.Error("a pane was closed on a tmux error")
			}
		})
	}
}

// A decline on a start the daemon is not typing a task for says nothing about a
// task: a restarted session may well have had it.
func TestARestartedSessionIsNotToldItNeverHadItsTask(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, nil)
	mgr.deliveryArmed = nil
	declineAndExit(t, mgr, fake, sess)
	watchScreen(mgr, sess.FullID, 3*time.Second)
	assertFailedOnce(t, mgr, fake, sess.FullID, seen, "Settings Error")
	s, _ := mgr.GetSession(sess.FullID)
	if note := mgr.store.Snapshot(s).LaunchFailure; strings.Contains(note, "task") {
		t.Errorf("reason talks about a task: %q", note)
	}
}

// A one-shot that is not judged ends as it did before this rule.
func TestAnUnjudgedOneShotCompletesAsBefore(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, func(s *Session) { s.OneShot = true })
	mgr.deliveryArmed = nil // restarted, or the daemon was
	setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
	ctx, cancel := context.WithCancel(context.Background())
	mgr.StartScreenCapture(ctx, sess.FullID, 20)
	time.Sleep(500 * time.Millisecond)
	cancel()
	mgr.bg.Wait()
	s, _ := mgr.GetSession(sess.FullID)
	if got := mgr.store.Snapshot(s).State; got != StateComplete {
		t.Fatalf("state = %s, want complete as before; transitions %v", got, seen.list())
	}
}

// A one-shot that would fail but lacks proof stays open.
func TestAOneShotWithoutProofIsNeitherFailedNorComplete(t *testing.T) {
	mgr, fake, sess, seen := launchFixture(t, func(s *Session) { s.OneShot = true })
	mgr.paneProcsFn = fakeProcs(fake, paneProc{pid: 301, ppid: 1, tty: fakeTTY, comm: "node"})
	setLaunchPane(fake, sess.TmuxSession, plainExitPane, "bash")
	runWatchers(t, mgr, sess.FullID)
	assertLeftAlone(t, mgr, fake, sess.FullID, seen)
}

// A line entered in the web terminal is a person using the session; a single
// key without Enter is not read as that.
func TestALineEnteredInTheWebTerminalCountsAsSomeoneUsingTheSession(t *testing.T) {
	mgr, fake, sess, _ := launchFixture(t, nil)
	setLaunchPane(fake, sess.TmuxSession, agentBox, "claude")
	if err := mgr.SendRawKeys(sess.FullID, "a"); err != nil {
		t.Fatal(err)
	}
	if !mgr.EndedBeforeTask(sess.FullID) {
		t.Fatal("a key without Enter ended the wait for the first task")
	}
	if err := mgr.SendRawKeys(sess.FullID, "\r"); err != nil {
		t.Fatal(err)
	}
	if mgr.EndedBeforeTask(sess.FullID) {
		t.Error("an entered line did not count")
	}
}

// Only a fresh channel start waits for a first task.
func TestOnlyAFreshChannelStartWaitsForItsFirstTask(t *testing.T) {
	for name, tc := range map[string]struct {
		channel bool
		resume  string
		want    bool
	}{"fresh channel start": {true, "", true}, "resumed start": {true, "made-up-id", false}, "channel off": {false, "", false}} {
		t.Run(name, func(t *testing.T) {
			mgr, _, sess, _ := launchFixture(t, func(s *Session) { s.DaemonDeliversTask = false })
			mgr.deliveryArmed = nil
			ctx, cancel := context.WithCancel(context.Background())
			mgr.armTaskDeliveryAfterStart(ctx, sess, madeUpChannelBackend(tc.channel), tc.resume)
			cancel()
			mgr.bg.Wait()
			if got := mgr.EndedBeforeTask(sess.FullID); got != tc.want {
				t.Errorf("waiting for a first task = %v, want %v", got, tc.want)
			}
		})
	}
}

type madeUpChannelBackend bool

func (b madeUpChannelBackend) ChannelEnabled() bool { return bool(b) }
