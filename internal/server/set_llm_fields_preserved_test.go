// Black-box REST tests of POST /api/autonomous/prds/{id}/set_llm against
// the presence-aware handler: fields decode as *string, so an absent key
// leaves the PRD field unchanged, "" clears it, and a non-empty value
// replaces it. GET /api/autonomous/prds/{id} is used to read back the
// PRD (omitted keys in the response = empty field).
//
// Mirrors the construction pattern of bl320_set_llm_decomp_profile_test.go
// and bl416_set_guardrail_llm_test.go, but with a real autonomous manager
// (no fakes) so full persistence round-trips are exercised.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// setLLMRESTServer builds a Server backed by a real autonomous manager in
// a temp data dir (same construction as newAutomationRESTServer).
func setLLMRESTServer(t *testing.T) (*Server, *autonomous.Manager) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	return &Server{autonomousMgr: newTestAPI(mgr)}, mgr
}

// spOr sets p to *s when s != "" so nil/"" seeding is explicit.
func spOr(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

// seedSetLLMRESTPRD creates a PRD and seeds distinct non-empty values for
// the four LLM fields under test (decomposition_model when non-empty), then
// verifies the stored PRD carries all of them.
func seedSetLLMRESTPRD(t *testing.T, mgr *autonomous.Manager, backend, effort, model, decompProfile, decompModel string) string {
	t.Helper()
	prd, err := mgr.CreatePRD("set_llm REST black-box spec", t.TempDir(), backend, model, autonomous.Effort(effort))
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if decompProfile != "" || decompModel != "" {
		if _, err := mgr.SetPRDLLM(prd.ID, nil, nil, nil, spOr(decompProfile), spOr(decompModel), "seed"); err != nil {
			t.Fatalf("SetPRDLLM (seed): %v", err)
		}
	}
	stored, ok := mgr.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing from store after seed")
	}
	if stored.Backend != backend {
		t.Fatalf("seed backend: got %q want %q", stored.Backend, backend)
	}
	if string(stored.Effort) != effort {
		t.Fatalf("seed effort: got %q want %q", stored.Effort, effort)
	}
	if stored.Model != model {
		t.Fatalf("seed model: got %q want %q", stored.Model, model)
	}
	if stored.DecompositionProfile != decompProfile {
		t.Fatalf("seed decomposition_profile: got %q want %q", stored.DecompositionProfile, decompProfile)
	}
	if stored.DecompositionModel != decompModel {
		t.Fatalf("seed decomposition_model: got %q want %q", stored.DecompositionModel, decompModel)
	}
	return prd.ID
}

// postSetLLMREST POSTs body to the set_llm endpoint and requires 200.
func postSetLLMREST(t *testing.T, srv *Server, prdID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+prdID+"/set_llm", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("set_llm %s: want 200, got %d: %s", body, rr.Code, rr.Body.String())
	}
	return rr
}

// getPRDREST fetches the PRD via GET and decodes it into a map.
// Omitted (omitempty) keys read as "" so "cleared" and "absent" both
// surface as the empty string.
func getPRDREST(t *testing.T, srv *Server, prdID string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+prdID, nil)
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET prd: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var prd map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&prd); err != nil {
		t.Fatalf("GET prd: body not JSON: %v", err)
	}
	return prd
}

// prdField returns the PRD's value for key, "" when the key is absent.
func prdField(prd map[string]any, key string) string {
	v, _ := prd[key].(string)
	return v
}

// lastSetPRDLLMNoteREST returns the Note of the most recent decision-log
// entry of kind set_prd_llm as seen through the GET endpoint.
func lastSetPRDLLMNoteREST(t *testing.T, prd map[string]any) string {
	t.Helper()
	raw, ok := prd["decisions"].([]any)
	if !ok {
		t.Fatal("GET prd: no 'decisions' array")
	}
	note := ""
	found := false
	for _, e := range raw {
		d, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if d["kind"] != "set_prd_llm" {
			continue
		}
		found = true
		note, _ = d["note"].(string)
	}
	if !found {
		t.Fatalf("no set_prd_llm decision entry in %v", prd["decisions"])
	}
	return note
}

