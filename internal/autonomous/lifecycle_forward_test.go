// BL427 (phase 5) — tests for federation peer forwarding: delivery and the
// signed envelope, the retry sequence, the offline peer, refusals, the loop
// rules (including a two-daemon ping-pong), token redaction, the disabled
// state, subscription validation and engine routing with one-shot semantics.
// Peers are httptest servers; the retry sleep is recorded or injected, so
// every test runs at full speed.

package autonomous

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	fwdLocal    = "daemon-a"
	fwdPeerName = "peer-b"
	fwdToken    = "tok-forward-SECRET-123"
	fwdKind     = "prd.completed"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func fwdEvent() LifecycleEvent {
	return LifecycleEvent{
		ID:        "ev-1",
		At:        time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC),
		Kind:      fwdKind,
		Level:     LevelPRD,
		PRDID:     "prd-1",
		OldStatus: "running",
		NewStatus: "completed",
		Host:      fwdLocal,
	}
}

func fwdSub(target string) Subscription {
	return Subscription{
		ID:    "sub-1",
		Owner: "alice",
		Filter: LifecycleFilter{
			Kinds: []string{PRDStatusKind(PRDCompleted)},
		},
		Sink: SubscriptionSink{Type: SinkPeer, Target: target},
	}
}

// fwdPeerRef is a federated peer with the autonomous:write grant.
func fwdPeerRef(url string) PeerRef {
	return PeerRef{
		Name:               fwdPeerName,
		URL:                url,
		Token:              fwdToken,
		Federated:          true,
		HasAutonomousWrite: true,
	}
}

// ---------------------------------------------------------------------------
// Fake peer receiver
// ---------------------------------------------------------------------------

type fwdRecord struct {
	path   string
	auth   string
	header http.Header
	body   []byte
}

// fwdPeer is a fake receiving daemon. Each request consumes the next status in
// statuses; once they run out the last one repeats. No statuses means 200.
type fwdPeer struct {
	srv      *httptest.Server
	mu       sync.Mutex
	reqs     []fwdRecord
	statuses []int
	next     int
}

func newFwdPeer(t *testing.T, statuses ...int) *fwdPeer {
	t.Helper()
	p := &fwdPeer{statuses: statuses}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.reqs = append(p.reqs, fwdRecord{
			path:   r.URL.Path,
			auth:   r.Header.Get("Authorization"),
			header: r.Header.Clone(),
			body:   body,
		})
		code := http.StatusOK
		if len(p.statuses) > 0 {
			i := p.next
			if i >= len(p.statuses) {
				i = len(p.statuses) - 1
			}
			code = p.statuses[i]
			p.next++
		}
		p.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fwdPeer) requests() []fwdRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]fwdRecord(nil), p.reqs...)
}

// setStatuses replaces the scripted answers and restarts them from the first.
func (p *fwdPeer) setStatuses(codes ...int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.statuses = codes
	p.next = 0
}

// ---------------------------------------------------------------------------
// Forwarder harness
// ---------------------------------------------------------------------------

type fwdAuditRec struct {
	action  string
	details map[string]any
}

type fwdAlertRec struct {
	level, title, body string
}

// fwdRig is a PeerForwarder whose every seam is recorded or injected.
type fwdRig struct {
	mu            sync.Mutex
	audits        []fwdAuditRec
	alerts        []fwdAlertRec
	sleeps        []time.Duration
	skips         []bool
	settingsCalls int
	peers         map[string]PeerRef
	local         string
	settings      ForwardSettings
	sleepHook     func(time.Duration)
	f             *PeerForwarder
}

func newFwdRig(t *testing.T, mutate func(*ForwardSettings)) *fwdRig {
	t.Helper()
	r := &fwdRig{peers: map[string]PeerRef{}, local: fwdLocal}
	r.settings = DefaultForwardSettings()
	r.settings.Enabled = true
	if mutate != nil {
		mutate(&r.settings)
	}
	r.f = NewPeerForwarder(PeerForwarderConfig{
		LookupPeer: func(name string) (PeerRef, bool) {
			r.mu.Lock()
			defer r.mu.Unlock()
			p, ok := r.peers[name]
			return p, ok
		},
		LocalHost: func() string {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.local
		},
		Settings: func() ForwardSettings {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.settingsCalls++
			return r.settings
		},
		NewClient: func(skip bool, timeout time.Duration) *http.Client {
			r.mu.Lock()
			r.skips = append(r.skips, skip)
			r.mu.Unlock()
			return &http.Client{Timeout: timeout}
		},
		Audit: func(action string, details map[string]any) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.audits = append(r.audits, fwdAuditRec{action: action, details: details})
		},
		RaiseAlert: func(level, title, body string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.alerts = append(r.alerts, fwdAlertRec{level: level, title: title, body: body})
		},
		Sleep: func(d time.Duration) {
			r.mu.Lock()
			r.sleeps = append(r.sleeps, d)
			r.mu.Unlock()
			if r.sleepHook != nil {
				r.sleepHook(d)
			}
		},
		Jitter: func() float64 { return 0.5 }, // no spread: delays are exact
	})
	return r
}

func (r *fwdRig) addPeer(p PeerRef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers[p.Name] = p
}

func (r *fwdRig) setEnabled(on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settings.Enabled = on
}

