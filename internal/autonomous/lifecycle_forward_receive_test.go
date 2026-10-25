// BL427 (phase 5) — tests for the receiving side of peer forwarding
// (lifecycle_forward_receive.go): the envelope checks, the loop rules, the
// re-emit into the local dispatcher, and the rule that a forwarded event never
// drives the coordinator, never reaches the executor's state, and is never
// forwarded on.

package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const recvLocal = "daemon-b"

// recvManager returns a zero-value Manager wired to receive with the given
// settings and local host name. A zero-value Manager is what the lifecycle tests
// use; it builds its dispatcher on first use.
func recvManager(set ForwardSettings, local string) *Manager {
	m := new(Manager)
	m.SetForwardReceive(ForwardReceiveConfig{
		Settings:  func() ForwardSettings { return set },
		LocalHost: func() string { return local },
	})
	return m
}

func recvEnabled() ForwardSettings {
	return ForwardSettings{Enabled: true}
}

// recvPRDEvent is a local prd.completed event as the origin daemon emitted it.
func recvPRDEvent() LifecycleEvent {
	return LifecycleEvent{
		ID:        "ev-recv-1",
		At:        time.Now().UTC(),
		Kind:      PRDStatusKind(PRDCompleted),
		Level:     LevelPRD,
		PRDID:     "prd-recv-1",
		OldStatus: string(PRDRunning),
		NewStatus: string(PRDCompleted),
	}
}

// originChain is the chain of an event that left daemon-a for the first time.
func originChain() ForwardChain {
	return ForwardChain{OriginHost: "daemon-a", Hops: 1, Visited: []string{"daemon-a"}}
}

// recvEnvelope builds the envelope a peer's forwarder sends: the sender is the
// last host in the chain.
func recvEnvelope(ev LifecycleEvent, chain ForwardChain) ForwardedEnvelope {
	return ForwardedEnvelope{
		Version:        WebhookEnvelopeVersion,
		DeliveryID:     "delivery-recv-1",
		SentAt:         time.Now().UTC(),
		Host:           chain.Visited[len(chain.Visited)-1],
		SubscriptionID: "sub-peer-a",
		Event:          ev,
		Forward:        &chain,
	}
}

