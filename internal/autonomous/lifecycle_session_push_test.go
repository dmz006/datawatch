// BL427 (phase 3) — tests for the session-push delivery engine
// (LifecycleSubscriptionEngine). Covers the verify contract:
// bridge preferred when the session has a ChannelPort, SendInput
// fallback on bridge error / port 0, send_input actions targeting the
// newly started worker session, one-shot removal ONLY after successful
// delivery (retry once, then retain), expired subscriptions never
// firing, slow subscribers never blocking the executor's emit path,
// delete cancelling in-flight/queued delivery, and the audit
// create/remove records going through the SetAuditFn path.

package autonomous

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// sessionPushRecorder captures bridge / SendInput / comm deliveries.
type sessionPushRecorder struct {
	mu         sync.Mutex
	bridge     []string // "port|sessionID|text"
	input      []string // "fullID|source|text"
	comm       []string // message texts
	bridgeErr  error
	inputErr   error
	inputDelay time.Duration
	block      chan struct{} // when non-nil, SendInput waits on it
	entered    bool          // set when SendInput (or bridge) has started
}

func (r *sessionPushRecorder) postBridge(port int, sessionID, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entered = true
	r.bridge = append(r.bridge, fmt.Sprintf("%d|%s|%s", port, sessionID, text))
	return r.bridgeErr
}

func (r *sessionPushRecorder) sendInput(fullID, text, source string) error {
	r.mu.Lock()
	delay, block := r.inputDelay, r.block
	inputErr := r.inputErr
	r.entered = true
	r.mu.Unlock()
	if block != nil {
		<-block
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	r.mu.Lock()
	r.input = append(r.input, fullID+"|"+source+"|"+text)
	r.mu.Unlock()
	return inputErr
}

func (r *sessionPushRecorder) commSend(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.comm = append(r.comm, text)
}

func (r *sessionPushRecorder) bridgeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bridge)
}

func (r *sessionPushRecorder) inputCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.input)
}

// sessionTable is a name/fullID -> (fullID, port) lookup table for
// SessionPushConfig.FindSession.
type sessionTable map[string]int

func (st sessionTable) find(nameOrID string) (string, int, bool) {
	port, ok := st[nameOrID]
	if !ok {
		return "", 0, false
	}
	return nameOrID, port, true // fullID == name for test brevity
}

// newEngineForTest wires a Manager + store + engine with the recorder's
// delivery hooks and starts the engine. Cleanup closes the engine and
// the dispatcher.
func newEngineForTest(t *testing.T, rec *sessionPushRecorder, table sessionTable, commWindow time.Duration) (*Manager, *SubscriptionStore, *LifecycleSubscriptionEngine) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	store, err := NewSubscriptionStore(t.TempDir()+"/subs.json", nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	cfg := SessionPushConfig{
		FindSession: table.find,
		PostBridge:  rec.postBridge,
		SendInput:   rec.sendInput,
		Comm:        CommSinkConfig{Send: rec.commSend, Window: commWindow},
	}
	e := NewLifecycleSubscriptionEngine(m, store, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		e.Close()
		m.lifecycleBus().Close()
	})
	return m, store, e
}

// startedEvent is one task.session_started event: PRD prd-1, task
// task-1, worker session "worker-1".
func startedEvent() LifecycleEvent {
	return LifecycleEvent{
		Kind:      KindTaskSessionStarted,
		Level:     LevelTask,
		PRDID:     "prd-1",
		TaskID:    "task-1",
		SessionID: "worker-1",
	}
}

// TestSessionPushBridgePreferred — a session sink whose target has a
// ChannelPort (> 0) delivers through the bridge POST and never through
// SendInput.
func TestSessionPushBridgePreferred(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, _ := newEngineForTest(t, rec, sessionTable{"ops": 9013}, 0)

	sub, err := store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(startedEvent())

	waitFor(t, "bridge delivery", func() bool { return rec.bridgeCount() == 1 })
	if got := rec.inputCount(); got != 0 {
		t.Errorf("SendInput called %d times with a live bridge port, want 0", got)
	}
	call := rec.bridge[0]
	if !strings.HasPrefix(call, "9013|") {
		t.Errorf("bridge call %q is not against the session's ChannelPort 9013", call)
	}
	if !strings.Contains(call, "PRD prd-1") {
		t.Errorf("bridge text %q does not describe the matched event", call)
	}
	if sub.ExpiresAt != nil || !sub.OneShot {
		// non-one-shot subscriptions are untouched by delivery
		if _, ok := store.Get(sub.ID); !ok {
			t.Errorf("non-one-shot subscription %s was removed after delivery", sub.ID)
		}
	}
}

