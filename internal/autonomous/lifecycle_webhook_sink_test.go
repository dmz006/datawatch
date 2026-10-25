// BL427 (phase 5) — tests for the lifecycle outbound webhook sink. Receivers
// are real HTTP servers (net/http/httptest); the clock, the sleep, the jitter
// and the secret resolver are injected, so every retry sequence is exact and
// no test waits for a real backoff.

package autonomous

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

const (
	webhookTestSecret = "s3cr3t-value-0123456789"
	webhookTestRef    = "${secret:lifecycle-hook}"
	webhookTestQuery  = "token=QUERYTOKEN42"
)

var webhookTestBase = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

type webhookAuditRec struct {
	action  string
	details map[string]any
}

type webhookAlertRec struct{ level, title, body string }

type webhookHarness struct {
	t *testing.T

	mu       sync.Mutex
	set      WebhookSettings
	audits   []webhookAuditRec
	alerts   []webhookAlertRec
	sleeps   []time.Duration
	resolved int
	secrets  map[string]string
	resolve  func(ref string) (string, error)

	tick    atomic.Int64
	onSleep func(time.Duration)
	sink    *WebhookSink
}

// newWebhookHarness builds an enabled sink that allow-lists 127.0.0.1 and uses
// the real guarded HTTP client. mutate adjusts the settings; jitter 0.5 makes
// every delay exactly the curve value.
func newWebhookHarness(t *testing.T, mutate func(*WebhookSettings)) *webhookHarness {
	t.Helper()
	h := &webhookHarness{
		t:       t,
		set:     WebhookSettings{Enabled: true, AllowHosts: []string{"127.0.0.1"}},
		secrets: map[string]string{webhookTestRef: webhookTestSecret},
	}
	if mutate != nil {
		mutate(&h.set)
	}
	h.sink = NewWebhookSink(WebhookSinkConfig{
		Settings: func() WebhookSettings {
			h.mu.Lock()
			defer h.mu.Unlock()
			s := h.set
			s.AllowHosts = append([]string(nil), s.AllowHosts...)
			return s
		},
		ResolveSecret: func(ref string) (string, error) {
			h.mu.Lock()
			h.resolved++
			fn, v, ok := h.resolve, h.secrets[ref], false
			_, ok = h.secrets[ref]
			h.mu.Unlock()
			if fn != nil {
				return fn(ref)
			}
			if !ok {
				return "", fmt.Errorf("no such secret %q", ref)
			}
			return v, nil
		},
		Audit: func(action string, details map[string]any) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.audits = append(h.audits, webhookAuditRec{action, details})
		},
		RaiseAlert: func(level, title, body string) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.alerts = append(h.alerts, webhookAlertRec{level, title, body})
		},
		Sleep: func(d time.Duration) {
			h.mu.Lock()
			h.sleeps = append(h.sleeps, d)
			fn := h.onSleep
			h.mu.Unlock()
			if fn != nil {
				fn(d)
			}
		},
		Now:      func() time.Time { return webhookTestBase.Add(time.Duration(h.tick.Add(1)) * time.Second) },
		Jitter:   func() float64 { return 0.5 },
		Hostname: func() string { return "sender-host" },
	})
	return h
}

func (h *webhookHarness) setSettings(fn func(*WebhookSettings)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fn(&h.set)
}

func (h *webhookHarness) deliver(ctx context.Context, target string) error {
	return h.sink.Deliver(ctx, webhookTestSub(target), webhookTestEvent())
}

func (h *webhookHarness) snapshot() (audits []webhookAuditRec, alerts []webhookAlertRec, sleeps []time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]webhookAuditRec(nil), h.audits...),
		append([]webhookAlertRec(nil), h.alerts...),
		append([]time.Duration(nil), h.sleeps...)
}

func (h *webhookHarness) sleepsNow() []time.Duration {
	_, _, s := h.snapshot()
	return s
}

func webhookTestSub(target string) Subscription {
	return Subscription{
		ID:     "sub-1",
		Owner:  "operator",
		Filter: LifecycleFilter{Kinds: []string{"prd.completed"}},
		Sink:   SubscriptionSink{Type: "webhook", Target: target, SecretRef: webhookTestRef},
	}
}

func webhookTestEvent() LifecycleEvent {
	return LifecycleEvent{
		ID: "ev-1", At: webhookTestBase, Kind: "prd.completed", Level: LevelPRD,
		PRDID: "prd-1", OldStatus: "running", NewStatus: "completed", Host: "event-host",
	}
}

// auditOf returns the audit records with the given action, in order.
func auditOf(recs []webhookAuditRec, action string) []map[string]any {
	var out []map[string]any
	for _, r := range recs {
		if r.action == action {
			out = append(out, r.details)
		}
	}
	return out
}

func outcomes(recs []map[string]any) []string {
	var out []string
	for _, d := range recs {
		out = append(out, fmt.Sprint(d["outcome"]))
	}
	return out
}

func equalDurations(a, b []time.Duration) bool {
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

// hookReq is one request a fake receiver saw.
type hookReq struct {
	header http.Header
	body   []byte
	uri    string
}

// hookServer is a fake receiver. statuses is answered in order and the last
// entry repeats; handler, when set, replaces that behaviour.
type hookServer struct {
	*httptest.Server
	mu       sync.Mutex
	reqs     []hookReq
	statuses []int
	respHdr  map[string]string
	handler  func(idx int, w http.ResponseWriter, r *http.Request)
	release  chan struct{}
}

func newHookServer(t *testing.T, statuses ...int) *hookServer {
	t.Helper()
	if len(statuses) == 0 {
		statuses = []int{200}
	}
	s := &hookServer{statuses: statuses, release: make(chan struct{})}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	// Registered after Close, so it runs first and frees any blocked handler.
	t.Cleanup(func() {
		select {
		case <-s.release:
		default:
			close(s.release)
		}
	})
	return s
}

func (s *hookServer) serve(w http.ResponseWriter, r *http.Request) {
	buf := new(strings.Builder)
	_, _ = bufCopy(buf, r)
	s.mu.Lock()
	idx := len(s.reqs)
	s.reqs = append(s.reqs, hookReq{header: r.Header.Clone(), body: []byte(buf.String()), uri: r.URL.RequestURI()})
	statuses, respHdr, handler := s.statuses, s.respHdr, s.handler
	s.mu.Unlock()
	if handler != nil {
		handler(idx, w, r)
		return
	}
	for k, v := range respHdr {
		w.Header().Set(k, v)
	}
	status := statuses[len(statuses)-1]
	if idx < len(statuses) {
		status = statuses[idx]
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte("receiver says no secrets here"))
}

func bufCopy(dst *strings.Builder, r *http.Request) (int64, error) {
	b := make([]byte, 4096)
	var n int64
	for {
		m, err := r.Body.Read(b)
		dst.Write(b[:m])
		n += int64(m)
		if err != nil {
			return n, nil
		}
	}
}

func (s *hookServer) requests() []hookReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]hookReq(nil), s.reqs...)
}

func (s *hookServer) hits() int { return len(s.requests()) }

