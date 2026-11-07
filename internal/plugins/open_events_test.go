// Open service events on the plugin service views — api.go,
// registry_services.go, registry_attach.go, supervisor.go,
// attach_health.go.
//
// The card's "N open service events — Events →" link renders from the
// service view's open_events count. That count is computed at render
// time by a counter wired in by the daemon (cmd/datawatch/main.go) and
// reports the active conditions on the process-wide service event
// recorder for the plugin: service_down, token_rejected,
// enrichment_backlog_growing, model_node_unreachable. Zero is omitted
// so a healthy plugin's view carries no field and the card renders no
// link.

package plugins

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
)

// openEventCount is the same counting logic the daemon wires in main.go:
// one per active condition the recorder holds for the plugin.
func openEventCount(rec *ServiceEventRecorder, name string) int {
	down, token, backlog, node := rec.Active(name, "")
	open := 0
	if down {
		open++
	}
	if token {
		open++
	}
	if backlog {
		open++
	}
	if node {
		open++
	}
	return open
}

// quietRecorder builds a recorder that raises no real alerts and writes
// no audit entries (both sinks are dummies).
func quietRecorder() *ServiceEventRecorder {
	return NewServiceEventRecorder(ServiceEventOptions{
		Alert: func(level alerts.Level, title, body string) {},
		Audit: func(action, subject string, _ map[string]any) {},
	})
}

// TestAPI_View_OpenEventsAbsentByDefault covers the healthy default:
// without a counter wired, and with a clean recorder behind a wired
// counter, the service view carries no open_events field — in both
// lifecycle modes.
func TestAPI_View_OpenEventsAbsentByDefault(t *testing.T) {
	srv := healthServer(t, "1.0.0")
	pAttach := statusPlugin("attach", LifecycleAttachOnly)
	pAttach.Connection.BaseURL = srv.URL

	r := &Registry{byName: map[string]*Plugin{"attach": pAttach}}
	pr := registerAttachProbe(t, r, "attach", pAttach)
	pr.check(context.Background())

	rec := quietRecorder()
	api := NewAPI(r)
	api.SetOpenEventsCounter(func(name string) int { return openEventCount(rec, name) })

	svcA := decodeServiceView(t, api.view(pAttach))
	requireAbsent(t, svcA, "open_events")

	// No counter wired at all (the pre-wiring state) also sends nothing.
	svcNone := decodeServiceView(t, NewAPI(r).view(pAttach))
	requireAbsent(t, svcNone, "open_events")

	sup := statusPlugin("sup", LifecycleManaged)
	sup.Lifecycle.Entry = "echo"
	sup.Dir = t.TempDir()
	rSup := &Registry{
		byName:      map[string]*Plugin{"sup": sup},
		managed:     map[string]bool{"sup": true},
		supervisors: map[string]*ServiceSupervisor{},
	}
	s, err := NewServiceSupervisor(sup, nil)
	if err != nil {
		t.Fatalf("NewServiceSupervisor: %v", err)
	}
	s.state = StateHealthy
	s.lastProbeOK = true
	rSup.supervisors["sup"] = s
	svcM := decodeServiceView(t, api.view(sup))
	requireAbsent(t, svcM, "open_events")
}

// TestAPI_View_OpenEventsPresent covers the failure path: three failing
// probes raise service_down on the recorder (the default threshold) and
// the service view then reports open_events = 1 — exactly what the
// card's events link renders. Recovery clears the field.
func TestAPI_View_OpenEventsPresent(t *testing.T) {
	p := statusPlugin("svc", LifecycleAttachOnly)
	p.Connection.BaseURL = "http://127.0.0.1:1" // nothing listens here

	r := &Registry{byName: map[string]*Plugin{"svc": p}}
	pr := registerAttachProbe(t, r, "svc", p)

	rec := quietRecorder()
	// The exact wiring the attach manager installs: each completed
	// check feeds the recorder through the OnResult hook.
	pr.OnResult = func(healthy bool, _ int) { rec.RecordHealthProbe("svc", healthy) }
	api := NewAPI(r)
	api.SetOpenEventsCounter(func(name string) int { return openEventCount(rec, name) })

	svc := decodeServiceView(t, api.view(p))
	requireAbsent(t, svc, "open_events")

	// Three consecutive failing probes (the default threshold) raise
	// service_down, and the view reports exactly one open event.
	for i := 0; i < DefaultDownFailureThreshold; i++ {
		pr.check(context.Background())
	}
	svc = decodeServiceView(t, api.view(p))
	requireKey(t, svc, "open_events", 1)

	// Recovery clears the condition and the field drops out again.
	rec.RecordHealthProbe("svc", true)
	svc = decodeServiceView(t, api.view(p))
	requireAbsent(t, svc, "open_events")
}

