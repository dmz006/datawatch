package autonomous_test

// Regression guard for the MCP resource subscription path.
//
// With mcp.resource_subscriptions on, an MCP client can subscribe to the
// lifecycle events resource and is notified through the same dispatcher and
// the same delivery gate a stored subscription uses. This file pins down two
// promises of that additive path:
//
//   - TestExistingSubscriptionDelivery: a stored subscription of every
//     existing sink type (alert, topic, mcp_queue, session) delivers exactly
//     the same events whether the resource path is off or on, and the stored
//     record is byte-for-byte untouched by either.
//   - TestMCPClientSubscriptionFilters: an MCP client subscription receives
//     exactly the events the same filter delivers to a stored subscription.
//
// This is an external test package because the resource path lives in
// internal/mcp, which imports internal/autonomous. The MCP side runs as the
// daemon runs it: a real server answering resources/subscribe over SSE.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mcp"
)

const (
	// pathTestTimeout bounds every wait for an asynchronous delivery.
	pathTestTimeout = 5 * time.Second
	// pathTestSettle is how long a test keeps watching after the expected
	// deliveries arrived, so an unexpected extra one is seen as a failure.
	pathTestSettle = 300 * time.Millisecond
	// pathTestResourceBase is the lifecycle events resource URI.
	pathTestResourceBase = "datawatch://events/lifecycle"
)

// pathTestCreatedAt is a fixed creation time, so a stored record serializes
// to the same bytes in every run.
var pathTestCreatedAt = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// waitUntil polls cond until it holds, then lets the system settle so a
// delivery that should not happen has the chance to show itself.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(pathTestTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(pathTestSettle)
}

// ---- sink recorder ----------------------------------------------------------

// pathRecorder captures what the alert, topic and session sinks deliver.
type pathRecorder struct {
	mu     sync.Mutex
	alerts []string
	topics []string
	inputs []string
}

func (r *pathRecorder) recordAlert(level, title, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.alerts = append(r.alerts, level+"|"+title+"|"+body)
}

func (r *pathRecorder) publishTopics(topics []string, title, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.topics = append(r.topics, strings.Join(topics, ",")+"|"+title+"|"+message)
}

func (r *pathRecorder) sendInput(fullID, text, source string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputs = append(r.inputs, fullID+"|"+source+"|"+text)
	return nil
}

func (r *pathRecorder) copyOf(list *[]string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), *list...)
}

// ---- real MCP server and client over SSE -------------------------------------

// resourceNote is one notifications/resources/updated message a client got.
// The wire message names the subscribed URI; the event itself is observed on
// the bus (recordingBus), because identifying it is the server's business.
type resourceNote struct {
	URI string
}

// sseMessage is one JSON-RPC message the server sent down the stream.
type sseMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params map[string]any  `json:"params"`
	Error  json.RawMessage `json:"error"`
}

// resourceClient is a minimal MCP client over the SSE transport.
type resourceClient struct {
	t      *testing.T
	msgURL string
	nextID int
	resps  chan sseMessage

	mu    sync.Mutex
	notes []resourceNote
}

// freeLoopbackPort returns a loopback TCP port nothing is listening on.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// startResourceServer runs a real MCP SSE server over the lifecycle bus with the resource
// subscription setting at on, and returns an initialized client connected to
// it. The server and the client stop with the test.
func startResourceServer(t *testing.T, bus mcp.LifecycleBus, on bool) *resourceClient {
	t.Helper()
	port := freeLoopbackPort(t)
	cfg := &config.MCPConfig{ResourceSubscriptions: on, SSEHost: "127.0.0.1", SSEPort: port}
	srv := mcp.New("path-test-host", nil, cfg, t.TempDir(), mcp.Options{})
	srv.RegisterResources()
	srv.SetLifecycleBus(bus)
	if srv.ResourceSubscriptionsEnabled() != on {
		t.Fatalf("ResourceSubscriptionsEnabled = %v, want %v", srv.ResourceSubscriptionsEnabled(), on)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.ServeSSE(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(pathTestTimeout):
		}
	})

	c := &resourceClient{t: t, resps: make(chan sseMessage, 16)}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	endpoint := c.openStream(ctx, base)
	if strings.HasPrefix(endpoint, "/") {
		endpoint = base + endpoint
	}
	c.msgURL = endpoint

	init := c.rpc("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "path-test", "version": "1"},
	})
	if len(init.Error) > 0 {
		t.Fatalf("initialize refused: %s", init.Error)
	}
	c.post(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return c
}

