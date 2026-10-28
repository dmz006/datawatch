package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pilog"
)

// writePiLog writes a pi session log for workDir under root whose newest
// event is age old, and returns the log's start time.
func writePiLog(t *testing.T, root, workDir string, now time.Time, age time.Duration) time.Time {
	t.Helper()
	dir := filepath.Join(root, pilog.ProjectDirName(workDir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	started := now.Add(-time.Hour).UTC().Truncate(time.Second)
	name := started.Format("2006-01-02T15-04-05-000Z") + "_fake.jsonl"
	ts := now.Add(-age).UTC().Format(time.RFC3339Nano)
	line := `{"type":"message","timestamp":"` + ts + `","message":{"role":"assistant","content":[{"type":"text","text":"x"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return started
}

func TestPiLogLastEventAge(t *testing.T) {
	root := t.TempDir()
	workDir := "/work/proj"
	now := time.Now()
	started := writePiLog(t, root, workDir, now, 90*time.Second)

	if got := piLogLastEventAge("pi-prompt", root, workDir, started, now); got < 89*time.Second || got > 91*time.Second {
		t.Errorf("pi-prompt age: got %s, want ~90s", got)
	}
	if got := piLogLastEventAge("claude-code", root, workDir, started, now); got != 0 {
		t.Errorf("non-pi backend: got %s, want 0", got)
	}
	if got := piLogLastEventAge("pi-prompt", root, "/work/other", started, now); got != 0 {
		t.Errorf("no log for work dir: got %s, want 0 (fallback)", got)
	}
	if got := piLogLastEventAge("pi-prompt", "", workDir, started, now); got != 0 {
		t.Errorf("empty root: got %s, want 0", got)
	}
}

func TestDecideWorkerStallUsesLogLastEventAge(t *testing.T) {
	const threshold = 10 * time.Minute
	dir := t.TempDir()
	paneLog := filepath.Join(dir, "pane.log") // touched just now: pane noise
	if err := os.WriteFile(paneLog, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	obs := workerStallObservation{LogPath: paneLog, Threshold: threshold, Now: time.Now().Add(time.Hour)}
	// Pane log is an hour stale, so with no log age this stalls.
	if stalled, _ := decideWorkerStall(obs); !stalled {
		t.Fatal("unknown log age, stale pane log: want stalled")
	}
	obs.LogLastEventAge = time.Minute
	if stalled, reason := decideWorkerStall(obs); stalled {
		t.Fatalf("growing log: want not stalled, got %q", reason)
	}
	obs.Now = time.Now() // pane log fresh again
	obs.LogLastEventAge = time.Hour
	if stalled, _ := decideWorkerStall(obs); !stalled {
		t.Fatal("log silent past threshold: want stalled despite fresh pane log")
	}
}
