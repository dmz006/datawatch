// Tests for the WebSocket hello handshake and the delta / summary session
// stream (ws_session_stream.go). BL432 / GH#242.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/gorilla/websocket"
)

// wsTestFrameTimeout bounds a single WS read in tests. It is generous
// (~10x the old 3 s hard-coded deadline) and well above actual frame
// delivery time, since frames are enqueued on the hub before the read
// begins; the bound only guards against a genuinely stuck read.
const wsTestFrameTimeout = 30 * time.Second

// newSessionStreamServer starts a hub + /ws endpoint with the given
// server.ws_session_list_default.
func newSessionStreamServer(t *testing.T, listDefault string) (*Server, *httptest.Server) {
	t.Helper()
	s := newTestServer(t, nil, nil)
	s.cfg = config.DefaultConfig()
	s.cfg.Server.WSSessionListDefault = listDefault
	s.cfg.Server.SessionSummaryTextChars = 30
	s.hub = NewHub()
	s.wireHubSessionStream()
	go s.hub.Run()
	srv := httptest.NewServer(http.HandlerFunc(s.handleWS))
	t.Cleanup(srv.Close)
	return s, srv
}

func dialSessionStream(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+mustHost(t, srv.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readWSFrame(t *testing.T, conn *websocket.Conn) WSMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(wsTestFrameTimeout))
	_, b, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m WSMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return m
}