// openStream opens the SSE stream (retrying while the server starts), starts
// the reader and returns the message endpoint the server announces.
func (c *resourceClient) openStream(ctx context.Context, base string) string {
	c.t.Helper()
	var resp *http.Response
	deadline := time.Now().Add(pathTestTimeout)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/sse", nil)
		if err != nil {
			c.t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Accept", "text/event-stream")
		resp, err = http.DefaultClient.Do(req)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("open SSE stream: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		_ = resp.Body.Close()
		c.t.Fatalf("SSE status = %d: %s", resp.StatusCode, body)
	}

	endpoint := make(chan string, 1)
	go c.readStream(resp.Body, endpoint)
	select {
	case ep := <-endpoint:
		return ep
	case <-time.After(pathTestTimeout):
		c.t.Fatal("the server never announced a message endpoint")
	}
	return ""
}

// readStream decodes the SSE stream: the endpoint event, then JSON-RPC
// messages. A message with a method and no id is a notification.
func (c *resourceClient) readStream(body io.ReadCloser, endpoint chan<- string) {
	defer func() { _ = body.Close() }()
	r := bufio.NewReader(body)
	var event string
	var data []string
	for {
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "" && err == nil:
			c.dispatch(event, strings.Join(data, "\n"), endpoint)
			event, data = "", nil
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		if err != nil {
			return
		}
	}
}

func (c *resourceClient) dispatch(event, data string, endpoint chan<- string) {
	switch event {
	case "endpoint":
		select {
		case endpoint <- data:
		default:
		}
	case "message":
		var msg sseMessage
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			return
		}
		if msg.Method == "" || len(msg.ID) > 0 {
			c.resps <- msg
			return
		}
		if msg.Method != "notifications/resources/updated" {
			return
		}
		note := resourceNote{}
		note.URI, _ = msg.Params["uri"].(string)
		c.mu.Lock()
		c.notes = append(c.notes, note)
		c.mu.Unlock()
	}
}

// post sends one JSON-RPC message to the server.
func (c *resourceClient) post(msg map[string]any) {
	c.t.Helper()
	body, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(c.msgURL, "application/json", bytes.NewReader(body))
	if err != nil {
		c.t.Fatalf("post %v: %v", msg["method"], err)
	}
	_ = resp.Body.Close()
}

// rpc sends one request and waits for its response.
func (c *resourceClient) rpc(method string, params map[string]any) sseMessage {
	c.t.Helper()
	c.nextID++
	want := fmt.Sprint(c.nextID)
	c.post(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params})
	for {
		select {
		case msg := <-c.resps:
			if string(msg.ID) == want {
				return msg
			}
		case <-time.After(pathTestTimeout):
			c.t.Fatalf("no response to %s", method)
		}
	}
}

// subscribe asks the server to subscribe the client to uri; it returns the
// refusal, or nil when the server accepted.
func (c *resourceClient) subscribe(uri string) error {
	c.t.Helper()
	msg := c.rpc("resources/subscribe", map[string]any{"uri": uri})
	if len(msg.Error) > 0 {
		return fmt.Errorf("%s", msg.Error)
	}
	return nil
}

// received returns the notifications the client has so far.
func (c *resourceClient) received() []resourceNote {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]resourceNote(nil), c.notes...)
}

// countFor returns how many notifications arrived for one URI.
func (c *resourceClient) countFor(uri string) int {
	n := 0
	for _, note := range c.received() {
		if note.URI == uri {
			n++
		}
	}
	return n
}

