// Tests for the two opt-in lifecycle event kinds (scope_warning and
// guardrail_warn) at the NOTIFICATION side of the wire: the resolver's
// default set stays the five shipped kinds, the two warning kinds are
// delivered through a subscription only when the event's Automaton (or
// its profile, or the global default) opted in — a subscription can
// narrow but never widen that resolved set — and the test-fire + sink
// description surface handles both kinds.

package autonomous

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestNotifyDefaultSetUnchanged — the two new kinds are opt-in: the
// shipped default notify set is still exactly the five kinds it was,
// neither warning kind appears in it, and both are listed as valid
// opt-in choices in the same order the kind constants name them.
func TestNotifyDefaultSetUnchanged(t *testing.T) {
	defaults := DefaultNotifyEvents()
	wantDefault := []string{
		PRDStatusKind(PRDNeedsReview),
		PRDStatusKind(PRDBlocked),
		PRDStatusKind(PRDFailed),
		PRDStatusKind(PRDCompleted),
		PRDStatusKind(PRDCancelled),
	}
	if !reflect.DeepEqual(defaults, wantDefault) {
		t.Fatalf("default notify set = %v, want the unchanged %v", defaults, wantDefault)
	}
	if slices.Contains(defaults, KindScopeWarning) || slices.Contains(defaults, KindGuardrailWarn) {
		t.Fatalf("default set must remain opt-out for the warning kinds: %v", defaults)
	}

	optIn := OptInNotifyEvents()
	for _, k := range []string{KindScopeWarning, KindGuardrailWarn} {
		if !slices.Contains(optIn, k) {
			t.Errorf("OptInNotifyEvents() = %v, missing the opt-in kind %q", optIn, k)
		}
	}
	valid := ValidNotifyEventKinds()
	if !slices.Contains(valid, KindScopeWarning) || !slices.Contains(valid, KindGuardrailWarn) {
		t.Errorf("valid notify kinds missing the warning kinds: %v", valid)
	}
}

// optInEngine wires a manager + subscription store + running engine with
// the session-push recorder and a pending queue, so delivery assertions
// can watch both the session sink and the MCP queue sink.
func optInEngine(t *testing.T) (*Manager, *SubscriptionStore, *LifecycleSubscriptionEngine, *sessionPushRecorder, *PendingEventQueue) {
	t.Helper()
	rec := &sessionPushRecorder{}
	m, store, e := newEngineForTest(t, rec, sessionTable{"ops": 9013}, 0)
	queue := NewPendingEventQueue(50)
	e.cfg.PendingQueue = queue
	return m, store, e, rec, queue
}

// scopeWarnEvent and guardWarnEvent are the two warning events as their
// producers emit them (lifecycle_emit_warning.go): task-level scope
// warning for the named Automaton, prd-level guardrail warn named via
// StoryID.
func scopeWarnEvent(prdID string) LifecycleEvent {
	return LifecycleEvent{
		Kind:    KindScopeWarning,
		Level:   LevelTask,
		PRDID:   prdID,
		StoryID: "story-1",
		TaskID:  "task-1",
		Files:   []string{"b.go"},
		// Deep link to the alert raised alongside the warning (the
		// shape emitScopeWarning produces from the alert id).
		Link: "?alert=alert-42",
	}
}

func guardWarnEvent(prdID string) LifecycleEvent {
	v := GuardrailVerdict{Guardrail: "rules", Outcome: "warn", Summary: "one finding"}
	ev := LifecycleEvent{Kind: KindGuardrailWarn, Level: LevelPRD, PRDID: prdID, StoryID: "story-1", Guardrail: "rules"}
	ev.Verdict = &v
	return ev
}

