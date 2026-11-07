// GET /api/plugins service views — api.go.
//
// Every kind: service plugin reports its lifecycle mode, address and
// health findings; fields with no value (no health data yet, service not
// up, never restarted) are omitted rather than sent as placeholders.

package plugins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// healthServer answers health probes with an optional version field.
func healthServer(t *testing.T, version string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if version == "" {
			w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
			return
		}
		w.Write([]byte(`{"status":"ok","version":"` + version + `"}`)) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv
}

func statusPlugin(name string, modes ...string) *Plugin {
	return &Plugin{
		Manifest: Manifest{
			Name:       name,
			Kind:       KindService,
			Lifecycle:  &PluginLifecycle{SupportedModes: modes},
			Connection: &PluginConnection{BaseURL: "http://127.0.0.1:9", HealthPath: "/healthz"},
		},
		Enabled: true,
	}
}

func newAPIRegistry(r *Registry) *API { return NewAPI(r) }

// decodeServiceView unmarshals a plugin view and returns the service
// object as a JSON map (or nil when absent).
func decodeServiceView(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var outer map[string]any
	if err := json.Unmarshal(b, &outer); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}
	svc, _ := outer["service"].(map[string]any)
	return svc
}

func requireKey(t *testing.T, svc map[string]any, key string, want any) {
	t.Helper()
	if svc == nil {
		t.Fatalf("no service object for key %q", key)
	}
	got, ok := svc[key]
	if !ok {
		t.Fatalf("service object missing %q: %v", key, svc)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("service.%s = %s, want %s (full: %v)", key, gotJSON, wantJSON, svc)
	}
}

func requireAbsent(t *testing.T, svc map[string]any, key string) {
	t.Helper()
	if svc == nil {
		return
	}
	if got, ok := svc[key]; ok {
		t.Errorf("service object should omit %q, got %v (full: %v)", key, got, svc)
	}
}

// registerAttachProbe puts a probe (with its controls) into the registry
// as if StartAttachProbes had launched it for name.
func registerAttachProbe(t *testing.T, r *Registry, name string, p *Plugin) *AttachHealthProbe {
	t.Helper()
	pr, err := NewAttachHealthProbe(p, nil)
	if err != nil {
		t.Fatalf("NewAttachHealthProbe: %v", err)
	}
	if r.attachProbes == nil {
		r.attachProbes = map[string]*attachProbe{}
	}
	r.attachProbes[name] = &attachProbe{
		probe:  pr,
		cancel: func() {},
		doneCh: func() (c chan struct{}) { c = make(chan struct{}); close(c); return }(),
	}
	return pr
}

// TestAPI_ServiceView_AttachOnlyWithHealth covers an attach-only service
// with health data: mode, address, version, uptime and the last probe
// outcome are present; managed-only fields are absent.
func TestAPI_ServiceView_AttachOnlyWithHealth(t *testing.T) {
	srv := healthServer(t, "9.9.9")
	p := statusPlugin("svc", LifecycleAttachOnly)
	p.Connection.BaseURL = srv.URL

	r := &Registry{byName: map[string]*Plugin{"svc": p}}
	pr := registerAttachProbe(t, r, "svc", p)
	pr.check(context.Background())
	if got := pr.Status(); got != AttachHealthUp {
		t.Fatalf("probe status = %q, want up", got)
	}

	svc := decodeServiceView(t, newAPIRegistry(r).view(p))
	requireKey(t, svc, "mode", "attach_only")
	requireKey(t, svc, "address", srv.URL)
	requireKey(t, svc, "version", "9.9.9")
	if up, ok := svc["uptime_seconds"].(float64); !ok || up < 0 || up > 60 {
		t.Errorf("uptime_seconds = %v, want a small non-negative number", svc["uptime_seconds"])
	}
	if lc, ok := svc["last_health_check"].(string); !ok || lc == "" {
		t.Errorf("last_health_check missing or empty: %v", svc)
	}
	requireKey(t, svc, "last_health_status", "up")
	requireAbsent(t, svc, "last_health_error")
	requireAbsent(t, svc, "state")
	requireAbsent(t, svc, "pid")
	requireAbsent(t, svc, "restarts")
}

// TestAPI_ServiceView_AttachOnlyDown covers a probe whose last check
// failed: the error is reported, uptime is gone.
func TestAPI_ServiceView_AttachOnlyDown(t *testing.T) {
	p := statusPlugin("svc", LifecycleAttachOnly)
	p.Connection.BaseURL = "http://127.0.0.1:1" // nothing listens here

	r := &Registry{byName: map[string]*Plugin{"svc": p}}
	pr := registerAttachProbe(t, r, "svc", p)
	pr.check(context.Background())
	if got := pr.Status(); got != AttachHealthDown {
		t.Fatalf("probe status = %q, want down", got)
	}

	svc := decodeServiceView(t, newAPIRegistry(r).view(p))
	requireKey(t, svc, "last_health_status", "down")
	requireAbsent(t, svc, "uptime_seconds")
	if errStr, _ := svc["last_health_error"].(string); errStr == "" {
		t.Errorf("last_health_error should be set for a failed probe: %v", svc)
	}
}

