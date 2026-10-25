// BL427 (phase 5) — REST-level tests for POST /api/autonomous/events/forward
// (autonomous.ForwardedEventsPath). They drive the real autonomous.Manager
// receiver, the real subscription store and engine, and the real PeerForwarder,
// through the real handler and the real fedAuthMiddleware where the test needs
// the bearer token checked. The last test is the in-process two-daemon round
// trip over httptest.

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

const fwdPeerToken = "tok-forward-peer"

// fwdEvent is a PRD completion as the origin daemon emitted it.
func fwdEvent(id string) autonomous.LifecycleEvent {
	return autonomous.LifecycleEvent{
		ID:        id,
		At:        time.Now().UTC(),
		Kind:      "prd.completed",
		Level:     autonomous.LevelPRD,
		PRDID:     "prd-fwd-1",
		OldStatus: "running",
		NewStatus: "completed",
	}
}

// fwdChain is the chain of an event that left daemon-a for the first time.
func fwdChain() *autonomous.ForwardChain {
	return &autonomous.ForwardChain{OriginHost: "daemon-a", Hops: 1, Visited: []string{"daemon-a"}}
}

// fwdBody marshals a forwarded envelope the way the forwarder sends it.
func fwdBody(t *testing.T, ev autonomous.LifecycleEvent, chain *autonomous.ForwardChain, sender string) []byte {
	t.Helper()
	b, err := json.Marshal(autonomous.ForwardedEnvelope{
		Version:        autonomous.WebhookEnvelopeVersion,
		DeliveryID:     "delivery-fwd-1",
		SentAt:         time.Now().UTC(),
		Host:           sender,
		SubscriptionID: "sub-peer-a",
		Event:          ev,
		Forward:        chain,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return b
}

// fwdPeer is a federated peer entry that holds the shared token and the given
// capabilities, as the peer registry does.
func fwdPeer(name, token string, caps ...string) *multiserver.Entry {
	return &multiserver.Entry{Name: name, Token: token, Enabled: true, Federated: true, Capabilities: caps}
}

// fwdPost runs one request through the handler. peer, when set, is the
// authenticated caller (what fedAuthMiddleware puts on the context); sig is
// the signature header value.
func fwdPost(t *testing.T, s *Server, body []byte, peer *multiserver.Entry, sig string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, autonomous.ForwardedEventsPath, bytes.NewReader(body))
	if sig != "" {
		req.Header.Set(autonomous.WebhookSignatureHeader, sig)
	}
	if peer != nil {
		req = req.WithContext(context.WithValue(req.Context(), fedPeerKey, peer))
	}
	rr := httptest.NewRecorder()
	s.handleAutonomousForwardedEvents(rr, req)
	return rr
}

// fwdSigned posts body as peer, signed with the peer's token.
func fwdSigned(t *testing.T, s *Server, body []byte, peer *multiserver.Entry) *httptest.ResponseRecorder {
	t.Helper()
	return fwdPost(t, s, body, peer, autonomous.SignWebhookBody([]byte(peer.Token), time.Now(), body))
}

// fwdReceiver returns an enabled receiving manager for daemon local, and the
// server that exposes it, with the audit log in a fresh directory.
func fwdReceiver(t *testing.T, local string, enabled bool) (*Server, *autonomous.Manager, *audit.Log, string) {
	t.Helper()
	mgr := new(autonomous.Manager)
	mgr.SetForwardReceive(autonomous.ForwardReceiveConfig{
		Settings:  func() autonomous.ForwardSettings { return autonomous.ForwardSettings{Enabled: enabled} },
		LocalHost: func() string { return local },
	})
	dir := t.TempDir()
	log, err := audit.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	s := &Server{auditLog: log, token: "admin-" + local}
	s.SetAutonomousForwardedEvents(mgr)
	return s, mgr, log, dir
}

// fwdAuditRecords closes the log and returns the forward-receive records it
// wrote, in order.
func fwdAuditRecords(t *testing.T, log *audit.Log, dir string) []audit.Entry {
	t.Helper()
	_ = log.Close()
	raw, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	var out []audit.Entry
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var e audit.Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("audit line is not JSON: %v", err)
		}
		if e.Action == audit.ActionLifecycleForwardReceive {
			out = append(out, e)
		}
	}
	return out
}