// TestNotifyOptIn_SubscriptionDelivery — a subscription that filters a
// warning kind still only delivers when the event's Automaton resolves
// that kind (here the Automaton tier opted it in): session sink and
// MCP queue sink both receive exactly the events for the opted-in
// Automaton, and exactly none for an Automaton whose resolved set
// excludes the kind.
func TestNotifyOptIn_SubscriptionDelivery(t *testing.T) {
	m, store, _, rec, queue := optInEngine(t)
	saveAutomaton(t, m, "prd-1", []string{KindScopeWarning, KindGuardrailWarn}) // opted in
	// prd-gated: nothing persisted — its resolved set is the global
	// default, which excludes both warning kinds.

	sub, err := store.Add(Subscription{
		Owner:  "tester",
		Filter: LifecycleFilter{Kinds: []string{KindScopeWarning, KindGuardrailWarn}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	subQ, err := store.Add(Subscription{
		Owner:  "tester",
		Filter: LifecycleFilter{Kinds: []string{KindScopeWarning, KindGuardrailWarn}},
		Sink:   SubscriptionSink{Type: SinkMCPQueue, Target: "team"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	_ = sub
	_ = subQ

	// Delivered: the opted-in Automaton's warning kind reaches both sinks.
	m.emit(scopeWarnEvent("prd-1"))
	waitFor(t, "subscription delivery for the opted-in kind", func() bool { return rec.bridgeCount() == 1 })
	waitFor(t, "queue delivery for the opted-in kind", func() bool {
		evs, _, _ := queue.Pending("team", 0, 10)
		return len(evs) == 1
	})
	if !strings.Contains(rec.bridge[0], "scope warning") || !strings.Contains(rec.bridge[0], "b.go") {
		t.Errorf("session delivery = %q, want the scope_warning description", rec.bridge)
	}
	qEvs, _, _ := queue.Pending("team", 0, 10)
	if len(qEvs) != 1 || qEvs[0].Kind != KindScopeWarning || qEvs[0].PRDID != "prd-1" {
		t.Errorf("queue events = %+v, want the one opt-in scope_warning", qEvs)
	}
	// The queued event carries the full payload: the finding files and
	// the deep link to the alert raised alongside the warning.
	if !reflect.DeepEqual(qEvs[0].Files, []string{"b.go"}) {
		t.Errorf("queue[0].Files = %v, want [b.go]", qEvs[0].Files)
	}
	if qEvs[0].Link != "?alert=alert-42" {
		t.Errorf("queue[0].Link = %q, want the alert deep link carried verbatim", qEvs[0].Link)
	}

	// Gated: the same subscription filters these kinds, but prd-gated's
	// resolved set (the global default) does not include them.
	m.emit(scopeWarnEvent("prd-gated"))
	m.emit(guardWarnEvent("prd-gated"))
	time.Sleep(100 * time.Millisecond) // let the gated events drain
	if got := rec.bridgeCount(); got != 1 {
		t.Errorf("bridge deliveries = %d, want still 1 (gated kinds must not widen the set)", got)
	}
	if evs, _, _ := queue.Pending("team", 0, 10); len(evs) != 1 {
		t.Errorf("queue events = %d, want still 1", len(evs))
	}
}

// TestNotifyOptIn_AutomatonLevel — per-Automaton resolution for the two
// warning kinds: the global tier can include them for every Automaton,
// but a non-empty Automaton list REPLACES the global set (override), and
// a nil list falls back to it (clear). Each assertion runs through the
// real gate that a live notification sinks off.
func TestNotifyOptIn_AutomatonLevel(t *testing.T) {
	// Global tier: the daemon's config default_events includes both warning
	// kinds. Any Automaton with NOTHING of its own delivers both.
	cfg := DefaultConfig()
	defaults := append([]string(nil), DefaultNotifyEvents()...)
	cfg.NotifyDefaultEvents = append(defaults, KindScopeWarning, KindGuardrailWarn)
	m := newEmitManager(t, cfg, nil)
	t.Cleanup(func() { m.lifecycleBus().Close() })

	// An Automaton with NO list of its own: its resolved tier is the global
	// one, which now includes both warning kinds.
	fresh := newEmitPRD(t, m, PRDActive)
	if !m.NotifyEventAllowed(scopeWarnEvent(fresh.ID)) {
		t.Fatal("global tier includes scope_warning, so an unset Automaton must deliver it")
	}
	if !m.NotifyEventAllowed(guardWarnEvent(fresh.ID)) {
		t.Fatal("global tier includes guardrail_warn, so an unset Automaton must deliver it")
	}

	// Override: a non-empty Automaton list REPLACES the global set — the
	// Automaton keeps what it listed (scope_warning) and loses what it did
	// not (guardrail_warn), even though the global tier has both.
	if _, err := m.SetPRDNotifyEvents(fresh.ID, []string{KindScopeWarning}); err != nil {
		t.Fatalf("SetPRDNotifyEvents: %v", err)
	}
	if set, ok := m.ResolveNotifyEventsFor(fresh.ID); !ok || set.Source != NotifySourceAutomaton ||
		!reflect.DeepEqual(set.Events, []string{KindScopeWarning}) {
		t.Fatalf("override: resolved = %+v ok=%v, want the Automaton tier's list", set, ok)
	}
	if !m.NotifyEventAllowed(scopeWarnEvent(fresh.ID)) {
		t.Fatal("override keeps the listed scope_warning")
	}
	if m.NotifyEventAllowed(guardWarnEvent(fresh.ID)) {
		t.Fatal("override must drop the unlisted guardrail_warn")
	}

	// Clear: an empty Automaton list falls back to the (now extended)
	// global tier, which has BOTH warning kinds again.
	if _, err := m.SetPRDNotifyEvents(fresh.ID, nil); err != nil {
		t.Fatalf("SetPRDNotifyEvents(nil): %v", err)
	}
	if set, _ := m.ResolveNotifyEventsFor(fresh.ID); set.Source != NotifySourceGlobal {
		t.Fatalf("cleared list must fall back to the global tier, got %+v", set)
	}
	if !m.NotifyEventAllowed(scopeWarnEvent(fresh.ID)) || !m.NotifyEventAllowed(guardWarnEvent(fresh.ID)) {
		t.Fatal("cleared list: the global tier's warning kinds must be delivered again")
	}
}

// TestNotifyOptIn_SubscriptionCannotWiden — the subscription filters a
// warning kind, but the event's Automaton's resolved set decides: a
// subscription can never make an unlisted kind deliver.
func TestNotifyOptIn_SubscriptionCannotWiden(t *testing.T) {
	m, store, _, rec, queue := optInEngine(t)
	// prd-narrow has a non-empty list (task.session_started) — non-empty
	// means it REPLACES the global set, so neither warning kind is in its
	// resolved set. The subscription then filters a warning kind too: it
	// can match (narrow) the resolved set, never widen it.
	saveAutomaton(t, m, "prd-narrow", []string{KindTaskSessionStarted})

	subKinds := []string{KindScopeWarning, KindGuardrailWarn, KindTaskSessionStarted}
	subs := []SubscriptionSink{{Type: SinkSession, Target: "ops"}, {Type: SinkMCPQueue, Target: "queue"}}
	for i, sink := range subs {
		if _, err := store.Add(Subscription{
			Filter: LifecycleFilter{Kinds: subKinds},
			Sink:   sink,
		}); err != nil {
			t.Fatalf("Add(%d): %v", i, err)
		}
	}

	m.emit(scopeWarnEvent("prd-narrow"))
	m.emit(guardWarnEvent("prd-narrow"))
	time.Sleep(100 * time.Millisecond) // let both gated events drain
	if got := rec.bridgeCount(); got != 0 {
		t.Errorf("bridge deliveries = %d for unlisted kinds, want 0", got)
	}
	if evs, _, _ := queue.Pending("queue", 0, 10); len(evs) != 0 {
		t.Errorf("queue events = %d for unlisted kinds, want 0", len(evs))
	}
	// And the Automaton's OWN listed kind for the same Automaton still
	// delivers: the subscription narrows the kinds it wants, the resolver
	// decides which of those the Automaton opted into — both work.
	m.emit(startedEventNarrowed("prd-narrow"))
	waitFor(t, "delivery of the Automaton's listed kind", func() bool {
		evs, _, _ := queue.Pending("queue", 0, 10)
		return rec.bridgeCount() == 1 && len(evs) == 1
	})
	if evs, _, _ := queue.Pending("queue", 0, 10); len(evs) != 1 || evs[0].Kind != KindTaskSessionStarted {
		t.Errorf("queue events = %+v, want the one task.session_started for the same Automaton", evs)
	}
}

// startedEventNarrowed is startedEvent() pointed at the named Automaton.
func startedEventNarrowed(prdID string) LifecycleEvent {
	ev := startedEvent()
	ev.PRDID = prdID
	return ev
}

// TestNotifyOptIn_TestFireAndDescribe — the operator surface: a
// subscription test-fire through a session sink reports the warning
// sink name and delivers the labelled synthetic event, and the sink
// description + level both render the warning kinds (DescribeLifecycle
// Event is what the PWA and REST surfaces show; sinkLevel is what an
// alert sink records).
func TestNotifyOptIn_TestFireAndDescribe(t *testing.T) {
	_, store, e, rec, _ := optInEngine(t)
	sub, err := store.Add(Subscription{
		Owner:  "tester",
		Filter: LifecycleFilter{Kinds: []string{KindScopeWarning, KindGuardrailWarn}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	res, err := e.FireTest(sub.ID)
	if err != nil || !res.OK || res.Sink != SinkSession {
		t.Fatalf("FireTest: res=%+v err=%v", res, err)
	}
	if got := rec.bridgeCount(); got != 1 || !strings.Contains(rec.bridge[0], "[test] ") {
		t.Fatalf("test-fire delivery: count=%d bridge=%v, want one [test]-labelled message", got, rec.bridge)
	}

	// DescribeLifecycleEvent renders both warning kinds with the Automaton,
	// the task (scope) and the guardrail detail (warn), the way an operator
	// expects to read them.
	scope := scopeWarnEvent("prd-fire")
	scope.Decision = &Decision{Kind: "undeclared_edit_detected", Note: "b.go (3 lines)"}
	got := DescribeLifecycleEvent(scope)
	for _, want := range []string{"scope warning", "Automaton prd-fire", "(task task-1)", "b.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("DescribeLifecycleEvent(scope_warning) = %q, want %q", got, want)
		}
	}
	if sinkLevel(scope) != "warn" {
		t.Errorf("sinkLevel(scope_warning) = %q, want %q", sinkLevel(scope), "warn")
	}

	guard := guardWarnEvent("prd-fire")
	got = DescribeLifecycleEvent(guard)
	for _, want := range []string{"rules", "warn", "one finding", "Automaton prd-fire"} {
		if !strings.Contains(got, want) {
			t.Errorf("DescribeLifecycleEvent(guardrail_warn) = %q, want %q", got, want)
		}
	}
	if sinkLevel(guard) != "warn" {
		t.Errorf("sinkLevel(guardrail_warn) = %q, want %q", sinkLevel(guard), "warn")
	}
}

// TestNotifyAudit_NewKindsVerbatim — the audit event written when an
// Automaton's (or a guardrail profile's) notify_events list changes keeps
// the newly opted-in warning kinds verbatim, in order, in its "new"
// detail. The audit helper is the shared fakeAuditLog from
// notify_events_audit_test.go and the audit sites are the existing
// SetPRDNotifyEvents / UpdateGuardrailProfile calls — this table only
// checks the two new kinds flow through unchanged.
func TestNotifyAudit_NewKindsVerbatim(t *testing.T) {
	cases := []struct {
		name    string
		scope   string // "automaton" | "profile"
		list    []string
		wantNew string
	}{
		{"scope_warning_automaton", "automaton", []string{KindScopeWarning}, KindScopeWarning},
		{"guardrail_warn_automaton", "automaton", []string{KindGuardrailWarn}, KindGuardrailWarn},
		{"both_in_order_automaton", "automaton", []string{KindScopeWarning, KindGuardrailWarn, "prd.blocked"},
			KindScopeWarning + "," + KindGuardrailWarn + ",prd.blocked"},
		{"both_reversed_profile", "profile", []string{KindGuardrailWarn, KindScopeWarning},
			KindGuardrailWarn + "," + KindScopeWarning},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _, _, _ := optInEngine(t)
			fake := &fakeAuditLog{}
			m.SetAuditFn(fake.fn)

			switch c.scope {
			case "automaton":
				prd, err := m.CreatePRD("audit-"+c.name, t.TempDir(), "opencode", "", EffortNormal)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := m.SetPRDNotifyEvents(prd.ID, c.list); err != nil {
					t.Fatal(err)
				}
				if fake.count("autonomous_notify_events_changed") != 1 {
					t.Fatalf("audit actions = %v, want exactly one autonomous_notify_events_changed", fake.actions)
				}
				d := fake.details[0]
				if d["scope"] != "automaton" || d["prd_id"] != prd.ID {
					t.Errorf("scope/prd_id = %v/%v, want automaton/%s", d["scope"], d["prd_id"], prd.ID)
				}
			case "profile":
				p, err := m.CreateGuardrailProfile("audit-"+c.name, "", nil, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := m.UpdateGuardrailProfile(p.ID, p.Name, "", nil, nil, c.list); err != nil {
					t.Fatal(err)
				}
				if fake.count("autonomous_notify_events_changed") != 1 {
					t.Fatalf("audit actions = %v, want exactly one autonomous_notify_events_changed", fake.actions)
				}
				d := fake.details[0]
				if d["scope"] != "profile" || d["profile_id"] != p.ID {
					t.Errorf("scope/profile_id = %v/%v, want profile/%s", d["scope"], d["profile_id"], p.ID)
				}
			}

			// The opt-in kinds appear verbatim, in order, in the audit "new".
			if got := fake.details[0]["new"]; got != c.wantNew {
				t.Errorf("audit \"new\" = %v, want %q verbatim and in order", got, c.wantNew)
			}
		})
	}
}
