package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// The SubagentStop hook is the moment a subagent has written its last usage.
// The daemon records it under the parent session right then, instead of
// waiting for the next periodic scan, and a Stop of the parent does not.
func TestSubagentStopHookRecordsUsageUnderParent(t *testing.T) {
	sm, dir, _ := newEndAttrManager(t)
	srv := NewServer(NewHub(), sm, "h", "", nil, nil, "")
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	sess := &session.Session{
		ID: "sb01", FullID: "h-sb01", Hostname: "h", TmuxSession: "cs-sb01", BackendFamily: "claude-code",
		ProjectDir: "/work/sub-hook", Task: "subagent hook", State: session.StateRunning, CreatedAt: now, UpdatedAt: now,
	}
	if err := sm.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := session.NewTracker(dir, sess); err != nil {
		t.Fatal(err)
	}
	sub := session.ClaudeSubagentDir(sess.ProjectDir, sess.FullID)
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","isSidechain":true,"message":{"id":"m1","usage":{"input_tokens":7,"output_tokens":70},"content":[]}}` + "\n"
	if err := os.WriteFile(filepath.Join(sub, "agent-x1.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	post := func(event string) {
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.FullID+"/hook-event", bytes.NewReader([]byte(`{"event":"`+event+`"}`)))
		rr := httptest.NewRecorder()
		srv.handleSessionHookEvent(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status %d %s", event, rr.Code, rr.Body.String())
		}
	}
	post("Stop")
	time.Sleep(100 * time.Millisecond)
	if got, _ := sm.GetSession(sess.FullID); got.SubagentTokensOut != 0 {
		t.Fatalf("a Stop of the parent recorded subagent usage: %+v", got.SubagentTokensOut)
	}
	post("SubagentStop")
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := sm.GetSession(sess.FullID)
		if got.SubagentTokensIn == 7 && got.SubagentTokensOut == 70 && got.TokensOut == 70 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("SubagentStop did not record the subagent usage under the parent: sub %d/%d total out %d",
				got.SubagentTokensIn, got.SubagentTokensOut, got.TokensOut)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