func (r *fwdRig) auditRecs() []fwdAuditRec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]fwdAuditRec(nil), r.audits...)
}

func (r *fwdRig) alertRecs() []fwdAlertRec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]fwdAlertRec(nil), r.alerts...)
}

func (r *fwdRig) alertCount() int { return len(r.alertRecs()) }

func (r *fwdRig) sleepLog() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.sleeps...)
}

func (r *fwdRig) settingsCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.settingsCalls
}

func fwdOutcomes(recs []fwdAuditRec) []string {
	var out []string
	for _, rec := range recs {
		out = append(out, fmt.Sprint(rec.details["outcome"]))
	}
	return out
}

func fwdDurationsEqual(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fwdEnvelopeEventIDs decodes each request body and returns its event ids.
func fwdEnvelopeEventIDs(t *testing.T, reqs []fwdRecord) []string {
	t.Helper()
	var ids []string
	for _, q := range reqs {
		var env WebhookEnvelope
		if err := json.Unmarshal(q.body, &env); err != nil {
			t.Fatalf("body is not an envelope: %v", err)
		}
		ids = append(ids, env.Event.ID)
	}
	return ids
}

// ---------------------------------------------------------------------------
// Delivery and envelope
// ---------------------------------------------------------------------------

func TestForwardDeliversOnceWithBearerAndEnvelope(t *testing.T) {
	peer := newFwdPeer(t)
	r := newFwdRig(t, nil)
	r.addPeer(fwdPeerRef(peer.srv.URL))

	if err := r.f.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent()); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	reqs := peer.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	q := reqs[0]
	if q.path != ForwardedEventsPath {
		t.Errorf("path = %q, want %q", q.path, ForwardedEventsPath)
	}
	if q.auth != "Bearer "+fwdToken {
		t.Errorf("Authorization = %q, want the peer bearer token", q.auth)
	}

	var env WebhookEnvelope
	if err := json.Unmarshal(q.body, &env); err != nil {
		t.Fatalf("body is not an envelope: %v", err)
	}
	if env.Version != WebhookEnvelopeVersion || env.Host != fwdLocal || env.SubscriptionID != "sub-1" {
		t.Errorf("envelope = %+v", env)
	}
	if env.Event.ID != "ev-1" || env.Event.Kind != fwdKind {
		t.Errorf("event = %+v", env.Event)
	}
	if env.Forward == nil || env.Forward.OriginHost != fwdLocal || env.Forward.Hops != 1 ||
		len(env.Forward.Visited) != 1 || env.Forward.Visited[0] != fwdLocal {
		t.Errorf("forward block = %+v, want origin, one hop and this host visited", env.Forward)
	}

	id := q.header.Get(WebhookDeliveryHeader)
	if id != env.DeliveryID || len(id) != 32 {
		t.Errorf("delivery header %q does not match envelope delivery_id %q", id, env.DeliveryID)
	}
	if got := q.header.Get(WebhookEventHeader); got != fwdKind {
		t.Errorf("event header = %q, want %q", got, fwdKind)
	}
	if err := VerifyWebhookSignature([]byte(fwdToken), q.header.Get(WebhookSignatureHeader), q.body, time.Now(), 0); err != nil {
		t.Errorf("signature does not verify under the peer token: %v", err)
	}
	chain, err := federation.DecodeChain(q.header.Get(federation.HopChainHeader))
	if err != nil || len(chain) != 1 || !federation.VerifyLastHop(chain, fwdToken) {
		t.Fatalf("hop chain = %+v err=%v, want one entry verifying under the peer token", chain, err)
	}
	if got := federation.OriginActor(chain); got != "alice" {
		t.Errorf("origin actor = %q, want the subscription owner", got)
	}

	audits := r.auditRecs()
	if len(audits) != 1 || audits[0].action != audit.ActionLifecycleForwardDelivery {
		t.Fatalf("audit = %+v, want one forward delivery record", audits)
	}
	if audits[0].details["outcome"] != WebhookOutcomeDelivered || audits[0].details["attempt"] != 1 {
		t.Errorf("record details = %+v", audits[0].details)
	}
	if r.alertCount() != 0 {
		t.Errorf("a delivered forward raised %d alert(s)", r.alertCount())
	}
}

func TestForwardRetriesServerErrorsThenDelivers(t *testing.T) {
	peer := newFwdPeer(t, 500, 500, 200)
	r := newFwdRig(t, nil)
	r.addPeer(fwdPeerRef(peer.srv.URL))

	if err := r.f.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent()); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	reqs := peer.requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3 (two 500s then a 200)", len(reqs))
	}
	for i := 1; i < len(reqs); i++ {
		if !bytes.Equal(reqs[i].body, reqs[0].body) {
			t.Errorf("attempt %d sent different bytes", i+1)
		}
		if reqs[i].header.Get(WebhookDeliveryHeader) != reqs[0].header.Get(WebhookDeliveryHeader) {
			t.Errorf("attempt %d carries a different delivery id", i+1)
		}
	}
	want := []time.Duration{time.Second, 4 * time.Second}
	if got := r.sleepLog(); !fwdDurationsEqual(got, want) {
		t.Errorf("sleeps = %v, want %v", got, want)
	}
	if got := fwdOutcomes(r.auditRecs()); fmt.Sprint(got) != "[retry retry delivered]" {
		t.Errorf("outcomes = %v, want [retry retry delivered]", got)
	}
	if r.alertCount() != 0 {
		t.Errorf("a delivery that succeeded on retry raised an alert")
	}
}