// recordingBus is the lifecycle bus the MCP server is given: the Automata
// manager itself, observed. Every resource subscription asks
// LifecycleEventDeliverable before it notifies its client, so the events that
// call lets through are exactly the events that subscription delivers. The
// stored-subscription engine uses the manager directly and never passes here.
type recordingBus struct {
	mgr *autonomous.Manager

	mu       sync.Mutex
	accepted map[string][]string // filter (as JSON) -> IDs of the events let through
}

func newRecordingBus(mgr *autonomous.Manager) *recordingBus {
	return &recordingBus{mgr: mgr, accepted: make(map[string][]string)}
}

func (b *recordingBus) Subscribe(f autonomous.LifecycleFilter, fn func(autonomous.LifecycleEvent)) func() {
	return b.mgr.Subscribe(f, fn)
}

func (b *recordingBus) LifecycleEventDeliverable(f autonomous.LifecycleFilter, ev autonomous.LifecycleEvent) bool {
	ok := b.mgr.LifecycleEventDeliverable(f, ev)
	if ok {
		key := filterKey(f)
		b.mu.Lock()
		b.accepted[key] = append(b.accepted[key], ev.ID)
		b.mu.Unlock()
	}
	return ok
}

// deliveredFor returns the IDs of the events a subscription on f delivered.
func (b *recordingBus) deliveredFor(f autonomous.LifecycleFilter) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.accepted[filterKey(f)]...)
}

func filterKey(f autonomous.LifecycleFilter) string {
	raw, _ := json.Marshal(f)
	return string(raw)
}

// ---- harness: manager, stored subscriptions and (optionally) the MCP server --

// pathHarness is one Automata manager with the stored-subscription engine
// running over it and an MCP server whose resource path is on or off.
type pathHarness struct {
	t         *testing.T
	mgr       *autonomous.Manager
	store     *autonomous.SubscriptionStore
	storePath string
	rec       *pathRecorder
	queue     *autonomous.PendingEventQueue
	bus       *recordingBus
	client    *resourceClient
}

// pathSessions is the session lookup of the session sink: one known session.
func pathSessions(nameOrID string) (string, int, bool) {
	return nameOrID, 0, nameOrID == "worker-1"
}

func newPathHarness(t *testing.T, resourcePathOn bool) *pathHarness {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.Stop)
	// automaton-1 opts into the session-started kind; automaton-2 does not, so
	// the same kind from automaton-2 is stopped by the notify-event gate.
	for _, prd := range []*autonomous.PRD{
		{ID: "automaton-1", NotifyEvents: []string{autonomous.KindTaskSessionStarted}},
		{ID: "automaton-2"},
	} {
		if err := mgr.Store().SavePRD(prd); err != nil {
			t.Fatalf("save Automaton %s: %v", prd.ID, err)
		}
	}

	h := &pathHarness{
		t:         t,
		mgr:       mgr,
		storePath: filepath.Join(t.TempDir(), "subs.json"),
		rec:       &pathRecorder{},
		queue:     autonomous.NewPendingEventQueue(100),
	}
	h.store, err = autonomous.NewSubscriptionStore(h.storePath, nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	engine := autonomous.NewLifecycleSubscriptionEngine(mgr, h.store, autonomous.SessionPushConfig{
		FindSession:   pathSessions,
		SendInput:     h.rec.sendInput,
		RecordAlert:   h.rec.recordAlert,
		PublishTopics: h.rec.publishTopics,
		PendingQueue:  h.queue,
	})
	if err := engine.Start(); err != nil {
		t.Fatalf("engine Start: %v", err)
	}
	t.Cleanup(engine.Close)

	h.bus = newRecordingBus(mgr)
	h.client = startResourceServer(t, h.bus, resourcePathOn)
	return h
}

func (h *pathHarness) addSubscription(sub autonomous.Subscription) {
	h.t.Helper()
	if _, err := h.store.Add(sub); err != nil {
		h.t.Fatalf("store.Add %s: %v", sub.ID, err)
	}
}