// target returns the receiver URL with a path and a query string (which must
// never reach an audit record, an alert or an error).
func (s *hookServer) target() string { return s.URL + "/hook?" + webhookTestQuery }

// refusedTarget returns a URL whose port nothing listens on.
func refusedTarget(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return "http://" + addr + "/hook?" + webhookTestQuery
}

// leakCheck fails when any recorded string carries the secret, the query
// string or URL userinfo.
func (h *webhookHarness) leakCheck(extra ...string) {
	h.t.Helper()
	audits, alerts, _ := h.snapshot()
	var all []string
	for _, a := range audits {
		raw, err := json.Marshal(a.details)
		if err != nil {
			h.t.Fatalf("audit details do not marshal: %v", err)
		}
		all = append(all, a.action, string(raw))
	}
	for _, a := range alerts {
		all = append(all, a.level, a.title, a.body)
	}
	all = append(all, extra...)
	for _, s := range all {
		for _, bad := range []string{webhookTestSecret, webhookTestQuery, "QUERYTOKEN42", "token=", "hunter2", "user:", "/hook"} {
			if strings.Contains(s, bad) {
				h.t.Errorf("recorded text leaks %q: %s", bad, s)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Delivery: success, signature, headers, envelope
// ---------------------------------------------------------------------------

func TestWebhookSinkDeliversSignedOnFirstAttempt(t *testing.T) {
	srv := newHookServer(t, 200)
	h := newWebhookHarness(t, nil)

	if err := h.deliver(context.Background(), srv.target()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	reqs := srv.requests()
	if len(reqs) != 1 {
		t.Fatalf("hits = %d, want 1", len(reqs))
	}
	r := reqs[0]
	if r.uri != "/hook?"+webhookTestQuery {
		t.Errorf("request URI = %q", r.uri)
	}
	for k, want := range map[string]string{
		"Content-Type":     "application/json",
		"User-Agent":       "datawatch-lifecycle/1",
		WebhookEventHeader: "prd.completed",
	} {
		if got := r.header.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	sig := r.header.Get(WebhookSignatureHeader)
	if !strings.HasPrefix(sig, "t=") || !strings.Contains(sig, ",v1=") {
		t.Fatalf("signature header = %q", sig)
	}
	tsStr := strings.TrimPrefix(strings.SplitN(sig, ",", 2)[0], "t=")
	ts, _ := strconv.ParseInt(tsStr, 10, 64)
	if err := VerifyWebhookSignature([]byte(webhookTestSecret), sig, r.body, time.Unix(ts, 0), 0); err != nil {
		t.Errorf("signature does not verify: %v", err)
	}
	if err := VerifyWebhookSignature([]byte(webhookTestSecret), sig, append([]byte("x"), r.body...), time.Unix(ts, 0), 0); err == nil {
		t.Error("signature verified for a different body")
	}
	if err := VerifyWebhookSignature([]byte("another-secret"), sig, r.body, time.Unix(ts, 0), 0); err == nil {
		t.Error("signature verified with the wrong secret")
	}

	var env WebhookEnvelope
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("body is not the envelope: %v", err)
	}
	id := r.header.Get(WebhookDeliveryHeader)
	if _, err := hex.DecodeString(id); err != nil || len(id) != 32 {
		t.Errorf("delivery id %q is not 32 hex characters", id)
	}
	if env.Version != WebhookEnvelopeVersion || env.DeliveryID != id || env.Host != "sender-host" ||
		env.SubscriptionID != "sub-1" || env.Event.ID != "ev-1" || env.Event.Kind != "prd.completed" ||
		env.Event.PRDID != "prd-1" || env.Truncated {
		t.Errorf("envelope = %+v", env)
	}
	if !env.SentAt.After(webhookTestBase) || env.SentAt.Location() != time.UTC {
		t.Errorf("sent_at = %v, want a UTC time from the injected clock", env.SentAt)
	}

	audits, alerts, sleeps := h.snapshot()
	if len(alerts) != 0 || len(sleeps) != 0 {
		t.Errorf("alerts=%v sleeps=%v, want none", alerts, sleeps)
	}
	if len(audits) != 1 || audits[0].action != audit.ActionLifecycleWebhookDelivery {
		t.Fatalf("audits = %+v, want one delivery record", audits)
	}
	d := audits[0].details
	if d["subscription_id"] != "sub-1" || d["delivery_id"] != id || d["attempt"] != 1 || d["max_attempts"] != DefaultWebhookMaxAttempts ||
		d["host"] != strings.TrimPrefix(srv.URL, "http://") || d["status_code"] != 200 || d["outcome"] != "delivered" || d["error"] != "" {
		t.Errorf("audit details = %+v", d)
	}
	if _, ok := d["duration_ms"].(int64); !ok {
		t.Errorf("duration_ms = %#v, want int64", d["duration_ms"])
	}
	h.leakCheck()
}

func TestWebhookSinkOverTLSWithFactoryClient(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	h := newWebhookHarness(t, nil)
	h.sink.cfg.NewClient = func(allow []string, timeout time.Duration) *http.Client { return ts.Client() }

	if err := h.deliver(context.Background(), ts.URL+"/hook"); err != nil {
		t.Fatalf("Deliver over https: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", hits.Load())
	}
}

func TestWebhookSinkNilFactoryResultFallsBackToGuardedClient(t *testing.T) {
	srv := newHookServer(t, 200)
	h := newWebhookHarness(t, nil)
	h.sink.cfg.NewClient = func([]string, time.Duration) *http.Client { return nil }
	if err := h.deliver(context.Background(), srv.target()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if srv.hits() != 1 {
		t.Errorf("hits = %d, want 1", srv.hits())
	}
}

// ---------------------------------------------------------------------------
// Delivery: retry sequence
// ---------------------------------------------------------------------------

func TestWebhookSinkRetriesThenDelivers(t *testing.T) {
	srv := newHookServer(t, 500, 500, 200)
	h := newWebhookHarness(t, nil)

	if err := h.deliver(context.Background(), srv.target()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	reqs := srv.requests()
	if len(reqs) != 3 {
		t.Fatalf("hits = %d, want 3", len(reqs))
	}
	if _, _, sleeps := h.snapshot(); !equalDurations(sleeps, []time.Duration{time.Second, 4 * time.Second}) {
		t.Errorf("sleeps = %v, want [1s 4s]", sleeps)
	}
	id := reqs[0].header.Get(WebhookDeliveryHeader)
	sigs := map[string]bool{}
	for i, r := range reqs {
		if got := r.header.Get(WebhookDeliveryHeader); got != id {
			t.Errorf("attempt %d delivery id = %q, want %q", i+1, got, id)
		}
		if string(r.body) != string(reqs[0].body) {
			t.Errorf("attempt %d body changed between retries", i+1)
		}
		sig := r.header.Get(WebhookSignatureHeader)
		sigs[sig] = true
		tsv, _ := strconv.ParseInt(strings.TrimPrefix(strings.SplitN(sig, ",", 2)[0], "t="), 10, 64)
		if err := VerifyWebhookSignature([]byte(webhookTestSecret), sig, r.body, time.Unix(tsv, 0), 0); err != nil {
			t.Errorf("attempt %d signature: %v", i+1, err)
		}
	}
	if len(sigs) != 3 {
		t.Errorf("each attempt must be signed afresh with the current time; got %d distinct signatures", len(sigs))
	}
	var env WebhookEnvelope
	_ = json.Unmarshal(reqs[2].body, &env)
	if env.DeliveryID != id {
		t.Errorf("envelope delivery id %q differs from header %q", env.DeliveryID, id)
	}

	audits, alerts, _ := h.snapshot()
	if len(alerts) != 0 {
		t.Errorf("alerts = %v, want none for a delivery that succeeded", alerts)
	}
	rec := auditOf(audits, audit.ActionLifecycleWebhookDelivery)
	if got := strings.Join(outcomes(rec), ","); got != "retry,retry,delivered" {
		t.Errorf("outcomes = %s", got)
	}
	for i, d := range rec {
		if d["attempt"] != i+1 || d["delivery_id"] != id {
			t.Errorf("record %d = %+v", i, d)
		}
	}
	if rec[0]["status_code"] != 500 || rec[0]["error"] != "receiver answered 500" || rec[2]["status_code"] != 200 {
		t.Errorf("retry records = %+v", rec)
	}
	if len(auditOf(audits, audit.ActionLifecycleWebhookFailed)) != 0 {
		t.Error("a delivered event must not write a final-failure record")
	}
	h.leakCheck()
}

func TestWebhookSinkExactBackoffSequenceAndOneAlertWhenAllAttemptsFail(t *testing.T) {
	srv := newHookServer(t, 503)
	h := newWebhookHarness(t, nil)

	err := h.deliver(context.Background(), srv.target())
	if !errors.Is(err, ErrWebhookDelivery) {
		t.Fatalf("Deliver = %v, want ErrWebhookDelivery", err)
	}
	if srv.hits() != 5 {
		t.Errorf("hits = %d, want 5", srv.hits())
	}
	audits, alerts, sleeps := h.snapshot()
	if want := []time.Duration{time.Second, 4 * time.Second, 16 * time.Second, 64 * time.Second}; !equalDurations(sleeps, want) {
		t.Errorf("sleeps = %v, want %v", sleeps, want)
	}
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want exactly 1: %+v", len(alerts), alerts)
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	if alerts[0].level != "error" || !strings.Contains(alerts[0].body, "sub-1") || !strings.Contains(alerts[0].body, host) {
		t.Errorf("alert = %+v, want level error naming the subscription and %s", alerts[0], host)
	}
	rec := auditOf(audits, audit.ActionLifecycleWebhookDelivery)
	if got := strings.Join(outcomes(rec), ","); got != "retry,retry,retry,retry,failed" {
		t.Errorf("attempt outcomes = %s", got)
	}
	final := auditOf(audits, audit.ActionLifecycleWebhookFailed)
	if len(final) != 1 {
		t.Fatalf("final records = %d, want 1", len(final))
	}
	if final[0]["outcome"] != "failed" || final[0]["attempt"] != 5 || final[0]["status_code"] != 503 ||
		final[0]["host"] != host || final[0]["subscription_id"] != "sub-1" {
		t.Errorf("final record = %+v", final[0])
	}
	h.leakCheck(err.Error())
}

func TestWebhookSinkBackoffJitterCapAndSettings(t *testing.T) {
	cases := []struct {
		name   string
		jitter float64
		mutate func(*WebhookSettings)
		want   []time.Duration
	}{
		{"jitter low end", 0, nil, []time.Duration{800 * time.Millisecond, 3200 * time.Millisecond, 12800 * time.Millisecond, 51200 * time.Millisecond}},
		{"jitter high end", 1, nil, []time.Duration{1200 * time.Millisecond, 4800 * time.Millisecond, 19200 * time.Millisecond, 76800 * time.Millisecond}},
		{"jitter out of range is clamped", 7, nil, []time.Duration{1200 * time.Millisecond, 4800 * time.Millisecond, 19200 * time.Millisecond, 76800 * time.Millisecond}},
		{"jitter negative is clamped", -3, nil, []time.Duration{800 * time.Millisecond, 3200 * time.Millisecond, 12800 * time.Millisecond, 51200 * time.Millisecond}},
		{"capped at backoff max", 0.5, func(s *WebhookSettings) { s.BackoffMax = 8 * time.Second }, []time.Duration{time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}},
		{"custom base", 0.5, func(s *WebhookSettings) { s.BackoffBase = 10 * time.Millisecond; s.BackoffMax = time.Minute }, []time.Duration{10 * time.Millisecond, 40 * time.Millisecond, 160 * time.Millisecond, 640 * time.Millisecond}},
		{"three attempts", 0.5, func(s *WebhookSettings) { s.MaxAttempts = 3 }, []time.Duration{time.Second, 4 * time.Second}},
		{"one attempt never sleeps", 0.5, func(s *WebhookSettings) { s.MaxAttempts = 1 }, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newHookServer(t, 500)
			h := newWebhookHarness(t, c.mutate)
			h.sink.cfg.Jitter = func() float64 { return c.jitter }
			if err := h.deliver(context.Background(), srv.target()); !errors.Is(err, ErrWebhookDelivery) {
				t.Fatalf("Deliver = %v", err)
			}
			if sleeps := h.sleepsNow(); !equalDurations(sleeps, c.want) {
				t.Errorf("sleeps = %v, want %v", sleeps, c.want)
			}
			if srv.hits() != len(c.want)+1 {
				t.Errorf("hits = %d, want %d", srv.hits(), len(c.want)+1)
			}
		})
	}
}

func TestWebhookSinkRetriesTransientStatuses(t *testing.T) {
	for _, code := range []int{408, 429, 500, 502, 503, 504, 599} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			srv := newHookServer(t, code, 200)
			h := newWebhookHarness(t, nil)
			if err := h.deliver(context.Background(), srv.target()); err != nil {
				t.Fatalf("Deliver: %v", err)
			}
			if srv.hits() != 2 {
				t.Errorf("hits = %d, want 2", srv.hits())
			}
			if sleeps := h.sleepsNow(); !equalDurations(sleeps, []time.Duration{time.Second}) {
				t.Errorf("sleeps = %v, want [1s]", sleeps)
			}
		})
	}
}

func TestWebhookSinkHonoursRetryAfterOn429(t *testing.T) {
	future := webhookTestBase.Add(10 * time.Minute).UTC().Format(http.TimeFormat)
	cases := []struct {
		name       string
		retryAfter string
		want       time.Duration
	}{
		{"longer than the next delay", "30", 30 * time.Second},
		{"capped", "99999", webhookRetryAfterCap},
		{"shorter than the next delay", "0", time.Second},
		{"not a number", "soon", time.Second},
		{"missing", "", time.Second},
		{"negative", "-5", time.Second},
		{"past date", webhookTestBase.Add(-time.Hour).UTC().Format(http.TimeFormat), time.Second},
		{"far future date is capped", future, webhookRetryAfterCap},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newHookServer(t, 429, 200)
			if c.retryAfter != "" {
				srv.respHdr = map[string]string{"Retry-After": c.retryAfter}
			}
			h := newWebhookHarness(t, nil)
			if err := h.deliver(context.Background(), srv.target()); err != nil {
				t.Fatalf("Deliver: %v", err)
			}
			if sleeps := h.sleepsNow(); !equalDurations(sleeps, []time.Duration{c.want}) {
				t.Errorf("sleeps = %v, want [%v]", sleeps, c.want)
			}
		})
	}
}

func TestWebhookSinkRetryAfterIgnoredOutside429(t *testing.T) {
	srv := newHookServer(t, 503, 200)
	srv.respHdr = map[string]string{"Retry-After": "90"}
	h := newWebhookHarness(t, nil)
	if err := h.deliver(context.Background(), srv.target()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if sleeps := h.sleepsNow(); !equalDurations(sleeps, []time.Duration{time.Second}) {
		t.Errorf("sleeps = %v, want [1s]", sleeps)
	}
}

// ---------------------------------------------------------------------------
// Delivery: permanent failures
// ---------------------------------------------------------------------------

func TestWebhookSinkTerminalStatusesAreNotRetried(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 410, 413, 422, 301, 302, 307, 308, 304} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			srv := newHookServer(t, code, 200)
			h := newWebhookHarness(t, nil)

			err := h.deliver(context.Background(), srv.target())
			if !errors.Is(err, ErrWebhookDelivery) {
				t.Fatalf("Deliver = %v, want ErrWebhookDelivery", err)
			}
			if srv.hits() != 1 {
				t.Errorf("hits = %d, want exactly 1", srv.hits())
			}
			audits, alerts, sleeps := h.snapshot()
			if len(sleeps) != 0 {
				t.Errorf("sleeps = %v, want none", sleeps)
			}
			if len(alerts) != 1 {
				t.Errorf("alerts = %d, want 1", len(alerts))
			}
			rec := auditOf(audits, audit.ActionLifecycleWebhookDelivery)
			if len(rec) != 1 || rec[0]["outcome"] != "failed" || rec[0]["attempt"] != 1 || rec[0]["status_code"] != code {
				t.Errorf("attempt records = %+v", rec)
			}
			if final := auditOf(audits, audit.ActionLifecycleWebhookFailed); len(final) != 1 {
				t.Errorf("final records = %d, want 1", len(final))
			}
			h.leakCheck(err.Error())
		})
	}
}