func TestForwardPermanentStatusesAreNotRetried(t *testing.T) {
	cases := []struct {
		name     string
		code     int
		outcome  string
		sentinel error
	}{
		{"redirect is never followed", http.StatusFound, WebhookOutcomeFailed, ErrForwardDelivery},
		{"bad request is permanent", http.StatusBadRequest, WebhookOutcomeFailed, ErrForwardDelivery},
		{"loop refusal from the peer", http.StatusConflict, WebhookOutcomeRefused, ErrForwardRefused},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			peer := newFwdPeer(t, c.code)
			r := newFwdRig(t, nil)
			r.addPeer(fwdPeerRef(peer.srv.URL))

			err := r.f.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent())
			if !errors.Is(err, c.sentinel) {
				t.Fatalf("err = %v, want %v", err, c.sentinel)
			}
			if n := len(peer.requests()); n != 1 {
				t.Errorf("requests = %d, want 1 (permanent status is not retried)", n)
			}
			if got := r.sleepLog(); len(got) != 0 {
				t.Errorf("sleeps = %v, want none", got)
			}
			audits := r.auditRecs()
			if len(audits) != 1 || audits[0].details["outcome"] != c.outcome {
				t.Errorf("audit = %+v, want one %q record", audits, c.outcome)
			}
			if r.alertCount() != 1 {
				t.Errorf("alerts = %d, want 1 for the dropped forward", r.alertCount())
			}
		})
	}
}

func TestForwardOfflinePeerIsBoundedAndAlertsOnce(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	url := gone.URL
	gone.Close()

	r := newFwdRig(t, func(s *ForwardSettings) { s.MaxAttempts = 3 })
	r.addPeer(fwdPeerRef(url))

	err := r.f.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent())
	if !errors.Is(err, ErrForwardDelivery) {
		t.Fatalf("err = %v, want ErrForwardDelivery", err)
	}
	audits := r.auditRecs()
	if len(audits) != 3 {
		t.Fatalf("audit records = %d, want one per attempt (3)", len(audits))
	}
	if got := fwdOutcomes(audits); fmt.Sprint(got) != "[retry retry failed]" {
		t.Errorf("outcomes = %v, want [retry retry failed]", got)
	}
	if last := audits[len(audits)-1].details["attempt"]; last != 3 {
		t.Errorf("final record attempt = %v, want 3", last)
	}
	if n := len(r.sleepLog()); n != 2 {
		t.Errorf("sleeps = %d, want 2 between three attempts", n)
	}
	alerts := r.alertRecs()
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want exactly one", len(alerts))
	}
	if alerts[0].title != "Lifecycle peer forward failed" || alerts[0].level != webhookAlertLevel {
		t.Errorf("alert = %+v", alerts[0])
	}
}

func TestForwardContextCancelledBeforeSendMakesNoRequest(t *testing.T) {
	peer := newFwdPeer(t)
	r := newFwdRig(t, nil)
	r.addPeer(fwdPeerRef(peer.srv.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.f.Forward(ctx, fwdSub(fwdPeerName), fwdEvent())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want a wrapped context.Canceled", err)
	}
	if n := len(peer.requests()); n != 0 {
		t.Errorf("requests = %d, want 0", n)
	}
	if got := fwdOutcomes(r.auditRecs()); fmt.Sprint(got) != "[skipped]" {
		t.Errorf("outcomes = %v, want [skipped]", got)
	}
	if r.alertCount() != 0 {
		t.Errorf("an abandoned forward raised an alert")
	}
}

// ---------------------------------------------------------------------------
// Refusals
// ---------------------------------------------------------------------------

func TestForwardUnknownPeerRefused(t *testing.T) {
	r := newFwdRig(t, nil)
	err := r.f.Forward(context.Background(), fwdSub("ghost"), fwdEvent())
	if !errors.Is(err, ErrForwardRefused) {
		t.Fatalf("err = %v, want ErrForwardRefused", err)
	}
	audits := r.auditRecs()
	if len(audits) != 1 || audits[0].details["attempt"] != 0 || audits[0].details["outcome"] != WebhookOutcomeRefused {
		t.Errorf("audit = %+v, want one refused record with attempt 0", audits)
	}
	if r.alertCount() != 1 {
		t.Errorf("alerts = %d, want 1", r.alertCount())
	}
}

func TestForwardPeerRefusedWhenNotGranted(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PeerRef)
		want   string
	}{
		{"not a federation peer", func(p *PeerRef) { p.Federated = false }, "not a federation peer"},
		{"lacks autonomous:write", func(p *PeerRef) { p.HasAutonomousWrite = false }, "autonomous:write"},
		{"no bearer token", func(p *PeerRef) { p.Token = "" }, "no bearer token"},
		{"no usable URL", func(p *PeerRef) { p.URL = "ftp://peer.example.net" }, "absolute http or https"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			peer := newFwdPeer(t)
			r := newFwdRig(t, nil)
			ref := fwdPeerRef(peer.srv.URL)
			c.mutate(&ref)
			r.addPeer(ref)

			err := r.f.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent())
			if !errors.Is(err, ErrForwardRefused) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want a refusal mentioning %q", err, c.want)
			}
			if n := len(peer.requests()); n != 0 {
				t.Errorf("requests = %d, want 0", n)
			}
		})
	}
}