// fwdWaitQueue polls a pending-events queue until it holds want events or the
// deadline passes, and returns what it holds.
func fwdWaitQueue(pq *autonomous.PendingEventQueue, queue string, want int) []autonomous.LifecycleEvent {
	deadline := time.Now().Add(3 * time.Second)
	for {
		evs, _, _ := pq.Pending(queue, 0, 50)
		if len(evs) >= want || time.Now().After(deadline) {
			return evs
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// fwdSubscriptionEngine builds the real subscription engine over m with one
// mcp_queue subscription named queue for prd.completed, and a topic sink on
// topic. The returned engine is closed at test end.
func fwdSubscriptionEngine(t *testing.T, m *autonomous.Manager, queue, topic string, pq *autonomous.PendingEventQueue, publish func([]string, string, string), fwd *autonomous.PeerForwarder) *autonomous.LifecycleSubscriptionEngine {
	t.Helper()
	store, err := autonomous.NewSubscriptionStore(filepath.Join(t.TempDir(), "lifecycle_subscriptions.json"), nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	subs := []autonomous.Subscription{
		{Filter: autonomous.LifecycleFilter{Kinds: []string{"prd.completed"}}, Sink: autonomous.SubscriptionSink{Type: autonomous.SinkMCPQueue, Target: queue}},
	}
	if topic != "" {
		subs = append(subs, autonomous.Subscription{Filter: autonomous.LifecycleFilter{Kinds: []string{"prd.completed"}}, Sink: autonomous.SubscriptionSink{Type: autonomous.SinkTopic, Target: topic}})
	}
	for _, sub := range subs {
		if _, err := store.Add(sub); err != nil {
			t.Fatalf("store.Add: %v", err)
		}
	}
	eng := autonomous.NewLifecycleSubscriptionEngine(m, store, autonomous.SessionPushConfig{
		PendingQueue:  pq,
		PublishTopics: publish,
		Forwarder:     fwd,
	})
	if err := eng.Start(); err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	t.Cleanup(eng.Close)
	return eng
}

// TestForwardedEvents_AcceptReachesMCPQueueAndTopicWithOrigin: an accepted
// event reaches a local mcp_queue subscription and a local topic sink, both
// naming the origin host, and the response is 202 with no token in it.
func TestForwardedEvents_AcceptReachesMCPQueueAndTopicWithOrigin(t *testing.T) {
	s, mgr, log, dir := fwdReceiver(t, "daemon-b", true)
	pq := autonomous.NewPendingEventQueue(64)
	var mu sync.Mutex
	var topicMessages []string
	fwdSubscriptionEngine(t, mgr, "fwd-queue", "fwd-topic", pq, func(topics []string, title, message string) {
		mu.Lock()
		defer mu.Unlock()
		topicMessages = append(topicMessages, strings.Join(topics, ",")+"|"+message)
	}, nil)

	rr := fwdPost(t, s, fwdBody(t, fwdEvent("ev-accept-1"), fwdChain(), "daemon-a"), nil, "")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s, want 202", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "tok") || strings.Contains(rr.Body.String(), "admin") {
		t.Fatalf("response echoes a credential: %s", rr.Body.String())
	}

	evs := fwdWaitQueue(pq, "fwd-queue", 1)
	if len(evs) != 1 {
		t.Fatalf("mcp_queue got %d events, want 1", len(evs))
	}
	if evs[0].Host != "daemon-a" || evs[0].Forward == nil || evs[0].Forward.OriginHost != "daemon-a" {
		t.Fatalf("queued event = host %q forward %+v, want origin daemon-a", evs[0].Host, evs[0].Forward)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(topicMessages)
		mu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(topicMessages) != 1 {
		t.Fatalf("topic sink got %d messages, want 1", len(topicMessages))
	}
	// The topic sink publishes to the alerts topic and the Automaton's own topic.
	if !strings.HasPrefix(topicMessages[0], "alerts,automaton-prd-fwd-1|") || !strings.Contains(topicMessages[0], "forwarded from daemon-a") {
		t.Fatalf("topic message = %q, want the alerts and Automaton topics and the origin host", topicMessages[0])
	}

	recs := fwdAuditRecords(t, log, dir)
	if len(recs) != 1 || recs[0].Details["outcome"] != "accepted" || recs[0].Details["origin_host"] != "daemon-a" {
		t.Fatalf("audit records = %+v, want one accepted record for daemon-a", recs)
	}
}

// TestForwardedEvents_CapabilityMissing403: a peer without autonomous:write is
// refused with 403 and the refusal is audited; the event is not delivered.
func TestForwardedEvents_CapabilityMissing403(t *testing.T) {
	s, mgr, log, dir := fwdReceiver(t, "daemon-b", true)
	pq := autonomous.NewPendingEventQueue(64)
	fwdSubscriptionEngine(t, mgr, "fwd-queue", "", pq, nil, nil)

	peer := fwdPeer("daemon-a", fwdPeerToken, "autonomous:read")
	body := fwdBody(t, fwdEvent("ev-cap-1"), fwdChain(), "daemon-a")
	rr := fwdSigned(t, s, body, peer)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s, want 403", rr.Code, rr.Body.String())
	}
	if evs := fwdWaitQueue(pq, "fwd-queue", 1); len(evs) != 0 {
		t.Fatalf("refused event was delivered: %+v", evs)
	}
	recs := fwdAuditRecords(t, log, dir)
	if len(recs) != 1 || recs[0].Details["outcome"] != "refused" || recs[0].Details["status_code"] != float64(http.StatusForbidden) {
		t.Fatalf("audit records = %+v, want one refused 403 record", recs)
	}
}

// TestForwardedEvents_NoTokenThroughAuthWrapper401: through the real auth
// middleware, a request without a token and one with a wrong token are both 401,
// and the handler never runs.
func TestForwardedEvents_NoTokenThroughAuthWrapper401(t *testing.T) {
	s, mgr, _, _ := fwdReceiver(t, "daemon-b", true)
	pq := autonomous.NewPendingEventQueue(64)
	fwdSubscriptionEngine(t, mgr, "fwd-queue", "", pq, nil, nil)
	wrapped := s.fedAuthMiddleware(http.HandlerFunc(s.handleAutonomousForwardedEvents))

	for _, auth := range []string{"", "Bearer not-a-real-token"} {
		req := httptest.NewRequest(http.MethodPost, autonomous.ForwardedEventsPath,
			bytes.NewReader(fwdBody(t, fwdEvent("ev-auth-1"), fwdChain(), "daemon-a")))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("auth %q: status = %d, want 401", auth, rr.Code)
		}
		if strings.Contains(rr.Body.String(), "not-a-real-token") {
			t.Fatalf("401 body echoes the presented token: %s", rr.Body.String())
		}
	}
	if evs := fwdWaitQueue(pq, "fwd-queue", 1); len(evs) != 0 {
		t.Fatalf("unauthenticated event was delivered: %+v", evs)
	}
}

// TestForwardedEvents_DisabledIs503: with forwarding disabled, or the receiver
// not wired, every request is answered 503 before the body is read.
func TestForwardedEvents_DisabledIs503(t *testing.T) {
	s, _, log, dir := fwdReceiver(t, "daemon-b", false)
	peer := fwdPeer("daemon-a", fwdPeerToken, "autonomous:write")
	// A body that would fail to parse: 503 proves it was never read.
	rr := fwdSigned(t, s, []byte("{not json"), peer)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled: status = %d body=%s, want 503", rr.Code, rr.Body.String())
	}
	recs := fwdAuditRecords(t, log, dir)
	if len(recs) != 1 || recs[0].Details["outcome"] != "refused" || recs[0].Details["status_code"] != float64(http.StatusServiceUnavailable) {
		t.Fatalf("disabled audit records = %+v", recs)
	}

	unwired := &Server{}
	rr = fwdPost(t, unwired, fwdBody(t, fwdEvent("ev-unwired"), fwdChain(), "daemon-a"), nil, "")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired: status = %d, want 503", rr.Code)
	}
}

