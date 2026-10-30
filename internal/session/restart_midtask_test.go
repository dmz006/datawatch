// Mid-task restart adoption matrix.
//
// A claude-code one-shot PRD worker that is mid-task when the daemon restarts
// has a long, wrapped task on its pane (or scrolled off it), tool output, a
// spinner/status line and the bottom prompt box. Manager B on the same data
// directory must adopt it and never end it: not through ResumeMonitors, not
// through the boot-time screen-capture recovery that cmd/datawatch starts a few
// seconds after boot, and not through the periodic reconciler. The persisted
// delivery stamp is aged, as it is for any worker that has been running longer
// than the task-echo guard window.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestRestartMidTask

package session

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const mtHarness = "\n\n---\n" +
	"Checkpoint every 5-10 min: write CHECKPOINT.md (steps done/now/remaining). " +
	"Then keep working immediately in the same turn — do not stop or wait. " +
	"CHECKPOINT.md must not be committed. " +
	"Do not run git add or git commit — your work is committed automatically when you finish. " +
	"Finish with a line: DATAWATCH_COMPLETE: <one-line plain-text summary of what you did> — that summary becomes the commit message.\n" +
	"---"

const mtTask = "Work in internal/session and cmd/datawatch. Evidence: on a real restart two workers were ended " +
	"shortly after the new daemon started while the others stayed alive. Read ResumeMonitors and every function it " +
	"calls, then write a deterministic test with the fake terminal multiplexer and fix the smallest correct way." + mtHarness