func TestForwardLoopRulesRefuseWithoutRequest(t *testing.T) {
	cases := []struct {
		name  string
		chain ForwardChain
		want  string
	}{
		{
			name:  "already forwarded from another origin",
			chain: ForwardChain{OriginHost: "daemon-c", Hops: 1, Visited: []string{"daemon-c"}},
			want:  "already forwarded",
		},
		{
			name:  "local host already in the visited list",
			chain: ForwardChain{OriginHost: "daemon-c", Hops: 2, Visited: []string{"daemon-c", fwdLocal}},
			want:  "visited list",
		},
		{
			name:  "event originated on this host",
			chain: ForwardChain{OriginHost: fwdLocal, Hops: 1, Visited: []string{fwdLocal}},
			want:  "originated on this host",
		},
		{
			name:  "hop cap reached",
			chain: ForwardChain{OriginHost: "daemon-c", Hops: DefaultForwardMaxHops, Visited: []string{"daemon-c", "daemon-d", "daemon-e"}},
			want:  "hop cap",
		},
		{
			name:  "received event under the cap is still never forwarded on",
			chain: ForwardChain{OriginHost: "daemon-c", Hops: 2, Visited: []string{"daemon-c", "daemon-d"}},
			want:  "never forwarded on",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			peer := newFwdPeer(t)
			r := newFwdRig(t, nil)
			r.addPeer(fwdPeerRef(peer.srv.URL))
			ev := fwdEvent()
			chain := c.chain
			ev.Forward = &chain

			err := r.f.Forward(context.Background(), fwdSub(fwdPeerName), ev)
			if !errors.Is(err, ErrForwardRefused) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want a loop refusal mentioning %q", err, c.want)
			}
			if n := len(peer.requests()); n != 0 {
				t.Errorf("requests = %d, want 0", n)
			}
			audits := r.auditRecs()
			if len(audits) != 1 || audits[0].details["outcome"] != WebhookOutcomeRefused {
				t.Errorf("audit = %+v, want one refused record", audits)
			}
		})
	}
}

func TestForwardPingPongTerminatesWithinHopCap(t *testing.T) {
	// Two daemons, each forwarding to the other. A receiver rebuilds the event
	// from the envelope exactly as the receiving side does (the chain marks it
	// as forwarded) and hands it to its own forwarder. The loop must end in at
	// most DefaultForwardMaxHops deliveries.
	var recvA, recvB http.Handler
	var deliveries int32
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { recvA.ServeHTTP(w, r) }))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { recvB.ServeHTTP(w, r) }))
	defer srvB.Close()

	rigA := newFwdRig(t, nil)
	rigA.local = "daemon-a"
	rigA.addPeer(PeerRef{Name: "daemon-b", URL: srvB.URL, Token: "tok-b", Federated: true, HasAutonomousWrite: true})
	rigB := newFwdRig(t, nil)
	rigB.local = "daemon-b"
	rigB.addPeer(PeerRef{Name: "daemon-a", URL: srvA.URL, Token: "tok-a", Federated: true, HasAutonomousWrite: true})

	receiver := func(self *PeerForwarder, next string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&deliveries, 1)
			body, _ := io.ReadAll(r.Body)
			var env WebhookEnvelope
			if err := json.Unmarshal(body, &env); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			ev := env.Event
			ev.Forward = env.Forward
			if err := self.Forward(context.Background(), fwdSub(next), ev); err != nil {
				w.WriteHeader(http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
	}
	recvA = receiver(rigA.f, "daemon-b")
	recvB = receiver(rigB.f, "daemon-a")

	err := rigA.f.Forward(context.Background(), fwdSub("daemon-b"), fwdEvent())
	if !errors.Is(err, ErrForwardRefused) {
		t.Fatalf("err = %v, want the originating forward refused by the loop", err)
	}
	n := atomic.LoadInt32(&deliveries)
	if n < 1 || n > int32(DefaultForwardMaxHops) {
		t.Fatalf("deliveries = %d, want between 1 and the hop cap %d", n, DefaultForwardMaxHops)
	}
}

func TestForwardRefusesWithoutLocalHostOrFactory(t *testing.T) {
	peer := newFwdPeer(t)
	enabled := func() ForwardSettings { return ForwardSettings{Enabled: true} }
	lookup := func(string) (PeerRef, bool) { return fwdPeerRef(peer.srv.URL), true }
	factory := func(bool, time.Duration) *http.Client { return &http.Client{} }

	cases := []struct {
		name string
		cfg  PeerForwarderConfig
		want string
	}{
		{
			name: "local host unknown",
			cfg: PeerForwarderConfig{LookupPeer: lookup, LocalHost: func() string { return "  " },
				Settings: enabled, NewClient: factory},
			want: "local host name is unknown",
		},
		{
			name: "no local host seam",
			cfg:  PeerForwarderConfig{LookupPeer: lookup, Settings: enabled, NewClient: factory},
			want: "local host name is unknown",
		},
		{
			name: "no client factory",
			cfg: PeerForwarderConfig{LookupPeer: lookup, LocalHost: func() string { return fwdLocal },
				Settings: enabled},
			want: "not wired",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := NewPeerForwarder(c.cfg)
			err := f.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent())
			if !errors.Is(err, ErrForwardRefused) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want a refusal mentioning %q", err, c.want)
			}
			if n := len(peer.requests()); n != 0 {
				t.Errorf("requests = %d, want 0", n)
			}
		})
	}
}

