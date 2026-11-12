// Tests for the peer event subscription: events read from a registered peer
// enter local dispatch with the peer name set, a peer without the read
// capability is refused, and each peer runs in its own goroutine with a timeout
// and backoff.

package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	peerSubLocal = "daemon-local"
	peerSubToken = "tok-peer-subscription-secret"
)

// peerSubEvent is an Automation status event as a peer's own emitter produces it.
func peerSubEvent(id, host string) LifecycleEvent {
	return LifecycleEvent{
		ID:           id,
		At:           time.Now().UTC(),
		Kind:         string(LevelAutomation) + "." + string(AutomationActive),
		Level:        LevelAutomation,
		AutomationID: "auto-" + id,
		OldStatus:    string(AutomationDraft),
		NewStatus:    string(AutomationActive),
		Host:         host,
	}
}

// peerSubRequest is what the fake peer saw on one request.
type peerSubRequest struct {
	query  map[string]string
	auth   string
	sig    string
	chain  string
	agent  string
	method string
	path   string
}

// peerSubServer is a scripted peer events endpoint. handle returns the status
// and body of each request (n is 1-based); nil answers an empty page.
type peerSubServer struct {
	*httptest.Server
	mu    sync.Mutex
	reqs  []peerSubRequest
	count atomic.Int64
}

func newPeerSubServer(t *testing.T, handle func(n int, r *http.Request, w http.ResponseWriter)) *peerSubServer {
	t.Helper()
	ps := &peerSubServer{}
	ps.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		ps.mu.Lock()
		ps.reqs = append(ps.reqs, peerSubRequest{
			query: q, auth: r.Header.Get("Authorization"), sig: r.Header.Get(WebhookSignatureHeader),
			chain: r.Header.Get(federation.HopChainHeader), agent: r.Header.Get("User-Agent"),
			method: r.Method, path: r.URL.Path,
		})
		ps.mu.Unlock()
		n := int(ps.count.Add(1))
		if handle == nil {
			peerSubPage(w, nil, 0)
			return
		}
		handle(n, r, w)
	}))
	t.Cleanup(ps.Close)
	return ps
}

func (ps *peerSubServer) requests() []peerSubRequest {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return append([]peerSubRequest(nil), ps.reqs...)
}

// peerSubPage writes a 200 page the way the peer's events endpoint does.
func peerSubPage(w http.ResponseWriter, evs []LifecycleEvent, next uint64) {
	if evs == nil {
		evs = []LifecycleEvent{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"events": evs, "next_cursor": next, "dropped": false})
}

// peerSubRecorder collects published events, audit records and alerts.
type peerSubRecorder struct {
	mu      sync.Mutex
	events  []LifecycleEvent
	audits  []map[string]any
	actions []string
	alerts  []string
	delays  []time.Duration
}

func (r *peerSubRecorder) publish(ev LifecycleEvent) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *peerSubRecorder) audit(action string, d map[string]any) {
	r.mu.Lock()
	r.actions = append(r.actions, action)
	r.audits = append(r.audits, d)
	r.mu.Unlock()
}

func (r *peerSubRecorder) alert(_, title, body string) {
	r.mu.Lock()
	r.alerts = append(r.alerts, title+": "+body)
	r.mu.Unlock()
}

func (r *peerSubRecorder) sleep(d time.Duration) {
	r.mu.Lock()
	r.delays = append(r.delays, d)
	r.mu.Unlock()
	time.Sleep(time.Millisecond)
}

func (r *peerSubRecorder) snapshot() (evs []LifecycleEvent, audits []map[string]any, alerts []string, delays []time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]LifecycleEvent(nil), r.events...), append([]map[string]any(nil), r.audits...),
		append([]string(nil), r.alerts...), append([]time.Duration(nil), r.delays...)
}

// peerSubPeer builds a usable registry entry for url.
func peerSubPeer(url string) PeerRef {
	return PeerRef{Name: "peer-alpha", URL: url, Token: peerSubToken, Federated: true, HasAutonomousRead: true}
}

