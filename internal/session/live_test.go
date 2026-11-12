package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestClassifyLifeTable(t *testing.T) {
	cases := []struct {
		name  string
		facts LifeFacts
		want  Life
	}{
		{"nothing", LifeFacts{}, LifeGone},
		{"terminal", LifeFacts{TerminalExists: true}, LifeLive},
		{"launch in flight", LifeFacts{LaunchInFlight: true}, LifeLive},
		{"runner held", LifeFacts{RunnerHeld: true}, LifeLive},
		{"subprocess alive", LifeFacts{SubprocessAlive: true}, LifeLive},
		{"daemon owned", LifeFacts{DaemonOwned: true}, LifeLive},
		{"final state", LifeFacts{FinalState: true}, LifeEnded},
		{"final state beats every live fact", LifeFacts{FinalState: true, TerminalExists: true, RunnerHeld: true, DaemonOwned: true}, LifeEnded},
		{"final state beats foreign", LifeFacts{FinalState: true, ForeignHost: true}, LifeEnded},
		{"foreign", LifeFacts{ForeignHost: true}, LifeForeign},
		{"foreign beats live facts", LifeFacts{ForeignHost: true, TerminalExists: true, LaunchInFlight: true, RunnerHeld: true, SubprocessAlive: true, DaemonOwned: true}, LifeForeign},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyLife(tc.facts); got != tc.want {
				t.Fatalf("ClassifyLife(%+v) = %q, want %q", tc.facts, got, tc.want)
			}
		})
	}
}

func TestLifeOfTerminalSessionIsLive(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	if err := fake.NewSessionWithSize("cs-lv01", 80, 24); err != nil {
		t.Fatal(err)
	}
	sess := &Session{ID: "lv01", FullID: "testhost-lv01", Hostname: "testhost", TmuxSession: "cs-lv01", State: StateRunning}

	terminals, ok := mgr.TerminalSnapshot()
	if !ok {
		t.Fatal("TerminalSnapshot must succeed against the fake")
	}
	if !terminals["cs-lv01"] {
		t.Fatalf("snapshot missing the fake terminal: %v", terminals)
	}
	if got := mgr.LifeOf(sess, terminals); got != LifeLive {
		t.Fatalf("LifeOf = %q, want %q", got, LifeLive)
	}
}

func TestLifeOfNoTerminalIsGone(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	sess := &Session{ID: "gn01", FullID: "testhost-gn01", Hostname: "testhost", TmuxSession: "cs-gn01", State: StateRunning, LogFile: filepath.Join(t.TempDir(), "output.log")}

	terminals, ok := mgr.TerminalSnapshot()
	if !ok {
		t.Fatal("TerminalSnapshot must succeed against the fake")
	}
	if got := mgr.LifeOf(sess, terminals); got != LifeGone {
		t.Fatalf("LifeOf = %q, want %q", got, LifeGone)
	}
	// A record with no terminal name at all is judged the same way.
	sess.TmuxSession = ""
	if got := mgr.LifeOf(sess, terminals); got != LifeGone {
		t.Fatalf("LifeOf (no terminal name) = %q, want %q", got, LifeGone)
	}
}

func TestLifeOfChatModeRunnerIsLive(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	sess := &Session{ID: "ch01", FullID: "testhost-ch01", Hostname: "testhost", OutputMode: "chat", State: StateRunning}

	terminals, _ := mgr.TerminalSnapshot()
	if got := mgr.LifeOf(sess, terminals); got != LifeGone {
		t.Fatalf("before the runner is registered LifeOf = %q, want %q", got, LifeGone)
	}
	mgr.RegisterRunner(sess.FullID)
	if got := mgr.LifeOf(sess, terminals); got != LifeLive {
		t.Fatalf("with a registered runner LifeOf = %q, want %q", got, LifeLive)
	}
	mgr.ReleaseRunner(sess.FullID)
	if got := mgr.LifeOf(sess, terminals); got != LifeGone {
		t.Fatalf("after the runner is released LifeOf = %q, want %q", got, LifeGone)
	}
}

func TestLifeOfForeignHostIsNotJudged(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	if err := fake.NewSessionWithSize("cs-fr01", 80, 24); err != nil {
		t.Fatal(err)
	}
	terminals, _ := mgr.TerminalSnapshot()

	// Another host's record with nothing running here is not gone.
	gone := &Session{ID: "fr01", FullID: "otherhost-fr01", Hostname: "otherhost", TmuxSession: "cs-none", State: StateRunning}
	if got := mgr.LifeOf(gone, terminals); got != LifeForeign {
		t.Fatalf("LifeOf (foreign, no terminal) = %q, want %q", got, LifeForeign)
	}
	// Even a name that happens to match a local terminal does not make it live.
	clash := &Session{ID: "fr02", FullID: "otherhost-fr02", Hostname: "otherhost", TmuxSession: "cs-fr01", State: StateRunning}
	if got := mgr.LifeOf(clash, terminals); got != LifeForeign {
		t.Fatalf("LifeOf (foreign, matching name) = %q, want %q", got, LifeForeign)
	}
}

