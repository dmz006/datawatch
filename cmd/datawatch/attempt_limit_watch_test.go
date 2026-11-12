package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// limitWatchBase is a fixed fake wall clock for the limit watcher tests —
// no test may call time.Now().
var limitWatchBase = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// limitWatchGitOut runs git in dir with a fixed identity and returns stdout.
func limitWatchGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestAttemptLimitWatchMaxAttemptStopsAndKillsOnce feeds the watcher one
// observation past the maximum attempt time and asserts it stops, preserves
// what it can, and kills the session exactly once.
func TestAttemptLimitWatchMaxAttemptStopsAndKillsOnce(t *testing.T) {
	var events []string
	sessions := &fakeStallSessions{events: &events}
	store := &fakeStallStore{events: &events}
	task := &autonomouspkg.Task{ID: "task-1", PRDID: "prd-1"}
	// workDir holds nothing git can snapshot: the (expected) snapshot
	// failure must not block the kill.
	w := newAttemptLimitWatcher(limitWatchBase, attemptObservation{Now: limitWatchBase},
		sessions, store, t.TempDir(), task, "sess-1")

	err := w.check(attemptObservation{Now: limitWatchBase.Add(70 * time.Minute)}, time.Hour, 3*time.Hour)

	if err == nil {
		t.Fatal("want a stop error after 70m with a 1h maximum attempt time, got nil")
	}
	if !errors.Is(err, autonomouspkg.ErrWorkerStalled) {
		t.Fatalf("error %q does not wrap ErrWorkerStalled", err)
	}
	kills := 0
	for _, ev := range events {
		if strings.HasPrefix(ev, "kill:") {
			kills++
		}
	}
	if kills != 1 {
		t.Fatalf("want exactly one kill, events = %v", events)
	}
}

// TestAttemptLimitWatchNoProgressStops asserts a worker that shows no output,
// no file changes, no commits and no CPU for longer than the no-progress
// limit is stopped.
func TestAttemptLimitWatchNoProgressStops(t *testing.T) {
	var events []string
	sessions := &fakeStallSessions{events: &events}
	store := &fakeStallStore{events: &events}
	task := &autonomouspkg.Task{ID: "task-1", PRDID: "prd-1"}
	w := newAttemptLimitWatcher(limitWatchBase, attemptObservation{Now: limitWatchBase},
		sessions, store, t.TempDir(), task, "sess-1")

	err := w.check(attemptObservation{Now: limitWatchBase.Add(2 * time.Hour)}, 0, time.Hour)

	if err == nil {
		t.Fatal("want a stop error after 2h idle with a 1h no-progress limit, got nil")
	}
	if !strings.Contains(err.Error(), "no progress for") {
		t.Fatalf("error %q should mention the no-progress reason", err)
	}
	if len(events) != 1 || events[0] != "kill:sess-1" {
		t.Fatalf("events = %v, want the kill", events)
	}
}

