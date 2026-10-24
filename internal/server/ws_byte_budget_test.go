// Byte-budget test for the WebSocket session list on the real hub paths
// (ws_session_window.go, ws_session_broadcast.go; wire behaviour in the
// header of ws_session_stream.go). Every frame is measured as the raw
// websocket message it arrives as, against a 4,000-session fixture whose
// legacy full list is about 35 MB.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/gorilla/websocket"
)

// Fixture shape. At these text sizes the legacy full list, every session
// with its text fields, marshals to about 35 MB.
const (
	budgetFixtureSessions = 4000 // sessions the daemon holds
	budgetFixtureActive   = 24   // active: always in the default view
	budgetFixtureFresh    = 16   // finished within the window: in the default view
	budgetTaskChars       = 3000 // task text per session
	budgetPromptChars     = 2500 // prompt context per session
	budgetResponseChars   = 3000 // last response per session
)

// Byte budgets. Each one protects a single path.
const (
	// budgetMaxFrame caps any single websocket message. It keeps the
	// connect-time and hello frames from carrying the whole list again: a
	// 4,000-session daemon must not push ~35 MB at one client.
	budgetMaxFrame = 300 * 1024
	// budgetMaxStateFrame caps the session_state for one changed session. It
	// protects the window client's per-change cost, one summary row.
	budgetMaxStateFrame = 8 * 1024
	// budgetMaxIdleBytes caps everything a window client receives for
	// budgetIdleChanges changes in a row. It is the idle-view budget in
	// miniature.
	budgetMaxIdleBytes = 200 * 1024
	// budgetIdleChanges is how many state changes the idle run applies.
	budgetIdleChanges = 20
	// budgetMinFullList is the least the fixture's full-list marshal may be.
	// It proves the frame cap is meaningful: the old connect frame was this
	// whole list, so a passing budget means the list was not sent.
	budgetMinFullList = 10 << 20
)

// budgetSentinel ends a drain. The hub delivers in queue order, so every
// frame queued before the notification has been read when it arrives.
const budgetSentinel = "byte-budget-sentinel"

// budgetFrame is one websocket message as it arrived.
type budgetFrame struct {
	msg   WSMessage
	bytes int
}

// budgetReader reads frames and keeps the largest one seen across the test.
type budgetReader struct {
	t       *testing.T
	largest int
}

func (r *budgetReader) next(conn *websocket.Conn) budgetFrame {
	r.t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(wsTestFrameTimeout))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		r.t.Fatalf("read: %v", err)
	}
	var m WSMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		r.t.Fatalf("decode frame: %v", err)
	}
	if len(raw) > r.largest {
		r.largest = len(raw)
	}
	return budgetFrame{msg: m, bytes: len(raw)}
}

// drain sends the sentinel and returns, per connection, the frames received
// before it. The sentinel itself is not returned.
func (r *budgetReader) drain(h *Hub, conns ...*websocket.Conn) [][]budgetFrame {
	r.t.Helper()
	h.BroadcastNotification(budgetSentinel)
	out := make([][]budgetFrame, len(conns))
	for i, conn := range conns {
		for {
			f := r.next(conn)
			if f.msg.Type == MsgNotification && strings.Contains(string(f.msg.Data), budgetSentinel) {
				break
			}
			out[i] = append(out[i], f)
		}
	}
	return out
}

// hello sends a hello and reads frames up to hello_ack. The frames that came
// first are returned too.
func (r *budgetReader) hello(conn *websocket.Conn, data string) (before []budgetFrame, ack HelloAckData) {
	r.t.Helper()
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello","data":`+data+`}`)); err != nil {
		r.t.Fatalf("write hello: %v", err)
	}
	for {
		f := r.next(conn)
		if f.msg.Type != MsgHelloAck {
			before = append(before, f)
			continue
		}
		if err := json.Unmarshal(f.msg.Data, &ack); err != nil {
			r.t.Fatalf("decode hello_ack: %v", err)
		}
		return before, ack
	}
}

// budgetTypes lists the message types of frames, for failure messages.
func budgetTypes(frames []budgetFrame) []MessageType {
	ts := make([]MessageType, 0, len(frames))
	for _, f := range frames {
		ts = append(ts, f.msg.Type)
	}
	return ts
}

// budgetCount counts the frames of one type.
func budgetCount(frames []budgetFrame, typ MessageType) int {
	n := 0
	for _, f := range frames {
		if f.msg.Type == typ {
			n++
		}
	}
	return n
}