// TestForwardedEvents_MalformedAndOversize400: malformed JSON, an unknown
// event kind and an oversized body are each 400. The envelope is signed, so the
// refusal is the envelope check and not the signature.
func TestForwardedEvents_MalformedAndOversize400(t *testing.T) {
	s, _, _, _ := fwdReceiver(t, "daemon-b", true)
	peer := fwdPeer("daemon-a", fwdPeerToken, "autonomous:write")

	unknown := fwdEvent("ev-unknown-1")
	unknown.Kind = "prd.bogus"
	bigPRD := fwdEvent("ev-big-1")
	bigPRD.PRDID = strings.Repeat("p", autonomous.WebhookMaxBodyBytes)

	cases := []struct {
		name string
		body []byte
	}{
		{"malformed json", []byte(`{"version":1,`)},
		{"unknown kind", fwdBody(t, unknown, fwdChain(), "daemon-a")},
		{"oversized body", fwdBody(t, bigPRD, fwdChain(), "daemon-a")},
		{"not a forwarded envelope", fwdBody(t, fwdEvent("ev-plain-1"), nil, "daemon-a")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := fwdSigned(t, s, c.body, peer)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", rr.Code, rr.Body.String())
			}
		})
	}
}

// TestForwardedEvents_MethodNotAllowed405: only POST is accepted.
func TestForwardedEvents_MethodNotAllowed405(t *testing.T) {
	s, _, _, _ := fwdReceiver(t, "daemon-b", true)
	req := httptest.NewRequest(http.MethodGet, autonomous.ForwardedEventsPath, nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousForwardedEvents(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: status = %d, want 405", rr.Code)
	}
}

// TestForwardedEvents_BadSignature401: a peer whose request carries no
// signature, or one made with another key, is refused with 401 before the body
// is parsed.
func TestForwardedEvents_BadSignature401(t *testing.T) {
	s, _, log, dir := fwdReceiver(t, "daemon-b", true)
	peer := fwdPeer("daemon-a", fwdPeerToken, "autonomous:write")
	body := fwdBody(t, fwdEvent("ev-sig-1"), fwdChain(), "daemon-a")

	if rr := fwdPost(t, s, body, peer, ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("no signature: status = %d, want 401", rr.Code)
	}
	wrongKey := autonomous.SignWebhookBody([]byte("some-other-token"), time.Now(), body)
	if rr := fwdPost(t, s, body, peer, wrongKey); rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key: status = %d, want 401", rr.Code)
	}
	stale := autonomous.SignWebhookBody([]byte(fwdPeerToken), time.Now().Add(-time.Hour), body)
	if rr := fwdPost(t, s, body, peer, stale); rr.Code != http.StatusUnauthorized {
		t.Fatalf("stale signature: status = %d, want 401", rr.Code)
	}
	recs := fwdAuditRecords(t, log, dir)
	if len(recs) != 3 {
		t.Fatalf("audit records = %d, want 3 refusals", len(recs))
	}
	for _, r := range recs {
		if r.Details["reason"] != "the request signature is missing or invalid" {
			t.Fatalf("refusal reason = %v", r.Details["reason"])
		}
	}
}

