package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// subTestSession is a minimal MCP client session that collects notifications.
type subTestSession struct {
	id string
	ch chan mcpsdk.JSONRPCNotification
}

func newSubTestSession(id string) *subTestSession {
	return &subTestSession{id: id, ch: make(chan mcpsdk.JSONRPCNotification, 16)}
}
func (s *subTestSession) Initialize()                                            {}
func (s *subTestSession) Initialized() bool                                      { return true }
func (s *subTestSession) NotificationChannel() chan<- mcpsdk.JSONRPCNotification { return s.ch }
func (s *subTestSession) SessionID() string                                      { return s.id }

// subTestEnv is a real MCP server over a real Automata manager.
type subTestEnv struct {
	srv  *Server
	mgr  *autonomous.Manager
	sess *subTestSession
	ctx  context.Context
}

func newSubTestEnv(t *testing.T, on bool) *subTestEnv {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	cfg := &config.MCPConfig{ResourceSubscriptions: on}
	s := New("test-host", nil, cfg, t.TempDir(), Options{})
	s.RegisterResources()
	s.SetLifecycleBus(mgr)
	sess := newSubTestSession("client-1")
	if err := s.srv.RegisterSession(context.Background(), sess); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	return &subTestEnv{srv: s, mgr: mgr, sess: sess, ctx: s.srv.WithContext(context.Background(), sess)}
}

// call sends one JSON-RPC request and returns the decoded response object.
func (e *subTestEnv) call(t *testing.T, method string, params map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	resp := e.srv.srv.HandleMessage(e.ctx, raw)
	b, _ := json.Marshal(resp)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	return out
}

func (e *subTestEnv) emitQuestion(kind, prd string) {
	e.mgr.EmitQuestionEvent(autonomous.LifecycleEvent{Kind: kind, Level: autonomous.LevelPRD, PRDID: prd})
}

func (e *subTestEnv) waitNotification(t *testing.T) mcpsdk.JSONRPCNotification {
	t.Helper()
	select {
	case n := <-e.sess.ch:
		return n
	case <-time.After(3 * time.Second):
		t.Fatal("no notification arrived")
	}
	return mcpsdk.JSONRPCNotification{}
}

func (e *subTestEnv) expectSilence(t *testing.T) {
	t.Helper()
	select {
	case n := <-e.sess.ch:
		t.Fatalf("unexpected notification: %+v", n)
	case <-time.After(300 * time.Millisecond):
	}
}

func isErrorResponse(resp map[string]any) bool { _, ok := resp["error"]; return ok }

const subTestURI = "datawatch://events/lifecycle?kinds=question.answered"

func TestResourceSubscriptionDeliversMatchingEvent(t *testing.T) {
	e := newSubTestEnv(t, true)
	if resp := e.call(t, "resources/subscribe", map[string]any{"uri": subTestURI}); isErrorResponse(resp) {
		t.Fatalf("subscribe refused: %v", resp)
	}
	e.emitQuestion(autonomous.KindQuestionAnswered, "p1")
	n := e.waitNotification(t)
	if n.Method != "notifications/resources/updated" {
		t.Fatalf("method = %q", n.Method)
	}
	if got := n.Params.AdditionalFields["uri"]; got != subTestURI {
		t.Fatalf("uri = %v, want the subscribed URI", got)
	}
	meta, _ := n.Params.AdditionalFields["_meta"].(map[string]any)
	if meta["kind"] != autonomous.KindQuestionAnswered || meta["prd_id"] != "p1" {
		t.Fatalf("meta = %v", meta)
	}
}

func TestResourceSubscriptionFilterIsTheLifecycleFilter(t *testing.T) {
	e := newSubTestEnv(t, true)
	uri := "datawatch://events/lifecycle?kinds=question.answered&prd_id=only-this"
	e.call(t, "resources/subscribe", map[string]any{"uri": uri})
	e.emitQuestion(autonomous.KindQuestionAnswered, "other")      // wrong prd
	e.emitQuestion(autonomous.KindQuestionWithdrawn, "only-this") // wrong kind
	e.expectSilence(t)
	e.emitQuestion(autonomous.KindQuestionAnswered, "only-this")
	e.waitNotification(t)
}

