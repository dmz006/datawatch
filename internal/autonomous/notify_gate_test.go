package autonomous

// Tests for the notify-event gate (Manager.NotifyEventAllowed, wired in
// InstallLifecycleSinks's subscription handler and in the session-push
// engine's handle callback): the resolved set — Automaton's own
// notify_events > guardrail/profile default > global
// (autonomous.notify.default_events) — is applied FIRST, and a
// subscription or sink filter only narrows within it. Test-fire
// (Test: true) and non-notify-kind events are never gated.

import (
	"testing"
	"time"
)

// saveAutomaton persists an Automaton (PRD record) on m with exactly the
// given notify_events, so a gate test controls the resolved set for its
// events' automaton.
func saveAutomaton(t *testing.T, m *Manager, prdID string, notifyEvents []string) {
	t.Helper()
	if err := m.Store().SavePRD(&PRD{ID: prdID, NotifyEvents: notifyEvents}); err != nil {
		t.Fatalf("SavePRD(%s): %v", prdID, err)
	}
}

// TestNotifyGateResolvesPerAutomaton — the resolver runs first: a kind
// OUT of the global (default) tier still delivers when the event's
// Automaton opted in (prd.rejected is not one of the five shipped
// defaults), and a kind IN the global tier delivers with no Automaton
// persisted at all (prd.completed is a default) — one manager, two
// outcomes.
func TestNotifyGateResolvesPerAutomaton(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.lifecycleBus().Close()

	rec := &alertRecorder{}
	cancel := InstallLifecycleSinks(m, SinkConfig{RecordAlert: rec.Record})
	defer cancel()

	// Automaton tier: rejected is NOT in the global five-kind baseline,
	// but this Automaton's own set includes it → delivers.
	saveAutomaton(t, m, "prd-optin", []string{PRDStatusKind(PRDRejected)})
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDRejected), Level: LevelPRD, PRDID: "prd-optin", NewStatus: string(PRDRejected)})
	waitFor(t, "opt-in tier delivery", func() bool { return rec.count() == 1 })

	// Global tier: no Automaton persisted for prd-unknown (missing
	// automaton falls back to the global set); completed IS a default →
	// delivers.
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: "prd-unknown", NewStatus: string(PRDCompleted)})
	waitFor(t, "global tier delivery", func() bool { return rec.count() == 2 })

	if got := rec.count(); got != 2 {
		t.Errorf("RecordAlert called %d times, want exactly 2 (one per resolved kind)", got)
	}
}

// TestNotifyGateBlocksUnresolvedKind — with no Automaton persisted for
// the event (missing automaton → global tier) a kind OUTSIDE the
// delivered tier delivers nothing, while a kind IN the tier on the next
// event does: the gate is per event, per Automaton.
func TestNotifyGateBlocksUnresolvedKind(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.lifecycleBus().Close()

	rec := &alertRecorder{}
	cancel := InstallLifecycleSinks(m, SinkConfig{RecordAlert: rec.Record})
	defer cancel()

	const prdID = "prd-blocked" // no Automaton persisted: global tier only

	// rejected is not in the shipped five-kind baseline → gated out.
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDRejected), Level: LevelPRD, PRDID: prdID, NewStatus: string(PRDRejected)})
	time.Sleep(300 * time.Millisecond)
	if got := rec.count(); got != 0 {
		t.Fatalf("RecordAlert called %d times for a kind outside the resolved set, want 0", got)
	}

	// completed is in the shipped default list → the SAME gate passes it.
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: prdID, NewStatus: string(PRDCompleted)})
	waitFor(t, "delivered kind passes the same gate", func() bool { return rec.count() == 1 })
	if got := rec.count(); got != 1 {
		t.Errorf("RecordAlert called %d times, want 1 (only the resolved kind)", got)
	}
}

// TestNotifyGateTestFireNeverGated — a synthetic Test-fire event is
// never subject to the resolved set: with the GLOBAL tier turned off
// (default_events: []) and no Automaton opted in, a Test: true event
// still delivers, and the non-test twin of it is gated out.
func TestNotifyGateTestFireNeverGated(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NotifyDefaultEvents = []string{} // explicit global-tier opt-out
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.lifecycleBus().Close()

	rec := &alertRecorder{}
	cancel := InstallLifecycleSinks(m, SinkConfig{RecordAlert: rec.Record})
	defer cancel()

	const prdID = "prd-testfire" // no Automaton: global tier (off) is the resolved set

	// Non-test event of a delivered-tier kind with everything turned off
	// → gated.
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: prdID, NewStatus: string(PRDCompleted)})
	time.Sleep(300 * time.Millisecond)
	if got := rec.count(); got != 0 {
		t.Fatalf("RecordAlert called %d times with the resolved set empty, want 0", got)
	}

	// The synthetic test event is never gated → delivers.
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: prdID, NewStatus: string(PRDCompleted), Test: true})
	waitFor(t, "test-fire delivery despite empty resolved set", func() bool { return rec.count() == 1 })
	if got := rec.count(); got != 1 {
		t.Errorf("RecordAlert called %d times after the test-fire, want 1", got)
	}
}

