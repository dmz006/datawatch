// Tests that a federation peer being down never blocks local delivery.
//
// A peer can be down in two ways: it accepts a connection and never answers
// (a hang), or nothing listens at its address (a refusal). Either way the
// daemon's own sinks (comm, session, topic, alert, mcp_queue and webhook
// subscriptions, plus the built-in alert, topic and comm sinks) must receive
// every local event within the normal time, and when the peer returns the
// exchange must resume without a restart.
//
// Clocks: the forwarder, the webhook sink and the peer event subscriber all
// run on one fake clock whose Sleep returns at once, so no retry delay is ever
// waited out. The only real waits are short HTTP timeouts.

package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	// peerDownDeadline is the normal time a local sink has to receive an event
	// while a peer is down. A hanging peer holds its request open for far longer.
	peerDownDeadline = 2 * time.Second
	// peerDownLongTimeout keeps a hanging request in flight for the whole test.
	peerDownLongTimeout = 30 * time.Second
	// peerDownShortTimeout is the per-request timeout of the bounded-hang run.
	peerDownShortTimeout = 100 * time.Millisecond

	peerDownHangName   = "hang-peer"
	peerDownRefuseName = "refuse-peer"
	peerDownFlakyName  = "flaky-peer"
	peerDownReadName   = "read-peer"
	peerDownToken      = "tok-peer-down-SECRET-456"
	peerDownLocalHost  = fwdLocal
)

// ---------------------------------------------------------------------------
// Fake clock and peers
// ---------------------------------------------------------------------------

// peerDownClock is a fake clock. Sleep advances it and returns at once.
type peerDownClock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
}

func newPeerDownClock() *peerDownClock {
	return &peerDownClock{now: time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)}
}

func (c *peerDownClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Millisecond)
	return c.now
}

func (c *peerDownClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.slept = append(c.slept, d)
}

func (c *peerDownClock) sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.slept...)
}

// peerDownFreeAddr returns a loopback address with nothing listening on it:
// a connection to it is refused.
func peerDownFreeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

// peerDownStartAt starts a server on addr, the address a refused peer used.
func peerDownStartAt(t *testing.T, addr string, h http.Handler) *httptest.Server {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		// Called from a delivery goroutine as well, so it must not Fatalf.
		t.Errorf("the peer could not return on %s: %v", addr, err)
		return nil
	}
	srv := httptest.NewUnstartedServer(h)
	_ = srv.Listener.Close()
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

// peerDownHangPeer accepts requests and answers none of them until the request
// is cancelled or the test releases it.
type peerDownHangPeer struct {
	srv     *httptest.Server
	entered atomic.Int64
	exited  atomic.Int64
	release chan struct{}
	once    sync.Once
}

func newPeerDownHangPeer(t *testing.T) *peerDownHangPeer {
	t.Helper()
	p := &peerDownHangPeer{release: make(chan struct{})}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.entered.Add(1)
		defer p.exited.Add(1)
		// Read the body so the server notices a client that gives up.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-p.release:
		}
	}))
	// Cleanups run last-in first-out: release the hung requests, then close.
	t.Cleanup(p.srv.Close)
	t.Cleanup(p.free)
	return p
}

func (p *peerDownHangPeer) free() { p.once.Do(func() { close(p.release) }) }

// peerDownReceiver records every forwarded envelope it is sent.
type peerDownReceiver struct {
	mu   sync.Mutex
	ids  []string
	hits atomic.Int64
}

func (p *peerDownReceiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var env WebhookEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err == nil {
		p.mu.Lock()
		p.ids = append(p.ids, env.Event.ID)
		p.mu.Unlock()
	}
	p.hits.Add(1)
	w.WriteHeader(http.StatusOK)
}

func (p *peerDownReceiver) eventIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.ids...)
}

// ---------------------------------------------------------------------------
// Rig: every local sink, a forwarder with the fake clock, peer subscriptions
// ---------------------------------------------------------------------------

