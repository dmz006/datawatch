// Tests for WebSocket output scoping and unsubscribe (ws_output_scope.go).
// BL432 / GH#242.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/gorilla/websocket"
)

// newOutputScopeServer starts a hub + /ws endpoint with the given
// server.ws_output_scope_default.
var hubLogMu sync.Mutex
var hubLog []*Hub

func newOutputScopeServer(t *testing.T, scopeDefault string) (*Server, *httptest.Server) {
	t.Helper()
	s, srv := newSessionStreamServer(t, "")
	s.cfg.Server.WSOutputScopeDefault = scopeDefault
	hubLogMu.Lock()
	hubLog = append(hubLog, s.hub)
	hubLogMu.Unlock()
	t.Logf("new server: %p hub=%p url=%s (total hubs=%d)", s, s.hub, srv.URL, len(hubLog))
	return s, srv
}

func wsSend(t *testing.T, conn *websocket.Conn, typ MessageType, data string) {
	t.Helper()
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"`+string(typ)+`","data":`+data+`}`)); err != nil {
		t.Fatalf("write %s: %v", typ, err)
	}
}

// wsSubscribe subscribes and waits for the subscribe's initial
// pane_capture, by which point the subscription is recorded.
func wsSubscribe(t *testing.T, conn *websocket.Conn, id string) {
	t.Helper()
	wsSend(t, conn, MsgSubscribe, `{"session_id":"`+id+`"}`)
	for {
		m := readWSFrame(t, conn)
		if m.Type != "pane_capture" {
			continue
		}
		var d struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(m.Data, &d) != nil {
			continue
		}
		// Exact session-ID match (short/full forms equivalent) instead of a
		// substring scan of the whole frame payload, which unrelated
		// sessions' frames can satisfy in their pane text.
		if sessionIDsMatch(d.SessionID, id) {
			return
		}
	}
}

// wsSync sends ping and waits for pong: the read pump handles messages in
// order, so everything sent before has been applied.
func wsSync(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	wsSend(t, conn, MsgPing, `{}`)
	for {
		if m := readWSFrame(t, conn); m.Type == "pong" {
			return
		}
	}
}

// skipConnectFrames reads the connect-time sessions frame.
func skipConnectFrames(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if m := readWSFrame(t, conn); m.Type != MsgSessions {
		t.Fatalf("first frame = %s, want sessions", m.Type)
	}
}

// broadcastSessionTraffic emits one of every per-session frame type for
// id, plus control frames that are never scoped.
func broadcastSessionTraffic(h *Hub, id string) {
	h.BroadcastOutput(id, []string{"out " + id})
	h.BroadcastRawOutput(id, []string{"raw " + id})
	h.BroadcastPaneCapture(id, []string{"pane " + id})
	h.BroadcastChatMessage(id, "assistant", "chat "+id, true)
	h.BroadcastResponse(id, "resp "+id)
	h.BroadcastNeedsInput(id, "prompt "+id)
	h.BroadcastSessionState(testSession(strings.TrimPrefix(id, "testhost-"), "n", session.StateRunning))
	h.BroadcastAlert(&alerts.Alert{ID: "al-" + id, SessionID: id, Title: "t"})
}

// frameSessions maps frame type → session IDs it carried.
func frameSessions(t *testing.T, frames []WSMessage) map[MessageType][]string {
	t.Helper()
	out := map[MessageType][]string{}
	for _, f := range frames {
		var d struct {
			SessionID string `json:"session_id"`
			Session   struct {
				FullID string `json:"full_id"`
			} `json:"session"`
		}
		_ = json.Unmarshal(f.Data, &d)
		id := d.SessionID
		if id == "" {
			id = d.Session.FullID
		}
		out[f.Type] = append(out[f.Type], id)
	}
	return out
}

var scopedTypes = []MessageType{MsgOutput, "raw_output", "pane_capture", MsgChatMessage}
var unscopedTypes = []MessageType{MsgResponse, MsgNeedsInput, MsgSessionState, MsgAlert}

func TestWSOutputScope_AllModeGetsEverySession(t *testing.T) {
	s, srv := newOutputScopeServer(t, "")
	conn := dialSessionStream(t, srv)
	skipConnectFrames(t, conn)
	if ack := sayHello(t, conn, `{"output":"all"}`); ack.Output != "all" {
		t.Fatalf("ack = %+v", ack)
	}
	broadcastSessionTraffic(s.hub, "testhost-a")
	broadcastSessionTraffic(s.hub, "testhost-b")
	got := frameSessions(t, drainUntilSentinel(t, s.hub, conn)[0])
	for _, typ := range append(scopedTypes, unscopedTypes...) {
		if strings.Join(got[typ], ",") != "testhost-a,testhost-b" {
			t.Errorf("%s sessions = %v, want both", typ, got[typ])
		}
	}
}

func TestWSOutputScope_SubscribedModeFiltersTerminalFrames(t *testing.T) {
	s, srv := newOutputScopeServer(t, "")
	conn := dialSessionStream(t, srv)
	skipConnectFrames(t, conn)
	if ack := sayHello(t, conn, `{"output":"subscribed"}`); ack.Output != "subscribed" {
		t.Fatalf("ack = %+v", ack)
	}
	wsSubscribe(t, conn, "testhost-a")
	broadcastSessionTraffic(s.hub, "testhost-a")
	broadcastSessionTraffic(s.hub, "testhost-b")
	got := frameSessions(t, drainUntilSentinel(t, s.hub, conn)[0])
	for _, typ := range scopedTypes {
		if strings.Join(got[typ], ",") != "testhost-a" {
			t.Errorf("%s sessions = %v, want only the subscribed one", typ, got[typ])
		}
	}
	for _, typ := range unscopedTypes {
		if strings.Join(got[typ], ",") != "testhost-a,testhost-b" {
			t.Errorf("control frame %s sessions = %v, want both", typ, got[typ])
		}
	}
}

// One subscribed-mode client must not affect what an all-mode client on
// the same hub receives.
func TestWSOutputScope_MixedClients(t *testing.T) {
	s, srv := newOutputScopeServer(t, "")
	scoped := dialSessionStream(t, srv)
	legacy := dialSessionStream(t, srv)
	skipConnectFrames(t, scoped)
	skipConnectFrames(t, legacy)
	sayHello(t, scoped, `{"output":"subscribed"}`)
	s.hub.BroadcastOutput("testhost-b", []string{"x"})
	frames := drainUntilSentinel(t, s.hub, scoped, legacy)
	if n := len(frameSessions(t, frames[0])[MsgOutput]); n != 0 {
		t.Errorf("scoped client got %d output frames, want 0", n)
	}
	if got := frameSessions(t, frames[1])[MsgOutput]; len(got) != 1 {
		t.Errorf("legacy client output = %v, want one frame", got)
	}
}

func TestWSOutputScope_ShortAndFullIDsMatch(t *testing.T) {
	cases := []struct{ name, subscribeAs, broadcastAs string }{
		{"short subscribe, full output", "a1b2", "testhost-a1b2"},
		{"full subscribe, short output", "testhost-a1b2", "a1b2"},
		{"case-insensitive", "A1B2", "testhost-a1b2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, srv := newOutputScopeServer(t, "")
			conn := dialSessionStream(t, srv)
			skipConnectFrames(t, conn)
			sayHello(t, conn, `{"output":"subscribed"}`)
			wsSubscribe(t, conn, tc.subscribeAs)
			s.hub.BroadcastOutput(tc.broadcastAs, []string{"x"})
			s.hub.BroadcastOutput("testhost-zzzz", []string{"y"})
			got := frameSessions(t, drainUntilSentinel(t, s.hub, conn)[0])[MsgOutput]
			if len(got) != 1 || got[0] != tc.broadcastAs {
				t.Errorf("output sessions = %v, want [%s]", got, tc.broadcastAs)
			}
		})
	}
}

func TestSessionIDsMatch(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"host-abcd", "host-abcd", true},
		{"host-abcd", "abcd", true},
		{"abcd", "my-host-abcd", true},
		{"HOST-ABCD", "abcd", true},
		{"host-abcd", "host-abce", false},
		{"host-abcd", "bcd", false}, // suffix must start at a '-'
		{"abcd", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		if got := sessionIDsMatch(tc.a, tc.b); got != tc.want {
			t.Errorf("sessionIDsMatch(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// findHubClient returns the hub's only registered client, polling until
// the registration lands or the frame deadline expires. The test only
// knows the connection is set up from frames it has already received, but
// the hub applies s.hub.register in its own Run goroutine (unbuffered
// handoff, then map write), so under heavy scheduling pressure the map
// write can lag behind the frame deliveries the test already observed;
// an immediate snapshot assert flakes there. Bound the wait by deadline
// instead (BL550). All original assertions are preserved at the deadline.
func findHubClient(t *testing.T, h *Hub, conn *websocket.Conn) *client {
	t.Helper()
	deadline := time.Now().Add(wsTestFrameTimeout)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		h.mu.RLock()
		clients := make([]*client, 0, len(h.clients))
		for c := range h.clients {
			clients = append(clients, c)
		}
		nSend := make([]int, 0, len(clients))
		for c := range h.clients {
			nSend = append(nSend, len(c.send))
		}
		h.mu.RUnlock()
		if len(clients) == 1 {
			return clients[0]
		}
		if time.Now().After(deadline) {
			t.Logf("diag: hub=%p clients=%d sendLens=%v pendingRegister=%d pendingUnregister=%d pendingBroadcast=%d",
				h, len(clients), nSend, len(h.register), len(h.unregister), len(h.broadcast))
			hubLogMu.Lock()
			for i, hh := range hubLog {
				hh.mu.RLock()
				t.Logf("hublog[%d]: hub=%p clients=%d regPending=%d unregPending=%d bcastPending=%d",
					i, hh, len(hh.clients), len(hh.register), len(hh.unregister), len(hh.broadcast))
				hh.mu.RUnlock()
			}
			hubLogMu.Unlock()
			probeConn(t, conn)
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			t.Fatalf("hub has %d clients, want 1\n%s", len(clients), buf[:n])
		}
		select {
		case <-ticker.C:
		case <-time.After(time.Until(deadline)):
		}
	}
}

// probeConn inspects the test-side socket state right before failing:
// writes a PING (300 ms write deadline) and does one read (300 ms
// deadline). If the server already closed/reset the socket, the write
// fails (EPIPE/RST) or the read returns EOF/close-error; if the socket
// is still open, the read returns a PONG or times out with no data.
func probeConn(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	wd := time.Now().Add(300 * time.Millisecond)
	werr := conn.WriteControl(websocket.PingMessage, nil, wd)
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	mt, b, rerr := conn.ReadMessage()
	t.Logf("probe: writePing err=%v; read type=%d n=%d err=%v data=%.200q",
		werr, mt, len(b), rerr, string(b))
}

func TestWSOutputScope_UnsubscribeStopsDeliveryAndCancelsCapture(t *testing.T) {
	cases := []struct{ name, subscribeAs, unsubscribeAs string }{
		{"same id", "testhost-a1b2", "testhost-a1b2"},
		{"short unsubscribes full", "testhost-a1b2", "a1b2"},
		{"full unsubscribes short", "a1b2", "testhost-a1b2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, srv := newOutputScopeServer(t, "")
			conn := dialSessionStream(t, srv)
			skipConnectFrames(t, conn)
			sayHello(t, conn, `{"output":"subscribed"}`)
			wsSubscribe(t, conn, tc.subscribeAs)
			wsSubscribe(t, conn, "testhost-other")

			// Swap the real capture cancel for one the test can observe.
			c := findHubClient(t, s.hub, conn)
			cancelled := make(chan struct{})
			c.mu.Lock()
			if _, ok := c.captureCancels[tc.subscribeAs]; !ok {
				c.mu.Unlock()
				t.Fatalf("subscribe recorded no capture for %s", tc.subscribeAs)
			}
			c.captureCancels[tc.subscribeAs] = func() { close(cancelled) }
			c.mu.Unlock()

			// BL550 — explicit teardown: stop any capture that is still
			// running for this client (notably the other session's real
			// StartScreenCapture goroutine) and wait for the hub to drop
			// the client before the subtest ends, so neither leaks into a
			// later test. Run first (LIFO), before dialSessionStream's
			// conn.Close cleanup. Re-read the cancel map here (not at
			// registration time): on the success path the production
			// unsubscribe already fired and deleted the stub's entry, so
			// only still-live captures are closed exactly once.
			clientToClean := c
			t.Cleanup(func() {
				clientToClean.mu.Lock()
				remaining := make(map[string]context.CancelFunc, len(clientToClean.captureCancels))
				for id, fn := range clientToClean.captureCancels {
					if fn != nil {
						remaining[id] = fn
					}
				}
				clientToClean.mu.Unlock()
				for id, fn := range remaining {
					t.Logf("cleanup: cancelling leftover capture %q", id)
					fn()
				}
				_ = conn.Close()
				// Poll on the real condition: hub registry empty again.
				deadline := time.Now().Add(wsTestFrameTimeout)
				for s.hub.ClientCount() != 0 && time.Now().Before(deadline) {
					time.Sleep(5 * time.Millisecond)
				}
				if got := s.hub.ClientCount(); got != 0 {
					t.Errorf("cleanup: hub still has %d clients, want 0", got)
				}
			})

			s.hub.BroadcastOutput("testhost-a1b2", []string{"before"})
			if got := frameSessions(t, drainUntilSentinel(t, s.hub, conn)[0])[MsgOutput]; len(got) != 1 {
				t.Fatalf("before unsubscribe: output = %v", got)
			}

			wsSend(t, conn, MsgUnsubscribe, `{"session_id":"`+tc.unsubscribeAs+`"}`)
			wsSync(t, conn)
			select {
			case <-cancelled:
			case <-time.After(wsTestFrameTimeout):
				t.Fatal("unsubscribe did not cancel the screen capture")
			}
			c.mu.Lock()
			_, subLeft := c.subscribed[tc.subscribeAs]
			_, capLeft := c.captureCancels[tc.subscribeAs]
			otherCap := c.captureCancels["testhost-other"] != nil
			c.mu.Unlock()
			if subLeft || capLeft {
				t.Errorf("after unsubscribe: subscribed=%v capture=%v, want both gone", subLeft, capLeft)
			}
			if !otherCap {
				t.Error("unsubscribe cancelled another session's capture")
			}

			s.hub.BroadcastOutput("testhost-a1b2", []string{"after"})
			s.hub.BroadcastOutput("testhost-other", []string{"still"})
			got := frameSessions(t, drainUntilSentinel(t, s.hub, conn)[0])[MsgOutput]
			if strings.Join(got, ",") != "testhost-other" {
				t.Errorf("after unsubscribe: output = %v, want only testhost-other", got)
			}
		})
	}
}

// unsubscribe also works for all-mode clients (stops the capture) and is a
// no-op for a session never subscribed.
func TestClientUnsubscribe_Direct(t *testing.T) {
	n := 0
	c := &client{
		subscribed:     map[string]bool{"h-a": true, "h-b": true},
		captureCancels: map[string]context.CancelFunc{"h-a": func() { n++ }},
	}
	if got := c.unsubscribe("zzz"); got != 0 || n != 0 || len(c.subscribed) != 2 {
		t.Fatalf("unknown id: cancelled=%d calls=%d subs=%v", got, n, c.subscribed)
	}
	if got := c.unsubscribe("a"); got != 1 || n != 1 {
		t.Fatalf("cancelled=%d calls=%d, want 1/1", got, n)
	}
	if c.subscribed["h-a"] || !c.subscribed["h-b"] || len(c.captureCancels) != 0 {
		t.Errorf("after unsubscribe: subs=%v caps=%d", c.subscribed, len(c.captureCancels))
	}
}

func TestWSOutputScope_ConfigDefaultSubscribedForNoHelloClient(t *testing.T) {
	s, srv := newOutputScopeServer(t, "subscribed")
	conn := dialSessionStream(t, srv)
	skipConnectFrames(t, conn)
	wsSubscribe(t, conn, "testhost-a")
	broadcastSessionTraffic(s.hub, "testhost-a")
	broadcastSessionTraffic(s.hub, "testhost-b")
	got := frameSessions(t, drainUntilSentinel(t, s.hub, conn)[0])
	if strings.Join(got[MsgOutput], ",") != "testhost-a" {
		t.Errorf("no-hello client under default=subscribed: output = %v", got[MsgOutput])
	}
	if len(got[MsgAlert]) != 2 {
		t.Errorf("alerts = %v, want both", got[MsgAlert])
	}
	// hello without output reports the configured default; an explicit
	// output=all overrides it.
	if ack := sayHello(t, conn, `{"session_view":"full"}`); ack.Output != "subscribed" {
		t.Errorf("ack = %+v, want output=subscribed from config", ack)
	}
	if ack := sayHello(t, conn, `{"output":"all"}`); ack.Output != "all" {
		t.Fatalf("ack = %+v", ack)
	}
	s.hub.BroadcastOutput("testhost-b", []string{"x"})
	if got := frameSessions(t, drainUntilSentinel(t, s.hub, conn)[0])[MsgOutput]; len(got) != 1 {
		t.Errorf("after hello output=all: output = %v", got)
	}
}

func TestWSOutputScope_HelloBadValueUnchanged(t *testing.T) {
	_, srv := newOutputScopeServer(t, "")
	conn := dialSessionStream(t, srv)
	sayHello(t, conn, `{"output":"subscribed"}`)
	if ack := sayHello(t, conn, `{"output":"some"}`); ack.Output != "subscribed" {
		t.Errorf("ack = %+v, want output unchanged by an unknown value", ack)
	}
}

// The default is read live per frame. In-process client so every config
// write is ordered against the hub goroutine by a channel.
func TestWSOutputScope_ConfigDefaultReadLive(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.cfg = config.DefaultConfig()
	s.hub = NewHub()
	s.wireHubSessionStream()
	go s.hub.Run()
	ch := make(chan []byte, 16)
	s.hub.register <- &client{hub: s.hub, send: ch, subscribed: map[string]bool{}}

	next := func() WSMessage {
		select {
		case b := <-ch:
			var m WSMessage
			_ = json.Unmarshal(b, &m)
			return m
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
		return WSMessage{}
	}
	s.hub.BroadcastOutput("testhost-a", []string{"x"})
	if m := next(); m.Type != MsgOutput {
		t.Fatalf("default all: got %s", m.Type)
	}
	applyConfigPatch(s.cfg, map[string]interface{}{"server.ws_output_scope_default": "subscribed"})
	s.hub.BroadcastOutput("testhost-a", []string{"x"})
	s.hub.BroadcastNotification("sentinel")
	if m := next(); m.Type != MsgNotification {
		t.Fatalf("after switching to subscribed: got %s, want output suppressed", m.Type)
	}
}

func TestApplyConfigPatch_WSOutputScopeDefault(t *testing.T) {
	cases := []struct {
		name string
		val  interface{}
		want string
	}{
		{"subscribed", "subscribed", "subscribed"},
		{"case and space normalized", " ALL ", "all"},
		{"invalid ignored", "some", "all"},
		{"non-string ignored", true, "all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			applyConfigPatch(cfg, map[string]interface{}{"server.ws_output_scope_default": tc.val})
			if cfg.Server.WSOutputScopeDefault != tc.want {
				t.Errorf("got %q want %q", cfg.Server.WSOutputScopeDefault, tc.want)
			}
		})
	}
}

func TestHandleGetConfig_WSOutputScopeDefault(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Server.WSOutputScopeDefault = "subscribed"
	rr := httptest.NewRecorder()
	s.handleConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var v map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	srv, _ := v["server"].(map[string]any)
	if srv["ws_output_scope_default"] != "subscribed" {
		t.Errorf("server.ws_output_scope_default = %v", srv["ws_output_scope_default"])
	}
}

func TestHandleInfo_AdvertisesWSOutputScope(t *testing.T) {
	s := bl90Server(t)
	rr := httptest.NewRecorder()
	s.handleInfo(rr, httptest.NewRequest(http.MethodGet, "/api/info", nil))
	var v struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	have := " " + strings.Join(v.Capabilities, " ") + " "
	for _, c := range []string{"ws.output_scoped", "ws.unsubscribe"} {
		if !strings.Contains(have, " "+c+" ") {
			t.Errorf("capabilities %v missing %s", v.Capabilities, c)
		}
	}
}