// TestAPI_ServiceView_NoHealthDataYet covers a service with no health
// findings at all: only mode and address are present.
func TestAPI_ServiceView_NoHealthDataYet(t *testing.T) {
	p := statusPlugin("svc")                             // no lifecycle block: attach-only by construction
	r := &Registry{byName: map[string]*Plugin{"svc": p}} // no probes running

	svc := decodeServiceView(t, newAPIRegistry(r).view(p))
	requireKey(t, svc, "mode", "attach_only")
	requireKey(t, svc, "address", "http://127.0.0.1:9")
	requireAbsent(t, svc, "version")
	requireAbsent(t, svc, "uptime_seconds")
	requireAbsent(t, svc, "last_health_check")
	requireAbsent(t, svc, "last_health_status")
	requireAbsent(t, svc, "last_health_error")
	requireAbsent(t, svc, "state")
	requireAbsent(t, svc, "pid")
	requireAbsent(t, svc, "restarts")
	requireAbsent(t, svc, "last_error")
}

// TestAPI_ServiceView_Managed covers a supervised service: the existing
// fields (state, pid, restarts) plus mode, address, version, uptime and
// the last health check outcome.
func TestAPI_ServiceView_Managed(t *testing.T) {
	p := statusPlugin("svc", LifecycleAttachOnly, LifecycleManaged)
	p.Lifecycle.Entry = "echo"
	p.Dir = t.TempDir()

	r := &Registry{
		byName:      map[string]*Plugin{"svc": p},
		managed:     map[string]bool{"svc": true},
		supervisors: map[string]*ServiceSupervisor{},
	}
	s, err := NewServiceSupervisor(p, nil)
	if err != nil {
		t.Fatalf("NewServiceSupervisor: %v", err)
	}
	s.state = StateHealthy
	s.pid = 4242
	s.restarts = 3
	s.startedAt = time.Now().Add(-90 * time.Second)
	s.version = "2.0.1"
	s.lastProbeAt = time.Now()
	s.lastProbeOK = true
	r.supervisors["svc"] = s

	svc := decodeServiceView(t, newAPIRegistry(r).view(p))
	requireKey(t, svc, "mode", "managed")
	requireKey(t, svc, "state", "healthy")
	requireKey(t, svc, "address", "http://127.0.0.1:9")
	requireKey(t, svc, "version", "2.0.1")
	if up, ok := svc["uptime_seconds"].(float64); !ok || up < 85 || up > 95 {
		t.Errorf("uptime_seconds = %v, want ~90", svc["uptime_seconds"])
	}
	requireKey(t, svc, "last_health_status", "up")
	if lc, ok := svc["last_health_check"].(string); !ok || lc == "" {
		t.Errorf("last_health_check missing or empty: %v", svc)
	}
	requireKey(t, svc, "pid", 4242)
	requireKey(t, svc, "restarts", 3)
	requireAbsent(t, svc, "last_error")
	requireAbsent(t, svc, "last_health_error")
}

// TestAPI_ServiceView_ManagedStopped confirms uptime is omitted once the
// service is no longer running even though it was started.
func TestAPI_ServiceView_ManagedStopped(t *testing.T) {
	p := statusPlugin("svc", LifecycleManaged)
	p.Lifecycle.Entry = "echo"
	p.Dir = t.TempDir()
	r := &Registry{
		byName:      map[string]*Plugin{"svc": p},
		managed:     map[string]bool{"svc": true},
		supervisors: map[string]*ServiceSupervisor{},
	}
	s, err := NewServiceSupervisor(p, nil)
	if err != nil {
		t.Fatalf("NewServiceSupervisor: %v", err)
	}
	s.state = StateStopped
	s.startedAt = time.Now().Add(-10 * time.Second)
	r.supervisors["svc"] = s

	svc := decodeServiceView(t, newAPIRegistry(r).view(p))
	requireKey(t, svc, "mode", "managed")
	requireKey(t, svc, "state", "stopped")
	requireAbsent(t, svc, "uptime_seconds")
}

