// BL274 v6.22.0 — docs_apply mode=execute integration tests (audit-honesty backfill).
//
// Sprint 3 shipped mode=execute with no integration test for the full
// plan→approval-token→execute round-trip. This file covers it via the
// real handleDocsApply against a docsindex Runtime backed by an in-memory
// chunk corpus + a fake ToolInvoker that records every call.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/docsindex"
)

// fakeInvoker records every Invoke call so tests can assert which steps ran.
type fakeInvoker struct {
	mu    sync.Mutex
	calls []invokeCall
	fail  string // if set, return error when this tool name is called
}

type invokeCall struct {
	Tool string
	Args map[string]interface{}
}

func (f *fakeInvoker) Invoke(_ context.Context, name string, args map[string]interface{}) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, invokeCall{Tool: name, Args: args})
	if f.fail == name {
		return "", errInvoker(name)
	}
	return `{"ok":true,"tool":"` + name + `"}`, nil
}

type errInvoker string

func (e errInvoker) Error() string { return "fake invoker forced fail: " + string(e) }

// helper — build a docsindex Runtime with one curated howto chunk.
func setupDocsRuntime(t *testing.T) (*docsindex.Runtime, *fakeInvoker) {
	t.Helper()
	dir := t.TempDir()
	body := "# How-to\n\n## Step\n\nDo the thing.\n"
	frontmatter := `docs:
  index: true
exec_params:
  - {name: target, required: true}
exec_steps:
  - tool: list_things
    description: list before
    args: {}
    read_only: true
  - tool: write_thing
    description: write the thing
    args: {target: "{{params.target}}"}
    read_only: false
  - tool: list_things
    description: list after
    args: {}
    read_only: true`
	chunk := docsindex.Chunk{
		Path:           "howto/test.md",
		Anchor:         "",
		Title:          "How-to",
		Body:           body,
		Source:         "core",
		ContentHash:    "h1",
		FrontmatterRaw: frontmatter,
	}
	bm25 := docsindex.BuildBM25([]docsindex.Chunk{chunk})

	// Persist a JSON snapshot the runtime can load (Init expects byte stream).
	jsonPath := filepath.Join(dir, "bm25.json")
	if err := docsindex.SaveJSON(bm25, jsonPath); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}
	embedded, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	rt, err := docsindex.Init(embedded, dir, nil)
	if err != nil {
		t.Fatalf("docsindex.Init: %v", err)
	}
	inv := &fakeInvoker{}
	rt.AttachInvoker(inv)
	return rt, inv
}

// applyHandler invokes handleDocsApply directly with a *Server stub.
func applyHandler(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	rt := docsindex.Default()
	if rt == nil {
		t.Fatal("docsindex Default not initialized")
	}
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/docs/apply", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleDocsApply(rr, req, rt)
	return rr
}