func TestForwardReceive_AcceptReemitsWithOriginHost(t *testing.T) {
	m := recvManager(recvEnabled(), recvLocal)
	got := make(chan LifecycleEvent, 2)
	cancel := m.Subscribe(LifecycleFilter{}, func(ev LifecycleEvent) { got <- ev })
	defer cancel()

	if !m.ForwardReceiveEnabled() {
		t.Fatal("receive reports disabled while wired and enabled")
	}
	env := recvEnvelope(recvPRDEvent(), originChain())
	if err := m.ReceiveForwardedEvent(env); err != nil {
		t.Fatalf("ReceiveForwardedEvent: %v", err)
	}

	select {
	case ev := <-got:
		if ev.Host != "daemon-a" {
			t.Errorf("Host = %q, want the origin host daemon-a", ev.Host)
		}
		if ev.Forward == nil || ev.Forward.OriginHost != "daemon-a" || ev.Forward.Hops != 1 {
			t.Errorf("Forward = %+v, want origin daemon-a with 1 hop", ev.Forward)
		}
		if ev.ID != "ev-recv-1" || ev.Kind != PRDStatusKind(PRDCompleted) || ev.PRDID != "prd-recv-1" {
			t.Errorf("event identity changed: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("local subscriber did not receive the forwarded event")
	}
	// The caller's envelope is not mutated by the injection.
	if env.Event.Host != "" || env.Event.Forward != nil {
		t.Errorf("envelope mutated: %+v", env.Event)
	}
}

func TestForwardReceive_AssignsIDAndTimeWhenAbsent(t *testing.T) {
	m := recvManager(recvEnabled(), recvLocal)
	got := make(chan LifecycleEvent, 1)
	defer m.Subscribe(LifecycleFilter{}, func(ev LifecycleEvent) { got <- ev })()

	ev := recvPRDEvent()
	ev.ID, ev.At = "", time.Time{}
	if err := m.ReceiveForwardedEvent(recvEnvelope(ev, originChain())); err != nil {
		t.Fatalf("ReceiveForwardedEvent: %v", err)
	}
	select {
	case out := <-got:
		if out.ID == "" || out.At.IsZero() {
			t.Fatalf("ID or time not filled: %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no delivery")
	}
}

func TestForwardReceive_DisabledOrUnwired(t *testing.T) {
	cases := []struct {
		name string
		m    *Manager
	}{
		{"nil manager", nil},
		{"never wired", new(Manager)},
		{"forwarding disabled", recvManager(ForwardSettings{Enabled: false}, recvLocal)},
		{"no local host name", recvManager(recvEnabled(), "  ")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := recvEnvelope(recvPRDEvent(), originChain())
			if err := c.m.ReceiveForwardedEvent(env); !errors.Is(err, ErrForwardReceiveDisabled) {
				t.Fatalf("err = %v, want ErrForwardReceiveDisabled", err)
			}
			if c.m.ForwardReceiveEnabled() {
				t.Fatal("ForwardReceiveEnabled = true, want false")
			}
		})
	}
	// Settings == nil is also unwired.
	m := new(Manager)
	m.SetForwardReceive(ForwardReceiveConfig{LocalHost: func() string { return recvLocal }})
	if err := m.ReceiveForwardedEvent(recvEnvelope(recvPRDEvent(), originChain())); !errors.Is(err, ErrForwardReceiveDisabled) {
		t.Fatalf("nil Settings: err = %v, want ErrForwardReceiveDisabled", err)
	}
	// A zero config clears the wiring.
	m = recvManager(recvEnabled(), recvLocal)
	m.SetForwardReceive(ForwardReceiveConfig{})
	if m.ForwardReceiveEnabled() {
		t.Fatal("zero config left receiving enabled")
	}
	// Nil receiver setters are no-ops.
	var nilM *Manager
	nilM.SetForwardReceive(ForwardReceiveConfig{})
}

func TestForwardReceive_LiveSettingsRead(t *testing.T) {
	enabled := atomic.Bool{}
	enabled.Store(true)
	m := new(Manager)
	m.SetForwardReceive(ForwardReceiveConfig{
		Settings:  func() ForwardSettings { return ForwardSettings{Enabled: enabled.Load()} },
		LocalHost: func() string { return recvLocal },
	})
	if !m.ForwardReceiveEnabled() {
		t.Fatal("enabled read as disabled")
	}
	enabled.Store(false)
	if m.ForwardReceiveEnabled() {
		t.Fatal("a live switch to disabled was not seen")
	}
	if err := m.ReceiveForwardedEvent(recvEnvelope(recvPRDEvent(), originChain())); !errors.Is(err, ErrForwardReceiveDisabled) {
		t.Fatalf("after switch off: err = %v, want ErrForwardReceiveDisabled", err)
	}
}

// recvMutate returns a valid envelope from daemon-a, changed by f.
func recvMutate(f func(env *ForwardedEnvelope)) ForwardedEnvelope {
	env := recvEnvelope(recvPRDEvent(), originChain())
	f(&env)
	return env
}

func TestForwardReceive_RejectsInvalidEnvelopes(t *testing.T) {
	longHost := strings.Repeat("h", maxForwardHostLen+1)
	cases := []struct {
		name string
		env  ForwardedEnvelope
	}{
		{"unsupported version", recvMutate(func(e *ForwardedEnvelope) { e.Version = 2 })},
		{"not a forwarded envelope", recvMutate(func(e *ForwardedEnvelope) { e.Forward = nil })},
		{"event already marked forwarded", recvMutate(func(e *ForwardedEnvelope) {
			e.Event.Forward = &ForwardChain{OriginHost: "daemon-c", Hops: 1, Visited: []string{"daemon-c"}}
		})},
		{"empty origin host", recvMutate(func(e *ForwardedEnvelope) {
			e.Forward = &ForwardChain{OriginHost: "", Hops: 1, Visited: []string{""}}
			e.Host = ""
		})},
		{"origin host too long", recvMutate(func(e *ForwardedEnvelope) {
			e.Forward = &ForwardChain{OriginHost: longHost, Hops: 1, Visited: []string{longHost}}
			e.Host = longHost
		})},
		{"zero hops", recvMutate(func(e *ForwardedEnvelope) {
			e.Forward = &ForwardChain{OriginHost: "daemon-a", Hops: 0, Visited: nil}
		})},
		{"hop count differs from visited list", recvMutate(func(e *ForwardedEnvelope) {
			e.Forward = &ForwardChain{OriginHost: "daemon-a", Hops: 2, Visited: []string{"daemon-a"}}
		})},
		{"empty visited entry", recvMutate(func(e *ForwardedEnvelope) {
			e.Forward = &ForwardChain{OriginHost: "daemon-a", Hops: 2, Visited: []string{"daemon-a", ""}}
			e.Host = ""
		})},
		{"visited list does not start at origin", recvMutate(func(e *ForwardedEnvelope) {
			e.Forward = &ForwardChain{OriginHost: "daemon-a", Hops: 1, Visited: []string{"daemon-c"}}
			e.Host = "daemon-c"
		})},
		{"sender is not the last visited host", recvMutate(func(e *ForwardedEnvelope) { e.Host = "daemon-x" })},
		{"unknown status in a prd kind", recvMutate(func(e *ForwardedEnvelope) { e.Event.Kind = "prd.bogus" })},
		{"unknown status in a task kind", recvMutate(func(e *ForwardedEnvelope) {
			e.Event.Kind, e.Event.Level = "task.bogus", LevelTask
		})},
		{"unknown automation status", recvMutate(func(e *ForwardedEnvelope) {
			e.Event.Kind, e.Event.Level = "automation.bogus", LevelAutomation
		})},
		{"kind without a level prefix", recvMutate(func(e *ForwardedEnvelope) { e.Event.Kind = "" })},
		{"prd kind at task level", recvMutate(func(e *ForwardedEnvelope) { e.Event.Level = LevelTask })},
		{"task session kind at prd level", recvMutate(func(e *ForwardedEnvelope) {
			e.Event.Kind, e.Event.Level = KindTaskSessionStarted, LevelPRD
		})},
		{"prd update at task level", recvMutate(func(e *ForwardedEnvelope) {
			e.Event.Kind, e.Event.Level = KindPRDUpdated, LevelTask
		})},
		{"envelope over the size cap", recvMutate(func(e *ForwardedEnvelope) {
			e.Event.PRDID = strings.Repeat("x", WebhookMaxBodyBytes)
		})},
	}
	m := recvManager(recvEnabled(), recvLocal)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := m.ReceiveForwardedEvent(c.env)
			if !errors.Is(err, ErrForwardReceiveInvalid) {
				t.Fatalf("err = %v, want ErrForwardReceiveInvalid", err)
			}
			if errors.Is(err, ErrForwardReceiveLoop) || errors.Is(err, ErrForwardReceiveDisabled) {
				t.Fatalf("err = %v also matches a different sentinel", err)
			}
		})
	}
}