// storedBytes returns the subscription file exactly as it is on disk.
func (h *pathHarness) storedBytes() []byte {
	h.t.Helper()
	raw, err := os.ReadFile(h.storePath)
	if err != nil {
		h.t.Fatalf("read subscription file: %v", err)
	}
	return raw
}

func (h *pathHarness) emit(events []autonomous.LifecycleEvent) {
	for _, ev := range events {
		h.mgr.EmitQuestionEvent(ev)
	}
}

// queued returns the event IDs waiting in one pending-events queue.
func (h *pathHarness) queued(name string) []string {
	events, _, _ := h.queue.Pending(name, 0, 100)
	ids := make([]string, 0, len(events))
	for _, ev := range events {
		ids = append(ids, ev.ID)
	}
	return ids
}

// ---- TestExistingSubscriptionDelivery ----------------------------------------

// existingDeliveryEvents is the event stream both runs see. Events 1 to 3 pass
// the notify-event gate; event 4 (a kind automaton-2 did not opt into) does not.
func existingDeliveryEvents() []autonomous.LifecycleEvent {
	return []autonomous.LifecycleEvent{
		{ID: "event-1", Kind: autonomous.KindQuestionAnswered, Level: autonomous.LevelPRD, PRDID: "automaton-1"},
		{ID: "event-2", Kind: autonomous.KindQuestionWithdrawn, Level: autonomous.LevelPRD, PRDID: "automaton-2"},
		{ID: "event-3", Kind: autonomous.KindTaskSessionStarted, Level: autonomous.LevelTask, PRDID: "automaton-1", TaskID: "task-1", SessionID: "worker-1"},
		{ID: "event-4", Kind: autonomous.KindTaskSessionStarted, Level: autonomous.LevelTask, PRDID: "automaton-2", TaskID: "task-9", SessionID: "worker-9"},
	}
}

// existingDeliveryFilter selects the three kinds the event stream carries.
func existingDeliveryFilter() autonomous.LifecycleFilter {
	return autonomous.LifecycleFilter{Kinds: []string{
		autonomous.KindQuestionAnswered, autonomous.KindQuestionWithdrawn, autonomous.KindTaskSessionStarted,
	}}
}

const existingDeliveryQueue = "regression-queue"

// existingSinkFor returns the stored sink of one existing sink type.
func existingSinkFor(sinkType string) autonomous.SubscriptionSink {
	switch sinkType {
	case autonomous.SinkAlert, autonomous.SinkTopic:
		return autonomous.SubscriptionSink{Type: sinkType, Target: "alerts"}
	case autonomous.SinkMCPQueue:
		return autonomous.SubscriptionSink{Type: sinkType, Target: existingDeliveryQueue}
	default:
		return autonomous.SubscriptionSink{Type: sinkType, Target: "worker-1"}
	}
}

// delivered returns what the sink of one type received, as comparable lines.
func (h *pathHarness) delivered(sinkType string) []string {
	switch sinkType {
	case autonomous.SinkAlert:
		return h.rec.copyOf(&h.rec.alerts)
	case autonomous.SinkTopic:
		return h.rec.copyOf(&h.rec.topics)
	case autonomous.SinkSession:
		return h.rec.copyOf(&h.rec.inputs)
	case autonomous.SinkMCPQueue:
		events, _, _ := h.queue.Pending(existingDeliveryQueue, 0, 100)
		lines := make([]string, 0, len(events))
		for _, ev := range events {
			lines = append(lines, strings.Join([]string{ev.ID, ev.Kind, string(ev.Level), ev.PRDID, ev.TaskID, ev.SessionID}, "|"))
		}
		return lines
	}
	return nil
}

// wantDelivered is what a sink of the given type must have received for the
// events that pass the gate, derived from the sinks' documented line formats.
func wantDelivered(sinkType string, passing []autonomous.LifecycleEvent) []string {
	var lines []string
	for _, ev := range passing {
		text := autonomous.DescribeLifecycleEvent(ev)
		switch sinkType {
		case autonomous.SinkAlert:
			lines = append(lines, "warn|"+ev.Kind+"|"+text)
		case autonomous.SinkTopic:
			lines = append(lines, "alerts,automaton-"+ev.PRDID+"|"+ev.Kind+"|"+text)
		case autonomous.SinkSession:
			lines = append(lines, "worker-1|lifecycle-sub|"+text)
		case autonomous.SinkMCPQueue:
			lines = append(lines, strings.Join([]string{ev.ID, ev.Kind, string(ev.Level), ev.PRDID, ev.TaskID, ev.SessionID}, "|"))
		}
	}
	return lines
}

