// Tests for the session manager's temp-file release hook on the end path:
// emitSessionEnd must run the scratch releaser before the end listener, the
// releaser must remove the ending session's own leftover temp files (task
// capture files, per-session worker config directories) and must leave a
// live session's files untouched.

package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/llm/scratch"
)

// newEndTestManager builds a bare Manager whose end path is fully
// instrumentable — no tmux, no processes — because emitSessionEnd is the
// single end-path funnel.
func newEndTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager("testhost", t.TempDir(), "/bin/true", 0)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// wireReleaser installs the same scratch-releaser shape main.go wires in
// production: release under both keys a backend might have recorded with
// (the tmux session name and the bare session id); unrecorded keys are
// no-ops.
func wireReleaser(m *Manager) {
	m.SetScratchReleaser(func(s *Session) {
		if s == nil {
			return
		}
		keys := map[string]bool{}
		if s.TmuxSession != "" {
			keys[s.TmuxSession] = true
		}
		keys[s.ID] = true
		for k := range keys {
			scratch.ReleaseSession(k)
		}
	})
}

// TestEmitSessionEndReleasesOwnScratchBeforeListener: the end path runs the
// scratch releaser first, then exactly one end listener; releasing removes
// the ending session's files and leaves a live session's files.
func TestEmitSessionEndReleasesOwnScratchBeforeListener(t *testing.T) {
	m := newEndTestManager(t)

	// The ending session's backends recorded their files under its tmux
	// name; the live session recorded under its own.
	const endedKey, liveKey = "cs-host-dead1", "cs-host-live1"

	dir := t.TempDir()
	endedFile := filepath.Join(dir, "datawatch-pi-task-"+endedKey+"-17.txt")
	endedDir := filepath.Join(dir, "datawatch-pi-agent-"+endedKey+"-17")
	if err := os.MkdirAll(endedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endedFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(endedDir, "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	liveFile := filepath.Join(dir, "datawatch-pi-task-"+liveKey+"-17.txt")
	if err := os.WriteFile(liveFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	scratch.RecordPath(endedKey, endedFile)
	scratch.RecordPath(endedKey, endedDir)
	scratch.RecordPath(liveKey, liveFile)

	var order []string
	releaseCalls, listenerCalls := 0, 0
	wireReleaser(m)
	m.SetScratchReleaser(func(s *Session) {
		releaseCalls++
		order = append(order, "release")
		if s == nil || s.FullID != "testhost/cs-host-dead1-17" {
			t.Fatalf("releaser got the wrong session: %+v", s)
		}
		wireReleaser(m) // re-assert the production shape; the call below does the work
		// (SetScratchReleaser just overwrote the hook; release manually now
		// in the same production shape.)
		if s.TmuxSession != "" {
			scratch.ReleaseSession(s.TmuxSession)
		}
		scratch.ReleaseSession(s.ID)
	})
	m.SetOnSessionEnd(func(s *Session) {
		listenerCalls++
		order = append(order, "listen")
	})

	m.emitSessionEnd(&Session{
		ID:          "cs-host-dead1-17",
		FullID:      "testhost/cs-host-dead1-17",
		TmuxSession: "cs-host-dead1",
	})

	if releaseCalls != 1 {
		t.Fatalf("releaser ran %d times; want exactly 1", releaseCalls)
	}
	if listenerCalls != 1 {
		t.Fatalf("end listener ran %d times; want exactly 1", listenerCalls)
	}
	if len(order) != 2 || order[0] != "release" || order[1] != "listen" {
		t.Fatalf("end-path order = %v; want [release listen]", order)
	}
	if _, err := os.Stat(endedFile); !os.IsNotExist(err) {
		t.Fatalf("ended session's temp file survived the end path")
	}
	if _, err := os.Stat(endedDir); !os.IsNotExist(err) {
		t.Fatalf("ended session's worker config dir survived the end path")
	}
	if _, err := os.Stat(liveFile); err != nil {
		t.Fatalf("live session's temp file was removed by another session's end path: %v", err)
	}
	if paths := scratch.Paths(liveKey); len(paths) != 1 || paths[0] != liveFile {
		t.Fatalf("live session's scratch record changed: %v", paths)
	}
}

// TestEmitSessionEndNilReleaserStillDelivers: with no scratch releaser
// wired (e.g. an embed build without the daemon launcher) the end listener
// still fires exactly once — the hook must not change the delivery.
func TestEmitSessionEndNilReleaserStillDelivers(t *testing.T) {
	m := newEndTestManager(t)
	calls := 0
	m.SetOnSessionEnd(func(s *Session) { calls++ })
	m.emitSessionEnd(&Session{FullID: "testhost/x"})
	if calls != 1 {
		t.Fatalf("end listener ran %d times without a releaser; want 1", calls)
	}
}

// TestEmitSessionEndNilReleaserAndNilListener: a fully bare end emission
// (no hooks at all) must not panic — the pre-hook behaviour of the call
// sites before this change.
func TestEmitSessionEndNilReleaserAndNilListener(t *testing.T) {
	m := newEndTestManager(t)
	m.SetScratchReleaser(nil)
	m.SetOnSessionEnd(nil)
	m.emitSessionEnd(&Session{FullID: "testhost/y"}) // must not panic
}