func TestResourceSubscriptionUnsubscribeStopsDelivery(t *testing.T) {
	e := newSubTestEnv(t, true)
	e.call(t, "resources/subscribe", map[string]any{"uri": subTestURI})
	if resp := e.call(t, "resources/unsubscribe", map[string]any{"uri": subTestURI}); isErrorResponse(resp) {
		t.Fatalf("unsubscribe refused: %v", resp)
	}
	if n := e.srv.resourceSubs.count("client-1"); n != 0 {
		t.Fatalf("subscriptions held = %d, want 0", n)
	}
	e.emitQuestion(autonomous.KindQuestionAnswered, "p1")
	e.expectSilence(t)
	// Unsubscribing a URI that was never subscribed is not an error.
	if resp := e.call(t, "resources/unsubscribe", map[string]any{"uri": subTestURI}); isErrorResponse(resp) {
		t.Fatalf("second unsubscribe refused: %v", resp)
	}
}

func TestResourceSubscriptionSameURITwiceDeliversOnce(t *testing.T) {
	e := newSubTestEnv(t, true)
	e.call(t, "resources/subscribe", map[string]any{"uri": subTestURI})
	e.call(t, "resources/subscribe", map[string]any{"uri": subTestURI})
	if n := e.srv.resourceSubs.count("client-1"); n != 1 {
		t.Fatalf("subscriptions held = %d, want 1", n)
	}
	e.emitQuestion(autonomous.KindQuestionAnswered, "p1")
	e.waitNotification(t)
	e.expectSilence(t)
}

func TestResourceSubscriptionRefusesBadURIs(t *testing.T) {
	e := newSubTestEnv(t, true)
	for name, uri := range map[string]string{
		"other resource":   "datawatch://version",
		"no filter":        "datawatch://events/lifecycle",
		"unknown kind":     "datawatch://events/lifecycle?kinds=not.a.kind",
		"bad level":        "datawatch://events/lifecycle?level=galaxy",
		"unknown param":    "datawatch://events/lifecycle?kinds=question.answered&colour=red",
		"empty uri":        "",
		"prefix lookalike": "datawatch://events/lifecycle-extra?kinds=question.answered",
	} {
		resp := e.call(t, "resources/subscribe", map[string]any{"uri": uri})
		if !isErrorResponse(resp) {
			t.Errorf("%s: subscribe to %q was accepted", name, uri)
		}
	}
	if n := e.srv.resourceSubs.count("client-1"); n != 0 {
		t.Fatalf("refused subscriptions were registered: %d", n)
	}
}

func TestResourceSubscriptionQuotaPerSession(t *testing.T) {
	e := newSubTestEnv(t, true)
	for i := 0; i < maxResourceSubscriptionsPerSession; i++ {
		uri := "datawatch://events/lifecycle?kinds=question.answered&prd_id=p" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
		if resp := e.call(t, "resources/subscribe", map[string]any{"uri": uri}); isErrorResponse(resp) {
			t.Fatalf("subscription %d refused: %v", i, resp)
		}
	}
	over := "datawatch://events/lifecycle?kinds=question.answered&prd_id=one-too-many"
	if resp := e.call(t, "resources/subscribe", map[string]any{"uri": over}); !isErrorResponse(resp) {
		t.Fatal("subscription over the quota was accepted")
	}
}

func TestResourceSubscriptionSessionEndReleasesSubscriptions(t *testing.T) {
	e := newSubTestEnv(t, true)
	e.call(t, "resources/subscribe", map[string]any{"uri": subTestURI})
	e.srv.srv.UnregisterSession(e.ctx, e.sess.SessionID())
	if n := e.srv.resourceSubs.count("client-1"); n != 0 {
		t.Fatalf("subscriptions held after session end = %d", n)
	}
}

func TestResourceSubscriptionOffAdvertisesAndBehavesAsBefore(t *testing.T) {
	e := newSubTestEnv(t, false)
	if e.srv.ResourceSubscriptionsEnabled() {
		t.Fatal("resource subscriptions enabled with the setting off")
	}
	init := e.call(t, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "t", "version": "1"},
	})
	b, _ := json.Marshal(init)
	if strings.Contains(string(b), `"subscribe":true`) {
		t.Fatalf("subscribe advertised with the setting off: %s", b)
	}
	if resp := e.call(t, "resources/subscribe", map[string]any{"uri": subTestURI}); !isErrorResponse(resp) {
		t.Fatal("resources/subscribe accepted with the setting off")
	}
	list := e.call(t, "resources/list", map[string]any{})
	lb, _ := json.Marshal(list)
	if strings.Contains(string(lb), lifecycleEventsResourceURI) {
		t.Fatal("events resource listed with the setting off")
	}
}