// peerSubSubscriber builds a subscriber wired to rec with peer as its only
// registered peer. PollInterval is short so a test does not wait.
func peerSubSubscriber(rec *peerSubRecorder, peer PeerRef, set ForwardSettings) *PeerEventSubscriber {
	return NewPeerEventSubscriber(PeerEventSubscriberConfig{
		LookupPeer: func(name string) (PeerRef, bool) {
			if name == peer.Name {
				return peer, true
			}
			return PeerRef{}, false
		},
		LocalHost:    func() string { return peerSubLocal },
		Settings:     func() ForwardSettings { return set },
		NewClient:    func(_ bool, timeout time.Duration) *http.Client { return &http.Client{Timeout: timeout} },
		Publish:      rec.publish,
		Audit:        rec.audit,
		RaiseAlert:   rec.alert,
		PollInterval: time.Millisecond,
		Sleep:        rec.sleep,
		Jitter:       func() float64 { return 0.5 },
	})
}

func peerSubSettings() ForwardSettings {
	return ForwardSettings{
		Enabled:     true,
		MaxAttempts: 3,
		Backoff:     []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond},
		Timeout:     2 * time.Second,
	}
}

func peerSubWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPeerEventSubscriptionDeliversEventsWithPeerName(t *testing.T) {
	valid := peerSubEvent("ev-1", "daemon-alpha")
	noHost := peerSubEvent("ev-2", "")
	spoofed := peerSubEvent("ev-3", "daemon-alpha")
	spoofed.Peer = "spoofed-peer"
	ps := newPeerSubServer(t, func(n int, _ *http.Request, w http.ResponseWriter) {
		if n == 1 {
			peerSubPage(w, []LifecycleEvent{valid, noHost, spoofed}, 7)
			return
		}
		peerSubPage(w, nil, 7)
	})
	rec := &peerSubRecorder{}
	sub := peerSubSubscriber(rec, peerSubPeer(ps.URL), peerSubSettings())
	defer sub.StopAll()

	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "watch"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	peerSubWaitFor(t, "three events", func() bool { evs, _, _, _ := rec.snapshot(); return len(evs) == 3 })
	peerSubWaitFor(t, "a second read", func() bool { return len(ps.requests()) >= 2 })

	evs, _, alerts, _ := rec.snapshot()
	if len(alerts) != 0 {
		t.Fatalf("unexpected alerts: %v", alerts)
	}
	for _, ev := range evs {
		if ev.Peer != "peer-alpha" {
			t.Errorf("event %s Peer = %q, want peer-alpha (payload value must not win)", ev.ID, ev.Peer)
		}
		if ev.Forward == nil || ev.Forward.Hops != 1 || len(ev.Forward.Visited) != 1 || ev.Forward.Visited[0] != ev.Host {
			t.Errorf("event %s is not marked as forwarded from its origin host: %+v", ev.ID, ev.Forward)
		}
	}
	if evs[0].Host != "daemon-alpha" {
		t.Errorf("Host = %q, want the peer's own host name daemon-alpha", evs[0].Host)
	}
	if evs[1].Host != "peer-alpha" {
		t.Errorf("an event with no host got Host %q, want the peer name", evs[1].Host)
	}

	reqs := ps.requests()
	first, second := reqs[0], reqs[1]
	if first.method != http.MethodGet || first.path != PeerEventsPath {
		t.Errorf("request = %s %s, want GET %s", first.method, first.path, PeerEventsPath)
	}
	if first.query["queue"] != "watch" || first.query["since"] != "0" || first.query["limit"] == "" {
		t.Errorf("first query = %v", first.query)
	}
	if second.query["since"] != "7" {
		t.Errorf("second read since = %q, want the cursor 7 the peer returned", second.query["since"])
	}
	if first.auth != "Bearer "+peerSubToken {
		t.Errorf("Authorization = %q, want the peer's stored bearer token", first.auth)
	}
	if first.sig == "" || strings.Contains(first.sig, peerSubToken) {
		t.Errorf("signature header = %q: want a signature that does not contain the token", first.sig)
	}
	if first.chain == "" || strings.Contains(first.chain, peerSubToken) {
		t.Errorf("hop chain header = %q: want a chain that does not contain the token", first.chain)
	}
	if first.agent != webhookUserAgent {
		t.Errorf("User-Agent = %q, want %q", first.agent, webhookUserAgent)
	}
}

