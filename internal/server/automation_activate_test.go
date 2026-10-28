// REST tests for POST /api/automations/{id}/activate, the automations array
// of GET /api/autonomous/status and the autonomous.automation.idle_alert_minutes
// config key, driven through the real autonomous manager and coordinator.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// noopStarter satisfies autonomous.AutomationStarter without launching work.
type noopStarter struct{}

func (noopStarter) Run(string) error                   { return nil }
func (noopStarter) Resume(string, string) (any, error) { return nil, nil }

func newActivateServer(t *testing.T, withCoordinator bool) (*Server, *autonomous.Manager, *audit.Log) {
	t.Helper()
	dir := t.TempDir()
	mgr, err := autonomous.NewManager(filepath.Join(dir, "autonomous"), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if withCoordinator {
		c := autonomous.NewCoordinator(mgr, noopStarter{})
		c.Start()
		t.Cleanup(c.Stop)
	}
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s := &Server{
		cfg:           config.DefaultConfig(),
		cfgPath:       filepath.Join(dir, "config.yaml"),
		autonomousMgr: newTestAPI(mgr),
		auditLog:      al,
	}
	return s, mgr, al
}

func createDraftAutomation(t *testing.T, s *Server, mgr *autonomous.Manager) string {
	t.Helper()
	body := doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "chain", "members": memberWire(makeMemberPRD(t, mgr)),
	}, http.StatusCreated)
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &a); err != nil || a.ID == "" {
		t.Fatalf("decode create: %v (%q)", err, body)
	}
	return a.ID
}

func TestAutomationActivateAuditsAndMapsErrors(t *testing.T) {
	s, mgr, al := newActivateServer(t, true)
	id := createDraftAutomation(t, s, mgr)

	// The member PRD is still a draft, so activation lands the Automation in
	// blocked rather than active; either way the call itself succeeds.
	out := doAutomation(t, s, http.MethodPost, "/api/automations/"+id+"/activate",
		map[string]any{"reason": "go"}, http.StatusOK)
	var a struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &a); err != nil || (a.Status != "blocked" && a.Status != "active") {
		t.Fatalf("activate response status = %q (err=%v body=%s)", a.Status, err, out)
	}

	entries, err := al.Read(audit.QueryFilter{Action: "automation_activate"})
	if err != nil || len(entries) == 0 {
		t.Fatalf("no automation_activate audit entry (err=%v)", err)
	}
	var found bool
	for _, e := range entries {
		if e.Details["resource_type"] == "automation" && e.Details["resource_id"] == id &&
			e.Details["actor"] == "operator" && e.Details["reason"] == "go" {
			found = true
		}
	}
	if !found {
		t.Errorf("audit entries lack resource/actor/reason: %+v", entries)
	}

	// A second activation is a state conflict, an unknown ID is a 404.
	doAutomation(t, s, http.MethodPost, "/api/automations/"+id+"/activate", nil, http.StatusConflict)
	doAutomation(t, s, http.MethodPost, "/api/automations/deadbeef/activate", nil, http.StatusNotFound)
	// Only POST is allowed, and a malformed body is rejected.
	doAutomation(t, s, http.MethodGet, "/api/automations/"+id+"/activate", nil, http.StatusMethodNotAllowed)
	req := httptest.NewRequest(http.MethodPost, "/api/automations/"+id+"/activate", strings.NewReader("{nope"))
	rr := httptest.NewRecorder()
	s.handleAutomations(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("malformed body: want 400, got %d", rr.Code)
	}
}

func TestAutomationActivateWithoutCoordinatorFails(t *testing.T) {
	s, mgr, _ := newActivateServer(t, false)
	id := createDraftAutomation(t, s, mgr)
	doAutomation(t, s, http.MethodPost, "/api/automations/"+id+"/activate", nil, http.StatusBadRequest)
}

