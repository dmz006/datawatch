package server

// The projects_defaults block (branch_retention_days, weekly_share_percent,
// major_trigger) through GET and PUT /api/config: a PUT sets each key and the
// GET reads it back, the saved file reloads with the same values, a refused
// value answers 400 with the key named and leaves the stored value unchanged,
// and an unset major_trigger reads back as manual.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// projectsDefaultsBlock runs GET /api/config and returns its projects_defaults
// object.
func projectsDefaultsBlock(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var full map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	block, ok := full["projects_defaults"].(map[string]interface{})
	if !ok {
		t.Fatalf("GET /api/config has no projects_defaults object: %v", full["projects_defaults"])
	}
	return block
}

func TestProjectsDefaults_SetThenReadBack(t *testing.T) {
	cfg := config.DefaultConfig()
	s := attemptCostAPIServer(t, cfg)

	code, body := putConfigRaw(t, s, map[string]interface{}{
		"projects_defaults.branch_retention_days": 30,
		"projects_defaults.weekly_share_percent":  25,
		"projects_defaults.major_trigger":         "milestone",
	})
	if code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d (%s)", code, body)
	}

	block := projectsDefaultsBlock(t, s)
	if got := block["branch_retention_days"]; got != float64(30) {
		t.Errorf("branch_retention_days read back %v, want 30", got)
	}
	if got := block["weekly_share_percent"]; got != float64(25) {
		t.Errorf("weekly_share_percent read back %v, want 25", got)
	}
	if got := block["major_trigger"]; got != "milestone" {
		t.Errorf("major_trigger read back %v, want milestone", got)
	}

	// The saved file carries the same values.
	saved, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatalf("reload saved config: %v", err)
	}
	if saved.ProjectsDefaults.BranchRetentionDays != 30 ||
		saved.ProjectsDefaults.WeeklySharePercent != 25 ||
		saved.ProjectsDefaults.MajorTrigger != "milestone" {
		t.Errorf("saved projects_defaults = %+v, want 30/25/milestone", saved.ProjectsDefaults)
	}
}

func TestProjectsDefaults_UnsetMajorTriggerReadsManual(t *testing.T) {
	s := attemptCostAPIServer(t, &config.Config{})
	block := projectsDefaultsBlock(t, s)
	if got := block["major_trigger"]; got != "manual" {
		t.Errorf("unset major_trigger reads %v, want manual", got)
	}
	if got := block["branch_retention_days"]; got != float64(0) {
		t.Errorf("unset branch_retention_days reads %v, want 0", got)
	}
}

func TestProjectsDefaults_StringNumbersAccepted(t *testing.T) {
	cfg := &config.Config{}
	s := attemptCostAPIServer(t, cfg)
	code, body := putConfigRaw(t, s, map[string]interface{}{
		"projects_defaults.branch_retention_days": "14",
	})
	if code != http.StatusOK {
		t.Fatalf("PUT string number: want 200, got %d (%s)", code, body)
	}
	if cfg.ProjectsDefaults.BranchRetentionDays != 14 {
		t.Errorf("branch_retention_days = %d, want 14", cfg.ProjectsDefaults.BranchRetentionDays)
	}
}

func TestProjectsDefaults_RefusalsLeaveValueUnchanged(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value interface{}
		want  string // substring the 400 body must carry
	}{
		{"negative retention", "projects_defaults.branch_retention_days", -1, "projects_defaults.branch_retention_days"},
		{"share above 100", "projects_defaults.weekly_share_percent", 101, "projects_defaults.weekly_share_percent"},
		{"share negative", "projects_defaults.weekly_share_percent", -5, "projects_defaults.weekly_share_percent"},
		{"non-numeric retention", "projects_defaults.branch_retention_days", "soon", "projects_defaults.branch_retention_days"},
		{"bad trigger", "projects_defaults.major_trigger", "weekly", "projects_defaults.major_trigger"},
		{"unknown field", "projects_defaults.nonsense", 1, "projects_defaults.nonsense"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{ProjectsDefaults: config.ProjectsDefaultsConfig{
				BranchRetentionDays: 7, WeeklySharePercent: 10, MajorTrigger: "never",
			}}
			s := attemptCostAPIServer(t, cfg)
			code, body := putConfigRaw(t, s, map[string]interface{}{tc.key: tc.value})
			if code != http.StatusBadRequest {
				t.Fatalf("PUT %s=%v: want 400, got %d (%s)", tc.key, tc.value, code, body)
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("400 body %q does not name %q", body, tc.want)
			}
			if cfg.ProjectsDefaults.BranchRetentionDays != 7 ||
				cfg.ProjectsDefaults.WeeklySharePercent != 10 ||
				cfg.ProjectsDefaults.MajorTrigger != "never" {
				t.Errorf("refused PUT changed stored value: %+v", cfg.ProjectsDefaults)
			}
		})
	}
}

func TestProjectsDefaults_ApplyPatchIgnoresBadValue(t *testing.T) {
	cfg := &config.Config{ProjectsDefaults: config.ProjectsDefaultsConfig{WeeklySharePercent: 40}}
	applyConfigPatch(cfg, map[string]interface{}{"projects_defaults.weekly_share_percent": 900})
	if cfg.ProjectsDefaults.WeeklySharePercent != 40 {
		t.Errorf("out-of-range share applied: %d, want 40 unchanged", cfg.ProjectsDefaults.WeeklySharePercent)
	}
}