func TestForwardEndpointPolicy(t *testing.T) {
	good := []struct{ raw, want string }{
		{"https://peer.example.net", ForwardedEventsPath},
		{"https://peer.example.net/", ForwardedEventsPath},
		{"http://10.0.0.5:8080", ForwardedEventsPath},
		{"https://peer.example.net/base/", "/base" + ForwardedEventsPath},
	}
	for _, c := range good {
		u, err := forwardEndpoint(c.raw)
		if err != nil {
			t.Errorf("forwardEndpoint(%q): %v", c.raw, err)
			continue
		}
		if u.Path != c.want {
			t.Errorf("forwardEndpoint(%q).Path = %q, want %q", c.raw, u.Path, c.want)
		}
	}
	u, err := forwardEndpoint("https://peer.example.net/?x=1#frag")
	if err != nil || u.RawQuery != "" || u.Fragment != "" {
		t.Errorf("query and fragment must be dropped: %v %+v", err, u)
	}
	bad := []string{
		"",
		"ftp://peer.example.net",
		"https://",
		"https://user:pw@peer.example.net",
		"://no-scheme",
	}
	for _, raw := range bad {
		if _, err := forwardEndpoint(raw); err == nil {
			t.Errorf("forwardEndpoint(%q) accepted a URL that must be refused", raw)
		}
	}
}

// ---------------------------------------------------------------------------
// Token, skip-verify and oversize
// ---------------------------------------------------------------------------

func TestForwardTokenNeverInAuditOrAlert(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	failing := newFwdPeer(t, http.StatusInternalServerError)

	for _, c := range []struct{ name, url string }{
		{"offline peer", goneURL},
		{"server error", failing.srv.URL},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newFwdRig(t, func(s *ForwardSettings) { s.MaxAttempts = 2 })
			r.addPeer(fwdPeerRef(c.url))
			err := r.f.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent())
			if err == nil {
				t.Fatal("expected a dropped forward")
			}
			var all strings.Builder
			all.WriteString(err.Error())
			for _, a := range r.auditRecs() {
				fmt.Fprint(&all, a.action, a.details)
			}
			for _, a := range r.alertRecs() {
				all.WriteString(a.title + " " + a.body)
			}
			if strings.Contains(all.String(), fwdToken) {
				t.Errorf("bearer token appeared in an error, an audit record or an alert: %s", all.String())
			}
		})
	}
}

func TestForwardSkipVerifyPassedToClientFactory(t *testing.T) {
	peer := newFwdPeer(t)
	r := newFwdRig(t, nil)
	ref := fwdPeerRef(peer.srv.URL)
	ref.TLSSkipVerify = true
	r.addPeer(ref)

	if err := r.f.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent()); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.skips) != 1 || !r.skips[0] {
		t.Errorf("client factory skip flags = %v, want [true]", r.skips)
	}
}

