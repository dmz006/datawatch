// Configuration Accessibility round-trips for the four plan-coverage
// finding-filter keys (plan_coverage_suppress_created_files,
// plan_coverage_scoped_requirements, plan_coverage_requirement_sections,
// plan_coverage_min_requirement_chars) on both REST config surfaces:
// GET/PUT /api/config (dotted keys) and PUT /api/autonomous/config.
// Defaults come from EffectivePlanCoverageFilters; invalid values
// reject the whole PUT with 400.

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func newPlanCoverageFiltersServer(t *testing.T) (*Server, *autonomous.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	mgr, err := autonomous.NewManager(filepath.Join(dir, "autonomous"), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	s := &Server{
		cfg:           config.DefaultConfig(),
		cfgPath:       cfgPath,
		autonomousMgr: newTestAPI(mgr),
	}
	return s, mgr, cfgPath
}

func putExpect(t *testing.T, h http.HandlerFunc, path string, body any, wantStatus int) string {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPut, path, bytes.NewReader(b)))
	if rr.Code != wantStatus {
		t.Fatalf("PUT %s %s: want %d, got %d body=%s", path, b, wantStatus, rr.Code, rr.Body.String())
	}
	return rr.Body.String()
}

func getFilters(t *testing.T, s *Server) map[string]string {
	t.Helper()
	want := []string{
		"plan_coverage_suppress_created_files",
		"plan_coverage_scoped_requirements",
		"plan_coverage_requirement_sections",
		"plan_coverage_min_requirement_chars",
	}
	aut := getAutonomousConfigMap(t, s)
	out := make(map[string]string, len(want))
	for _, k := range want {
		raw, ok := aut[k]
		if !ok {
			t.Fatalf("GET /api/config missing autonomous.%s", k)
		}
		// Decode through JSON so quoted strings come back unquoted.
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("decode autonomous.%s %s: %v", k, raw, err)
		}
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

func TestPlanCoverageFilters_ConfigGetDefaults(t *testing.T) {
	s, _, _ := newPlanCoverageFiltersServer(t)
	got := getFilters(t, s)
	if got["plan_coverage_suppress_created_files"] != "true" {
		t.Errorf("suppress_created_files default = %q, want true", got["plan_coverage_suppress_created_files"])
	}
	if got["plan_coverage_scoped_requirements"] != "true" {
		t.Errorf("scoped_requirements default = %q, want true", got["plan_coverage_scoped_requirements"])
	}
	if got["plan_coverage_requirement_sections"] != "scope,constraints" {
		t.Errorf("requirement_sections default = %q, want scope,constraints", got["plan_coverage_requirement_sections"])
	}
	if got["plan_coverage_min_requirement_chars"] != "30" {
		t.Errorf("min_requirement_chars default = %q, want 30", got["plan_coverage_min_requirement_chars"])
	}
}

func TestPlanCoverageFilters_ConfigRoundTrip(t *testing.T) {
	s, mgr, _ := newPlanCoverageFiltersServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.plan_coverage_suppress_created_files": false,
		"autonomous.plan_coverage_scoped_requirements":    false,
		"autonomous.plan_coverage_requirement_sections":   " scope , Goals ,scope",
		"autonomous.plan_coverage_min_requirement_chars":  80,
	})
	got := getFilters(t, s)
	if got["plan_coverage_suppress_created_files"] != "false" {
		t.Errorf("suppress_created_files after PUT = %q, want false", got["plan_coverage_suppress_created_files"])
	}
	if got["plan_coverage_scoped_requirements"] != "false" {
		t.Errorf("scoped_requirements after PUT = %q, want false", got["plan_coverage_scoped_requirements"])
	}
	if got["plan_coverage_requirement_sections"] != "scope,goals" {
		t.Errorf("requirement_sections after PUT = %q, want scope,goals (trimmed, lowercased, deduped)", got["plan_coverage_requirement_sections"])
	}
	if got["plan_coverage_min_requirement_chars"] != "80" {
		t.Errorf("min_requirement_chars after PUT = %q, want 80", got["plan_coverage_min_requirement_chars"])
	}
	// The live autonomous manager must see the same values (the PUT /api/config
	// path syncs the autonomous manager).
	cfg := mgr.Config()
	if cfg.PlanCoverageSuppressCreatedFiles == nil || *cfg.PlanCoverageSuppressCreatedFiles != false {
		t.Errorf("manager suppress_created_files = %v, want explicit false", cfg.PlanCoverageSuppressCreatedFiles)
	}
	if cfg.PlanCoverageScopedRequirements == nil || *cfg.PlanCoverageScopedRequirements != false {
		t.Errorf("manager scoped_requirements = %v, want explicit false", cfg.PlanCoverageScopedRequirements)
	}
	if cfg.PlanCoverageMinRequirementChars != 80 {
		t.Errorf("manager min_requirement_chars = %d, want 80", cfg.PlanCoverageMinRequirementChars)
	}
}

