package autonomous

// Live, opt-in smoke for the Automation model-tier operations set_llm (execution
// and planning defaults) and set_verification_llm (verification defaults). It
// drives a SANDBOX daemon over its REST API, in the same way as
// TestAutomationControlLiveSmoke in automation_control_live_smoke_test.go, and
// shares the client helpers of automation_live_smoke_test.go.
//
// It is skipped unless DW_LIVE_AUTOMATION_MODEL_TIER_SMOKE is set, so `go test ./...`
// in CI skips it. The variable has no default: its value is the base URL of the
// sandbox daemon the operator stood up, so the test can never reach production by
// accident. Environment:
//
//	DW_LIVE_AUTOMATION_MODEL_TIER_SMOKE=https://host:port  base URL of the sandbox daemon
//	DW_BASE_TOKEN=...                                      bearer token, if the daemon has one
//	DW_BASE_INSECURE=0                                     verify TLS (default: skip verify)
//	DW_LIVE_AUTOMATION_PROJECT_DIR=...                     project_dir for the smoke PRD (optional)
//
// Run it against a sandbox daemon the operator started:
//
//	DW_LIVE_AUTOMATION_MODEL_TIER_SMOKE=https://127.0.0.1:18443 \
//	  go test -count=1 -run TestAutomationModelTierLiveSmoke -v ./internal/autonomous/
//
// The backend and model values are the ones testdata/datawatch.yaml lists for the
// sandbox (the llms ollama-local-node and ollama-localhost, model qwen3:1.7b),
// so the daemon's backend lookup accepts them.
//
// The test only calls the REST API of that daemon and runs no LLM work. The
// Automation is a draft with one member, never activated, and it is deleted at
// the end; the draft member PRD is left in place and logged.
//
// The operator's live run of this smoke is still owed: it has been compiled and
// checked to skip, but not yet run against a sandbox daemon.

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

const modelTierSmokeEnv = "DW_LIVE_AUTOMATION_MODEL_TIER_SMOKE"

// modelTierSmokeAutomation is the model-tier subset of an Automation record.
type modelTierSmokeAutomation struct {
	ID                  string `json:"id"`
	Version             int64  `json:"version"`
	Backend             string `json:"backend"`
	Model               string `json:"model"`
	Effort              string `json:"effort"`
	PlanningBackend     string `json:"planning_backend"`
	PlanningModel       string `json:"planning_model"`
	VerificationBackend string `json:"verification_backend"`
	VerificationModel   string `json:"verification_model"`
	VerificationEffort  string `json:"verification_effort"`
}

func modelTierSmokeDecode(t *testing.T, step string, out []byte) modelTierSmokeAutomation {
	t.Helper()
	var a modelTierSmokeAutomation
	if err := json.Unmarshal(out, &a); err != nil {
		t.Fatalf("%s: decode automation: %v body=%s", step, err, out)
	}
	return a
}

func modelTierSmokeGet(t *testing.T, c *smokeClient, id string) modelTierSmokeAutomation {
	t.Helper()
	return modelTierSmokeDecode(t, "get "+id, c.must(t, http.MethodGet, "/api/automations/"+id, nil, http.StatusOK))
}

// modelTierSmokePost POSTs one operation and expects want. On 200 it returns the
// decoded record; on any other expected status it returns the zero record.
func modelTierSmokePost(t *testing.T, c *smokeClient, step, id, op string, body map[string]any, want int) modelTierSmokeAutomation {
	t.Helper()
	status, out := c.do(t, http.MethodPost, "/api/automations/"+id+"/"+op, body)
	t.Logf("%s: POST /api/automations/%s/%s -> %d %s", step, id, op, status, strings.TrimSpace(string(out)))
	if status != want {
		t.Fatalf("%s: %s returned %d, want %d body=%s", step, op, status, want, out)
	}
	if status != http.StatusOK {
		return modelTierSmokeAutomation{}
	}
	return modelTierSmokeDecode(t, step, out)
}