func TestWebhookSinkDoesNotFollowRedirects(t *testing.T) {
	landing := newHookServer(t, 200)
	srv := newHookServer(t, 302)
	srv.handler = func(idx int, w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, landing.URL+"/landing", http.StatusFound)
	}
	h := newWebhookHarness(t, nil)

	err := h.deliver(context.Background(), srv.target())
	if !errors.Is(err, ErrWebhookDelivery) {
		t.Fatalf("Deliver = %v", err)
	}
	if srv.hits() != 1 || landing.hits() != 0 {
		t.Errorf("origin hits = %d, redirect target hits = %d; want 1 and 0", srv.hits(), landing.hits())
	}
	if !strings.Contains(err.Error(), "redirects are not followed") {
		t.Errorf("error = %v", err)
	}
}

func TestWebhookSinkRedirectNotFollowedEvenForFactoryClient(t *testing.T) {
	landing := newHookServer(t, 200)
	srv := newHookServer(t, 302)
	srv.handler = func(idx int, w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, landing.URL, http.StatusFound)
	}
	h := newWebhookHarness(t, nil)
	h.sink.cfg.NewClient = func([]string, time.Duration) *http.Client { return &http.Client{} } // default policy would follow
	if err := h.deliver(context.Background(), srv.target()); !errors.Is(err, ErrWebhookDelivery) {
		t.Fatalf("Deliver = %v", err)
	}
	if landing.hits() != 0 {
		t.Errorf("a factory client followed the redirect (%d hits)", landing.hits())
	}
}

