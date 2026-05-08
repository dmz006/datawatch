// BL274 Sprint 3, v6.18.0 — approval-token store tests.

package docsindex

import (
	"testing"
	"time"
)

func TestApprovalStore_IssueAndGet(t *testing.T) {
	store := NewApprovalStore()
	steps := []ExecStep{
		{Tool: "ping", Description: "ping it", ReadOnly: true, Provenance: "authored"},
		{Tool: "write", Description: "write it", ReadOnly: false, Provenance: "authored"},
	}
	tok, err := store.Issue("howto/x.md", steps, map[string]string{"k": "v"}, true)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(tok) < 32 {
		t.Errorf("token too short: %d", len(tok))
	}
	got, err := store.Get(tok, "howto/x.md")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.HowtoID != "howto/x.md" || len(got.Steps) != 2 || !got.RiskGate {
		t.Errorf("approval roundtrip wrong: %+v", got)
	}
}

func TestApprovalStore_HowtoMismatch(t *testing.T) {
	store := NewApprovalStore()
	tok, _ := store.Issue("howto/a.md", nil, nil, false)
	if _, err := store.Get(tok, "howto/b.md"); err != ErrHowtoMismatch {
		t.Errorf("expected ErrHowtoMismatch, got %v", err)
	}
}

func TestApprovalStore_TTLEviction(t *testing.T) {
	store := NewApprovalStore()
	store.SetTTL(50 * time.Millisecond)
	tok, _ := store.Issue("howto/x.md", nil, nil, false)
	if _, err := store.Get(tok, ""); err != nil {
		t.Fatalf("token should be live: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := store.Get(tok, ""); err != ErrApprovalNotFound {
		t.Errorf("expected ErrApprovalNotFound after TTL, got %v", err)
	}
}

func TestApprovalStore_AdvanceAndDelete(t *testing.T) {
	store := NewApprovalStore()
	tok, _ := store.Issue("howto/x.md", []ExecStep{{Tool: "a"}, {Tool: "b"}, {Tool: "c"}}, nil, false)
	store.Advance(tok, 2)
	got, _ := store.Get(tok, "")
	if got.NextStep != 2 {
		t.Errorf("Advance did not stick: %d", got.NextStep)
	}
	store.Delete(tok)
	if _, err := store.Get(tok, ""); err != ErrApprovalNotFound {
		t.Errorf("expected gone after Delete, got %v", err)
	}
}

func TestApprovalStore_UnknownToken(t *testing.T) {
	store := NewApprovalStore()
	if _, err := store.Get("doesnotexist", ""); err != ErrApprovalNotFound {
		t.Errorf("expected ErrApprovalNotFound, got %v", err)
	}
}

func TestExecStep_DefaultProvenance(t *testing.T) {
	body := `---
exec_steps:
  - tool: do_a_thing
    description: does the thing
    read_only: true
exec_params: []
---
# body`
	fm, err := ParseFrontMatter(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	steps, err := fm.ResolveExecSteps(nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(steps) != 1 || steps[0].Provenance != "authored" {
		t.Errorf("default provenance not set to 'authored': %+v", steps)
	}
}