func TestPeerEventSubscriptionDropsWhatIsNotLifecycle(t *testing.T) {
	forwarded := peerSubEvent("ev-fwd", "daemon-alpha")
	forwarded.Forward = &ForwardChain{OriginHost: "daemon-z", Hops: 1, Visited: []string{"daemon-z"}}
	test := peerSubEvent("ev-test", "daemon-alpha")
	test.Test = true
	unknown := peerSubEvent("ev-unknown", "daemon-alpha")
	unknown.Kind = "session.output"
	loop := peerSubEvent("ev-loop", peerSubLocal)
	good := peerSubEvent("ev-good", "daemon-alpha")

	ps := newPeerSubServer(t, func(n int, _ *http.Request, w http.ResponseWriter) {
		if n == 1 {
			peerSubPage(w, []LifecycleEvent{forwarded, test, unknown, loop, good}, 5)
			return
		}
		peerSubPage(w, nil, 5)
	})
	rec := &peerSubRecorder{}
	sub := peerSubSubscriber(rec, peerSubPeer(ps.URL), peerSubSettings())
	defer sub.StopAll()
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	peerSubWaitFor(t, "the good event", func() bool { evs, _, _, _ := rec.snapshot(); return len(evs) >= 1 })
	peerSubWaitFor(t, "five audit records", func() bool { _, a, _, _ := rec.snapshot(); return len(a) >= 5 })

	evs, audits, _, _ := rec.snapshot()
	if len(evs) != 1 || evs[0].ID != "ev-good" {
		t.Fatalf("published %d events (%v), want only ev-good", len(evs), evs)
	}
	refused := 0
	for _, a := range audits {
		if a["outcome"] == WebhookOutcomeRefused {
			refused++
		}
		if a["direction"] != "read" || a["peer"] != "peer-alpha" {
			t.Errorf("audit record lacks direction or peer: %v", a)
		}
	}
	if refused != 4 {
		t.Errorf("refused records = %d, want 4 (forwarded, test, unknown kind, this host)", refused)
	}
}

func TestPeerEventSubscriptionRefusesPeerWithoutReadCapability(t *testing.T) {
	ps := newPeerSubServer(t, nil)
	rec := &peerSubRecorder{}
	peer := peerSubPeer(ps.URL)
	peer.HasAutonomousRead = false
	sub := peerSubSubscriber(rec, peer, peerSubSettings())

	err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"})
	if !errors.Is(err, ErrPeerEventRefused) {
		t.Fatalf("Start error = %v, want ErrPeerEventRefused", err)
	}
	if !strings.Contains(err.Error(), string(federation.CapAutonomousRead)) {
		t.Errorf("the refusal does not name the missing capability: %v", err)
	}
	if got := len(ps.requests()); got != 0 {
		t.Fatalf("the peer saw %d request(s); a peer without read capability must never be called", got)
	}
	if running := sub.Running(); len(running) != 0 {
		t.Fatalf("goroutines running for a refused peer: %v", running)
	}
	_, audits, alerts, _ := rec.snapshot()
	if len(alerts) != 1 {
		t.Errorf("alerts = %d, want exactly 1: %v", len(alerts), alerts)
	}
	if len(audits) != 1 || audits[0]["outcome"] != WebhookOutcomeRefused {
		t.Errorf("audit records = %v, want one refusal", audits)
	}
	if len(rec.actions) != 1 || rec.actions[0] != audit.ActionLifecycleForwardReceive {
		t.Errorf("audit actions = %v", rec.actions)
	}
}