func TestForwardRefusesOversizeEnvelopeWithoutSending(t *testing.T) {
	peer := newFwdPeer(t)
	r := newFwdRig(t, nil)
	r.addPeer(fwdPeerRef(peer.srv.URL))
	ev := fwdEvent()
	ev.Kind = strings.Repeat("k", WebhookMaxBodyBytes+1)

	err := r.f.Forward(context.Background(), fwdSub(fwdPeerName), ev)
	if !errors.Is(err, ErrForwardDelivery) || !strings.Contains(err.Error(), "byte cap") {
		t.Fatalf("err = %v, want a size refusal", err)
	}
	if n := len(peer.requests()); n != 0 {
		t.Errorf("requests = %d, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// Disabled
// ---------------------------------------------------------------------------

func TestForwardDisabledMakesNoRequest(t *testing.T) {
	peer := newFwdPeer(t)
	r := newFwdRig(t, func(s *ForwardSettings) { s.Enabled = false })
	r.addPeer(fwdPeerRef(peer.srv.URL))

	if err := r.f.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent()); err != nil {
		t.Fatalf("disabled Forward returned %v, want nil", err)
	}
	if n := len(peer.requests()); n != 0 {
		t.Errorf("requests = %d, want 0 while disabled", n)
	}
	if len(r.auditRecs()) != 0 || r.alertCount() != 0 {
		t.Errorf("disabled forward audited or alerted")
	}
	if r.f.Enabled() {
		t.Errorf("Enabled() = true while settings say disabled")
	}

	var nilForwarder *PeerForwarder
	if nilForwarder.Enabled() {
		t.Errorf("a nil forwarder must report disabled")
	}
	if err := nilForwarder.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent()); err != nil {
		t.Errorf("nil forwarder Forward = %v, want nil", err)
	}
	if err := (&PeerForwarder{}).Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent()); err != nil {
		t.Errorf("unconfigured forwarder Forward = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// Subscription validation
// ---------------------------------------------------------------------------

func TestForwardSubscriptionValidation(t *testing.T) {
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	exact := strings.Repeat("a", MaxPeerNameLen)
	long := strings.Repeat("a", MaxPeerNameLen+1)
	cases := []struct {
		name    string
		mutate  func(*Subscription)
		wantErr string
	}{
		{"valid name", func(s *Subscription) { s.Sink.Target = "peer-b.edge_1" }, ""},
		{"name at the length cap", func(s *Subscription) { s.Sink.Target = exact }, ""},
		{"one-shot is accepted", func(s *Subscription) { s.OneShot = true }, ""},
		{"empty target", func(s *Subscription) { s.Sink.Target = "" }, "sink target is required"},
		{"blank target", func(s *Subscription) { s.Sink.Target = "   " }, "sink target is required"},
		{"name too long", func(s *Subscription) { s.Sink.Target = long }, "limit is 64"},
		{"space in name", func(s *Subscription) { s.Sink.Target = "peer b" }, "may contain only"},
		{"slash in name", func(s *Subscription) { s.Sink.Target = "peer/b" }, "may contain only"},
		{"action refused", func(s *Subscription) {
			s.Action = &SubscriptionAction{Type: SinkActionSendInput, Text: "hi"}
		}, "action is only valid with a session sink"},
		{"secret reference refused", func(s *Subscription) { s.Sink.SecretRef = "${secret:x}" },
			"secret_ref is only valid with a webhook sink"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sub := fwdSub(fwdPeerName)
			c.mutate(&sub)
			err := ValidateSubscription(sub, now)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSubscription: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want one mentioning %q", err, c.wantErr)
			}
		})
	}

	sub := fwdSub(fwdPeerName)
	sub.Sink.Type = "carrier-pigeon"
	if err := ValidateSubscription(sub, now); err == nil || !strings.Contains(err.Error(), SinkPeer) {
		t.Errorf("unknown sink error = %v, want it to list the peer sink", err)
	}
	if SinkPeer != "peer" {
		t.Errorf("SinkPeer = %q, want %q", SinkPeer, "peer")
	}
}

func TestForwardPeerSubscriptionTargetChecks(t *testing.T) {
	on := ForwardSettings{Enabled: true}
	found := func(p PeerRef) func(string) (PeerRef, bool) {
		return func(string) (PeerRef, bool) { return p, true }
	}
	notFound := func(string) (PeerRef, bool) { return PeerRef{}, false }
	good := PeerRef{Name: fwdPeerName, Federated: true, HasAutonomousWrite: true}

	cases := []struct {
		name     string
		settings ForwardSettings
		lookup   func(string) (PeerRef, bool)
		sub      Subscription
		want     string
	}{
		{"disabled names the key", ForwardSettings{}, found(good), fwdSub(fwdPeerName),
			"autonomous.notify.forwarding.enabled"},
		{"bad name is checked first", on, notFound, fwdSub("bad name"), "may contain only"},
		{"no registry", on, nil, fwdSub(fwdPeerName), "no federation peer registry"},
		{"unknown peer", on, notFound, fwdSub(fwdPeerName), "not a registered"},
		{"not federated", on, found(PeerRef{Name: fwdPeerName, HasAutonomousWrite: true}), fwdSub(fwdPeerName),
			"not a federation peer"},
		{"no write grant", on, found(PeerRef{Name: fwdPeerName, Federated: true}), fwdSub(fwdPeerName),
			"autonomous:write"},
		{"accepted", on, found(good), fwdSub(fwdPeerName), ""},
		{"webhook sinks are not this check's business", ForwardSettings{},
			nil, Subscription{Sink: SubscriptionSink{Type: SinkWebhook}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePeerSubscriptionTarget(c.sub, c.settings, c.lookup)
			if c.want == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, c.want)
			}
		})
	}
}

