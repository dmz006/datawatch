// REST tests for the Automation run-shape settings: max_parallel and stage_gate
// on create, update and get, a fork's own max_parallel and a decision_wait
// member's question inside the member list, the set_max_parallel and
// set_stage_gate control operations, and insert_member with a question. They run
// through the real autonomous manager, so the stored record, the version bump
// and the audit entry are what a client would see.

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// runShapeRecord is the run-shape subset of the Automation wire record.
type runShapeRecord struct {
	ID          string `json:"id"`
	Version     int64  `json:"version"`
	Status      string `json:"status"`
	MaxParallel int    `json:"max_parallel"`
	StageGate   bool   `json:"stage_gate"`
	Members     []struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		PRDID       string `json:"prd_id"`
		MaxParallel int    `json:"max_parallel"`
		Question    string `json:"question"`
		OnComplete  string `json:"on_complete"`
		Stage       string `json:"stage"`
	} `json:"members"`
	PendingInserts []struct {
		ID       string `json:"id"`
		Question string `json:"question"`
	} `json:"pending_inserts"`
	Description string `json:"description"`
}

func decodeRunShape(t *testing.T, body string) runShapeRecord {
	t.Helper()
	var a runShapeRecord
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatalf("decode automation: %v (body=%q)", err, body)
	}
	return a
}

func (a runShapeRecord) memberIndex(id string) int {
	for i, m := range a.Members {
		if m.ID == id {
			return i
		}
	}
	return -1
}

// forkMembers is a fork over two Automaton branches and the join that collects
// them, in member order fork, b1, b2, join.
func forkMembers(p1, p2 string) []any {
	return []any{
		map[string]any{"id": "f", "kind": "fork", "fork_members": []string{"b1", "b2"}},
		map[string]any{"id": "b1", "prd_id": p1, "stage": "build"},
		map[string]any{"id": "b2", "prd_id": p2, "stage": "review"},
		map[string]any{"id": "j", "kind": "join", "join_members": []string{"b1", "b2"}},
	}
}

func TestAutomationRESTCreateUpdateRunShape(t *testing.T) {
	s, mgr, _ := newAutomationAuditServer(t)
	p1, p2 := makeMemberPRD(t, mgr), makeMemberPRD(t, mgr)

	members := forkMembers(p1, p2)
	members[0].(map[string]any)["max_parallel"] = 1
	created := decodeRunShape(t, doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "shaped", "description": "keep me", "entry_member_id": "f", "members": members,
		"max_parallel": 3, "stage_gate": true,
	}, http.StatusCreated))
	if created.MaxParallel != 3 || !created.StageGate {
		t.Fatalf("create did not store the settings: max_parallel=%d stage_gate=%v", created.MaxParallel, created.StageGate)
	}
	if i := created.memberIndex("f"); i < 0 || created.Members[i].MaxParallel != 1 {
		t.Fatalf("fork max_parallel not stored: %+v", created.Members)
	}
	if i := created.memberIndex("b2"); i < 0 || created.Members[i].Stage != "review" {
		t.Fatalf("member stage not stored: %+v", created.Members)
	}

	// get returns them.
	got := decodeRunShape(t, doAutomation(t, s, http.MethodGet, "/api/automations/"+created.ID, nil, http.StatusOK))
	if got.MaxParallel != 3 || !got.StageGate {
		t.Fatalf("get lacks the settings: %+v", got)
	}

	// An update that omits them keeps them; 0 and false clear them.
	kept := decodeRunShape(t, doAutomation(t, s, http.MethodPut, "/api/automations/"+created.ID, map[string]any{
		"name": "shaped", "expected_version": created.Version,
	}, http.StatusOK))
	if kept.MaxParallel != 3 || !kept.StageGate {
		t.Fatalf("an update that omits the settings changed them: max_parallel=%d stage_gate=%v", kept.MaxParallel, kept.StageGate)
	}
	cleared := decodeRunShape(t, doAutomation(t, s, http.MethodPut, "/api/automations/"+created.ID, map[string]any{
		"name": "shaped", "expected_version": kept.Version, "max_parallel": 0, "stage_gate": false,
	}, http.StatusOK))
	if cleared.MaxParallel != 0 || cleared.StageGate {
		t.Fatalf("0 and false did not clear: max_parallel=%d stage_gate=%v", cleared.MaxParallel, cleared.StageGate)
	}

	// A negative limit is refused.
	doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "bad", "members": memberWire(makeMemberPRD(t, mgr)), "max_parallel": -1,
	}, http.StatusBadRequest)
}