// TestForwardedEvents_LoopsAre409: a visited-host loop, an origin-host loop and
// a hop-cap loop are each refused with 409 and audited; none is delivered.
func TestForwardedEvents_LoopsAre409(t *testing.T) {
	s, mgr, log, dir := fwdReceiver(t, "daemon-b", true)
	pq := autonomous.NewPendingEventQueue(64)
	fwdSubscriptionEngine(t, mgr, "fwd-queue", "", pq, nil, nil)
	peer := fwdPeer("daemon-a", fwdPeerToken, "autonomous:write")

	cases := []struct {
		name   string
		chain  *autonomous.ForwardChain
		sender string
	}{
		{"visited list already holds this host", &autonomous.ForwardChain{OriginHost: "daemon-a", Hops: 2, Visited: []string{"daemon-a", "daemon-b"}}, "daemon-b"},
		{"origin is this host", &autonomous.ForwardChain{OriginHost: "daemon-b", Hops: 1, Visited: []string{"daemon-b"}}, "daemon-b"},
		{"hop cap reached", &autonomous.ForwardChain{OriginHost: "daemon-a", Hops: autonomous.DefaultForwardMaxHops, Visited: []string{"daemon-a", "daemon-c", "daemon-d"}}, "daemon-d"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := fwdSigned(t, s, fwdBody(t, fwdEvent("ev-loop-1"), c.chain, c.sender), peer)
			if rr.Code != http.StatusConflict {
				t.Fatalf("status = %d body=%s, want 409", rr.Code, rr.Body.String())
			}
		})
	}
	if evs := fwdWaitQueue(pq, "fwd-queue", 1); len(evs) != 0 {
		t.Fatalf("looped event was delivered: %+v", evs)
	}
	recs := fwdAuditRecords(t, log, dir)
	if len(recs) != len(cases) {
		t.Fatalf("audit records = %d, want %d", len(recs), len(cases))
	}
	for _, r := range recs {
		if r.Details["status_code"] != float64(http.StatusConflict) {
			t.Fatalf("loop audit record = %+v, want 409", r.Details)
		}
	}
}