// existingRun is what one run of the delivery scenario observed.
type existingRun struct {
	delivered    []string
	storedBefore []byte
	storedAfter  []byte
	listed       []byte
	notified     []string // IDs of the events the MCP client subscription delivered
	notes        int      // notifications that reached the MCP client
}

// runExistingDelivery stores one subscription of sinkType, emits the event
// stream and records what the sink received. With resourcePathOn the MCP
// client also holds a subscription to the same filter, so both paths are live
// at once; with it off the server refuses resources/subscribe.
func runExistingDelivery(t *testing.T, sinkType string, resourcePathOn bool) existingRun {
	t.Helper()
	h := newPathHarness(t, resourcePathOn)
	h.addSubscription(autonomous.Subscription{
		ID:        "sub-existing",
		Owner:     "operator",
		Filter:    existingDeliveryFilter(),
		Sink:      existingSinkFor(sinkType),
		CreatedAt: pathTestCreatedAt,
	})
	run := existingRun{storedBefore: h.storedBytes()}

	uri := pathTestResourceBase + "?kinds=question.answered,question.withdrawn,task.session_started"
	err := h.client.subscribe(uri)
	switch {
	case resourcePathOn && err != nil:
		t.Fatalf("resource path on: subscribe refused: %v", err)
	case !resourcePathOn && err == nil:
		t.Fatal("resource path off: resources/subscribe was accepted")
	}

	h.emit(existingDeliveryEvents())
	const passing = 3
	waitUntil(t, "the stored subscription's deliveries", func() bool { return len(h.delivered(sinkType)) >= passing })
	if resourcePathOn {
		waitUntil(t, "the client's notifications", func() bool { return len(h.client.received()) >= passing })
	}

	run.delivered = h.delivered(sinkType)
	run.storedAfter = h.storedBytes()
	run.listed, _ = json.Marshal(h.store.List(""))
	run.notified = h.bus.deliveredFor(existingDeliveryFilter())
	run.notes = h.client.countFor(uri)
	return run
}

// TestExistingSubscriptionDelivery proves a stored subscription of each
// existing sink type delivers identical events with the MCP resource path on
// and off, and that the stored JSON is byte-identical.
func TestExistingSubscriptionDelivery(t *testing.T) {
	passing := existingDeliveryEvents()[:3]
	for _, sinkType := range []string{
		autonomous.SinkAlert, autonomous.SinkTopic, autonomous.SinkMCPQueue, autonomous.SinkSession,
	} {
		t.Run(sinkType, func(t *testing.T) {
			off := runExistingDelivery(t, sinkType, false)
			on := runExistingDelivery(t, sinkType, true)

			// The baseline is real delivery, not an empty run on both sides.
			want := wantDelivered(sinkType, passing)
			if !reflect.DeepEqual(off.delivered, want) {
				t.Fatalf("resource path off: delivered %q, want %q", off.delivered, want)
			}
			if !reflect.DeepEqual(on.delivered, off.delivered) {
				t.Fatalf("delivery differs with the resource path on:\n on:  %q\n off: %q", on.delivered, off.delivered)
			}

			// The stored record is never rewritten by delivery, on either path.
			for name, run := range map[string]existingRun{"off": off, "on": on} {
				if !bytes.Equal(run.storedBefore, run.storedAfter) {
					t.Errorf("resource path %s: stored JSON changed while delivering:\n before: %s\n after:  %s", name, run.storedBefore, run.storedAfter)
				}
			}
			if !bytes.Equal(off.storedAfter, on.storedAfter) {
				t.Errorf("stored JSON differs between resource path off and on:\n off: %s\n on:  %s", off.storedAfter, on.storedAfter)
			}
			if !bytes.Equal(off.listed, on.listed) {
				t.Errorf("listed subscriptions differ between resource path off and on:\n off: %s\n on:  %s", off.listed, on.listed)
			}

			// With the path on the client was notified too, so the on run really
			// exercised both consumers of the one dispatcher.
			if got, wantIDs := on.notified, []string{"event-1", "event-2", "event-3"}; !reflect.DeepEqual(got, wantIDs) {
				t.Errorf("resource path on: client subscription delivered %q, want %q", got, wantIDs)
			}
			if on.notes != len(on.notified) {
				t.Errorf("resource path on: client got %d notifications for %d delivered events", on.notes, len(on.notified))
			}
			if len(off.notified) != 0 || off.notes != 0 {
				t.Errorf("resource path off: client subscription delivered %q (%d notifications), want nothing", off.notified, off.notes)
			}
		})
	}
}

