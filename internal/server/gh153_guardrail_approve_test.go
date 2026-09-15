// GH#153 — per-guardrail block approval endpoint tests.
//
// TC-1: approveVerdict marks named verdict approved, returns unblocked=true when last block
// TC-2: approveVerdict with multiple blocks — unblocked=false until all approved
// TC-3: approveVerdict returns found=false for unknown guardrail name
// TC-4: approveVerdict for unknown session returns found=false
// TC-5: HTTP POST /api/sessions/{id}/guardrail/{name}/approve — 200 + response shape
// TC-6: HTTP POST returns 404 for unknown guardrail name

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupGuardrailStore(sessionID string, verdicts []HookGuardrailVerdict) {
	globalHookStore.mu.Lock()
	defer globalHookStore.mu.Unlock()
	tel := &SessionTelemetry{GuardrailVerdicts: verdicts}
	globalHookStore.state[sessionID] = &SessionStatusBoard{SessionID: sessionID, Telemetry: tel}
}

func TestGH153_ApproveSingleBlock_Unblocked(t *testing.T) {
	sid := "test-gh153-a"
	setupGuardrailStore(sid, []HookGuardrailVerdict{
		{Guardrail: "content-safety", Outcome: "block", Summary: "unsafe content"},
	})
	tel, found, unblocked := globalHookStore.approveVerdict(sid, "content-safety", "false positive")
	if !found {
		t.Fatal("expected found=true")
	}
	if !unblocked {
		t.Fatal("expected unblocked=true after approving the only block")
	}
	if !tel.GuardrailVerdicts[0].Approved {
		t.Fatal("expected verdict.Approved=true")
	}
	if tel.GuardrailVerdicts[0].ApprovalNote != "false positive" {
		t.Fatalf("unexpected approval note: %q", tel.GuardrailVerdicts[0].ApprovalNote)
	}
}

func TestGH153_ApproveOneOfTwoBlocks_StillBlocked(t *testing.T) {
	sid := "test-gh153-b"
	setupGuardrailStore(sid, []HookGuardrailVerdict{
		{Guardrail: "content-safety", Outcome: "block"},
		{Guardrail: "scope-check", Outcome: "block"},
	})
	_, found, unblocked := globalHookStore.approveVerdict(sid, "content-safety", "")
	if !found {
		t.Fatal("expected found=true")
	}
	if unblocked {
		t.Fatal("expected unblocked=false while scope-check block remains")
	}
}

func TestGH153_ApproveBothBlocks_Unblocked(t *testing.T) {
	sid := "test-gh153-c"
	setupGuardrailStore(sid, []HookGuardrailVerdict{
		{Guardrail: "content-safety", Outcome: "block"},
		{Guardrail: "scope-check", Outcome: "block"},
	})
	globalHookStore.approveVerdict(sid, "content-safety", "") //nolint:errcheck
	_, _, unblocked := globalHookStore.approveVerdict(sid, "scope-check", "")
	if !unblocked {
		t.Fatal("expected unblocked=true after both blocks approved")
	}
}

func TestGH153_ApproveUnknownGuardrail_NotFound(t *testing.T) {
	sid := "test-gh153-d"
	setupGuardrailStore(sid, []HookGuardrailVerdict{
		{Guardrail: "content-safety", Outcome: "block"},
	})
	_, found, _ := globalHookStore.approveVerdict(sid, "nonexistent", "")
	if found {
		t.Fatal("expected found=false for unknown guardrail name")
	}
}

func TestGH153_ApproveUnknownSession_NotFound(t *testing.T) {
	_, found, _ := globalHookStore.approveVerdict("no-such-session", "content-safety", "")
	if found {
		t.Fatal("expected found=false for unknown session")
	}
}

func TestGH153_HTTPApprove_200(t *testing.T) {
	sid := "test-gh153-http"
	setupGuardrailStore(sid, []HookGuardrailVerdict{
		{Guardrail: "content-safety", Outcome: "block"},
	})
	srv := &Server{}
	req := httptest.NewRequest(http.MethodPost,
		"/api/sessions/"+sid+"/guardrail/content-safety/approve",
		strings.NewReader(`{"note":"operator override"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleSessionGuardrailApprove(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["approved"] != true {
		t.Errorf("want approved=true, got %v", resp["approved"])
	}
	if resp["session_unblocked"] != true {
		t.Errorf("want session_unblocked=true, got %v", resp["session_unblocked"])
	}
	if resp["guardrail"] != "content-safety" {
		t.Errorf("want guardrail=content-safety, got %v", resp["guardrail"])
	}
}

func TestGH153_HTTPApprove_404_UnknownGuardrail(t *testing.T) {
	sid := "test-gh153-http-404"
	setupGuardrailStore(sid, []HookGuardrailVerdict{
		{Guardrail: "content-safety", Outcome: "block"},
	})
	srv := &Server{}
	req := httptest.NewRequest(http.MethodPost,
		"/api/sessions/"+sid+"/guardrail/does-not-exist/approve",
		nil)
	rr := httptest.NewRecorder()
	srv.handleSessionGuardrailApprove(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rr.Code)
	}
}
