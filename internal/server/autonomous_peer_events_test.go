package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// peerEventsHarness is a daemon-side control over a real PeerEventSubscriber
// that reads a fake peer's events endpoint.
type peerEventsHarness struct {
	s       *Server
	enabled bool
	mu      sync.Mutex
	seen    []autonomous.LifecycleEvent
	reads   int
	cancel  context.CancelFunc
	sub     *autonomous.PeerEventSubscriber
}

func newPeerEventsHarness(t *testing.T, enabled bool) *peerEventsHarness {
	t.Helper()
	h := &peerEventsHarness{enabled: enabled}
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.reads++
		first := h.reads == 1
		h.mu.Unlock()
		if r.URL.Path != autonomous.PeerEventsPath || r.URL.Query().Get("queue") != "ops" ||
			r.Header.Get("Authorization") != "Bearer peer-token" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		evs := []autonomous.LifecycleEvent{}
		if first {
			evs = append(evs, autonomous.LifecycleEvent{
				ID: "ev1", At: time.Now().UTC(), Kind: "automation.active", Level: autonomous.LevelAutomation,
				AutomationID: "auto-1", OldStatus: "draft", NewStatus: "active", Host: "far-host",
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"events": evs, "next_cursor": 1, "dropped": false})
	}))
	t.Cleanup(peer.Close)

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.sub = autonomous.NewPeerEventSubscriber(autonomous.PeerEventSubscriberConfig{
		LookupPeer: func(name string) (autonomous.PeerRef, bool) {
			if name != "far" {
				return autonomous.PeerRef{}, false
			}
			return autonomous.PeerRef{Name: "far", URL: peer.URL, Token: "peer-token", Federated: true, HasAutonomousRead: true}, true
		},
		LocalHost: func() string { return "local-host" },
		Settings: func() autonomous.ForwardSettings {
			return autonomous.ForwardSettings{Enabled: true, MaxAttempts: 3, Timeout: 2 * time.Second}
		},
		NewClient:    func(_ bool, timeout time.Duration) *http.Client { return &http.Client{Timeout: timeout} },
		Publish:      func(ev autonomous.LifecycleEvent) { h.mu.Lock(); h.seen = append(h.seen, ev); h.mu.Unlock() },
		PollInterval: time.Millisecond,
	})
	t.Cleanup(func() { cancel(); h.sub.StopAll() })
	h.s = &Server{cfg: config.DefaultConfig()}
	h.s.SetPeerEvents(NewPeerEventControl(ctx, h.sub, func() bool { return h.enabled }))
	return h
}

func (h *peerEventsHarness) call(t *testing.T, method, path, body string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	h.s.handleAutonomousPeerEvents(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rr.Code, rr.Body.String()
}

func (h *peerEventsHarness) waitSeen(t *testing.T, n int) []autonomous.LifecycleEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		got := append([]autonomous.LifecycleEvent(nil), h.seen...)
		h.mu.Unlock()
		if len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no event crossed from the fake peer within 5s")
	return nil
}

// Start reads a peer, one event crosses into local dispatch stamped with the
// peer name, the read is listed, and stop ends it.
func TestPeerEventsStartReadStopAndEventCrosses(t *testing.T) {
	h := newPeerEventsHarness(t, true)

	if code, body := h.call(t, http.MethodGet, "/api/autonomous/peer-events", ""); code != 200 ||
		!strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"running":[]`) {
		t.Fatalf("status before start: %d %s", code, body)
	}
	if code, body := h.call(t, http.MethodPost, "/api/autonomous/peer-events/start", `{"peer":"far","queue":"ops"}`); code != 200 || !strings.Contains(body, `"started":true`) {
		t.Fatalf("start: %d %s", code, body)
	}
	evs := h.waitSeen(t, 1)
	if evs[0].ID != "ev1" || evs[0].Peer != "far" || evs[0].Host != "far-host" || evs[0].Forward == nil {
		t.Fatalf("event that crossed is not stamped as a forwarded event from peer far: %+v", evs[0])
	}
	if _, body := h.call(t, http.MethodGet, "/api/autonomous/peer-events", ""); !strings.Contains(body, `"running":["far"]`) {
		t.Fatalf("status while running: %s", body)
	}
	if code, _ := h.call(t, http.MethodPost, "/api/autonomous/peer-events/start", `{"peer":"far","queue":"ops"}`); code != http.StatusConflict {
		t.Errorf("second start for the same peer: %d, want 409", code)
	}
	if code, body := h.call(t, http.MethodPost, "/api/autonomous/peer-events/stop", `{"peer":"far"}`); code != 200 || !strings.Contains(body, `"stopped":true`) {
		t.Fatalf("stop: %d %s", code, body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(h.sub.Running()) > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if r := h.sub.Running(); len(r) != 0 {
		t.Fatalf("still reading after stop: %v", r)
	}
	if _, body := h.call(t, http.MethodPost, "/api/autonomous/peer-events/stop", `{"peer":"far"}`); !strings.Contains(body, `"stopped":false`) {
		t.Errorf("stop of a peer that is not read: %s", body)
	}
}

func TestPeerEventsRefusals(t *testing.T) {
	off := newPeerEventsHarness(t, false)
	if code, _ := off.call(t, http.MethodPost, "/api/autonomous/peer-events/start", `{"peer":"far","queue":"ops"}`); code != http.StatusConflict {
		t.Errorf("start while peer_events is off: %d, want 409", code)
	}
	if _, body := off.call(t, http.MethodGet, "/api/autonomous/peer-events", ""); !strings.Contains(body, `"enabled":false`) {
		t.Errorf("status while off: %s", body)
	}

	on := newPeerEventsHarness(t, true)
	cases := []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPost, "/api/autonomous/peer-events/start", `{"queue":"ops"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/autonomous/peer-events/start", `not json`, http.StatusBadRequest},
		{http.MethodPost, "/api/autonomous/peer-events/start", `{"peer":"ghost","queue":"ops"}`, http.StatusUnprocessableEntity},
		{http.MethodPost, "/api/autonomous/peer-events/start", `{"peer":"far"}`, http.StatusUnprocessableEntity},
		{http.MethodGet, "/api/autonomous/peer-events/start", ``, http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/autonomous/peer-events", `{}`, http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/autonomous/peer-events/nope", `{}`, http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		if code, body := on.call(t, c.method, c.path, c.body); code != c.want {
			t.Errorf("%s %s %q: %d (%s), want %d", c.method, c.path, c.body, code, strings.TrimSpace(body), c.want)
		}
	}
	unwired := &Server{cfg: config.DefaultConfig()}
	rr := httptest.NewRecorder()
	unwired.handleAutonomousPeerEvents(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/peer-events", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("unwired: %d, want 503", rr.Code)
	}
}