// mtWrap hard-wraps s at width columns on word boundaries, the way the claude
// input box wraps a long typed task. Existing newlines are kept.
func mtWrap(s string, width int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, w := range strings.Fields(para) {
			if line != "" && len(line)+1+len(w) > width {
				out = append(out, line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += w
		}
		out = append(out, line)
	}
	return out
}

const (
	mtToolOutput = "● Bash(go test ./internal/session/...)\n" +
		"  ⎿  ok  \tgithub.com/dmz006/datawatch/internal/session\t4.2s\n" +
		"● Read(internal/session/manager.go)\n" +
		"  ⎿  Read 220 lines\n"
	mtStatus  = "✻ Reading… (38s · ↓ 1.2k tokens · esc to interrupt)\n"
	mtBoxIdle = "╭──────────────────────────────────────────╮\n" +
		"│ >                                        │\n" +
		"╰──────────────────────────────────────────╯\n" +
		"  ? for shortcuts\n"
	mtBoxTyping = "╭──────────────────────────────────────────╮\n" +
		"│ > also check the reconcil                │\n" +
		"╰──────────────────────────────────────────╯\n"
)

// mtEchoed renders the typed task as a user message ("> " first line, indented
// continuation) wrapped at width.
func mtEchoed(width int) string {
	lines := mtWrap(mtTask, width-2)
	var b strings.Builder
	for i, l := range lines {
		if i == 0 {
			b.WriteString("> " + l + "\n")
		} else {
			b.WriteString("  " + l + "\n")
		}
	}
	return b.String()
}

// mtTail keeps only the last n wrapped lines of the echoed task, as if the top
// of the message had scrolled off.
func mtTail(width, n int) string {
	lines := strings.Split(strings.TrimRight(mtEchoed(width), "\n"), "\n")
	if n > len(lines) {
		n = len(lines)
	}
	return strings.Join(lines[len(lines)-n:], "\n") + "\n"
}

type mtOpts struct {
	pane     string
	age      time.Duration // how long ago the task was delivered
	noLog    bool          // remove the pane log file before Manager B boots
	noDeliv  bool          // no persisted TaskDeliveredAt
	observe  time.Duration
	skipReco bool // do not run the boot-time screen-capture recovery
}

// mtRun boots Manager A, delivers the task, "restarts" into Manager B and
// returns after the observation window with the final session and the number
// of onSessionEnd calls.
func mtRun(t *testing.T, o mtOpts) (*Session, int32, *FakeTmux) {
	t.Helper()
	dataDir := t.TempDir()
	a, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(a)
	fake := a.WithFakeTmux()
	ctxA, cancelA := context.WithCancel(context.Background())
	sess, err := a.Start(ctxA, mtTask, "", t.TempDir(), &StartOptions{
		Backend: "claude-code",
		OneShot: true,
		PRDID:   "prd-midtask",
		LaunchFn: func(_ context.Context, _, tmuxSession, _, logFile string) error {
			_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
			_ = fake.PipeOutput(tmuxSession, logFile)
			fake.mu.Lock()
			fake.Pane[tmuxSession] = tdIdlePane
			fake.mu.Unlock()
			return nil
		},
	})
	if err != nil {
		cancelA()
		t.Fatalf("Start: %v", err)
	}
	if !o.noDeliv && !a.ClaimTaskDelivery(sess.FullID) {
		cancelA()
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	cur, _ := a.GetSession(sess.FullID)
	cur.State = StateRunning
	if !o.noDeliv {
		past := time.Now().Add(-o.age)
		cur.TaskDeliveredAt = &past
	}
	if err := a.SaveSession(cur); err != nil {
		cancelA()
		t.Fatalf("SaveSession: %v", err)
	}
	cancelA()
	a.mu.Lock()
	for _, c := range a.monitors {
		c()
	}
	a.mu.Unlock()

	if cur.LogFile != "" {
		if o.noLog {
			_ = os.Remove(cur.LogFile)
		} else if _, err := os.Stat(cur.LogFile); err != nil {
			_ = os.WriteFile(cur.LogFile, nil, 0o600)
		}
	}
	fake.mu.Lock()
	fake.Pane[sess.TmuxSession] = o.pane
	fake.mu.Unlock()

	b, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(b)
	b.tmux = fake
	b.probeClaudeAliveFunc = func(*Session) bool { return true }
	var ended atomic.Int32
	b.SetOnSessionEnd(func(*Session) { ended.Add(1) })

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		time.Sleep(200 * time.Millisecond) // let monitors drain
	}()
	b.ResumeMonitors(ctx)
	// cmd/datawatch starts screen capture for live one-shot sessions shortly
	// after boot; the interval is shortened here, the code path is the same.
	if !o.skipReco {
		for _, s := range b.ListSessions() {
			if s.OneShot && s.TmuxSession != "" && (s.State == StateRunning || s.State == StateWaitingInput) {
				b.StartScreenCapture(ctx, s.FullID, 50)
			}
		}
	}
	b.reconcileSessions(ctx)

	obs := o.observe
	if obs == 0 {
		obs = 2600 * time.Millisecond
	}
	deadline := time.Now().Add(obs)
	for time.Now().Before(deadline) {
		if c, _ := b.GetSession(sess.FullID); c != nil && c.State != StateRunning {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	final, _ := b.GetSession(sess.FullID)
	// The end callback runs just after the state flips; on a loaded runner the
	// loop above can read the new state before the callback has counted.
	if final != nil && final.State != StateRunning && final.State != StateWaitingInput {
		for wait := time.Now().Add(3 * time.Second); ended.Load() == 0 && time.Now().Before(wait); {
			time.Sleep(25 * time.Millisecond)
		}
	}
	return final, ended.Load(), fake
}

func mtAssertAdopted(t *testing.T, o mtOpts) {
	t.Helper()
	final, ended, fake := mtRun(t, o)
	if final == nil {
		t.Fatal("session vanished")
	}
	if final.State != StateRunning {
		t.Errorf("live worker ended by startup logic: state=%q, want %q\npane:\n%s", final.State, StateRunning, o.pane)
	}
	if ended != 0 {
		t.Errorf("onSessionEnd called %d times for a live worker, want 0", ended)
	}
	if !fake.SessionExists(final.TmuxSession) {
		t.Error("the worker's pane was closed")
	}
}

func TestRestartMidTask_PaneMatrix(t *testing.T) {
	widths := []int{60, 80, 100, 120, 160}
	type pc struct {
		name string
		pane func(w int) string
	}
	panes := []pc{
		{"echo_visible", func(w int) string {
			return mtEchoed(w) + "\n" + mtToolOutput + "\n" + mtStatus + "\n" + mtBoxIdle
		}},
		{"echo_scrolled_off", func(w int) string {
			return mtToolOutput + "\n" + mtStatus + "\n" + mtBoxIdle
		}},
		{"echo_tail_only", func(w int) string {
			return mtTail(w, 3) + "\n" + mtToolOutput + "\n" + mtStatus + "\n" + mtBoxIdle
		}},
		{"echo_tail_one_line", func(w int) string {
			return mtTail(w, 1) + "\n" + mtToolOutput + "\n" + mtStatus + "\n" + mtBoxIdle
		}},
		{"status_only", func(w int) string { return mtStatus }},
		{"typing_box", func(w int) string {
			return mtEchoed(w) + "\n" + mtToolOutput + "\n" + mtStatus + "\n" + mtBoxTyping
		}},
		{"typing_box_no_echo", func(w int) string {
			return mtToolOutput + "\n" + mtStatus + "\n" + mtBoxTyping
		}},
	}
	stamps := []struct {
		name string
		o    mtOpts
	}{
		{"old_stamp", mtOpts{age: 20 * time.Minute}},
		{"no_stamp", mtOpts{noDeliv: true}},
	}
	for _, st := range stamps {
		for _, p := range panes {
			for _, w := range widths {
				t.Run(fmt.Sprintf("%s/%s/w%d", st.name, p.name, w), func(t *testing.T) {
					t.Parallel()
					o := st.o
					o.pane = p.pane(w)
					mtAssertAdopted(t, o)
				})
			}
		}
	}
}

func TestRestartMidTask_PersistedFieldMatrix(t *testing.T) {
	pane := mtEchoed(80) + "\n" + mtToolOutput + "\n" + mtStatus + "\n" + mtBoxIdle
	cases := map[string]mtOpts{
		"fresh_delivery_log":      {age: 2 * time.Second},
		"old_delivery_log":        {age: 20 * time.Minute},
		"old_delivery_no_log":     {age: 20 * time.Minute, noLog: true},
		"fresh_delivery_no_log":   {age: 2 * time.Second, noLog: true},
		"no_delivery_stamp":       {noDeliv: true},
		"no_delivery_stamp_nolog": {noDeliv: true, noLog: true},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			o.pane = pane
			mtAssertAdopted(t, o)
		})
	}
}

// A real completion on a mid-task pane after restart is still detected.
func TestRestartMidTask_RealCompletionStillDetected(t *testing.T) {
	pane := mtEchoed(80) + "\n" + mtToolOutput + "\n" +
		"● Done.\nDATAWATCH_COMPLETE: fixed the restart adoption path\n\n" + mtBoxIdle
	final, ended, _ := mtRun(t, mtOpts{pane: pane, age: 20 * time.Minute, observe: 6 * time.Second})
	if final.State != StateComplete {
		t.Errorf("real completion not detected: state=%q, want %q", final.State, StateComplete)
	}
	if ended == 0 {
		t.Error("onSessionEnd not called for a real completion")
	}
}

// mtLaunchLine is the shell line the claude-code backend types into a fresh
// pane; the terminal hard-wraps it at the pane width, so any wrapped fragment
// can begin with the marker text.
const mtLaunchLine = "cd '/home/user/.datawatch/prd-worktrees/91e60001' && NO_COLOR=1 claude --dangerously-skip-permissions " +
	"--add-dir '/home/user/.datawatch/prd-worktrees/91e60001' --session-id 'abcd'; echo 'DATAWATCH_COMPLETE: claude done'"

// mtHardWrap wraps at exactly width columns (no word boundaries).
func mtHardWrap(s string, width int) string {
	var b strings.Builder
	for len(s) > width {
		b.WriteString(s[:width] + "\n")
		s = s[width:]
	}
	b.WriteString(s + "\n")
	return b.String()
}

// mtLaunchWrappedAtMarker returns the launch line wrapped so a line starts at
// the marker text, the worst case for the scanners.
func mtLaunchWrappedAtMarker() string {
	i := strings.Index(mtLaunchLine, "DATAWATCH_COMPLETE:")
	return "$ " + mtLaunchLine[:i] + "\n" + mtLaunchLine[i:] + "\n"
}

func TestRestartMidTask_ScrollbackShapes(t *testing.T) {
	const chip = "> [Pasted text #1 +24 lines]\n"
	ansi := func(s string) string { return "\x1b[39m\x1b[2m" + strings.ReplaceAll(s, "\n", "\x1b[0m\n\x1b[2m") }
	cases := map[string]string{
		"launch_wrapped_chip": mtLaunchWrappedAtMarker() + "\n" + chip + "\n" + mtToolOutput + mtStatus + mtBoxIdle,
		"launch_wrapped_echo": mtLaunchWrappedAtMarker() + "\n" + mtEchoed(80) + "\n" + mtToolOutput + mtStatus + mtBoxIdle,
		"launch_hardwrap_80":  mtHardWrap(mtLaunchLine, 80) + "\n" + chip + mtToolOutput + mtStatus + mtBoxIdle,
		"launch_hardwrap_120": mtHardWrap(mtLaunchLine, 120) + "\n" + chip + mtToolOutput + mtStatus + mtBoxIdle,
		"nudge_line": mtEchoed(80) + "\n" + mtToolOutput + "> If you are finished, print DATAWATCH_COMPLETE followed by a one-line summary;\n" +
			"  otherwise continue with the task.\n" + mtStatus + mtBoxIdle,
		"ansi_echo":        ansi(mtEchoed(80) + "\n" + mtToolOutput + mtStatus + mtBoxIdle),
		"ansi_chip":        ansi(chip + "\n" + mtToolOutput + mtStatus + mtBoxIdle),
		"waiting_prompt":   mtEchoed(80) + "\n" + mtToolOutput + "● Next I will run the tests.\n\n" + mtBoxIdle,
		"doc_read_output":  chip + "● Read(docs/plans/x.md)\n  ⎿  Read 12 lines\n     DATAWATCH_COMPLETE: <one-line plain-text summary of what you did>\n" + mtStatus + mtBoxIdle,
		"summary_in_code":  chip + "● Read(x_test.go)\n  ⎿  \"DATAWATCH_COMPLETE: added the widget\"\n" + mtStatus + mtBoxIdle,
		"echo_only_marker": "DATAWATCH_COMPLETE:\n" + chip + mtStatus + mtBoxIdle,
	}
	for name, pane := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mtAssertAdopted(t, mtOpts{pane: pane, age: 20 * time.Minute})
		})
	}
}