func TestForwardReceive_AcceptsKnownKinds(t *testing.T) {
	m := recvManager(recvEnabled(), recvLocal)
	kinds := []struct {
		kind  string
		level LifecycleLevel
	}{
		{PRDStatusKind(PRDCompleted), LevelPRD},
		{PRDStatusKind(PRDPaused), LevelPRD},
		{KindPRDUpdated, LevelPRD},
		{TaskStatusKind(TaskVerifying), LevelTask},
		{TaskStatusKind(TaskWaitingCapacity), LevelTask},
		{KindTaskSessionStarted, LevelTask},
		{KindTaskSessionCleared, LevelTask},
		{AutomationStatusKind(AutomationCompleted), LevelAutomation},
	}
	for _, k := range kinds {
		ev := recvPRDEvent()
		ev.Kind, ev.Level = k.kind, k.level
		if err := m.ReceiveForwardedEvent(recvEnvelope(ev, originChain())); err != nil {
			t.Errorf("kind %s at level %s: %v", k.kind, k.level, err)
		}
	}
}

func TestForwardReceive_LoopRefusals(t *testing.T) {
	cases := []struct {
		name     string
		maxHops  int
		chain    ForwardChain
		sender   string
		wantLoop bool
	}{
		{"origin is this host", 0, ForwardChain{OriginHost: recvLocal, Hops: 1, Visited: []string{recvLocal}}, recvLocal, true},
		{"already in visited list", 0, ForwardChain{OriginHost: "daemon-a", Hops: 2, Visited: []string{"daemon-a", recvLocal}}, recvLocal, true},
		{"hop cap reached with the default cap", 0, ForwardChain{OriginHost: "daemon-a", Hops: DefaultForwardMaxHops, Visited: []string{"daemon-a", "daemon-c", "daemon-d"}}, "daemon-d", true},
		{"hop cap reached with a lower cap", 2, ForwardChain{OriginHost: "daemon-a", Hops: 2, Visited: []string{"daemon-a", "daemon-c"}}, "daemon-c", true},
		{"below the default cap is accepted", 0, ForwardChain{OriginHost: "daemon-a", Hops: 2, Visited: []string{"daemon-a", "daemon-c"}}, "daemon-c", false},
		{"below an explicit cap of three is accepted", 3, ForwardChain{OriginHost: "daemon-a", Hops: 2, Visited: []string{"daemon-a", "daemon-c"}}, "daemon-c", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := recvManager(ForwardSettings{Enabled: true, MaxHops: c.maxHops}, recvLocal)
			env := recvEnvelope(recvPRDEvent(), c.chain)
			env.Host = c.sender
			err := m.ReceiveForwardedEvent(env)
			if c.wantLoop {
				if !errors.Is(err, ErrForwardReceiveLoop) {
					t.Fatalf("err = %v, want ErrForwardReceiveLoop", err)
				}
				if errors.Is(err, ErrForwardReceiveInvalid) {
					t.Fatalf("err = %v also matches ErrForwardReceiveInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want accept", err)
			}
		})
	}
}

