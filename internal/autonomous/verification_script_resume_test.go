package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeVerifyRunSeams swaps the record reader and the process-liveness check
// for one test and restores them on cleanup. The poll interval is shortened
// so a waiter returns quickly.
func fakeVerifyRunSeams(t *testing.T, read func(dir string) (verifyRunRecord, error), live func(pid int, started time.Time) bool) {
	t.Helper()
	oldRead, oldLive, oldPoll := verifyRecordReadFn, verifyPIDLiveFn, verifyWaitPollInterval
	verifyRecordReadFn = read
	verifyPIDLiveFn = live
	verifyWaitPollInterval = time.Millisecond
	t.Cleanup(func() {
		verifyRecordReadFn, verifyPIDLiveFn, verifyWaitPollInterval = oldRead, oldLive, oldPoll
	})
}

// markerCount returns how many lines the marker file holds; a missing file is
// zero. Each script run appends one line, so this counts real executions.
func markerCount(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

func exitCodePtr(v int) *int { return &v }

func TestVerificationScriptResumeAliveRecordIsWaitedOnNotRerun(t *testing.T) {
	root := useRunRoot(t)
	dir := t.TempDir()
	writeScript(t, dir, "gate.sh", "echo ran >> marker.txt\necho should-not-run\n")
	script := filepath.Join(dir, "gate.sh")
	runDir := verifyRunDirFor(root, script)
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, verifyOutputFile), []byte("from-live-run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	reads := 0
	read := func(d string) (verifyRunRecord, error) {
		if d != runDir {
			return verifyRunRecord{}, os.ErrNotExist
		}
		reads++
		rec := verifyRunRecord{ScriptPath: script, PID: 4242, StartedAt: started}
		if reads >= 3 {
			rec.ExitCode = exitCodePtr(0) // the live run ends after a few polls
		}
		return rec, nil
	}
	live := func(pid int, at time.Time) bool {
		return pid == 4242 && at.Equal(started) && reads < 3
	}
	fakeVerifyRunSeams(t, read, live)

	out, code, err := runVerificationScript("gate.sh", dir)
	if err != nil || code != 0 || !strings.Contains(out, "from-live-run") {
		t.Fatalf("out=%q code=%d err=%v; want the live run's output and exit 0", out, code, err)
	}
	if reads < 3 {
		t.Fatalf("record read %d times; the waiter must poll until the run ends", reads)
	}
	if n := markerCount(t, filepath.Join(dir, "marker.txt")); n != 0 {
		t.Fatalf("script ran %d times; an alive record must be waited on, not re-run", n)
	}
}

func TestVerificationScriptResumeFinishedRecordIsConsumedWithoutRun(t *testing.T) {
	root := useRunRoot(t)
	dir := t.TempDir()
	writeScript(t, dir, "gate.sh", "echo ran >> marker.txt\necho should-not-run\n")
	script := filepath.Join(dir, "gate.sh")
	runDir := verifyRunDirFor(root, script)
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, verifyOutputFile), []byte("from-record\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeVerifyRunSeams(t,
		func(d string) (verifyRunRecord, error) {
			return verifyRunRecord{ScriptPath: script, PID: 4242, StartedAt: time.Now(), ExitCode: exitCodePtr(5)}, nil
		},
		func(int, time.Time) bool { return false },
	)

	out, code, err := runVerificationScript("gate.sh", dir)
	if err != nil || code != 5 || !strings.Contains(out, "from-record") {
		t.Fatalf("out=%q code=%d err=%v; want the recorded output and exit 5", out, code, err)
	}
	if n := markerCount(t, filepath.Join(dir, "marker.txt")); n != 0 {
		t.Fatalf("script ran %d times; a finished record must be consumed with no run", n)
	}
}

