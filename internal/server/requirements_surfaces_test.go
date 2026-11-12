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
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

// fakeRequirementsReporter is a test stand-in for the requirements engine.
type fakeRequirementsReporter struct {
	in autonomous.RequirementsReportInput
}

func (f fakeRequirementsReporter) RequirementsReport(b autonomous.AutomationBackends) autonomous.RequirementsReport {
	in := f.in
	in.Backends = b
	return autonomous.BuildRequirementsReport(in)
}

func newRequirementsServer(t *testing.T) (*Server, *compute.Registry, *audit.Log) {
	t.Helper()
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })

	add := map[string]any{"name": "gpu-a", "kind": "remote", "address": "http://ollama:11434"}
	body, _ := json.Marshal(add)
	w := httptest.NewRecorder()
	cfg := config.DefaultConfig()
	s := &Server{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.yaml"), computeReg: reg, auditLog: al}
	s.handleComputeNodes(w, httptest.NewRequest(http.MethodPost, "/api/compute/nodes?probe=skip", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("add compute node: %d %s", w.Code, w.Body.String())
	}
	return s, reg, al
}

func TestRequirementsReportGET_JSON(t *testing.T) {
	s, _, _ := newRequirementsServer(t)
	s.SetRequirementsReporter(fakeRequirementsReporter{
		in: autonomous.RequirementsReportInput{
			Nodes: map[string][]autonomous.ReportCapabilityNode{
				"classification_verdict": {{Name: "gpu-a", Found: "0.40.5", Required: "0.35.0", Enabled: true}},
			},
		},
	})
	w := httptest.NewRecorder()
	s.handleRequirementsReport(w, httptest.NewRequest(http.MethodGet, "/api/automation/requirements-report", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}
	var rep autonomous.RequirementsReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, w.Body.String())
	}
	if len(rep.Capabilities) != 1 || rep.Capabilities[0].Capability != "classification_verdict" {
		t.Fatalf("capabilities = %+v", rep.Capabilities)
	}
	if rep.Capabilities[0].MinimumOllama != "0.35.0" || !rep.Capabilities[0].Satisfied {
		t.Errorf("capability line = %+v", rep.Capabilities[0])
	}
	joined := strings.Join(rep.Missing, "\n")
	for _, want := range []string{"no execution backend", "no planning backend", "no verification backend", "neither a Claude backend nor a local model pool"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing list lacks %q:\n%s", want, joined)
		}
	}
	if rep.OK {
		t.Errorf("report should not be OK with unmet backend minimums")
	}
}

func TestRequirementsReportGET_Text(t *testing.T) {
	s, _, _ := newRequirementsServer(t)
	s.SetRequirementsReporter(fakeRequirementsReporter{
		in: autonomous.RequirementsReportInput{
			Nodes: map[string][]autonomous.ReportCapabilityNode{
				"classification_verdict": {{Name: "gpu-a", Found: "0.40.5", Required: "0.35.0", Enabled: true}},
			},
		},
	})
	s.SetAutomationBackends(func() autonomous.AutomationBackends {
		return autonomous.AutomationBackends{
			Execution: []string{"claude-code"}, Planning: []string{"ollama"}, Verification: []string{"ollama"},
			ClaudeConfigured: true,
		}
	})
	w := httptest.NewRecorder()
	s.handleRequirementsReport(w, httptest.NewRequest(http.MethodGet, "/api/automation/requirements-report?format=text", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"Automation requirements",
		"classification_verdict",
		"Ollama minimum: 0.35.0 (built-in table)",
		"node gpu-a: Ollama 0.40.5, requires 0.35.0 — enabled, meets the minimum",
		"satisfied",
		"execution: claude-code",
		"All requirements are met",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("text lacks %q:\n%s", want, body)
		}
	}
}

