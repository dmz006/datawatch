// REST tests for the operator control sub-paths under /api/automations/{id}/:
// pause, resume, cancel, skip_member, insert_member, remove_member,
// reorder_edges and approve_insert, driven through the real autonomous manager
// and coordinator.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
)

// ctlAutomation is the subset of the Automation wire record the control tests read.
type ctlAutomation struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
	Status  string `json:"status"`
	Members []struct {
		ID         string `json:"id"`
		PRDID      string `json:"prd_id"`
		Status     string `json:"status"`
		OnComplete string `json:"on_complete"`
	} `json:"members"`
	PendingInserts []struct {
		ID    string `json:"id"`
		PRDID string `json:"prd_id"`
	} `json:"pending_inserts"`
}

func (a ctlAutomation) member(id string) (status, onComplete string, ok bool) {
	for _, m := range a.Members {
		if m.ID == id {
			return m.Status, m.OnComplete, true
		}
	}
	return "", "", false
}

func decodeCtl(t *testing.T, body string) ctlAutomation {
	t.Helper()
	var a ctlAutomation
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatalf("decode automation: %v (body=%q)", err, body)
	}
	return a
}

// newActiveAutomation creates a chain m0 -> m1 -> m2 over approved member PRDs,
// activates it and returns the active record (m0 running, m1 and m2 pending).
func newActiveAutomation(t *testing.T) (*Server, *autonomous.Manager, *audit.Log, ctlAutomation) {
	t.Helper()
	s, mgr, al := newActivateServer(t, true)
	members := make([]any, 0, 3)
	var entryPRD string
	for i, edge := range []string{"m1", "m2", ""} {
		prdID := makeMemberPRD(t, mgr)
		prd, ok := mgr.Store().GetPRD(prdID)
		if !ok {
			t.Fatalf("member prd %s missing", prdID)
		}
		prd.Status = autonomous.PRDApproved
		if err := mgr.Store().SavePRD(prd); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
		if i == 0 {
			entryPRD = prdID
		}
		members = append(members, map[string]any{"id": "m" + string(rune('0'+i)), "prd_id": prdID, "on_complete": edge})
	}
	created := doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "ctl", "entry_member_id": "m0", "members": members,
	}, http.StatusCreated)
	id := decodeCtl(t, created).ID
	a := decodeCtl(t, doAutomation(t, s, http.MethodPost, "/api/automations/"+id+"/activate", nil, http.StatusOK))
	if a.Status != "active" {
		t.Fatalf("activated status = %q, want active", a.Status)
	}
	// The no-op starter launches nothing, so mark the entry member's PRD running
	// as a real start would; otherwise a resume reads it as a run that errored.
	prd, _ := mgr.Store().GetPRD(entryPRD)
	prd.Status = autonomous.PRDRunning
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return s, mgr, al, a
}

// ctl posts one control request and decodes the Automation it returns.
func ctl(t *testing.T, s *Server, a ctlAutomation, op string, body map[string]any, want int) ctlAutomation {
	t.Helper()
	if body == nil {
		body = map[string]any{}
	}
	if _, set := body["expected_version"]; !set {
		body["expected_version"] = a.Version
	}
	out := doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/"+op, body, want)
	if want != http.StatusOK {
		return ctlAutomation{}
	}
	return decodeCtl(t, out)
}

func wantAudit(t *testing.T, al *audit.Log, action, id string, fields map[string]any) {
	t.Helper()
	entries, err := al.Read(audit.QueryFilter{Action: action})
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	for _, e := range entries {
		if e.Details["resource_type"] != "automation" || e.Details["resource_id"] != id {
			continue
		}
		match := true
		for k, v := range fields {
			if e.Details[k] != v {
				match = false
			}
		}
		if match {
			return
		}
	}
	t.Errorf("no %s audit entry for %s with %v (got %+v)", action, id, fields, entries)
}