func TestVerificationScriptResumeConsumedOnceThenRunsFresh(t *testing.T) {
	root := useRunRoot(t)
	dir := t.TempDir()
	writeScript(t, dir, "gate.sh", "echo ran >> marker.txt\necho fresh-run\n")
	script := filepath.Join(dir, "gate.sh")
	runDir := verifyRunDirFor(root, script)
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, verifyOutputFile), []byte("recorded-result\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An undelivered finished record, as left by a daemon that died after the
	// script ended.
	if err := writeVerifyRecord(runDir, &verifyRunRecord{ScriptPath: script, PID: 4242, StartedAt: time.Now(), ExitCode: exitCodePtr(5)}); err != nil {
		t.Fatal(err)
	}

	out, code, err := runVerificationScript("gate.sh", dir)
	if err != nil || code != 5 || !strings.Contains(out, "recorded-result") {
		t.Fatalf("first call: out=%q code=%d err=%v; want the recorded result", out, code, err)
	}
	if n := markerCount(t, filepath.Join(dir, "marker.txt")); n != 0 {
		t.Fatalf("first call ran the script %d times", n)
	}
	rec, _ := readOnlyRecord(t, root)
	if !rec.Delivered {
		t.Fatalf("consumed record not marked delivered: %+v", rec)
	}

	out, code, err = runVerificationScript("gate.sh", dir)
	if err != nil || code != 0 || !strings.Contains(out, "fresh-run") {
		t.Fatalf("second call: out=%q code=%d err=%v; a delivered record must not be consumed again", out, code, err)
	}
	if n := markerCount(t, filepath.Join(dir, "marker.txt")); n != 1 {
		t.Fatalf("second call ran the script %d times, want 1", n)
	}
}

func TestVerificationScriptResumeStalePIDIsRerunOnce(t *testing.T) {
	useRunRoot(t)
	dir := t.TempDir()
	writeScript(t, dir, "gate.sh", "echo ran >> marker.txt\necho fresh-run\n")
	script := filepath.Join(dir, "gate.sh")
	fakeVerifyRunSeams(t,
		func(string) (verifyRunRecord, error) {
			return verifyRunRecord{ScriptPath: script, PID: 4242, StartedAt: time.Now()}, nil
		},
		func(int, time.Time) bool { return false }, // the recorded pid is gone
	)

	out, code, err := runVerificationScript("gate.sh", dir)
	if err != nil || code != 0 || !strings.Contains(out, "fresh-run") {
		t.Fatalf("out=%q code=%d err=%v; want a fresh run of the script", out, code, err)
	}
	if n := markerCount(t, filepath.Join(dir, "marker.txt")); n != 1 {
		t.Fatalf("script ran %d times after a stale pid, want exactly one re-run", n)
	}
}

func TestVerificationScriptResumeMissingRecordRunsFromStart(t *testing.T) {
	useRunRoot(t)
	dir := t.TempDir()
	writeScript(t, dir, "gate.sh", "echo ran >> marker.txt\necho fresh-run\n")
	fakeVerifyRunSeams(t,
		func(string) (verifyRunRecord, error) { return verifyRunRecord{}, os.ErrNotExist },
		func(int, time.Time) bool { return true },
	)

	out, code, err := runVerificationScript("gate.sh", dir)
	if err != nil || code != 0 || !strings.Contains(out, "fresh-run") {
		t.Fatalf("out=%q code=%d err=%v; want a fresh run when no record exists", out, code, err)
	}
	if n := markerCount(t, filepath.Join(dir, "marker.txt")); n != 1 {
		t.Fatalf("script ran %d times with no record, want 1", n)
	}
}

func TestVerificationScriptWaitEndsWithoutExitCodeWhenProcessGone(t *testing.T) {
	root := useRunRoot(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "gate.sh")
	runDir := verifyRunDirFor(root, script)
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	calls := 0
	fakeVerifyRunSeams(t,
		func(string) (verifyRunRecord, error) {
			return verifyRunRecord{ScriptPath: script, PID: 4242, StartedAt: started}, nil
		},
		func(int, time.Time) bool { calls++; return calls == 1 }, // alive once, then gone
	)

	out, code, err := waitVerifyRun(runDir, verifyRunRecord{ScriptPath: script, PID: 4242, StartedAt: started})
	if err == nil || code != -1 {
		t.Fatalf("out=%q code=%d err=%v; a run that ends without an exit code is a failure", out, code, err)
	}
}