// ---- TestMCPClientSubscriptionFilters ----------------------------------------

// filterTestEvents carries every dimension a filter can select on: kind,
// level, Automaton and task. Event 6 is a kind its Automaton did not opt
// into, so the delivery gate stops it for both paths.
func filterTestEvents() []autonomous.LifecycleEvent {
	return []autonomous.LifecycleEvent{
		{ID: "event-1", Kind: autonomous.KindQuestionAnswered, Level: autonomous.LevelPRD, PRDID: "automaton-1"},
		{ID: "event-2", Kind: autonomous.KindQuestionAnswered, Level: autonomous.LevelPRD, PRDID: "automaton-2"},
		{ID: "event-3", Kind: autonomous.KindQuestionWithdrawn, Level: autonomous.LevelPRD, PRDID: "automaton-1"},
		{ID: "event-4", Kind: autonomous.KindTaskSessionStarted, Level: autonomous.LevelTask, PRDID: "automaton-1", TaskID: "task-1"},
		{ID: "event-5", Kind: autonomous.KindTaskSessionStarted, Level: autonomous.LevelTask, PRDID: "automaton-1", TaskID: "task-2"},
		{ID: "event-6", Kind: autonomous.KindTaskSessionStarted, Level: autonomous.LevelTask, PRDID: "automaton-2", TaskID: "task-1"},
		{ID: "event-7", Kind: autonomous.KindQuestionAnswered, Level: autonomous.LevelTask, PRDID: "automaton-1", TaskID: "task-1"},
	}
}