func TestPeerEventSubscriptionRefusals(t *testing.T) {
	ps := newPeerSubServer(t, nil)
	local := func() string { return peerSubLocal }
	okPeer := peerSubPeer(ps.URL)
	cases := []struct {
		name string
		src  PeerEventSource
		mut  func(*PeerRef)
		cfg  func(*PeerEventSubscriberConfig)
		want string
	}{
		{name: "empty queue", src: PeerEventSource{Peer: "peer-alpha"}, want: "queue name is required"},
		{name: "bad peer name", src: PeerEventSource{Peer: "peer alpha", Queue: "q"}, want: "peer name may contain only"},
		{name: "unknown peer", src: PeerEventSource{Peer: "peer-other", Queue: "q"}, want: "no registered, enabled federation peer"},
		{name: "not a federation peer", src: PeerEventSource{Peer: "peer-alpha", Queue: "q"}, mut: func(p *PeerRef) { p.Federated = false }, want: "not a federation peer"},
		{name: "no token", src: PeerEventSource{Peer: "peer-alpha", Queue: "q"}, mut: func(p *PeerRef) { p.Token = " " }, want: "no bearer token"},
		{name: "bad url", src: PeerEventSource{Peer: "peer-alpha", Queue: "q"}, mut: func(p *PeerRef) { p.URL = "ftp://x" }, want: "http or https"},
		{name: "url with credentials", src: PeerEventSource{Peer: "peer-alpha", Queue: "q"}, mut: func(p *PeerRef) { p.URL = "http://u:p@x" }, want: "user information"},
		{name: "unknown local host", src: PeerEventSource{Peer: "peer-alpha", Queue: "q"}, cfg: func(c *PeerEventSubscriberConfig) { c.LocalHost = func() string { return " " } }, want: "local host name is unknown"},
		{name: "no registry", src: PeerEventSource{Peer: "peer-alpha", Queue: "q"}, cfg: func(c *PeerEventSubscriberConfig) { c.LookupPeer = nil }, want: "not wired"},
		{name: "no client", src: PeerEventSource{Peer: "peer-alpha", Queue: "q"}, cfg: func(c *PeerEventSubscriberConfig) { c.NewClient = nil }, want: "not wired"},
		{name: "no dispatch", src: PeerEventSource{Peer: "peer-alpha", Queue: "q"}, cfg: func(c *PeerEventSubscriberConfig) { c.Publish = nil }, want: "not wired"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &peerSubRecorder{}
			peer := okPeer
			if tc.mut != nil {
				tc.mut(&peer)
			}
			cfg := PeerEventSubscriberConfig{
				LookupPeer: func(name string) (PeerRef, bool) { return peer, name == "peer-alpha" },
				LocalHost:  local,
				Settings:   func() ForwardSettings { return peerSubSettings() },
				NewClient:  func(_ bool, d time.Duration) *http.Client { return &http.Client{Timeout: d} },
				Publish:    rec.publish, Audit: rec.audit, RaiseAlert: rec.alert,
			}
			if tc.cfg != nil {
				tc.cfg(&cfg)
			}
			sub := NewPeerEventSubscriber(cfg)
			err := sub.Start(context.Background(), tc.src)
			if !errors.Is(err, ErrPeerEventRefused) {
				t.Fatalf("Start error = %v, want ErrPeerEventRefused", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
			if strings.Contains(err.Error(), peerSubToken) {
				t.Errorf("the refusal leaks the token: %v", err)
			}
			if len(sub.Running()) != 0 {
				t.Errorf("a goroutine runs for a refused peer")
			}
			if _, _, alerts, _ := rec.snapshot(); len(alerts) != 1 {
				t.Errorf("alerts = %d, want 1", len(alerts))
			}
		})
	}
}

func TestPeerEventSubscriptionNilSubscriber(t *testing.T) {
	var sub *PeerEventSubscriber
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "p", Queue: "q"}); !errors.Is(err, ErrPeerEventRefused) {
		t.Fatalf("nil Start = %v", err)
	}
	if sub.Stop("p") || sub.Running() != nil {
		t.Fatal("a nil subscriber must report nothing running")
	}
	sub.StopAll() // must not panic
}