func TestDocsApply_PlanReturnsApprovalToken(t *testing.T) {
	_, _ = setupDocsRuntime(t)
	rr := applyHandler(t, `{"howto_id":"howto/test.md","mode":"plan","params":{"target":"foo"}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v — body: %s", err, rr.Body.String())
	}
	tok, _ := resp["approval_token"].(string)
	if len(tok) < 32 {
		t.Errorf("approval_token too short / missing: %q", tok)
	}
	steps, _ := resp["steps"].([]interface{})
	if len(steps) != 3 {
		t.Errorf("steps count = %d, want 3", len(steps))
	}
}

func TestDocsApply_ExecuteRequiresToken(t *testing.T) {
	_, _ = setupDocsRuntime(t)
	rr := applyHandler(t, `{"howto_id":"howto/test.md","mode":"execute"}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 missing-token, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDocsApply_ExecuteRejectsBadToken(t *testing.T) {
	_, _ = setupDocsRuntime(t)
	rr := applyHandler(t, `{"howto_id":"howto/test.md","mode":"execute","approval_token":"deadbeef"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401 bad-token, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDocsApply_ExecuteFullRoundtrip(t *testing.T) {
	_, inv := setupDocsRuntime(t)

	// 1. Plan.
	planRR := applyHandler(t, `{"howto_id":"howto/test.md","mode":"plan","params":{"target":"foo"}}`)
	if planRR.Code != http.StatusOK {
		t.Fatalf("plan: want 200, got %d: %s", planRR.Code, planRR.Body.String())
	}
	var planResp map[string]interface{}
	_ = json.Unmarshal(planRR.Body.Bytes(), &planResp)
	tok := planResp["approval_token"].(string)

	// 2. Execute (no risk gate — runs all 3 steps).
	body := `{"howto_id":"howto/test.md","mode":"execute","approval_token":"` + tok + `"}`
	execRR := applyHandler(t, body)
	if execRR.Code != http.StatusOK {
		t.Fatalf("execute: want 200, got %d: %s", execRR.Code, execRR.Body.String())
	}
	var execResp map[string]interface{}
	_ = json.Unmarshal(execRR.Body.Bytes(), &execResp)
	stepsRun, _ := execResp["steps_run"].([]interface{})
	if len(stepsRun) != 3 {
		t.Errorf("steps_run count = %d, want 3", len(stepsRun))
	}
	complete, _ := execResp["complete"].(bool)
	if !complete {
		t.Errorf("complete=%v, want true", complete)
	}
	// Verify the invoker actually got called for all 3 steps.
	if len(inv.calls) != 3 {
		t.Errorf("invoker calls = %d, want 3", len(inv.calls))
	}
	if inv.calls[0].Tool != "list_things" || inv.calls[1].Tool != "write_thing" || inv.calls[2].Tool != "list_things" {
		t.Errorf("invoker call sequence wrong: %+v", inv.calls)
	}
	// Verify {{params.target}} expanded to "foo" in step 2's args.
	if v, _ := inv.calls[1].Args["target"].(string); v != "foo" {
		t.Errorf("params.target expansion failed: got %q, want 'foo'", v)
	}
}

func TestDocsApply_ExecuteWithRiskGatePausesAtMutating(t *testing.T) {
	_, inv := setupDocsRuntime(t)

	// 1. Plan with risk_gate=true.
	planRR := applyHandler(t, `{"howto_id":"howto/test.md","mode":"plan","params":{"target":"foo"},"risk_gate":true}`)
	if planRR.Code != http.StatusOK {
		t.Fatalf("plan: %d %s", planRR.Code, planRR.Body.String())
	}
	var planResp map[string]interface{}
	_ = json.Unmarshal(planRR.Body.Bytes(), &planResp)
	tok := planResp["approval_token"].(string)

	// 2. Execute round 1 — should run step 0 (read_only) then pause before
	//    step 1 (write_thing, mutating).
	body := `{"howto_id":"howto/test.md","mode":"execute","approval_token":"` + tok + `"}`
	r1 := applyHandler(t, body)
	if r1.Code != http.StatusOK {
		t.Fatalf("execute round 1: %d %s", r1.Code, r1.Body.String())
	}
	var resp1 map[string]interface{}
	_ = json.Unmarshal(r1.Body.Bytes(), &resp1)
	if c, _ := resp1["complete"].(bool); c {
		t.Errorf("round 1 should not be complete")
	}
	pending, _ := resp1["pending_step"].(map[string]interface{})
	if pending == nil {
		t.Fatalf("pending_step missing — round 1 should pause at write_thing")
	}
	if pending["tool"] != "write_thing" {
		t.Errorf("pending_step.tool = %v, want write_thing", pending["tool"])
	}
	nextTok, _ := resp1["next_approval_token"].(string)
	if len(nextTok) < 32 {
		t.Errorf("next_approval_token missing/short: %q", nextTok)
	}
	if len(inv.calls) != 1 {
		t.Errorf("after round 1: invoker calls = %d, want 1 (only the read_only step)", len(inv.calls))
	}

	// 3. Execute round 2 with continuation token — runs write_thing + final list.
	body2 := `{"howto_id":"howto/test.md","mode":"execute","approval_token":"` + nextTok + `"}`
	r2 := applyHandler(t, body2)
	if r2.Code != http.StatusOK {
		t.Fatalf("execute round 2: %d %s", r2.Code, r2.Body.String())
	}
	var resp2 map[string]interface{}
	_ = json.Unmarshal(r2.Body.Bytes(), &resp2)
	if c, _ := resp2["complete"].(bool); !c {
		t.Errorf("round 2 should be complete")
	}
	if len(inv.calls) != 3 {
		t.Errorf("after round 2: invoker calls = %d, want 3 total", len(inv.calls))
	}
}

func TestDocsApply_ExecuteHaltsOnError(t *testing.T) {
	_, inv := setupDocsRuntime(t)
	inv.fail = "write_thing" // step 1 will error

	// Plan + execute.
	planRR := applyHandler(t, `{"howto_id":"howto/test.md","mode":"plan","params":{"target":"foo"}}`)
	var planResp map[string]interface{}
	_ = json.Unmarshal(planRR.Body.Bytes(), &planResp)
	tok := planResp["approval_token"].(string)

	body := `{"howto_id":"howto/test.md","mode":"execute","approval_token":"` + tok + `"}`
	rr := applyHandler(t, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("execute: %d %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if h, _ := resp["halted"].(bool); !h {
		t.Errorf("halted=%v, want true", h)
	}
	if c, _ := resp["complete"].(bool); c {
		t.Errorf("complete=%v, want false (errored)", c)
	}
	// Should have called step 0 (success) and step 1 (failure) but NOT step 2.
	if len(inv.calls) != 2 {
		t.Errorf("invoker calls = %d, want 2 (halt before step 2)", len(inv.calls))
	}
}