// Pid reuse: a pid is trusted only when its live start time is the recorded
// start time.
func TestVerificationScriptLivenessRejectsReusedPID(t *testing.T) {
	pid := os.Getpid()
	started, _, ok := procStartAndState(pid)
	if !ok {
		t.Skip("/proc start times unavailable on this host")
	}
	if !verifyPIDMatchesRecord(pid, started) {
		t.Fatalf("own pid %d with its own start time must be live", pid)
	}
	if verifyPIDMatchesRecord(pid, started.Add(-time.Hour)) {
		t.Fatal("a pid whose start time differs from the record must not be trusted (pid reuse)")
	}
	if verifyPIDMatchesRecord(0, started) || verifyPIDMatchesRecord(-1, started) {
		t.Fatal("a non-positive pid must never be trusted")
	}
	if verifyPIDMatchesRecord(pid, time.Time{}) {
		t.Fatal("a record with no start time must not be trusted")
	}
}

// End to end with real files: a record whose pid is alive but started at a
// different time is a reused pid, so the script runs again instead of being
// waited on.
func TestVerificationScriptPIDReuseIsRerunNotWaited(t *testing.T) {
	root := useRunRoot(t)
	dir := t.TempDir()
	writeScript(t, dir, "gate.sh", "echo ran >> marker.txt\necho fresh-run\n")
	script := filepath.Join(dir, "gate.sh")
	runDir := verifyRunDirFor(root, script)
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	reused := verifyRunRecord{ScriptPath: script, PID: os.Getpid(), StartedAt: time.Now().Add(-time.Hour)}
	if err := writeVerifyRecord(runDir, &reused); err != nil {
		t.Fatal(err)
	}

	out, code, err := runVerificationScript("gate.sh", dir)
	if err != nil || code != 0 || !strings.Contains(out, "fresh-run") {
		t.Fatalf("out=%q code=%d err=%v; a reused pid must not be waited on", out, code, err)
	}
	if n := markerCount(t, filepath.Join(dir, "marker.txt")); n != 1 {
		t.Fatalf("script ran %d times, want 1", n)
	}
}

// Boot-resume: a verifying task with no verdict whose script run is alive is
// kept verifying, not reset; once the run is gone the task is reset.
func TestVerificationScriptBootHoldsTaskWithLiveRun(t *testing.T) {
	projDir := t.TempDir()
	script := filepath.Join(projDir, "gate.sh")
	writeScript(t, projDir, "gate.sh", "echo ran\n")
	started := time.Now()

	for _, tc := range []struct {
		name       string
		alive      bool
		wantStatus TaskStatus
	}{
		{name: "live run keeps task verifying", alive: true, wantStatus: TaskVerifying},
		{name: "gone run resets task", alive: false, wantStatus: TaskPending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, prd := verifyingBootManager(t, []Story{
				{Title: "story", Tasks: []Task{
					{Title: "task", Status: TaskVerifying, VerificationScript: "gate.sh"},
				}},
			})
			// NewManager points the global run root at its own scratch area, so
			// the test's root is installed after the manager exists.
			root := useRunRoot(t)
			prd.ProjectDir = projDir
			if err := m.Store().SavePRD(prd); err != nil {
				t.Fatal(err)
			}
			m.SetSessionAliveFn(func(string) bool { return false })
			runDir := verifyRunDirFor(root, script)
			fakeVerifyRunSeams(t,
				func(d string) (verifyRunRecord, error) {
					if d != runDir {
						return verifyRunRecord{}, os.ErrNotExist
					}
					return verifyRunRecord{ScriptPath: script, PID: 4242, StartedAt: started}, nil
				},
				func(pid int, at time.Time) bool { return tc.alive && pid == 4242 },
			)

			m.resetInProgressTasksForResume(prd)

			got, _ := m.Store().GetPRD(prd.ID)
			if st := got.Story[0].Tasks[0].Status; st != tc.wantStatus {
				t.Fatalf("status=%s, want %s", st, tc.wantStatus)
			}
		})
	}
}