func TestForwardStoreRefusesPeerSubscriptionWhenDisabled(t *testing.T) {
	store, err := NewSubscriptionStore(filepath.Join(t.TempDir(), "subs.json"), nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	settings := ForwardSettings{Enabled: false}
	good := PeerRef{Name: fwdPeerName, Federated: true, HasAutonomousWrite: true}
	store.SetSinkValidator(func(sub Subscription) error {
		return ValidatePeerSubscriptionTarget(sub, settings, func(string) (PeerRef, bool) { return good, true })
	})

	if _, err := store.Add(fwdSub(fwdPeerName)); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("Add while disabled: err = %v, want a refusal naming the disabled state", err)
	}
	settings.Enabled = true
	if _, err := store.Add(fwdSub(fwdPeerName)); err != nil {
		t.Fatalf("Add while enabled: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Chains, settings, wire pins
// ---------------------------------------------------------------------------

func TestForwardChainHelpers(t *testing.T) {
	first := extendForwardChain(nil, "a")
	if first.OriginHost != "a" || first.Hops != 1 || len(first.Visited) != 1 || first.Visited[0] != "a" {
		t.Errorf("locally originated chain = %+v", first)
	}
	prior := &ForwardChain{OriginHost: "b", Hops: 1, Visited: []string{"b"}}
	next := extendForwardChain(prior, "a")
	if next.OriginHost != "b" || next.Hops != 2 || fmt.Sprint(next.Visited) != "[b a]" {
		t.Errorf("extended chain = %+v", next)
	}
	if fmt.Sprint(prior.Visited) != "[b]" || prior.Hops != 1 {
		t.Errorf("prior chain was mutated: %+v", prior)
	}
	if reason := forwardRefusal(nil, "a", 3); reason != "" {
		t.Errorf("a locally originated event must be allowed, got %q", reason)
	}
}

func TestForwardSettingsDefaults(t *testing.T) {
	d := DefaultForwardSettings()
	if d.Enabled || d.MaxHops != DefaultForwardMaxHops || d.MaxAttempts != DefaultWebhookMaxAttempts ||
		d.BackoffBase != DefaultWebhookBackoffBase || d.BackoffMax != DefaultWebhookBackoffMax ||
		d.Timeout != DefaultWebhookTimeout {
		t.Errorf("DefaultForwardSettings = %+v", d)
	}
	z := ForwardSettings{}.withDefaults()
	if z.MaxHops != DefaultForwardMaxHops || z.MaxAttempts != DefaultWebhookMaxAttempts ||
		z.BackoffBase != DefaultWebhookBackoffBase || z.BackoffMax != DefaultWebhookBackoffMax ||
		z.Timeout != DefaultWebhookTimeout {
		t.Errorf("zero settings did not take defaults: %+v", z)
	}
}

func TestForwardWirePinned(t *testing.T) {
	if ForwardedEventsPath != "/api/autonomous/events/forward" {
		t.Errorf("ForwardedEventsPath = %q", ForwardedEventsPath)
	}
	if DefaultForwardMaxHops != 3 {
		t.Errorf("DefaultForwardMaxHops = %d, want 3", DefaultForwardMaxHops)
	}
	if MaxPeerNameLen != 64 {
		t.Errorf("MaxPeerNameLen = %d, want 64", MaxPeerNameLen)
	}
}

func TestForwardEventJSONOmitsForwardWhenLocal(t *testing.T) {
	local, err := json.Marshal(fwdEvent())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(local), `"forward"`) {
		t.Errorf("a local event serialised a forward key: %s", local)
	}
	ev := fwdEvent()
	ev.Forward = &ForwardChain{OriginHost: "daemon-c", Hops: 2, Visited: []string{"daemon-c", "daemon-d"}}
	fwd, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fwd), `"forward":{"origin_host":"daemon-c","hops":2,"visited":["daemon-c","daemon-d"]}`) {
		t.Errorf("forwarded event JSON = %s", fwd)
	}
}

// ---------------------------------------------------------------------------
// Engine routing
// ---------------------------------------------------------------------------

// newFwdEngine starts a session-push engine over a fresh Manager and store.
func newFwdEngine(t *testing.T, r *fwdRig) (*LifecycleSubscriptionEngine, *SubscriptionStore, *Manager) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	store, err := NewSubscriptionStore(filepath.Join(t.TempDir(), "subs.json"), nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	eng := NewLifecycleSubscriptionEngine(m, store, SessionPushConfig{Forwarder: r.f})
	if err := eng.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(eng.Close)
	return eng, store, m
}