// TestRegistry_HealthObserverFeedsEventRecorder covers the daemon chain
// end to end: a live attach probe → OnResult hook → registry observer →
// service event recorder → the API counter, with both failing and
// healthy endpoints registered at the same time.
func TestRegistry_HealthObserverFeedsEventRecorder(t *testing.T) {
	pDown := statusPlugin("down", LifecycleAttachOnly)
	pDown.Connection.BaseURL = "http://127.0.0.1:1"
	up := healthServer(t, "1.0.0")
	pUp := statusPlugin("up", LifecycleAttachOnly)
	pUp.Connection.BaseURL = up.URL

	r := &Registry{
		byName:  map[string]*Plugin{"down": pDown, "up": pUp},
		managed: map[string]bool{},
	}

	rec := quietRecorder()
	var observerCalls atomic.Int64
	r.SetHealthObserver(func(name string, healthy bool, _ int) {
		observerCalls.Add(1)
		rec.RecordHealthProbe(name, healthy)
	})
	api := NewAPI(r)
	api.SetOpenEventsCounter(func(name string) int { return openEventCount(rec, name) })

	ctx := context.Background()
	r.StartAttachProbes(ctx)
	t.Cleanup(r.StopAttachProbes)

	// The "down" plugin fails every probe; three consecutive failures
	// (the default threshold) raise service_down. Drive checks directly
	// so the test does not depend on the probe interval; the probe it
	// runs is the real one the manager installed.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if rep, ok := r.AttachHealth("down"); ok && !rep.LastCheck.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the down probe never completed a check")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for i := 1; i < DefaultDownFailureThreshold; i++ {
		ap, ok := r.attachProbes["down"] // same-package access: the real probe
		if !ok {
			t.Fatal("attach probe for the down plugin disappeared")
		}
		ap.probe.check(context.Background())
	}

	if observerCalls.Load() == 0 {
		t.Fatal("the attach probes never fed the health observer")
	}
	svcDown := decodeServiceView(t, api.view(pDown))
	requireKey(t, svcDown, "open_events", 1)
	if downActive, _, _, _ := rec.Active("down", ""); !downActive {
		t.Error("service_down not active for the failing plugin after the threshold")
	}
	svcUp := decodeServiceView(t, api.view(pUp))
	requireAbsent(t, svcUp, "open_events")
}

// TestRegistry_HealthObserverTokenRejected covers the 401 path: the
// probe's HTTP status code reaches the recorder's token condition and
// counts as an open event for the owning plugin only.
func TestRegistry_HealthObserverTokenRejected(t *testing.T) {
	p := statusPlugin("svc", LifecycleAttachOnly)
	p.Connection.BaseURL = "http://127.0.0.1:1"
	r := &Registry{byName: map[string]*Plugin{"svc": p}}
	pr := registerAttachProbe(t, r, "svc", p)

	rec := quietRecorder()
	pr.OnResult = func(healthy bool, code int) {
		rec.RecordHealthProbe("svc", healthy)
		if code != 0 {
			rec.RecordHTTPStatus("svc", "probe", code)
		}
	}

	// A probe completing with a 401 (the transport succeeds, the
	// endpoint is hostile): the recorder raises token_rejected for
	// this plugin and the counter reports exactly one open event.
	rec.RecordHTTPStatus("svc", "probe", 401)
	if _, token, _, _ := rec.Active("svc", ""); !token {
		t.Fatal("token_rejected not active for the plugin after a 401")
	}
	if got := openEventCount(rec, "svc"); got != 1 {
		t.Fatalf("open event count = %d, want 1 (token only)", got)
	}

	// Another plugin sharing the recorder is unaffected.
	other := statusPlugin("other", LifecycleAttachOnly)
	other.Connection.BaseURL = "http://127.0.0.1:9"
	api := NewAPI(&Registry{byName: map[string]*Plugin{"other": other}})
	api.SetOpenEventsCounter(func(name string) int { return openEventCount(rec, name) })
	svcOther := decodeServiceView(t, api.view(other))
	requireAbsent(t, svcOther, "open_events")
}

// TestAttachHealthProbe_OnResult covers the hook contract: exactly one
// call per completed check with the outcome and HTTP status (a
// transport failure carries code 0), and no call when the check never
// ran because the context was already cancelled.
func TestAttachHealthProbe_OnResult(t *testing.T) {
	up := healthServer(t, "1.0.0")
	a, err := NewAttachHealthProbe(attachProbePlugin(up.URL, "/healthz"), nil)
	if err != nil {
		t.Fatalf("NewAttachHealthProbe: %v", err)
	}
	var got [][2]any
	a.OnResult = func(healthy bool, code int) { got = append(got, [2]any{healthy, code}) }

	a.check(context.Background())
	if len(got) != 1 || got[0][0] != true || got[0][1] != 200 {
		t.Fatalf("after an up check got %v, want one [true 200] call", got)
	}

	bad := attachProbePlugin("http://127.0.0.1:1", "/healthz")
	a2, err := NewAttachHealthProbe(bad, nil)
	if err != nil {
		t.Fatalf("NewAttachHealthProbe: %v", err)
	}
	var got2 [][2]any
	a2.OnResult = func(healthy bool, code int) { got2 = append(got2, [2]any{healthy, code}) }
	a2.check(context.Background())
	if len(got2) != 1 || got2[0][0] != false || got2[0][1] != 0 {
		t.Fatalf("after a transport failure got %v, want one [false 0] call", got2)
	}

	a3, err := NewAttachHealthProbe(attachProbePlugin("http://127.0.0.1:1", "/healthz"), nil)
	if err != nil {
		t.Fatalf("NewAttachHealthProbe: %v", err)
	}
	called := false
	a3.OnResult = func(bool, int) { called = true }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a3.check(ctx)
	if called {
		t.Fatal("OnResult was called for a check cancelled before it ran")
	}
}

// TestServiceSupervisor_OnProbe covers the managed-path hook: one call
// per recorded probe outcome, true for a healthy probe and false for a
// failing one.
func TestServiceSupervisor_OnProbe(t *testing.T) {
	p := statusPlugin("svc", LifecycleManaged)
	p.Lifecycle.Entry = "echo"
	p.Dir = t.TempDir()
	s, err := NewServiceSupervisor(p, nil)
	if err != nil {
		t.Fatalf("NewServiceSupervisor: %v", err)
	}
	var got []bool
	s.OnProbe = func(healthy bool) { got = append(got, healthy) }

	s.recordProbe(true, "1.0.0")
	s.recordProbe(false, "")
	if len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("OnProbe calls = %v, want [true false]", got)
	}
}
