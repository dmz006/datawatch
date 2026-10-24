// Gate scope setting (full | touched-plus-smoke | touched) and its
// companion smoke_packages key on every config surface, mirroring the
// gate_disagreement reference tests:
//   - PUT /api/config autonomous.default_quality_gates.{scope,smoke_packages}
//     (round-trip, invalid-scope 400, comma-separated string form)
//   - GET /api/config default_quality_gates exposes both keys
//   - POST /api/autonomous/prds/<id>/set_quality_gates carries both keys
//     and rejects an invalid scope with 400 naming the valid values
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func gateScopePut(t *testing.T, s *Server, patch map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(patch)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

// defaultQualityGatesGET decodes the GET /api/config
// autonomous.default_quality_gates object.
func defaultQualityGatesGET(t *testing.T, s *Server) map[string]json.RawMessage {
	t.Helper()
	raw := getAutonomousConfigMap(t, s)["default_quality_gates"]
	if len(raw) == 0 {
		t.Fatalf("GET /api/config: autonomous.default_quality_gates missing")
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode default_quality_gates: %v (%s)", err, raw)
	}
	return out
}

func TestGateScope_PutConfigRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	gateScopePut(t, s, map[string]any{
		"autonomous.default_quality_gates.scope":          "touched-plus-smoke",
		"autonomous.default_quality_gates.smoke_packages": []any{"internal/pipeline", "internal/server"},
	})

	qg := defaultQualityGatesGET(t, s)
	if got := string(qg["scope"]); got != `"touched-plus-smoke"` {
		t.Fatalf("GET scope = %s, want \"touched-plus-smoke\"", got)
	}
	var smoke []string
	if err := json.Unmarshal(qg["smoke_packages"], &smoke); err != nil {
		t.Fatalf("GET smoke_packages not a list: %v (%s)", err, qg["smoke_packages"])
	}
	if len(smoke) != 2 || smoke[0] != "internal/pipeline" || smoke[1] != "internal/server" {
		t.Fatalf("GET smoke_packages = %v", smoke)
	}

	// The live manager sees the new values.
	if got := mgr.Config().DefaultQualityGates.Scope; got != "touched-plus-smoke" {
		t.Fatalf("live manager Scope = %q, want touched-plus-smoke", got)
	}
	if got := mgr.Config().DefaultQualityGates.SmokePackages; len(got) != 2 || got[0] != "internal/pipeline" {
		t.Fatalf("live manager SmokePackages = %v", got)
	}

	// And it persists to config.yaml.
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "scope: touched-plus-smoke") {
		t.Errorf("config.yaml lacks scope: touched-plus-smoke:\n%s", raw)
	}
	if !strings.Contains(string(raw), "smoke_packages") {
		t.Errorf("config.yaml lacks smoke_packages:\n%s", raw)
	}
}

func TestGateScope_PutConfigAllValidScopesRoundTrip(t *testing.T) {
	for _, scope := range []string{"", "full", "touched-plus-smoke", "touched"} {
		s, mgr, _ := newMaxConcurrentTasksServer(t)
		rr := gateScopePut(t, s, map[string]any{
			"autonomous.default_quality_gates.scope": scope,
		})
		if rr.Code != http.StatusOK {
			t.Fatalf("scope %q: PUT = %d body=%s", scope, rr.Code, rr.Body)
		}
		if got := mgr.Config().DefaultQualityGates.Scope; got != scope {
			t.Fatalf("scope %q: live manager = %q", scope, got)
		}
		if got := string(defaultQualityGatesGET(t, s)["scope"]); got != `"`+scope+`"` {
			t.Fatalf("scope %q: GET = %s", scope, got)
		}
	}
}

func TestGateScope_PutConfigRejectsInvalidScope(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	rr := gateScopePut(t, s, map[string]any{
		"autonomous.default_quality_gates.scope": "half-suite",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid scope: want 400, got %d body=%s", rr.Code, rr.Body)
	}
	body := rr.Body.String()
	for _, v := range []string{"full", "touched-plus-smoke", "touched"} {
		if !strings.Contains(body, v) {
			t.Errorf("400 body does not name %q: %s", v, body)
		}
	}
	// The whole PUT is rejected before anything is applied.
	if s.cfg.Autonomous.DefaultQualityGates.Scope != "" {
		t.Errorf("rejected PUT still stored scope %q", s.cfg.Autonomous.DefaultQualityGates.Scope)
	}
	if mgr.Config().DefaultQualityGates.Scope != "" {
		t.Errorf("rejected PUT still set the live manager scope %q", mgr.Config().DefaultQualityGates.Scope)
	}
}

func TestGateScope_PutConfigSmokePackagesCommaString(t *testing.T) {
	// The comm/CLI wire form: a comma-separated string instead of a JSON list.
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	rr := gateScopePut(t, s, map[string]any{
		"autonomous.default_quality_gates.scope":          "touched",
		"autonomous.default_quality_gates.smoke_packages": " internal/pipeline , internal/autonomous ,",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d body=%s", rr.Code, rr.Body)
	}
	got := mgr.Config().DefaultQualityGates.SmokePackages
	if len(got) != 2 || got[0] != "internal/pipeline" || got[1] != "internal/autonomous" {
		t.Fatalf("SmokePackages = %v, want [internal/pipeline internal/autonomous]", got)
	}
}

func TestGateScope_PRDSetQualityGatesRoundTrip(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	prd, err := mgr.CreatePRD("gate scope prd", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"enabled":        true,
		"test_command":   "go test ./...",
		"scope":          "touched-plus-smoke",
		"smoke_packages": []string{"internal/pipeline"},
	})
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodPost,
		"/api/autonomous/prds/"+prd.ID+"/set_quality_gates", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("set_quality_gates = %d body=%s", rr.Code, rr.Body)
	}
	var resp struct {
		QualityGates *struct {
			Scope         string   `json:"scope"`
			SmokePackages []string `json:"smoke_packages"`
		} `json:"quality_gates"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (%s)", err, rr.Body)
	}
	if resp.QualityGates == nil {
		t.Fatalf("response lacks quality_gates: %s", rr.Body)
	}
	if resp.QualityGates.Scope != "touched-plus-smoke" {
		t.Errorf("quality_gates.scope = %q, want touched-plus-smoke", resp.QualityGates.Scope)
	}
	if len(resp.QualityGates.SmokePackages) != 1 || resp.QualityGates.SmokePackages[0] != "internal/pipeline" {
		t.Errorf("quality_gates.smoke_packages = %v", resp.QualityGates.SmokePackages)
	}
	// Empty scope stays inherit and still succeeds.
	body2, _ := json.Marshal(map[string]any{"scope": ""})
	rr2 := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr2, httptest.NewRequest(http.MethodPost,
		"/api/autonomous/prds/"+prd.ID+"/set_quality_gates", bytes.NewReader(body2)))
	if rr2.Code != http.StatusOK {
		t.Fatalf("empty scope set_quality_gates = %d body=%s", rr2.Code, rr2.Body)
	}
}

func TestGateScope_PRDSetQualityGatesRejectsInvalidScope(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	prd, err := mgr.CreatePRD("gate scope prd", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"scope": "most"})
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodPost,
		"/api/autonomous/prds/"+prd.ID+"/set_quality_gates", bytes.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid scope: want 400, got %d body=%s", rr.Code, rr.Body)
	}
	for _, v := range []string{"full", "touched-plus-smoke", "touched"} {
		if !strings.Contains(rr.Body.String(), v) {
			t.Errorf("400 body does not name %q: %s", v, rr.Body)
		}
	}
}