// The wrapped tail of the typed launch command is shell source, not output;
// the wrapper's own echo output after the worker exits is a real completion.
func TestRestartMidTask_ShellSourceTailIsNotCompletion(t *testing.T) {
	for _, l := range []string{
		"DATAWATCH_COMPLETE: claude done'",
		"DATAWATCH_COMPLETE: pi done'",
		"DATAWATCH_COMPLETE: shell task done\"",
		"  ● DATAWATCH_COMPLETE: claude done'",
	} {
		if isRealCompletionLine(l, "DATAWATCH_COMPLETE:") {
			t.Errorf("%q counted as a real completion", l)
		}
	}
	for _, l := range []string{
		"DATAWATCH_COMPLETE: claude done",
		"DATAWATCH_COMPLETE: fixed the 'foo' option",
		"DATAWATCH_COMPLETE: renamed 'a' to 'b'",
		"● DATAWATCH_COMPLETE: added the widget and its tests",
	} {
		if !isRealCompletionLine(l, "DATAWATCH_COMPLETE:") {
			t.Errorf("%q rejected as a completion", l)
		}
	}
}

// The wrapper's own output line after a worker exits still completes a
// restarted session once the stamp is old.
func TestRestartMidTask_WrapperOutputLineStillCompletes(t *testing.T) {
	pane := "> [Pasted text #1 +24 lines]\n" + mtToolOutput + "DATAWATCH_COMPLETE: claude done\n"
	final, ended, _ := mtRun(t, mtOpts{pane: pane, age: 20 * time.Minute, observe: 6 * time.Second})
	if final.State != StateComplete || ended == 0 {
		t.Errorf("wrapper completion not detected: state=%q ended=%d", final.State, ended)
	}
}