func TestAutomationRESTDecisionWaitMemberRoundTrip(t *testing.T) {
	s, mgr, _ := newAutomationAuditServer(t)
	created := decodeRunShape(t, doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "gate", "entry_member_id": "m0",
		"members": []any{
			map[string]any{"id": "m0", "prd_id": makeMemberPRD(t, mgr), "on_complete": "ask"},
			map[string]any{"id": "ask", "kind": "decision_wait", "question": "Ship the migration now?"},
		},
	}, http.StatusCreated))
	i := created.memberIndex("ask")
	if i < 0 || created.Members[i].Kind != "decision_wait" || created.Members[i].Question != "Ship the migration now?" {
		t.Fatalf("decision_wait member = %+v", created.Members)
	}

	// A decision wait needs a question, and no other kind carries one.
	doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "noq", "members": []any{map[string]any{"id": "ask", "kind": "decision_wait"}},
	}, http.StatusBadRequest)
	doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "stray", "members": []any{map[string]any{"prd_id": makeMemberPRD(t, mgr), "question": "why?"}},
	}, http.StatusBadRequest)
}

func TestAutomationRESTSetMaxParallel(t *testing.T) {
	s, mgr, al := newAutomationAuditServer(t)
	p1, p2 := makeMemberPRD(t, mgr), makeMemberPRD(t, mgr)
	a := decodeRunShape(t, doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "shaped", "description": "keep me", "entry_member_id": "f", "members": forkMembers(p1, p2),
		"budget_policy": map[string]any{"ceiling_type": "dollar", "ceiling_value": 5, "window": "automation"},
	}, http.StatusCreated))
	path := "/api/automations/" + a.ID + "/set_max_parallel"

	got := decodeRunShape(t, doAutomation(t, s, http.MethodPost, path, map[string]any{
		"max_parallel": 3, "expected_version": a.Version, "actor": "alice", "reason": "lanes freed",
	}, http.StatusOK))
	if got.MaxParallel != 3 || got.Version != a.Version+1 {
		t.Fatalf("set_max_parallel result: max_parallel=%d version=%d (want 3, %d)", got.MaxParallel, got.Version, a.Version+1)
	}
	wantAudit(t, al, "automation_set_max_parallel", a.ID, map[string]any{
		"actor": "alice", "reason": "lanes freed", "max_parallel": float64(3), "version": float64(a.Version + 1),
	})
	// Only the one field changed: the description and the budget survive, unlike a PUT.
	raw := doAutomation(t, s, http.MethodGet, "/api/automations/"+a.ID, nil, http.StatusOK)
	if read := decodeRunShape(t, raw); read.Description != "keep me" || !strings.Contains(raw, `"budget_policy"`) {
		t.Fatalf("set_max_parallel touched other fields: %s", raw)
	}

	// member_id names a fork: that fork's limit, not the Automation's.
	forked := decodeRunShape(t, doAutomation(t, s, http.MethodPost, path, map[string]any{
		"max_parallel": 2, "member_id": "f",
	}, http.StatusOK))
	if i := forked.memberIndex("f"); i < 0 || forked.Members[i].MaxParallel != 2 || forked.MaxParallel != 3 {
		t.Fatalf("fork limit: members=%+v automation=%d", forked.Members, forked.MaxParallel)
	}
	wantAudit(t, al, "automation_set_max_parallel", a.ID, map[string]any{"member_id": "f", "max_parallel": float64(2)})

	// 0 clears.
	cleared := decodeRunShape(t, doAutomation(t, s, http.MethodPost, path, map[string]any{"max_parallel": 0}, http.StatusOK))
	if cleared.MaxParallel != 0 {
		t.Fatalf("0 did not clear: %d", cleared.MaxParallel)
	}

	// Refusals.
	doAutomation(t, s, http.MethodPost, path, map[string]any{}, http.StatusBadRequest)                                     // missing
	doAutomation(t, s, http.MethodPost, path, map[string]any{"max_parallel": -2}, http.StatusBadRequest)                   // negative
	doAutomation(t, s, http.MethodPost, path, map[string]any{"max_parallel": 1, "member_id": "b1"}, http.StatusBadRequest) // not a fork
	doAutomation(t, s, http.MethodPost, path, map[string]any{"max_parallel": 1, "member_id": "nope"}, http.StatusNotFound)
	doAutomation(t, s, http.MethodPost, path, map[string]any{"max_parallel": 1, "expected_version": 1}, http.StatusConflict) // stale
	doAutomation(t, s, http.MethodGet, path, nil, http.StatusMethodNotAllowed)
	doAutomation(t, s, http.MethodPost, "/api/automations/ffffffff/set_max_parallel", map[string]any{"max_parallel": 1}, http.StatusNotFound)
	if n := countAutomationAudit(t, al, "automation_set_max_parallel"); n != 3 {
		t.Errorf("audit entries = %d, want 3 (refusals write none)", n)
	}
}

