// Worker session profile settings (autonomous.worker_profiles,
// autonomous.worker_profile_defaults, autonomous.worker_profile) on the
// /api/config REST surface: GET returns all three keys, a valid PUT
// round-trips, and a PUT that would store a built-in profile name or an
// unknown profile reference rejects the whole PUT (400) and leaves the
// config unchanged.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newWorkerProfileServer(t *testing.T) *Server {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	return &Server{cfg: config.DefaultConfig(), cfgPath: cfgPath}
}

// putWorkerProfileConfig returns the status code and response body of a PUT /api/config.
func putWorkerProfileConfig(t *testing.T, s *Server, body any) (int, string) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal PUT body: %v", err)
	}
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr.Code, rr.Body.String()
}

func getWorkerProfileView(t *testing.T, s *Server) (profilesRaw, defaultsRaw, nameRaw json.RawMessage) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous map[string]json.RawMessage `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.Autonomous["worker_profiles"], got.Autonomous["worker_profile_defaults"], got.Autonomous["worker_profile"]
}

// A valid user profile for the tests.
func validUserProfile() map[string]any {
	return map[string]any{
		"name":    "triage",
		"mcp":     map[string]any{"mode": "datawatch"},
		"tools":   map[string]any{"mode": "allow", "allow": []string{"Read", "Grep"}},
		"skills":  false,
		"context": "digest",
	}
}

func TestWorkerProfileConfig_GetReturnsThreeKeys(t *testing.T) {
	s := newWorkerProfileServer(t)
	profilesRaw, defaultsRaw, nameRaw := getWorkerProfileView(t, s)
	for _, raw := range []json.RawMessage{profilesRaw, defaultsRaw, nameRaw} {
		if len(raw) == 0 {
			t.Fatalf("GET /api/config autonomous map lacks a worker profile key: %s %s %s", profilesRaw, defaultsRaw, nameRaw)
		}
	}

	// Default shape: an empty profile table (not null), both defaults maps
	// present and empty, and an empty global name.
	var profiles map[string]json.RawMessage
	if err := json.Unmarshal(profilesRaw, &profiles); err != nil {
		t.Fatalf("worker_profiles must be an object, got %s", profilesRaw)
	}
	if len(profiles) != 0 {
		t.Fatalf("default worker_profiles must be empty, got %s", profilesRaw)
	}
	var defs struct {
		Role map[string]string `json:"role"`
		Kind map[string]string `json:"kind"`
	}
	if err := json.Unmarshal(defaultsRaw, &defs); err != nil {
		t.Fatalf("worker_profile_defaults must be an object, got %s", defaultsRaw)
	}
	if len(defs.Role) != 0 || len(defs.Kind) != 0 {
		t.Fatalf("default worker_profile_defaults must be empty, got %s", defaultsRaw)
	}
	var name string
	if err := json.Unmarshal(nameRaw, &name); err != nil {
		t.Fatalf("worker_profile must be a string, got %s", nameRaw)
	}
	if name != "" {
		t.Fatalf("default worker_profile must be empty, got %q", name)
	}
}

func TestWorkerProfileConfig_PutRoundTrip(t *testing.T) {
	s := newWorkerProfileServer(t)
	code, body := putWorkerProfileConfig(t, s, map[string]any{
		"autonomous.worker_profiles": map[string]any{
			"triage": validUserProfile(),
		},
		"autonomous.worker_profile_defaults": map[string]any{
			"role": map[string]any{"worker": "triage"},
			"kind": map[string]any{"docs": "triage"},
		},
		"autonomous.worker_profile": "triage",
	})
	if code != http.StatusOK {
		t.Fatalf("PUT /api/config: want 200, got %d body=%s", code, body)
	}

	// The stored config holds the round-tripped values.
	p, ok := s.cfg.Autonomous.WorkerProfiles["triage"]
	if !ok {
		t.Fatalf("stored WorkerProfiles lacks \"triage\": %v", s.cfg.Autonomous.WorkerProfiles)
	}
	if p.Name != "triage" || p.Context != "digest" || len(p.Tools.Allow) != 2 {
		t.Fatalf("stored triage profile = %+v", p)
	}
	if got := s.cfg.Autonomous.WorkerProfileDefaults.Role["worker"]; got != "triage" {
		t.Fatalf("stored defaults.role[worker] = %q, want triage", got)
	}
	if got := s.cfg.Autonomous.WorkerProfileDefaults.Kind["docs"]; got != "triage" {
		t.Fatalf("stored defaults.kind[docs] = %q, want triage", got)
	}
	if got := s.cfg.Autonomous.WorkerProfile; got != "triage" {
		t.Fatalf("stored WorkerProfile = %q, want triage", got)
	}
	if verr := config.ValidateWorkerProfileConfig(s.cfg); verr != nil {
		t.Fatalf("stored config must pass the load-time validator: %v", verr)
	}

	// GET reflects the same values.
	profilesRaw, defaultsRaw, nameRaw := getWorkerProfileView(t, s)
	var profiles map[string]map[string]any
	if err := json.Unmarshal(profilesRaw, &profiles); err != nil {
		t.Fatalf("decode worker_profiles: %v", err)
	}
	if got := profiles["triage"]["name"]; got != "triage" {
		t.Fatalf("GET worker_profiles.triage.name = %v, want triage", got)
	}
	var defs struct {
		Role map[string]string `json:"role"`
		Kind map[string]string `json:"kind"`
	}
	if err := json.Unmarshal(defaultsRaw, &defs); err != nil {
		t.Fatalf("decode worker_profile_defaults: %v", err)
	}
	if defs.Role["worker"] != "triage" || defs.Kind["docs"] != "triage" {
		t.Fatalf("GET worker_profile_defaults = %+v", defs)
	}
	var name string
	if err := json.Unmarshal(nameRaw, &name); err != nil || name != "triage" {
		t.Fatalf("GET worker_profile = %q, want triage", name)
	}

	// A following PUT that changes one key leaves the others intact.
	code, body = putWorkerProfileConfig(t, s, map[string]any{"autonomous.worker_profile": "minimal"})
	if code != http.StatusOK {
		t.Fatalf("follow-up PUT: want 200, got %d body=%s", code, body)
	}
	if got := s.cfg.Autonomous.WorkerProfile; got != "minimal" {
		t.Fatalf("follow-up PUT: stored WorkerProfile = %q, want minimal", got)
	}
	if _, ok := s.cfg.Autonomous.WorkerProfiles["triage"]; !ok {
		t.Fatalf("follow-up PUT must not drop the existing user profiles")
	}
}

func TestWorkerProfileConfig_PutUnknownProfileRejected(t *testing.T) {
	for _, body := range []map[string]any{
		// The global name references a profile nobody defined.
		{"autonomous.worker_profile": "nope"},
		// A role default references a profile nobody defined.
		{"autonomous.worker_profile_defaults": map[string]any{"role": map[string]any{"worker": "nope"}}},
		// A kind default does the same.
		{"autonomous.worker_profile_defaults": map[string]any{"kind": map[string]any{"docs": "nope"}}},
	} {
		s := newWorkerProfileServer(t)
		code, resp := putWorkerProfileConfig(t, s, body)
		if code != http.StatusBadRequest {
			t.Fatalf("PUT %v: want 400, got %d body=%s", body, code, resp)
		}
		if !strings.Contains(resp, "nope") {
			t.Fatalf("PUT %v: error must name the unknown profile, got %s", body, resp)
		}
		// The config is left unchanged.
		if s.cfg.Autonomous.WorkerProfile != "" {
			t.Fatalf("rejected PUT changed WorkerProfile to %q", s.cfg.Autonomous.WorkerProfile)
		}
		if len(s.cfg.Autonomous.WorkerProfiles) != 0 {
			t.Fatalf("rejected PUT changed WorkerProfiles to %v", s.cfg.Autonomous.WorkerProfiles)
		}
		if len(s.cfg.Autonomous.WorkerProfileDefaults.Role) != 0 || len(s.cfg.Autonomous.WorkerProfileDefaults.Kind) != 0 {
			t.Fatalf("rejected PUT changed WorkerProfileDefaults to %+v", s.cfg.Autonomous.WorkerProfileDefaults)
		}
	}
}

func TestWorkerProfileConfig_PutBuiltinNameRejected(t *testing.T) {
	for _, body := range []map[string]any{
		// A user profile named like a built-in.
		{"autonomous.worker_profiles": map[string]any{"full": validUserProfile()}},
		// ...and the other reserved names too.
		{"autonomous.worker_profiles": map[string]any{"minimal": validUserProfile()}},
		{"autonomous.worker_profiles": map[string]any{"digest": validUserProfile()}},
		{"autonomous.worker_profiles": map[string]any{"datawatch-tools": validUserProfile()}},
	} {
		s := newWorkerProfileServer(t)
		code, resp := putWorkerProfileConfig(t, s, body)
		if code != http.StatusBadRequest {
			t.Fatalf("PUT %v: want 400, got %d body=%s", body, code, resp)
		}
		if len(s.cfg.Autonomous.WorkerProfiles) != 0 {
			t.Fatalf("rejected PUT stored a user profile: %v", s.cfg.Autonomous.WorkerProfiles)
		}
		if verr := config.ValidateWorkerProfileConfig(s.cfg); verr != nil {
			t.Fatalf("config after rejected PUT must stay valid: %v", verr)
		}
	}

	// Referencing a built-in profile is, of course, still valid.
	s := newWorkerProfileServer(t)
	code, resp := putWorkerProfileConfig(t, s, map[string]any{"autonomous.worker_profile": "full"})
	if code != http.StatusOK {
		t.Fatalf("PUT worker_profile=full: want 200 (built-ins are referenceable), got %d body=%s", code, resp)
	}
	if got := s.cfg.Autonomous.WorkerProfile; got != "full" {
		t.Fatalf("stored WorkerProfile = %q, want full", got)
	}
}