// TestMCPClientSubscriptionFilters proves an MCP client subscription matches
// exactly the events the same filter matches for a stored subscription.
func TestMCPClientSubscriptionFilters(t *testing.T) {
	h := newPathHarness(t, true)

	// An Automation owning automaton-1 only, so the Automation scope parameter
	// is resolved at delivery time the way a stored subscription's is.
	automation, err := h.mgr.AutomationStore().Create(&autonomous.Automation{
		Name:    "filter scope",
		Members: []autonomous.AutomationMember{{ID: "m1", PRDID: "automaton-1"}},
	})
	if err != nil {
		t.Fatalf("create Automation: %v", err)
	}

	cases := []struct {
		name   string
		query  string
		filter autonomous.LifecycleFilter
		want   []string // event IDs the filter delivers
	}{
		{
			name:   "one kind",
			query:  "kinds=question.answered",
			filter: autonomous.LifecycleFilter{Kinds: []string{autonomous.KindQuestionAnswered}},
			want:   []string{"event-1", "event-2", "event-7"},
		},
		{
			name:  "two kinds",
			query: "kinds=question.answered,question.withdrawn",
			filter: autonomous.LifecycleFilter{Kinds: []string{
				autonomous.KindQuestionAnswered, autonomous.KindQuestionWithdrawn,
			}},
			want: []string{"event-1", "event-2", "event-3", "event-7"},
		},
		{
			name:   "task level",
			query:  "level=task",
			filter: autonomous.LifecycleFilter{Level: autonomous.LevelTask},
			want:   []string{"event-4", "event-5", "event-7"},
		},
		{
			name:   "Automaton level",
			query:  "level=prd",
			filter: autonomous.LifecycleFilter{Level: autonomous.LevelPRD},
			want:   []string{"event-1", "event-2", "event-3"},
		},
		{
			name:   "kind and Automaton",
			query:  "kinds=task.session_started&prd_id=automaton-1",
			filter: autonomous.LifecycleFilter{Kinds: []string{autonomous.KindTaskSessionStarted}, PRDID: "automaton-1"},
			want:   []string{"event-4", "event-5"},
		},
		{
			name:   "kind and task",
			query:  "kinds=task.session_started&task_id=task-1",
			filter: autonomous.LifecycleFilter{Kinds: []string{autonomous.KindTaskSessionStarted}, TaskID: "task-1"},
			want:   []string{"event-4"},
		},
		{
			name:   "level, Automaton and task",
			query:  "level=task&prd_id=automaton-1&task_id=task-1",
			filter: autonomous.LifecycleFilter{Level: autonomous.LevelTask, PRDID: "automaton-1", TaskID: "task-1"},
			want:   []string{"event-4", "event-7"},
		},
		{
			name:   "Automation scope",
			query:  "level=prd&automation_id=" + automation.ID,
			filter: autonomous.LifecycleFilter{Level: autonomous.LevelPRD, AutomationID: automation.ID},
			want:   []string{"event-1", "event-3"},
		},
		{
			name:   "gated kind delivers nothing",
			query:  "kinds=task.session_started&prd_id=automaton-2",
			filter: autonomous.LifecycleFilter{Kinds: []string{autonomous.KindTaskSessionStarted}, PRDID: "automaton-2"},
			want:   nil,
		},
	}

	totalWant := 0
	for i, tc := range cases {
		uri := pathTestResourceBase + "?" + tc.query
		// The URI names the same filter the stored record carries.
		parsed, err := mcp.ParseLifecycleResourceURI(uri)
		if err != nil {
			t.Fatalf("%s: parse %s: %v", tc.name, uri, err)
		}
		if !reflect.DeepEqual(parsed, tc.filter) {
			t.Fatalf("%s: URI parses to %+v, want %+v", tc.name, parsed, tc.filter)
		}
		h.addSubscription(autonomous.Subscription{
			ID:        fmt.Sprintf("sub-filter-%d", i),
			Filter:    tc.filter,
			Sink:      autonomous.SubscriptionSink{Type: autonomous.SinkMCPQueue, Target: fmt.Sprintf("filter-%d", i)},
			CreatedAt: pathTestCreatedAt,
		})
		if err := h.client.subscribe(uri); err != nil {
			t.Fatalf("%s: subscribe refused: %v", tc.name, err)
		}
		totalWant += len(tc.want)
	}

	h.emit(filterTestEvents())
	waitUntil(t, "every stored subscription and client subscription to deliver", func() bool {
		stored := 0
		for i := range cases {
			stored += len(h.queued(fmt.Sprintf("filter-%d", i)))
		}
		return stored >= totalWant && len(h.client.received()) >= totalWant
	})

	if n := len(h.client.received()); n != totalWant {
		t.Errorf("client received %d notifications in total, want %d", n, totalWant)
	}
	for i, tc := range cases {
		uri := pathTestResourceBase + "?" + tc.query
		stored := h.queued(fmt.Sprintf("filter-%d", i))
		client := h.bus.deliveredFor(tc.filter)
		if !reflect.DeepEqual(emptyIfNil(stored), emptyIfNil(tc.want)) {
			t.Errorf("%s: stored subscription delivered %q, want %q", tc.name, stored, tc.want)
		}
		if !reflect.DeepEqual(emptyIfNil(client), emptyIfNil(stored)) {
			t.Errorf("%s: client subscription delivered %q, the stored subscription with the same filter %q", tc.name, client, stored)
		}
		if n := h.client.countFor(uri); n != len(client) {
			t.Errorf("%s: client got %d notifications for %d delivered events", tc.name, n, len(client))
		}
	}
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