type peerDownRig struct {
	t     *testing.T
	m     *Manager
	store *SubscriptionStore
	clock *peerDownClock
	rec   *sessionPushRecorder
	queue *PendingEventQueue
	hook  *httptest.Server
	hooks atomic.Int64

	mu      sync.Mutex
	local   map[string]int
	peers   map[string]PeerRef
	alerts  []string
	audits  []string
	onSleep func()
}

// newPeerDownRig wires the built-in sinks and a subscription engine with one
// subscription per local sink type. timeout and attempts are the forwarder's
// per-request timeout and attempt cap.
func newPeerDownRig(t *testing.T, timeout time.Duration, attempts int) *peerDownRig {
	t.Helper()
	r := &peerDownRig{
		t:     t,
		clock: newPeerDownClock(),
		rec:   &sessionPushRecorder{},
		queue: NewPendingEventQueue(0),
		local: map[string]int{},
		peers: map[string]PeerRef{},
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	store, err := NewSubscriptionStore(filepath.Join(t.TempDir(), "subs.json"), nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	r.m, r.store = m, store
	// The store runs the live webhook check on Add; the harness sink allows
	// the loopback receiver below.
	store.SetSinkValidator(func(sub Subscription) error {
		return ValidateWebhookSubscriptionTarget(sub, WebhookSettings{Enabled: true, AllowHosts: []string{"127.0.0.1"}})
	})

	r.hook = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		r.hooks.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.hook.Close)
	hookRig := newWebhookHarness(t, nil)

	fwd := NewPeerForwarder(PeerForwarderConfig{
		LookupPeer: func(name string) (PeerRef, bool) {
			r.mu.Lock()
			defer r.mu.Unlock()
			p, ok := r.peers[name]
			return p, ok
		},
		LocalHost: func() string { return peerDownLocalHost },
		Settings: func() ForwardSettings {
			s := DefaultForwardSettings()
			s.Enabled = true
			s.Timeout = timeout
			s.MaxAttempts = attempts
			return s
		},
		NewClient: func(_ bool, d time.Duration) *http.Client { return &http.Client{Timeout: d} },
		Audit: func(_ string, details map[string]any) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.audits = append(r.audits, fmt.Sprint(details["outcome"]))
		},
		RaiseAlert: func(_, _, body string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.alerts = append(r.alerts, body)
		},
		Sleep: func(d time.Duration) {
			r.clock.Sleep(d)
			r.mu.Lock()
			fn := r.onSleep
			r.mu.Unlock()
			if fn != nil {
				fn()
			}
		},
		Now:    r.clock.Now,
		Jitter: func() float64 { return 0.5 },
	})

	// The built-in sinks: alert and topic in one install, comm in another (a
	// comm sink coalesces, and with it set the other two are not used).
	cancelAlertTopic := InstallLifecycleSinks(m, SinkConfig{
		RecordAlert:   func(_, _, _ string) { r.add("builtin-alert") },
		PublishTopics: func(_ []string, _, _ string) { r.add("builtin-topic") },
	})
	cancelComm := InstallLifecycleSinks(m, SinkConfig{
		CommSink:   func(string) { r.add("builtin-comm") },
		CommWindow: time.Millisecond,
	})

	eng := NewLifecycleSubscriptionEngine(m, store, SessionPushConfig{
		FindSession:   sessionTable{"worker-1": 0}.find,
		SendInput:     r.rec.sendInput,
		Comm:          CommSinkConfig{Send: func(string) { r.add("sub-comm") }, Window: time.Millisecond},
		RecordAlert:   func(_, _, _ string) { r.add("sub-alert") },
		PublishTopics: func(_ []string, _, _ string) { r.add("sub-topic") },
		PendingQueue:  r.queue,
		Webhook:       hookRig.sink,
		Forwarder:     fwd,
	})
	if err := eng.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		eng.Close()
		cancelAlertTopic()
		cancelComm()
		m.lifecycleBus().Close()
	})

	sinks := []SubscriptionSink{
		{Type: SinkComm, Target: "operator"},
		{Type: SinkSession, Target: "worker-1"},
		{Type: SinkTopic, Target: "alerts"},
		{Type: SinkAlert, Target: "alerts"},
		{Type: SinkMCPQueue, Target: "local-feed"},
		{Type: SinkWebhook, Target: r.hook.URL + "/hook", SecretRef: webhookTestRef},
	}
	for _, sink := range sinks {
		r.subscribe(sink)
	}
	return r
}