// TestForwardedEvents_ForwardedEventNotForwardedAgain: a forwarded event that
// matches a peer-sink subscription is never sent on. The fake peer must receive
// zero requests, and the local mcp_queue subscription still sees the event.
// (The positive control, a local event reaching a peer sink, is in the
// autonomous package tests: the server package cannot emit a local event.)
func TestForwardedEvents_ForwardedEventNotForwardedAgain(t *testing.T) {
	var peerHits atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peerHits.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(fake.Close)

	fwd := autonomous.NewPeerForwarder(autonomous.PeerForwarderConfig{
		Settings:  func() autonomous.ForwardSettings { return autonomous.ForwardSettings{Enabled: true} },
		LocalHost: func() string { return "daemon-b" },
		LookupPeer: func(name string) (autonomous.PeerRef, bool) {
			return autonomous.PeerRef{Name: name, URL: fake.URL, Token: fwdPeerToken, Federated: true, HasAutonomousWrite: true}, true
		},
		NewClient: multiserver.HTTPClient,
	})
	s, mgr, _, _ := fwdReceiver(t, "daemon-b", true)
	pq := autonomous.NewPendingEventQueue(64)
	store, err := autonomous.NewSubscriptionStore(filepath.Join(t.TempDir(), "lifecycle_subscriptions.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []autonomous.Subscription{
		{Filter: autonomous.LifecycleFilter{Kinds: []string{"prd.completed"}}, Sink: autonomous.SubscriptionSink{Type: autonomous.SinkPeer, Target: "daemon-c"}},
		{Filter: autonomous.LifecycleFilter{Kinds: []string{"prd.completed"}}, Sink: autonomous.SubscriptionSink{Type: autonomous.SinkMCPQueue, Target: "fwd-queue"}},
	} {
		if _, err := store.Add(sub); err != nil {
			t.Fatalf("store.Add: %v", err)
		}
	}
	eng := autonomous.NewLifecycleSubscriptionEngine(mgr, store, autonomous.SessionPushConfig{PendingQueue: pq, Forwarder: fwd})
	if err := eng.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)

	rr := fwdPost(t, s, fwdBody(t, fwdEvent("ev-noloop-1"), fwdChain(), "daemon-a"), nil, "")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	if evs := fwdWaitQueue(pq, "fwd-queue", 1); len(evs) != 1 {
		t.Fatalf("local mcp_queue got %d events, want 1", len(evs))
	}
	time.Sleep(200 * time.Millisecond)
	if n := peerHits.Load(); n != 0 {
		t.Fatalf("fake peer received %d request(s); a forwarded event must never be forwarded on", n)
	}
}

// TestForwardedEvents_ExecutorStateUntouched: an accepted event writes nothing
// to the Automaton data directory, the executor's state. Every file there is
// hashed before and after.
func TestForwardedEvents_ExecutorStateUntouched(t *testing.T) {
	dataDir := t.TempDir()
	mgr, err := autonomous.NewManager(dataDir, autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.SetForwardReceive(autonomous.ForwardReceiveConfig{
		Settings:  func() autonomous.ForwardSettings { return autonomous.ForwardSettings{Enabled: true} },
		LocalHost: func() string { return "daemon-b" },
	})
	pq := autonomous.NewPendingEventQueue(64)
	fwdSubscriptionEngine(t, mgr, "fwd-queue", "", pq, nil, nil)
	s := &Server{}
	s.SetAutonomousForwardedEvents(mgr)

	before := fwdSnapshot(t, dataDir)
	rr := fwdPost(t, s, fwdBody(t, fwdEvent("ev-state-1"), fwdChain(), "daemon-a"), nil, "")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	if evs := fwdWaitQueue(pq, "fwd-queue", 1); len(evs) != 1 {
		t.Fatalf("event not delivered to the local queue: %d", len(evs))
	}
	after := fwdSnapshot(t, dataDir)
	if len(before) != len(after) {
		t.Fatalf("data dir file count changed: %d → %d", len(before), len(after))
	}
	for name, sum := range before {
		if after[name] != sum {
			t.Fatalf("executor state file %s changed by a forwarded event", name)
		}
	}
}

// fwdSnapshot maps every file under dir to a SHA-256 of its content.
func fwdSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(raw)
		rel, _ := filepath.Rel(dir, path)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	return out
}

