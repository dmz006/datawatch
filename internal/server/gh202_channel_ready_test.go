// GH#202 — channel registrations hijacked across sessions.
//
// Session channel MCP servers register at `claude mcp add --scope user`
// (GH#128/v8.12.0: a per-session CLAUDE_CONFIG_DIR broke auth/onboarding
// worse than this). That means every Claude process on a host loads every
// session's channel entry, so a stray duplicate bridge process can call
// POST /api/channel/ready carrying a real session_id but the wrong port —
// or no session_id at all — and the old handler blindly re-pointed the
// session, silently stealing it from its real, still-running bridge.
//
// These tests cover the fix: handleChannelReady must not move a session to
// a new port while its current one is still answering a health probe.

package server

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func TestHandleChannelReady_DoesNotStealLiveSession(t *testing.T) {
	s := bl90Server(t)
	s.hub = NewHub()

	// A fake bridge process that's still alive on its registered port.
	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer alive.Close()
	alivePort := alive.Listener.Addr().(*net.TCPAddr).Port

	sess := &session.Session{
		FullID:        "testhost-aaaa",
		ID:            "aaaa",
		Hostname:      "testhost",
		State:         session.StateRunning,
		BackendFamily: "claude-code",
		CreatedAt:     time.Now(),
		ChannelReady:  true,
		ChannelPort:   alivePort,
	}
	if err := s.manager.SaveSession(sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// A stray duplicate bridge calls ready with the same session_id but a
	// different port — must be ignored, not steal the live registration.
	body, _ := json.Marshal(map[string]any{"session_id": sess.FullID, "port": 9999})
	req := httptest.NewRequest(http.MethodPost, "/api/channel/ready", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleChannelReady(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ignored_existing_alive" {
		t.Errorf("status = %q, want %q", resp["status"], "ignored_existing_alive")
	}
	got, ok := s.manager.GetSession(sess.FullID)
	if !ok {
		t.Fatal("session vanished")
	}
	if got.ChannelPort != alivePort {
		t.Errorf("ChannelPort changed to %d, want unchanged %d — stray duplicate stole the session", got.ChannelPort, alivePort)
	}

	// The real bridge dies — a re-registration should now be honored.
	alive.Close()
	req2 := httptest.NewRequest(http.MethodPost, "/api/channel/ready", bytes.NewReader(body))
	rr2 := httptest.NewRecorder()
	s.handleChannelReady(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr2.Code, rr2.Body.String())
	}
	got2, _ := s.manager.GetSession(sess.FullID)
	if got2.ChannelPort != 9999 {
		t.Errorf("after bridge death, ChannelPort = %d, want 9999 (re-registration should win once the old port is dead)", got2.ChannelPort)
	}
}

func TestHandleChannelReady_SessionlessFallbackSkipsLiveSessions(t *testing.T) {
	s := bl90Server(t)
	s.hub = NewHub()

	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer alive.Close()
	alivePort := alive.Listener.Addr().(*net.TCPAddr).Port

	live := &session.Session{
		FullID: "testhost-bbbb", ID: "bbbb", Hostname: "testhost",
		State: session.StateRunning, BackendFamily: "claude-code",
		CreatedAt:    time.Now(), // newest — would win the old unconditional "most recent" pick
		ChannelReady: true, ChannelPort: alivePort,
	}
	orphan := &session.Session{
		FullID: "testhost-cccc", ID: "cccc", Hostname: "testhost",
		State: session.StateRunning, BackendFamily: "claude-code",
		CreatedAt: time.Now().Add(-time.Minute), // older, but genuinely has no channel yet
	}
	if err := s.manager.SaveSession(live); err != nil {
		t.Fatal(err)
	}
	if err := s.manager.SaveSession(orphan); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"port": 12345}) // no session_id
	req := httptest.NewRequest(http.MethodPost, "/api/channel/ready", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleChannelReady(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}

	gotLive, _ := s.manager.GetSession(live.FullID)
	if gotLive.ChannelPort != alivePort {
		t.Errorf("live session's port changed to %d, want unchanged %d — session-less fallback stole it", gotLive.ChannelPort, alivePort)
	}
	gotOrphan, _ := s.manager.GetSession(orphan.FullID)
	if gotOrphan.ChannelPort != 12345 {
		t.Errorf("orphan session's port = %d, want 12345 — it should have claimed the session-less registration", gotOrphan.ChannelPort)
	}
}
