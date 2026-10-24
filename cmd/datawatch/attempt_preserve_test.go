package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// fakeStallSessions records EndSession calls into a shared event log.
type fakeStallSessions struct {
	events *[]string
	whys   []session.EndReason
}

func (f *fakeStallSessions) EndSession(fullID string, why session.EndReason) error {
	*f.events = append(*f.events, "kill:"+fullID)
	f.whys = append(f.whys, why)
	return nil
}

// testStallEnd is the end a stall kill records in these tests.
var testStallEnd = session.EndReason{Caller: "test.stall", Reason: "test stall reason"}

// fakeStallStore records RecordTaskAttemptSnapshot calls into the same log.
type fakeStallStore struct {
	events *[]string
	refs   []string
}

func (f *fakeStallStore) RecordTaskAttemptSnapshot(prdID, taskID, ref string) error {
	*f.events = append(*f.events, "record:"+ref)
	f.refs = append(f.refs, ref)
	return nil
}

// stallTestGit runs git in dir with a fixed identity, never touching global config.
func stallTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// stallTestRepo returns a repo with one commit and an uncommitted edit, so a
// snapshot has something to preserve.
func stallTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stallTestGit(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stallTestGit(t, dir, "add", "work.txt")
	stallTestGit(t, dir, "commit", "-q", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("worker edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAttemptSnapshotRecordedBeforeStallKill(t *testing.T) {
	t.Run("snapshot recorded before kill", func(t *testing.T) {
		var events []string
		sessions := &fakeStallSessions{events: &events}
		store := &fakeStallStore{events: &events}
		task := &autonomouspkg.Task{ID: "task-1", PRDID: "prd-1"}

		killStalledWorker(context.Background(), sessions, store, true, stallTestRepo(t), task, "sess-1", testStallEnd)

		if len(store.refs) != 1 {
			t.Fatalf("want one recorded snapshot, got %v", store.refs)
		}
		if !strings.HasPrefix(store.refs[0], "refs/datawatch/attempts/task-1/1") {
			t.Fatalf("snapshot ref = %q, want attempt 1 for task-1", store.refs[0])
		}
		if len(events) != 2 || events[0] != "record:"+store.refs[0] || events[1] != "kill:sess-1" {
			t.Fatalf("event order = %v, want snapshot record before kill", events)
		}
	})

	t.Run("preserve disabled skips snapshot", func(t *testing.T) {
		var events []string
		sessions := &fakeStallSessions{events: &events}
		store := &fakeStallStore{events: &events}
		task := &autonomouspkg.Task{ID: "task-1", PRDID: "prd-1"}

		killStalledWorker(context.Background(), sessions, store, false, stallTestRepo(t), task, "sess-1", testStallEnd)

		if len(events) != 1 || events[0] != "kill:sess-1" {
			t.Fatalf("event order = %v, want kill only", events)
		}
	})

	t.Run("snapshot failure still kills", func(t *testing.T) {
		var events []string
		sessions := &fakeStallSessions{events: &events}
		store := &fakeStallStore{events: &events}
		task := &autonomouspkg.Task{ID: "task-1", PRDID: "prd-1"}

		killStalledWorker(context.Background(), sessions, store, true, t.TempDir(), task, "sess-1", testStallEnd)

		if len(events) != 1 || events[0] != "kill:sess-1" {
			t.Fatalf("event order = %v, want kill only after failed snapshot", events)
		}
	})
}
