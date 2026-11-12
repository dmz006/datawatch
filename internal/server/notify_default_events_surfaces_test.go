// Configuration Accessibility round-trip for autonomous.notify.default_events
// (default lifecycle event kinds the built-in lifecycle sinks subscribe to)
// on the REST config surface: GET /api/config must always expose the key
// (even when empty), and PUT /api/config must persist the value from both
// an array and a comma-separated string, with an empty list really clearing.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// newNotifyDefaultEventsServer builds the minimal Server wiring needed to
// exercise the /api/config GET/PUT surface (no autonomous manager required
// — autonomous.notify.default_events lives on s.cfg.Autonomous only).
func newNotifyDefaultEventsServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	s := &Server{
		cfg:     config.DefaultConfig(),
		cfgPath: cfgPath,
	}
	return s, cfgPath
}

func getNotifyDefaultEvents(t *testing.T, s *Server) []string {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			Notify struct {
				DefaultEvents []string `json:"default_events"`
			} `json:"notify"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.Autonomous.Notify.DefaultEvents
}

func TestAutonomousNotifyConfigSurfaces(t *testing.T) {
	s, _ := newNotifyDefaultEventsServer(t)

	// Default: key present on GET with the five-kind baseline (non-nil)
	// so the PWA settings card renders the field.
	got := getNotifyDefaultEvents(t, s)
	if got == nil {
		t.Fatalf("GET /api/config autonomous.notify.default_events = nil, want the baseline list (key must always be present)")
	}
	wantBaseline := []string{"prd.needs_review", "prd.blocked", "prd.failed", "prd.completed", "prd.cancelled"}
	if len(got) != len(wantBaseline) {
		t.Fatalf("GET default = %v, want baseline %v", got, wantBaseline)
	}
	for i := range wantBaseline {
		if got[i] != wantBaseline[i] {
			t.Fatalf("GET default = %v, want baseline %v", got, wantBaseline)
		}
	}

	// PUT with an array persists.
	b, _ := json.Marshal(map[string]any{"autonomous.notify.default_events": []string{"prd.completed", "task.failed"}})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT array: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	got = getNotifyDefaultEvents(t, s)
	if len(got) != 2 || got[0] != "prd.completed" || got[1] != "task.failed" {
		t.Fatalf("GET after PUT array = %v, want [prd.completed task.failed]", got)
	}
	if v := s.cfg.Autonomous.Notify.DefaultEvents; len(v) != 2 || v[0] != "prd.completed" || v[1] != "task.failed" {
		t.Fatalf("cfg.Autonomous.Notify.DefaultEvents = %v, want [prd.completed task.failed]", v)
	}

	// PUT with a comma-separated string persists.
	b, _ = json.Marshal(map[string]any{"autonomous.notify.default_events": "task.session_started,task.completed"})
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT csv: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	got = getNotifyDefaultEvents(t, s)
	if len(got) != 2 || got[0] != "task.session_started" || got[1] != "task.completed" {
		t.Fatalf("GET after PUT csv = %v, want [task.session_started task.completed]", got)
	}

	// PUT empty list really clears it (and GET still renders the key).
	b, _ = json.Marshal(map[string]any{"autonomous.notify.default_events": []string{}})
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT clear: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	got = getNotifyDefaultEvents(t, s)
	if got == nil {
		t.Fatalf("GET after PUT clear = nil, want empty list (key must stay present)")
	}
	if len(got) != 0 {
		t.Fatalf("GET after PUT clear = %v, want []", got)
	}
	if v := s.cfg.Autonomous.Notify.DefaultEvents; v == nil || len(v) != 0 {
		t.Fatalf("cfg.Autonomous.Notify.DefaultEvents after clear = %v (len %d), want non-nil empty", v, len(v))
	}
}

// TestAutonomousNotifyConfigOptInWarningKinds pins the opt-in warning
// kinds on the REST config surface: the new scope_warning /
// guardrail_warn kinds are accepted on PUT /api/config (array and CSV
// forms) and persist, while an unknown kind is rejected with 400 and
// the stored value is left unchanged. Warnings remain opt-in — never
// in the default set.
func TestAutonomousNotifyConfigOptInWarningKinds(t *testing.T) {
	s, _ := newNotifyDefaultEventsServer(t)

	// 1) Array form: new warning kinds accepted alongside baseline kinds.
	b, _ := json.Marshal(map[string]any{"autonomous.notify.default_events": []string{"prd.blocked", "scope_warning", "guardrail_warn"}})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT array with warning kinds: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	got := getNotifyDefaultEvents(t, s)
	want := []string{"prd.blocked", "scope_warning", "guardrail_warn"}
	if len(got) != len(want) {
		t.Fatalf("GET after PUT = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("GET after PUT = %v, want %v", got, want)
		}
	}

	// 2) CSV form: same kinds accepted.
	b, _ = json.Marshal(map[string]any{"autonomous.notify.default_events": "scope_warning,guardrail_warn"})
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT csv with warning kinds: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	got = getNotifyDefaultEvents(t, s)
	if len(got) != 2 || got[0] != "scope_warning" || got[1] != "guardrail_warn" {
		t.Fatalf("GET after CSV PUT = %v, want [scope_warning guardrail_warn]", got)
	}
}

// TestAutonomousNotifyConfigRejectsUnknownKinds pins the negative side
// of the config surface: an unknown kind (or an empty entry) rejects
// the whole PUT with 400 and leaves the stored value unchanged.
func TestAutonomousNotifyConfigRejectsUnknownKinds(t *testing.T) {
	s, _ := newNotifyDefaultEventsServer(t)

	// First establish a known-good stored value.
	b, _ := json.Marshal(map[string]any{"autonomous.notify.default_events": []string{"prd.blocked"}})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT good value: want 200, got %d", rr.Code)
	}

	for _, bad := range []map[string]any{
		{"autonomous.notify.default_events": []string{"prd.blocked", "scope.warngin"}}, // typo / unknown
		{"autonomous.notify.default_events": []string{"prd.blocked", ""}},                // empty entry
		{"autonomous.notify.default_events": "scope warning"},                              // unknown CSV
		{"autonomous.notify.default_events": "not-an-array"},                              // wrong type
	} {
		bb, _ := json.Marshal(bad)
		rr = httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(bb)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %v: want 400, got %d body=%s", bad, rr.Code, rr.Body.String())
		}
		if got := getNotifyDefaultEvents(t, s); len(got) != 1 || got[0] != "prd.blocked" {
			t.Errorf("after rejected PUT %v, stored = %v, want unchanged [prd.blocked]", bad, got)
		}
	}
}