// budgetHasField reports whether name is in fields.
func budgetHasField(fields []string, name string) bool {
	for _, f := range fields {
		if f == name {
			return true
		}
	}
	return false
}

// budgetText returns n characters of filler, led by a label that names the
// session and its revision, so every revision fingerprints differently.
func budgetText(label string, rev, n int) string {
	head := fmt.Sprintf("%s rev %d: ", label, rev)
	filler := strings.Repeat("lorem ipsum dolor sit amet consectetur adipiscing elit ", n/40+1)
	return head + filler[:n-len(head)]
}

// budgetSession builds fixture session i. rev changes its text without
// changing its identity, which is how the test makes a session "change".
func budgetSession(i int, st session.State, updated time.Time, rev int) *session.Session {
	id := fmt.Sprintf("b%04d", i)
	return &session.Session{
		ID: id, FullID: "testhost-" + id, Name: "budget-" + id, State: st,
		ProjectDir:    "/home/user/workspace/budget",
		Task:          budgetText(id+" task", rev, budgetTaskChars),
		PromptContext: budgetText(id+" prompt", rev, budgetPromptChars),
		LastResponse:  budgetText(id+" response", rev, budgetResponseChars),
		CreatedAt:     updated.Add(-time.Hour),
		UpdatedAt:     updated,
	}
}

// seedBudgetSessions stores the fixture and returns every session it wrote,
// in order, so the test can measure the legacy full list.
func seedBudgetSessions(t *testing.T, s *Server, now time.Time) []*session.Session {
	t.Helper()
	done := []session.State{session.StateComplete, session.StateFailed, session.StateKilled}
	all := make([]*session.Session, 0, budgetFixtureSessions)
	for i := 0; i < budgetFixtureSessions; i++ {
		var sess *session.Session
		switch {
		case i < budgetFixtureActive:
			sess = budgetSession(i, session.StateRunning, now.Add(-time.Duration(i)*time.Second), 0)
		case i < budgetFixtureActive+budgetFixtureFresh:
			// Finished 10 s to 160 s ago: inside the 5-minute window.
			k := i - budgetFixtureActive + 1
			sess = budgetSession(i, done[i%len(done)], now.Add(-time.Duration(10*k)*time.Second), 0)
		default:
			sess = budgetSession(i, done[i%len(done)], now.Add(-time.Hour-time.Duration(i)*time.Second), 0)
		}
		if err := s.manager.SaveSession(sess); err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
		all = append(all, sess)
	}
	return all
}

// budgetFullListBytes is the size of the legacy full list: every session,
// text and all, in one sessions frame.
func budgetFullListBytes(t *testing.T, all []*session.Session) int {
	t.Helper()
	b, err := json.Marshal(SessionsData{Sessions: all})
	if err != nil {
		t.Fatalf("marshal full list: %v", err)
	}
	return len(b)
}

// changeBudgetSession stores revision rev of active fixture session i and
// sends it through HTTPServer.NotifyStateChange, the path a real change takes.
func changeBudgetSession(t *testing.T, s *Server, hs *HTTPServer, i, rev int) {
	t.Helper()
	sess := budgetSession(i, session.StateRunning, time.Now(), rev)
	if err := s.manager.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	live, ok := s.manager.GetSession(sess.FullID)
	if !ok {
		t.Fatalf("session %s not held", sess.FullID)
	}
	hs.NotifyStateChange(live, session.StateRunning)
}

// newByteBudgetServer starts a hub and /ws endpoint with the default config,
// so the window and the summary text cut are the shipped values.
func newByteBudgetServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s := newTestServer(t, nil, nil)
	s.cfg = config.DefaultConfig()
	s.hub = NewHub()
	s.wireHubSessionStream()
	go s.hub.Run()
	srv := httptest.NewServer(http.HandlerFunc(s.handleWS))
	t.Cleanup(srv.Close)
	return s, srv
}