// TestSessionPushFallbackOnBridgeError — when the bridge POST errors
// (connection refused, timeout, non-2xx), delivery falls back to
// SendInput with the "lifecycle-sub" source tag.
func TestSessionPushFallbackOnBridgeError(t *testing.T) {
	rec := &sessionPushRecorder{bridgeErr: fmt.Errorf("connection refused")}
	m, store, _ := newEngineForTest(t, rec, sessionTable{"ops": 9013}, 0)

	if _, err := store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(startedEvent())

	waitFor(t, "SendInput fallback", func() bool { return rec.inputCount() == 1 })
	if rec.bridgeCount() != 1 {
		t.Errorf("bridge was tried %d times, want exactly 1 before the fallback", rec.bridgeCount())
	}
	call := rec.input[0]
	parts := strings.SplitN(call, "|", 3)
	if parts[0] != "ops" || parts[1] != sessionPushSourceTag {
		t.Errorf("fallback SendInput %q: want fullID %q and source %q", call, "ops", sessionPushSourceTag)
	}
}

// TestSessionPushFallbackWhenNoPort — port 0 means the session has no
// channel bridge: go straight to SendInput without a bridge attempt.
func TestSessionPushFallbackWhenNoPort(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, _ := newEngineForTest(t, rec, sessionTable{"legacy": 0}, 0)

	if _, err := store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "legacy"},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(startedEvent())

	waitFor(t, "SendInput fallback", func() bool { return rec.inputCount() == 1 })
	if rec.bridgeCount() != 0 {
		t.Errorf("bridge was attempted %d times for a port-0 session, want 0", rec.bridgeCount())
	}
}

// TestSessionPushUnknownSession — an unresolvable target fails
// delivery (both retries logged) and does not panic or crash the
// engine; the subscription stays armed.
func TestSessionPushUnknownSession(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, _ := newEngineForTest(t, rec, sessionTable{}, 0)

	sub, err := store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ghost"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(startedEvent())
	time.Sleep(150 * time.Millisecond) // let both attempts run and log

	if rec.bridgeCount() != 0 || rec.inputCount() != 0 {
		t.Errorf("unknown session delivered via bridge(%d)/input(%d), want neither", rec.bridgeCount(), rec.inputCount())
	}
	if _, ok := store.Get(sub.ID); !ok {
		t.Errorf("failed delivery must not consume the subscription")
	}
}

// TestSessionPushSendInputTargetsNewWorker — a send_input ACTION
// subscription on task.session_started delivers the action text to the
// session the event records (the newly started worker), not to the
// subscription's owner target.
func TestSessionPushSendInputTargetsNewWorker(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, _ := newEngineForTest(t, rec, sessionTable{"ops": 0, "worker-1": 0}, 0)

	if _, err := store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
		Action: &SubscriptionAction{Type: SinkActionSendInput, Text: "hello from the lifecycle engine"},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(startedEvent())

	waitFor(t, "worker send_input", func() bool { return rec.inputCount() == 1 })
	call := rec.input[0]
	if !strings.HasPrefix(call, "worker-1|") {
		t.Errorf("send_input action went to %q, want the worker session %q from the event", call, "worker-1")
	}
	if !strings.Contains(call, "hello from the lifecycle engine") {
		t.Errorf("delivered text %q is not the subscription's action text", call)
	}
	if strings.Contains(call, "ops|") {
		t.Errorf("action delivery leaked to the owner target: %q", call)
	}
}