func TestPeerEventSubscriptionBackoffThenRecovery(t *testing.T) {
	ps := newPeerSubServer(t, func(n int, _ *http.Request, w http.ResponseWriter) {
		switch {
		case n <= 4:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case n == 5:
			peerSubPage(w, []LifecycleEvent{peerSubEvent("ev-late", "daemon-alpha")}, 9)
		default:
			peerSubPage(w, nil, 9)
		}
	})
	rec := &peerSubRecorder{}
	sub := peerSubSubscriber(rec, peerSubPeer(ps.URL), peerSubSettings())
	defer sub.StopAll()
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	peerSubWaitFor(t, "recovery", func() bool { evs, _, _, _ := rec.snapshot(); return len(evs) == 1 })

	_, audits, alerts, delays := rec.snapshot()
	if len(alerts) != 1 || !strings.Contains(alerts[0], "keeps retrying") {
		t.Fatalf("alerts = %v, want exactly one after MaxAttempts consecutive failures", alerts)
	}
	// Four failures: the shared curve's schedule, last entry repeating, +-0 jitter at 0.5.
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond}
	got := []time.Duration{}
	for _, d := range delays {
		if d >= 5*time.Millisecond { // the poll-interval waits are 1ms
			got = append(got, d)
		}
	}
	if len(got) < len(want) {
		t.Fatalf("backoff delays = %v, want at least %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("delay %d = %v, want %v (all: %v)", i, got[i], w, got)
		}
	}
	retries := 0
	for _, a := range audits {
		if a["outcome"] == WebhookOutcomeRetry {
			retries++
			if a["status_code"] != http.StatusServiceUnavailable {
				t.Errorf("retry record status = %v", a["status_code"])
			}
		}
	}
	if retries != 4 {
		t.Errorf("retry records = %d, want 4", retries)
	}
	// After recovery the failure count resets: a new outage alerts again.
	if got := len(sub.Running()); got != 1 {
		t.Errorf("goroutines = %d, want 1 still running after recovery", got)
	}
}

func TestPeerEventSubscriptionHonoursRetryAfter(t *testing.T) {
	ps := newPeerSubServer(t, func(n int, _ *http.Request, w http.ResponseWriter) {
		if n == 1 {
			w.Header().Set("Retry-After", "30")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		peerSubPage(w, nil, 0)
	})
	rec := &peerSubRecorder{}
	sub := peerSubSubscriber(rec, peerSubPeer(ps.URL), peerSubSettings())
	defer sub.StopAll()
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatal(err)
	}
	peerSubWaitFor(t, "a second request", func() bool { return len(ps.requests()) >= 2 })
	_, _, _, delays := rec.snapshot()
	if len(delays) == 0 || delays[0] < 30*time.Second {
		t.Fatalf("first delay = %v, want at least the 30s Retry-After", delays)
	}
}

func TestPeerEventSubscriptionPollTimeout(t *testing.T) {
	release := make(chan struct{})
	ps := newPeerSubServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		if n == 1 {
			select { // hang until the client gives up
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		peerSubPage(w, []LifecycleEvent{peerSubEvent("ev-after-timeout", "daemon-alpha")}, 1)
	})
	defer close(release)
	set := peerSubSettings()
	set.Timeout = 50 * time.Millisecond
	rec := &peerSubRecorder{}
	sub := peerSubSubscriber(rec, peerSubPeer(ps.URL), set)
	defer sub.StopAll()
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatal(err)
	}
	peerSubWaitFor(t, "the event after a timed out poll", func() bool { evs, _, _, _ := rec.snapshot(); return len(evs) == 1 })
	_, audits, _, _ := rec.snapshot()
	sawRetry := false
	for _, a := range audits {
		if a["outcome"] == WebhookOutcomeRetry {
			sawRetry = true
		}
	}
	if !sawRetry {
		t.Errorf("the timed out poll was not recorded as a retry: %v", audits)
	}
}

func TestPeerEventSubscriptionPermanentRefusalStops(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		ps := newPeerSubServer(t, func(_ int, _ *http.Request, w http.ResponseWriter) {
			http.Error(w, "no", status)
		})
		rec := &peerSubRecorder{}
		sub := peerSubSubscriber(rec, peerSubPeer(ps.URL), peerSubSettings())
		if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
			t.Fatal(err)
		}
		peerSubWaitFor(t, "the goroutine to stop", func() bool { return len(sub.Running()) == 0 })
		sub.StopAll()
		if got := len(ps.requests()); got != 1 {
			t.Errorf("status %d: peer saw %d requests, want 1 (a permanent answer is not retried)", status, got)
		}
		_, audits, alerts, _ := rec.snapshot()
		if len(alerts) != 1 {
			t.Errorf("status %d: alerts = %v, want 1", status, alerts)
		}
		if len(audits) != 1 || audits[0]["status_code"] != status {
			t.Errorf("status %d: audits = %v", status, audits)
		}
	}
}