// TestForwardReceive_ForwardedEventNeverForwardedOn: a forwarded event that
// reaches a peer-sink subscription is refused by the forwarder before any
// request, so the fake peer sees nothing.
func TestForwardReceive_ForwardedEventNeverForwardedOn(t *testing.T) {
	var calls atomic.Int32
	counting := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("the fake peer must not be reached")
	})}
	fwd := NewPeerForwarder(PeerForwarderConfig{
		Settings:  func() ForwardSettings { return ForwardSettings{Enabled: true} },
		LocalHost: func() string { return recvLocal },
		LookupPeer: func(name string) (PeerRef, bool) {
			return PeerRef{Name: name, URL: "https://peer.example.net", Token: "tok", Federated: true, HasAutonomousWrite: true}, true
		},
		NewClient: func(bool, time.Duration) *http.Client { return counting },
	})
	m := recvManager(recvEnabled(), recvLocal)
	got := make(chan LifecycleEvent, 1)
	defer m.Subscribe(LifecycleFilter{}, func(ev LifecycleEvent) {
		_ = fwd.Forward(context.Background(), Subscription{ID: "sub-peer", Sink: SubscriptionSink{Type: SinkPeer, Target: "daemon-c"}}, ev)
		got <- ev
	})()

	if err := m.ReceiveForwardedEvent(recvEnvelope(recvPRDEvent(), originChain())); err != nil {
		t.Fatalf("ReceiveForwardedEvent: %v", err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the peer subscription did not see the event")
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("fake peer was reached %d time(s); a forwarded event must never be forwarded on", n)
	}
}

