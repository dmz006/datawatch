// REST tests for POST /api/automations/{id}/return_to_draft — the thin
// sibling of activate that returns a blocked Automation (nothing started) to
// draft — plus one activation of a fork entry through the REST surface.
//
// The success round-trip (draft <- blocked <- draft, CAS, audit) is covered
// at the manager level in
// internal/autonomous/automation_draft_return_test.go, because the
// returnable state — blocked with no started member and no cursor — is a
// deliberate operator-recovery shape rather than something the stepper
// leaves behind during a normal activation.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
)

// returnDraftSpy answers the one method under test and records the
// arguments the REST layer passes down; the rest of the AutonomousAPI
// surface is provided by the embedded fakeOrchAutonomous.
type returnDraftSpy struct {
	fakeOrchAutonomous
	calledID      string
	calledActor   string
	calledVersion int64
	result        any
	err           error
}

func (r *returnDraftSpy) ReturnAutomationToDraft(id, actor string, v int64) (any, error) {
	r.calledID, r.calledActor, r.calledVersion = id, actor, v
	return r.result, r.err
}

// The REST endpoint is a thin pass-through: it parses actor and
// expected_version, defaults the actor, maps the manager's 404/409 error
// prefixes to their status codes, and audits the call.
func TestAutomationReturnToDraftRestWiring(t *testing.T) {
	dir := t.TempDir()
	al, err := audit.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })
	spy := &returnDraftSpy{
		result: map[string]any{"id": "id1", "status": "draft", "version": 5},
	}
	s := &Server{
		cfg:           config.DefaultConfig(),
		cfgPath:       filepath.Join(dir, "config.yaml"),
		autonomousMgr: spy,
		auditLog:      al,
	}

	out := doAutomation(t, s, http.MethodPost, "/api/automations/id1/return_to_draft",
		map[string]any{"actor": "dmz", "expected_version": 4}, http.StatusOK)
	if !strings.Contains(out, `"draft"`) {
		t.Errorf("response = %q, want the manager's draft record", out)
	}
	if spy.calledID != "id1" || spy.calledActor != "dmz" || spy.calledVersion != 4 {
		t.Errorf("manager call = (%q %q %d), want (id1 dmz 4)", spy.calledID, spy.calledActor, spy.calledVersion)
	}

	// An omitted actor defaults to operator; an omitted version is zero.
	doAutomation(t, s, http.MethodPost, "/api/automations/id1/return_to_draft", nil, http.StatusOK)
	if spy.calledActor != "operator" || spy.calledVersion != 0 {
		t.Errorf("defaulted manager call = (%q %d), want (operator 0)", spy.calledActor, spy.calledVersion)
	}

	// The manager's status prefixes map to the wire's codes, with the
	// manager's reason kept in the body.
	spy.err = fmt.Errorf("409: automation id1 has a cursor on member m0")
	conflict := doAutomation(t, s, http.MethodPost, "/api/automations/id1/return_to_draft", nil, http.StatusConflict)
	if !strings.Contains(conflict, "cursor") {
		t.Errorf("409 body = %q, want the manager's reason", conflict)
	}
	spy.err = fmt.Errorf("404: automation id1 not found")
	doAutomation(t, s, http.MethodPost, "/api/automations/id1/return_to_draft", nil, http.StatusNotFound)
	spy.err = fmt.Errorf("something else")
	doAutomation(t, s, http.MethodPost, "/api/automations/id1/return_to_draft", nil, http.StatusBadRequest)

	// A wrong method is rejected before the manager is reached, and the audit
	// trail carries the operator's identity.
	spy.calledActor = ""
	doAutomation(t, s, http.MethodGet, "/api/automations/id1/return_to_draft", nil, http.StatusMethodNotAllowed)
	if spy.calledActor != "" {
		t.Errorf("GET reached the manager")
	}
	entries, err := al.Read(audit.QueryFilter{Action: "automation_return_to_draft"})
	if err != nil || len(entries) == 0 {
		t.Fatalf("no automation_return_to_draft audit entry (err=%v)", err)
	}
	var found bool
	for _, e := range entries {
		if e.Details["resource_id"] == "id1" && e.Details["actor"] == "dmz" {
			found = true
		}
	}
	if !found {
		t.Errorf("audit entries lack resource_id/actor: %+v", entries)
	}
}