func TestPeerEventSubscriptionRedirectIsNotFollowed(t *testing.T) {
	var elsewhere atomic.Int64
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
	}))
	defer other.Close()
	ps := newPeerSubServer(t, func(_ int, _ *http.Request, w http.ResponseWriter) {
		w.Header().Set("Location", other.URL)
		w.WriteHeader(http.StatusFound)
	})
	rec := &peerSubRecorder{}
	sub := peerSubSubscriber(rec, peerSubPeer(ps.URL), peerSubSettings())
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatal(err)
	}
	peerSubWaitFor(t, "the goroutine to stop", func() bool { return len(sub.Running()) == 0 })
	if elsewhere.Load() != 0 {
		t.Fatal("the redirect was followed: the bearer token would have gone to a second host")
	}
}

func TestPeerEventSubscriptionUnreadableAnswerIsRetried(t *testing.T) {
	ps := newPeerSubServer(t, func(n int, _ *http.Request, w http.ResponseWriter) {
		if n == 1 {
			_, _ = w.Write([]byte("not json"))
			return
		}
		peerSubPage(w, []LifecycleEvent{peerSubEvent("ev-ok", "daemon-alpha")}, 2)
	})
	rec := &peerSubRecorder{}
	sub := peerSubSubscriber(rec, peerSubPeer(ps.URL), peerSubSettings())
	defer sub.StopAll()
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatal(err)
	}
	peerSubWaitFor(t, "the event", func() bool { evs, _, _, _ := rec.snapshot(); return len(evs) == 1 })
}

func TestPeerEventSubscriptionNeverLeaksToken(t *testing.T) {
	// A transport failure's error text can carry the URL; it must come out
	// scrubbed in the audit record and the alert.
	ps := newPeerSubServer(t, nil)
	url := ps.URL
	ps.Close() // every read now fails to connect
	set := peerSubSettings()
	set.MaxAttempts = 1
	rec := &peerSubRecorder{}
	peer := peerSubPeer(url)
	sub := peerSubSubscriber(rec, peer, set)
	defer sub.StopAll()
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatal(err)
	}
	peerSubWaitFor(t, "an alert", func() bool { _, _, a, _ := rec.snapshot(); return len(a) == 1 })
	_, audits, alerts, _ := rec.snapshot()
	blob, _ := json.Marshal(audits)
	for _, text := range append(alerts, string(blob)) {
		if strings.Contains(text, peerSubToken) {
			t.Errorf("the token leaked: %s", text)
		}
	}
}

