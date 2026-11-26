// Before typing a task or queued input into a claude-code session, datawatch
// looks for two signs that the agent has left, where the pane's shell would run
// the text outside every check the agent has.

package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// markerTask's second line is what a shell would run. It is harmless.
const (
	markerLine = "touch MADE-UP-MARKER-RUN-BY-SHELL"
	markerTask = "Reply with the single word ready.\n" + markerLine
)

const (
	guardShellPID = 100
	guardTTY      = "pts/99"
)

// What is on a made-up pane's terminal.
var (
	guardShellRow = func(foreground int) paneProc {
		return paneProc{pid: guardShellPID, ppid: 1, pgid: guardShellPID, tpgid: foreground, tty: guardTTY, comm: "bash"}
	}
	// the shell is reading the terminal
	paneBareShell = []paneProc{guardShellRow(guardShellPID)}
	// something the agent left behind, in the background
	paneLeftover = []paneProc{guardShellRow(guardShellPID),
		{pid: 301, ppid: 1, pgid: 250, tpgid: guardShellPID, tty: guardTTY, comm: "sleep"}}
	// the agent is the foreground job
	paneAgent = []paneProc{guardShellRow(200),
		{pid: 200, ppid: guardShellPID, pgid: 200, tpgid: 200, tty: guardTTY, comm: "claude"}}
	// a wrapper script that did not exec: tmux still names the pane "bash"
	paneWrapper = []paneProc{guardShellRow(200),
		{pid: 200, ppid: guardShellPID, pgid: 200, tpgid: 200, tty: guardTTY, comm: "bash"},
		{pid: 201, ppid: 200, pgid: 200, tpgid: 200, tty: guardTTY, comm: "claude"}}
	// job control off: the agent shares the shell's group
	paneNoJobControl = []paneProc{guardShellRow(guardShellPID),
		{pid: 200, ppid: guardShellPID, pgid: guardShellPID, tpgid: guardShellPID, tty: guardTTY, comm: "claude"}}
)

const (
	// what the review left on screen: an old prompt row, then the exit line
	leftScreen = "❯ /exit\n  made-up goodbye\nDATAWATCH_COMPLETE: claude done\nuser@host:~/made-up$ "
	// the same after the exit line scrolled away
	scrolledScreen = "made-up shell output\n❯ "
	liveBox        = "──────────\n❯ \n──────────\n  ⏵⏵ bypass permissions on"
)

func guardFixture(t *testing.T, screen string, procs []paneProc) (*Manager, *FakeTmux, *Session) {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	dir := t.TempDir()
	logFile := filepath.Join(dir, "output.log")
	if err := os.WriteFile(logFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sess := &Session{
		ID: "tg01", FullID: "testhost-tg01", Hostname: "testhost",
		TmuxSession: "cs-testhost-tg01", BackendFamily: "claude-code",
		Task: markerTask, ProjectDir: dir, LogFile: logFile,
		State: StateWaitingInput, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		DaemonDeliversTask: true,
	}
	if err := fake.NewSessionWithSize(sess.TmuxSession, 120, 40); err != nil {
		t.Fatal(err)
	}
	fake.Pane[sess.TmuxSession] = screen
	mgr.paneProcsFn = func(string) (int, string, []paneProc, bool) { return guardShellPID, guardTTY, procs, procs != nil }
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.WaitBackground)
	return mgr, fake, sess
}

// typed returns every text datawatch sent to the pane.
func typed(fake *FakeTmux) []string {
	var out []string
	for _, c := range fake.CallsSnapshot() {
		if strings.HasPrefix(c.Op, "send-") {
			out = append(out, c.Args...)
		}
	}
	return out
}

func typedContains(fake *FakeTmux, want string) bool {
	return strings.Contains(strings.Join(typed(fake), "\x00"), want)
}