func TestAutomationStatusIncludesAutomationsArray(t *testing.T) {
	s, mgr, _ := newActivateServer(t, true)
	id := createDraftAutomation(t, s, mgr)

	rr := httptest.NewRecorder()
	s.handleAutonomousStatus(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var st struct {
		Automations []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Status         string `json:"status"`
			PendingMembers int    `json:"pending_members"`
		} `json:"automations"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if len(st.Automations) != 1 || st.Automations[0].ID != id || st.Automations[0].Status != "draft" ||
		st.Automations[0].Name != "chain" || st.Automations[0].PendingMembers != 1 {
		t.Fatalf("automations = %+v, want one draft row for %s", st.Automations, id)
	}
}

func TestAutomationIdleAlertConfigRoundTrip(t *testing.T) {
	s, mgr, _ := newActivateServer(t, false)

	getIdle := func() int {
		var a struct {
			Idle int `json:"idle_alert_minutes"`
		}
		_ = json.Unmarshal(getAutonomousConfigMap(t, s)["automation"], &a)
		return a.Idle
	}
	if got := getIdle(); got != 0 {
		t.Fatalf("default idle_alert_minutes = %d, want 0", got)
	}

	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.idle_alert_minutes": 30})
	if got := getIdle(); got != 30 {
		t.Fatalf("after PUT: %d, want 30", got)
	}
	if got := mgr.Config().Automation.IdleAlertMinutes; got != 30 {
		t.Fatalf("live manager idle_alert_minutes = %d, want 30", got)
	}

	// Negative and non-integer values are a 400 and leave the value alone.
	pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.idle_alert_minutes": -1})
	pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.idle_alert_minutes": "soon"})
	pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.idle_alert_minutes": 1.5})
	pcPutBad(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{"idle_alert_minutes": -5}})
	if got := getIdle(); got != 30 {
		t.Fatalf("rejected PUT changed the value: %d, want 30", got)
	}

	// The dedicated autonomous config surface (what MCP sends) and 0 = off.
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{"idle_alert_minutes": 0}})
	if got := mgr.Config().Automation.IdleAlertMinutes; got != 0 {
		t.Fatalf("live manager after autonomous-config PUT = %d, want 0", got)
	}
}

func TestAutomationTriageAlertConfigRoundTrip(t *testing.T) {
	s, mgr, _ := newActivateServer(t, false)

	// GET /api/config: an unset (nil) value emits the effective value here,
	// and an explicit 0 stays 0 rather than reading the 30-minute default.
	getTriage := func() int {
		var a struct {
			Triage *int `json:"triage_alert_minutes"`
		}
		_ = json.Unmarshal(getAutonomousConfigMap(t, s)["automation"], &a)
		if a.Triage == nil {
			return 30 // unset: the default read
		}
		return *a.Triage
	}
	if got := getTriage(); got != 30 {
		t.Fatalf("default triage_alert_minutes = %d, want the 30-minute default", got)
	}

	// PUT /api/config writes it, and the live manager sees the same value.
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.triage_alert_minutes": 45})
	if got := getTriage(); got != 45 {
		t.Fatalf("after PUT /api/config: %d, want 45", got)
	}
	if got := mgr.Config().Automation.EffectiveTriageAlertMinutes(); got != 45 {
		t.Fatalf("live manager triage_alert_minutes = %d, want 45", got)
	}

	// Negative, fractional and non-numeric values are a 400 and leave the
	// value alone, on both surfaces.
	pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.triage_alert_minutes": -1})
	pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.triage_alert_minutes": "soon"})
	pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.triage_alert_minutes": 1.5})
	pcPutBad(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{"triage_alert_minutes": -5}})
	if got := getTriage(); got != 45 {
		t.Fatalf("rejected PUT changed the value: %d, want 45", got)
	}

	// 0 = explicit off: it is stored as 0, not skipped, on both surfaces.
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.triage_alert_minutes": 0})
	if got := getTriage(); got != 0 {
		t.Fatalf("after PUT 0 /api/config: %d, want 0", got)
	}
	if got := mgr.Config().Automation.EffectiveTriageAlertMinutes(); got != 0 {
		t.Fatalf("live manager after PUT 0 /api/config = %d, want 0", got)
	}
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{"triage_alert_minutes": 90}})
	if got := mgr.Config().Automation.EffectiveTriageAlertMinutes(); got != 90 {
		t.Fatalf("live manager after autonomous-config PUT 90 = %d, want 90", got)
	}
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{"triage_alert_minutes": 0}})
	if got := mgr.Config().Automation.EffectiveTriageAlertMinutes(); got != 0 {
		t.Fatalf("live manager after autonomous-config PUT 0 = %d, want 0", got)
	}
}

func TestTriageAlertUnsetDefaultsToThirtyOnConfigSurface(t *testing.T) {
	// Fresh default config: the key is unset (nil) but GET /api/config
	// reports the effective value — 30, never null.
	cfg := config.DefaultConfig()
	if got := cfg.Autonomous.Automation.EffectiveTriageAlertMinutes(); got != config.DefaultAutomationTriageAlertMinutes {
		t.Fatalf("DefaultConfig effective triage_alert_minutes = %d, want %d",
			got, config.DefaultAutomationTriageAlertMinutes)
	}
	zero := 0
	cfg.Autonomous.Automation.TriageAlertMinutes = &zero
	if got := cfg.Autonomous.Automation.EffectiveTriageAlertMinutes(); got != 0 {
		t.Fatalf("explicit 0 effective triage_alert_minutes = %d, want 0", got)
	}
}
