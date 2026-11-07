package plugins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// attachProbePlugin builds a kind: service plugin whose connection points
// at the given base URL with a health path.
func attachProbePlugin(baseURL, healthPath string) *Plugin {
	return &Plugin{Manifest: Manifest{Name: "svc", Kind: KindService,
		Connection: &PluginConnection{BaseURL: baseURL, HealthPath: healthPath}}}
}

func attachProbeKind(name, kind string, conn *PluginConnection) *Plugin {
	return &Plugin{Manifest: Manifest{Name: name, Kind: kind, Connection: conn}}
}

func TestNewAttachHealthProbe(t *testing.T) {
	cases := []struct {
		name    string
		p       *Plugin
		wantErr string
	}{
		{"nil plugin", nil, "plugin is nil"},
		{"wrong kind", attachProbeKind("x", KindAutomation, nil), "not"},
		{"no connection", attachProbeKind("x", KindService, nil), "base_url"},
		{"no base url", attachProbeKind("x", KindService,
			&PluginConnection{HealthPath: "/h"}), "base_url"},
		{"no health path", attachProbeKind("x", KindService,
			&PluginConnection{BaseURL: "http://127.0.0.1:1"}), "health_path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewAttachHealthProbe(tc.p, nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}

	// Valid plugin: URL is base+path with slash normalization.
	p := attachProbePlugin("http://127.0.0.1:8090/", "/healthz")
	a, err := NewAttachHealthProbe(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.url != "http://127.0.0.1:8090/healthz" {
		t.Fatalf("url = %q", a.url)
	}
	if got := a.Status(); got != AttachHealthUnknown {
		t.Fatalf("initial status = %q, want unknown", got)
	}

	// A connection auth token that cannot be resolved is a construction error.
	bad := attachProbePlugin("http://127.0.0.1:8090", "/healthz")
	bad.Connection.Auth = &PluginAuth{Type: "bearer", Token: "${secret:missing-secret}"}
	if _, err := NewAttachHealthProbe(bad, &mockPluginStore{data: map[string]string{}}); err == nil {
		t.Fatal("expected an error for an unresolvable auth token")
	}
}

func TestAttachHealthProbe_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("path = %q, want /healthz", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","version":"1.4.2"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	a, err := NewAttachHealthProbe(attachProbePlugin(srv.URL, "/healthz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.check(ctx)

	rep := a.LastCheck()
	if rep.Status != AttachHealthUp {
		t.Fatalf("status = %q, want up", rep.Status)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d", rep.StatusCode)
	}
	if rep.LastCheck.IsZero() || rep.Latency < 0 {
		t.Fatalf("last check = %v, latency = %v", rep.LastCheck, rep.Latency)
	}
	if rep.LastError != "" {
		t.Fatalf("last error = %q", rep.LastError)
	}
	if a.Version() != "1.4.2" {
		t.Fatalf("version = %q, want 1.4.2", a.Version())
	}
}

func TestAttachHealthProbe_UpNoVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	a, err := NewAttachHealthProbe(attachProbePlugin(srv.URL, "/healthz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	a.check(context.Background())

	if a.Status() != AttachHealthUp {
		t.Fatalf("status = %q, want up", a.Status())
	}
	if got := a.Version(); got != "" {
		t.Fatalf("version = %q, want empty (response carried no version)", got)
	}
}

func TestAttachHealthProbe_DownRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // port closed → connection refused

	a, err := NewAttachHealthProbe(attachProbePlugin(url, "/healthz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	a.check(context.Background())

	rep := a.LastCheck()
	if rep.Status != AttachHealthDown {
		t.Fatalf("status = %q, want down", rep.Status)
	}
	if rep.StatusCode != 0 {
		t.Fatalf("status code = %d, want 0 for a transport failure", rep.StatusCode)
	}
	if rep.LastError == "" {
		t.Fatal("last error is empty, want the transport error recorded")
	}
}

func TestAttachHealthProbe_DownTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return // client went away (timed out); let Close proceed
		case <-time.After(2 * time.Second):
			w.Write([]byte(`{}`)) //nolint:errcheck
		}
	}))
	defer srv.Close()

	a, err := NewAttachHealthProbe(attachProbePlugin(srv.URL, "/healthz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	a.client.Timeout = 100 * time.Millisecond
	a.check(context.Background())

	rep := a.LastCheck()
	if rep.Status != AttachHealthDown {
		t.Fatalf("status = %q, want down", rep.Status)
	}
	if rep.LastError == "" || !strings.Contains(strings.ToLower(rep.LastError), "timeout") {
		t.Fatalf("last error = %q, want a timeout", rep.LastError)
	}
}

func TestAttachHealthProbe_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"version":"9.9.9"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	a, err := NewAttachHealthProbe(attachProbePlugin(srv.URL, "/healthz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	a.check(context.Background())

	rep := a.LastCheck()
	if rep.Status != AttachHealthDown {
		t.Fatalf("status = %q, want down", rep.Status)
	}
	if rep.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want 503", rep.StatusCode)
	}
	if !strings.Contains(rep.LastError, "503") {
		t.Fatalf("last error = %q, want the 503 response named", rep.LastError)
	}
}

func TestAttachHealthProbe_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"version":"2.0.0"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	store := &mockPluginStore{data: map[string]string{"svc-token": "sekret"}}
	p := attachProbePlugin(srv.URL, "/healthz")
	p.Connection.Auth = &PluginAuth{Type: "bearer", Token: "${secret:svc-token}"}

	a, err := NewAttachHealthProbe(p, store)
	if err != nil {
		t.Fatal(err)
	}
	a.check(context.Background())

	if gotAuth != "Bearer sekret" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer sekret")
	}
}

func TestAttachHealthProbe_RunStopsOnCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`)) //nolint:errcheck
	}))
	defer srv.Close()

	a, err := NewAttachHealthProbe(attachProbePlugin(srv.URL, "/healthz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Interval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()

	// Let at least one probe land, then cancel and require the loop to stop.
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	if a.Status() != AttachHealthUp {
		t.Fatalf("status = %q, want up after probes ran", a.Status())
	}
}

func TestAttachHealthProbe_RunUsesDefaultInterval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`)) //nolint:errcheck
	}))
	defer srv.Close()

	a, err := NewAttachHealthProbe(attachProbePlugin(srv.URL, "/healthz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Interval left zero: Run must use DefaultAttachHealthInterval, and the
	// immediate first probe must still happen.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()
	time.Sleep(150 * time.Millisecond) // well inside the 5s default interval
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	if a.Status() != AttachHealthUp {
		t.Fatalf("status = %q, want up after the initial probe", a.Status())
	}
}

func TestAttachHealthProbe_CheckCancelledContextRecordsNothing(t *testing.T) {
	a, err := NewAttachHealthProbe(attachProbePlugin("http://127.0.0.1:1", "/healthz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.check(ctx) // request fails with cancellation; that is not a finding
	if a.Status() != AttachHealthUnknown {
		t.Fatalf("status = %q, want unknown (cancellation must not record down)", a.Status())
	}
}

func TestAttachHealthProbe_RunCancelledBeforeStart(t *testing.T) {
	a, err := NewAttachHealthProbe(attachProbePlugin("http://127.0.0.1:1", "/healthz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return for an already-cancelled context")
	}
	if a.Status() != AttachHealthUnknown {
		t.Fatalf("status = %q, want unknown (no probe ran)", a.Status())
	}
}