func (r *peerDownRig) subscribe(sink SubscriptionSink) {
	r.t.Helper()
	sub := Subscription{
		Owner:  "operator",
		Filter: LifecycleFilter{Kinds: []string{fwdKind}},
		Sink:   sink,
	}
	if _, err := r.store.Add(sub); err != nil {
		r.t.Fatalf("Add %s subscription: %v", sink.Type, err)
	}
}

// addPeer registers a federation peer at url and subscribes it to the events.
func (r *peerDownRig) addPeer(name, url string) {
	r.t.Helper()
	r.mu.Lock()
	r.peers[name] = PeerRef{Name: name, URL: url, Token: peerDownToken, Federated: true, HasAutonomousWrite: true}
	r.mu.Unlock()
	r.subscribe(SubscriptionSink{Type: SinkPeer, Target: name})
}

func (r *peerDownRig) add(sink string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.local[sink]++
}

func (r *peerDownRig) setOnSleep(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onSleep = fn
}

func (r *peerDownRig) alertTexts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.alerts...)
}

func (r *peerDownRig) outcomes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.audits...)
}

// localCounts is how many events each local sink has received so far. The
// comm sinks coalesce, so their count is messages, not events.
func (r *peerDownRig) localCounts() map[string]int {
	r.mu.Lock()
	out := make(map[string]int, len(r.local)+3)
	for k, v := range r.local {
		out[k] = v
	}
	r.mu.Unlock()
	out["sub-session"] = r.rec.inputCount()
	out["sub-webhook"] = int(r.hooks.Load())
	evs, _, _ := r.queue.Pending("local-feed", 0, 0)
	out["sub-queue"] = len(evs)
	return out
}

var peerDownLocalSinks = []string{
	"builtin-alert", "builtin-topic", "builtin-comm",
	"sub-alert", "sub-topic", "sub-comm", "sub-session", "sub-queue", "sub-webhook",
}

// localShort lists the local sinks that have not yet received n events (one
// message for a coalescing comm sink).
func (r *peerDownRig) localShort(n int) []string {
	counts := r.localCounts()
	var short []string
	for _, name := range peerDownLocalSinks {
		want := n
		if strings.HasSuffix(name, "-comm") && n > 0 {
			want = 1
		}
		if counts[name] < want {
			short = append(short, fmt.Sprintf("%s=%d/%d", name, counts[name], want))
		}
	}
	return short
}

// requireLocal fails unless every local sink has received n events within
// peerDownDeadline, and then checks no sink received a duplicate.
func (r *peerDownRig) requireLocal(n int) {
	r.t.Helper()
	start := time.Now()
	for {
		short := r.localShort(n)
		if len(short) == 0 {
			break
		}
		if time.Since(start) > peerDownDeadline {
			r.t.Fatalf("local sinks short of %d event(s) after %v: %v", n, peerDownDeadline, short)
		}
		time.Sleep(time.Millisecond)
	}
	counts := r.localCounts()
	for _, name := range peerDownLocalSinks {
		if !strings.HasSuffix(name, "-comm") && counts[name] != n {
			r.t.Errorf("sink %s received %d event(s), want exactly %d", name, counts[name], n)
		}
	}
}

// queueEventIDs is the event ids the local mcp_queue sink holds, in order.
func (r *peerDownRig) queueEventIDs() []string {
	evs, _, _ := r.queue.Pending("local-feed", 0, 0)
	ids := make([]string, 0, len(evs))
	for _, ev := range evs {
		ids = append(ids, ev.ID)
	}
	return ids
}

func peerDownEvent(i int) LifecycleEvent {
	ev := fwdEvent()
	ev.ID = fmt.Sprintf("ev-%d", i)
	return ev
}