func TestPeerEventSubscriptionOneGoroutinePerPeer(t *testing.T) {
	psA := newPeerSubServer(t, nil)
	psB := newPeerSubServer(t, nil)
	rec := &peerSubRecorder{}
	peers := map[string]PeerRef{
		"peer-alpha": {Name: "peer-alpha", URL: psA.URL, Token: "tok-a", Federated: true, HasAutonomousRead: true},
		"peer-beta":  {Name: "peer-beta", URL: psB.URL, Token: "tok-b", Federated: true, HasAutonomousRead: true},
	}
	sub := NewPeerEventSubscriber(PeerEventSubscriberConfig{
		LookupPeer:   func(n string) (PeerRef, bool) { p, ok := peers[n]; return p, ok },
		LocalHost:    func() string { return peerSubLocal },
		Settings:     func() ForwardSettings { return peerSubSettings() },
		NewClient:    func(_ bool, d time.Duration) *http.Client { return &http.Client{Timeout: d} },
		Publish:      rec.publish,
		Audit:        rec.audit,
		RaiseAlert:   rec.alert,
		PollInterval: time.Millisecond,
	})
	ctx := context.Background()
	for _, p := range []string{"peer-alpha", "peer-beta"} {
		if err := sub.Start(ctx, PeerEventSource{Peer: p, Queue: "q-" + p}); err != nil {
			t.Fatalf("Start %s: %v", p, err)
		}
	}
	if err := sub.Start(ctx, PeerEventSource{Peer: "peer-alpha", Queue: "q"}); !errors.Is(err, ErrPeerEventRunning) {
		t.Fatalf("second Start for one peer = %v, want ErrPeerEventRunning", err)
	}
	if got := len(sub.Running()); got != 2 {
		t.Fatalf("running = %v, want both peers", sub.Running())
	}
	peerSubWaitFor(t, "both peers to be read", func() bool { return len(psA.requests()) > 0 && len(psB.requests()) > 0 })
	if a, b := psA.requests()[0], psB.requests()[0]; a.auth != "Bearer tok-a" || b.auth != "Bearer tok-b" ||
		a.query["queue"] != "q-peer-alpha" || b.query["queue"] != "q-peer-beta" {
		t.Errorf("each peer must be read with its own token and queue: %+v %+v", a, b)
	}

	if !sub.Stop("peer-alpha") {
		t.Fatal("Stop reported no goroutine for peer-alpha")
	}
	peerSubWaitFor(t, "peer-alpha to stop", func() bool { return len(sub.Running()) == 1 })
	if sub.Stop("peer-alpha") {
		t.Fatal("Stop reported a goroutine for an already stopped peer")
	}
	// A stopped peer can be started again.
	if err := sub.Start(ctx, PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	sub.StopAll()
	if got := sub.Running(); len(got) != 0 {
		t.Fatalf("running after StopAll = %v", got)
	}
}

func TestPeerEventSubscriptionStopsWhenContextEnds(t *testing.T) {
	ps := newPeerSubServer(t, nil)
	rec := &peerSubRecorder{}
	sub := peerSubSubscriber(rec, peerSubPeer(ps.URL), peerSubSettings())
	sub.cfg.Sleep = nil // wait on the real timer so cancellation interrupts it
	sub.cfg.PollInterval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	if err := sub.Start(ctx, PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatal(err)
	}
	peerSubWaitFor(t, "the first read", func() bool { return len(ps.requests()) >= 1 })
	cancel()
	peerSubWaitFor(t, "the goroutine to stop", func() bool { return len(sub.Running()) == 0 })
}

func TestPeerEventSubscriptionDisabledReadsNothing(t *testing.T) {
	ps := newPeerSubServer(t, nil)
	var enabled atomic.Bool
	set := peerSubSettings()
	rec := &peerSubRecorder{}
	sub := peerSubSubscriber(rec, peerSubPeer(ps.URL), set)
	sub.cfg.Settings = func() ForwardSettings { s := set; s.Enabled = enabled.Load(); return s }
	defer sub.StopAll()
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if n := len(ps.requests()); n != 0 {
		t.Fatalf("%d request(s) made while forwarding is disabled", n)
	}
	enabled.Store(true)
	peerSubWaitFor(t, "a read once enabled", func() bool { return len(ps.requests()) > 0 })

	// A nil settings hook is disabled too.
	nilSet := peerSubSubscriber(&peerSubRecorder{}, peerSubPeer(ps.URL), set)
	nilSet.cfg.Settings = nil
	if got := nilSet.settings(); got.Enabled {
		t.Fatal("a nil settings hook must read as disabled")
	}
}

func TestPeerEventSubscriptionStopsWhenCapabilityIsRevoked(t *testing.T) {
	ps := newPeerSubServer(t, nil)
	var revoked atomic.Bool
	peer := peerSubPeer(ps.URL)
	rec := &peerSubRecorder{}
	sub := peerSubSubscriber(rec, peer, peerSubSettings())
	sub.cfg.LookupPeer = func(string) (PeerRef, bool) {
		p := peer
		p.HasAutonomousRead = !revoked.Load()
		return p, true
	}
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatal(err)
	}
	peerSubWaitFor(t, "a read", func() bool { return len(ps.requests()) >= 1 })
	revoked.Store(true)
	peerSubWaitFor(t, "the goroutine to stop", func() bool { return len(sub.Running()) == 0 })
	_, _, alerts, _ := rec.snapshot()
	if len(alerts) != 1 || !strings.Contains(alerts[0], "refused") {
		t.Fatalf("alerts = %v, want one refusal", alerts)
	}
}