// TestForwardReceive_DoesNotDriveCoordinator: the Automation coordinator is
// subscribed to the same dispatcher. A forwarded terminal event for a member
// must not advance the Automation or touch the member PRD; the same event
// generated locally does (positive control).
func TestForwardReceive_DoesNotDriveCoordinator(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	h.setStatus(0, PRDCompleted)
	h.c.Start()
	defer h.c.Stop()

	h.m.SetForwardReceive(ForwardReceiveConfig{
		Settings:  func() ForwardSettings { return ForwardSettings{Enabled: true} },
		LocalHost: func() string { return recvLocal },
	})
	before := h.get(a)
	prdBefore, _ := h.m.store.GetPRD(h.prds[0].ID)

	delivered := make(chan LifecycleEvent, 1)
	defer h.m.Subscribe(LifecycleFilter{Level: LevelPRD}, func(ev LifecycleEvent) { delivered <- ev })()

	ev := recvPRDEvent()
	ev.PRDID = h.prds[0].ID
	if err := h.m.ReceiveForwardedEvent(recvEnvelope(ev, originChain())); err != nil {
		t.Fatalf("ReceiveForwardedEvent: %v", err)
	}
	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("local subscriber did not see the forwarded event")
	}
	// The coordinator runs on its own goroutine; give it time to act if it would.
	time.Sleep(200 * time.Millisecond)

	runs, resumes := h.fake.counts()
	if len(runs) != 0 || len(resumes) != 0 {
		t.Fatalf("coordinator started members from a forwarded event: runs=%v resumes=%v", runs, resumes)
	}
	after := h.get(a)
	if after.Version != before.Version || after.CurrentMemberID != before.CurrentMemberID || after.Status != before.Status {
		t.Fatalf("automation changed by a forwarded event: before v%d cursor=%q, after v%d cursor=%q",
			before.Version, before.CurrentMemberID, after.Version, after.CurrentMemberID)
	}
	prdAfter, _ := h.m.store.GetPRD(h.prds[0].ID)
	if prdAfter.Status != prdBefore.Status || len(prdAfter.Decisions) != len(prdBefore.Decisions) {
		t.Fatalf("member PRD written by a forwarded event: status %s→%s decisions %d→%d",
			prdBefore.Status, prdAfter.Status, len(prdBefore.Decisions), len(prdAfter.Decisions))
	}

	// Positive control: the same event from this host drives the coordinator.
	h.event(0, PRDCompleted)
	if runs, _ := h.fake.counts(); len(runs) != 1 {
		t.Fatalf("positive control: coordinator did not advance on a local event: runs=%v", runs)
	}
}

func TestForwardReceive_CoordinatorDirectSkipsForwarded(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	h.setStatus(0, PRDCompleted)
	before := h.get(a)

	h.c.onMemberTerminal(LifecycleEvent{
		Level: LevelPRD, Kind: PRDStatusKind(PRDCompleted), PRDID: h.prds[0].ID, NewStatus: string(PRDCompleted),
		Forward: &ForwardChain{OriginHost: "daemon-a", Hops: 1, Visited: []string{"daemon-a"}},
	})
	if runs, _ := h.fake.counts(); len(runs) != 0 {
		t.Fatalf("forwarded terminal event started a member: %v", runs)
	}
	if got := h.get(a); got.Version != before.Version || got.CurrentMemberID != before.CurrentMemberID {
		t.Fatalf("automation advanced on a forwarded event: v%d→v%d cursor %q→%q",
			before.Version, got.Version, before.CurrentMemberID, got.CurrentMemberID)
	}
	if len(h.audits) != 0 {
		t.Fatalf("forwarded terminal event audited a coordinator action: %v", h.audits)
	}
}

