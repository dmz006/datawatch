// Tests for the bounded session-list broadcast (ws_session_broadcast.go):
// a state change streams one session to window and delta clients, a bounded
// frame to legacy clients, and does no whole-list work nobody needs.

package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
	"github.com/gorilla/websocket"
)

const deltaFixtureSessions = 300

// seedDeltaSessions stores 100 active sessions and 200 finished ones last
// updated an hour ago, so the default view holds 100 of the 300.
func seedDeltaSessions(t *testing.T, s *Server, now time.Time) (active, aged []string) {
	t.Helper()
	for i := 0; i < deltaFixtureSessions; i++ {
		id := fmt.Sprintf("d%03d", i)
		var sess *session.Session
		if i < 100 {
			sess = windowTestSession(id, session.StateRunning, now.Add(-time.Duration(i)*time.Second))
			active = append(active, sess.FullID)
		} else {
			sess = windowTestSession(id, session.StateComplete, now.Add(-time.Hour-time.Duration(i)*time.Second))
			aged = append(aged, sess.FullID)
		}
		if err := s.manager.SaveSession(sess); err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
	}
	return active, aged
}

// dialWindowClient connects and announces the window form, consuming the
// provisional connect frame, the ack and the one rows frame.
func dialWindowClient(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	conn := dialSessionStream(t, srv)
	if first := readWSFrame(t, conn); first.Type != MsgSessions {
		t.Fatalf("connect frame = %s", first.Type)
	}
	if ack := sayHello(t, conn, `{"session_list":"window"}`); ack.SessionList != "window" {
		t.Fatalf("ack.session_list = %q", ack.SessionList)
	}
	if rows := readWSFrame(t, conn); rows.Type != MsgSessions {
		t.Fatalf("rows frame = %s", rows.Type)
	}
	return conn
}

// changeSession replaces a stored session with a copy whose task differs,
// through the store lock, and returns the stored (live) session.
func changeSession(t *testing.T, s *Server, id string, st session.State, updated time.Time) *session.Session {
	t.Helper()
	sess := windowTestSession(id, st, updated)
	sess.Task = "changed " + id
	if err := s.manager.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	live, ok := s.manager.GetSession(sess.FullID)
	if !ok {
		t.Fatalf("session %s not held", sess.FullID)
	}
	return live
}

func broadcastAndWait(s *Server) {
	s.broadcastSessionsAsync()
	s.broadcastWG.Wait()
}

