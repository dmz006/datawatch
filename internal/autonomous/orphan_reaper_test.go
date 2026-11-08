package autonomous

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// startFixtureSleeper starts a harmless sleeping script in its own session
// inside dir. Nothing here touches tmux or any real LLM.
func startFixtureSleeper(t *testing.T, dir string, extraEnv ...string) *exec.Cmd {
	t.Helper()
	script := filepath.Join(dir, "sleeper.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile :; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Dir = dir
	env := []string{"PATH=" + os.Getenv("PATH")}
	cmd.Env = append(env, extraEnv...) // TMUX and TMUX_TMPDIR deliberately absent
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }() // reap so exit is observable
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // own session group only
		<-done
	})
	return cmd
}

func fastReaper() *OrphanReaper {
	return &OrphanReaper{Grace: 3 * time.Second, KillSettle: time.Second, Poll: 5 * time.Millisecond}
}

func pidAlive(pid int) bool {
	r := &OrphanReaper{}
	return !r.gone(pid)
}

func TestOrphanFinderMatchesByEvidenceNotName(t *testing.T) {
	t.Setenv("TMUX", "")
	os.Unsetenv("TMUX")
	os.Unsetenv("TMUX_TMPDIR")
	wt := t.TempDir()
	other := t.TempDir()
	mine := startFixtureSleeper(t, wt)
	same := startFixtureSleeper(t, other) // same name and script name, no evidence

	time.Sleep(100 * time.Millisecond)
	procs := fastReaper().Find(OrphanEvidence{Dirs: []string{wt}})
	found := map[int]OrphanProc{}
	for _, p := range procs {
		found[p.PID] = p
	}
	p, ok := found[mine.Process.Pid]
	if !ok {
		t.Fatalf("fixture in worktree not found: %+v", procs)
	}
	if p.Evidence == "" || p.StartTime == 0 || p.Command == "" {
		t.Fatalf("incomplete record: %+v", p)
	}
	if _, bad := found[same.Process.Pid]; bad {
		t.Fatalf("matched a same-named process with no evidence")
	}
}

func TestOrphanReapEndsFixtureAndLeavesBystander(t *testing.T) {
	os.Unsetenv("TMUX")
	os.Unsetenv("TMUX_TMPDIR")
	wt := t.TempDir()
	other := t.TempDir()
	mine := startFixtureSleeper(t, wt, "SECRET_TOKEN=hunter2-do-not-print")
	bystander := startFixtureSleeper(t, other)
	time.Sleep(100 * time.Millisecond)

	r := fastReaper()
	res := r.EndOrphans(OrphanEvidence{Dirs: []string{wt}})
	if res.Count < 1 {
		t.Fatalf("expected the fixture ended, got %+v", res)
	}
	if len(res.Survivors) != 0 {
		t.Fatalf("unexpected survivors: %+v", res.Survivors)
	}
	for _, e := range res.Ended {
		if strings.Contains(e.Command, "hunter2") || strings.Contains(e.Command, "SECRET_TOKEN") {
			t.Fatalf("environment leaked into command: %q", e.Command)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for pidAlive(mine.Process.Pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pidAlive(mine.Process.Pid) {
		t.Fatal("fixture still alive")
	}
	if !pidAlive(bystander.Process.Pid) {
		t.Fatal("bystander without evidence was ended")
	}
}

func TestOrphanFinderEnvEvidence(t *testing.T) {
	os.Unsetenv("TMUX")
	os.Unsetenv("TMUX_TMPDIR")
	other := t.TempDir()
	const id = "sess-evidence-abcd1234"
	withID := startFixtureSleeper(t, other, "DATAWATCH_SESSION_ID="+id)
	without := startFixtureSleeper(t, other)
	time.Sleep(100 * time.Millisecond)

	procs := fastReaper().Find(OrphanEvidence{SessionID: id})
	var gotWith, gotWithout bool
	for _, p := range procs {
		if p.PID == withID.Process.Pid {
			gotWith = p.Evidence == "env"
		}
		if p.PID == without.Process.Pid {
			gotWithout = true
		}
	}
	if !gotWith || gotWithout {
		t.Fatalf("env evidence wrong: with=%v without=%v", gotWith, gotWithout)
	}
}

// fakeProc builds a fake proc root so the PID-reuse and escalation logic can
// be driven deterministically without signalling real processes.
type fakeProc struct {
	root string
	mu   sync.Mutex
	sigs []string
	now  time.Time
}

func newFakeProc(t *testing.T) *fakeProc {
	return &fakeProc{root: t.TempDir(), now: time.Unix(1000, 0)}
}

func (f *fakeProc) put(t *testing.T, pid, ppid int, start uint64, cwd string) {
	t.Helper()
	d := filepath.Join(f.root, fmt.Sprint(pid))
	_ = os.RemoveAll(d)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	stat := fmt.Sprintf("%d (sleeper (x)) S %d %s%d\n", pid, ppid, strings.Repeat("0 ", 17), start)
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(d, "stat"), []byte(stat), 0o644))
	must(os.WriteFile(filepath.Join(d, "comm"), []byte("sleeper"), 0o644))
	must(os.WriteFile(filepath.Join(d, "cmdline"), []byte("sleeper\x00arg1\x00arg2\x00arg3\x00"), 0o644))
	if cwd != "" {
		must(os.Symlink(cwd, filepath.Join(d, "cwd")))
	}
}

func (f *fakeProc) reaper(onSig func(pid int, sig syscall.Signal)) *OrphanReaper {
	return &OrphanReaper{
		ProcRoot: f.root,
		SelfPID:  -1,
		Grace:    time.Second, KillSettle: time.Second, Poll: 100 * time.Millisecond,
		Now:   func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now },
		Sleep: func(d time.Duration) { f.mu.Lock(); f.now = f.now.Add(d); f.mu.Unlock() },
		Signal: func(pid int, sig syscall.Signal) error {
			f.mu.Lock()
			f.sigs = append(f.sigs, fmt.Sprintf("%d:%d", pid, sig))
			f.mu.Unlock()
			if onSig != nil {
				onSig(pid, sig)
			}
			return nil
		},
	}
}

