package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

// fakePluginsAPI records SetEnabled/SetManaged calls. Plugins named in
// managedOK declare managed lifecycle support.
type fakePluginsAPI struct {
	known     map[string]bool
	managedOK map[string]bool
	calls     []string
}

func (f *fakePluginsAPI) List() []any { return nil }
func (f *fakePluginsAPI) Get(name string) (any, bool) {
	return map[string]any{"name": name}, f.known[name]
}
func (f *fakePluginsAPI) Reload() error { return nil }
func (f *fakePluginsAPI) SetEnabled(name string, on bool) bool {
	if !f.known[name] {
		return false
	}
	if on {
		f.calls = append(f.calls, "enable:"+name)
	} else {
		f.calls = append(f.calls, "disable:"+name)
	}
	return true
}
func (f *fakePluginsAPI) SetManaged(name string, on bool) bool {
	if !f.known[name] || !f.managedOK[name] {
		return false
	}
	if on {
		f.calls = append(f.calls, "managed:"+name)
	} else {
		f.calls = append(f.calls, "unmanaged:"+name)
	}
	return true
}
func (f *fakePluginsAPI) Test(context.Context, string, string, map[string]any) (any, error) {
	return nil, nil
}
func (f *fakePluginsAPI) Install(string) error { return nil }

func newManagedPluginsServer(t *testing.T) (*Server, *fakePluginsAPI) {
	t.Helper()
	s := bl90Server(t)
	f := &fakePluginsAPI{
		known:     map[string]bool{"svc": true, "hook": true},
		managedOK: map[string]bool{"svc": true},
	}
	s.SetPluginsAPI(f)
	return s, f
}

func postPlugin(s *Server, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(http.MethodPost, path, nil)
	} else {
		req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	}
	rr := httptest.NewRecorder()
	s.handlePlugins(rr, req)
	return rr
}

func callsEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestPluginEnable_EmptyBodyKeepsBehavior(t *testing.T) {
	s, f := newManagedPluginsServer(t)
	rr := postPlugin(s, "/api/plugins/hook/enable", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if want := []string{"enable:hook"}; !callsEqual(f.calls, want) {
		t.Errorf("calls=%v want %v", f.calls, want)
	}
}

func TestPluginEnable_ManagedTrueOptsInBeforeEnable(t *testing.T) {
	s, f := newManagedPluginsServer(t)
	rr := postPlugin(s, "/api/plugins/svc/enable", `{"managed":true}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if want := []string{"managed:svc", "enable:svc"}; !callsEqual(f.calls, want) {
		t.Errorf("calls=%v want %v", f.calls, want)
	}
}

func TestPluginEnable_ManagedFalseOptsOut(t *testing.T) {
	s, f := newManagedPluginsServer(t)
	rr := postPlugin(s, "/api/plugins/svc/enable", `{"managed":false}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if want := []string{"unmanaged:svc", "enable:svc"}; !callsEqual(f.calls, want) {
		t.Errorf("calls=%v want %v", f.calls, want)
	}
}

func TestPluginEnable_BodyWithoutManagedKeepsBehavior(t *testing.T) {
	s, f := newManagedPluginsServer(t)
	rr := postPlugin(s, "/api/plugins/svc/enable", `{}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if want := []string{"enable:svc"}; !callsEqual(f.calls, want) {
		t.Errorf("calls=%v want %v", f.calls, want)
	}
}

func TestPluginEnable_ManagedUnsupportedIs400(t *testing.T) {
	s, f := newManagedPluginsServer(t)
	rr := postPlugin(s, "/api/plugins/hook/enable", `{"managed":true}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 body=%s", rr.Code, rr.Body.String())
	}
	if len(f.calls) != 0 {
		t.Errorf("plugin must not be enabled on rejected opt-in, calls=%v", f.calls)
	}
}

func TestPluginEnable_ManagedUnknownIs404(t *testing.T) {
	s, _ := newManagedPluginsServer(t)
	rr := postPlugin(s, "/api/plugins/nope/enable", `{"managed":true}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", rr.Code)
	}
}

func TestPluginEnable_MalformedBodyIs400(t *testing.T) {
	s, f := newManagedPluginsServer(t)
	rr := postPlugin(s, "/api/plugins/svc/enable", `{"managed":`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", rr.Code)
	}
	if len(f.calls) != 0 {
		t.Errorf("calls=%v want none", f.calls)
	}
}

func TestPluginDisable_IgnoresBody(t *testing.T) {
	s, f := newManagedPluginsServer(t)
	rr := postPlugin(s, "/api/plugins/svc/disable", `{"managed":true}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if want := []string{"disable:svc"}; !callsEqual(f.calls, want) {
		t.Errorf("calls=%v want %v", f.calls, want)
	}
}

func TestAuditPluginService_WritesEntry(t *testing.T) {
	s := bl90Server(t)
	l, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close() //nolint:errcheck
	s.SetAuditLog(l)

	actions := []string{
		"plugin_service_start", "plugin_service_stop",
		"plugin_service_restart", "plugin_service_health_fail",
	}
	details := map[string]any{"pid": 42}
	for _, a := range actions {
		s.AuditPluginService("svc", a, details)
	}
	if len(details) != 1 {
		t.Errorf("caller's details map was mutated: %v", details)
	}

	entries, err := l.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(actions) {
		t.Fatalf("entries=%d want %d", len(entries), len(actions))
	}
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Action] = true
		if e.Actor != "daemon" {
			t.Errorf("actor=%q want daemon", e.Actor)
		}
		if e.Details["resource_type"] != "plugin" || e.Details["resource_id"] != "svc" {
			t.Errorf("details=%v want resource plugin/svc", e.Details)
		}
		if _, ok := e.Details["pid"]; !ok {
			t.Errorf("details=%v missing pid", e.Details)
		}
	}
	for _, a := range actions {
		if !seen[a] {
			t.Errorf("missing audit action %q", a)
		}
	}
}

func TestAuditPluginService_NilLogIsNoop(t *testing.T) {
	s := bl90Server(t)
	s.AuditPluginService("svc", "plugin_service_start", nil) // must not panic
}
