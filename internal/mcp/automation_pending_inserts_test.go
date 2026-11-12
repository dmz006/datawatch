// automation_get carries pending_inserts through unchanged and its tool
// description points callers at the ids for automation_approve_insert.

package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestAutomationMCPGetReturnsPendingInserts(t *testing.T) {
	h := newAutomationHarness(t)

	// Create a record, then attach a pending insert through the update
	// handler (PUT merges extra keys into the stored entry).
	if _, err := h.s.handleAutomationCreate(context.Background(), callReq(map[string]any{"name": "chain-pending"})); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := h.s.handleAutomationUpdate(context.Background(), callReq(map[string]any{
		"id": "auto-1",
		"pending_inserts": []map[string]any{{
			"id":     "9c30ef8e",
			"prd_id": "p-77",
			"reason": "tests found a gap",
		}},
	})); err != nil {
		t.Fatalf("update: %v", err)
	}

	res, err := h.s.handleAutomationGet(context.Background(), callReq(map[string]any{"id": "auto-1"}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	text := automationText(t, res)
	for _, want := range []string{"pending_inserts", "9c30ef8e", "p-77", "tests found a gap"} {
		if !strings.Contains(text, want) {
			t.Errorf("get text missing %q: %s", want, text)
		}
	}

	// The tool description says the ids feed automation_approve_insert.
	d := (&Server{}).toolAutomationGet().Description
	for _, want := range []string{"pending_inserts", "automation_approve_insert"} {
		if !strings.Contains(d, want) {
			t.Errorf("description does not mention %q: %s", want, d)
		}
	}
}
