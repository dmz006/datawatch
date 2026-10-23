// v8.36.0 — memory-strategy (keep/purge/archive) on session delete, matching
// the PRD hard-delete BL386/#175 feature. A session's memories were
// previously always silently kept with no operator choice at all — not even
// the backend supported anything else, let alone Android or the PWA.

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func newSessionDeleteTestServer(t *testing.T) (*Server, *scopeTestBackend) {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	b := &scopeTestBackend{}
	srv := NewServer(NewHub(), sm, "h", "", nil, nil, "")
	// Registered after the TempDir, so it runs before its removal.
	t.Cleanup(func() {
		srv.broadcastWG.Wait()
		sm.WaitBackground()
	})
	srv.memoryBackend = b
	return srv, b
}

func startTestSession(t *testing.T, srv *Server, projectDir string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"task": "hello", "project_dir": projectDir})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleStartSession: status %d body: %s", w.Code, w.Body.String())
	}
	// handleStartSession broadcasts the session list from an untracked
	// goroutine that marshals the live *Session; wait for it now so the
	// caller's own mutations (SendInput, Kill) cannot race with it.
	srv.broadcastWG.Wait()
	var resp struct {
		FullID string `json:"full_id"`
		ID     string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	fullID := resp.FullID
	if fullID == "" {
		fullID = resp.ID
	}
	// Found 2026-10-09: handleStartSession is real end-to-end — it
	// creates a genuine tmux-backed session via the manager, not a
	// mock. A caller that exercises some other action (rollback,
	// input, start itself) and never kills it left the tmux pane
	// running forever: t.TempDir() wipes the test's on-disk session
	// store at teardown, but the actual tmux pane lives on a separate,
	// persistent tmux server process this test only *connects* to, not
	// owns — and if that server happens to be the real production one
	// (TMUX_TMPDIR inherited from the shell, not test-isolated), the
	// leak is a real orphaned session on a live daemon, not just test
	// noise. Every caller gets this for free now instead of each test
	// needing to remember its own teardown.
	t.Cleanup(func() { stopTestSession(t, srv, fullID) })
	return fullID
}

// stopTestSession tears a startTestSession session down far enough that
// nothing is still writing under the manager's data dir when t.TempDir's
// RemoveAll runs. Cleanups run last-in-first-out and the TempDir removal
// was registered first, so this always runs before it. Order matters:
// Kill cancels the monitor goroutine and kills the pane (KillTmuxSession
// alone leaves the monitor appending to the session log and saving the
// store), and only then can we wait for the files under the data dir to
// stop changing.
func stopTestSession(t *testing.T, srv *Server, fullID string) {
	t.Helper()
	srv.broadcastWG.Wait()
	if sess, ok := srv.manager.GetSession(fullID); ok && sess.State != session.StateKilled {
		_ = srv.manager.Kill(fullID)
	}
	srv.manager.KillTmuxSession(fullID)
	// Deterministic wait: monitors, screen capture and send-queue drains
	// have exited, so no late writer remains under the data dir.
	srv.manager.WaitBackground()
	srv.broadcastWG.Wait()
	// Final assertion only; the waits above are what make it stable.
	waitDirQuiet(t, srv.manager.DataDir())
}

// waitDirQuiet polls dir until two consecutive snapshots of its files
// (path, size, mtime) match, so a monitor or store write that was in
// flight when the session was killed has finished. Bounded: fails the
// test rather than waiting forever.
func waitDirQuiet(t *testing.T, dir string) {
	t.Helper()
	snapshot := func() string {
		var b strings.Builder
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, ierr := d.Info(); ierr == nil {
				fmt.Fprintf(&b, "%s %d %d\n", p, info.Size(), info.ModTime().UnixNano())
			}
			return nil
		})
		return b.String()
	}
	deadline := time.Now().Add(2 * time.Second)
	prev := snapshot()
	for {
		time.Sleep(50 * time.Millisecond)
		cur := snapshot()
		if cur == prev {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("files under %s were still changing 2s after the session was killed", dir)
			return
		}
		prev = cur
	}
}

// TestHandleDeleteSession_MemoryStrategyPurge verifies purge removes only
// the deleted session's own memories, leaving another session's untouched —
// exercising the PurgeScope fix (session-local purges were previously
// project-wide, see bl386_phase3_test.go's regression tests).
func TestHandleDeleteSession_MemoryStrategyPurge(t *testing.T) {
	srv, backend := newSessionDeleteTestServer(t)
	projectDir := t.TempDir()
	sessA := startTestSession(t, srv, projectDir)
	sessB := startTestSession(t, srv, projectDir)
	// sessA is deleted by the test itself below; sessB never is (it's the
	// control used to verify the OTHER session's memory survives), so its
	// spawned tmux session would otherwise leak on every run.
	t.Cleanup(func() {
		srv.broadcastWG.Wait() // handleDeleteSession's broadcast marshals sessB
		_ = srv.manager.Delete(sessB, false)
	})

	_, _ = backend.Save(projectDir, "session A memory", "", "", sessA, nil)
	_, _ = backend.Save(projectDir, "session B memory", "", "", sessB, nil)

	body, _ := json.Marshal(map[string]any{"id": sessA, "memory_strategy": "purge"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/delete", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleDeleteSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleDeleteSession: status %d body: %s", w.Code, w.Body.String())
	}

	remaining, _ := backend.ListRecent(projectDir, 100)
	if len(remaining) != 1 || remaining[0].SessionID != sessB {
		t.Fatalf("expected only session B's memory to remain, got: %+v", remaining)
	}
}

// TestHandleDeleteSession_MemoryStrategyKeep_Default verifies omitting
// memory_strategy (or "keep") leaves memories untouched — the pre-existing
// behavior, unaffected by this change.
func TestHandleDeleteSession_MemoryStrategyKeep_Default(t *testing.T) {
	srv, backend := newSessionDeleteTestServer(t)
	projectDir := t.TempDir()
	sess := startTestSession(t, srv, projectDir)
	_, _ = backend.Save(projectDir, "keep me", "", "", sess, nil)

	body, _ := json.Marshal(map[string]any{"id": sess})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/delete", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleDeleteSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleDeleteSession: status %d body: %s", w.Code, w.Body.String())
	}

	remaining, _ := backend.ListRecent(projectDir, 100)
	if len(remaining) != 1 {
		t.Fatalf("expected the memory to survive a plain (keep) delete, got: %+v", remaining)
	}
}