func TestAutomationRESTControl(t *testing.T) {
	t.Run("pause then resume", func(t *testing.T) {
		s, _, al, a := newActiveAutomation(t)
		paused := ctl(t, s, a, "pause", map[string]any{"actor": "alice", "reason": "lunch"}, http.StatusOK)
		if paused.Status != "paused" || paused.Version != a.Version+1 {
			t.Fatalf("pause: status=%s version=%d", paused.Status, paused.Version)
		}
		wantAudit(t, al, "automation_pause", a.ID, map[string]any{"actor": "alice", "reason": "lunch", "expected_version": float64(a.Version)})
		resumed := ctl(t, s, paused, "resume", nil, http.StatusOK)
		if resumed.Status != "active" {
			t.Fatalf("resume: status=%s", resumed.Status)
		}
		wantAudit(t, al, "automation_resume", a.ID, map[string]any{"actor": "operator"})
		// Resuming an active Automation is a state conflict.
		ctl(t, s, resumed, "resume", nil, http.StatusConflict)
	})

	t.Run("skip member", func(t *testing.T) {
		s, _, al, a := newActiveAutomation(t)
		got := ctl(t, s, a, "skip_member", map[string]any{"member_id": "m0", "reason": "not needed"}, http.StatusOK)
		if st, _, _ := got.member("m0"); st != "skipped" {
			t.Fatalf("m0 status = %q, want skipped", st)
		}
		if st, _, _ := got.member("m1"); st != "running" {
			t.Fatalf("m1 status = %q, want running (skip follows on_complete)", st)
		}
		wantAudit(t, al, "automation_skip_member", a.ID, map[string]any{"member_id": "m0", "edge": "on_complete", "reason": "not needed"})
		// An edge that does not exist is a 400.
		ctl(t, s, got, "skip_member", map[string]any{"member_id": "m1", "edge": "sideways"}, http.StatusBadRequest)
	})

	t.Run("remove member", func(t *testing.T) {
		s, _, al, a := newActiveAutomation(t)
		got := ctl(t, s, a, "remove_member", map[string]any{"member_id": "m2"}, http.StatusOK)
		if _, _, ok := got.member("m2"); ok || len(got.Members) != 2 {
			t.Fatalf("m2 still present: %+v", got.Members)
		}
		if _, onComplete, _ := got.member("m1"); onComplete != "" {
			t.Fatalf("m1 on_complete = %q, want cleared", onComplete)
		}
		wantAudit(t, al, "automation_remove_member", a.ID, map[string]any{"member_id": "m2"})
		// The running entry member cannot be removed.
		ctl(t, s, got, "remove_member", map[string]any{"member_id": "m0"}, http.StatusConflict)
		ctl(t, s, got, "remove_member", map[string]any{"member_id": "nope"}, http.StatusNotFound)
	})

	t.Run("reorder edges", func(t *testing.T) {
		s, _, al, a := newActiveAutomation(t)
		got := ctl(t, s, a, "reorder_edges", map[string]any{"member_id": "m1", "on_complete": "m2", "on_failed": "m2"}, http.StatusOK)
		if _, onComplete, _ := got.member("m1"); onComplete != "m2" {
			t.Fatalf("m1 on_complete = %q, want m2", onComplete)
		}
		wantAudit(t, al, "automation_reorder_edges", a.ID, map[string]any{"member_id": "m1", "on_failed": "m2"})
		// The running member cannot be rewired.
		ctl(t, s, got, "reorder_edges", map[string]any{"member_id": "m0", "on_complete": "m2"}, http.StatusConflict)
	})

	t.Run("insert member", func(t *testing.T) {
		s, mgr, al, a := newActiveAutomation(t)
		prdID := makeMemberPRD(t, mgr)
		// A reason is required.
		ctl(t, s, a, "insert_member", map[string]any{"member": map[string]any{"prd_id": prdID}}, http.StatusBadRequest)
		got := ctl(t, s, a, "insert_member", map[string]any{
			"member": map[string]any{"prd_id": prdID, "name": "late addition"}, "reason": "found a gap", "actor": "alice",
		}, http.StatusOK)
		if len(got.PendingInserts) != 1 || got.PendingInserts[0].PRDID != prdID || len(got.Members) != 3 {
			t.Fatalf("insert on a running automation should be held: pending=%+v members=%d", got.PendingInserts, len(got.Members))
		}
		wantAudit(t, al, "automation_insert_member", a.ID, map[string]any{"prd_id": prdID, "urgent": false, "reason": "found a gap", "actor": "alice"})
		// A missing prd_id is a 400.
		ctl(t, s, got, "insert_member", map[string]any{"member": map[string]any{}, "reason": "x"}, http.StatusBadRequest)
	})

	t.Run("cancel", func(t *testing.T) {
		s, _, al, a := newActiveAutomation(t)
		got := ctl(t, s, a, "cancel", map[string]any{"cancel_in_flight": false, "reason": "stop"}, http.StatusOK)
		if got.Status != "cancelled" {
			t.Fatalf("cancel: status=%s", got.Status)
		}
		wantAudit(t, al, "automation_cancel", a.ID, map[string]any{"cancel_in_flight": false, "reason": "stop"})
	})

	t.Run("routing", func(t *testing.T) {
		s, _, _, a := newActiveAutomation(t)
		for _, op := range automationControlOps {
			doAutomation(t, s, http.MethodGet, "/api/automations/"+a.ID+"/"+op, nil, http.StatusMethodNotAllowed)
			doAutomation(t, s, http.MethodDelete, "/api/automations/"+a.ID+"/"+op, nil, http.StatusMethodNotAllowed)
		}
		doAutomation(t, s, http.MethodPost, "/api/automations/deadbeef/pause", map[string]any{"expected_version": 1}, http.StatusNotFound)
		req := httptest.NewRequest(http.MethodPost, "/api/automations/"+a.ID+"/pause", strings.NewReader("{nope"))
		rr := httptest.NewRecorder()
		s.handleAutomations(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("malformed body: want 400, got %d", rr.Code)
		}
	})
}