// TestRESTSetLLM_OmittedFieldsUnchanged — each POST carries exactly one
// legacy LLM field; that field is updated and every other field survives
// byte-for-byte.
func TestRESTSetLLM_OmittedFieldsUnchanged(t *testing.T) {
	seed := map[string]string{
		"backend":               "opencode",
		"effort":                "high",
		"model":                 "qwen",
		"decomposition_profile": "plan-profile-seed",
	}
	updates := map[string]string{
		"backend":               "opencode-updated",
		"effort":                "low",
		"model":                 "newm",
		"decomposition_profile": "x",
	}
	bodies := map[string]string{
		"backend":               `{"backend":"opencode-updated","actor":"t"}`,
		"effort":                `{"effort":"low","actor":"t"}`,
		"model":                 `{"model":"newm","actor":"t"}`,
		"decomposition_profile": `{"decomposition_profile":"x","actor":"t"}`,
	}
	for _, field := range []string{"backend", "effort", "model", "decomposition_profile"} {
		t.Run(field, func(t *testing.T) {
			srv, mgr := setLLMRESTServer(t)
			prdID := seedSetLLMRESTPRD(t, mgr, seed["backend"], seed["effort"], seed["model"], seed["decomposition_profile"], "")
			postSetLLMREST(t, srv, prdID, bodies[field])
			prd := getPRDREST(t, srv, prdID)
			if got := prdField(prd, field); got != updates[field] {
				t.Errorf("%s: got %q, want %q", field, got, updates[field])
			}
			for other, want := range seed {
				if other == field {
					continue
				}
				if got := prdField(prd, other); got != want {
					t.Errorf("%s: got %q, want unchanged %q", other, got, want)
				}
			}
		})
	}
}

// TestRESTSetLLM_EmptyFieldsClear — an explicit "" in the body clears that
// field and leaves every other field (including fields cleared earlier in
// the same PRD's history) untouched.
func TestRESTSetLLM_EmptyFieldsClear(t *testing.T) {
	seed := map[string]string{
		"backend":               "opencode",
		"effort":                "high",
		"model":                 "qwen",
		"decomposition_profile": "plan-profile-seed",
	}
	for _, field := range []string{"backend", "effort", "model", "decomposition_profile"} {
		t.Run(field, func(t *testing.T) {
			srv, mgr := setLLMRESTServer(t)
			prdID := seedSetLLMRESTPRD(t, mgr, seed["backend"], seed["effort"], seed["model"], seed["decomposition_profile"], "")
			body := fmt.Sprintf(`{"%s":"","actor":"t"}`, field)
			postSetLLMREST(t, srv, prdID, body)
			prd := getPRDREST(t, srv, prdID)
			if got := prdField(prd, field); got != "" {
				t.Errorf("%s: got %q, want cleared (empty)", field, got)
			}
			for other, want := range seed {
				if other == field {
					continue
				}
				if got := prdField(prd, other); got != want {
					t.Errorf("%s: got %q, want unchanged %q", other, got, want)
				}
			}
		})
	}
}

// TestRESTSetLLM_SetReplaces — non-empty values replace the seeded values;
// fields not in the body are left exactly as seeded.
func TestRESTSetLLM_SetReplaces(t *testing.T) {
	seed := map[string]string{
		"backend":               "opencode",
		"effort":                "high",
		"model":                 "qwen",
		"decomposition_profile": "plan-profile-seed",
		"decomposition_model":   "plan-model-seed",
	}
	srv, mgr := setLLMRESTServer(t)
	prdID := seedSetLLMRESTPRD(t, mgr, seed["backend"], seed["effort"], seed["model"], seed["decomposition_profile"], seed["decomposition_model"])
	postSetLLMREST(t, srv, prdID, `{"backend":"opencode-replaced","model":"newm","actor":"t"}`)
	prd := getPRDREST(t, srv, prdID)
	if got := prdField(prd, "backend"); got != "opencode-replaced" {
		t.Errorf("backend: got %q, want opencode-replaced", got)
	}
	if got := prdField(prd, "model"); got != "newm" {
		t.Errorf("model: got %q, want newm", got)
	}
	for _, field := range []string{"effort", "decomposition_profile", "decomposition_model"} {
		if got := prdField(prd, field); got != seed[field] {
			t.Errorf("%s: got %q, want unchanged %q", field, got, seed[field])
		}
	}
}

// TestRESTSetLLM_DecisionLogNamesOnlySentFields — after a call that sends
// only decomposition_profile, the latest set_prd_llm decision-log entry
// names decomposition_profile and NOT backend/effort/model.
func TestRESTSetLLM_DecisionLogNamesOnlySentFields(t *testing.T) {
	srv, mgr := setLLMRESTServer(t)
	prdID := seedSetLLMRESTPRD(t, mgr, "opencode", "high", "qwen", "plan-profile-seed", "")
	postSetLLMREST(t, srv, prdID, `{"decomposition_profile":"x","actor":"t"}`)
	prd := getPRDREST(t, srv, prdID)
	note := lastSetPRDLLMNoteREST(t, prd)
	if !strings.Contains(note, "decomposition_profile") {
		t.Errorf("note should mention decomposition_profile; got %q", note)
	}
	for _, bad := range []string{"backend", "effort", "model"} {
		if strings.Contains(note, bad) {
			t.Errorf("note should NOT mention %q; got %q", bad, note)
		}
	}
}