func rowFullID(t *testing.T, f WSMessage) string {
	t.Helper()
	var d struct {
		Session struct {
			FullID string `json:"full_id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(f.Data, &d); err != nil {
		t.Fatalf("decode session_state: %v", err)
	}
	return d.Session.FullID
}

func TestWSBroadcastDelta_OneChangeAmongManyStreamsOneSessionState(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	now := time.Now()
	seedDeltaSessions(t, s, now)
	conn := dialWindowClient(t, srv)

	changeSession(t, s, "d007", session.StateRunning, now)
	broadcastAndWait(s)

	frames := drainUntilSentinel(t, s.hub, conn)[0]
	if len(frames) != 1 || frames[0].Type != MsgSessionState {
		t.Fatalf("frames = %v, want exactly one session_state", frameTypes(frames))
	}
	if got := rowFullID(t, frames[0]); got != "testhost-d007" {
		t.Errorf("session_state for %q, want testhost-d007", got)
	}
	// A repeat with nothing changed streams nothing.
	broadcastAndWait(s)
	if extra := drainUntilSentinel(t, s.hub, conn)[0]; len(extra) != 0 {
		t.Errorf("unchanged pass streamed %v", frameTypes(extra))
	}
}

func TestWSBroadcastDelta_DeletedSessionStreamsOneRemoved(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	now := time.Now()
	seedDeltaSessions(t, s, now)
	if err := s.manager.SaveSession(windowTestSession("gone", session.StateComplete, now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	conn := dialWindowClient(t, srv)

	if err := s.manager.Delete("testhost-gone", false); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	broadcastAndWait(s)

	frames := drainUntilSentinel(t, s.hub, conn)[0]
	if len(frames) != 1 || frames[0].Type != MsgSessionRemoved {
		t.Fatalf("frames = %v, want exactly one session_removed", frameTypes(frames))
	}
	var d SessionRemovedData
	if err := json.Unmarshal(frames[0].Data, &d); err != nil || d.SessionID != "testhost-gone" {
		t.Errorf("session_removed = %s (%v), want testhost-gone", frames[0].Data, err)
	}
}

func TestWSBroadcastDelta_AgedOutSessionStreamsNothingAndReentersAsState(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	now := time.Now()
	seedDeltaSessions(t, s, now)
	// Finished two minutes ago: in the default 5-minute view.
	if err := s.manager.SaveSession(windowTestSession("fade", session.StateComplete, now.Add(-2*time.Minute))); err != nil {
		t.Fatal(err)
	}
	conn := dialWindowClient(t, srv)

	// Narrow the window so the session ages out. It is still held: no frame,
	// and above all no session_removed.
	s.cfg.Server.RecentSessionMinutes = 1
	broadcastAndWait(s)
	if frames := drainUntilSentinel(t, s.hub, conn)[0]; len(frames) != 0 {
		t.Fatalf("aged-out session streamed %v, want nothing", frameTypes(frames))
	}

	// It re-enters the window: a changed session_state.
	s.cfg.Server.RecentSessionMinutes = 5
	broadcastAndWait(s)
	frames := drainUntilSentinel(t, s.hub, conn)[0]
	if len(frames) != 1 || frames[0].Type != MsgSessionState || rowFullID(t, frames[0]) != "testhost-fade" {
		t.Fatalf("re-entry frames = %v, want one session_state for testhost-fade", frameTypes(frames))
	}
}

func TestWSBroadcastDelta_RestartedFinishedSessionAppearsAsState(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	now := time.Now()
	seedDeltaSessions(t, s, now)
	conn := dialWindowClient(t, srv)

	// d150 is finished and an hour old, so the client never saw it; a restart
	// makes it active with a fresh UpdatedAt.
	changeSession(t, s, "d150", session.StateRunning, now)
	broadcastAndWait(s)

	frames := drainUntilSentinel(t, s.hub, conn)[0]
	if len(frames) != 1 || frames[0].Type != MsgSessionState || rowFullID(t, frames[0]) != "testhost-d150" {
		t.Fatalf("frames = %v, want one session_state for testhost-d150", frameTypes(frames))
	}
}

func TestWSBroadcastDelta_FinishedOldSessionChangeIsNeverStreamed(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	now := time.Now()
	seedDeltaSessions(t, s, now)
	windowConn := dialWindowClient(t, srv)
	deltaConn := dialSessionStream(t, srv)
	readWSFrame(t, deltaConn)
	sayHello(t, deltaConn, `{"session_list":"delta"}`)

	// Still finished and still an hour old, but its content changed.
	changeSession(t, s, "d180", session.StateComplete, now.Add(-time.Hour))
	live, _ := s.manager.GetSession("testhost-d180")
	srvHTTP := &HTTPServer{hub: s.hub, manager: s.manager}
	srvHTTP.NotifyStateChange(live, session.StateComplete)
	broadcastAndWait(s)

	for i, frames := range drainUntilSentinel(t, s.hub, windowConn, deltaConn) {
		if len(frames) != 0 {
			t.Errorf("client %d got %v for an aged-out finished session", i, frameTypes(frames))
		}
	}
}

func TestWSBroadcastDelta_LegacyClientGetsOneBoundedFrame(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	now := time.Now()
	seedDeltaSessions(t, s, now)
	conn := dialSessionStream(t, srv)
	if first := readWSFrame(t, conn); first.Type != MsgSessions {
		t.Fatalf("connect frame = %s", first.Type)
	}

	changeSession(t, s, "d003", session.StateRunning, now)
	broadcastAndWait(s)

	frames := drainUntilSentinel(t, s.hub, conn)[0]
	if len(frames) != 1 || frames[0].Type != MsgSessions {
		t.Fatalf("frames = %v, want exactly one sessions frame", frameTypes(frames))
	}
	d := decodeRecordsFrame(t, frames[0])
	if len(d.Sessions) != 100 || d.Total != deltaFixtureSessions || !d.More {
		t.Errorf("sessions=%d total=%d more=%v, want 100 / %d / true", len(d.Sessions), d.Total, d.More, deltaFixtureSessions)
	}
	if d.WindowMinutes != 5 {
		t.Errorf("window_minutes = %d, want 5", d.WindowMinutes)
	}
	for _, row := range d.Sessions {
		if st := row["state"]; st != string(session.StateRunning) {
			t.Fatalf("legacy frame carries %v session %v; only default-view sessions belong", st, row["full_id"])
		}
	}
}

func TestWSBroadcastDelta_NotifyStateChangeMixedClients(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	now := time.Now()
	seedDeltaSessions(t, s, now)
	windowConn := dialWindowClient(t, srv)
	legacyConn := dialSessionStream(t, srv)
	if first := readWSFrame(t, legacyConn); first.Type != MsgSessions {
		t.Fatalf("connect frame = %s", first.Type)
	}

	live := changeSession(t, s, "d010", session.StateRunning, now)
	(&HTTPServer{hub: s.hub, manager: s.manager}).NotifyStateChange(live, session.StateRunning)

	all := drainUntilSentinel(t, s.hub, windowConn, legacyConn)
	if w := all[0]; len(w) != 1 || w[0].Type != MsgSessionState || rowFullID(t, w[0]) != "testhost-d010" {
		t.Errorf("window client frames = %v, want one session_state for testhost-d010", frameTypes(w))
	}
	l := all[1]
	if len(l) != 2 || l[0].Type != MsgSessions || l[1].Type != MsgSessionState {
		t.Errorf("legacy client frames = %v, want the bounded sessions frame then session_state", frameTypes(l))
	}
	if len(l) > 0 {
		if d := decodeRecordsFrame(t, l[0]); len(d.Sessions) != 100 || !d.More {
			t.Errorf("legacy sessions frame: %d sessions more=%v, want 100 / true", len(d.Sessions), d.More)
		}
	}
}

func TestWSBroadcastDelta_WindowOnlyNeverBuildsTheLegacyFrame(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	now := time.Now()
	seedDeltaSessions(t, s, now)
	if err := s.manager.SaveSession(windowTestSession("gone", session.StateComplete, now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	windowConn := dialWindowClient(t, srv)
	deltaConn := dialSessionStream(t, srv)
	readWSFrame(t, deltaConn)
	sayHello(t, deltaConn, `{"session_list":"delta"}`)

	srvHTTP := &HTTPServer{hub: s.hub, manager: s.manager}
	live := changeSession(t, s, "d020", session.StateRunning, now)
	srvHTTP.NotifyStateChange(live, session.StateRunning)
	srvHTTP.NotifyNeedsInput(live, "prompt?")
	broadcastAndWait(s)
	if err := s.manager.Delete("testhost-gone", false); err != nil {
		t.Fatal(err)
	}
	broadcastAndWait(s)

	drainUntilSentinel(t, s.hub, windowConn, deltaConn)
	if n := s.hub.legacyFrameBuilds.Load(); n != 0 {
		t.Fatalf("legacy sessions frame built %d times with only window and delta clients connected", n)
	}

	// Control: once a legacy client is connected the frame is built.
	legacyConn := dialSessionStream(t, srv)
	readWSFrame(t, legacyConn)
	changeSession(t, s, "d021", session.StateRunning, now)
	broadcastAndWait(s)
	drainUntilSentinel(t, s.hub, legacyConn)
	if n := s.hub.legacyFrameBuilds.Load(); n != 1 {
		t.Fatalf("legacy sessions frame built %d times with a legacy client connected, want 1", n)
	}
}

func TestWSBroadcastDelta_NoClientDoesNoListWork(t *testing.T) {
	s, _ := newSessionStreamServer(t, "")
	seedDeltaSessions(t, s, time.Now())
	calls := 0
	s.hub.setSessionViewSource(func(func(session.State, time.Time) bool) ([]*session.Session, map[string]struct{}) {
		calls++
		return nil, nil
	})

	broadcastAndWait(s)
	live, _ := s.manager.GetSession("testhost-d001")
	srvHTTP := &HTTPServer{hub: s.hub, manager: s.manager}
	srvHTTP.NotifyStateChange(live, session.StateRunning)
	srvHTTP.NotifyNeedsInput(live, "prompt?")
	if calls != 0 {
		t.Errorf("session view source called %d times with no client connected", calls)
	}
}

func TestWSBroadcastDelta_FrameSizeBoundedWith4000Sessions(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	now := time.Now()
	for i := 0; i < 4000; i++ {
		st, updated := session.StateComplete, now.Add(-time.Hour)
		if i < 60 {
			st, updated = session.StateRunning, now
		}
		sess := windowTestSession(fmt.Sprintf("s%04d", i), st, updated)
		sess.Task = fmt.Sprintf("%0400d", i)
		sess.LastResponse = sess.Task
		if err := s.manager.SaveSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	windowConn := dialWindowClient(t, srv)
	legacyConn := dialSessionStream(t, srv)
	readWSFrame(t, legacyConn)

	changeSession(t, s, "s0005", session.StateRunning, now)
	broadcastAndWait(s)

	const budget = 300 * 1024
	for i, frames := range drainUntilSentinel(t, s.hub, windowConn, legacyConn) {
		if len(frames) == 0 {
			t.Fatalf("client %d got no frame", i)
		}
		for _, f := range frames {
			if len(f.Data) > budget {
				t.Errorf("client %d %s frame is %d bytes, over the %d budget", i, f.Type, len(f.Data), budget)
			}
		}
	}
}