func TestRequirementsReportNoEngine(t *testing.T) {
	s, _, _ := newRequirementsServer(t)
	w := httptest.NewRecorder()
	s.handleRequirementsReport(w, httptest.NewRequest(http.MethodGet, "/api/automation/requirements-report", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestModelConsentGET(t *testing.T) {
	s, _, _ := newRequirementsServer(t)
	s.cfg.Autonomous.ModelPullConsent = config.ModelPullConsent{
		Models:     []config.ModelConsentEntry{{Name: "claude-sonnet-4.5 (claude code)", Allowed: true, Note: "operator"}},
		Publishers: []string{"anthropic"},
	}
	w := httptest.NewRecorder()
	s.handleModelConsent(w, httptest.NewRequest(http.MethodGet, "/api/automation/model-consent", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}
	var view modelConsentView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(view.Models) != 1 || !view.Models[0].Allowed || view.Models[0].Note != "operator" {
		t.Errorf("models = %+v", view.Models)
	}
	if len(view.Publishers) != 1 || view.Publishers[0] != "anthropic" {
		t.Errorf("publishers = %+v", view.Publishers)
	}
	if !strings.Contains(view.DecisionRule, "allowed: true") {
		t.Errorf("decision rule = %q", view.DecisionRule)
	}
}

func TestModelConsentPUT_Valid(t *testing.T) {
	s, _, al := newRequirementsServer(t)
	body := `{"models":[{"name":"gpt-5","allowed":true,"note":"paid"},{"name":"deepseek-v3","allowed":false}],"publishers":["anthropic"]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/automation/model-consent", strings.NewReader(body))
	s.handleModelConsent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}
	got := s.cfg.Autonomous.ModelPullConsent
	if len(got.Models) != 2 || got.Models[0].Allowed != true || got.Models[1].Allowed != false {
		t.Fatalf("stored models = %+v", got.Models)
	}
	entries, err := al.Read(audit.QueryFilter{Action: audit.ActionModelPullConsentChanged})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("consent audit entries = %d", len(entries))
	}
	details := entries[0].Details
	models, _ := details["models"].([]any)
	joined := []string{}
	for _, m := range models {
		joined = append(joined, m.(string))
	}
	if !strings.Contains(strings.Join(joined, " "), "gpt-5=pull_allowed") || !strings.Contains(strings.Join(joined, " "), "deepseek-v3=pull_refused") {
		t.Errorf("audit models = %v", joined)
	}
}

func TestModelConsentPUT_Invalid(t *testing.T) {
	s, _, _ := newRequirementsServer(t)
	// A duplicated model name the gate could not interpret is a 400, like
	// the autonomous config surface.
	body := `{"models":[{"name":"dup","allowed":true},{"name":"dup","allowed":false}]}`
	w := httptest.NewRecorder()
	s.handleModelConsent(w, httptest.NewRequest(http.MethodPut, "/api/automation/model-consent", strings.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestNodeBackendVersions(t *testing.T) {
	s, reg, _ := newRequirementsServer(t)
	_ = reg.RecordBackendVersion("gpu-a", compute.VersionResult{Backend: "ollama", Raw: "Ollama version 0.40.5", Version: "0.40.5"})
	w := httptest.NewRecorder()
	s.handleNodeBackendVersions(w, httptest.NewRequest(http.MethodGet, "/api/compute/nodes/gpu-a/versions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}
	var nv nodeBackendVersions
	if err := json.Unmarshal(w.Body.Bytes(), &nv); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if nv.Name != "gpu-a" || nv.Kind != "remote" || nv.BackendVersion != "0.40.5" || nv.Backend != "ollama" {
		t.Errorf("identity = %+v", nv)
	}
	if len(nv.Capabilities) != 1 || nv.Capabilities[0].Capability != "classification_verdict" {
		t.Fatalf("capabilities = %+v", nv.Capabilities)
	}
	c := nv.Capabilities[0]
	if c.Minimum != "0.35.0" || c.OK == nil || !*c.OK {
		t.Errorf("line = %+v", c)
	}
}

func TestNodeBackendVersions_BelowMinimum(t *testing.T) {
	s, reg, _ := newRequirementsServer(t)
	_ = reg.RecordBackendVersion("gpu-a", compute.VersionResult{Backend: "ollama", Raw: "0.34.0", Version: "0.34.0"})
	w := httptest.NewRecorder()
	s.handleNodeBackendVersions(w, httptest.NewRequest(http.MethodGet, "/api/compute/nodes/gpu-a/versions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var nv nodeBackendVersions
	if err := json.Unmarshal(w.Body.Bytes(), &nv); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	c := nv.Capabilities[0]
	if *c.OK {
		t.Errorf("line = %+v", c)
	}
	if !strings.Contains(c.Reason, "gpu-a") || !strings.Contains(c.Reason, "0.35.0") {
		t.Errorf("reason = %q", c.Reason)
	}
}

func TestNodeBackendVersions_UnknownVersionFailsClosed(t *testing.T) {
	s, reg, _ := newRequirementsServer(t)
	_ = reg.RecordBackendVersion("gpu-a", compute.VersionResult{Backend: "ollama", Raw: "unknown-no-version", Version: ""}) // no semver the gate can compare
	w := httptest.NewRecorder()
	s.handleNodeBackendVersions(w, httptest.NewRequest(http.MethodGet, "/api/compute/nodes/gpu-a/versions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var nv nodeBackendVersions
	if err := json.Unmarshal(w.Body.Bytes(), &nv); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	c := nv.Capabilities[0]
	if !c.Unknown || *c.OK {
		t.Errorf("unknown version must fail closed: %+v", c)
	}
}

func TestNodeBackendVersions_UnknownNode(t *testing.T) {
	s, _, _ := newRequirementsServer(t)
	w := httptest.NewRecorder()
	s.handleNodeBackendVersions(w, httptest.NewRequest(http.MethodGet, "/api/compute/nodes/ghost/versions", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