func TestForwardEngineRoutesPeerSinkAndSkipsForwarded(t *testing.T) {
	peer := newFwdPeer(t)
	r := newFwdRig(t, nil)
	r.addPeer(fwdPeerRef(peer.srv.URL))
	_, store, m := newFwdEngine(t, r)
	if _, err := store.Add(fwdSub(fwdPeerName)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	first := fwdEvent()
	m.emit(first)
	waitFor(t, "first peer delivery", func() bool { return len(peer.requests()) == 1 })

	forwarded := fwdEvent()
	forwarded.ID = "ev-forwarded"
	forwarded.Forward = &ForwardChain{OriginHost: "daemon-c", Hops: 1, Visited: []string{"daemon-c"}}
	m.emit(forwarded)
	marker := fwdEvent()
	marker.ID = "ev-marker"
	m.emit(marker)
	waitFor(t, "marker delivery", func() bool { return len(peer.requests()) == 2 })

	ids := fwdEnvelopeEventIDs(t, peer.requests())
	if fmt.Sprint(ids) != "[ev-1 ev-marker]" {
		t.Errorf("events reaching the peer = %v, want the forwarded event skipped", ids)
	}
	if n := len(store.List("")); n != 1 {
		t.Errorf("a repeating subscription must stay: %d left, want 1", n)
	}
}

func TestForwardEngineOneShotConsumedOnlyAfterSuccess(t *testing.T) {
	peer := newFwdPeer(t, http.StatusInternalServerError)
	r := newFwdRig(t, func(s *ForwardSettings) { s.MaxAttempts = 1 })
	r.addPeer(fwdPeerRef(peer.srv.URL))
	_, store, m := newFwdEngine(t, r)
	sub := fwdSub(fwdPeerName)
	sub.OneShot = true
	if _, err := store.Add(sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	m.emit(fwdEvent())
	waitFor(t, "dropped forward alert", func() bool { return r.alertCount() == 1 })
	if n := len(store.List("")); n != 1 {
		t.Fatalf("one-shot consumed after a failed forward: %d left, want 1", n)
	}

	peer.setStatuses(http.StatusOK)
	m.emit(fwdEvent())
	waitFor(t, "one-shot consumed after success", func() bool { return len(store.List("")) == 0 })
	if n := len(peer.requests()); n != 2 {
		t.Errorf("requests = %d, want 2 (one failed, one delivered)", n)
	}
}

func TestForwardEngineDisabledLeavesOneShotArmed(t *testing.T) {
	peer := newFwdPeer(t)
	r := newFwdRig(t, func(s *ForwardSettings) { s.Enabled = false })
	r.addPeer(fwdPeerRef(peer.srv.URL))
	_, store, m := newFwdEngine(t, r)
	sub := fwdSub(fwdPeerName)
	sub.OneShot = true
	if _, err := store.Add(sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Wait until the engine has looked at the disabled settings for the first event.
	m.emit(fwdEvent())
	waitFor(t, "engine checked the disabled settings", func() bool { return r.settingsCallCount() >= 1 })
	if n := len(store.List("")); n != 1 {
		t.Fatalf("one-shot consumed while disabled: %d left, want 1", n)
	}

	r.setEnabled(true)
	m.emit(fwdEvent())
	waitFor(t, "armed one-shot consumed after enabling", func() bool { return len(store.List("")) == 0 })
	if n := len(peer.requests()); n != 1 {
		t.Errorf("requests = %d, want 1 (only the event after enabling)", n)
	}
}

func TestForwardEngineRetryDoesNotBlockEmit(t *testing.T) {
	peer := newFwdPeer(t, http.StatusInternalServerError)
	r := newFwdRig(t, func(s *ForwardSettings) { s.MaxAttempts = 2 })
	release := make(chan struct{})
	r.sleepHook = func(time.Duration) { <-release }
	r.addPeer(fwdPeerRef(peer.srv.URL))
	_, store, m := newFwdEngine(t, r)
	if _, err := store.Add(fwdSub(fwdPeerName)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	start := time.Now()
	m.emit(fwdEvent())
	if d := time.Since(start); d > time.Second {
		t.Errorf("emit took %v while the peer is retrying; the emit path must not wait", d)
	}
	waitFor(t, "first attempt while the retry waits", func() bool { return len(peer.requests()) == 1 })
	close(release)
	waitFor(t, "alert after the second attempt", func() bool { return r.alertCount() == 1 })
	if n := len(peer.requests()); n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
}

// TestForwardDefaultTimerWaitsBetweenAttempts runs the retry wait on its own
// timer (no Sleep seam) and checks that a context ending during the wait
// abandons the forward instead of waiting out the delay.
func TestForwardDefaultTimerWaitsBetweenAttempts(t *testing.T) {
	t.Run("timer fires then delivers", func(t *testing.T) {
		peer := newFwdPeer(t, http.StatusInternalServerError, http.StatusOK)
		f := NewPeerForwarder(PeerForwarderConfig{
			LookupPeer: func(string) (PeerRef, bool) { return fwdPeerRef(peer.srv.URL), true },
			LocalHost:  func() string { return fwdLocal },
			Settings: func() ForwardSettings {
				s := DefaultForwardSettings()
				s.Enabled = true
				s.BackoffBase = time.Millisecond
				s.BackoffMax = time.Millisecond
				return s
			},
			NewClient: func(_ bool, timeout time.Duration) *http.Client { return &http.Client{Timeout: timeout} },
		})
		if err := f.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent()); err != nil {
			t.Fatalf("Forward: %v", err)
		}
		if n := len(peer.requests()); n != 2 {
			t.Errorf("requests = %d, want 2", n)
		}
	})

	t.Run("context ends during the wait", func(t *testing.T) {
		peer := newFwdPeer(t, http.StatusInternalServerError)
		f := NewPeerForwarder(PeerForwarderConfig{
			LookupPeer: func(string) (PeerRef, bool) { return fwdPeerRef(peer.srv.URL), true },
			LocalHost:  func() string { return fwdLocal },
			Settings: func() ForwardSettings {
				s := DefaultForwardSettings()
				s.Enabled = true
				s.BackoffBase = time.Hour
				s.BackoffMax = time.Hour
				return s
			},
			NewClient: func(_ bool, timeout time.Duration) *http.Client { return &http.Client{Timeout: timeout} },
		})
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		start := time.Now()
		err := f.Forward(ctx, fwdSub(fwdPeerName), fwdEvent())
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want a wrapped context.DeadlineExceeded", err)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("forward waited %v; the hour-long delay must be cut short by the context", d)
		}
		if n := len(peer.requests()); n != 1 {
			t.Errorf("requests = %d, want 1 before the context ended", n)
		}
	})
}

// TestForwardNilClientFactoryResultFallsBack covers a factory that returns no
// client: the forwarder builds a plain one with the peer's timeout.
func TestForwardNilClientFactoryResultFallsBack(t *testing.T) {
	peer := newFwdPeer(t)
	f := NewPeerForwarder(PeerForwarderConfig{
		LookupPeer: func(string) (PeerRef, bool) { return fwdPeerRef(peer.srv.URL), true },
		LocalHost:  func() string { return fwdLocal },
		Settings: func() ForwardSettings {
			s := DefaultForwardSettings()
			s.Enabled = true
			return s
		},
		NewClient: func(bool, time.Duration) *http.Client { return nil },
	})
	if err := f.Forward(context.Background(), fwdSub(fwdPeerName), fwdEvent()); err != nil {
		t.Fatalf("Forward with a nil factory client: %v", err)
	}
	if n := len(peer.requests()); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
}
