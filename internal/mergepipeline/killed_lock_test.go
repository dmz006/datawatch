//go:build unix

package mergepipeline

// A killed run does not keep the repository's merge lock once nothing of it is
// left running. These use the daemon's real, persisted lock store.

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// killedRun starts a run in a child process whose check runs command, which is
// expected to kill the child (the check shell's parent).
func killedRun(t *testing.T, s sandbox, lockDir, scratch, command string) {
	t.Helper()
	child := exec.Command(os.Args[0], "-test.run=^TestKilledRunHelper$", "-test.timeout=60s")
	child.Env = append(os.Environ(), "DW_KL_REPO="+s.dir, "DW_KL_LOCKS="+lockDir, "DW_KL_SCRATCH="+scratch,
		"DW_KL_CMD="+command, "TMPDIR="+t.TempDir())
	done := make(chan error, 1)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- child.Wait() }()
	select {
	case <-done:
	case <-time.After(40 * time.Second):
		_ = child.Process.Kill() // end-reason-ok: the test kills its own child process
		t.Fatal("the child run was not killed")
	}
}

func TestKilledRunHelper(t *testing.T) {
	dir := os.Getenv("DW_KL_REPO")
	if dir == "" {
		t.Skip("only run as a child of the killed-run tests")
	}
	locks, err := autonomous.NewMergeLocks(os.Getenv("DW_KL_LOCKS"))
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{Resolver: resolverWithRecord(t, noTagSteps(check("dies", os.Getenv("DW_KL_CMD")))), VersionFiles: testVersionFiles,
		Dir: dir, Project: "p", Ship: ExecShipRunner{}, Isolate: true, ScratchRoot: os.Getenv("DW_KL_SCRATCH"), Locks: locks}
	_, _ = r.Run(context.Background(), RunRequest{Title: "x", Branches: []string{"feature"}, AutomatonID: "a-killed"})
}

func otherAutomatonRun(t *testing.T, s sandbox, lockDir, scratch string) (RunResult, error) {
	t.Helper()
	locks, err := autonomous.NewMergeLocks(lockDir)
	if err != nil {
		t.Fatal(err)
	}
	r := s.runner(t, noTagSteps())
	r.Locks, r.ScratchRoot = locks, scratch
	return r.Run(context.Background(), RunRequest{Title: "x", Branches: []string{"other"}, AutomatonID: "a-other"})
}

func TestKilledRunDoesNotKeepTheMergeLock(t *testing.T) {
	s := newSandbox(t)
	runGit(t, s.dir, "branch", "other", "feature")
	lockDir, scratch := t.TempDir(), t.TempDir()
	killedRun(t, s, lockDir, scratch, "kill -9 $PPID")
	locks, err := autonomous.NewMergeLocks(lockDir)
	if err != nil {
		t.Fatal(err)
	}
	if held, ok := locks.Status(s.dir); !ok || held.AutomatonID != "a-killed" {
		t.Fatalf("fixture: the killed run should have left its lock: %+v", locks.List())
	}
	if res, err := otherAutomatonRun(t, s, lockDir, scratch); err != nil || !res.Pushed {
		t.Fatalf("the next run: %v %q %+v", err, res.Run.Reason, res.Run.Steps)
	}
}

// While something the killed run started is still working in its scratch tree,
// the old run is not proven gone and the lock stays.
func TestKilledRunsLiveCheckKeepsTheMergeLock(t *testing.T) {
	s := newSandbox(t)
	runGit(t, s.dir, "branch", "other", "feature")
	lockDir, scratch := t.TempDir(), t.TempDir()
	pidFile := s.note("pid")
	killedRun(t, s, lockDir, scratch, "sleep 60 >/dev/null 2>&1 & echo $! > '"+pidFile+"'; kill -9 $PPID; wait")
	pid, err := strconv.Atoi(s.read("pid"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) }) // end-reason-ok: the test's own sleep
	res, err := otherAutomatonRun(t, s, lockDir, scratch)
	if err == nil || res.Pushed || !strings.Contains(res.Run.Reason, "still running") {
		t.Fatalf("err=%v reason=%q, want a refusal because the old run's check still runs", err, res.Run.Reason)
	}
	// The same Automaton may not take it back over a live check either.
	locks, err := autonomous.NewMergeLocks(lockDir)
	if err != nil {
		t.Fatal(err)
	}
	same := s.runner(t, noTagSteps())
	same.Locks, same.ScratchRoot = locks, scratch
	got, err := same.Run(context.Background(), RunRequest{Title: "x", Branches: []string{"feature"}, AutomatonID: "a-killed"})
	if err == nil || got.Pushed || !strings.Contains(got.Run.Reason, "still running") {
		t.Fatalf("the same Automaton: err=%v reason=%q, want the same refusal", err, got.Run.Reason)
	}
	// Stop everything left in the old run's scratch folder, as it would end.
	_ = exec.Command("sh", "-c", "for p in /proc/[0-9]*; do case \"$(readlink $p/cwd)\" in '"+scratch+"'*) kill -9 ${p#/proc/};; esac; done").Run()
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err = otherAutomatonRun(t, s, lockDir, scratch)
		if err == nil && res.Pushed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after the old run ended: %v %q", err, res.Run.Reason)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
