// BL416 — set_llm / set_task_llm accept guardrail_backend / guardrail_model
// with presence-aware semantics.
//
//   - unknown guardrail_backend → 400 on both endpoints
//   - known guardrail_backend → 200
//   - guardrail-only body does NOT invoke the legacy (overwrite-all) setter
//   - legacy-only body does NOT invoke the guardrail setter
//   - mixed body invokes both

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

// guardrailLLMTestOrch embeds fakeOrchAutonomous and counts setter calls.
type guardrailLLMTestOrch struct {
	fakeOrchAutonomous
	prdLLMCalls, taskLLMCalls             int
	prdGuardrailCalls, taskGuardrailCalls int
	gotBackend, gotModel                  *string
}

func (o *guardrailLLMTestOrch) SetPRDLLM(string, string, string, string, string, string, string) (any, error) {
	o.prdLLMCalls++
	return map[string]any{"ok": true}, nil
}
func (o *guardrailLLMTestOrch) SetTaskLLM(string, string, string, string, string, string) (any, error) {
	o.taskLLMCalls++
	return map[string]any{"ok": true}, nil
}
func (o *guardrailLLMTestOrch) SetPRDGuardrailLLM(_ string, backend, model *string, _ string) (any, error) {
	o.prdGuardrailCalls++
	o.gotBackend, o.gotModel = backend, model
	return map[string]any{"ok": true}, nil
}
func (o *guardrailLLMTestOrch) SetTaskGuardrailLLM(_, _ string, backend, model *string, _ string) (any, error) {
	o.taskGuardrailCalls++
	o.gotBackend, o.gotModel = backend, model
	return map[string]any{"ok": true}, nil
}

func bl416Post(t *testing.T, orch *guardrailLLMTestOrch, action, body string) *httptest.ResponseRecorder {
	t.Helper()
	reg := inference.NewRegistry()
	_ = reg.Add(&inference.LLM{Name: "good-llm", Kind: inference.KindOllama})
	srv := &Server{autonomousMgr: orch}
	srv.inferenceReg = reg
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/p1/"+action, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)
	return rr
}

func TestBL416_SetLLM_UnknownGuardrailBackend_Returns400(t *testing.T) {
	for _, tc := range []struct{ action, body string }{
		{"set_llm", `{"guardrail_backend":"bad-llm"}`},
		{"set_task_llm", `{"task_id":"t1","guardrail_backend":"bad-llm"}`},
	} {
		orch := &guardrailLLMTestOrch{}
		rr := bl416Post(t, orch, tc.action, tc.body)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d: %s", tc.action, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "unknown guardrail LLM") {
			t.Errorf("%s: want 'unknown guardrail LLM' in body, got %s", tc.action, rr.Body.String())
		}
		if orch.prdGuardrailCalls+orch.taskGuardrailCalls+orch.prdLLMCalls+orch.taskLLMCalls != 0 {
			t.Errorf("%s: no setter should run on validation failure", tc.action)
		}
	}
}

func TestBL416_SetLLM_KnownGuardrailBackend_Returns200(t *testing.T) {
	for _, tc := range []struct{ action, body string }{
		{"set_llm", `{"guardrail_backend":"good-llm","guardrail_model":"m1"}`},
		{"set_task_llm", `{"task_id":"t1","guardrail_backend":"good-llm","guardrail_model":"m1"}`},
	} {
		orch := &guardrailLLMTestOrch{}
		rr := bl416Post(t, orch, tc.action, tc.body)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: want 200, got %d: %s", tc.action, rr.Code, rr.Body.String())
		}
		if orch.gotBackend == nil || *orch.gotBackend != "good-llm" || orch.gotModel == nil || *orch.gotModel != "m1" {
			t.Errorf("%s: guardrail setter got %v/%v", tc.action, orch.gotBackend, orch.gotModel)
		}
	}
}