func TestWebhookSinkTimeoutIsRetriedThenDropped(t *testing.T) {
	srv := newHookServer(t)
	srv.handler = func(idx int, w http.ResponseWriter, r *http.Request) {
		select {
		case <-srv.release:
		case <-r.Context().Done():
		}
	}
	h := newWebhookHarness(t, func(s *WebhookSettings) { s.Timeout = 50 * time.Millisecond; s.MaxAttempts = 2 })

	err := h.deliver(context.Background(), srv.target())
	if !errors.Is(err, ErrWebhookDelivery) || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("Deliver = %v, want a timeout failure", err)
	}
	if srv.hits() != 2 {
		t.Errorf("hits = %d, want 2 (a timeout is retried)", srv.hits())
	}
	audits, alerts, sleeps := h.snapshot()
	if !equalDurations(sleeps, []time.Duration{time.Second}) || len(alerts) != 1 {
		t.Errorf("sleeps=%v alerts=%d", sleeps, len(alerts))
	}
	rec := auditOf(audits, audit.ActionLifecycleWebhookDelivery)
	if got := strings.Join(outcomes(rec), ","); got != "retry,failed" || rec[0]["error"] != "timeout" || rec[0]["status_code"] != 0 {
		t.Errorf("records = %v %+v", got, rec)
	}
	h.leakCheck(err.Error())
}

func TestWebhookSinkConnectionRefusedIsRetriedThenDropped(t *testing.T) {
	target := refusedTarget(t)
	h := newWebhookHarness(t, func(s *WebhookSettings) { s.MaxAttempts = 3 })

	err := h.deliver(context.Background(), target)
	if !errors.Is(err, ErrWebhookDelivery) || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("Deliver = %v, want connection refused", err)
	}
	audits, alerts, sleeps := h.snapshot()
	if !equalDurations(sleeps, []time.Duration{time.Second, 4 * time.Second}) {
		t.Errorf("sleeps = %v", sleeps)
	}
	if len(alerts) != 1 {
		t.Errorf("alerts = %d, want 1", len(alerts))
	}
	rec := auditOf(audits, audit.ActionLifecycleWebhookDelivery)
	if got := strings.Join(outcomes(rec), ","); got != "retry,retry,failed" {
		t.Errorf("outcomes = %s", got)
	}
	h.leakCheck(err.Error())
}

// ---------------------------------------------------------------------------
// Delivery: secret, disabled, refused targets
// ---------------------------------------------------------------------------