func TestAutomationRESTSetStageGate(t *testing.T) {
	s, mgr, al := newAutomationAuditServer(t)
	a := decodeRunShape(t, doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "shaped", "entry_member_id": "f", "members": forkMembers(makeMemberPRD(t, mgr), makeMemberPRD(t, mgr)),
	}, http.StatusCreated))
	path := "/api/automations/" + a.ID + "/set_stage_gate"
	if a.StageGate {
		t.Fatal("a new Automation has the stage gate on")
	}

	on := decodeRunShape(t, doAutomation(t, s, http.MethodPost, path, map[string]any{
		"stage_gate": true, "expected_version": a.Version, "actor": "alice", "reason": "ordered stages",
	}, http.StatusOK))
	if !on.StageGate || on.Version != a.Version+1 {
		t.Fatalf("set_stage_gate on: stage_gate=%v version=%d", on.StageGate, on.Version)
	}
	wantAudit(t, al, "automation_set_stage_gate", a.ID, map[string]any{"actor": "alice", "stage_gate": true})

	off := decodeRunShape(t, doAutomation(t, s, http.MethodPost, path, map[string]any{"stage_gate": false}, http.StatusOK))
	if off.StageGate {
		t.Fatal("stage_gate=false did not turn the gate off")
	}
	wantAudit(t, al, "automation_set_stage_gate", a.ID, map[string]any{"stage_gate": false})

	doAutomation(t, s, http.MethodPost, path, map[string]any{}, http.StatusBadRequest)
	doAutomation(t, s, http.MethodPost, path, map[string]any{"stage_gate": true, "expected_version": 1}, http.StatusConflict)
	doAutomation(t, s, http.MethodPost, "/api/automations/ffffffff/set_stage_gate", map[string]any{"stage_gate": true}, http.StatusNotFound)
	if n := countAutomationAudit(t, al, "automation_set_stage_gate"); n != 2 {
		t.Errorf("audit entries = %d, want 2", n)
	}
}