// TestSessionPushOneShotDeletedOnlyAfterSuccess — a one-shot
// subscription is removed from the store exactly once, after a
// SUCCESSFUL delivery, and the removal is recorded in the audit hook.
func TestSessionPushOneShotDeletedOnlyAfterSuccess(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, e := newEngineForTest(t, rec, sessionTable{"ops": 9013}, 0)

	var auditMu sync.Mutex
	var audited []string
	e.auditHook = func(action string, details map[string]any) {
		auditMu.Lock()
		audited = append(audited, fmt.Sprintf("%s|%v", action, details["subscription_id"]))
		auditMu.Unlock()
	}

	sub, err := store.Add(Subscription{
		Owner:   "alice",
		Filter:  LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:    SubscriptionSink{Type: SinkSession, Target: "ops"},
		OneShot: true,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(startedEvent())

	waitFor(t, "one-shot removal", func() bool {
		_, ok := store.Get(sub.ID)
		return !ok
	})
	if rec.bridgeCount() != 1 {
		t.Errorf("one-shot delivered via bridge %d times, want 1", rec.bridgeCount())
	}

	// A second matching event must not re-deliver: the registration is gone.
	m.emit(startedEvent())
	time.Sleep(150 * time.Millisecond)
	if got := rec.bridgeCount(); got != 1 {
		t.Errorf("deleted one-shot fired again (bridge calls now %d), want 1 total", got)
	}

	auditMu.Lock()
	actions := strings.Join(audited, " ")
	auditMu.Unlock()
	if !strings.Contains(actions, auditLifecycleSubCreate+"|"+sub.ID) {
		t.Errorf("audit records %q lack the create for %s", actions, sub.ID)
	}
	if !strings.Contains(actions, auditLifecycleSubRemove+"|"+sub.ID) {
		t.Errorf("audit records %q lack the removal for consumed one-shot %s", actions, sub.ID)
	}
}

// TestSessionPushOneShotSendInputTargetsWorker — the watch-and-inject
// shape combined: a task.session_started + send_input action + one-shot
// subscription. It locks the dispatcher path end to end (event →
// Manager.Subscribe → engine → session adapter → injected sender) and
// the one-shot consume-after-success with an action: the ACTION text is
// typed into the newly started worker exactly once (SendInput fallback,
// tagged), never leaked to the owner target, and the record is deleted
// ONLY after that delivery succeeds. This is the exact case the
// --task/--on session_started/--send/--one-shot shorthand produces.
func TestSessionPushOneShotSendInputTargetsWorker(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, e := newEngineForTest(t, rec, sessionTable{"worker-1": 0}, 0)

	var auditMu sync.Mutex
	var audited []string
	e.auditHook = func(action string, details map[string]any) {
		auditMu.Lock()
		audited = append(audited, fmt.Sprintf("%s|%v", action, details["subscription_id"]))
		auditMu.Unlock()
	}

	sub, err := store.Add(Subscription{
		Owner:   "alice",
		Filter:  LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:    SubscriptionSink{Type: SinkSession, Target: "ops"}, // owner target: ignored for an action
		Action:  &SubscriptionAction{Type: SinkActionSendInput, Text: "Start with the Setup section of TASK.md"},
		OneShot: true,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(startedEvent()) // worker-1 at port 0 → SendInput fallback

	waitFor(t, "worker send_input delivery", func() bool { return rec.inputCount() == 1 })

	call := rec.input[0]
	if !strings.HasPrefix(call, "worker-1|") {
		t.Errorf("send_input action went to %q, want the worker session %q from the event", call, "worker-1")
	}
	if !strings.Contains(call, "Start with the Setup section of TASK.md") {
		t.Errorf("delivered text %q is not the subscription's action text", call)
	}
	if !strings.Contains(call, "lifecycle-sub") {
		t.Errorf("delivered text %q lacks the %q source tag", call, sessionPushSourceTag)
	}
	if strings.Contains(call, "ops|") {
		t.Errorf("action delivery leaked to the owner target: %q", call)
	}
	if rec.bridgeCount() != 0 {
		t.Errorf("bridge attempted %d times for a port-0 worker, want 0", rec.bridgeCount())
	}

	// One-shot consumed: a second matching event re-delivers nothing, and
	// the record is left empty in the store.
	m.emit(startedEvent())
	time.Sleep(150 * time.Millisecond)
	if got := rec.inputCount(); got != 1 {
		t.Errorf("one-shot send_input fired %d times, want exactly 1", got)
	}
	waitFor(t, "one-shot removal", func() bool {
		_, ok := store.Get(sub.ID)
		return !ok
	})

	auditMu.Lock()
	actions := strings.Join(audited, " ")
	auditMu.Unlock()
	if !strings.Contains(actions, auditLifecycleSubCreate+"|"+sub.ID) {
		t.Errorf("audit records %q lack the create for %s", actions, sub.ID)
	}
	if !strings.Contains(actions, auditLifecycleSubRemove+"|"+sub.ID) {
		t.Errorf("audit records %q lack the removal for consumed one-shot %s", actions, sub.ID)
	}
}

// TestSessionPushOneShotRetriedOnceThenRetained — a failing one-shot
// delivery retries exactly once (2 attempts total), logs, and LEAVES
// the subscription armed for a later event.
func TestSessionPushOneShotRetriedOnceThenRetained(t *testing.T) {
	rec := &sessionPushRecorder{inputErr: fmt.Errorf("session busy")}
	m, store, _ := newEngineForTest(t, rec, sessionTable{"ops": 0}, 0)

	sub, err := store.Add(Subscription{
		Filter:  LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:    SubscriptionSink{Type: SinkSession, Target: "ops"},
		OneShot: true,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(startedEvent())

	waitFor(t, "two failed attempts", func() bool { return rec.inputCount() == 2 })
	if _, ok := store.Get(sub.ID); !ok {
		t.Errorf("one-shot %s was deleted despite two failed deliveries — it must stay armed", sub.ID)
	}
}

// TestSessionPushExpiredSubNeverFires — a subscription whose ExpiresAt
// has passed (at resync time) is never registered, so a matching event
// delivers nothing.
func TestSessionPushExpiredSubNeverFires(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, e := newEngineForTest(t, rec, sessionTable{"ops": 9013}, 0)

	expires := time.Now().Add(2 * time.Second)
	if _, err := store.Add(Subscription{
		Filter:    LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:      SubscriptionSink{Type: SinkSession, Target: "ops"},
		ExpiresAt: &expires,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// The engine's clock is PAST the expiry: resync must skip it.
	e.mu.Lock()
	e.nowFn = func() time.Time { return time.Now().Add(1 * time.Hour) }
	e.mu.Unlock()
	e.resync()

	m.emit(startedEvent())
	time.Sleep(200 * time.Millisecond)

	if rec.bridgeCount() != 0 || rec.inputCount() != 0 {
		t.Errorf("expired subscription delivered (bridge=%d, input=%d), want 0/0", rec.bridgeCount(), rec.inputCount())
	}
}

// TestSessionPushSlowSubscriberDoesNotBlockEmit — a slow subscriber
// (SendInput stalling on a busy session) must never hold up the
// executor: emit returns promptly and the delivery lands later on the
// subscriber's own goroutine.
func TestSessionPushSlowSubscriberDoesNotBlockEmit(t *testing.T) {
	rec := &sessionPushRecorder{inputDelay: 300 * time.Millisecond}
	m, store, _ := newEngineForTest(t, rec, sessionTable{"ops": 0}, 0)

	if _, err := store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	start := time.Now()
	m.emit(startedEvent())
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("emit blocked on a slow subscriber: took %v", elapsed)
	}

	// Meanwhile a second event must have been QUEUED, not dropped, and
	// both deliver in order once the slow call finishes.
	m.emit(startedEvent())
	waitFor(t, "both queued events delivered", func() bool { return rec.inputCount() == 2 })
}

// TestSessionPushDeleteCancelsDelivery — deleting a subscription
// mid-delivery cancels it: the in-flight send may finish, but a later
// event reaches nothing anymore (exactly one delivery total).
func TestSessionPushDeleteCancelsDelivery(t *testing.T) {
	rec := &sessionPushRecorder{block: make(chan struct{})}
	m, store, _ := newEngineForTest(t, rec, sessionTable{"ops": 0}, 0)

	sub, err := store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(startedEvent())

	// Wait for the handler to enter the (blocked) SendInput, then
	// delete the subscription.
	waitFor(t, "in-flight delivery entered", func() bool { return rec.inputEntered() })
	if err := store.Delete(sub.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// A matching event AFTER the cancel must not deliver.
	m.emit(startedEvent())

	// Release the in-flight delivery; it completes exactly once.
	close(rec.block)
	waitFor(t, "in-flight delivery completes", func() bool { return rec.inputCount() == 1 })
	time.Sleep(200 * time.Millisecond)
	if got := rec.inputCount(); got != 1 {
		t.Errorf("SendInput called %d times after delete, want exactly 1 (the in-flight one)", got)
	}
}

// TestSessionPushCommCoalesced — a coalescing comm subscription batches
// same-Automaton events into ONE message after the quiet window.
func TestSessionPushCommCoalesced(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, _ := newEngineForTest(t, rec, sessionTable{"team": 0}, 100*time.Millisecond)

	if _, err := store.Add(Subscription{
		Filter:   LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:     SubscriptionSink{Type: SinkComm, Target: "team"},
		Coalesce: true,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ev := startedEvent()
	ev.TaskID = "task-a"
	m.emit(ev)
	ev.TaskID = "task-b"
	m.emit(ev)

	waitFor(t, "coalesced comm flush", func() bool { return rec.commCount() >= 1 })
	time.Sleep(300 * time.Millisecond) // allow (incorrect) extra flushes to appear
	if got := rec.commCount(); got != 1 {
		t.Fatalf("coalescing produced %d comm sends for two same-PRD events in one window, want 1", got)
	}
	msg := rec.comm[0]
	if !strings.Contains(msg, "task-a") || !strings.Contains(msg, "task-b") {
		t.Errorf("coalesced message %q lacks one of the events (want both task lines)", msg)
	}
}

func (r *sessionPushRecorder) commCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.comm)
}

func (r *sessionPushRecorder) inputEntered() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entered
}