// fwdDaemon is one side of the two-daemon round trip: a real receiving handler
// behind the real auth middleware, a local queue subscription, and a forwarder.
type fwdDaemon struct {
	name  string
	token string
	srv   *httptest.Server
	hits  atomic.Int32
	mgr   *autonomous.Manager
	queue *autonomous.PendingEventQueue
	fwd   *autonomous.PeerForwarder
}

// newFwdDaemon builds one daemon. peerURL is read at send time, so the two
// daemons can be wired to each other after both servers exist.
func newFwdDaemon(t *testing.T, name, peerName, token string, peerURL func() string) *fwdDaemon {
	t.Helper()
	s, mgr, _, _ := fwdReceiver(t, name, true)
	store, err := multiserver.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("multiserver.NewStore: %v", err)
	}
	if err := store.Add(&multiserver.Entry{Name: peerName, URL: "https://" + peerName + ".invalid", Token: token, Enabled: true, Federated: true, Capabilities: []string{"autonomous:write"}}); err != nil {
		t.Fatalf("store.Add peer: %v", err)
	}
	s.serverStore = store
	s.token = "admin-" + name

	d := &fwdDaemon{name: name, token: token, mgr: mgr, queue: autonomous.NewPendingEventQueue(64)}
	mux := http.NewServeMux()
	mux.Handle(autonomous.ForwardedEventsPath, s.fedAuthMiddleware(http.HandlerFunc(s.handleAutonomousForwardedEvents)))
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.hits.Add(1)
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(d.srv.Close)

	d.fwd = autonomous.NewPeerForwarder(autonomous.PeerForwarderConfig{
		Settings:  func() autonomous.ForwardSettings { return autonomous.ForwardSettings{Enabled: true} },
		LocalHost: func() string { return name },
		LookupPeer: func(n string) (autonomous.PeerRef, bool) {
			if n != peerName {
				return autonomous.PeerRef{}, false
			}
			return autonomous.PeerRef{Name: peerName, URL: peerURL(), Token: token, Federated: true, HasAutonomousWrite: true}, true
		},
		NewClient: multiserver.HTTPClient,
	})
	fwdSubscriptionEngine(t, mgr, "events-"+name, "", d.queue, nil, nil)
	return d
}