// The two routes of the review, and a person who used tmux directly: the agent
// is gone, a ❯ row is still on screen, and delivery runs.
func TestTheTaskIsNeverTypedOnceTheAgentHasLeft(t *testing.T) {
	cases := map[string]struct {
		screen string
		procs  []paneProc
		before func(*testing.T, *Manager, *Session)
	}{
		"input was sent through datawatch first": {leftScreen, paneBareShell, func(t *testing.T, m *Manager, s *Session) {
			if err := m.SendInput(s.FullID, "made-up note", "web"); err != nil {
				t.Fatal(err)
			}
		}},
		"a process was left on the terminal":         {leftScreen, paneLeftover, nil},
		"the exit line is up, processes unreadable":  {leftScreen, nil, nil},
		"the exit line scrolled away, a bare shell":  {scrolledScreen, paneBareShell, nil},
		"the exit line scrolled away, with leftover": {scrolledScreen, paneLeftover, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess := guardFixture(t, tc.screen, tc.procs)
			if tc.before != nil {
				tc.before(t, mgr, sess)
			}
			if !mgr.ClaimTaskDelivery(sess.FullID) {
				t.Fatal("claim")
			}
			err := mgr.DeliverOneShotTask(sess.FullID, markerTask, 1, 2*time.Second, time.Millisecond)
			if typedContains(fake, markerLine) {
				t.Fatalf("the task was typed into the shell: %q", typed(fake))
			}
			if !errors.Is(err, ErrTaskUndelivered) {
				t.Errorf("err = %v, want undelivered", err)
			}
			if reason := mgr.TaskDeliveryFailure(sess.FullID); !strings.Contains(reason, "not typed again") {
				t.Errorf("reason = %q, want it to say the task was not typed again", reason)
			}
			if got, _ := mgr.GetSession(sess.FullID); !mgr.store.Snapshot(got).State.IsTerminal() {
				t.Error("the session was left live and deliverable")
			}
		})
	}
}

// Queued and other automated input is held to the same rule.
func TestQueuedInputIsNeverTypedOnceTheAgentHasLeft(t *testing.T) {
	for _, source := range []string{"schedule", "automaton", "questions-answer", "supervisor-wake", "idle-nudge", "task-redelivery", FilterSource("made-up")} {
		t.Run(source, func(t *testing.T) {
			mgr, fake, sess := guardFixture(t, leftScreen, paneBareShell)
			err := mgr.SendInput(sess.FullID, markerLine, source)
			if typedContains(fake, markerLine) {
				t.Fatalf("typed into the shell: %q", typed(fake))
			}
			if err == nil {
				t.Error("the send reported success")
			}
		})
	}
}

// Everything else types exactly as before.
func TestTheGuardChangesNothingElse(t *testing.T) {
	type send struct {
		screen, source, family string
		procs                  []paneProc
	}
	cases := map[string]send{
		"a live agent":                         {liveBox, "channel-task", "claude-code", paneAgent},
		"a live agent, queued input":           {liveBox, "schedule", "claude-code", paneAgent},
		"a live agent behind a wrapper":        {liveBox, "channel-task", "claude-code", paneWrapper},
		"job control off, the agent running":   {liveBox, "channel-task", "claude-code", paneNoJobControl},
		"an unreadable process table":          {liveBox, "channel-task", "claude-code", nil},
		"a person's own send to a bare shell":  {leftScreen, "web", "claude-code", paneBareShell},
		"a person's tool send to a bare shell": {leftScreen, "mcp", "claude-code", paneBareShell},
		"a start-up dialog answer":             {leftScreen, StartupAcceptSource, "claude-code", paneBareShell},
		"queued input to a shell session":      {leftScreen, "schedule", "shell", paneBareShell},
		"the typed launch command on screen":   {"user@host$ cd /x && claude; echo 'DATAWATCH_COMPLETE: claude done'\n" + liveBox, "channel-task", "claude-code", paneAgent},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess := guardFixture(t, tc.screen, tc.procs)
			mgr.store.Mutate(func() { sess.BackendFamily = tc.family })
			if err := mgr.SendInput(sess.FullID, "made-up words", tc.source); err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !typedContains(fake, "made-up words") {
				t.Fatalf("not typed: %q", typed(fake))
			}
		})
	}
}

// The rate-limit resume line is queued input too.
func TestTheRateLimitResumeIsNotTypedIntoAShell(t *testing.T) {
	mgr, fake, sess := guardFixture(t, leftScreen, paneBareShell)
	mgr.store.Mutate(func() { sess.State = StateRateLimited })
	mgr.ResumeRateLimitedSession(t.Context(), sess.FullID)
	if typedContains(fake, "rate limit has reset") {
		t.Fatalf("typed into the shell: %q", typed(fake))
	}
}

