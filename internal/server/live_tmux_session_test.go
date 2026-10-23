//go:build livetmux

// Live tmux integration test for the start/delete path. Excluded from
// normal builds: line 1 is the `livetmux` build tag, so a plain
// `go test ./internal/server/` can never compile it or start a real
// multiplexer. On top of the tag, the test body is environment-gated: it
// skips unless DATAWATCH_LIVE_TMUX=1, and it t.Fatals unless the process
// is fully isolated from any live multiplexer — TMUX must be empty (no
// attached client) and TMUX_TMPDIR must be a per-run throwaway directory
// under os.TempDir() created by this test run (see internal/testhome).
// Together the tag and the guard guarantee the test can never touch the
// operator's multiplexer server or socket. Run it explicitly with:
//
//	DATAWATCH_LIVE_TMUX=1 go test -tags livetmux -run TestLiveTmuxSessionStartDelete ./internal/server/
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/testhome"
)

// TestLiveTmuxSessionStartDelete starts one real tmux session through the
// HTTP handler and tears it back down again, proving the start/delete path
// works against a real multiplexer — on the isolated per-test-run socket,
// never the operator's.
func TestLiveTmuxSessionStartDelete(t *testing.T) {
	if os.Getenv("DATAWATCH_LIVE_TMUX") != "1" {
		t.Skip("live tmux test disabled; set DATAWATCH_LIVE_TMUX=1 to run it")
	}

	// Hard isolation gate: refuse to run against a live multiplexer.
	// 1) TMUX must be empty — inside a client, tmux would connect to the
	//    client's server.
	// 2) TMUX_TMPDIR must be a directory under os.TempDir() that
	//    testhome created for this run (marker file), so a tmux this test
	//    spawns binds its socket in a throwaway dir it later removes.
	if tmux := os.Getenv("TMUX"); tmux != "" {
		t.Fatalf("refusing: TMUX=%q (process is inside a tmux client; a live multiplexer could be reached)", tmux)
	}
	tmpdir := os.Getenv("TMUX_TMPDIR")
	if tmpdir == "" {
		t.Fatal("refusing: TMUX_TMPDIR empty (no isolated socket dir)")
	}
	if fi, err := os.Stat(tmpdir); err != nil || !fi.IsDir() {
		t.Fatalf("refusing: TMUX_TMPDIR %q is not a directory: %v", tmpdir, err)
	}
	if !underDir(os.TempDir(), tmpdir, t) {
		t.Fatalf("refusing: TMUX_TMPDIR %q is not under os.TempDir() %s", tmpdir, os.TempDir())
	}
	if !testhome.TmuxDirOwned(tmpdir) {
		t.Fatalf("refusing: TMUX_TMPDIR %q was not created by this test run (missing testhome marker)", tmpdir)
	}

	// Start a real session through the handler. Build the manager and
	// server here rather than via newLLMServer — newLLMServer swaps in
	// the in-memory fake multiplexer, and the whole point of this test is
	// a real tmux socket (the isolated per-run one).
	sm, err := session.NewManager("h", t.TempDir(), "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// Deliberately no WithFakeTmux here: the manager keeps its real tmux
	// backend and talks only to the throwaway TMUX_TMPDIR server.
	t.Cleanup(sm.WaitBackground)
	srv := NewServer(NewHub(), sm, "h", "", nil, nil, "")

	payload := map[string]any{
		"task":        "live-tmux start/delete round-trip",
		"project_dir": t.TempDir(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleStartSession: status %d body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		FullID string `json:"full_id"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	sessID := resp.FullID
	if sessID == "" {
		sessID = resp.ID
	}
	if sessID == "" {
		t.Fatal("start response had no session id")
	}
	sess, ok := srv.manager.GetSession(sessID)
	if !ok || sess == nil {
		t.Fatalf("started session %q not found in manager", sessID)
	}
	if sess.TmuxSession == "" {
		t.Fatal("started session has no tmux session name")
	}
	// Proof this really hit a multiplexer (the isolated one): the tmux
	// session must exist via a direct tmux query.
	tm := session.TmuxManager{}
	if !tm.SessionExists(sess.TmuxSession) {
		t.Fatalf("tmux session %q does not exist in isolated TMUX_TMPDIR %q", sess.TmuxSession, tmpdir)
	}

	// Tear down exactly this session, via the manager's per-session
	// methods: cancel its monitor, kill its tmux pane (only its own name
	// is ever passed), drain background writers, then delete it from the
	// store. No KillAll, no blanket tmux kill-server.
	srv.broadcastWG.Wait()
	if sess.State != session.StateKilled {
		if err := srv.manager.Kill(sessID); err != nil {
			t.Fatalf("Kill(%s): %v", sessID, err)
		}
	}
	srv.manager.KillTmuxSession(sessID)
	srv.manager.WaitBackground()
	srv.broadcastWG.Wait()
	if err := srv.manager.Delete(sessID, false); err != nil {
		t.Fatalf("Delete(%s): %v", sessID, err)
	}

	if tm.SessionExists(sess.TmuxSession) {
		t.Errorf("tmux session %q still exists after teardown", sess.TmuxSession)
	}
	if _, ok := srv.manager.GetSession(sessID); ok {
		t.Errorf("session %q still in manager after Delete", sessID)
	}
}
