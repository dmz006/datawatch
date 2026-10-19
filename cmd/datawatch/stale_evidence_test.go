package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeAgedFile creates path with content and sets its mtime to age before now.
func writeAgedFile(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-age)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

// noSignal is a childAlive/backendBusy stand-in that never reports activity.
func noSignal() bool { return false }

func TestWorkerActivityLiveChildNotStalled(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "output.log")
	writeAgedFile(t, logPath, time.Hour)
	workDir := t.TempDir()

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
	})

	childAlive := func() bool {
		select {
		case <-done:
			return false
		default:
			return true
		}
	}

	time.Sleep(2 * time.Second)
	stalled, reason := workerActivityEvidence(logPath, workDir, childAlive, noSignal, time.Second, time.Now())
	if stalled {
		t.Fatalf("live child must not be stalled, got reason %q", reason)
	}
}

func TestWorkerActivityNoSignalsIsStalled(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "output.log")
	writeAgedFile(t, logPath, time.Hour)
	workDir := t.TempDir()

	stalled, reason := workerActivityEvidence(logPath, workDir, noSignal, noSignal, 10*time.Minute, time.Now())
	if !stalled {
		t.Fatal("no output, children, busy backend or file changes must be stalled")
	}
	if !strings.Contains(reason, "no output, no children, no file changes for") {
		t.Fatalf("reason = %q", reason)
	}
	if !strings.Contains(reason, "3600 seconds") {
		t.Fatalf("reason should report whole idle seconds of the old log, got %q", reason)
	}
}

func TestWorkerActivityRecentFileChangeNotStalled(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "output.log")
	writeAgedFile(t, logPath, time.Hour)
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	stalled, reason := workerActivityEvidence(logPath, workDir, noSignal, noSignal, 10*time.Minute, time.Now())
	if stalled {
		t.Fatalf("fresh file in workDir must not be stalled, got reason %q", reason)
	}
}

func TestWorkerActivityGitDirIgnored(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "output.log")
	writeAgedFile(t, logPath, time.Hour)
	workDir := t.TempDir()
	writeAgedFile(t, filepath.Join(workDir, "tracked.go"), time.Hour)
	// A fresh file inside .git (index, refs, lock files) is not worker activity.
	if err := os.MkdirAll(filepath.Join(workDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, ".git", "index"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	stalled, reason := workerActivityEvidence(logPath, workDir, noSignal, noSignal, 10*time.Minute, time.Now())
	if !stalled {
		t.Fatal("a file inside .git must be ignored, so the worker is stalled")
	}
	if !strings.Contains(reason, "no file changes for") {
		t.Fatalf("reason = %q", reason)
	}
}

func TestWorkerActivityBusyBackendNotStalled(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "output.log")
	writeAgedFile(t, logPath, time.Hour)
	busy := func() bool { return true }

	stalled, reason := workerActivityEvidence(logPath, t.TempDir(), noSignal, busy, 10*time.Minute, time.Now())
	if stalled {
		t.Fatalf("busy backend must not be stalled, got reason %q", reason)
	}
}

func TestWorkerActivityFreshLogNotStalled(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "output.log")
	if err := os.WriteFile(logPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	stalled, reason := workerActivityEvidence(logPath, t.TempDir(), noSignal, noSignal, 10*time.Minute, time.Now())
	if stalled {
		t.Fatalf("fresh log must not be stalled, got reason %q", reason)
	}
}

func TestWorkerActivityNilCallbacksTreatedAsIdle(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "output.log")
	writeAgedFile(t, logPath, time.Hour)

	stalled, reason := workerActivityEvidence(logPath, "", nil, nil, 10*time.Minute, time.Now())
	if !stalled {
		t.Fatal("nil callbacks must count as no children and not busy")
	}
	if !strings.Contains(reason, "3600 seconds") {
		t.Fatalf("reason = %q", reason)
	}
}

func TestWorkerActivityNoTimestampsUsesThreshold(t *testing.T) {
	stalled, reason := workerActivityEvidence("", "", noSignal, noSignal, 10*time.Minute, time.Now())
	if !stalled {
		t.Fatal("no log and no workDir with no activity must be stalled")
	}
	if !strings.Contains(reason, "600 seconds") {
		t.Fatalf("reason should fall back to the threshold, got %q", reason)
	}
}

func TestWorkerActivityThresholdZeroDisabled(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "output.log")
	writeAgedFile(t, logPath, 2*time.Hour)

	stalled, reason := workerActivityEvidence(logPath, "", noSignal, noSignal, 0, time.Now())
	if stalled || reason != "" {
		t.Fatalf("threshold 0 must never stall, got stalled=%v reason=%q", stalled, reason)
	}
}

func TestWorkerActivityWalkCapIgnoresLateFiles(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "output.log")
	writeAgedFile(t, logPath, time.Hour)
	workDir := t.TempDir()

	// Fill the walk budget with stale files that sort before the fresh one, so
	// the fresh file lies beyond staleEvidenceMaxEntries and is never inspected.
	for i := 0; i < staleEvidenceMaxEntries; i++ {
		writeAgedFile(t, filepath.Join(workDir, fmt.Sprintf("a%05d", i)), time.Hour)
	}
	if err := os.WriteFile(filepath.Join(workDir, "zz_fresh"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	stalled, reason := workerActivityEvidence(logPath, workDir, noSignal, noSignal, 10*time.Minute, time.Now())
	if !stalled {
		t.Fatalf("file beyond the walk cap must not count, got reason %q", reason)
	}
}