func TestWebhookSinkSecretResolutionFailureMakesNoRequest(t *testing.T) {
	cases := []struct {
		name  string
		ref   string
		setup func(*webhookHarness)
		want  string
	}{
		{"unknown reference", "${secret:missing}", nil, "could not be resolved"},
		{"resolver error", webhookTestRef, func(h *webhookHarness) {
			h.resolve = func(string) (string, error) { return "", errors.New("vault is locked") }
		}, "vault is locked"},
		{"empty value", webhookTestRef, func(h *webhookHarness) { h.secrets[webhookTestRef] = "" }, "empty value"},
		{"no reference on the subscription", "", nil, "no signing secret reference"},
		{"no resolver configured", webhookTestRef, func(h *webhookHarness) { h.sink.cfg.ResolveSecret = nil }, "no secret resolver"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newHookServer(t, 200)
			h := newWebhookHarness(t, nil)
			if c.setup != nil {
				c.setup(h)
			}
			sub := webhookTestSub(srv.target())
			sub.Sink.SecretRef = c.ref

			err := h.sink.Deliver(context.Background(), sub, webhookTestEvent())
			if !errors.Is(err, ErrWebhookDelivery) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Deliver = %v, want a failure containing %q", err, c.want)
			}
			if srv.hits() != 0 {
				t.Errorf("hits = %d, want 0: a failed secret resolution must not POST", srv.hits())
			}
			audits, alerts, sleeps := h.snapshot()
			if len(sleeps) != 0 || len(alerts) != 1 {
				t.Errorf("sleeps=%v alerts=%d", sleeps, len(alerts))
			}
			rec := auditOf(audits, audit.ActionLifecycleWebhookDelivery)
			if len(rec) != 1 || rec[0]["outcome"] != "failed" || rec[0]["attempt"] != 0 {
				t.Errorf("records = %+v", rec)
			}
			if len(auditOf(audits, audit.ActionLifecycleWebhookFailed)) != 1 {
				t.Error("want one final-failure record")
			}
			h.leakCheck(err.Error())
		})
	}
}

func TestWebhookSinkResolvesSecretOnEveryDelivery(t *testing.T) {
	srv := newHookServer(t, 200)
	h := newWebhookHarness(t, nil)
	if err := h.deliver(context.Background(), srv.target()); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.secrets[webhookTestRef] = "rotated-secret"
	h.mu.Unlock()
	if err := h.deliver(context.Background(), srv.target()); err != nil {
		t.Fatal(err)
	}
	reqs := srv.requests()
	if len(reqs) != 2 || h.resolved != 2 {
		t.Fatalf("hits=%d resolved=%d, want 2 and 2", len(reqs), h.resolved)
	}
	sig := reqs[1].header.Get(WebhookSignatureHeader)
	tsv, _ := strconv.ParseInt(strings.TrimPrefix(strings.SplitN(sig, ",", 2)[0], "t="), 10, 64)
	if err := VerifyWebhookSignature([]byte("rotated-secret"), sig, reqs[1].body, time.Unix(tsv, 0), 0); err != nil {
		t.Errorf("second delivery is not signed with the rotated secret: %v", err)
	}
}

func TestWebhookSinkDisabledDoesNothing(t *testing.T) {
	srv := newHookServer(t, 200)
	for name, build := range map[string]func() *webhookHarness{
		"enabled false": func() *webhookHarness {
			return newWebhookHarness(t, func(s *WebhookSettings) { s.Enabled = false })
		},
		"no settings function": func() *webhookHarness {
			h := newWebhookHarness(t, nil)
			h.sink.cfg.Settings = nil
			return h
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := build()
			if err := h.deliver(context.Background(), srv.target()); err != nil {
				t.Fatalf("Deliver = %v, want nil", err)
			}
			audits, alerts, sleeps := h.snapshot()
			if srv.hits() != 0 || len(audits) != 0 || len(alerts) != 0 || len(sleeps) != 0 || h.resolved != 0 {
				t.Errorf("hits=%d audits=%d alerts=%d sleeps=%d resolved=%d, want all zero",
					srv.hits(), len(audits), len(alerts), len(sleeps), h.resolved)
			}
		})
	}
	// The default settings ship disabled.
	if DefaultWebhookSettings().Enabled {
		t.Error("DefaultWebhookSettings().Enabled must be false")
	}
}

func TestWebhookSinkRefusesTargetsThatFailTheGuard(t *testing.T) {
	srv := newHookServer(t, 200)
	port := strings.TrimPrefix(srv.URL, "http://127.0.0.1")
	cases := []struct {
		name   string
		target string
		allow  []string
	}{
		{"host not on the allow-list", srv.URL + "/hook?" + webhookTestQuery, []string{"hooks.example.net"}},
		{"empty allow-list", srv.URL + "/hook?" + webhookTestQuery, nil},
		{"allow-listed host with another port", srv.URL + "/hook?" + webhookTestQuery, []string{"127.0.0.1:1"}},
		{"private address, not allow-listed", "https://10.1.2.3/hook?" + webhookTestQuery, []string{"hooks.example.net"}},
		{"loopback, not allow-listed", "https://127.0.0.1" + port + "/hook?" + webhookTestQuery, []string{"hooks.example.net"}},
		{"cloud metadata address even when allow-listed", "http://169.254.169.254/latest?" + webhookTestQuery, []string{"169.254.169.254"}},
		{"obfuscated loopback", "http://2130706433" + port + "/hook?" + webhookTestQuery, []string{"127.0.0.1"}},
		{"userinfo", "https://user:hunter2@hooks.example.net/hook?" + webhookTestQuery, []string{"hooks.example.net"}},
		{"plain http to a host that is not allow-listed", "http://hooks.example.net/hook?" + webhookTestQuery, []string{"other.example.net"}},
		{"ftp scheme", "ftp://hooks.example.net/hook?" + webhookTestQuery, []string{"hooks.example.net"}},
		{"no host", "https:///hook?" + webhookTestQuery, []string{"hooks.example.net"}},
		{"empty target", "", []string{"hooks.example.net"}},
		{"unparseable", "http://[::1/hook?" + webhookTestQuery, []string{"::1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newWebhookHarness(t, func(s *WebhookSettings) { s.AllowHosts = c.allow })
			err := h.deliver(context.Background(), c.target)
			if !errors.Is(err, ErrWebhookDelivery) {
				t.Fatalf("Deliver = %v, want a refusal", err)
			}
			if srv.hits() != 0 || h.resolved != 0 {
				t.Errorf("hits=%d resolved=%d: a refused target must not reach the network or the vault", srv.hits(), h.resolved)
			}
			audits, alerts, sleeps := h.snapshot()
			rec := auditOf(audits, audit.ActionLifecycleWebhookDelivery)
			if len(rec) != 1 || rec[0]["outcome"] != "refused" || rec[0]["attempt"] != 0 || rec[0]["error"] == "" {
				t.Errorf("records = %+v, want one refused record", rec)
			}
			final := auditOf(audits, audit.ActionLifecycleWebhookFailed)
			if len(final) != 1 || final[0]["outcome"] != "refused" {
				t.Errorf("final = %+v", final)
			}
			if len(alerts) != 1 || alerts[0].level != "error" || len(sleeps) != 0 {
				t.Errorf("alerts=%v sleeps=%v", alerts, sleeps)
			}
			h.leakCheck(err.Error())
		})
	}
}