func TestLifeOfFinalStateIsEnded(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	if err := fake.NewSessionWithSize("cs-en01", 80, 24); err != nil {
		t.Fatal(err)
	}
	terminals, _ := mgr.TerminalSnapshot()
	for _, st := range []State{StateComplete, StateFailed, StateKilled} {
		sess := &Session{ID: "en01", FullID: "testhost-en01", Hostname: "testhost", TmuxSession: "cs-en01", State: st}
		if got := mgr.LifeOf(sess, terminals); got != LifeEnded {
			t.Fatalf("LifeOf state %q = %q, want %q", st, got, LifeEnded)
		}
	}
}

func TestLifeOfLaunchInFlightIsLive(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	sess := &Session{ID: "la01", FullID: "testhost-la01", Hostname: "testhost", TmuxSession: "cs-la01", State: StateRunning}
	terminals, _ := mgr.TerminalSnapshot()

	if got := mgr.LifeOf(sess, terminals); got != LifeGone {
		t.Fatalf("before the launch registers LifeOf = %q, want %q", got, LifeGone)
	}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mgr.registerLaunch(sess, cancel); err != nil {
		t.Fatalf("registerLaunch: %v", err)
	}
	if got := mgr.LifeOf(sess, terminals); got != LifeLive {
		t.Fatalf("with a launch in flight LifeOf = %q, want %q", got, LifeLive)
	}
	mgr.deregisterLaunch(sess.FullID)
	if got := mgr.LifeOf(sess, terminals); got != LifeGone {
		t.Fatalf("after the launch returns LifeOf = %q, want %q", got, LifeGone)
	}
}

func TestLifeOfSubprocessWithLivePIDIsLive(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not available: %v", err)
	}
	if _, err := os.Stat("/proc/self/cmdline"); err != nil {
		t.Skipf("no /proc: %v", err)
	}
	mgr, _ := newTestManagerWithFake(t)
	dir := t.TempDir()
	// Two commands, so bash keeps the full command line instead of replacing
	// itself with the last command.
	task := fmt.Sprintf("sleep 5; true # live-judgement-%d", time.Now().UnixNano())
	cmd := exec.Command("bash", "-c", task)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Right after Start the child's command line is still empty until its
	// exec finishes, so wait (bounded) for it to show up.
	deadline := time.Now().Add(5 * time.Second)
	for !subprocessAlive(cmd.Process.Pid, task) {
		if time.Now().After(deadline) {
			t.Fatal("child command line never appeared in /proc")
		}
		time.Sleep(10 * time.Millisecond)
	}

	sess := &Session{ID: "sp01", FullID: "testhost-sp01", Hostname: "testhost", State: StateRunning, Task: task, LogFile: filepath.Join(dir, "output.log")}
	terminals, _ := mgr.TerminalSnapshot()

	// No recorded pid: nothing drives the session.
	if got := mgr.LifeOf(sess, terminals); got != LifeGone {
		t.Fatalf("without a recorded pid LifeOf = %q, want %q", got, LifeGone)
	}
	if err := os.WriteFile(subprocessFile(sess, subprocessPIDFile), []byte(fmt.Sprint(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mgr.LifeOf(sess, terminals); got != LifeLive {
		t.Fatalf("with a live pid LifeOf = %q, want %q", got, LifeLive)
	}
	// A pid that runs some other command is not this task.
	sess.Task = "a different task entirely"
	if got := mgr.LifeOf(sess, terminals); got != LifeGone {
		t.Fatalf("with a pid running another command LifeOf = %q, want %q", got, LifeGone)
	}
}

func TestTerminalSnapshotListingErrors(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	mgr.tmux = &errListTmux{FakeTmux: NewFakeTmux(), err: errors.New("tmux list-sessions: permission denied")}
	if set, ok := mgr.TerminalSnapshot(); ok || set != nil {
		t.Fatalf("a broken listing must give ok=false, got %v ok=%v", set, ok)
	}
	mgr.tmux = &errListTmux{FakeTmux: NewFakeTmux(), err: errors.New("no server running on /tmp/tmux-1000/default")}
	set, ok := mgr.TerminalSnapshot()
	if !ok || len(set) != 0 {
		t.Fatalf("the plain no-server answer must be an empty snapshot, got %v ok=%v", set, ok)
	}
}

// errListTmux is a FakeTmux whose listing fails with a fixed error.
type errListTmux struct {
	*FakeTmux
	err error
}

func (e *errListTmux) ListSessions(string) ([]string, error) { return nil, e.err }