func TestPlanCoverageFilters_ConfigPartialPutKeepsExplicitFalse(t *testing.T) {
	s, _, _ := newPlanCoverageFiltersServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.plan_coverage_suppress_created_files": false,
		"autonomous.plan_coverage_scoped_requirements":    false,
	})
	// A later PUT touching only the section list must not flip the
	// explicit false booleans back to the true defaults.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.plan_coverage_requirement_sections": "goals",
	})
	got := getFilters(t, s)
	if got["plan_coverage_suppress_created_files"] != "false" {
		t.Errorf("suppress_created_files after partial PUT = %q, want false (explicit value must survive)", got["plan_coverage_suppress_created_files"])
	}
	if got["plan_coverage_scoped_requirements"] != "false" {
		t.Errorf("scoped_requirements after partial PUT = %q, want false (explicit value must survive)", got["plan_coverage_scoped_requirements"])
	}
	if got["plan_coverage_requirement_sections"] != "goals" {
		t.Errorf("requirement_sections after partial PUT = %q, want goals", got["plan_coverage_requirement_sections"])
	}
}

func TestPlanCoverageFilters_ConfigInvalidValues(t *testing.T) {
	s, _, _ := newPlanCoverageFiltersServer(t)
	for _, body := range []map[string]any{
		{"autonomous.plan_coverage_suppress_created_files": "yes"},
		{"autonomous.plan_coverage_scoped_requirements": "on"},
		{"autonomous.plan_coverage_requirement_sections": 42},
		{"autonomous.plan_coverage_requirement_sections": true},
		{"autonomous.plan_coverage_min_requirement_chars": 1.5},
		{"autonomous.plan_coverage_min_requirement_chars": -1},
		{"autonomous.plan_coverage_min_requirement_chars": "abc"},
	} {
		putExpect(t, s.handlePutConfig, "/api/config", body, http.StatusBadRequest)
	}
	// A fully valid PUT must still succeed after the refusals (no state
	// was stored by the failures).
	putExpect(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.plan_coverage_suppress_created_files": true,
	}, http.StatusOK)
	got := getFilters(t, s)
	if got["plan_coverage_suppress_created_files"] != "true" {
		t.Errorf("suppress_created_files = %q after valid PUT, want true", got["plan_coverage_suppress_created_files"])
	}
}

func TestPlanCoverageFilters_ConfigAutonomousEndpointRoundTrip(t *testing.T) {
	s, mgr, _ := newPlanCoverageFiltersServer(t)
	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{
		"plan_coverage_suppress_created_files": false,
		"plan_coverage_scoped_requirements":    false,
		"plan_coverage_requirement_sections":   "goals",
		"plan_coverage_min_requirement_chars":  120,
	})
	cfg := mgr.Config()
	if cfg.PlanCoverageSuppressCreatedFiles == nil || *cfg.PlanCoverageSuppressCreatedFiles != false {
		t.Errorf("manager suppress_created_files = %v, want explicit false", cfg.PlanCoverageSuppressCreatedFiles)
	}
	if cfg.PlanCoverageScopedRequirements == nil || *cfg.PlanCoverageScopedRequirements != false {
		t.Errorf("manager scoped_requirements = %v, want explicit false", cfg.PlanCoverageScopedRequirements)
	}
	if cfg.PlanCoverageRequirementSections != "goals" {
		t.Errorf("manager requirement_sections = %q, want goals", cfg.PlanCoverageRequirementSections)
	}
	if cfg.PlanCoverageMinRequirementChars != 120 {
		t.Errorf("manager min_requirement_chars = %d, want 120", cfg.PlanCoverageMinRequirementChars)
	}
	// The /api/config readback must agree with the manager.
	got := getFilters(t, s)
	if got["plan_coverage_suppress_created_files"] != "false" ||
		got["plan_coverage_scoped_requirements"] != "false" ||
		got["plan_coverage_requirement_sections"] != "goals" ||
		got["plan_coverage_min_requirement_chars"] != "120" {
		t.Errorf("GET /api/config after autonomous PUT = %+v, want false/false/goals/120", got)
	}
}

func TestPlanCoverageFilters_ConfigAutonomousEndpointInvalidValues(t *testing.T) {
	s, m, _ := newPlanCoverageFiltersServer(t)
	for _, body := range []map[string]any{
		{"plan_coverage_suppress_created_files": "yes"},
		{"plan_coverage_scoped_requirements": 42},
		{"plan_coverage_requirement_sections": true},
		{"plan_coverage_requirement_sections": []any{"scope"}},
		{"plan_coverage_min_requirement_chars": "abc"},
		{"plan_coverage_min_requirement_chars": 0.5},
		{"plan_coverage_min_requirement_chars": -2},
	} {
		putExpect(t, s.handleAutonomousConfig, "/api/autonomous/config", body, http.StatusBadRequest)
	}
	// The manager config must be untouched by the refusals.
	cfg := m.Config()
	if cfg.PlanCoverageSuppressCreatedFiles != nil || cfg.PlanCoverageScopedRequirements != nil {
		t.Errorf("refused PUTs must not store partial filter state: %+v", cfg)
	}
	// A valid PUT still succeeds afterwards.
	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{
		"plan_coverage_min_requirement_chars": 0,
	})
	if got := m.Config().PlanCoverageMinRequirementChars; got != 0 {
		t.Errorf("min_requirement_chars = %d, want 0 (0 reads the 30 default)", got)
	}
}