func TestWebhookSinkDNSRebindingIsRefusedAtDialTime(t *testing.T) {
	// The name passes the allow-list check, but now resolves to the cloud
	// metadata address: the dial-time re-check must refuse it, not retry it.
	h := newWebhookHarness(t, func(s *WebhookSettings) { s.AllowHosts = []string{"hooks.example.test"} })
	h.sink.cfg.NewClient = func(allow []string, timeout time.Duration) *http.Client {
		r := &fakeWebhookResolver{fn: func(int, string) ([]net.IPAddr, error) { return ipAddrs("169.254.169.254"), nil }}
		return newWebhookHTTPClient(allow, timeout, r)
	}

	err := h.deliver(context.Background(), "http://hooks.example.test/hook?"+webhookTestQuery)
	if !errors.Is(err, ErrWebhookDelivery) {
		t.Fatalf("Deliver = %v", err)
	}
	audits, alerts, sleeps := h.snapshot()
	rec := auditOf(audits, audit.ActionLifecycleWebhookDelivery)
	if len(rec) != 1 || rec[0]["outcome"] != "refused" || rec[0]["attempt"] != 1 {
		t.Errorf("records = %+v, want one refused attempt", rec)
	}
	if len(sleeps) != 0 || len(alerts) != 1 {
		t.Errorf("sleeps=%v alerts=%d: a guard refusal is permanent", sleeps, len(alerts))
	}
	h.leakCheck(err.Error())
}

func TestWebhookSinkAllowListedPrivateAddressIsReachable(t *testing.T) {
	// 127.0.0.1 is loopback, i.e. blocked unless explicitly allow-listed. The
	// other tests rely on this; assert it directly for both spellings.
	srv := newHookServer(t, 200)
	hostPort := strings.TrimPrefix(srv.URL, "http://")
	for _, allow := range [][]string{{"127.0.0.1"}, {hostPort}} {
		h := newWebhookHarness(t, func(s *WebhookSettings) { s.AllowHosts = allow })
		if err := h.deliver(context.Background(), srv.target()); err != nil {
			t.Errorf("allow %v: Deliver = %v", allow, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Delivery: context, settings, payload, concurrency
// ---------------------------------------------------------------------------

func TestWebhookSinkContextCancelStopsRetries(t *testing.T) {
	srv := newHookServer(t, 500)
	h := newWebhookHarness(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.onSleep = func(time.Duration) { cancel() }

	err := h.deliver(ctx, srv.target())
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrWebhookDelivery) {
		t.Fatalf("Deliver = %v, want the context error and not a delivery failure", err)
	}
	if srv.hits() != 1 {
		t.Errorf("hits = %d, want 1: no attempt after the cancel", srv.hits())
	}
	audits, alerts, sleeps := h.snapshot()
	if len(alerts) != 0 {
		t.Errorf("alerts = %v, want none for an abandoned delivery", alerts)
	}
	if len(sleeps) != 1 {
		t.Errorf("sleeps = %v", sleeps)
	}
	rec := auditOf(audits, audit.ActionLifecycleWebhookDelivery)
	if got := strings.Join(outcomes(rec), ","); got != "retry,skipped" {
		t.Errorf("outcomes = %s", got)
	}
	if len(auditOf(audits, audit.ActionLifecycleWebhookFailed)) != 0 {
		t.Error("an abandoned delivery is not a final failure")
	}
}

func TestWebhookSinkContextAlreadyCancelledSendsNothing(t *testing.T) {
	srv := newHookServer(t, 200)
	h := newWebhookHarness(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := h.deliver(ctx, srv.target())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Deliver = %v", err)
	}
	audits, _, _ := h.snapshot()
	rec := auditOf(audits, audit.ActionLifecycleWebhookDelivery)
	if srv.hits() != 0 || len(rec) != 1 || rec[0]["outcome"] != "skipped" || rec[0]["attempt"] != 0 {
		t.Errorf("hits=%d records=%+v", srv.hits(), rec)
	}
}

func TestWebhookSinkCancelDuringAttemptIsNotRetried(t *testing.T) {
	srv := newHookServer(t)
	started := make(chan struct{}, 1)
	srv.handler = func(idx int, w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-srv.release:
		case <-r.Context().Done():
		}
	}
	h := newWebhookHarness(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-started; cancel() }()

	err := h.deliver(ctx, srv.target())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Deliver = %v", err)
	}
	if srv.hits() != 1 || len(h.sleepsNow()) != 0 {
		t.Errorf("hits=%d sleeps=%v", srv.hits(), h.sleepsNow())
	}
}

func TestWebhookSinkDefaultWaitIsInterruptedByCancel(t *testing.T) {
	srv := newHookServer(t, 500)
	h := newWebhookHarness(t, func(s *WebhookSettings) { s.BackoffBase = time.Hour; s.BackoffMax = time.Hour })
	h.sink.cfg.Sleep = nil // exercise the real timer-and-context wait
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- h.deliver(ctx, srv.target()) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Deliver = %v, want the context deadline", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Deliver did not return after the context ended")
	}
}