func TestPeerEventSubscriptionFeedsManagerDispatch(t *testing.T) {
	ps := newPeerSubServer(t, func(n int, _ *http.Request, w http.ResponseWriter) {
		if n == 1 {
			peerSubPage(w, []LifecycleEvent{peerSubEvent("ev-bus", "daemon-alpha")}, 3)
			return
		}
		peerSubPage(w, nil, 3)
	})
	m := new(Manager)
	got := make(chan LifecycleEvent, 4)
	defer m.Subscribe(LifecycleFilter{Peer: "peer-alpha"}, func(e LifecycleEvent) { got <- e })()
	other := make(chan LifecycleEvent, 4)
	defer m.Subscribe(LifecycleFilter{Peer: "peer-other"}, func(e LifecycleEvent) { other <- e })()

	rec := &peerSubRecorder{}
	cfg := PeerEventSubscriberConfig{
		LookupPeer:   func(string) (PeerRef, bool) { return peerSubPeer(ps.URL), true },
		LocalHost:    func() string { return peerSubLocal },
		Settings:     func() ForwardSettings { return peerSubSettings() },
		NewClient:    func(_ bool, d time.Duration) *http.Client { return &http.Client{Timeout: d} },
		Audit:        rec.audit,
		RaiseAlert:   rec.alert,
		PollInterval: time.Millisecond,
	}
	sub := m.NewPeerEventSubscriber(cfg)
	defer sub.StopAll()
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q"}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-got:
		if ev.ID != "ev-bus" || ev.Peer != "peer-alpha" || ev.Forward == nil {
			t.Fatalf("local subscriber saw %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the local subscriber did not receive the peer's event")
	}
	select {
	case ev := <-other:
		t.Fatalf("a subscriber filtered to another peer received %+v", ev)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestPeerEventSubscriptionStampPeerEvent(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	ev := peerSubEvent("", "daemon-alpha")
	ev.At = time.Time{}
	out, reason := stampPeerEvent("peer-alpha", peerSubLocal, ev, now)
	if reason != "" {
		t.Fatalf("refused: %s", reason)
	}
	if out.ID == "" || !out.At.Equal(now) || out.Peer != "peer-alpha" || out.Host != "daemon-alpha" {
		t.Fatalf("stamped event = %+v", out)
	}
	if ev.Forward != nil || ev.Peer != "" {
		t.Fatal("the input event was mutated")
	}

	long := peerSubEvent("ev-long", strings.Repeat("h", maxForwardHostLen+1))
	if _, why := stampPeerEvent("peer-alpha", peerSubLocal, long, now); !strings.Contains(why, "too long") {
		t.Fatalf("an over-long host was accepted: %q", why)
	}
}

func TestPeerEventSubscriptionAuditTextIsBounded(t *testing.T) {
	if got := auditPeerText(strings.Repeat("x", 500)); len(got) != 128 {
		t.Fatalf("len = %d, want 128", len(got))
	}
	if got := auditPeerText("short"); got != "short" {
		t.Fatalf("got %q", got)
	}
}

func TestPeerEventSubscriptionDroppedQueueIsRecorded(t *testing.T) {
	ps := newPeerSubServer(t, func(n int, _ *http.Request, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"events": []LifecycleEvent{}, "next_cursor": 4, "dropped": n == 1})
	})
	rec := &peerSubRecorder{}
	sub := peerSubSubscriber(rec, peerSubPeer(ps.URL), peerSubSettings())
	defer sub.StopAll()
	if err := sub.Start(context.Background(), PeerEventSource{Peer: "peer-alpha", Queue: "q", SinceCursor: 2}); err != nil {
		t.Fatal(err)
	}
	peerSubWaitFor(t, "a skipped record", func() bool {
		_, a, _, _ := rec.snapshot()
		return len(a) >= 1 && a[0]["outcome"] == WebhookOutcomeSkipped
	})
	if first := ps.requests()[0]; first.query["since"] != "2" {
		t.Errorf("first read since = %q, want the resume cursor 2", first.query["since"])
	}
}