func (f *fakeProc) die(pid int) { _ = os.RemoveAll(filepath.Join(f.root, fmt.Sprint(pid))) }

func TestOrphanReapPIDReuseGuardStartTime(t *testing.T) {
	wt := t.TempDir()
	f := newFakeProc(t)
	f.put(t, 4242, 1, 1000, wt)
	r := f.reaper(nil)
	ev := OrphanEvidence{Dirs: []string{wt}}
	procs := r.Find(ev)
	if len(procs) != 1 || procs[0].PID != 4242 || procs[0].StartTime != 1000 {
		t.Fatalf("find: %+v", procs)
	}
	// The pid is reused by a new process in the same place: same cwd evidence,
	// different start time. It must not be signalled.
	f.put(t, 4242, 1, 2000, wt)
	res := r.Reap(ev, procs)
	if len(f.sigs) != 0 {
		t.Fatalf("signalled a reused pid: %v", f.sigs)
	}
	if res.Count != 0 || len(res.Survivors) != 1 {
		t.Fatalf("result: %+v", res)
	}
}

func TestOrphanReapPIDReuseGuardEvidenceGone(t *testing.T) {
	wt := t.TempDir()
	f := newFakeProc(t)
	f.put(t, 777, 1, 50, wt)
	r := f.reaper(nil)
	ev := OrphanEvidence{Dirs: []string{wt}}
	procs := r.Find(ev)
	f.put(t, 777, 1, 50, t.TempDir()) // same start time but evidence moved away
	res := r.Reap(ev, procs)
	if len(f.sigs) != 0 || len(res.Survivors) != 1 {
		t.Fatalf("signalled without evidence: %v %+v", f.sigs, res)
	}
}

func TestOrphanReapGuardReverifiesBeforeForcedSignal(t *testing.T) {
	wt := t.TempDir()
	f := newFakeProc(t)
	f.put(t, 900, 1, 10, wt)
	// The process ignores the polite signal and, while we wait, the pid is
	// reused by an unrelated process: no SIGKILL may be sent.
	r := f.reaper(func(pid int, sig syscall.Signal) {
		if sig == syscall.SIGTERM {
			f.put(t, 900, 1, 11, t.TempDir())
		}
	})
	ev := OrphanEvidence{Dirs: []string{wt}}
	res := r.Reap(ev, r.Find(ev))
	if len(f.sigs) != 1 || f.sigs[0] != fmt.Sprintf("900:%d", syscall.SIGTERM) {
		t.Fatalf("signals: %v", f.sigs)
	}
	if res.Count != 0 || len(res.Survivors) != 1 {
		t.Fatalf("result: %+v", res)
	}
}

func TestOrphanReapEscalatesPoliteThenForced(t *testing.T) {
	wt := t.TempDir()
	f := newFakeProc(t)
	f.put(t, 31, 1, 5, wt)
	r := f.reaper(func(pid int, sig syscall.Signal) {
		if sig == syscall.SIGKILL {
			f.die(pid)
		}
	})
	ev := OrphanEvidence{Dirs: []string{wt}}
	res := r.Reap(ev, r.Find(ev))
	want := []string{fmt.Sprintf("31:%d", syscall.SIGTERM), fmt.Sprintf("31:%d", syscall.SIGKILL)}
	if strings.Join(f.sigs, ",") != strings.Join(want, ",") {
		t.Fatalf("signals %v want %v", f.sigs, want)
	}
	if res.Count != 1 || res.Ended[0].PID != 31 {
		t.Fatalf("result: %+v", res)
	}
	if res.Ended[0].Command != "sleeper arg1 arg2" {
		t.Fatalf("command: %q", res.Ended[0].Command)
	}
}

func TestOrphanFinderAncestryEvidence(t *testing.T) {
	f := newFakeProc(t)
	f.put(t, 100, 1, 1, "")   // session root
	f.put(t, 101, 100, 2, "") // child
	f.put(t, 102, 101, 3, "") // grandchild
	f.put(t, 200, 1, 4, "")   // unrelated
	r := f.reaper(nil)
	got := map[int]bool{}
	for _, p := range r.Find(OrphanEvidence{RootPIDs: []int{100}}) {
		if p.Evidence != "ancestry" {
			t.Fatalf("evidence %q", p.Evidence)
		}
		got[p.PID] = true
	}
	if !got[101] || !got[102] || got[200] {
		t.Fatalf("ancestry result: %v", got)
	}
}

func TestOrphanFinderNoEvidenceNoMatch(t *testing.T) {
	f := newFakeProc(t)
	f.put(t, 10, 1, 1, t.TempDir())
	if got := f.reaper(nil).Find(OrphanEvidence{}); len(got) != 0 {
		t.Fatalf("matched with empty evidence: %+v", got)
	}
	// The filesystem root must never act as a session folder.
	if got := f.reaper(nil).Find(OrphanEvidence{Dirs: []string{"/"}}); len(got) != 0 {
		t.Fatalf("root dir matched: %+v", got)
	}
}