// TestForwardedEvents_TwoDaemonRoundTrip: daemon A forwards an event to daemon
// B over httptest, using B's real handler and auth. B's local subscription gets
// it exactly once with origin daemon-a. Then B is wired back to A: B's forwarder
// refuses to send A's event back, and A's handler refuses a looped envelope with
// 409, so the loop terminates and nothing is delivered twice.
func TestForwardedEvents_TwoDaemonRoundTrip(t *testing.T) {
	const tok = "tok-shared-a-b"
	var a, b *fwdDaemon
	a = newFwdDaemon(t, "daemon-a", "daemon-b", tok, func() string { return b.srv.URL })
	b = newFwdDaemon(t, "daemon-b", "daemon-a", tok, func() string { return a.srv.URL })
	ctx := context.Background()

	// A → B: A's forwarder sends its own event to B's real handler.
	local := fwdEvent("ev-round-1")
	if err := a.fwd.Forward(ctx, autonomous.Subscription{ID: "sub-a-to-b", Owner: "ops", Sink: autonomous.SubscriptionSink{Type: autonomous.SinkPeer, Target: "daemon-b"}}, local); err != nil {
		t.Fatalf("A forward to B: %v", err)
	}
	evs := fwdWaitQueue(b.queue, "events-daemon-b", 1)
	if len(evs) != 1 {
		t.Fatalf("B local subscription got %d events, want 1", len(evs))
	}
	if evs[0].ID != "ev-round-1" || evs[0].Host != "daemon-a" || evs[0].Forward == nil || evs[0].Forward.OriginHost != "daemon-a" {
		t.Fatalf("B received %+v, want ev-round-1 with origin daemon-a", evs[0])
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(fwdQueueAll(b.queue, "events-daemon-b")); n != 1 {
		t.Fatalf("B local subscription got %d events after settling, want exactly 1", n)
	}
	hitsToA := a.hits.Load()

	// Wiring B back to A: B holds the event it received, so its forwarder refuses
	// to send it on. A receives nothing.
	err := b.fwd.Forward(ctx, autonomous.Subscription{ID: "sub-b-to-a", Owner: "ops", Sink: autonomous.SubscriptionSink{Type: autonomous.SinkPeer, Target: "daemon-a"}}, evs[0])
	if !errors.Is(err, autonomous.ErrForwardRefused) {
		t.Fatalf("B re-forward of A's event: err = %v, want ErrForwardRefused", err)
	}
	if n := a.hits.Load(); n != hitsToA {
		t.Fatalf("A received %d request(s) from B's re-forward; want none", n-hitsToA)
	}

	// A crafted loop straight at A: an envelope that already visited A is 409.
	loop := fwdBody(t, fwdEvent("ev-round-loop"), &autonomous.ForwardChain{OriginHost: "daemon-a", Hops: 2, Visited: []string{"daemon-a", "daemon-b"}}, "daemon-b")
	resp := fwdHTTP(t, a, loop)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("looped envelope at A: status = %d, want 409", resp.StatusCode)
	}
	if n := len(fwdQueueAll(a.queue, "events-daemon-a")); n != 0 {
		t.Fatalf("A delivered a looped event (%d events)", n)
	}

	// B → A with a new B-origin event: accepted once at A, and A never sends it back.
	bEvent := fwdEvent("ev-round-b")
	if err := b.fwd.Forward(ctx, autonomous.Subscription{ID: "sub-b-origin", Owner: "ops", Sink: autonomous.SubscriptionSink{Type: autonomous.SinkPeer, Target: "daemon-a"}}, bEvent); err != nil {
		t.Fatalf("B forward of its own event to A: %v", err)
	}
	aEvs := fwdWaitQueue(a.queue, "events-daemon-a", 1)
	if len(aEvs) != 1 || aEvs[0].ID != "ev-round-b" || aEvs[0].Host != "daemon-b" {
		t.Fatalf("A received %+v, want ev-round-b with origin daemon-b", aEvs)
	}
	if err := a.fwd.Forward(ctx, autonomous.Subscription{ID: "sub-a-back", Owner: "ops", Sink: autonomous.SubscriptionSink{Type: autonomous.SinkPeer, Target: "daemon-b"}}, aEvs[0]); !errors.Is(err, autonomous.ErrForwardRefused) {
		t.Fatalf("A re-forward of B's event: err = %v, want ErrForwardRefused", err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(fwdQueueAll(b.queue, "events-daemon-b")); n != 1 {
		t.Fatalf("B got %d events after the loop attempts, want still 1", n)
	}
	if n := len(fwdQueueAll(a.queue, "events-daemon-a")); n != 1 {
		t.Fatalf("A got %d events after the loop attempts, want still 1", n)
	}
}

// fwdQueueAll returns every event the queue holds, from the start.
func fwdQueueAll(pq *autonomous.PendingEventQueue, queue string) []autonomous.LifecycleEvent {
	evs, _, _ := pq.Pending(queue, 0, 200)
	return evs
}

// fwdHTTP posts a signed envelope to daemon d over its real server, as a peer
// presenting the shared token, and returns the response.
func fwdHTTP(t *testing.T, d *fwdDaemon, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, d.srv.URL+autonomous.ForwardedEventsPath, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(autonomous.WebhookSignatureHeader, autonomous.SignWebhookBody([]byte(d.token), time.Now(), body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	})
	return resp
}
