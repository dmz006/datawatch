// Phase 3a fork/join and nested-Automation member shapes through the
// automation_create / automation_update / autonomous_status MCP tools. The
// members_json argument must reach the REST body unmodified, and the status
// payload must come back with active_cursors and join_wait intact.

package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// fjMembersJSON is a fork over two prd branches, joined by a join member
// that uses a non-default branch rule.
const fjMembersJSON = `[
  {"id":"plan","prd_id":"p-plan","on_complete":"fork1","on_blocked":"","on_failed":"","added_reason":"entry"},
  {"id":"fork1","kind":"fork","fork_members":["api","ui"]},
  {"id":"api","prd_id":"p-api","on_complete":"join1","on_blocked":"","on_failed":""},
  {"id":"ui","prd_id":"p-ui","on_complete":"join1","on_blocked":"","on_failed":""},
  {"id":"join1","kind":"join","join_members":["api","ui"],"on_branch_failure":"any_one_is_enough","on_complete":"","on_blocked":"","on_failed":""}
]`

// fjNestedMembersJSON runs a nested child Automation as one branch of a
// fork, joined under the default all_must_succeed rule.
const fjNestedMembersJSON = `[
  {"id":"fork1","kind":"fork","fork_members":["b1","n1"]},
  {"id":"b1","prd_id":"p-b1","on_complete":"join1","on_blocked":"","on_failed":""},
  {"id":"n1","kind":"automation","automation_id":"child-7","on_complete":"join1","on_blocked":"","on_failed":""},
  {"id":"join1","kind":"join","join_members":["b1","n1"],"on_branch_failure":"all_must_succeed","on_complete":"","on_blocked":"","on_failed":""}
]`

// assertMembersForwarded checks that the proxied body carries the members
// exactly as the caller sent them: same member order, keys and values.
func assertMembersForwarded(t *testing.T, got any, sent string) {
	t.Helper()
	var want []any
	if err := json.Unmarshal([]byte(sent), &want); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("members forwarded = %v, want %v", got, want)
	}
}

func TestAutomationCreateToolForkJoinMemberForwarding(t *testing.T) {
	h := newAutomationHarness(t)
	res, err := h.s.handleAutomationCreate(context.Background(), callReq(map[string]any{
		"name":            "fork-join-chain",
		"entry_member_id": "plan",
		"members_json":    fjMembersJSON,
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if h.rec.method != "POST" || h.rec.path != "/api/automations" {
		t.Errorf("proxied %s %s", h.rec.method, h.rec.path)
	}
	assertMembersForwarded(t, h.rec.body["members"], fjMembersJSON)
	if h.rec.body["entry_member_id"] != "plan" {
		t.Errorf("entry_member_id = %v", h.rec.body["entry_member_id"])
	}
}

func TestAutomationCreateToolNestedMemberForwarding(t *testing.T) {
	h := newAutomationHarness(t)
	if _, err := h.s.handleAutomationCreate(context.Background(), callReq(map[string]any{
		"name":         "nested-chain",
		"members_json": fjNestedMembersJSON,
	})); err != nil {
		t.Fatalf("create: %v", err)
	}
	assertMembersForwarded(t, h.rec.body["members"], fjNestedMembersJSON)

	// The update path forwards the same member shapes to PUT.
	res, err := h.s.handleAutomationUpdate(context.Background(), callReq(map[string]any{
		"id":           "auto-1",
		"members_json": fjNestedMembersJSON,
	}))
	if err != nil || res == nil {
		t.Fatalf("update err=%v res=%v", err, res)
	}
	if h.rec.method != "PUT" || h.rec.path != "/api/automations/auto-1" {
		t.Errorf("update proxied %s %s", h.rec.method, h.rec.path)
	}
	assertMembersForwarded(t, h.rec.body["members"], fjNestedMembersJSON)

	for _, key := range []string{"kind", "automation_id", "fork_members", "join_members", "on_branch_failure"} {
		if !strings.Contains(automationMembersDesc(t, "automation_create"), key) {
			t.Errorf("automation_create members_json description omits %q", key)
		}
		if !strings.Contains(automationMembersDesc(t, "automation_update"), key) {
			t.Errorf("automation_update members_json description omits %q", key)
		}
	}
}

// automationMembersDesc returns the members_json argument description of
// the named Automation tool.
func automationMembersDesc(t *testing.T, tool string) string {
	t.Helper()
	s := &Server{}
	var prop map[string]any
	switch tool {
	case "automation_create":
		prop, _ = s.toolAutomationCreate().InputSchema.Properties["members_json"].(map[string]any)
	case "automation_update":
		prop, _ = s.toolAutomationUpdate().InputSchema.Properties["members_json"].(map[string]any)
	default:
		t.Fatalf("unknown tool %q", tool)
	}
	desc, _ := prop["description"].(string)
	return desc
}

func TestAutomationStatusToolActiveCursorsAndJoinWait(t *testing.T) {
	const busy = `{"running":true,"automations":[{"id":"ab12","name":"fork-join-chain","status":"active","pending_members":2,` +
		`"active_cursors":[{"member_id":"api","prd_id":"p-api","status":"running"},{"member_id":"n1","automation_id":"child-7","status":"running"}],` +
		`"join_wait":{"join_member_id":"join1","rule":"any_one_is_enough","pending_branches":["api","n1"],"failed_branches":["ui"]}}]}`

	s, rec := activateServer(t, 200, busy)
	res, err := s.handleAutonomousStatus(context.Background(), callReq(nil))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != "GET" || rec.path != "/api/autonomous/status" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}

	// The tool forwards the REST payload, so the decoded result must match it.
	var got, want map[string]any
	if err := json.Unmarshal([]byte(automationText(t, res)), &got); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(busy), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("status payload changed in transit:\n got %v\nwant %v", got, want)
	}

	// An idle Automation still reports active_cursors, as an empty array.
	const idle = `{"running":true,"automations":[{"id":"cd34","status":"active","active_cursors":[]}]}`
	s, _ = activateServer(t, 200, idle)
	res, err = s.handleAutonomousStatus(context.Background(), callReq(nil))
	if err != nil || res == nil {
		t.Fatalf("idle err=%v res=%v", err, res)
	}
	if text := automationText(t, res); !strings.Contains(text, `"active_cursors":[]`) {
		t.Errorf("idle status lost empty active_cursors: %s", text)
	}

	desc := (&Server{}).toolAutonomousStatus().Description
	for _, key := range []string{"active_cursors", "join_wait", "join_member_id", "pending_branches"} {
		if !strings.Contains(desc, key) {
			t.Errorf("autonomous_status description omits %q", key)
		}
	}
}
