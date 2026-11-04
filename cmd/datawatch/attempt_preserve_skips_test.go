package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/gitguard"
)

// skipRecordingStore is an attemptSnapshotStore that records both calls with
// their task identity and an event log for ordering assertions.
type skipRecordingStore struct {
	events []string
	refs   []string
	skips  [][]autonomouspkg.SnapshotSkippedPath
	prds   []string
	tasks  []string
}

func (s *skipRecordingStore) RecordTaskAttemptSnapshot(prdID, taskID, ref string) error {
	s.events = append(s.events, "record:"+ref)
	s.refs = append(s.refs, ref)
	return nil
}

func (s *skipRecordingStore) RecordTaskSkippedSnapshotPaths(prdID, taskID string, skipped []autonomouspkg.SnapshotSkippedPath) error {
	s.events = append(s.events, "record-skipped:"+strconv.Itoa(len(skipped)))
	s.skips = append(s.skips, skipped)
	s.prds = append(s.prds, prdID)
	s.tasks = append(s.tasks, taskID)
	return nil
}

// preserveSkipsRepo is a stall state with one skipped candidate per of two
// different guard reasons on top of a normal work edit (kept).
func preserveSkipsRepo(t *testing.T) string {
	t.Helper()
	dir := stallTestRepo(t)
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("node_modules/pkg/main.js", "dep\n") // refused: ignore-dir
	write("weights/gpt.gguf", "weights\n")     // refused: pattern
	return dir
}

func TestPreserveAttemptWork_RecordsSkipListOnTask(t *testing.T) {
	t.Run("skips recorded with reasons, ref still recorded", func(t *testing.T) {
		store := &skipRecordingStore{}
		task := &autonomouspkg.Task{ID: "task-3", PRDID: "prd-3"}

		preserveAttemptWork(context.Background(), store, preserveSkipsRepo(t), task)

		// The snapshot itself still happens for the kept work.
		if len(store.refs) != 1 || !strings.HasPrefix(store.refs[0], "refs/datawatch/attempts/task-3/1") {
			t.Fatalf("snapshot refs = %v, want one attempt-1 ref for task-3", store.refs)
		}
		// The skip list lands on the same task, with paths and reasons.
		if len(store.skips) != 1 {
			t.Fatalf("RecordTaskSkippedSnapshotPaths calls = %d, want 1", len(store.skips))
		}
		if store.prds[0] != "prd-3" || store.tasks[0] != "task-3" {
			t.Fatalf("skips recorded on %s/%s, want prd-3/task-3", store.prds[0], store.tasks[0])
		}
		got := map[string]string{}
		for _, s := range store.skips[0] {
			if _, dup := got[s.Path]; dup {
				t.Errorf("duplicate skipped path %q", s.Path)
			}
			got[s.Path] = s.Reason
		}
		want := map[string]string{
			"node_modules/pkg/main.js": "ignore-dir",
			"weights/gpt.gguf":         "pattern",
		}
		if len(store.skips[0]) != len(want) {
			t.Errorf("skip list = %v, want %d entries", got, len(want))
		}
		for path, reason := range want {
			if got[path] != reason {
				t.Errorf("skipped %s has reason %q, want %q (all: %v)", path, got[path], reason, got)
			}
		}
		// Skips are recorded before the snapshot ref (the task carries the
		// warning even when the snapshot still succeeds).
		marker := "record-skipped:2"
		idx := -1
		for i, e := range store.events {
			if e == marker {
				idx = i
				break
			}
		}
		if idx == -1 {
			t.Fatalf("event order = %v, missing %q", store.events, marker)
		}
		if idx != 0 || len(store.events) < 2 || store.events[1] != "record:"+store.refs[0] {
			t.Errorf("event order = %v, want skips recorded before the ref", store.events)
		}
	})

	t.Run("a run without skips records nothing", func(t *testing.T) {
		store := &skipRecordingStore{}
		task := &autonomouspkg.Task{ID: "task-4", PRDID: "prd-4"}

		preserveAttemptWork(context.Background(), store, stallTestRepo(t), task)

		if len(store.refs) != 1 {
			t.Fatalf("snapshot refs = %v, want the attempt ref", store.refs)
		}
		if len(store.skips) != 0 || len(store.prds) != 0 || len(store.tasks) != 0 {
			t.Fatalf("no-skip run recorded skips: %v on %v/%v", store.skips, store.prds, store.tasks)
		}
	})

	t.Run("skips still recorded when the snapshot is refused", func(t *testing.T) {
		// Two kept candidates against a MaxStagedFiles limit of one refuses the
		// snapshot after both skips have been computed — exactly the path the
		// skip-recording must survive.
		prevLimit := gitguard.Default.MaxStagedFiles
		gitguard.Default.MaxStagedFiles = 1
		t.Cleanup(func() { gitguard.Default.MaxStagedFiles = prevLimit })

		store := &skipRecordingStore{}
		task := &autonomouspkg.Task{ID: "task-5", PRDID: "prd-5"}
		dir := preserveSkipsRepo(t)
		if err := os.MkdirAll(filepath.Join(dir, "extra"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "extra", "other.txt"), []byte("another kept file\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		preserveAttemptWork(context.Background(), store, dir, task)

		if len(store.refs) != 0 {
			t.Fatalf("refused snapshot still recorded refs: %v", store.refs)
		}
		if len(store.skips) != 1 || len(store.skips[0]) != 2 {
			t.Fatalf("refused run recorded skips %v, want the 2 computed skips", store.skips)
		}
	})
}