// sayHello sends hello and returns the hello_ack, skipping earlier frames.
func sayHello(t *testing.T, conn *websocket.Conn, data string) HelloAckData {
	t.Helper()
	msg := `{"type":"hello","data":` + data + `}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	for {
		m := readWSFrame(t, conn)
		if m.Type != MsgHelloAck {
			continue
		}
		var ack HelloAckData
		if err := json.Unmarshal(m.Data, &ack); err != nil {
			t.Fatalf("decode ack: %v", err)
		}
		return ack
	}
}

// drainUntilSentinel broadcasts a notification and returns every frame
// the connection receives before it. The hub delivers in queue order, so
// everything broadcast earlier arrives first.
func drainUntilSentinel(t *testing.T, h *Hub, conns ...*websocket.Conn) [][]WSMessage {
	t.Helper()
	h.BroadcastNotification("sentinel")
	out := make([][]WSMessage, len(conns))
	for i, conn := range conns {
		for {
			m := readWSFrame(t, conn)
			if m.Type == MsgNotification && strings.Contains(string(m.Data), "sentinel") {
				break
			}
			out[i] = append(out[i], m)
		}
	}
	return out
}

func frameTypes(frames []WSMessage) []MessageType {
	var ts []MessageType
	for _, f := range frames {
		ts = append(ts, f.Type)
	}
	return ts
}

// deltaFrames decodes session_state / session_removed frames, and fails on
// any sessions frame (delta clients must never see one).
func deltaFrames(t *testing.T, frames []WSMessage) (states []session.Session, removed []string) {
	t.Helper()
	for _, f := range frames {
		switch f.Type {
		case MsgSessionState:
			var d struct {
				Session session.Session `json:"session"`
			}
			if err := json.Unmarshal(f.Data, &d); err != nil {
				t.Fatalf("decode session_state: %v", err)
			}
			states = append(states, d.Session)
		case MsgSessionRemoved:
			var d SessionRemovedData
			if err := json.Unmarshal(f.Data, &d); err != nil {
				t.Fatalf("decode session_removed: %v", err)
			}
			removed = append(removed, d.SessionID)
		case MsgSessions:
			t.Fatalf("delta client received a sessions frame")
		}
	}
	return states, removed
}

func testSession(id, name string, st session.State) *session.Session {
	return &session.Session{ID: id, FullID: "testhost-" + id, Name: name, State: st, Task: "task " + id}
}

func TestWSSessionStream_NoHelloGetsFullList(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	conn := dialSessionStream(t, srv)
	if m := readWSFrame(t, conn); m.Type != MsgSessions {
		t.Fatalf("first frame = %s, want the connect-time sessions frame", m.Type)
	}
	s.hub.BroadcastSessions([]*session.Session{testSession("a", "one", session.StateRunning)})
	frames := drainUntilSentinel(t, s.hub, conn)[0]
	if len(frames) != 1 || frames[0].Type != MsgSessions {
		t.Fatalf("frames = %v, want exactly one sessions frame", frameTypes(frames))
	}
	var d SessionsData
	if err := json.Unmarshal(frames[0].Data, &d); err != nil || len(d.Sessions) != 1 || d.Sessions[0].FullID != "testhost-a" {
		t.Fatalf("sessions payload = %s (%v)", frames[0].Data, err)
	}
}

func TestWSSessionStream_HelloAckEcho(t *testing.T) {
	cases := []struct {
		name, hello        string
		wantList, wantView string
	}{
		{"delta summary", `{"session_list":"delta","session_view":"summary"}`, "delta", "summary"},
		{"full explicit", `{"session_list":"full","session_view":"full"}`, "full", "full"},
		{"empty hello keeps defaults", `{}`, "full", "full"},
		{"unknown keys ignored, bad value unchanged", `{"session_list":"bogus","output_scope":"x","session_view":"summary"}`, "full", "summary"},
		{"malformed data still acked", `"nope"`, "full", "full"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := newSessionStreamServer(t, "")
			conn := dialSessionStream(t, srv)
			ack := sayHello(t, conn, tc.hello)
			if ack.SessionList != tc.wantList || ack.SessionView != tc.wantView {
				t.Errorf("ack = %+v, want list=%s view=%s", ack, tc.wantList, tc.wantView)
			}
			want := map[string]bool{"ws.hello": false, "ws.session_list_delta": false}
			for _, c := range ack.Capabilities {
				if _, ok := want[c]; ok {
					want[c] = true
				}
			}
			for c, ok := range want {
				if !ok {
					t.Errorf("ack capabilities %v missing %s", ack.Capabilities, c)
				}
			}
		})
	}
}

func TestWSSessionStream_HelloCanSwitchBack(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	conn := dialSessionStream(t, srv)
	sayHello(t, conn, `{"session_list":"delta"}`)
	if ack := sayHello(t, conn, `{"session_list":"full"}`); ack.SessionList != "full" {
		t.Fatalf("ack = %+v", ack)
	}
	s.hub.BroadcastSessions([]*session.Session{testSession("a", "one", session.StateRunning)})
	frames := drainUntilSentinel(t, s.hub, conn)[0]
	if len(frames) != 1 || frames[0].Type != MsgSessions {
		t.Fatalf("frames = %v, want one sessions frame after switching back to full", frameTypes(frames))
	}
}

func TestWSSessionStream_DeltaDiff(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	conn := dialSessionStream(t, srv)
	if ack := sayHello(t, conn, `{"session_list":"delta"}`); ack.SessionList != "delta" || ack.SessionView != "full" {
		t.Fatalf("ack = %+v", ack)
	}

	a := testSession("a", "one", session.StateRunning)
	b := testSession("b", "two", session.StateRunning)
	steps := []struct {
		name        string
		list        func() []*session.Session
		wantStates  []string
		wantRemoved []string
	}{
		{"add two", func() []*session.Session { return []*session.Session{a, b} }, []string{"testhost-a", "testhost-b"}, nil},
		{"unchanged list sends nothing", func() []*session.Session { return []*session.Session{a, b} }, nil, nil},
		{"rename b", func() []*session.Session { b.Name = "renamed"; return []*session.Session{a, b} }, []string{"testhost-b"}, nil},
		{"needs input on a", func() []*session.Session {
			a.State = session.StateWaitingInput
			a.LastPrompt = "Proceed? [y/N]"
			return []*session.Session{a, b}
		}, []string{"testhost-a"}, nil},
		{"remove b, add c", func() []*session.Session {
			return []*session.Session{a, testSession("c", "three", session.StateRunning)}
		}, []string{"testhost-c"}, []string{"testhost-b"}},
		{"remove all", func() []*session.Session { return nil }, nil, []string{"testhost-a", "testhost-c"}},
	}
	for _, st := range steps {
		s.hub.BroadcastSessions(st.list())
		states, removed := deltaFrames(t, drainUntilSentinel(t, s.hub, conn)[0])
		var ids []string
		for _, ss := range states {
			ids = append(ids, ss.FullID)
		}
		if strings.Join(ids, ",") != strings.Join(st.wantStates, ",") {
			t.Errorf("%s: session_state ids = %v, want %v", st.name, ids, st.wantStates)
		}
		if strings.Join(removed, ",") != strings.Join(st.wantRemoved, ",") {
			t.Errorf("%s: session_removed = %v, want %v", st.name, removed, st.wantRemoved)
		}
		if st.name == "rename b" && (len(states) != 1 || states[0].Name != "renamed") {
			t.Errorf("rename payload = %+v", states)
		}
		if st.name == "needs input on a" && (len(states) != 1 || states[0].State != session.StateWaitingInput) {
			t.Errorf("needs-input payload = %+v", states)
		}
	}
}

// NotifyStateChange calls BroadcastSessions then BroadcastSessionState for
// the same change: a delta client must get one session_state, a full client
// the legacy pair.
func TestWSSessionStream_StateChangeNotDuplicated(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	full := dialSessionStream(t, srv)
	readWSFrame(t, full) // connect-time sessions
	sayHello(t, full, `{}`)
	delta := dialSessionStream(t, srv)
	sayHello(t, delta, `{"session_list":"delta"}`)

	a := testSession("a", "one", session.StateRunning)
	s.hub.BroadcastSessions([]*session.Session{a})
	drainUntilSentinel(t, s.hub, full, delta)

	a.State = session.StateComplete
	s.hub.BroadcastSessions([]*session.Session{a})
	s.hub.BroadcastSessionState(a)
	got := drainUntilSentinel(t, s.hub, full, delta)

	if ts := frameTypes(got[0]); len(ts) != 2 || ts[0] != MsgSessions || ts[1] != MsgSessionState {
		t.Errorf("full client frames = %v, want [sessions session_state]", ts)
	}
	states, _ := deltaFrames(t, got[1])
	if len(states) != 1 || states[0].State != session.StateComplete {
		t.Errorf("delta client session_state = %+v, want exactly one complete", states)
	}

	// A BroadcastSessionState for a change BroadcastSessions has not seen
	// still reaches the delta client.
	a.Name = "late rename"
	s.hub.BroadcastSessionState(a)
	states, _ = deltaFrames(t, drainUntilSentinel(t, s.hub, delta)[0])
	if len(states) != 1 || states[0].Name != "late rename" {
		t.Errorf("unseen change: delta states = %+v", states)
	}
}

func TestWSSessionStream_SummaryView(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	delta := dialSessionStream(t, srv)
	sayHello(t, delta, `{"session_list":"delta","session_view":"summary"}`)
	fullSummary := dialSessionStream(t, srv)
	readWSFrame(t, fullSummary)
	sayHello(t, fullSummary, `{"session_view":"summary"}`)
	fullView := dialSessionStream(t, srv)
	sayHello(t, fullView, `{"session_list":"delta"}`)

	long := strings.Repeat("x", 100)
	a := testSession("a", "one", session.StateRunning)
	a.Task = long
	a.LastResponse = "short"
	s.hub.BroadcastSessions([]*session.Session{a})
	s.hub.BroadcastSessionState(a)
	got := drainUntilSentinel(t, s.hub, delta, fullSummary, fullView)

	decode := func(f WSMessage) sessionListRecord {
		// Decoded twice: session.Session has its own UnmarshalJSON, which
		// would swallow truncated_fields if embedded.
		var d struct {
			Session json.RawMessage `json:"session"`
		}
		var extra struct {
			TruncatedFields []string `json:"truncated_fields"`
		}
		var sess session.Session
		if err := json.Unmarshal(f.Data, &d); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if err := json.Unmarshal(d.Session, &sess); err != nil {
			t.Fatalf("decode session: %v", err)
		}
		if err := json.Unmarshal(d.Session, &extra); err != nil {
			t.Fatalf("decode truncated_fields: %v", err)
		}
		return sessionListRecord{Session: &sess, TruncatedFields: extra.TruncatedFields}
	}
	wantTask := strings.Repeat("x", 30) + "…"
	for i, name := range []string{"delta summary", "full summary"} {
		var states []WSMessage
		for _, f := range got[i] {
			if f.Type == MsgSessionState {
				states = append(states, f)
			}
		}
		if len(states) != 1 {
			t.Fatalf("%s: %d session_state frames, want 1 (%v)", name, len(states), frameTypes(got[i]))
		}
		rec := decode(states[0])
		if rec.Task != wantTask || strings.Join(rec.TruncatedFields, ",") != "task" || rec.LastResponse != "short" {
			t.Errorf("%s: task=%q truncated=%v resp=%q", name, rec.Task, rec.TruncatedFields, rec.LastResponse)
		}
	}
	states, _ := deltaFrames(t, got[2])
	if len(states) != 1 || states[0].Task != long {
		t.Errorf("full-view delta client should get the untruncated task, got %+v", states)
	}
	if a.Task != long {
		t.Error("summary view mutated the live session")
	}
}

func TestWSSessionStream_ConfigDefaultDelta(t *testing.T) {
	s, srv := newSessionStreamServer(t, "delta")
	conn := dialSessionStream(t, srv)
	// Wait for the client to actually register on the hub so the
	// broadcast below is ordered against the read that follows, instead
	// of assuming a wall-clock bound.
	deadline := time.Now().Add(wsTestFrameTimeout)
	for s.hub.ClientCount() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("client never registered: ClientCount = %d", s.hub.ClientCount())
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.hub.BroadcastSessions([]*session.Session{testSession("a", "one", session.StateRunning)})
	frames := drainUntilSentinel(t, s.hub, conn)[0]
	// No connect-time list and no sessions frames, only the delta.
	states, _ := deltaFrames(t, frames)
	if len(frames) != 1 || len(states) != 1 || states[0].FullID != "testhost-a" {
		t.Fatalf("frames = %v, want one session_state", frameTypes(frames))
	}
	// hello without session_list reports the configured default.
	if ack := sayHello(t, conn, `{"session_view":"full"}`); ack.SessionList != "delta" {
		t.Errorf("ack = %+v, want session_list=delta from config", ack)
	}
}

// The default is read live at broadcast time. Uses in-process clients so
// every config write is ordered against the hub goroutine by a channel.
func TestWSSessionStream_ConfigDefaultReadLive(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.cfg = config.DefaultConfig()
	s.hub = NewHub()
	s.wireHubSessionStream()
	go s.hub.Run()
	ch := make(chan []byte, 16)
	s.hub.register <- &client{hub: s.hub, send: ch, subscribed: map[string]bool{}}

	next := func() MessageType {
		select {
		case b := <-ch:
			var m WSMessage
			_ = json.Unmarshal(b, &m)
			return m.Type
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
		return ""
	}
	s.hub.BroadcastSessions([]*session.Session{testSession("a", "one", session.StateRunning)})
	if mt := next(); mt != MsgSessions {
		t.Fatalf("default full: got %s", mt)
	}
	applyConfigPatch(s.cfg, map[string]interface{}{"server.ws_session_list_default": "delta"})
	s.hub.BroadcastSessions([]*session.Session{testSession("a", "renamed", session.StateRunning)})
	if mt := next(); mt != MsgSessionState {
		t.Fatalf("after switching to delta: got %s", mt)
	}
}

func TestWSSessionStream_SlowClientDropped(t *testing.T) {
	h := NewHub()
	go h.Run()
	slowDelta := &client{hub: h, send: make(chan []byte, 1), subscribed: map[string]bool{}, sessionList: sessionListDelta}
	// A full-mode client with the same tiny buffer is never offered the
	// delta frames, so it must not be dropped by them.
	slowFull := &client{hub: h, send: make(chan []byte, 1), subscribed: map[string]bool{}}
	h.register <- slowDelta
	h.register <- slowFull

	h.BroadcastSessions(nil) // seeds nothing, no frames for delta
	<-slowFull.send          // the (empty) sessions frame
	h.BroadcastSessions([]*session.Session{testSession("a", "one", session.StateRunning)})
	<-slowFull.send
	h.BroadcastSessions([]*session.Session{testSession("a", "two", session.StateRunning)})
	<-slowFull.send

	// slowDelta got session_state for a, then a second one with its
	// buffer still full → dropped and its channel closed.
	deadline := time.After(2 * time.Second)
	for h.ClientCount() != 1 {
		select {
		case <-deadline:
			t.Fatalf("slow delta client not dropped; clients=%d", h.ClientCount())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, ok := <-slowDelta.send; !ok {
		t.Fatal("expected the one buffered frame before close")
	}
	if _, ok := <-slowDelta.send; ok {
		t.Fatal("slow client's channel should be closed")
	}
	h.mu.RLock()
	_, fullStill := h.clients[slowFull]
	h.mu.RUnlock()
	if !fullStill {
		t.Error("full-mode client was dropped by frames it was never offered")
	}
}

func TestApplyConfigPatch_WSSessionListDefault(t *testing.T) {
	cases := []struct {
		name string
		val  interface{}
		want string
	}{
		{"delta", "delta", "delta"},
		{"case and space normalized", " FULL ", "full"},
		{"invalid ignored", "partial", "full"},
		{"non-string ignored", float64(1), "full"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			applyConfigPatch(cfg, map[string]interface{}{"server.ws_session_list_default": tc.val})
			if cfg.Server.WSSessionListDefault != tc.want {
				t.Errorf("got %q want %q", cfg.Server.WSSessionListDefault, tc.want)
			}
		})
	}
}

func TestHandleGetConfig_WSSessionListDefault(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Server.WSSessionListDefault = "delta"
	rr := httptest.NewRecorder()
	s.handleConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var v map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	srv, _ := v["server"].(map[string]any)
	if srv["ws_session_list_default"] != "delta" {
		t.Errorf("server.ws_session_list_default = %v", srv["ws_session_list_default"])
	}
}

func TestHandleInfo_AdvertisesWSHello(t *testing.T) {
	s := bl90Server(t)
	rr := httptest.NewRecorder()
	s.handleInfo(rr, httptest.NewRequest(http.MethodGet, "/api/info", nil))
	var v struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	have := strings.Join(v.Capabilities, " ")
	for _, c := range []string{"ws.hello", "ws.session_list_delta", "ws.session_view_summary"} {
		if !strings.Contains(" "+have+" ", " "+c+" ") {
			t.Errorf("capabilities %v missing %s", v.Capabilities, c)
		}
	}
}
