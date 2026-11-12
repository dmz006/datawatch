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

// planningRestartPolicyGet reads the autonomous block from GET /api/config.
func planningRestartPolicyGet(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var got struct {
		Autonomous map[string]interface{} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.Autonomous
}

// planningRestartPolicyPut sends a PUT /api/config and returns the recorder.
func planningRestartPolicyPut(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

func TestPlanningRestartPolicyConfig_AcceptsEachValue(t *testing.T) {
	for _, v := range []string{"reattach", "restart", "draft", "DRAFT"} {
		s := &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
		rr := planningRestartPolicyPut(t, s, map[string]any{"autonomous.planning_restart_policy": v})
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %q: want 200, got %d: %s", v, rr.Code, rr.Body.String())
		}
		want := strings.ToLower(v)
		if got := planningRestartPolicyGet(t, s)["planning_restart_policy"]; got != want {
			t.Errorf("read-back after PUT %q = %v, want %q", v, got, want)
		}
		if s.cfg.Autonomous.PlanningRestartPolicy != want {
			t.Errorf("stored policy after PUT %q = %q, want %q", v, s.cfg.Autonomous.PlanningRestartPolicy, want)
		}
	}
}

func TestPlanningRestartPolicyConfig_RefusesUnknownValue(t *testing.T) {
	for _, v := range []any{"later", "", "wait_quiet", 3, true} {
		s := &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
		before := s.cfg.Autonomous.PlanningRestartPolicy
		rr := planningRestartPolicyPut(t, s, map[string]any{"autonomous.planning_restart_policy": v})
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %v: want 400, got %d", v, rr.Code)
			continue
		}
		msg := rr.Body.String()
		for _, allowed := range []string{"reattach", "restart", "draft"} {
			if !strings.Contains(msg, allowed) {
				t.Errorf("PUT %v: 400 message %q does not list %q", v, msg, allowed)
			}
		}
		if s.cfg.Autonomous.PlanningRestartPolicy != before {
			t.Errorf("PUT %v: refused value was stored (%q)", v, s.cfg.Autonomous.PlanningRestartPolicy)
		}
	}
}

func TestPlanningRestartPolicyConfig_ReadBackDefault(t *testing.T) {
	// A fresh config reads the default.
	s := &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
	if got := planningRestartPolicyGet(t, s)["planning_restart_policy"]; got != "reattach" {
		t.Fatalf("default read = %v, want reattach", got)
	}
	// A stored value the normalizer does not recognise reads as the default,
	// never as the raw string.
	s.cfg.Autonomous.PlanningRestartPolicy = "bogus"
	if got := planningRestartPolicyGet(t, s)["planning_restart_policy"]; got != "reattach" {
		t.Fatalf("bogus stored read = %v, want reattach", got)
	}
}