// peerDownWaitUntil waits for cond for up to limit of real time.
func peerDownWaitUntil(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	start := time.Now()
	for !cond() {
		if time.Since(start) > limit {
			t.Fatalf("timed out after %v waiting for %s", limit, what)
		}
		time.Sleep(time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// A peer that is down never blocks local delivery
// ---------------------------------------------------------------------------

func TestPeerDownDoesNotBlockLocalDelivery(t *testing.T) {
	t.Run("hanging and refusing peers while a request is in flight", func(t *testing.T) {
		r := newPeerDownRig(t, peerDownLongTimeout, 2)
		hang := newPeerDownHangPeer(t)
		r.addPeer(peerDownHangName, hang.srv.URL)
		r.addPeer(peerDownRefuseName, "http://"+peerDownFreeAddr(t))

		start := time.Now()
		r.m.emit(peerDownEvent(1))
		r.requireLocal(1)
		if d := time.Since(start); d > peerDownDeadline {
			t.Errorf("first local delivery took %v with two peers down; want under %v", d, peerDownDeadline)
		}

		// The hanging peer now holds the first forward open. Its subscription
		// queue is stuck behind it; nothing else is.
		peerDownWaitUntil(t, peerDownDeadline, "the hanging peer to receive the request", func() bool { return hang.entered.Load() == 1 })
		peerDownWaitUntil(t, peerDownDeadline, "the refused forward to be dropped", func() bool {
			for _, a := range r.alertTexts() {
				if strings.Contains(a, peerDownRefuseName) {
					return true
				}
			}
			return false
		})

		r.m.emit(peerDownEvent(2))
		r.m.emit(peerDownEvent(3))
		r.requireLocal(3)
		if got := fmt.Sprint(r.queueEventIDs()); got != "[ev-1 ev-2 ev-3]" {
			t.Errorf("local queue = %s, want every event in emit order", got)
		}
		if n := hang.exited.Load(); n != 0 {
			t.Errorf("the hanging request ended (%d) before the local sinks were checked; the test did not prove the hang was in flight", n)
		}
		if n := hang.entered.Load(); n != 1 {
			t.Errorf("hanging peer saw %d requests, want 1: its later events wait behind the hung one", n)
		}
		if n := len(r.clock.sleeps()); n == 0 {
			t.Errorf("the refused peer was never retried on the fake clock")
		}
	})

	t.Run("a hanging peer is bounded by the timeout and the daemon keeps delivering", func(t *testing.T) {
		r := newPeerDownRig(t, peerDownShortTimeout, 2)
		hang := newPeerDownHangPeer(t)
		r.addPeer(peerDownHangName, hang.srv.URL)

		r.m.emit(peerDownEvent(1))
		r.requireLocal(1)
		peerDownWaitUntil(t, peerDownDeadline, "the hung forward to time out and be dropped", func() bool {
			return len(r.alertTexts()) == 1
		})
		if n := hang.entered.Load(); n != 2 {
			t.Errorf("hanging peer saw %d requests, want 2 (the attempt cap)", n)
		}
		if !strings.Contains(r.alertTexts()[0], peerDownHangName) {
			t.Errorf("alert %q does not name the hanging peer", r.alertTexts()[0])
		}

		// After the drop the peer subscription is free again and local delivery
		// carries on.
		r.m.emit(peerDownEvent(2))
		r.requireLocal(2)
		peerDownWaitUntil(t, peerDownDeadline, "the second hung forward to be dropped", func() bool {
			return len(r.alertTexts()) == 2
		})
	})

	t.Run("a refusing peer is retried on the fake clock and dropped once", func(t *testing.T) {
		r := newPeerDownRig(t, peerDownLongTimeout, 3)
		r.addPeer(peerDownRefuseName, "http://"+peerDownFreeAddr(t))

		start := time.Now()
		r.m.emit(peerDownEvent(1))
		r.requireLocal(1)
		peerDownWaitUntil(t, peerDownDeadline, "the refused forward to be dropped", func() bool {
			return len(r.alertTexts()) == 1
		})
		if d := time.Since(start); d > peerDownDeadline {
			t.Errorf("a refused peer took %v to give up; the delays must run on the fake clock", d)
		}
		if got := fmt.Sprint(r.outcomes()); got != "[retry retry failed]" {
			t.Errorf("forward outcomes = %s, want [retry retry failed]", got)
		}
		if n := len(r.clock.sleeps()); n != 2 {
			t.Errorf("fake clock slept %d times, want 2 (between three attempts)", n)
		}
		if a := r.alertTexts()[0]; strings.Contains(a, peerDownToken) {
			t.Errorf("alert leaks the peer token: %q", a)
		}
	})
}

// ---------------------------------------------------------------------------
// A peer that returns resumes delivery
// ---------------------------------------------------------------------------

func TestPeerDownRecovers(t *testing.T) {
	t.Run("a forward retry succeeds when the peer returns during the wait", func(t *testing.T) {
		r := newPeerDownRig(t, peerDownLongTimeout, 3)
		addr := peerDownFreeAddr(t)
		r.addPeer(peerDownFlakyName, "http://"+addr)
		recv := &peerDownReceiver{}
		var once sync.Once
		r.setOnSleep(func() {
			// The first retry delay passes on the fake clock and the peer comes back.
			once.Do(func() { peerDownStartAt(t, addr, recv) })
		})

		r.m.emit(peerDownEvent(1))
		r.requireLocal(1)
		peerDownWaitUntil(t, peerDownDeadline, "the forward to be delivered", func() bool { return recv.hits.Load() == 1 })
		if got := fmt.Sprint(recv.eventIDs()); got != "[ev-1]" {
			t.Errorf("peer received %s, want [ev-1]", got)
		}
		if got := fmt.Sprint(r.outcomes()); got != "[retry delivered]" {
			t.Errorf("forward outcomes = %s, want [retry delivered]", got)
		}
		if a := r.alertTexts(); len(a) != 0 {
			t.Errorf("a forward that recovered raised alerts: %v", a)
		}
	})

	t.Run("an event after a dropped forward reaches the peer once it is back", func(t *testing.T) {
		r := newPeerDownRig(t, peerDownLongTimeout, 2)
		addr := peerDownFreeAddr(t)
		r.addPeer(peerDownFlakyName, "http://"+addr)

		r.m.emit(peerDownEvent(1))
		r.requireLocal(1)
		peerDownWaitUntil(t, peerDownDeadline, "the first forward to be dropped", func() bool { return len(r.alertTexts()) == 1 })

		recv := &peerDownReceiver{}
		peerDownStartAt(t, addr, recv)
		r.m.emit(peerDownEvent(2))
		r.requireLocal(2)
		peerDownWaitUntil(t, peerDownDeadline, "the peer to receive the next event", func() bool { return recv.hits.Load() == 1 })
		// Forwarding is best effort with no outbox: the dropped event is not replayed.
		if got := fmt.Sprint(recv.eventIDs()); got != "[ev-2]" {
			t.Errorf("peer received %s, want only [ev-2]", got)
		}
		if n := len(r.store.List("")); n != 7 {
			t.Errorf("subscriptions = %d, want 7: a peer being down must not remove its subscription", n)
		}
		if a := r.alertTexts(); len(a) != 1 {
			t.Errorf("alerts = %d, want only the one from the dropped forward", len(a))
		}
	})

	t.Run("reading a peer resumes when it returns and local events flow throughout", func(t *testing.T) {
		r := newPeerDownRig(t, peerDownLongTimeout, 2)
		addr := peerDownFreeAddr(t)
		clock := newPeerDownClock()

		var (
			failuresSeen atomic.Int64
			gate         = make(chan struct{})
			stop         = make(chan struct{})
			audits       sync.Mutex
			retries      int
			accepted     int
			raised       int
		)
		cfg := PeerEventSubscriberConfig{
			LookupPeer: func(name string) (PeerRef, bool) {
				return PeerRef{Name: name, URL: "http://" + addr, Token: peerDownToken, Federated: true, HasAutonomousRead: true}, true
			},
			LocalHost: func() string { return peerDownLocalHost },
			Settings: func() ForwardSettings {
				s := DefaultForwardSettings()
				s.Enabled = true
				s.Timeout = peerDownLongTimeout
				s.MaxAttempts = 2
				return s
			},
			NewClient: func(_ bool, d time.Duration) *http.Client { return &http.Client{Timeout: d} },
			Audit: func(_ string, details map[string]any) {
				audits.Lock()
				defer audits.Unlock()
				switch details["outcome"] {
				case WebhookOutcomeRetry:
					retries++
				case "accepted":
					accepted++
				}
			},
			RaiseAlert: func(_, _, _ string) {
				audits.Lock()
				defer audits.Unlock()
				raised++
			},
			Now:    clock.Now,
			Jitter: func() float64 { return 0.5 },
			Sleep: func(d time.Duration) {
				clock.Sleep(d)
				switch failuresSeen.Add(1) {
				case 1:
					<-gate // hold the first backoff until the local checks are done
				case 2:
					// The peer comes back during the second backoff.
					remote := fwdEvent()
					remote.ID = "remote-1"
					remote.Host = "remote-host"
					peerDownStartAt(t, addr, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						peerSubPage(w, []LifecycleEvent{remote}, 7)
					}))
				default:
					<-stop // after a good read, wait out the poll interval
				}
			},
		}
		var pulled []string
		var pulledMu sync.Mutex
		cancelPull := r.m.Subscribe(LifecycleFilter{Kinds: []string{fwdKind}, Peer: peerDownReadName}, func(ev LifecycleEvent) {
			pulledMu.Lock()
			defer pulledMu.Unlock()
			pulled = append(pulled, ev.ID+"@"+ev.Peer)
		})
		t.Cleanup(cancelPull)

		sub := r.m.NewPeerEventSubscriber(cfg)
		t.Cleanup(func() {
			close(stop)
			sub.StopAll()
		})
		if err := sub.Start(context.Background(), PeerEventSource{Peer: peerDownReadName, Queue: "local-feed"}); err != nil {
			t.Fatalf("Start: %v", err)
		}

		// The peer refuses the first read. While the subscriber waits out its
		// backoff, local events still reach every local sink.
		peerDownWaitUntil(t, peerDownDeadline, "the first failed read", func() bool {
			audits.Lock()
			defer audits.Unlock()
			return retries == 1
		})
		r.m.emit(peerDownEvent(1))
		r.requireLocal(1)
		close(gate)

		// The second read fails too and raises the one alert; then the peer
		// returns and the third read publishes its event into local dispatch.
		peerDownWaitUntil(t, peerDownDeadline, "the peer's event to reach local dispatch", func() bool {
			pulledMu.Lock()
			defer pulledMu.Unlock()
			return len(pulled) == 1
		})
		pulledMu.Lock()
		got := fmt.Sprint(pulled)
		pulledMu.Unlock()
		if got != "["+"remote-1@"+peerDownReadName+"]" {
			t.Errorf("events read from the peer = %s, want remote-1 stamped with the peer name", got)
		}
		audits.Lock()
		r2, a2, n2 := retries, accepted, raised
		audits.Unlock()
		if r2 != 2 || a2 != 1 || n2 != 1 {
			t.Errorf("retries=%d accepted=%d alerts=%d, want 2, 1 and 1", r2, a2, n2)
		}

		// The peer's event also reached the local sinks that follow peer events:
		// the built-in alert sink saw both the local and the peer event.
		peerDownWaitUntil(t, peerDownDeadline, "the built-in alert sink to see the peer event", func() bool {
			return r.localCounts()["builtin-alert"] == 2
		})
		sleeps := clock.sleeps()
		if len(sleeps) < 2 || !sort.SliceIsSorted(sleeps[:2], func(i, j int) bool { return sleeps[i] < sleeps[j] }) {
			t.Errorf("read backoff delays = %v, want a growing curve for two failures", sleeps)
		}
	})
}
