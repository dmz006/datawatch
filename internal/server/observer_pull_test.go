package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/observer"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// newObserverPullServer builds a minimal Server whose federation peer store
// holds one federated, pull-opted-in entry named "peer1" pointing at url,
// authenticated with token. mutate can tweak the entry before Add.
func newObserverPullServer(t *testing.T, url, token string, mutate func(*multiserver.Entry)) *Server {
	t.Helper()
	st, err := multiserver.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	e := &multiserver.Entry{
		Name:                  "peer1",
		URL:                   url,
		Token:                 token,
		Enabled:               true,
		Federated:             true,
		ObserverPull:          true,
		Capabilities:          []string{federation.CapObserversRead},
		ObserverPullIntervalS: 30,
		ObserverPullTimeoutS:  5,
	}
	if mutate != nil {
		mutate(e)
	}
	if err := st.Add(e); err != nil {
		t.Fatalf("Add: %v", err)
	}
	return &Server{serverStore: st, cfg: pullCfg(true)}
}

// statsOK writes a minimal valid v2 StatsResponse.
func statsOK(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"v":2,"host":{"name":"remote-host","uptime_seconds":42}}`))
}

func TestObserverPullFetch_ValidPayloadDecodes(t *testing.T) {
	var gotPath string
	var gotV string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotV = r.URL.Query().Get("v")
		statsOK(w, r)
	}))
	defer srv.Close()

	s := newObserverPullServer(t, srv.URL, "token-abc", nil)
	op := &observerPull{s: s}
	snap, err := op.Fetch(context.Background(), "peer1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.V != 2 {
		t.Errorf("V = %d, want 2", snap.V)
	}
	if snap.Host.Name != "remote-host" {
		t.Errorf("Host.Name = %q, want %q", snap.Host.Name, "remote-host")
	}
	if gotPath != "/api/observer/stats" {
		t.Errorf("path = %q, want /api/observer/stats", gotPath)
	}
	if gotV != "2" {
		t.Errorf("v query = %q, want 2", gotV)
	}
}

func TestObserverPullFetch_BearerHeaderSent(t *testing.T) {
	const token = "secret-bearer-token-xyz"
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		statsOK(w, r)
	}))
	defer srv.Close()

	s := newObserverPullServer(t, srv.URL, token, nil)
	op := &observerPull{s: s}
	if _, err := op.Fetch(context.Background(), "peer1"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if want := "Bearer " + token; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestObserverPullFetch_CapabilityDenied_MakesNoRequest(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		statsOK(w, r)
	}))
	defer srv.Close()

	s := newObserverPullServer(t, srv.URL, "token-abc", func(e *multiserver.Entry) {
		e.Capabilities = []string{federation.CapSessionsList}
	})
	op := &observerPull{s: s}
	_, err := op.Fetch(context.Background(), "peer1")
	if !errors.Is(err, observer.ErrNotPermitted) {
		t.Fatalf("err = %v, want observer.ErrNotPermitted", err)
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Errorf("requests = %d, want 0", got)
	}
}

func TestObserverPullFetch_NotOptedIn_MakesNoRequest(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		statsOK(w, r)
	}))
	defer srv.Close()

	s := newObserverPullServer(t, srv.URL, "token-abc", func(e *multiserver.Entry) {
		e.ObserverPull = false
	})
	op := &observerPull{s: s}
	_, err := op.Fetch(context.Background(), "peer1")
	if !errors.Is(err, observer.ErrNotPermitted) {
		t.Fatalf("err = %v, want observer.ErrNotPermitted", err)
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Errorf("requests = %d, want 0", got)
	}
}

func TestObserverPullFetch_ForbiddenResponse_MapsToForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	s := newObserverPullServer(t, srv.URL, "token-abc", nil)
	op := &observerPull{s: s}
	_, err := op.Fetch(context.Background(), "peer1")
	if err == nil {
		t.Fatal("expected an error for a 403 response")
	}
	var httpErr *observer.PullHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *observer.PullHTTPError", err)
	}
	if httpErr.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want 403", httpErr.StatusCode)
	}
}

func TestObserverPullFetch_SlowPeer_MapsToTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
			statsOK(w, r)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	s := newObserverPullServer(t, srv.URL, "token-abc", func(e *multiserver.Entry) {
		e.ObserverPullTimeoutS = 1 // short per-peer timeout
	})
	op := &observerPull{s: s}
	_, err := op.Fetch(context.Background(), "peer1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestObserverPullFetch_TokenNotInErrorStrings(t *testing.T) {
	const token = "super-secret-peer-token"

	cases := map[string]http.HandlerFunc{
		"forbidden": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "denied", http.StatusForbidden)
		},
		"bad_payload": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("this is not json"))
		},
		"gate_failure": nil, // no request at all
	}
	for name, handler := range cases {
		name, handler := name, handler
		t.Run(name, func(t *testing.T) {
			var srv *httptest.Server
			if handler != nil {
				srv = httptest.NewServer(handler)
				defer srv.Close()
			} else {
				// Counting server that must never be hit.
				var requests int32
				srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(&requests, 1)
				}))
				defer srv.Close()
				if got := atomic.LoadInt32(&requests); got != 0 {
					t.Fatalf("requests = %d, want 0", got)
				}
			}

			mutate := func(e *multiserver.Entry) {}
			if name == "gate_failure" {
				mutate = func(e *multiserver.Entry) { e.ObserverPull = false }
			}
			s := newObserverPullServer(t, srv.URL, token, mutate)
			op := &observerPull{s: s}
			_, err := op.Fetch(context.Background(), "peer1")
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), token) {
				t.Errorf("error %q contains the bearer token", err)
			}
			if name == "bad_payload" && !errors.Is(err, observer.ErrBadPayload) {
				t.Errorf("err = %v, want observer.ErrBadPayload", err)
			}
		})
	}
}

func TestObserverPullFetch_TLSSkipVerify(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statsOK(w, r)
	}))
	defer srv.Close()
	// Sanity: this really is a TLS endpoint.
	if !strings.HasPrefix(srv.URL, "https://") {
		t.Fatalf("test server URL = %q, want https", srv.URL)
	}

	s := newObserverPullServer(t, srv.URL, "token-abc", func(e *multiserver.Entry) {
		e.TLSSkipVerify = true
	})
	op := &observerPull{s: s}
	snap, err := op.Fetch(context.Background(), "peer1")
	if err != nil {
		t.Fatalf("Fetch over TLS with skip-verify: %v", err)
	}
	if snap.Host.Name != "remote-host" {
		t.Errorf("Host.Name = %q, want %q", snap.Host.Name, "remote-host")
	}
}

// --- target sync / collector wiring ---------------------------------------

type testTicker struct{ ch chan time.Time }

func (tk *testTicker) Chan() <-chan time.Time { return tk.ch }
func (tk *testTicker) Stop()                  {}

// testTickers hands out channel-driven tickers so worker goroutines park
// after their initial fetch instead of running a real schedule.
type testTickers struct{ list []*testTicker }

func (r *testTickers) new(time.Duration) observer.Ticker {
	tk := &testTicker{ch: make(chan time.Time, 16)}
	r.list = append(r.list, tk)
	return tk
}

func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", d, what)
}

func TestObserverPullFetch_SyncTargetsOnPeerChange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(statsOK))
	defer srv.Close()

	s := newObserverPullServer(t, srv.URL, "token-abc", nil) // creates peer1

	reg := &testTickers{}
	op := &observerPull{s: s}
	op.col = observer.NewPullCollector(observer.PullCollectorOptions{
		Fetcher:   op,
		NewTicker: reg.new,
	})
	s.observerPull = op
	defer op.col.Stop()

	op.col.SetTargets(op.targets())
	waitFor(t, 2*time.Second, "initial pull of peer1", func() bool {
		st, ok := op.col.Last("peer1")
		return ok && !st.LastAttemptAt.IsZero()
	})

	// A new peer added to the store becomes a target after a sync.
	e2 := &multiserver.Entry{
		Name:         "peer2",
		URL:          srv.URL,
		Token:        "token-2",
		Enabled:      true,
		Federated:    true,
		ObserverPull: true,
		Capabilities: []string{federation.CapObserversRead},
	}
	if err := s.serverStore.Add(e2); err != nil {
		t.Fatalf("Add peer2: %v", err)
	}
	s.syncObserverPullTargets()
	waitFor(t, 2*time.Second, "pull of peer2 after sync", func() bool {
		st, ok := op.col.Last("peer2")
		return ok && !st.LastAttemptAt.IsZero()
	})

	// A deleted peer drops out of the target set after a sync.
	if err := s.serverStore.Delete("peer1"); err != nil {
		t.Fatalf("Delete peer1: %v", err)
	}
	s.syncObserverPullTargets()
	names := map[string]bool{}
	for _, tgt := range op.targets() {
		names[tgt.Name] = true
	}
	if names["peer1"] {
		t.Error("deleted peer1 is still in the target set")
	}
	if !names["peer2"] {
		t.Error("surviving peer2 dropped out of the target set")
	}
}

func TestObserverPullFetch_TargetsUseEntryAndGlobalDefaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(statsOK))
	defer srv.Close()

	// Entry with explicit interval/timeout.
	s := newObserverPullServer(t, srv.URL, "token-abc", func(e *multiserver.Entry) {
		e.ObserverPullIntervalS = 12
		e.ObserverPullTimeoutS = 3
	})
	// A second peer that leaves both at 0 and inherits the globals.
	e2 := &multiserver.Entry{
		Name:         "peer0",
		URL:          srv.URL,
		Token:        "token-0",
		Enabled:      true,
		Federated:    true,
		ObserverPull: true,
		Capabilities: []string{federation.CapObserversRead},
	}
	if err := s.serverStore.Add(e2); err != nil {
		t.Fatalf("Add peer0: %v", err)
	}
	// A third peer opted out — must never be a target.
	e3 := &multiserver.Entry{
		Name:         "peer3",
		URL:          srv.URL,
		Token:        "token-3",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{federation.CapObserversRead},
	}
	if err := s.serverStore.Add(e3); err != nil {
		t.Fatalf("Add peer3: %v", err)
	}

	op := &observerPull{s: s}
	targets := op.targets()
	byName := map[string]observer.PullTarget{}
	for _, tgt := range targets {
		byName[tgt.Name] = tgt
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2 (peer1, peer0); have %v", len(targets), byName)
	}
	t1, ok := byName["peer1"]
	if !ok {
		t.Fatal("peer1 missing from targets")
	}
	if t1.Interval != 12*time.Second || t1.Timeout != 3*time.Second {
		t.Errorf("peer1 Interval/Timeout = %s/%s, want 12s/3s", t1.Interval, t1.Timeout)
	}
	t0, ok := byName["peer0"]
	if !ok {
		t.Fatal("peer0 missing from targets")
	}
	def := config.DefaultObserverPullConfig()
	if t0.Interval != time.Duration(def.DefaultIntervalS)*time.Second {
		t.Errorf("peer0 Interval = %s, want global default %s", t0.Interval, time.Duration(def.DefaultIntervalS)*time.Second)
	}
	if t0.Timeout != time.Duration(def.DefaultTimeoutS)*time.Second {
		t.Errorf("peer0 Timeout = %s, want global default %s", t0.Timeout, time.Duration(def.DefaultTimeoutS)*time.Second)
	}
}

func pullCfg(enabled bool) *config.Config {
	c := &config.Config{}
	c.Observer.Pull.Enabled = enabled
	return c
}

func TestObserverPullTargets_GlobalGate(t *testing.T) {
	cases := []struct {
		name       string
		global     bool
		optedIn    bool
		wantTarget bool
		wantReason bool
	}{
		{"global off, peer opted in", false, true, false, true},
		{"global on, peer opted in", true, true, true, false},
		{"global on, peer not opted in", true, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requests int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requests, 1)
				statsOK(w, r)
			}))
			defer srv.Close()
			s := newObserverPullServer(t, srv.URL, "tok", func(e *multiserver.Entry) { e.ObserverPull = tc.optedIn })
			s.cfg.Observer.Pull.Enabled = tc.global
			op := &observerPull{s: s}
			got := op.targets()
			if (len(got) == 1) != tc.wantTarget || len(got) > 1 {
				t.Fatalf("targets = %v, wantTarget=%v", got, tc.wantTarget)
			}
			if !tc.wantTarget && atomic.LoadInt32(&requests) != 0 {
				t.Errorf("requests = %d, want 0", requests)
			}
			for _, row := range s.pullStatusRows() {
				_, has := row["reason"]
				if has != tc.wantReason {
					t.Errorf("row reason present=%v, want %v (%v)", has, tc.wantReason, row)
				}
				if has && row["reason"] != ObserverPullDisabledReason {
					t.Errorf("reason = %v", row["reason"])
				}
			}
			// Live read: flipping the flag changes the result without rebuilding.
			s.cfg.Observer.Pull.Enabled = !tc.global
			if again := op.targets(); tc.optedIn && (len(again) == 1) != !tc.global {
				t.Errorf("targets did not follow live config flip: %v", again)
			}
		})
	}
}
