// Tests for the end reason each operator surface records when it ends a
// session. The journal and the session timeline must both name the surface
// (by=) and say why (reason=). The hook-event tests pin that a Stop or
// SubagentStop event changes only the status board, never the session.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/session"
)

// endAttrJournal is a journal writer that is safe for the goroutines a
// session end can start.
type endAttrJournal struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (j *endAttrJournal) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.b.Write(p)
}

func (j *endAttrJournal) String() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.b.String()
}

// newEndAttrManager returns a manager over the in-memory tmux fake, with
// verbose journaling on and its journal captured.
func newEndAttrManager(t *testing.T) (*session.Manager, string, *endAttrJournal) {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	sm.WithFakeTmux()
	sm.SetVerbose(true)
	j := &endAttrJournal{}
	sm.SetJournal(j)
	t.Cleanup(sm.WaitBackground)
	return sm, dir, j
}

// saveEndAttrSession saves a session with a timeline tracker, so an end
// reaches timeline.md the way a live session's does.
func saveEndAttrSession(t *testing.T, sm *session.Manager, dir, id string, state session.State) *session.Session {
	t.Helper()
	now := time.Now()
	sess := &session.Session{
		ID: id, FullID: "h-" + id, Hostname: "h", TmuxSession: "cs-" + id,
		Task: "end attribution " + id, State: state, CreatedAt: now, UpdatedAt: now,
	}
	if err := sm.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if _, err := session.NewTracker(dir, sess); err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	return sess
}

// endAttrTimelineLines returns the session-ended lines of a session's timeline.
func endAttrTimelineLines(t *testing.T, dir string, sess *session.Session) []string {
	t.Helper()
	lines, err := session.ResumeTracker(dir, sess).ReadTimeline()
	if err != nil {
		t.Fatalf("ReadTimeline: %v", err)
	}
	var ended []string
	for _, line := range lines {
		if strings.Contains(line, "session ended") {
			ended = append(ended, line)
		}
	}
	return ended
}

// assertEndAttrRecorded checks that one end reached the journal and the
// timeline, each naming the caller and the reason.
func assertEndAttrRecorded(t *testing.T, j *endAttrJournal, ended []string, caller, reason string) {
	t.Helper()
	by := "by=" + caller
	why := "reason=" + reason
	if out := j.String(); !strings.Contains(out, by) || !strings.Contains(out, why) {
		t.Errorf("journal lacks %q and %q:\n%s", by, why, out)
	}
	if len(ended) != 1 {
		t.Fatalf("want one session-ended timeline line, got %d: %q", len(ended), ended)
	}
	if !strings.Contains(ended[0], by) || !strings.Contains(ended[0], why) {
		t.Errorf("timeline line %q lacks %q and %q", ended[0], by, why)
	}
}

func TestEndAttributionRESTSessionKill(t *testing.T) {
	sm, dir, j := newEndAttrManager(t)
	srv := NewServer(NewHub(), sm, "h", "", nil, nil, "")
	sess := saveEndAttrSession(t, sm, dir, "rk01", session.StateRunning)

	body, _ := json.Marshal(map[string]any{"id": sess.FullID})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/kill", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.handleKillSession(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("kill: status=%d body=%s", rr.Code, rr.Body.String())
	}
	srv.broadcastWG.Wait()

	reason := "REST POST /api/sessions/kill requested by operator"
	assertEndAttrRecorded(t, j, endAttrTimelineLines(t, dir, sess), endCallerRESTSessionKill, reason)
}

func TestEndAttributionCommandKill(t *testing.T) {
	sm, dir, j := newEndAttrManager(t)
	srv := NewServer(NewHub(), sm, "h", "", nil, nil, "")
	sess := saveEndAttrSession(t, sm, dir, "ck01", session.StateRunning)

	origin := "REST POST /api/command requested by operator"
	got := srv.executeCommand(router.Command{Type: router.CmdKill, SessionID: sess.FullID}, "kill "+sess.FullID, origin)
	if !strings.Contains(got, "Killed.") {
		t.Fatalf("kill command reply = %q, want a killed reply", got)
	}
	srv.broadcastWG.Wait()

	assertEndAttrRecorded(t, j, endAttrTimelineLines(t, dir, sess), endCallerCommandKill, origin)
}

func TestEndAttributionPRDDeleteRecordsReason(t *testing.T) {
	sm, dir, j := newEndAttrManager(t)
	al, err := audit.NewAt(t.TempDir() + "/audit.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	sess := saveEndAttrSession(t, sm, dir, "pd01", session.StateRunning)
	srv := &Server{autonomousMgr: &sessionSpyAutonomous{ids: []string{sess.FullID}}, manager: sm, auditLog: al}

	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodDelete, "/api/autonomous/prds/prd-pd1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("PRD delete: status=%d body=%s", rr.Code, rr.Body.String())
	}

	reason := "REST DELETE /api/autonomous/prds/prd-pd1 requested by operator: the PRD's task sessions end"
	assertEndAttrRecorded(t, j, endAttrTimelineLines(t, dir, sess), endCallerAutonomousPRDStop, reason)
}

// Stop and SubagentStop only change the status board. The session must stay
// running, no end may be recorded, and the journal says action=none.
func TestEndAttributionHookStopLeavesSessionRunning(t *testing.T) {
	sm, dir, j := newEndAttrManager(t)
	srv := NewServer(NewHub(), sm, "h", "", nil, nil, "")
	sess := saveEndAttrSession(t, sm, dir, "hs01", session.StateRunning)

	for _, event := range []string{"Stop", "SubagentStop"} {
		body, _ := json.Marshal(map[string]any{"event": event})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.FullID+"/hook-event", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		srv.handleSessionHookEvent(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s hook event: status=%d body=%s", event, rr.Code, rr.Body.String())
		}
	}

	got, ok := sm.GetSession(sess.FullID)
	if !ok {
		t.Fatal("session vanished after hook events")
	}
	if got.State != session.StateRunning {
		t.Errorf("state after Stop and SubagentStop = %s, want %s", got.State, session.StateRunning)
	}
	if ended := endAttrTimelineLines(t, dir, sess); len(ended) != 0 {
		t.Errorf("hook events added an ended record: %q", ended)
	}
	out := j.String()
	if strings.Contains(out, "[session] ended") {
		t.Errorf("hook events journaled an end:\n%s", out)
	}
	for _, want := range []string{
		"[session:debug] hook event Stop subagent=false session=hs01 state=running action=none (board only)",
		"[session:debug] hook event SubagentStop subagent=true session=hs01 state=running action=none (board only)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("journal lacks %q:\n%s", want, out)
		}
	}
	if board := HookBoardFor(sess.FullID); board.State != "waiting" {
		t.Errorf("status board state = %q, want waiting (the board is what a Stop changes)", board.State)
	}
}
