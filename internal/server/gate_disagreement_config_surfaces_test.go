// autonomous.gate_disagreement — Configuration Accessibility round-trip,
// mirroring the block_on_file_drift / max_concurrent_tasks surfaces:
// GET /api/config must show "warn" by default, PUT /api/config "block"
// must persist and reach the live manager, an invalid value must be a
// 400, and the PRD/task JSON the PWA / MCP readers consume must carry
// quality_gate_status / quality_gate_invalid_reason (PRD) and
// gate_disagreement (task).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func getAutonomousGateDisagreement(t *testing.T, s *Server) string {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["gate_disagreement"]
	if !ok {
		t.Fatal("GET /api/config autonomous map lacks gate_disagreement")
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode gate_disagreement: %v", err)
	}
	return v
}

func TestGateDisagreement_GetConfigDefaultIsWarn(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	if got := getAutonomousGateDisagreement(t, s); got != "warn" {
		t.Fatalf("GET /api/config autonomous.gate_disagreement = %q, want warn", got)
	}
	if got := mgr.Config().GateDisagreement; got != "warn" {
		t.Fatalf("live manager GateDisagreement = %q, want warn", got)
	}
}

func TestGateDisagreement_PutConfigRoundTripPersistsAndPropagates(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.gate_disagreement": "block"})
	if got := s.cfg.Autonomous.GateDisagreement; got != "block" {
		t.Fatalf("s.cfg.Autonomous.GateDisagreement = %q, want block", got)
	}
	if got := getAutonomousGateDisagreement(t, s); got != "block" {
		t.Fatalf("GET after PUT = %q, want block", got)
	}
	if got := mgr.Config().GateDisagreement; got != "block" {
		t.Fatalf("live manager GateDisagreement = %q, want block (propagation)", got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "gate_disagreement: block") {
		t.Fatalf("config.yaml lacks gate_disagreement: block:\n%s", raw)
	}
}

func TestGateDisagreement_PutConfigInvalidValueIs400(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	b, _ := json.Marshal(map[string]any{"autonomous.gate_disagreement": "nonsense"})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT nonsense: want 400, got %d (%s)", rr.Code, rr.Body.String())
	}
	bodyStr := rr.Body.String()
	if !strings.Contains(bodyStr, "warn") || !strings.Contains(bodyStr, "block") {
		t.Errorf("400 message should name the valid values; got %q", bodyStr)
	}
	// Nothing applied: stored value and live manager stay at the default.
	if got := s.cfg.Autonomous.GateDisagreement; got != "warn" {
		t.Fatalf("stored value changed to %q", got)
	}
	if got := mgr.Config().GateDisagreement; got != "warn" {
		t.Fatalf("live manager changed to %q", got)
	}
}

func TestGateDisagreement_PRDAndTaskJSONSurfaces(t *testing.T) {
	prd := autonomous.PRD{
		ID:                       "p1",
		QualityGateStatus:        "invalid",
		QualityGateInvalidReason: "quality gate command produced no output",
		Story: []autonomous.Story{{
			ID: "s1",
			Tasks: []autonomous.Task{
				{ID: "t1", GateDisagreement: &autonomous.GateDisagreement{
					VerifierOK: true, GateRegression: true, GateInvalid: false, Policy: "warn",
				}},
				{ID: "t2"},
			},
		}},
	}
	b, err := json.Marshal(prd)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		QualityGateStatus        string `json:"quality_gate_status"`
		QualityGateInvalidReason string `json:"quality_gate_invalid_reason"`
		Stories                  []struct {
			Tasks []map[string]json.RawMessage `json:"tasks"`
		} `json:"stories"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.QualityGateStatus != "invalid" {
		t.Errorf("prd quality_gate_status = %q, want invalid", got.QualityGateStatus)
	}
	if got.QualityGateInvalidReason == "" {
		t.Error("prd quality_gate_invalid_reason missing")
	}
	tasks := got.Stories[0].Tasks
	if _, ok := tasks[0]["gate_disagreement"]; !ok {
		t.Errorf("task t1 lacks gate_disagreement: %v", tasks[0])
	}
	if _, ok := tasks[1]["gate_disagreement"]; ok {
		t.Errorf("task t2 (no disagreement) must omit gate_disagreement: %v", tasks[1])
	}
}
