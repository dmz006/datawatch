// Requirements surface comm verbs, proxied against an httptest
// parent — the verbs read the REST routes over the loopback the same way
// the onboarding report verb does, and the requirements report text must
// come out of the shared autonomous renderer fed with the real JSON
// shape, not a hand-mirrored view.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// reqSurfDaemon answers every request with status and body and records the
// last request's method, path, query and body.
func reqSurfDaemon(t *testing.T, status int, body string) (*reqSurfRec, int) {
	t.Helper()
	rec := &reqSurfRec{}
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		rec.calls++
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		rec.body = string(raw)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	return rec, port
}

type reqSurfRec struct {
	calls         int
	method, path, query, body string
}

// requirementsSurfBody builds a real requirements report the way the engine
// builds it (same call the REST route makes) and serializes it, so the
// daemon answers with exactly the shape the route would.
func requirementsSurfBody(t *testing.T, meet bool) string {
	t.Helper()
	rep := autonomous.BuildRequirementsReport(autonomous.RequirementsReportInput{
		Nodes: map[string][]autonomous.ReportCapabilityNode{
			autonomous.CapabilityClassificationVerdict: {
				{Name: "n1", Found: "0.36.0", Required: "0.35.0", Enabled: meet},
				{Name: "n2", Found: "0.30.0", Required: "0.35.0", Enabled: true, Flagged: true},
			},
		},
		Backends: autonomous.AutomationBackends{
			Execution:           []string{"ollama", "pi-prompt"},
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

const consentSurfBody = `
{"models":[
  {"name":"qwen3:8b","allowed":true},
  {"name":"llama3.1:70b","allowed":false,"note":"too big for the pool"}],
 "publishers":["mistral"],
 "decision_rule":"A pull is allowed when the model is listed with allowed: true, or when the model is not listed and its publisher is on the publisher allow-list; every other pull is refused."}`

const nodeSurfBody = `
{"name":"n1","kind":"ollama","address":"ollama1:11434",
 "backend":"Ollama","backend_version":"0.36.0","version_probed_at":"2026-11-01T10:00:00Z",
 "capabilities":[
  {"capability":"classification_verdict","minimum":"0.35.0","ok":true},
  {"capability":"repo_ci_watch","minimum":"0.40.0","ok":false,
   "reason":"0.36.0 < 0.40.0"}]}`

func TestRequirementsSurfaceComm_ReportRendersFromRealShape(t *testing.T) {
	rec, port := reqSurfDaemon(t, http.StatusOK, requirementsSurfBody(t, true))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous requirements-report"), " ")
	if rec.method != http.MethodGet || rec.path != "/api/automation/requirements-report" || rec.query != "format=json" {
		t.Errorf("proxied %s %s?%s, want GET on the requirements report route", rec.method, rec.path, rec.query)
	}
	for _, want := range []string{
		"Automation requirements",
		"classification_verdict",
		"Ollama minimum: 0.35.0 (built-in table)",
		"node n1: Ollama 0.36.0, requires 0.35.0 — enabled, meets the minimum",
		"node n2: Ollama 0.30.0, requires 0.35.0 — enabled, FLAGGED: below the minimum (fails closed)",
		"satisfied",
		"execution: ollama, pi-prompt",
		"planning: autonomous",
		"verification: ollama",
		"claude configured: yes",
		"local model pool configured: no",
		"All requirements are met.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
}

func TestRequirementsSurfaceComm_ReportUnmetIsNamed(t *testing.T) {
	rec, port := reqSurfDaemon(t, http.StatusOK, requirementsSurfBody(t, false))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous requirements-report"), " ")
	if rec.path != "/api/automation/requirements-report" {
		t.Errorf("path = %s", rec.path)
	}
	for _, want := range []string{
		"disabled",
		"not satisfied",
		"requirement(s) unmet:",
		"classification_verdict: no node enabled with Ollama 0.35.0 or later",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "All requirements are met") {
		t.Error("an unmet report must not say all requirements are met")
	}
}

func TestRequirementsSurfaceComm_ReportVariantsAndErrors(t *testing.T) {
	_, port := reqSurfDaemon(t, http.StatusOK, requirementsSurfBody(t, true))
	r := setupPeerRouter(t, port)
	for _, msg := range []string{"autonomous requirements_report", "autonomous requirements-report text"} {
		if got := strings.Join(r.HandleTestMessage(msg), " "); !strings.Contains(got, "Automation requirements") {
			t.Errorf("%q: %q", msg, got)
		}
	}
	got := strings.Join(r.HandleTestMessage("autonomous requirements-report json"), " ")
	if !strings.Contains(got, `"capabilities"`) || !strings.Contains(got, `"ok":true`) {
		t.Errorf("json reply lost the REST body: %q", got)
	}
	for _, msg := range []string{"autonomous requirements-report xml", "autonomous requirements-report json extra"} {
		if got := strings.Join(r.HandleTestMessage(msg), " "); !strings.Contains(got, "usage:") {
			t.Errorf("%q: %q", msg, got)
		}
	}

	_, port = reqSurfDaemon(t, http.StatusInternalServerError, "boom")
	r = setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous requirements-report"), " "); !strings.Contains(got, "failed") {
		t.Errorf("error reply = %q", got)
	}

	_, port = reqSurfDaemon(t, http.StatusOK, "not json")
	r = setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous requirements-report"), " "); !strings.Contains(got, "decode report") {
		t.Errorf("bad body reply = %q", got)
	}
}

func TestRequirementsSurfaceComm_ConsentGetRendersPolicy(t *testing.T) {
	rec, port := reqSurfDaemon(t, http.StatusOK, consentSurfBody)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous model-pull-consent-get"), " ")
	if rec.method != http.MethodGet || rec.path != "/api/automation/model-consent" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	for _, want := range []string{
		"Model pull consent",
		"rule: A pull is allowed",
		"qwen3:8b allowed",
		"llama3.1:70b refused - too big for the pool",
		"publishers: mistral",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
}

func TestRequirementsSurfaceComm_ConsentSetSendsCompletePolicy(t *testing.T) {
	rec, port := reqSurfDaemon(t, http.StatusOK, `{"status":"ok"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage(
		"autonomous model-pull-consent-set model=qwen3:8b deny=llama3.1:70b publisher=mistral"), " ")
	if rec.method != http.MethodPut || rec.path != "/api/automation/model-consent" {
		t.Fatalf("proxied %s %s%q, want PUT on the consent route", rec.method, rec.path, rec.query)
	}
	var body struct {
		Models     []struct {
			Name    string `json:"name"`
			Allowed bool   `json:"allowed"`
			Note    string `json:"note"`
		} `json:"models"`
		Publishers []string `json:"publishers"`
	}
	if err := json.Unmarshal([]byte(rec.body), &body); err != nil {
		t.Fatalf("PUT body is not JSON: %v: %q", err, rec.body)
	}
	if len(body.Models) != 2 || body.Models[0].Name != "qwen3:8b" || !body.Models[0].Allowed {
		t.Errorf("models = %+v", body.Models)
	}
	if body.Models[1].Name != "llama3.1:70b" || body.Models[1].Allowed {
		t.Errorf("denied model = %+v", body.Models[1])
	}
	if len(body.Publishers) != 1 || body.Publishers[0] != "mistral" {
		t.Errorf("publishers = %v", body.Publishers)
	}
	if !strings.Contains(got, "ok") && !strings.Contains(got, "model pull consent set") {
		t.Errorf("reply = %q", got)
	}

	// clear sends an empty policy.
	rec.method, rec.body, rec.calls = "", "", 0
	got = strings.Join(r.HandleTestMessage("autonomous model-pull-consent-set clear"), " ")
	var clearBody struct {
		Models     []any    `json:"models"`
		Publishers []string `json:"publishers"`
	}
	if err := json.Unmarshal([]byte(rec.body), &clearBody); err != nil {
		t.Fatalf("clear body is not JSON: %v: %q", err, rec.body)
	}
	if len(clearBody.Models) != 0 || len(clearBody.Publishers) != 0 {
		t.Errorf("clear should send an empty policy: %v", rec.body)
	}
	if !strings.Contains(got, "ok") && !strings.Contains(got, "model pull consent set") {
		t.Errorf("clear reply = %q", got)
	}

	// A server refusal comes back as a failure, not success.
	_, port = reqSurfDaemon(t, http.StatusBadRequest, "model name listed twice: qwen3:8b")
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous model-pull-consent-set model=qwen3:8b"), " ")
	if !strings.Contains(got, "failed") || !strings.Contains(got, "twice") {
		t.Errorf("refusal reply = %q", got)
	}
}

func TestRequirementsSurfaceComm_ConsentSetUsage(t *testing.T) {
	for _, msg := range []string{
		"autonomous model-pull-consent-set",
		"autonomous model-pull-consent-set model=qwen3:8b model=qwen3:8b",
		"autonomous model-pull-consent-set clear model=qwen3:8b",
		"autonomous model-pull-consent-set model=",
		"autonomous model-pull-consent-set bogus",
	} {
		_, port := reqSurfDaemon(t, http.StatusOK, `{}`)
		r := setupPeerRouter(t, port)
		if got := strings.Join(r.HandleTestMessage(msg), " "); !strings.Contains(got, "failed") {
			t.Errorf("%q: %q", msg, got)
		}
	}
}

func TestRequirementsSurfaceComm_NodeVersionsRenders(t *testing.T) {
	rec, port := reqSurfDaemon(t, http.StatusOK, nodeSurfBody)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous node-versions n1"), " ")
	if rec.method != http.MethodGet || rec.path != "/api/compute/nodes/n1/versions" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	for _, want := range []string{
		"Node n1 (kind ollama, ollama1:11434)",
		"backend: Ollama 0.36.0 (recorded 2026-11-01T10:00:00Z)",
		"classification_verdict: requirement 0.35.0 - meets the minimum",
		"repo_ci_watch: requirement 0.40.0 - fails the minimum - 0.36.0 < 0.40.0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
}

func TestRequirementsSurfaceComm_NodeVersionsVariants(t *testing.T) {
	_, port := reqSurfDaemon(t, http.StatusNotFound, "node not found: nope")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous node-versions nope"), " ")
	if !strings.Contains(got, "not found") {
		t.Errorf("404 reply = %q", got)
	}

	_, port = reqSurfDaemon(t, http.StatusOK, nodeSurfBody)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous node_versions n1"), " ")
	if !strings.Contains(got, "Node n1") {
		t.Errorf("underscore alias reply = %q", got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous node-versions"), " ")
	if !strings.Contains(got, "usage:") {
		t.Errorf("missing name reply = %q", got)
	}
}

func TestRequirementsSurfaceComm_VerbList(t *testing.T) {
	verbs := RequirementsSurfacesCommVerbs()
	for _, want := range []string{"requirements-report", "model-pull-consent-get", "model-pull-consent-set", "node-versions"} {
		if !slices.Contains(verbs, want) {
			t.Errorf("RequirementsSurfacesCommVerbs() = %v, must contain %q", verbs, want)
		}
	}
	// Every listed verb parses as an autonomous command (so the help and
	// the dispatch share one vocabulary).
	for _, v := range verbs {
		if typ := Parse("autonomous " + v).Type; typ != CmdAutonomous {
			t.Errorf("comm verb %q parses as %v, want the autonomous command", v, typ)
		}
	}
}
