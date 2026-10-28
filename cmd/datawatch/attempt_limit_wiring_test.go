package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildAttemptObservation drives the loop-wiring seam that assembles one
// attempt observation from signal providers, with injected stand-ins so no
// real file stat, git repository or tmux session is needed.
func TestBuildAttemptObservation(t *testing.T) {
	var logCalls, fpCalls, cpuCalls int
	logSizeOf := func(string) int64 { logCalls++; return 1234 }
	fpAndHead := func(string) (string, string) { fpCalls++; return "fp-x", "sha-y" }
	cpuTicks := func(string) uint64 { cpuCalls++; return 42 }

	obs := buildAttemptObservation(
		limitWatchBase, logSizeOf, fpAndHead, cpuTicks,
		"/tmp/does-not-matter.log", "/tmp/does-not-matter.dir", "sess-1")

	if !obs.Now.Equal(limitWatchBase) {
		t.Fatalf("Now = %v, want %v", obs.Now, limitWatchBase)
	}
	if obs.LogSize != 1234 {
		t.Errorf("LogSize = %d, want 1234", obs.LogSize)
	}
	if obs.WorkFingerprint != "fp-x" || obs.HeadSHA != "sha-y" {
		t.Errorf("WorkFingerprint/HeadSHA = (%q, %q), want (\"fp-x\", \"sha-y\")",
			obs.WorkFingerprint, obs.HeadSHA)
	}
	if obs.ChildCPUTicks != 42 {
		t.Errorf("ChildCPUTicks = %d, want 42", obs.ChildCPUTicks)
	}
	if logCalls != 1 || fpCalls != 1 || cpuCalls != 1 {
		t.Errorf("provider calls = %d/%d/%d, want 1/1/1", logCalls, fpCalls, cpuCalls)
	}
}

// TestBuildAttemptObservationSkipsUnusedSignals asserts providers are only
// called for signals their caller actually asked for — empty inputs must not
// trigger observation work (and must not panic on guard providers).
func TestBuildAttemptObservationSkipsUnusedSignals(t *testing.T) {
	panicIf := func(name string) func(string) int64 {
		return func(string) int64 { t.Fatalf("%s provider must not be called", name); return 0 }
	}
	obs := buildAttemptObservation(
		limitWatchBase,
		panicIf("logsize"),
		func(string) (string, string) { t.Fatal("workdir provider must not be called"); return "", "" },
		func(string) uint64 { t.Fatal("cpu provider must not be called"); return 0 },
		"", "", "")
	if obs != (attemptObservation{Now: limitWatchBase}) {
		t.Errorf("obs = %+v, want zero-valued except Now", obs)
	}
}

// TestAttemptLogSizeOf checks the default log-size provider against a real
// temp file and a missing path.
func TestAttemptLogSizeOf(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session.log")
	if err := os.WriteFile(p, []byte("1234567"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := attemptLogSizeOf(p); got != 7 {
		t.Errorf("attemptLogSizeOf(existing) = %d, want 7", got)
	}
	if got := attemptLogSizeOf(filepath.Join(dir, "missing")); got != 0 {
		t.Errorf("attemptLogSizeOf(missing) = %d, want 0", got)
	}
}