// TestAttemptLimitWatchFingerprintChangeResetsClock asserts a file
// fingerprint change is progress and resets the no-progress clock, so the
// worker is not stopped even though it was idle for days.
func TestAttemptLimitWatchFingerprintChangeResetsClock(t *testing.T) {
	var events []string
	sessions := &fakeStallSessions{events: &events}
	store := &fakeStallStore{events: &events}
	task := &autonomouspkg.Task{ID: "task-1", PRDID: "prd-1"}
	w := newAttemptLimitWatcher(limitWatchBase, attemptObservation{Now: limitWatchBase, WorkFingerprint: "fp-1"},
		sessions, store, t.TempDir(), task, "sess-1")

	err := w.check(attemptObservation{Now: limitWatchBase.Add(48 * time.Hour), WorkFingerprint: "fp-2"}, 0, time.Hour)

	if err != nil {
		t.Fatalf("fingerprint change is progress, want no stop, got %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("want no kill or snapshot, events = %v", events)
	}
}

// TestAttemptLimitWatchCPUIncreaseResetsClock asserts a child CPU tick
// increase is progress and resets the no-progress clock too.
func TestAttemptLimitWatchCPUIncreaseResetsClock(t *testing.T) {
	var events []string
	sessions := &fakeStallSessions{events: &events}
	store := &fakeStallStore{events: &events}
	task := &autonomouspkg.Task{ID: "task-1", PRDID: "prd-1"}
	w := newAttemptLimitWatcher(limitWatchBase, attemptObservation{Now: limitWatchBase, ChildCPUTicks: 100},
		sessions, store, t.TempDir(), task, "sess-1")

	err := w.check(attemptObservation{Now: limitWatchBase.Add(48 * time.Hour), ChildCPUTicks: 200}, 0, time.Hour)

	if err != nil {
		t.Fatalf("CPU increase is progress, want no stop, got %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("want no kill or snapshot, events = %v", events)
	}
}

// TestAttemptLimitWatchBothLimitsZeroNeverStops asserts that with both
// limits disabled the watcher never stops or kills, even after days.
func TestAttemptLimitWatchBothLimitsZeroNeverStops(t *testing.T) {
	var events []string
	sessions := &fakeStallSessions{events: &events}
	store := &fakeStallStore{events: &events}
	task := &autonomouspkg.Task{ID: "task-1", PRDID: "prd-1"}
	w := newAttemptLimitWatcher(limitWatchBase, attemptObservation{Now: limitWatchBase},
		sessions, store, t.TempDir(), task, "sess-1")

	for _, d := range []time.Duration{24 * time.Hour, 2 * 24 * time.Hour, 7 * 24 * time.Hour} {
		if err := w.check(attemptObservation{Now: limitWatchBase.Add(d)}, 0, 0); err != nil {
			t.Fatalf("both limits 0 means no stop, got %v at +%s", err, d)
		}
	}
	if len(events) != 0 {
		t.Fatalf("want no kill or snapshot, events = %v", events)
	}
	if len(store.refs) != 0 {
		t.Fatalf("want no recorded refs, got %v", store.refs)
	}
}

// TestAttemptLimitWatchPreservesWorkBeforeKill runs a real stopping check in
// a temp git repo with an uncommitted edit and an untracked file, and asserts
// the work is snapshotted under refs/datawatch/attempts/ before the kill and
// left untouched on disk.
func TestAttemptLimitWatchPreservesWorkBeforeKill(t *testing.T) {
	gitPath := exec.Command("git", "--version")
	if _, err := gitPath.Output(); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	limitWatchGitOut(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	limitWatchGitOut(t, dir, "add", "work.txt")
	limitWatchGitOut(t, dir, "commit", "-q", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("worker edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var events []string
	sessions := &fakeStallSessions{events: &events}
	store := &fakeStallStore{events: &events}
	task := &autonomouspkg.Task{ID: "watch-task", PRDID: "prd-1"}
	w := newAttemptLimitWatcher(limitWatchBase, attemptObservation{Now: limitWatchBase},
		sessions, store, dir, task, "sess-f")

	err := w.check(attemptObservation{Now: limitWatchBase.Add(2 * time.Hour)}, 0, time.Hour)
	if err == nil {
		t.Fatal("want a stop error after 2h idle with a 1h no-progress limit, got nil")
	}

	if len(store.refs) != 1 {
		t.Fatalf("want one recorded snapshot ref, got %v", store.refs)
	}
	ref := store.refs[0]
	if !strings.HasPrefix(ref, "refs/datawatch/attempts/watch-task/") {
		t.Fatalf("snapshot ref = %q, want one under refs/datawatch/attempts/watch-task/", ref)
	}
	sha := limitWatchGitOut(t, dir, "rev-parse", ref)
	if sha == "" || strings.ContainsAny(sha, "\n") {
		t.Fatalf("git rev-parse %s did not resolve to a commit: %q", ref, sha)
	}

	// Snapshot must have happened before the kill.
	if len(events) != 2 || events[0] != "record:"+ref || events[1] != "kill:sess-f" {
		t.Fatalf("event order = %v, want the snapshot record before the kill", events)
	}

	// The working files must still be on disk, unmodified.
	got, err := os.ReadFile(filepath.Join(dir, "work.txt"))
	if err != nil || string(got) != "worker edit\n" {
		t.Fatalf("work.txt after check = %q, err = %v, want the worker edit preserved", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "fresh.txt"))
	if err != nil || string(got) != "brand new\n" {
		t.Fatalf("fresh.txt after check = %q, err = %v, want the untracked file preserved", got, err)
	}
}

// TestAttemptLimitWatchErrorShape asserts the returned error is a
// stalled-worker error with the human-readable no-progress wording.
func TestAttemptLimitWatchErrorShape(t *testing.T) {
	sessions := &fakeStallSessions{events: &[]string{}}
	store := &fakeStallStore{events: &[]string{}}
	task := &autonomouspkg.Task{ID: "task-1", PRDID: "prd-1"}
	w := newAttemptLimitWatcher(limitWatchBase, attemptObservation{Now: limitWatchBase},
		sessions, store, t.TempDir(), task, "sess-1")

	err := w.check(attemptObservation{Now: limitWatchBase.Add(90 * time.Minute)}, 0, time.Hour)

	if err == nil {
		t.Fatal("want a stop error, got nil")
	}
	if !errors.Is(err, autonomouspkg.ErrWorkerStalled) {
		t.Fatalf("error %q does not satisfy errors.Is(err, ErrWorkerStalled)", err)
	}
	for _, want := range []string{"no progress for", "no output, no file changes, no commits"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should contain %q", err, want)
		}
	}
}
