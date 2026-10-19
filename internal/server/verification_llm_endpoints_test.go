// set_task_verification_llm / set_story_verification_llm /
// set_verification_llm — per-unit verification (judge) LLM overrides
// with presence-aware semantics (absent = unchanged, "" = clear).

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/inference"
)

// verifLLMVals stores per-unit verification overrides so the fake
// manager can emulate presence-aware persistence.
type verifLLMVals struct{ backend, model, effort string }

type verifLLMTestOrch struct {
	fakeOrchAutonomous
	calls int
	units map[string]*verifLLMVals
}

func (o *verifLLMTestOrch) apply(key string, backend, model, effort *string) (any, error) {
	o.calls++
	if o.units == nil {
		o.units = map[string]*verifLLMVals{}
	}
	v := o.units[key]
	if v == nil {
		v = &verifLLMVals{}
		o.units[key] = v
	}
	if backend != nil {
		v.backend = *backend
	}
	if model != nil {
		v.model = *model
	}
	if effort != nil {
		v.effort = *effort
	}
	return &autonomous.PRD{ID: key}, nil
}

func (o *verifLLMTestOrch) SetTaskVerificationLLM(prdID, taskID string, backend, model, effort *string, _ string) (any, error) {
	return o.apply(prdID+"/task/"+taskID, backend, model, effort)
}
func (o *verifLLMTestOrch) SetStoryVerificationLLM(prdID, storyID string, backend, model, effort *string, _ string) (any, error) {
	return o.apply(prdID+"/story/"+storyID, backend, model, effort)
}
func (o *verifLLMTestOrch) SetPRDVerificationLLM(prdID string, backend, model, effort *string, _ string) (any, error) {
	return o.apply(prdID, backend, model, effort)
}

type verifLLMCase struct {
	action, unitJSON, key string
}

var verifLLMCases = []verifLLMCase{
	{"set_task_verification_llm", `"task_id":"t1",`, "p1/task/t1"},
	{"set_story_verification_llm", `"story_id":"s1",`, "p1/story/s1"},
	{"set_verification_llm", ``, "p1"},
}

func verifLLMServer(t *testing.T, orch *verifLLMTestOrch) (*Server, *audit.Log) {
	t.Helper()
	reg := inference.NewRegistry()
	_ = reg.Add(&inference.LLM{Name: "good-llm", Kind: inference.KindOllama})
	srv := &Server{autonomousMgr: orch}
	srv.inferenceReg = reg
	al, err := audit.NewAt(t.TempDir() + "/audit.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	srv.auditLog = al
	return srv, al
}

func verifLLMDo(srv *Server, method, action, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/autonomous/prds/p1/"+action, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)
	return rr
}

func TestVerificationLLMEndpoints_PersistAndPresenceAware(t *testing.T) {
	for _, tc := range verifLLMCases {
		orch := &verifLLMTestOrch{}
		srv, _ := verifLLMServer(t, orch)

		rr := verifLLMDo(srv, http.MethodPost, tc.action, `{`+tc.unitJSON+`"verification_backend":"good-llm","verification_model":"m1","verification_effort":"high"}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: want 200, got %d: %s", tc.action, rr.Code, rr.Body.String())
		}
		if got := *orch.units[tc.key]; got != (verifLLMVals{"good-llm", "m1", "high"}) {
			t.Fatalf("%s: persisted %+v", tc.action, got)
		}

		// Absent keys leave fields unchanged; "" clears only the given one.
		rr = verifLLMDo(srv, http.MethodPost, tc.action, `{`+tc.unitJSON+`"verification_model":""}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s clear: want 200, got %d: %s", tc.action, rr.Code, rr.Body.String())
		}
		if got := *orch.units[tc.key]; got != (verifLLMVals{"good-llm", "", "high"}) {
			t.Errorf("%s: after clearing model got %+v", tc.action, got)
		}

		// Empty backend clears without registry validation.
		rr = verifLLMDo(srv, http.MethodPost, tc.action, `{`+tc.unitJSON+`"verification_backend":""}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s clear backend: want 200, got %d: %s", tc.action, rr.Code, rr.Body.String())
		}
		if got := *orch.units[tc.key]; got != (verifLLMVals{"", "", "high"}) {
			t.Errorf("%s: after clearing backend got %+v", tc.action, got)
		}
	}
}

func TestVerificationLLMEndpoints_UnknownBackend400(t *testing.T) {
	for _, tc := range verifLLMCases {
		orch := &verifLLMTestOrch{}
		srv, _ := verifLLMServer(t, orch)
		rr := verifLLMDo(srv, http.MethodPost, tc.action, `{`+tc.unitJSON+`"verification_backend":"bad-llm"}`)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d: %s", tc.action, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "unknown LLM") || !strings.Contains(rr.Body.String(), "/api/llms") {
			t.Errorf("%s: unexpected error body %s", tc.action, rr.Body.String())
		}
		if orch.calls != 0 {
			t.Errorf("%s: manager must not run on validation failure", tc.action)
		}
	}
}

func TestVerificationLLMEndpoints_GET405(t *testing.T) {
	for _, tc := range verifLLMCases {
		orch := &verifLLMTestOrch{}
		srv, _ := verifLLMServer(t, orch)
		if rr := verifLLMDo(srv, http.MethodGet, tc.action, ``); rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", tc.action, rr.Code)
		}
	}
}

func TestVerificationLLMEndpoints_UnitIDRequired(t *testing.T) {
	for _, tc := range verifLLMCases[:2] {
		orch := &verifLLMTestOrch{}
		srv, _ := verifLLMServer(t, orch)
		rr := verifLLMDo(srv, http.MethodPost, tc.action, `{"verification_backend":"good-llm"}`)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "_id required") {
			t.Errorf("%s: want 400 id required, got %d %s", tc.action, rr.Code, rr.Body.String())
		}
	}
}

func TestVerificationLLMEndpoints_Audited(t *testing.T) {
	for _, tc := range verifLLMCases {
		orch := &verifLLMTestOrch{}
		srv, al := verifLLMServer(t, orch)
		if rr := verifLLMDo(srv, http.MethodPost, tc.action, `{`+tc.unitJSON+`"verification_backend":"good-llm"}`); rr.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tc.action, rr.Code, rr.Body.String())
		}
		entries, err := al.Read(audit.QueryFilter{})
		if err != nil || len(entries) == 0 {
			t.Fatalf("%s: no audit entry (err=%v)", tc.action, err)
		}
		e := entries[0]
		if e.Action != tc.action || e.Details["resource_type"] != "automaton" || e.Details["resource_id"] != "p1" {
			t.Errorf("%s: audit entry %+v", tc.action, e)
		}
		if e.Details["verification_backend"] != "good-llm" {
			t.Errorf("%s: audit detail missing backend: %+v", tc.action, e.Details)
		}
		if _, ok := e.Details["verification_model"]; ok {
			t.Errorf("%s: absent model should not appear in audit detail", tc.action)
		}
	}
}