func TestBL416_GuardrailOnlyBody_SkipsLegacySetter(t *testing.T) {
	orch := &guardrailLLMTestOrch{}
	if rr := bl416Post(t, orch, "set_llm", `{"guardrail_model":"m1","actor":"x"}`); rr.Code != http.StatusOK {
		t.Fatalf("set_llm: %d %s", rr.Code, rr.Body.String())
	}
	if orch.prdLLMCalls != 0 || orch.prdGuardrailCalls != 1 {
		t.Errorf("set_llm guardrail-only: SetPRDLLM=%d SetPRDGuardrailLLM=%d; want 0/1", orch.prdLLMCalls, orch.prdGuardrailCalls)
	}
	if orch.gotBackend != nil {
		t.Errorf("absent guardrail_backend should be nil, got %q", *orch.gotBackend)
	}

	orch = &guardrailLLMTestOrch{}
	if rr := bl416Post(t, orch, "set_task_llm", `{"task_id":"t1","guardrail_backend":""}`); rr.Code != http.StatusOK {
		t.Fatalf("set_task_llm: %d %s", rr.Code, rr.Body.String())
	}
	if orch.taskLLMCalls != 0 || orch.taskGuardrailCalls != 1 {
		t.Errorf("set_task_llm guardrail-only: SetTaskLLM=%d SetTaskGuardrailLLM=%d; want 0/1", orch.taskLLMCalls, orch.taskGuardrailCalls)
	}
	if orch.gotBackend == nil || *orch.gotBackend != "" {
		t.Errorf("explicit empty guardrail_backend should be non-nil \"\"")
	}
}

func TestBL416_LegacyOnlyBody_SkipsGuardrailSetter(t *testing.T) {
	orch := &guardrailLLMTestOrch{}
	if rr := bl416Post(t, orch, "set_llm", `{"backend":"good-llm","model":"m"}`); rr.Code != http.StatusOK {
		t.Fatalf("set_llm: %d %s", rr.Code, rr.Body.String())
	}
	if orch.prdLLMCalls != 1 || orch.prdGuardrailCalls != 0 {
		t.Errorf("set_llm legacy-only: SetPRDLLM=%d SetPRDGuardrailLLM=%d; want 1/0", orch.prdLLMCalls, orch.prdGuardrailCalls)
	}

	orch = &guardrailLLMTestOrch{}
	if rr := bl416Post(t, orch, "set_task_llm", `{"task_id":"t1","backend":"good-llm"}`); rr.Code != http.StatusOK {
		t.Fatalf("set_task_llm: %d %s", rr.Code, rr.Body.String())
	}
	if orch.taskLLMCalls != 1 || orch.taskGuardrailCalls != 0 {
		t.Errorf("set_task_llm legacy-only: SetTaskLLM=%d SetTaskGuardrailLLM=%d; want 1/0", orch.taskLLMCalls, orch.taskGuardrailCalls)
	}

	// An empty body (no keys at all) keeps the legacy behavior too.
	orch = &guardrailLLMTestOrch{}
	if rr := bl416Post(t, orch, "set_llm", `{}`); rr.Code != http.StatusOK {
		t.Fatalf("set_llm {}: %d %s", rr.Code, rr.Body.String())
	}
	if orch.prdLLMCalls != 1 || orch.prdGuardrailCalls != 0 {
		t.Errorf("set_llm {}: SetPRDLLM=%d SetPRDGuardrailLLM=%d; want 1/0", orch.prdLLMCalls, orch.prdGuardrailCalls)
	}
}

func TestBL416_MixedBody_InvokesBoth(t *testing.T) {
	orch := &guardrailLLMTestOrch{}
	if rr := bl416Post(t, orch, "set_llm", `{"backend":"good-llm","guardrail_backend":"good-llm"}`); rr.Code != http.StatusOK {
		t.Fatalf("set_llm: %d %s", rr.Code, rr.Body.String())
	}
	if orch.prdLLMCalls != 1 || orch.prdGuardrailCalls != 1 {
		t.Errorf("set_llm mixed: SetPRDLLM=%d SetPRDGuardrailLLM=%d; want 1/1", orch.prdLLMCalls, orch.prdGuardrailCalls)
	}

	orch = &guardrailLLMTestOrch{}
	if rr := bl416Post(t, orch, "set_task_llm", `{"task_id":"t1","effort":"high","guardrail_model":"m"}`); rr.Code != http.StatusOK {
		t.Fatalf("set_task_llm: %d %s", rr.Code, rr.Body.String())
	}
	if orch.taskLLMCalls != 1 || orch.taskGuardrailCalls != 1 {
		t.Errorf("set_task_llm mixed: SetTaskLLM=%d SetTaskGuardrailLLM=%d; want 1/1", orch.taskLLMCalls, orch.taskGuardrailCalls)
	}
}

func TestBL416_SetTaskLLM_GuardrailOnly_StillRequiresTaskID(t *testing.T) {
	orch := &guardrailLLMTestOrch{}
	rr := bl416Post(t, orch, "set_task_llm", `{"guardrail_backend":"good-llm"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "task_id required") {
		t.Fatalf("want 400 task_id required, got %d %s", rr.Code, rr.Body.String())
	}
}