// TestAPI_ServiceMode covers mode resolution: the operator's choice when
// both modes are supported, otherwise the single supported mode.
func TestAPI_ServiceMode(t *testing.T) {
	cases := []struct {
		name    string
		p       *Plugin
		managed bool
		want    string
	}{
		{"attach only", statusPlugin("a", LifecycleAttachOnly), false, "attach_only"},
		{"managed only", statusPlugin("m", LifecycleManaged), false, "managed"},
		{"both, opted in", statusPlugin("b1", LifecycleAttachOnly, LifecycleManaged), true, "managed"},
		{"both, not opted in", statusPlugin("b2", LifecycleAttachOnly, LifecycleManaged), false, "attach_only"},
		{"no lifecycle block", statusPlugin("n"), false, "attach_only"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Registry{
				byName:  map[string]*Plugin{tc.p.Name: tc.p},
				managed: map[string]bool{tc.p.Name: tc.managed},
				started: true,
			}
			if got := newAPIRegistry(r).serviceMode(tc.p); got != tc.want {
				t.Errorf("serviceMode = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAPI_ServiceView_NonService covers a non-service-kind plugin: it
// must encode without a service object.
func TestAPI_ServiceView_NonService(t *testing.T) {
	p := &Plugin{Manifest: Manifest{Name: "auto", Kind: KindAutomation}, Enabled: true}
	r := &Registry{byName: map[string]*Plugin{"auto": p}}

	v := newAPIRegistry(r).view(p)
	if _, ok := v.(*Plugin); !ok {
		t.Fatalf("non-service plugin view = %T, want *Plugin", v)
	}
}

// TestRegistry_AttachProbesLifecycle covers the probe set following
// enable and managed-mode changes, and full shutdown.
func TestRegistry_AttachProbesLifecycle(t *testing.T) {
	up := healthServer(t, "1.2.3")

	pUp := statusPlugin("up", LifecycleAttachOnly)
	pUp.Connection.BaseURL = up.URL
	pDown := statusPlugin("down", LifecycleAttachOnly)
	pDown.Connection.BaseURL = "http://127.0.0.1:1"
	pManaged := statusPlugin("managed", LifecycleManaged) // never attach-eligible

	r := &Registry{
		byName:  map[string]*Plugin{"up": pUp, "down": pDown, "managed": pManaged},
		managed: map[string]bool{},
	}
	ctx := context.Background()
	r.StartAttachProbes(ctx)
	t.Cleanup(r.StopAttachProbes)

	if _, ok := r.AttachHealth("up"); !ok {
		t.Fatal("no attach probe for eligible plugin up")
	}
	if _, ok := r.AttachHealth("down"); !ok {
		t.Fatal("no attach probe for eligible plugin down")
	}
	if _, ok := r.AttachHealth("managed"); ok {
		t.Fatal("attach probe for a managed-only plugin")
	}

	// Wait for at least one completed check on the up plugin.
	deadline := time.Now().Add(5 * time.Second)
	for {
		rep, _ := r.AttachHealth("up")
		if !rep.LastCheck.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("attach probe never completed a check")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rep, _ := r.AttachHealth("up"); rep.Version != "1.2.3" {
		t.Errorf("probe version = %q, want 1.2.3", rep.Version)
	}

	// Disabling a plugin stops its probe.
	if !r.SetEnabled("up", false) {
		t.Fatal("SetEnabled(up, false) = false")
	}
	if _, ok := r.AttachHealth("up"); ok {
		t.Fatal("attach probe still running for disabled plugin")
	}
	if !r.SetEnabled("up", true) {
		t.Fatal("SetEnabled(up, true) = false")
	}
	if _, ok := r.AttachHealth("up"); !ok {
		t.Fatal("attach probe not restarted for re-enabled plugin")
	}

	// Stopping everything leaves no probes behind.
	r.StopAttachProbes()
	if _, ok := r.AttachHealth("down"); ok {
		t.Fatal("attach probe survived StopAttachProbes")
	}
}

// TestService_ProbeVersion covers version extraction from a health
// response and probe-outcome recording.
func TestService_ProbeVersion(t *testing.T) {
	srv := healthServer(t, "3.1.4")

	p := statusPlugin("svc", LifecycleManaged)
	p.Lifecycle.Entry = "echo"
	p.Dir = t.TempDir()
	p.Connection.BaseURL = srv.URL
	s, err := NewServiceSupervisor(p, nil)
	if err != nil {
		t.Fatalf("NewServiceSupervisor: %v", err)
	}
	if s.healthURL == "" {
		t.Fatal("supervisor has no health URL")
	}

	version, err := s.probe(context.Background())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if version != "3.1.4" {
		t.Errorf("version = %q, want 3.1.4", version)
	}
	s.recordProbe(true, version)
	st := s.Status()
	if st.Version != "3.1.4" || st.LastHealthStatus != "up" || st.LastHealthCheck.IsZero() {
		t.Errorf("Status = %+v, want version 3.1.4 / up / set check time", st)
	}

	// A failing probe records a down outcome.
	s.healthURL = "http://127.0.0.1:1/healthz"
	if _, err := s.probe(context.Background()); err == nil {
		t.Fatal("probe against a dead endpoint succeeded")
	}
	s.recordProbe(false, "")
	st = s.Status()
	if st.LastHealthStatus != "down" || st.Version != "3.1.4" {
		t.Errorf("after down probe: status = %+v, want last=down, version retained", st)
	}
}
