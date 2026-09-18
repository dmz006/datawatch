// BL320 — set_llm endpoint decomposition_profile validation.
//
// TC-1: unknown decomposition_profile → HTTP 400 "unknown planning LLM"
// TC-2: valid decomposition_profile (in registry) → HTTP 200, SetPRDLLM called
// TC-3: no inferenceReg → validation skipped, SetPRDLLM called regardless

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

// setLLMTestOrch embeds fakeOrchAutonomous and captures SetPRDLLM calls.
type setLLMTestOrch struct {
	fakeOrchAutonomous
	capturedProfile string
	capturedBackend string
	returnErr       error
}

func (o *setLLMTestOrch) SetPRDLLM(prdID, backend, effort, model, decompositionProfile, decompositionModel, actor string) (any, error) {
	o.capturedProfile = decompositionProfile
	o.capturedBackend = backend
	if o.returnErr != nil {
		return nil, o.returnErr
	}
	return map[string]any{"id": prdID, "decomposition_profile": decompositionProfile}, nil
}

func newSetLLMTestServer(orch *setLLMTestOrch, reg *inference.Registry) *Server {
	s := &Server{autonomousMgr: orch}
	s.inferenceReg = reg
	return s
}

// TC-1: unknown decomposition_profile with registry wired → 400.
func TestBL320_SetLLM_UnknownDecompProfile_Returns400(t *testing.T) {
	reg := inference.NewRegistry()
	_ = reg.Add(&inference.LLM{Name: "ollama", Kind: inference.KindOllama})
	orch := &setLLMTestOrch{}
	srv := newSetLLMTestServer(orch, reg)

	body := `{"decomposition_profile":"this-llm-does-not-exist-xyz123","actor":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-1/set_llm",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "unknown planning LLM") {
		t.Errorf("want 'unknown planning LLM' in body, got: %s", rr.Body.String())
	}
	if orch.capturedProfile != "" {
		t.Error("SetPRDLLM should not be called when profile is invalid")
	}
}

// TC-2: valid decomposition_profile (in registry) → 200 + SetPRDLLM called with profile.
func TestBL320_SetLLM_ValidDecompProfile_Returns200(t *testing.T) {
	reg := inference.NewRegistry()
	_ = reg.Add(&inference.LLM{Name: "my-planning-llm", Kind: inference.KindOllama})
	orch := &setLLMTestOrch{}
	srv := newSetLLMTestServer(orch, reg)

	body := `{"decomposition_profile":"my-planning-llm","actor":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-2/set_llm",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if orch.capturedProfile != "my-planning-llm" {
		t.Errorf("SetPRDLLM got decompositionProfile=%q; want my-planning-llm", orch.capturedProfile)
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if resp["decomposition_profile"] != "my-planning-llm" {
		t.Errorf("response decomposition_profile=%v; want my-planning-llm", resp["decomposition_profile"])
	}
}

// TC-3: no inferenceReg → validation skipped, SetPRDLLM called regardless.
func TestBL320_SetLLM_NoRegistry_SkipsValidation(t *testing.T) {
	orch := &setLLMTestOrch{}
	srv := newSetLLMTestServer(orch, nil) // nil registry

	body := `{"decomposition_profile":"any-unknown-profile","actor":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-3/set_llm",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200 (no registry = no validation), got %d: %s", rr.Code, rr.Body.String())
	}
	if orch.capturedProfile != "any-unknown-profile" {
		t.Errorf("SetPRDLLM should be called with profile when registry is nil; got %q", orch.capturedProfile)
	}
}