// The case where the message was wrong: the first attempt typed the task and a
// later one was refused. The session's message must not say nothing was typed.
func TestARefusalAfterTheTaskWasTypedDoesNotSayNothingWasTyped(t *testing.T) {
	mgr, fake, sess := guardFixture(t, liveBox, paneAgent)
	fake.NoEcho = true // the typed text never shows, so delivery tries again
	gone := false
	mgr.paneProcsFn = func(string) (int, string, []paneProc, bool) {
		if gone {
			return guardShellPID, guardTTY, paneBareShell, true
		}
		return guardShellPID, guardTTY, paneAgent, true
	}
	now := time.Now()
	mgr.clock = func() time.Time { return now }
	mgr.sleepFn = func(d time.Duration) {
		now = now.Add(d)
		if !gone && typedContains(fake, markerLine) {
			gone = true // the agent leaves after the first attempt typed
			fake.mu.Lock()
			fake.Pane[sess.TmuxSession] = leftScreen
			fake.mu.Unlock()
		}
	}
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("claim")
	}
	if err := mgr.DeliverOneShotTask(sess.FullID, markerTask, 2, 5*time.Second, time.Millisecond); !errors.Is(err, ErrTaskUndelivered) {
		t.Fatalf("err = %v", err)
	}
	if n := strings.Count(strings.Join(typed(fake), "\x00"), markerLine); n != 1 {
		t.Fatalf("the task was typed %d times, want once: %q", n, typed(fake))
	}
	reason := mgr.TaskDeliveryFailure(sess.FullID)
	if strings.Contains(reason, "nothing was typed") {
		t.Errorf("reason says nothing was typed, but it was: %q", reason)
	}
	for _, want := range []string{"not typed again", "earlier attempt may already have typed it"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason %q lacks %q", reason, want)
		}
	}
}

// The exit line is also matched behind spaces and the marks a terminal UI puts
// in front of a line, so a running agent showing it there is refused too.
func TestTheExitLineIsMatchedBehindMarksAndSpaces(t *testing.T) {
	for name, line := range map[string]string{
		"spaces": "    DATAWATCH_COMPLETE: claude done",
		"corner": "  ⎿  DATAWATCH_COMPLETE: claude done",
		"bullet": "● DATAWATCH_COMPLETE: claude done",
	} {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess := guardFixture(t, line+"\n"+liveBox, paneAgent)
			if err := mgr.SendInput(sess.FullID, "made-up words", "schedule"); !errors.Is(err, ErrAgentLeft) {
				t.Fatalf("err = %v, typed %q", err, typed(fake))
			}
		})
	}
	for name, line := range map[string]string{
		"words before":   "it printed DATAWATCH_COMPLETE: claude done",
		"words after":    "DATAWATCH_COMPLETE: claude done and tidied up",
		"other summary":  "DATAWATCH_COMPLETE: made the change",
		"an ascii mark ": "> DATAWATCH_COMPLETE: claude done",
	} {
		t.Run(name, func(t *testing.T) {
			mgr, _, sess := guardFixture(t, line+"\n"+liveBox, paneAgent)
			if err := mgr.SendInput(sess.FullID, "made-up words", "schedule"); err != nil {
				t.Fatalf("refused: %v", err)
			}
		})
	}
}

// What a refusal does to a schedule: it is marked failed, so it is not tried
// again, and a recurring one does not come round again.
func TestARefusedScheduleIsNotRetriedAndARecurringOneStops(t *testing.T) {
	for name, every := range map[string]int{"one time": 0, "recurring": 60} {
		t.Run(name, func(t *testing.T) {
			mgr, fake, sess := guardFixture(t, leftScreen, paneBareShell)
			store, err := NewScheduleStore(filepath.Join(t.TempDir(), "schedule.json"))
			if err != nil {
				t.Fatal(err)
			}
			sc, err := store.AddFull(AddOptions{SessionID: sess.FullID, Command: markerLine,
				RunAt: time.Now().Add(-time.Second), RecurEvery: every})
			if err != nil {
				t.Fatal(err)
			}
			sent, err := mgr.FireScheduled(store, sess.FullID, sc)
			if sent || !errors.Is(err, ErrAgentLeft) || typedContains(fake, markerLine) {
				t.Fatalf("sent=%v err=%v typed=%q", sent, err, typed(fake))
			}
			var state string
			for _, e := range store.List() {
				if e.ID == sc.ID {
					state = string(e.State)
				}
			}
			if state != string(SchedFailed) {
				t.Errorf("state = %q, want failed", state)
			}
			if due := store.DuePending(time.Now().Add(time.Hour)); len(due) != 0 {
				t.Errorf("still due later: %+v", due)
			}
		})
	}
}