func TestAutomationRESTRunShapeOnRunningAutomation(t *testing.T) {
	s, _, al, a := newActiveAutomation(t)

	// Both controls work on an active Automation, and on a paused one.
	got := decodeRunShape(t, doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_max_parallel",
		map[string]any{"max_parallel": 2, "expected_version": a.Version}, http.StatusOK))
	if got.MaxParallel != 2 || got.Status != "active" {
		t.Fatalf("active: max_parallel=%d status=%s", got.MaxParallel, got.Status)
	}
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/pause", map[string]any{"expected_version": got.Version}, http.StatusOK)
	got = decodeRunShape(t, doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_stage_gate",
		map[string]any{"stage_gate": true}, http.StatusOK))
	if !got.StageGate || got.Status != "paused" || got.MaxParallel != 2 {
		t.Fatalf("paused: stage_gate=%v status=%s max_parallel=%d", got.StageGate, got.Status, got.MaxParallel)
	}
	// Neither is allowed once the Automation has ended.
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/cancel", map[string]any{"cancel_in_flight": false, "expected_version": got.Version}, http.StatusOK)
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_max_parallel", map[string]any{"max_parallel": 1}, http.StatusConflict)
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_stage_gate", map[string]any{"stage_gate": false}, http.StatusConflict)
	if n := countAutomationAudit(t, al, "automation_set_max_parallel") + countAutomationAudit(t, al, "automation_set_stage_gate"); n != 2 {
		t.Errorf("audit entries = %d, want 2", n)
	}
}

func TestAutomationRESTInsertDecisionWait(t *testing.T) {
	s, _, al, a := newActiveAutomation(t)
	path := "/api/automations/" + a.ID + "/insert_member"

	// On a started Automation the insert is held, and carries the question.
	held := decodeRunShape(t, doAutomation(t, s, http.MethodPost, path, map[string]any{
		"expected_version": a.Version, "reason": "sign-off before release", "actor": "alice",
		"member": map[string]any{"kind": "decision_wait", "question": "  Ship it now?  ", "stage": "release"},
	}, http.StatusOK))
	if len(held.PendingInserts) != 1 || held.PendingInserts[0].Question != "Ship it now?" {
		t.Fatalf("pending inserts = %+v", held.PendingInserts)
	}
	wantAudit(t, al, "automation_insert_member", a.ID, map[string]any{"question": "Ship it now?", "stage": "release"})

	// Approving lands a decision_wait member at the tail of the chain.
	approved := decodeRunShape(t, doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/approve_insert", map[string]any{
		"expected_version": held.Version, "member_id": held.PendingInserts[0].ID,
	}, http.StatusOK))
	i := approved.memberIndex(held.PendingInserts[0].ID)
	if i < 0 || approved.Members[i].Kind != "decision_wait" || approved.Members[i].Question != "Ship it now?" || approved.Members[i].Stage != "release" {
		t.Fatalf("approved member = %+v", approved.Members)
	}
	if j := approved.memberIndex("m2"); j < 0 || approved.Members[j].OnComplete != approved.Members[i].ID {
		t.Fatalf("the chain tail does not lead to the decision wait: %+v", approved.Members)
	}

	// A question is a different target from an Automaton, so naming two is refused,
	// as is a kind other than decision_wait, a kind without a question, and a question over the cap.
	bad := func(member map[string]any) {
		t.Helper()
		doAutomation(t, s, http.MethodPost, path, map[string]any{"reason": "x", "member": member}, http.StatusBadRequest)
	}
	bad(map[string]any{"prd_id": "abcd1234", "question": "both?"})
	bad(map[string]any{"pending_item_id": "item1", "question": "both?"})
	bad(map[string]any{"kind": "decision_wait"})
	bad(map[string]any{"kind": "fork", "question": "q"})
	bad(map[string]any{"question": strings.Repeat("q", 4097)})
	bad(map[string]any{})
}

func TestAutomationRESTInsertDecisionWaitOnDraft(t *testing.T) {
	s, mgr, _ := newAutomationAuditServer(t)
	a := decodeRunShape(t, doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "draft", "members": memberWire(makeMemberPRD(t, mgr)),
	}, http.StatusCreated))
	got := decodeRunShape(t, doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/insert_member", map[string]any{
		"expected_version": a.Version, "reason": "ask first",
		"member": map[string]any{"question": "Proceed?"},
	}, http.StatusOK))
	if len(got.Members) != 2 || got.Members[1].Kind != "decision_wait" || got.Members[1].Question != "Proceed?" {
		t.Fatalf("a draft insert did not land the decision wait: %+v", got.Members)
	}
	if len(got.PendingInserts) != 0 {
		t.Fatalf("a draft insert was held: %+v", got.PendingInserts)
	}
}