// TestNotifyGateEngineSubscription — the session-push engine applies the
// SAME gate before delivery: a subscription that filters an event's kind
// still does not fire when the event's Automaton didn't resolve it, and
// fires when another Automaton did. The subscription filter narrows the
// resolved set; it can never widen it.
func TestNotifyGateEngineSubscription(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, _ := newEngineForTest(t, rec, sessionTable{"ops": 9013}, 0)

	// One subscription matching BOTH kinds: delivery then differs only by
	// the gate (the filter matches either event).
	sub := Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted, PRDStatusKind(PRDCompleted)}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	}
	if _, err := store.Add(sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// prd-1 (opted into both kinds via newEngineForTest) → delivered.
	m.emit(startedEvent())
	waitFor(t, "engine delivery for a resolved Automaton", func() bool { return rec.bridgeCount() == 1 })

	// prd-gated (no Automaton; task.session_started is not a global
	// default) → filtered, then gated: nothing delivered even though the
	// subscription's filter matches the kind.
	m.emit(LifecycleEvent{Kind: KindTaskSessionStarted, Level: LevelTask, PRDID: "prd-gated", TaskID: "task-1", SessionID: "worker-1"})
	time.Sleep(100 * time.Millisecond)
	if got := rec.bridgeCount(); got != 1 {
		t.Errorf("bridge deliveries = %d for a gated Automaton, want still 1 (gate must apply before delivery)", got)
	}
}

// TestNotifyGateEngineOneShotStaysArmed — a gated one-shot event is
// ignored WITHOUT consuming the one-shot: the record stays in the store,
// and the next event for a resolved Automaton delivers AND consumes it.
func TestNotifyGateEngineOneShotStaysArmed(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, _ := newEngineForTest(t, rec, sessionTable{"ops": 9013}, 0)

	sub := Subscription{
		Filter:  LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:    SubscriptionSink{Type: SinkSession, Target: "ops"},
		OneShot: true,
	}
	if _, err := store.Add(sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Gated event: no delivery, and the one-shot must NOT be consumed.
	m.emit(LifecycleEvent{Kind: KindTaskSessionStarted, Level: LevelTask, PRDID: "prd-gated", TaskID: "task-1", SessionID: "worker-1"})
	time.Sleep(100 * time.Millisecond) // let the dispatcher drain before asserting
	if got := rec.bridgeCount(); got != 0 {
		t.Errorf("bridge deliveries = %d for a gated one-shot, want 0", got)
	}
	if subs := store.List(""); len(subs) != 1 {
		t.Fatalf("one-shot was consumed by a gated event: %d records left (want 1 armed)", len(subs))
	}

	// A resolved event delivers AND consumes the now-still-armed one-shot.
	m.emit(startedEvent())
	waitFor(t, "one-shot delivery for a resolved Automaton", func() bool { return rec.bridgeCount() == 1 })
	if subs := store.List(""); len(subs) != 0 {
		t.Fatalf("one-shot not consumed after a delivered event: %d records left (want 0)", len(subs))
	}
}

// TestNotifyGateEngineCommCoalesced — comm-sink subscriptions ride the
// same gate: the comm message arrives (coalesced, after the quiet
// window) for a resolved kind, and a gated event adds no send.
func TestNotifyGateEngineCommCoalesced(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, _ := newEngineForTest(t, rec, sessionTable{"ops": 9013}, commFanoutTestWindow)

	sub := Subscription{
		Filter:   LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:     SubscriptionSink{Type: SinkComm, Target: "team"},
		Coalesce: true,
	}
	if _, err := store.Add(sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Resolved event → coalesced comm send after the quiet window.
	m.emit(startedEvent())
	waitFor(t, "comm delivery for a resolved Automaton", func() bool { return rec.commCount() == 1 })

	// Gated event (kind not in prd-gated's resolved set) → no extra send.
	m.emit(LifecycleEvent{Kind: KindTaskSessionStarted, Level: LevelTask, PRDID: "prd-gated", TaskID: "task-1", SessionID: "worker-1"})
	time.Sleep(2 * commFanoutTestWindow)
	if got := rec.commCount(); got != 1 {
		t.Errorf("comm sends = %d after a gated event, want still 1", got)
	}
}