func TestResourceSubscriptionOnAdvertisesSubscribe(t *testing.T) {
	e := newSubTestEnv(t, true)
	init := e.call(t, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "t", "version": "1"},
	})
	b, _ := json.Marshal(init)
	if !strings.Contains(string(b), `"subscribe":true`) {
		t.Fatalf("subscribe not advertised with the setting on: %s", b)
	}
	list, _ := json.Marshal(e.call(t, "resources/list", map[string]any{}))
	if !strings.Contains(string(list), lifecycleEventsResourceURI) {
		t.Fatalf("events resource not listed: %s", list)
	}
}

func TestResourceSubscriptionNoBusRefuses(t *testing.T) {
	e := newSubTestEnv(t, true)
	e.srv.SetLifecycleBus(nil)
	if resp := e.call(t, "resources/subscribe", map[string]any{"uri": subTestURI}); !isErrorResponse(resp) {
		t.Fatal("subscription accepted without an Automata manager")
	}
}

func TestResourceSubscriptionParseURI(t *testing.T) {
	f, err := ParseLifecycleResourceURI("datawatch://events/lifecycle?kinds=task.failed, prd.completed&level=task&prd_id=a&task_id=b&automation_id=c")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Kinds) != 2 || f.Kinds[0] != "task.failed" || f.Kinds[1] != "prd.completed" ||
		f.Level != autonomous.LevelTask || f.PRDID != "a" || f.TaskID != "b" || f.AutomationID != "c" {
		t.Fatalf("filter = %+v", f)
	}
	if _, err := ParseLifecycleResourceURI("datawatch://events/lifecycle?level=prd"); err != nil {
		t.Fatalf("level alone must be accepted: %v", err)
	}
}

// A kind the Automaton's notify set excludes is gated exactly as for a
// persisted subscription: the shared gate is consulted for every delivery.
func TestResourceSubscriptionUsesTheSharedDeliveryGate(t *testing.T) {
	bus := &gateBus{allow: false}
	r := newResourceSubscriptions()
	var mu sync.Mutex
	var sent int
	r.notify = func(string, string, map[string]any) error { mu.Lock(); sent++; mu.Unlock(); return nil }
	f := autonomous.LifecycleFilter{Kinds: []string{autonomous.KindQuestionAnswered}}
	ev := autonomous.LifecycleEvent{Kind: autonomous.KindQuestionAnswered}
	r.deliver(bus, "s", "u", f, ev)
	if sent != 0 || bus.asked != 1 {
		t.Fatalf("gated event delivered: sent=%d asked=%d", sent, bus.asked)
	}
	bus.allow = true
	r.deliver(bus, "s", "u", f, ev)
	if sent != 1 {
		t.Fatalf("allowed event not delivered: sent=%d", sent)
	}
}

type gateBus struct {
	allow bool
	asked int
}

func (g *gateBus) Subscribe(autonomous.LifecycleFilter, func(autonomous.LifecycleEvent)) func() {
	return func() {}
}
func (g *gateBus) LifecycleEventDeliverable(autonomous.LifecycleFilter, autonomous.LifecycleEvent) bool {
	g.asked++
	return g.allow
}

func TestResourceSubscriptionReadReturnsFilterAndReportsLoopbackError(t *testing.T) {
	e := newSubTestEnv(t, true)
	out, err := e.srv.handleLifecycleEventsResource(context.Background(), makeReq(subTestURI))
	if err != nil {
		t.Fatal(err)
	}
	text := out[0].(mcpsdk.TextResourceContents).Text
	var body map[string]any
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatal(err)
	}
	filter, _ := body["filter"].(map[string]any)
	if kinds, _ := filter["kinds"].([]any); len(kinds) != 1 || kinds[0] != "question.answered" {
		t.Fatalf("filter = %v", filter)
	}
	if body["error"] == nil {
		t.Fatalf("expected the loopback error with no web port: %v", body)
	}
	if _, err := e.srv.handleLifecycleEventsResource(context.Background(), makeReq("datawatch://events/lifecycle?kinds=bogus")); err == nil {
		t.Fatal("an invalid filter must be an error")
	}
}

func TestPeerEventNameResourceURIFilter(t *testing.T) {
	f, err := ParseLifecycleResourceURI("datawatch://events/lifecycle?level=prd&peer=peer-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if f.Peer != "peer-alpha" {
		t.Fatalf("Peer = %q, want peer-alpha", f.Peer)
	}
	if _, err := ParseLifecycleResourceURI("datawatch://events/lifecycle?level=prd&peer=bad%20name"); err == nil {
		t.Fatal("a peer name outside the registered-name charset was accepted")
	}
}