// A blocked Automation the gate left a cursor on is not returnable: it was
// started, and the operator has to cancel it instead. Draft is refused too,
// because it is already a draft.
func TestAutomationReturnToDraftRefusals(t *testing.T) {
	s, mgr, _ := newActivateServer(t, true)
	id := createDraftAutomation(t, s, mgr)

	doAutomation(t, s, http.MethodPost, "/api/automations/"+id+"/return_to_draft", nil, http.StatusConflict)

	// Gate-denied activation: blocked with the cursor on the member.
	out := doAutomation(t, s, http.MethodPost, "/api/automations/"+id+"/activate", nil, http.StatusOK)
	var act struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &act); err != nil || act.Status != "blocked" {
		t.Fatalf("activate response = %q (err=%v body=%s)", act.Status, err, out)
	}
	conflict := doAutomation(t, s, http.MethodPost, "/api/automations/"+id+"/return_to_draft", nil, http.StatusConflict)
	if !strings.Contains(conflict, "cursor") {
		t.Errorf("conflict body = %q, want the cursor named", conflict)
	}

	// Refusals that do not depend on the blocked state: unknown ID is a 404,
	// a wrong method a 405, a malformed body a 400.
	doAutomation(t, s, http.MethodPost, "/api/automations/deadbeef/return_to_draft", nil, http.StatusNotFound)
	doAutomation(t, s, http.MethodGet, "/api/automations/"+id+"/return_to_draft", nil, http.StatusMethodNotAllowed)
	req := httptest.NewRequest(http.MethodPost, "/api/automations/"+id+"/return_to_draft", strings.NewReader("{nope"))
	rr := httptest.NewRecorder()
	s.handleAutomations(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("malformed body: want 400, got %d", rr.Code)
	}
}

// A fork entry member activates over REST exactly like any other entry: the
// fork fires (completed/forked) and its branches go through their own gates;
// with both branch PRDs still drafts the parent lands in blocked with the
// branches gate-denied.
func TestAutomationForkEntryActivatesOverREST(t *testing.T) {
	s, mgr, _ := newActivateServer(t, true)
	prdA := makeMemberPRD(t, mgr)
	prdB := makeMemberPRD(t, mgr)

	// A child Automation for the nested branch.
	childOut := doAutomation(t, s, http.MethodPost, "/api/automations",
		map[string]any{"name": "child", "members": memberWire(prdB)}, http.StatusCreated)
	var child struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(childOut), &child); err != nil || child.ID == "" {
		t.Fatalf("decode child create: %v (%q)", err, childOut)
	}

	parentOut := doAutomation(t, s, http.MethodPost, "/api/automations",
		fjBody("fj", fjMembers(prdA, child.ID)), http.StatusCreated)
	var parent struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(parentOut), &parent); err != nil || parent.ID == "" {
		t.Fatalf("decode parent create: %v (%q)", err, parentOut)
	}

	out := doAutomation(t, s, http.MethodPost, "/api/automations/"+parent.ID+"/activate",
		map[string]any{"actor": "dmz"}, http.StatusOK)
	var act struct {
		Status  string `json:"status"`
		Members []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Outcome string `json:"outcome"`
		} `json:"members"`
	}
	if err := json.Unmarshal([]byte(out), &act); err != nil {
		t.Fatalf("decode activate: %v (%q)", err, out)
	}
	if act.Status != "blocked" && act.Status != "active" {
		t.Fatalf("status = %q, want blocked or active", act.Status)
	}
	byID := map[string]struct {
		Status  string `json:"status"`
		Outcome string `json:"outcome"`
	}{}
	for _, m := range act.Members {
		byID[m.ID] = struct {
			Status  string `json:"status"`
			Outcome string `json:"outcome"`
		}{m.Status, m.Outcome}
	}
	fork, ok := byID["f1"]
	if !ok {
		t.Fatalf("members = %+v, want the f1 fork member", act.Members)
	}
	if fork.Status != "completed" || fork.Outcome != "forked" {
		t.Errorf("fork = %+v, want completed/forked (it fired its branches)", fork)
	}
	if act.Status == "blocked" {
		b, ok := byID["b1"]
		if !ok {
			t.Errorf("members = %+v, want branch b1", act.Members)
		} else if b.Status != "failed" || b.Outcome != "gate-denied" {
			t.Errorf("branch b1 = %+v, want failed/gate-denied (its PRD is still a draft)", b)
		}
		if n, ok := byID["n1"]; !ok || n.Status != "failed" {
			t.Errorf("branch n1 = %+v (members %+v), want failed (its child Automation is a draft)", n, act.Members)
		}
	}
}