func TestAutomationModelTierLiveSmoke(t *testing.T) {
	base := strings.TrimRight(os.Getenv(modelTierSmokeEnv), "/")
	if base == "" {
		t.Skip("set DW_LIVE_AUTOMATION_MODEL_TIER_SMOKE=https://host:port of a sandbox daemon to run the automation model-tier live smoke")
	}
	c := newSmokeClient(base, os.Getenv("DW_BASE_TOKEN"))
	projectDir := os.Getenv("DW_LIVE_AUTOMATION_PROJECT_DIR")

	// Values the sandbox configuration (testdata/datawatch.yaml) lists.
	const (
		execBackend     = "ollama-localhost"
		execModel       = "qwen3:1.7b"
		execEffort      = "high"
		planBackend     = "ollama-local-node"
		planModel       = "qwen3:1.7b"
		verifyBackend   = "ollama-local-node"
		verifyModel     = "qwen3:1.7b"
		verifyEffort    = "low"
		smokeActor      = "operator"
		smokeReasonText = "model-tier live smoke"
	)

	// (1) A draft Automation with one member.
	prd := smokeCreatePRD(t, c, "model tier smoke m1", projectDir)
	a := smokeCreate(t, c, smokeAutomationBody("model tier smoke", "m1", []any{
		map[string]any{"id": "m1", "prd_id": prd},
	}))
	id := a.ID
	t.Logf("create: id=%s version=%d status=%s prd=%s", id, a.Version, a.Status, prd)
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			status, out := c.do(t, http.MethodDelete, "/api/automations/"+id, nil)
			t.Logf("cleanup delete %s -> %d %s", id, status, strings.TrimSpace(string(out)))
		}
		t.Logf("draft smoke PRD left for operator review: %s", prd)
	})
	if a.Status != "draft" {
		t.Errorf("create: status %q, want draft", a.Status)
	}
	start := modelTierSmokeGet(t, c, id)

	// (2) set_llm with execution and planning values at the current version.
	got := modelTierSmokePost(t, c, "set_llm", id, "set_llm", map[string]any{
		"actor": smokeActor, "reason": smokeReasonText, "expected_version": start.Version,
		"backend": execBackend, "model": execModel, "effort": execEffort,
		"planning_backend": planBackend, "planning_model": planModel,
	}, http.StatusOK)
	check := func(step string, r modelTierSmokeAutomation, want modelTierSmokeAutomation) {
		t.Helper()
		if r.Backend != want.Backend || r.Model != want.Model || r.Effort != want.Effort ||
			r.PlanningBackend != want.PlanningBackend || r.PlanningModel != want.PlanningModel ||
			r.VerificationBackend != want.VerificationBackend || r.VerificationModel != want.VerificationModel ||
			r.VerificationEffort != want.VerificationEffort {
			t.Errorf("%s: got %+v, want fields of %+v", step, r, want)
		}
	}
	afterLLM := modelTierSmokeAutomation{
		Backend: execBackend, Model: execModel, Effort: execEffort,
		PlanningBackend: planBackend, PlanningModel: planModel,
	}
	check("set_llm response", got, afterLLM)
	if got.Version != start.Version+1 {
		t.Errorf("set_llm response: version %d, want %d", got.Version, start.Version+1)
	}
	read := modelTierSmokeGet(t, c, id)
	check("GET after set_llm", read, afterLLM)
	if read.Version != start.Version+1 {
		t.Errorf("GET after set_llm: version %d, want %d", read.Version, start.Version+1)
	}

	// (3) set_verification_llm leaves the execution and planning fields alone.
	got = modelTierSmokePost(t, c, "set_verification_llm", id, "set_verification_llm", map[string]any{
		"actor": smokeActor, "reason": smokeReasonText, "expected_version": read.Version,
		"backend": verifyBackend, "model": verifyModel, "effort": verifyEffort,
	}, http.StatusOK)
	afterVerify := afterLLM
	afterVerify.VerificationBackend, afterVerify.VerificationModel, afterVerify.VerificationEffort = verifyBackend, verifyModel, verifyEffort
	check("set_verification_llm response", got, afterVerify)
	if got.PlanningBackend != planBackend || got.PlanningModel != planModel {
		t.Errorf("set_verification_llm changed the planning fields: backend=%q model=%q", got.PlanningBackend, got.PlanningModel)
	}
	if got.Version != read.Version+1 {
		t.Errorf("set_verification_llm response: version %d, want %d", got.Version, read.Version+1)
	}
	read = modelTierSmokeGet(t, c, id)
	check("GET after set_verification_llm", read, afterVerify)

	// (4) A stale expected_version is a 409 and changes nothing.
	modelTierSmokePost(t, c, "set_llm stale version", id, "set_llm", map[string]any{
		"expected_version": read.Version + 50, "model": "stale-model",
	}, http.StatusConflict)
	modelTierSmokePost(t, c, "set_verification_llm stale version", id, "set_verification_llm", map[string]any{
		"expected_version": read.Version + 50, "model": "stale-model",
	}, http.StatusConflict)
	if after := modelTierSmokeGet(t, c, id); after.Version != read.Version {
		t.Errorf("stale version bumped the record: version %d, want %d", after.Version, read.Version)
	}

	// (5) set_verification_llm does not accept the planning fields.
	modelTierSmokePost(t, c, "set_verification_llm with planning_backend", id, "set_verification_llm", map[string]any{
		"expected_version": read.Version, "backend": verifyBackend, "planning_backend": planBackend,
	}, http.StatusBadRequest)
	modelTierSmokePost(t, c, "set_verification_llm with planning_model", id, "set_verification_llm", map[string]any{
		"expected_version": read.Version, "planning_model": planModel,
	}, http.StatusBadRequest)
	if after := modelTierSmokeGet(t, c, id); after.Version != read.Version {
		t.Errorf("rejected request bumped the record: version %d, want %d", after.Version, read.Version)
	}

	// (6) An empty string clears a field; the other fields stay.
	got = modelTierSmokePost(t, c, "set_llm clear model", id, "set_llm", map[string]any{
		"expected_version": read.Version, "model": "", "planning_model": "",
	}, http.StatusOK)
	cleared := afterVerify
	cleared.Model, cleared.PlanningModel = "", ""
	check("set_llm clear response", got, cleared)
	check("GET after set_llm clear", modelTierSmokeGet(t, c, id), cleared)

	got = modelTierSmokePost(t, c, "set_verification_llm clear effort", id, "set_verification_llm", map[string]any{
		"expected_version": got.Version, "effort": "",
	}, http.StatusOK)
	cleared.VerificationEffort = ""
	check("set_verification_llm clear response", got, cleared)
	check("GET after set_verification_llm clear", modelTierSmokeGet(t, c, id), cleared)

	// (7) Delete the draft Automation.
	status, out := c.do(t, http.MethodDelete, "/api/automations/"+id, nil)
	t.Logf("delete %s -> %d %s", id, status, strings.TrimSpace(string(out)))
	if status != http.StatusNoContent {
		t.Errorf("delete: status %d, want 204 body=%s", status, out)
	}
	deleted = status == http.StatusNoContent
	if deleted {
		if status, _ := c.do(t, http.MethodGet, "/api/automations/"+id, nil); status != http.StatusNotFound {
			t.Errorf("GET after delete: status %d, want 404", status)
		}
	}
}