// waitForClients waits until the hub holds want clients, so a client that
// closed is no longer counted.
func waitForClients(t *testing.T, h *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(wsTestFrameTimeout)
	for h.ClientCount() != want {
		if time.Now().After(deadline) {
			t.Fatalf("clients = %d, want %d", h.ClientCount(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// budgetRowsFrame is the window client's rows frame as the test decodes it.
type budgetRowsFrame struct {
	Sessions      []map[string]json.RawMessage `json:"sessions"`
	WindowMinutes int                          `json:"window_minutes"`
	Total         int                          `json:"total"`
	More          bool                         `json:"more"`
}

// checkBudgetRowsFrame asserts the window client's rows frame: the
// default-view summary rows only, with no task, prompt or response text.
func checkBudgetRowsFrame(t *testing.T, f budgetFrame, window, textChars int) {
	t.Helper()
	if f.msg.Type != MsgSessions {
		t.Fatalf("rows frame = %s, want sessions", f.msg.Type)
	}
	if f.bytes > budgetMaxFrame {
		t.Errorf("rows frame %d bytes, budget %d", f.bytes, budgetMaxFrame)
	}
	var d budgetRowsFrame
	if err := json.Unmarshal(f.msg.Data, &d); err != nil {
		t.Fatalf("decode rows frame: %v", err)
	}
	if want := budgetFixtureActive + budgetFixtureFresh; len(d.Sessions) != want || d.Total != budgetFixtureSessions || !d.More || d.WindowMinutes != window {
		t.Errorf("rows frame: %d rows total=%d more=%v window=%d; want %d / %d / true / %d",
			len(d.Sessions), d.Total, d.More, d.WindowMinutes, want, budgetFixtureSessions, window)
	}
	for _, row := range d.Sessions {
		for _, key := range []string{"task", "prompt_context", "last_response", "last_summary_long"} {
			if _, ok := row[key]; ok {
				t.Fatalf("summary row carries %q; rows must be summary-shaped", key)
			}
		}
		var line string
		if raw, ok := row["status_line"]; ok {
			if err := json.Unmarshal(raw, &line); err != nil {
				t.Fatalf("decode status_line: %v", err)
			}
		}
		// A cut keeps textChars runes and appends one ellipsis rune.
		if n := utf8.RuneCountInString(line); n > textChars+1 {
			t.Fatalf("status_line %d runes, want at most %d", n, textChars+1)
		}
	}
}

// budgetRecordsFrame is the bounded legacy sessions frame as the test
// decodes it. Records are read for their full_id and truncated_fields only:
// session.Session has its own UnmarshalJSON, which the embedded pointer in
// sessionListRecord would promote, so the typed record is not decoded here.
type budgetRecordsFrame struct {
	Sessions []struct {
		FullID          string   `json:"full_id"`
		TruncatedFields []string `json:"truncated_fields"`
	} `json:"sessions"`
	WindowMinutes int  `json:"window_minutes"`
	Total         int  `json:"total"`
	More          bool `json:"more"`
}

// checkBudgetLegacyFrame asserts the bounded legacy sessions frame: within
// budget, the default view only, long text cut and flagged.
func checkBudgetLegacyFrame(t *testing.T, f budgetFrame, window int) {
	t.Helper()
	if f.msg.Type != MsgSessions {
		t.Fatalf("legacy frame = %s, want sessions", f.msg.Type)
	}
	if f.bytes > budgetMaxFrame {
		t.Errorf("legacy sessions frame %d bytes, budget %d", f.bytes, budgetMaxFrame)
	}
	var d budgetRecordsFrame
	if err := json.Unmarshal(f.msg.Data, &d); err != nil {
		t.Fatalf("decode legacy frame: %v", err)
	}
	if want := budgetFixtureActive + budgetFixtureFresh; len(d.Sessions) != want || d.Total != budgetFixtureSessions || !d.More || d.WindowMinutes != window {
		t.Errorf("legacy frame: %d sessions total=%d more=%v window=%d; want %d / %d / true / %d",
			len(d.Sessions), d.Total, d.More, d.WindowMinutes, want, budgetFixtureSessions, window)
	}
	for _, rec := range d.Sessions {
		if !budgetHasField(rec.TruncatedFields, "task") || !budgetHasField(rec.TruncatedFields, "last_response") {
			t.Fatalf("record %s not cut: truncated_fields=%v", rec.FullID, rec.TruncatedFields)
		}
	}
}

// TestWSByteBudget_FourThousandSessions proves the byte budget on the real
// hub paths: the connect frame, the hello rows frame, one state change for
// each client kind, a run of state changes for a window client, and the
// largest frame across all of it.
func TestWSByteBudget_FourThousandSessions(t *testing.T) {
	s, srv := newByteBudgetServer(t)
	window := s.cfg.Server.RecentSessionMinutes
	if window <= 0 {
		window = defaultRecentSessionMinutes
	}
	textChars := s.cfg.Server.SessionSummaryTextChars
	now := time.Now()
	all := seedBudgetSessions(t, s, now)

	full := budgetFullListBytes(t, all)
	t.Logf("fixture: %d sessions, legacy full list %d bytes (%.1f MB)", len(all), full, float64(full)/(1<<20))
	if full <= budgetMinFullList {
		t.Fatalf("fixture full list is %d bytes, want more than %d: the frame cap could pass without the list being cut", full, budgetMinFullList)
	}

	r := &budgetReader{t: t}
	hs := &HTTPServer{hub: s.hub, manager: s.manager}

	// (1) A client that sends no hello gets the bounded legacy frame.
	legacy := dialSessionStream(t, srv)
	connect := r.next(legacy)
	t.Logf("(1) connect, no hello: %d bytes", connect.bytes)
	checkBudgetLegacyFrame(t, connect, window)

	// (2) A client that says hello window gets one summary-row frame.
	windowClient := dialSessionStream(t, srv)
	if first := r.next(windowClient); first.msg.Type != MsgSessions {
		t.Fatalf("window connect frame = %s, want sessions", first.msg.Type)
	}
	before, ack := r.hello(windowClient, `{"session_list":"window"}`)
	if ack.SessionList != sessionListWindow {
		t.Fatalf("hello_ack session_list = %q, want window", ack.SessionList)
	}
	if len(before) != 0 {
		t.Fatalf("frames before hello_ack = %v, want none beyond the connect frame", budgetTypes(before))
	}
	rows := r.next(windowClient)
	t.Logf("(2) hello window: rows frame %d bytes", rows.bytes)
	checkBudgetRowsFrame(t, rows, window, textChars)

	// (3) One state change: the window client gets one small session_state
	// and no sessions frame; the legacy client gets one bounded sessions
	// frame plus the per-change session_state, as before.
	changeBudgetSession(t, s, hs, 3, 1)
	got := r.drain(s.hub, windowClient, legacy)
	wf := got[0]
	if len(wf) != 1 || wf[0].msg.Type != MsgSessionState {
		t.Fatalf("window frames = %v, want one session_state", budgetTypes(wf))
	}
	if wf[0].bytes > budgetMaxStateFrame {
		t.Errorf("window session_state %d bytes, budget %d", wf[0].bytes, budgetMaxStateFrame)
	}
	if got := rowFullID(t, wf[0].msg); got != "testhost-b0003" {
		t.Errorf("window session_state for %q, want testhost-b0003", got)
	}
	lf := got[1]
	if budgetCount(lf, MsgSessions) != 1 || len(lf) != 2 {
		t.Fatalf("legacy frames = %v, want the bounded sessions frame then session_state", budgetTypes(lf))
	}
	t.Logf("(3) one state change: window session_state %d bytes; legacy sessions frame %d bytes, session_state %d bytes",
		wf[0].bytes, lf[0].bytes, lf[1].bytes)
	checkBudgetLegacyFrame(t, lf[0], window)

	// (4) The idle view in miniature: the legacy client leaves, then the
	// window client alone receives 20 changes in a row.
	_ = legacy.Close()
	waitForClients(t, s.hub, 1)
	for k := 0; k < budgetIdleChanges; k++ {
		changeBudgetSession(t, s, hs, k%budgetFixtureActive, k+2)
	}
	idle := r.drain(s.hub, windowClient)[0]
	idleBytes := 0
	for _, f := range idle {
		idleBytes += f.bytes
		if f.msg.Type != MsgSessionState || f.bytes > budgetMaxStateFrame {
			t.Errorf("idle frame %s of %d bytes; want session_state within %d", f.msg.Type, f.bytes, budgetMaxStateFrame)
		}
	}
	if n := len(idle); n == 0 || n > budgetIdleChanges {
		t.Errorf("%d changes sent %d frames, want 1..%d", budgetIdleChanges, n, budgetIdleChanges)
	}
	if idleBytes > budgetMaxIdleBytes {
		t.Errorf("idle run %d bytes, budget %d", idleBytes, budgetMaxIdleBytes)
	}
	t.Logf("(4) %d changes: %d frames, %d bytes total (budget %d)", budgetIdleChanges, len(idle), idleBytes, budgetMaxIdleBytes)

	// (5) The largest single frame across the whole test.
	t.Logf("(5) largest single frame: %d bytes (budget %d)", r.largest, budgetMaxFrame)
	if r.largest > budgetMaxFrame {
		t.Errorf("largest frame %d bytes, budget %d", r.largest, budgetMaxFrame)
	}
}