func TestAutomationRESTCancel(t *testing.T) {
	s, _, _, a := newActiveAutomation(t)

	// The in-flight choice has no default: a body without it is a 400 that names
	// both values.
	body := doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/cancel",
		map[string]any{"expected_version": a.Version}, http.StatusBadRequest)
	for _, want := range []string{"cancel_in_flight=true", "cancel_in_flight=false"} {
		if !strings.Contains(body, want) {
			t.Errorf("400 message %q does not name %s", body, want)
		}
	}
	// An explicit null is as good as missing.
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/cancel",
		map[string]any{"expected_version": a.Version, "cancel_in_flight": nil}, http.StatusBadRequest)

	// A stale expected_version is a 409 even with the choice present.
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/cancel",
		map[string]any{"expected_version": a.Version + 7, "cancel_in_flight": true}, http.StatusConflict)

	// Either explicit value is accepted. The starter used here cannot end member
	// sessions, so cancel_in_flight=true is refused; false goes through.
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/cancel",
		map[string]any{"expected_version": a.Version, "cancel_in_flight": true}, http.StatusConflict)
	got := decodeCtl(t, doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/cancel",
		map[string]any{"expected_version": a.Version, "cancel_in_flight": false}, http.StatusOK))
	if got.Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
	// A cancelled Automation cannot be cancelled again.
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/cancel",
		map[string]any{"expected_version": got.Version, "cancel_in_flight": false}, http.StatusConflict)
}

func TestAutomationRESTApprove(t *testing.T) {
	s, mgr, al, a := newActiveAutomation(t)

	// An unknown pending insert is refused.
	ctl(t, s, a, "approve_insert", map[string]any{"member_id": "nosuch"}, http.StatusNotFound)

	prdID := makeMemberPRD(t, mgr)
	held := ctl(t, s, a, "insert_member", map[string]any{
		"member": map[string]any{"prd_id": prdID}, "reason": "needed",
	}, http.StatusOK)
	if len(held.PendingInserts) != 1 {
		t.Fatalf("want one pending insert, got %+v", held.PendingInserts)
	}
	pendingID := held.PendingInserts[0].ID

	// A stale version is a 409.
	ctl(t, s, held, "approve_insert", map[string]any{"member_id": pendingID, "expected_version": held.Version - 1}, http.StatusConflict)
	// Only the operator may approve.
	ctl(t, s, held, "approve_insert", map[string]any{"member_id": pendingID, "actor": "agent:x1"}, http.StatusConflict)

	got := ctl(t, s, held, "approve_insert", map[string]any{"member_id": pendingID, "reason": "ok"}, http.StatusOK)
	if len(got.PendingInserts) != 0 || len(got.Members) != 4 {
		t.Fatalf("after approve: pending=%d members=%d, want 0 and 4", len(got.PendingInserts), len(got.Members))
	}
	if _, _, ok := got.member(pendingID); !ok {
		t.Errorf("approved member %s not in the graph", pendingID)
	}
	wantAudit(t, al, "automation_approve_insert", a.ID, map[string]any{"member_id": pendingID, "reason": "ok"})
}