func TestWebhookSinkDefaultWaitElapses(t *testing.T) {
	srv := newHookServer(t, 500, 200)
	h := newWebhookHarness(t, func(s *WebhookSettings) { s.BackoffBase = 5 * time.Millisecond; s.BackoffMax = time.Second })
	h.sink.cfg.Sleep = nil
	if err := h.deliver(context.Background(), srv.target()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if srv.hits() != 2 {
		t.Errorf("hits = %d, want 2", srv.hits())
	}
}

func TestWebhookSinkAuditAndAlertsNeverLeakSecretOrQuery(t *testing.T) {
	// Every failure path, with a secret, a query string and (for the refused
	// case) userinfo in play. Every recorded string is scanned.
	servers := map[string]string{
		"500s":       newHookServer(t, 500).target(),
		"400":        newHookServer(t, 400).target(),
		"refused":    refusedTarget(t),
		"redirect":   func() string { s := newHookServer(t, 302); return s.target() }(),
		"userinfo":   "https://user:hunter2@hooks.example.net/hook?" + webhookTestQuery,
		"not listed": "https://elsewhere.example.net/hook?" + webhookTestQuery,
	}
	for name, target := range servers {
		t.Run(name, func(t *testing.T) {
			h := newWebhookHarness(t, func(s *WebhookSettings) { s.MaxAttempts = 3 })
			err := h.deliver(context.Background(), target)
			if err == nil {
				t.Fatal("expected a failure")
			}
			audits, alerts, _ := h.snapshot()
			if len(audits) == 0 || len(alerts) != 1 {
				t.Fatalf("audits=%d alerts=%d", len(audits), len(alerts))
			}
			h.leakCheck(err.Error())
		})
	}
	// A failing resolver whose own error echoes the secret: the secret is
	// scrubbed from everything the sink writes.
	h := newWebhookHarness(t, nil)
	h.resolve = func(string) (string, error) {
		return webhookTestSecret, fmt.Errorf("lookup returned %s", webhookTestSecret)
	}
	err := h.deliver(context.Background(), newHookServer(t, 200).target())
	if err == nil {
		t.Fatal("expected a failure")
	}
	h.leakCheck(err.Error())
}

func TestWebhookSinkScrubsSecretFromHTTPStackErrors(t *testing.T) {
	// A factory client whose transport fails with an error that embeds the
	// secret and the full URL, as net/http's url.Error does.
	h := newWebhookHarness(t, func(s *WebhookSettings) { s.MaxAttempts = 2 })
	h.sink.cfg.NewClient = func([]string, time.Duration) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("proxy failed for %s using key %s", r.URL.String(), webhookTestSecret)
		})}
	}
	err := h.deliver(context.Background(), "http://127.0.0.1:9/hook?"+webhookTestQuery)
	if !errors.Is(err, ErrWebhookDelivery) {
		t.Fatalf("Deliver = %v", err)
	}
	if !strings.Contains(err.Error(), redactedText) {
		t.Errorf("error = %q, want redaction markers", err)
	}
	h.leakCheck(err.Error())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWebhookSinkSettingsChangeBetweenDeliveriesTakesEffect(t *testing.T) {
	srv := newHookServer(t, 500)
	h := newWebhookHarness(t, nil)
	target := srv.target()

	// 1. Enabled, five attempts.
	if err := h.deliver(context.Background(), target); !errors.Is(err, ErrWebhookDelivery) {
		t.Fatalf("first: %v", err)
	}
	if srv.hits() != 5 {
		t.Fatalf("hits after first = %d, want 5", srv.hits())
	}

	// 2. Attempts lowered live.
	h.setSettings(func(s *WebhookSettings) { s.MaxAttempts = 2 })
	if err := h.deliver(context.Background(), target); !errors.Is(err, ErrWebhookDelivery) {
		t.Fatalf("second: %v", err)
	}
	if srv.hits() != 7 {
		t.Fatalf("hits after second = %d, want 7", srv.hits())
	}

	// 3. Host removed from the allow-list live: refused, no new request.
	h.setSettings(func(s *WebhookSettings) { s.AllowHosts = []string{"hooks.example.net"} })
	if err := h.deliver(context.Background(), target); !errors.Is(err, ErrWebhookDelivery) {
		t.Fatalf("third: %v", err)
	}
	if srv.hits() != 7 {
		t.Fatalf("hits after third = %d, want 7", srv.hits())
	}

	// 4. Disabled live: nothing at all.
	before, _, _ := h.snapshot()
	h.setSettings(func(s *WebhookSettings) { s.Enabled = false; s.AllowHosts = []string{"127.0.0.1"} })
	if err := h.deliver(context.Background(), target); err != nil {
		t.Fatalf("fourth: %v", err)
	}
	if after, _, _ := h.snapshot(); len(after) != len(before) || srv.hits() != 7 {
		t.Errorf("a disabled sink wrote records or sent a request")
	}

	// 5. Re-enabled with a timeout; the receiver now answers 200.
	srv.mu.Lock()
	srv.statuses = []int{200}
	srv.mu.Unlock()
	h.setSettings(func(s *WebhookSettings) { s.Enabled = true })
	if err := h.deliver(context.Background(), target); err != nil {
		t.Fatalf("fifth: %v", err)
	}
	if srv.hits() != 8 {
		t.Errorf("hits after fifth = %d, want 8", srv.hits())
	}
}

func TestWebhookSinkTruncatesOversizeEventAndRefusesTheHopeless(t *testing.T) {
	srv := newHookServer(t, 200)
	h := newWebhookHarness(t, nil)
	sub := webhookTestSub(srv.target())

	big := webhookTestEvent()
	big.Decision = &Decision{Kind: "run", Note: strings.Repeat("n", WebhookMaxBodyBytes)}
	big.Verdict = &GuardrailVerdict{Guardrail: "rules", Outcome: "pass", Summary: "ok"}
	if err := h.sink.Deliver(context.Background(), sub, big); err != nil {
		t.Fatalf("truncatable event: %v", err)
	}
	reqs := srv.requests()
	if len(reqs) != 1 || len(reqs[0].body) > WebhookMaxBodyBytes {
		t.Fatalf("hits=%d", len(reqs))
	}
	var env WebhookEnvelope
	if err := json.Unmarshal(reqs[0].body, &env); err != nil {
		t.Fatal(err)
	}
	if !env.Truncated || env.Event.Decision != nil || env.Event.Verdict != nil || env.Event.ID != "ev-1" {
		t.Errorf("envelope = %+v, want truncated with the decision and verdict removed", env)
	}
	// The body is signed after truncation.
	sig := reqs[0].header.Get(WebhookSignatureHeader)
	tsv, _ := strconv.ParseInt(strings.TrimPrefix(strings.SplitN(sig, ",", 2)[0], "t="), 10, 64)
	if err := VerifyWebhookSignature([]byte(webhookTestSecret), sig, reqs[0].body, time.Unix(tsv, 0), 0); err != nil {
		t.Errorf("truncated body signature: %v", err)
	}

	hopeless := webhookTestEvent()
	hopeless.OldStatus = strings.Repeat("x", WebhookMaxBodyBytes)
	err := h.sink.Deliver(context.Background(), sub, hopeless)
	if !errors.Is(err, ErrWebhookDelivery) || !strings.Contains(err.Error(), "over the") {
		t.Fatalf("hopeless event: %v", err)
	}
	if srv.hits() != 1 {
		t.Errorf("hits = %d: an over-cap payload must not be sent", srv.hits())
	}
}

func TestWebhookSinkConcurrentDeliveries(t *testing.T) {
	ok := newHookServer(t, 200)
	flaky := newHookServer(t, 500, 200)
	flaky.handler = func(idx int, w http.ResponseWriter, r *http.Request) {
		if idx%2 == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
	h := newWebhookHarness(t, func(s *WebhookSettings) { s.MaxAttempts = 50 })

	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers*2)
	for i := 0; i < workers; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			sub := webhookTestSub(ok.target())
			sub.ID = fmt.Sprintf("sub-ok-%d", i)
			ev := webhookTestEvent()
			ev.ID = fmt.Sprintf("ev-%d", i)
			errs <- h.sink.Deliver(context.Background(), sub, ev)
		}(i)
		go func(i int) {
			defer wg.Done()
			sub := webhookTestSub(flaky.target())
			sub.ID = fmt.Sprintf("sub-flaky-%d", i)
			errs <- h.sink.Deliver(context.Background(), sub, webhookTestEvent())
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Deliver: %v", err)
		}
	}
	if ok.hits() != workers {
		t.Errorf("ok hits = %d, want %d", ok.hits(), workers)
	}
	// Each delivery id is unique and constant across that delivery's retries.
	ids := map[string]int{}
	for _, r := range ok.requests() {
		ids[r.header.Get(WebhookDeliveryHeader)]++
	}
	if len(ids) != workers {
		t.Errorf("distinct delivery ids = %d, want %d", len(ids), workers)
	}
	audits, alerts, _ := h.snapshot()
	if len(alerts) != 0 {
		t.Errorf("alerts = %v", alerts)
	}
	delivered := 0
	for _, a := range auditOf(audits, audit.ActionLifecycleWebhookDelivery) {
		if a["outcome"] == "delivered" {
			delivered++
		}
	}
	if delivered != workers*2 {
		t.Errorf("delivered records = %d, want %d", delivered, workers*2)
	}
}

// ---------------------------------------------------------------------------
// Unit tests of the helpers
// ---------------------------------------------------------------------------