// The startup one-shot reaper must not end an ad-hoc live claude-code worker
// whose pane only shows the wrapped launch command or the instruction.
func TestRestartMidTask_ReaperLeavesLiveAdhocWorker(t *testing.T) {
	panes := map[string]string{
		"launch_tail": mtLaunchWrappedAtMarker() + "\n> [Pasted text #1 +24 lines]\n" + mtStatus + mtBoxIdle,
		"placeholder": "> Implement the widget.\n  Finish with a line:\n" +
			"DATAWATCH_COMPLETE: <one-line plain-text summary of what was done>\n" + mtStatus + mtBoxIdle,
	}
	for name, pane := range panes {
		t.Run(name, func(t *testing.T) {
			mgr, fake := newTestManagerWithFake(t)
			sess := &Session{
				ID: "adhoc1", FullID: "testhost-adhoc1", Hostname: "testhost",
				State: StateRunning, OneShot: true, BackendFamily: "claude-code",
				TmuxSession: "cs-adhoc1", ProjectDir: t.TempDir(), Task: "worker task",
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			}
			if err := mgr.store.Save(sess); err != nil {
				t.Fatal(err)
			}
			_ = fake.NewSessionWithSize("cs-adhoc1", 80, 24)
			fake.mu.Lock()
			fake.Pane["cs-adhoc1"] = pane
			fake.mu.Unlock()
			if got := mgr.ReapFinishedOneShots(); len(got) != 0 {
				t.Errorf("live ad-hoc worker reaped: %v", got)
			}
			if cur, _ := mgr.store.Get(sess.FullID); cur.State != StateRunning {
				t.Errorf("state = %q, want running", cur.State)
			}
			if !fake.SessionExists("cs-adhoc1") {
				t.Error("pane was closed")
			}
		})
	}
}
