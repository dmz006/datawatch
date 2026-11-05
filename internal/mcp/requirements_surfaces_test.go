// Requirements surface MCP tools: the requirements report, the model-pull
// consent policy (read and set) and the compute node backend versions,
// proxied against an httptest backend.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

func reqSurfReportBody(t *testing.T, meet bool) string {
	t.Helper()
	rep := autonomous.BuildRequirementsReport(autonomous.RequirementsReportInput{
		Nodes: map[string][]autonomous.ReportCapabilityNode{
			autonomous.CapabilityClassificationVerdict: {
				{Name: "n1", Found: "0.36.0", Required: "0.35.0", Enabled: meet},
			},
		},
		Backends: autonomous.AutomationBackends{
			Execution:           []string{"ollama"},
			Planning:            []string{"autonomous"},
			Verification:        []string{"ollama"},
			ClaudeConfigured:    true,
			LocalPoolConfigured: false,
		},
	})
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const reqSurfConsentJSON = `{
"models":[{"name":"qwen3:8b","allowed":true},{"name":"llama3.1:70b","allowed":false,"note":"too big for the pool"}],
"publishers":["mistral"],
"decision_rule":"A pull is allowed when the model is listed with allowed: true, or unlisted with an allow-listed publisher; otherwise refused."}`

const reqSurfNodeJSON = `{
"name":"n1","kind":"ollama","address":"ollama1:11434",
"backend":"Ollama","backend_version":"0.36.0","version_probed_at":"2026-11-01T10:00:00Z",
"capabilities":[
 {"capability":"classification_verdict","minimum":"0.35.0","ok":true},
 {"capability":"repo_ci_watch","minimum":"0.40.0","ok":false,"reason":"0.36.0 < 0.40.0"}]}`

// TestRequirementsReportMCP_Text is the shape bug regression: the tool must
// decode the REST report and render it with the shared renderer, not echo a
// hand-mirrored shape.
func TestRequirementsReportMCP_Text(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, reqSurfReportBody(t, true))
	res, err := s.handleAutomationRequirementsReport(context.Background(), callReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodGet || rec.path != "/api/automation/requirements-report" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	text := automationText(t, res)
	for _, want := range []string{
		"Automation requirements",
		"Ollama minimum: 0.35.0 (built-in table)",
		"node n1: Ollama 0.36.0, requires 0.35.0",
		"enabled, meets the minimum",
		"satisfied",
		"execution: ollama",
		"planning: autonomous",
		"verification: ollama",
		"claude configured: yes",
		"local model pool configured: no",
		"All requirements are met.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "requirement(s) unmet") {
		t.Errorf("a satisfied report must not list unmet requirements:\n%s", text)
	}
}

func TestRequirementsReportMCP_TextUnmet(t *testing.T) {
	s, _ := activateServer(t, http.StatusOK, reqSurfReportBody(t, false))
	res, err := s.handleAutomationRequirementsReport(context.Background(), callReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	text := automationText(t, res)
	for _, want := range []string{
		"disabled",
		"not satisfied",
		"requirement(s) unmet:",
		"classification_verdict: no node enabled with Ollama 0.35.0 or later",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("unmet report lacks %q:\n%s", want, text)
		}
	}
}

func TestRequirementsReportMCP_JSONPassthrough(t *testing.T) {
	body := reqSurfReportBody(t, true)
	s, _ := activateServer(t, http.StatusOK, body)
	res, err := s.handleAutomationRequirementsReport(context.Background(), callReq(map[string]any{"format": "json"}))
	if err != nil {
		t.Fatal(err)
	}
	text := automationText(t, res)
	if text != body {
		t.Errorf("json format must pass the report body through unchanged:\n%s", text)
	}
	var rep autonomous.RequirementsReport
	if err := json.Unmarshal([]byte(text), &rep); err != nil {
		t.Fatalf("json reply is not a valid report: %v", err)
	}
	if !rep.OK || len(rep.Capabilities) != 1 || rep.Capabilities[0].Capability != "classification_verdict" {
		t.Errorf("decoding the reply back gives %v", rep)
	}
}

func TestRequirementsReportMCP_Errors(t *testing.T) {
	// A bad format is refused locally before any HTTP round-trip.
	s, _ := activateServer(t, http.StatusOK, reqSurfReportBody(t, true))
	res, err := s.handleAutomationRequirementsReport(context.Background(), callReq(map[string]any{"format": "xml"}))
	if err != nil {
		t.Fatal(err)
	}
	if text := automationText(t, res); !strings.Contains(text, "format must be text or json") {
		t.Errorf("bad format text = %q", text)
	}

	// A body that is not a report is surfaced as a decode failure, not a
	// blank reply.
	s, _ = activateServer(t, http.StatusOK, "not json")
	res, err = s.handleAutomationRequirementsReport(context.Background(), callReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	text := automationText(t, res)
	if !strings.Contains(text, "decode report") {
		t.Errorf("bad body text = %q", text)
	}

	// An upstream 500 carries the status and the body.
	s, _ = activateServer(t, http.StatusInternalServerError, "boom")
	res, err = s.handleAutomationRequirementsReport(context.Background(), callReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	text = automationText(t, res)
	if !strings.Contains(text, "HTTP 500") || !strings.Contains(text, "boom") {
		t.Errorf("500 text = %q", text)
	}
}

func TestModelPullConsentGetMCP(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, reqSurfConsentJSON)
	res, err := s.handleModelPullConsentGet(context.Background(), callReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodGet || rec.path != "/api/automation/model-consent" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	text := automationText(t, res)
	for _, want := range []string{
		"Model pull consent",
		"rule: A pull is allowed",
		"qwen3:8b allowed",
		"llama3.1:70b refused — too big for the pool",
		"publishers: mistral",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}

	// A server-side rejection (duplicate model name) is surfaced as the
	// error text, with the status and the upstream body.
	s, _ = activateServer(t, http.StatusBadRequest, "model name listed twice: qwen3:8b")
	res, err = s.handleModelPullConsentGet(context.Background(), callReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	text = automationText(t, res)
	if !strings.Contains(text, "HTTP 400") || !strings.Contains(text, "qwen3:8b") {
		t.Errorf("400 text = %q", text)
	}
}

func TestModelPullConsentSetMCP(t *testing.T) {
	// models and publishers arrive as JSON strings (WithString args).
	s, rec := activateServer(t, http.StatusOK, `{"status":"ok"}`)
	res, err := s.handleModelPullConsentSet(context.Background(), callReq(map[string]any{
		"models":     `["qwen3:8b","mistral:7b"]`,
		"publishers": `["mistral"]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodPut || rec.path != "/api/automation/model-consent" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	models, _ := rec.body["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("body models = %v", rec.body["models"])
	}
	for i, want := range []string{"qwen3:8b", "mistral:7b"} {
		m, _ := models[i].(map[string]any)
		if m["name"] != want || m["allowed"] != true {
			t.Errorf("body models[%d] = %v", i, m)
		}
	}
	pubs, _ := rec.body["publishers"].([]any)
	if len(pubs) != 1 || pubs[0] != "mistral" {
		t.Errorf("body publishers = %v", rec.body["publishers"])
	}
	if text := automationText(t, res); !strings.Contains(text, "model pull consent set") {
		t.Errorf("ack text = %q", text)
	}

	// Object entries (name / allowed / note) pass through for the
	// refusal-with-note composition.
	s, rec = activateServer(t, http.StatusOK, `{"status":"ok"}`)
	_, err = s.handleModelPullConsentSet(context.Background(), callReq(map[string]any{
		"models": `[{"name":"qwen3:8b","allowed":true},{"name":"llama3.1:70b","allowed":false,"note":"too big for the pool"}]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	models, _ = rec.body["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("body models = %v", rec.body["models"])
	}
	m1, _ := models[1].(map[string]any)
	if m1["name"] != "llama3.1:70b" || m1["allowed"] != false || m1["note"] != "too big for the pool" {
		t.Errorf("model[1] = %v", m1)
	}

	// Local input failures are refused without a plausible proxy call.
	s, _ = activateServer(t, http.StatusOK, `{"status":"ok"}`)
	res, err = s.handleModelPullConsentSet(context.Background(), callReq(map[string]any{"models": "not json"}))
	if err != nil {
		t.Fatal(err)
	}
	if text := automationText(t, res); !strings.Contains(text, "models must be a JSON array") {
		t.Errorf("bad models text = %q", text)
	}

	res, err = s.handleModelPullConsentSet(context.Background(), callReq(map[string]any{"models": `[{"allowed":true}]`}))
	if err != nil {
		t.Fatal(err)
	}
	if text := automationText(t, res); !strings.Contains(text, "non-empty") {
		t.Errorf("nameless entry text = %q", text)
	}

	res, err = s.handleModelPullConsentSet(context.Background(), callReq(map[string]any{"models": "[1,2]"}))
	if err != nil {
		t.Fatal(err)
	}
	if text := automationText(t, res); !strings.Contains(text, "name strings") {
		t.Errorf("bad item text = %q", text)
	}

	// A server-side rejection (duplicate model name) surfaces the status and body.
	s, _ = activateServer(t, http.StatusBadRequest, "model name listed twice: qwen3:8b")
	res, err = s.handleModelPullConsentSet(context.Background(), callReq(map[string]any{"models": `["qwen3:8b","qwen3:8b"]`}))
	if err != nil {
		t.Fatal(err)
	}
	text := automationText(t, res)
	if !strings.Contains(text, "HTTP 400") || !strings.Contains(text, "qwen3:8b") {
		t.Errorf("400 text = %q", text)
	}
}

func TestNodeBackendVersionsMCP(t *testing.T) {
	// name is required — refused locally with an actionable error.
	s, _ := activateServer(t, http.StatusOK, reqSurfNodeJSON)
	res, err := s.handleComputeNodeBackendVersions(context.Background(), callReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if text := automationText(t, res); !strings.Contains(text, "name is required") {
		t.Errorf("no-name text = %q", text)
	}

	// A named node reaches the exact route and renders the shared text.
	s, rec := activateServer(t, http.StatusOK, reqSurfNodeJSON)
	res, err = s.handleComputeNodeBackendVersions(context.Background(), callReq(map[string]any{"name": "n1"}))
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodGet || rec.path != "/api/compute/nodes/n1/versions" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	text := automationText(t, res)
	for _, want := range []string{
		"Node n1 (kind ollama, ollama1:11434)",
		"backend: Ollama 0.36.0 (recorded 2026-11-01T10:00:00Z)",
		"classification_verdict: requirement 0.35.0 — meets the minimum",
		"repo_ci_watch: requirement 0.40.0 — fails the minimum — 0.36.0 < 0.40.0",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}

	// A node with no recorded version still renders the identity lines.
	noVer := `{"name":"nope","kind":"local","address":"192.0.2.9:22","backend":"","capabilities":[]}`
	s, _ = activateServer(t, http.StatusOK, noVer)
	res, err = s.handleComputeNodeBackendVersions(context.Background(), callReq(map[string]any{"name": "nope"}))
	if err != nil {
		t.Fatal(err)
	}
	text = automationText(t, res)
	for _, want := range []string{"Node nope (kind local, 192.0.2.9:22)", "no version recorded", "no capability minimums apply"} {
		if !strings.Contains(text, want) {
			t.Errorf("no-version text lacks %q:\n%s", want, text)
		}
	}

	// An unknown node is surfaced as a 404.
	s, _ = activateServer(t, http.StatusNotFound, "unknown node nope")
	res, err = s.handleComputeNodeBackendVersions(context.Background(), callReq(map[string]any{"name": "nope"}))
	if err != nil {
		t.Fatal(err)
	}
	text = automationText(t, res)
	if !strings.Contains(text, "HTTP 404") || !strings.Contains(text, "unknown node nope") {
		t.Errorf("404 text = %q", text)
	}
}

func TestRequirementsSurfacesToolNames(t *testing.T) {
	got := RequirementsSurfacesToolNames()
	for _, want := range []string{
		"automation_requirements_report",
		"automation_model_pull_consent_get",
		"automation_model_pull_consent_set",
		"compute_node_backend_versions",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("RequirementsSurfacesToolNames() = %v, must contain %q", got, want)
		}
	}
}

func TestRequirementsSurfacesCaps(t *testing.T) {
	cases := map[string]string{
		"automation_requirements_report":    federation.CapAutonomousRead,
		"automation_model_pull_consent_get": federation.CapAutonomousRead,
		"automation_model_pull_consent_set": federation.CapConfigWrite,
		"compute_node_backend_versions":     federation.CapComputeRead,
	}
	for tool, want := range cases {
		got, ok := federation.RequiredCapForMCPTool(tool)
		if !ok {
			t.Errorf("%s: no cap mapped", tool)
			continue
		}
		if got != want {
			t.Errorf("%s: cap = %q, want %q", tool, got, want)
		}
	}
}