func TestWebhookSinkSettingsDefaults(t *testing.T) {
	d := DefaultWebhookSettings()
	if d.Enabled || d.MaxAttempts != 5 || d.BackoffBase != time.Second || d.BackoffMax != 64*time.Second || d.Timeout != 10*time.Second {
		t.Errorf("DefaultWebhookSettings = %+v", d)
	}
	got := WebhookSettings{Enabled: true, AllowHosts: []string{"a"}}.withDefaults()
	if got.MaxAttempts != 5 || got.BackoffBase != time.Second || got.BackoffMax != 64*time.Second || got.Timeout != 10*time.Second || !got.Enabled {
		t.Errorf("withDefaults = %+v", got)
	}
	kept := WebhookSettings{MaxAttempts: 2, BackoffBase: time.Millisecond, BackoffMax: time.Second, Timeout: time.Minute}.withDefaults()
	if kept.MaxAttempts != 2 || kept.BackoffBase != time.Millisecond || kept.BackoffMax != time.Second || kept.Timeout != time.Minute {
		t.Errorf("withDefaults overrode explicit values: %+v", kept)
	}
}

func TestWebhookSinkNewFillsSeams(t *testing.T) {
	s := NewWebhookSink(WebhookSinkConfig{})
	if s.cfg.Now == nil || s.cfg.Jitter == nil || s.cfg.NewClient == nil {
		t.Fatalf("seams not defaulted: %+v", s.cfg)
	}
	if j := s.cfg.Jitter(); j < 0 || j >= 1 {
		t.Errorf("default jitter = %v", j)
	}
	// No Settings: disabled, and no nil-function panics.
	if err := s.Deliver(context.Background(), webhookTestSub("https://x.example"), webhookTestEvent()); err != nil {
		t.Errorf("Deliver = %v", err)
	}
}

func TestWebhookSinkNilAuditAndAlertAreSafe(t *testing.T) {
	s := NewWebhookSink(WebhookSinkConfig{
		Settings: func() WebhookSettings {
			return WebhookSettings{Enabled: true, AllowHosts: []string{"hooks.example.net"}}
		},
		ResolveSecret: func(string) (string, error) { return "k", nil },
	})
	err := s.Deliver(context.Background(), webhookTestSub("https://elsewhere.example.net/x"), webhookTestEvent())
	if !errors.Is(err, ErrWebhookDelivery) {
		t.Errorf("Deliver = %v", err)
	}
}

func TestWebhookSinkHostnameSources(t *testing.T) {
	s := NewWebhookSink(WebhookSinkConfig{})
	osHost, _ := os.Hostname()
	if got := s.hostname(LifecycleEvent{Host: "ev"}); got != osHost && (osHost != "" || got != "ev") {
		t.Errorf("default hostname = %q, want os hostname %q", got, osHost)
	}
	s.cfg.Hostname = func() string { return "cfg" }
	if got := s.hostname(LifecycleEvent{Host: "ev"}); got != "cfg" {
		t.Errorf("hostname = %q, want cfg", got)
	}
	s.cfg.Hostname = func() string { return "" }
	if got := s.hostname(LifecycleEvent{Host: "ev"}); got != "ev" {
		t.Errorf("hostname = %q, want the event host", got)
	}
}

func TestWebhookSinkParseRetryAfter(t *testing.T) {
	now := webhookTestBase
	cases := map[string]time.Duration{
		"":      0,
		"  ":    0,
		"5":     5 * time.Second,
		" 7 ":   7 * time.Second,
		"0":     0,
		"-1":    0,
		"junk":  0,
		"121":   webhookRetryAfterCap,
		"99999": webhookRetryAfterCap,
		now.Add(45 * time.Second).UTC().Format(http.TimeFormat): 45 * time.Second,
		now.Add(-time.Minute).UTC().Format(http.TimeFormat):     0,
		now.Add(time.Hour).UTC().Format(http.TimeFormat):        webhookRetryAfterCap,
	}
	for in, want := range cases {
		if got := parseRetryAfter(in, now); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestWebhookSinkScrubAndHostHelpers(t *testing.T) {
	text := scrubWebhookText(`Post "https://u:pw@h.example/x?a=b&c=d": key=KEY failed; a=b&c=d; pw`,
		"https://u:pw@h.example/x?a=b&c=d", "KEY")
	for _, bad := range []string{"u:pw", "a=b&c=d", "KEY", "https://u:pw@h.example/x?a=b&c=d"} {
		if strings.Contains(text, bad) {
			t.Errorf("scrubbed text still contains %q: %s", bad, text)
		}
	}
	if got := scrubWebhookText("plain", "", ""); got != "plain" {
		t.Errorf("scrub of a clean string = %q", got)
	}
	if got := scrubWebhookText("::: x", "::bad url::", ""); got != "::: x" {
		t.Errorf("scrub with an unparseable target = %q", got)
	}

	for in, want := range map[string]string{
		"https://u:p@Hooks.Example:8443/a?b=c": "Hooks.Example:8443",
		"https://hooks.example/a":              "hooks.example",
		"":                                     "unknown",
		"not a url":                            "unknown",
		"http://[::1/x":                        "unknown",
		"/relative":                            "unknown",
	} {
		if got := webhookTargetHost(in); got != want {
			t.Errorf("webhookTargetHost(%q) = %q, want %q", in, got, want)
		}
	}

	if got := headerSafe("prd.completed\r\nX-Evil: 1\x00"); strings.ContainsAny(got, "\r\n\x00") {
		t.Errorf("headerSafe = %q", got)
	}
	a, b := newWebhookDeliveryID(), newWebhookDeliveryID()
	if len(a) != 32 || a == b {
		t.Errorf("delivery ids %q %q", a, b)
	}
}

func TestWebhookSinkErrTextClassifiesTimeouts(t *testing.T) {
	s := NewWebhookSink(WebhookSinkConfig{})
	run := &webhookRun{rawTarget: "https://h.example/x?q=1"}
	if got := s.errText(run, nil); got != "" {
		t.Errorf("errText(nil) = %q", got)
	}
	if got := s.errText(run, context.DeadlineExceeded); got != "timeout" {
		t.Errorf("errText(deadline) = %q", got)
	}
	if got := s.errText(run, timeoutErr{}); got != "timeout" {
		t.Errorf("errText(net timeout) = %q", got)
	}
	wrapped := &urlErrorForTest{}
	if got := s.errText(run, wrapped.err("https://h.example/x?q=1", errors.New("boom for https://h.example/x?q=1"))); strings.Contains(got, "q=1") {
		t.Errorf("errText leaked the query: %q", got)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

type urlErrorForTest struct{}

func (urlErrorForTest) err(u string, inner error) error {
	return &urlErr{op: "Post", url: u, err: inner}
}

// urlErr stands in for *url.Error without importing it twice in the test.
type urlErr struct {
	op, url string
	err     error
}

func (e *urlErr) Error() string { return e.op + ` "` + e.url + `": ` + e.err.Error() }
func (e *urlErr) Unwrap() error { return e.err }