func TestForwardReceive_DescribeNamesOrigin(t *testing.T) {
	ev := recvPRDEvent()
	if strings.Contains(DescribeLifecycleEvent(ev), "forwarded") {
		t.Fatalf("local event described as forwarded: %q", DescribeLifecycleEvent(ev))
	}
	ev.Forward = &ForwardChain{OriginHost: "daemon-a", Hops: 1, Visited: []string{"daemon-a"}}
	if got := DescribeLifecycleEvent(ev); !strings.HasSuffix(got, "(forwarded from daemon-a)") {
		t.Fatalf("forwarded event description = %q, want the origin suffix", got)
	}
}

func TestForwardReceive_WebhookEnvelopeCarriesForwardBlock(t *testing.T) {
	sink := NewWebhookSink(WebhookSinkConfig{Hostname: func() string { return recvLocal }})
	run := &webhookRun{deliveryID: "d-fwd", start: time.Now()}

	forwarded := recvPRDEvent()
	forwarded.Forward = &ForwardChain{OriginHost: "daemon-a", Hops: 1, Visited: []string{"daemon-a"}}
	body, err := sink.buildBody(run, Subscription{ID: "sub-w"}, forwarded, WebhookMaxBodyBytes)
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}
	var env struct {
		Forward *ForwardChain `json:"forward"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatal(err)
	}
	if env.Forward == nil || env.Forward.OriginHost != "daemon-a" {
		t.Fatalf("top-level forward block missing on a forwarded event: %s", body)
	}

	body, err = sink.buildBody(run, Subscription{ID: "sub-w"}, recvPRDEvent(), WebhookMaxBodyBytes)
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}
	if strings.Contains(string(body), `"forward"`) {
		t.Fatalf("a locally originated envelope carries a forward block: %s", body)
	}
}

// TestForwardReceive_PeerSinkPositiveControlAndNoForwardOn: the engine's peer
// sink forwards a locally emitted event (positive control: the fake peer is
// reached once), and the same kind of event received from a peer reaches the
// peer sink's subscription without being sent on (the fake peer count holds).
func TestForwardReceive_PeerSinkPositiveControlAndNoForwardOn(t *testing.T) {
	var peerHits atomic.Int32
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		peerHits.Add(1)
		return &http.Response{StatusCode: http.StatusAccepted, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
	})
	fwd := NewPeerForwarder(PeerForwarderConfig{
		Settings:  func() ForwardSettings { return ForwardSettings{Enabled: true} },
		LocalHost: func() string { return recvLocal },
		LookupPeer: func(name string) (PeerRef, bool) {
			return PeerRef{Name: name, URL: "https://peer.example.net", Token: "tok-peer", Federated: true, HasAutonomousWrite: true}, true
		},
		NewClient: func(bool, time.Duration) *http.Client { return &http.Client{Transport: rt} },
	})
	store, err := NewSubscriptionStore(filepath.Join(t.TempDir(), "lifecycle_subscriptions.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{PRDStatusKind(PRDCompleted)}},
		Sink:   SubscriptionSink{Type: SinkPeer, Target: "daemon-c"},
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}
	m := recvManager(recvEnabled(), recvLocal)
	eng := NewLifecycleSubscriptionEngine(m, store, SessionPushConfig{Forwarder: fwd})
	if err := eng.Start(); err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	defer eng.Close()

	// Positive control: a local event leaves through the peer sink.
	m.emit(recvPRDEvent())
	deadline := time.Now().Add(2 * time.Second)
	for peerHits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := peerHits.Load(); n != 1 {
		t.Fatalf("positive control: fake peer reached %d time(s), want 1", n)
	}

	// The forwarded event reaches the same subscription and goes nowhere.
	ev := recvPRDEvent()
	ev.ID = "ev-recv-peer-sink"
	if err := m.ReceiveForwardedEvent(recvEnvelope(ev, originChain())); err != nil {
		t.Fatalf("ReceiveForwardedEvent: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := peerHits.Load(); n != 1 {
		t.Fatalf("fake peer reached %d time(s) after a forwarded event, want still 1", n)
	}
}
