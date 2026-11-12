// REST tests for the Phase 3a multi-cursor surface on /api/automations and
// GET /api/autonomous/status (docs/plans/2026-10-14-automation-orchestration-design.md —
// "Member kinds", "Validation addenda", "autonomous_status extension" and
// "Phase 3a parity deltas"): fork/join and nested member fields round-trip
// through the existing CRUD endpoints, validation failures are 400s naming the
// offending member, a stale expected_version is a 409 on update and activate,
// and the status row carries active_cursors and join_wait.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// fjMembers is the wire graph the tests share: fork f1 starts branch b1 (a
// member PRD) and branch n1 (a nested child Automation) at once, and join j1
// collects both.
func fjMembers(prdID, childID string) []any {
	return []any{
		map[string]any{"id": "f1", "kind": "fork", "fork_members": []string{"b1", "n1"}},
		map[string]any{"id": "b1", "prd_id": prdID},
		map[string]any{"id": "n1", "kind": "automation", "automation_id": childID},
		map[string]any{"id": "j1", "kind": "join", "join_members": []string{"b1", "n1"}, "on_branch_failure": "all_must_succeed"},
	}
}

// fjBody wraps members in the create envelope, with f1 as the entry member.
func fjBody(name string, members []any) map[string]any {
	return map[string]any{
		"name": name, "actor": "operator", "reason": "fork/join",
		"entry_member_id": "f1", "members": members,
	}
}

// wireMemberOf returns the wire object for member id inside a body built by
// fjBody, so a case can break one field of it.
func wireMemberOf(body map[string]any, id string) map[string]any {
	for _, m := range body["members"].([]any) {
		if mm := m.(map[string]any); mm["id"] == id {
			return mm
		}
	}
	return nil
}

// fjWireMember is the subset of a stored member the CRUD tests read back.
type fjWireMember struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	PRDID           string   `json:"prd_id"`
	AutomationID    string   `json:"automation_id"`
	ForkMembers     []string `json:"fork_members"`
	JoinMembers     []string `json:"join_members"`
	OnBranchFailure string   `json:"on_branch_failure"`
}

// assertFJMembers checks that a fork/join automation body carries every new
// member field intact, keyed by member ID.
func assertFJMembers(t *testing.T, body, child string) {
	t.Helper()
	var a struct {
		Members []fjWireMember `json:"members"`
	}
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatalf("decode members: %v (body=%q)", err, body)
	}
	byID := map[string]fjWireMember{}
	for _, m := range a.Members {
		byID[m.ID] = m
	}
	if m := byID["b1"]; m.Kind != "" || m.PRDID == "" {
		t.Errorf("prd member b1 = %+v, want default kind and a prd_id", m)
	}
	if m := byID["f1"]; m.Kind != "fork" || strings.Join(m.ForkMembers, ",") != "b1,n1" {
		t.Errorf("fork f1 = %+v, want kind fork over b1,n1", m)
	}
	if m := byID["n1"]; m.Kind != "automation" || m.AutomationID != child {
		t.Errorf("nested n1 = %+v, want kind automation for %s", m, child)
	}
	if m := byID["j1"]; m.Kind != "join" || strings.Join(m.JoinMembers, ",") != "b1,n1" || m.OnBranchFailure != "all_must_succeed" {
		t.Errorf("join j1 = %+v, want kind join over b1,n1 with all_must_succeed", m)
	}
}

func TestAutomationRESTForkJoinNestedCRUD(t *testing.T) {
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	child := createDraftAutomation(t, s, mgr)
	prd := makeMemberPRD(t, mgr)

	// POST a fork/join graph with a nested branch -> 201, draft, version 1.
	created := doAutomation(t, s, http.MethodPost, "/api/automations", fjBody("fork join", fjMembers(prd, child)), http.StatusCreated)
	var a struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(created), &a); err != nil || a.ID == "" {
		t.Fatalf("decode create response: %v (body=%q)", err, created)
	}
	if a.Version != 1 || a.Status != "draft" {
		t.Fatalf("create: version=%d status=%q, want 1 / draft", a.Version, a.Status)
	}
	assertFJMembers(t, created, child)

	// GET by id returns the same member shapes.
	got := doAutomation(t, s, http.MethodGet, "/api/automations/"+a.ID, nil, http.StatusOK)
	assertFJMembers(t, got, child)

	// PUT with the version we saw -> 200, version 2, shapes intact.
	updated := doAutomation(t, s, http.MethodPut, "/api/automations/"+a.ID, map[string]any{
		"name": "fork join v2", "actor": "operator", "reason": "rename",
		"expected_version": 1, "entry_member_id": "f1", "members": fjMembers(prd, child),
	}, http.StatusOK)
	var up struct {
		Name    string `json:"name"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal([]byte(updated), &up); err != nil || up.Name != "fork join v2" || up.Version != 2 {
		t.Fatalf("update: name=%q version=%d err=%v, want v2 / 2 (body=%q)", up.Name, up.Version, err, updated)
	}
	assertFJMembers(t, updated, child)

	// DELETE a draft -> 204, and it is gone.
	doAutomation(t, s, http.MethodDelete, "/api/automations/"+a.ID, nil, http.StatusNoContent)
	doAutomation(t, s, http.MethodGet, "/api/automations/"+a.ID, nil, http.StatusNotFound)
}

func TestAutomationRESTForkJoinValidation400s(t *testing.T) {
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	child := createDraftAutomation(t, s, mgr)

	// Each case breaks one rule of the Phase 3a validation addenda. The 400
	// body must name the offending member ID.
	cases := []struct {
		name   string
		mutate func(body map[string]any)
		want   string
	}{
		{"fork with one branch", func(b map[string]any) { wireMemberOf(b, "f1")["fork_members"] = []string{"b1"} }, "f1"},
		{"fork carries an outcome edge", func(b map[string]any) { wireMemberOf(b, "f1")["on_complete"] = "b1" }, "f1"},
		{"branch carries an outcome edge", func(b map[string]any) { wireMemberOf(b, "b1")["on_failed"] = "n1" }, "b1"},
		{"join with one branch", func(b map[string]any) { wireMemberOf(b, "j1")["join_members"] = []string{"b1"} }, "j1"},
		{"join carries on_failed", func(b map[string]any) { wireMemberOf(b, "j1")["on_failed"] = "b1" }, "j1"},
		{"join as entry member", func(b map[string]any) { b["entry_member_id"] = "j1" }, "j1"},
		{"unknown kind", func(b map[string]any) { wireMemberOf(b, "n1")["kind"] = "loop" }, "n1"},
		{"unknown on_branch_failure", func(b map[string]any) { wireMemberOf(b, "j1")["on_branch_failure"] = "sometimes" }, "j1"},
		{"prd member carries fork_members", func(b map[string]any) { wireMemberOf(b, "b1")["fork_members"] = []string{"b1", "n1"} }, "b1"},
		{"fork carries prd_id", func(b map[string]any) { wireMemberOf(b, "f1")["prd_id"] = "p-none" }, "f1"},
		{"nested child does not exist", func(b map[string]any) { wireMemberOf(b, "n1")["automation_id"] = "deadbeef" }, "n1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fjBody("bad graph", fjMembers(makeMemberPRD(t, mgr), child))
			tc.mutate(body)
			out := doAutomation(t, s, http.MethodPost, "/api/automations", body, http.StatusBadRequest)
			if !strings.Contains(out, tc.want) {
				t.Fatalf("400 body %q does not name member %q", out, tc.want)
			}
		})
	}
}

func TestAutomationRESTUpdateStaleVersion409(t *testing.T) {
	s, mgr, _ := newActivateServer(t, true)
	child := createDraftAutomation(t, s, mgr)
	prd := makeMemberPRD(t, mgr)

	created := doAutomation(t, s, http.MethodPost, "/api/automations", fjBody("stale", fjMembers(prd, child)), http.StatusCreated)
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(created), &a); err != nil || a.ID == "" {
		t.Fatalf("decode create response: %v (body=%q)", err, created)
	}

	// Version 1 -> 2 with the current version.
	doAutomation(t, s, http.MethodPut, "/api/automations/"+a.ID, map[string]any{
		"name": "stale v2", "actor": "operator", "reason": "current",
		"expected_version": 1, "entry_member_id": "f1", "members": fjMembers(prd, child),
	}, http.StatusOK)

	// A writer still holding version 1 is refused, and nothing is written.
	doAutomation(t, s, http.MethodPut, "/api/automations/"+a.ID, map[string]any{
		"name": "stale v3", "actor": "operator", "reason": "stale",
		"expected_version": 1, "entry_member_id": "f1", "members": fjMembers(prd, child),
	}, http.StatusConflict)

	// The same stale version on activate is a 409, and the draft stays a draft.
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/activate", map[string]any{
		"actor": "operator", "reason": "stale", "expected_version": 1,
	}, http.StatusConflict)

	got := doAutomation(t, s, http.MethodGet, "/api/automations/"+a.ID, nil, http.StatusOK)
	var g struct {
		Name    string `json:"name"`
		Version int64  `json:"version"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(got), &g); err != nil || g.Name != "stale v2" || g.Version != 2 || g.Status != "draft" {
		t.Fatalf("after stale writes: name=%q version=%d status=%q err=%v, want stale v2 / 2 / draft", g.Name, g.Version, g.Status, err)
	}

	// The current version activates it.
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/activate", map[string]any{
		"actor": "operator", "reason": "current", "expected_version": 2,
	}, http.StatusOK)
}

// fjStatusCursor and fjStatusRow mirror the active_cursors and join_wait
// fields of an automations[] row on GET /api/autonomous/status.
type fjStatusCursor struct {
	MemberID     string `json:"member_id"`
	PRDID        string `json:"prd_id"`
	AutomationID string `json:"automation_id"`
	Status       string `json:"status"`
}

type fjStatusRow struct {
	ID              string           `json:"id"`
	Status          string           `json:"status"`
	CurrentMemberID string           `json:"current_member_id"`
	ActiveCursors   []fjStatusCursor `json:"active_cursors"`
	JoinWait        *struct {
		JoinMemberID    string   `json:"join_member_id"`
		Rule            string   `json:"rule"`
		PendingBranches []string `json:"pending_branches"`
		FailedBranches  []string `json:"failed_branches"`
	} `json:"join_wait"`
}

func TestAutomationRESTStatusActiveCursorsJoinWait(t *testing.T) {
	dataDir := t.TempDir()
	s, mgr := newAutomationRESTServer(t, dataDir)
	child := createDraftAutomation(t, s, mgr)
	livePRD := makeMemberPRD(t, mgr)

	// A draft fork/join Automation: its status row has no cursors and no join wait.
	draftBody := doAutomation(t, s, http.MethodPost, "/api/automations", fjBody("draft fork", fjMembers(makeMemberPRD(t, mgr), child)), http.StatusCreated)
	var d struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(draftBody), &d); err != nil || d.ID == "" {
		t.Fatalf("decode draft create: %v (body=%q)", err, draftBody)
	}

	// Reaching a live join needs approved member PRDs, which a REST test cannot
	// produce, so the running state is seeded into automations.jsonl and the
	// daemon is restarted over it. The live record: fork f1 has fired over
	// branch b1 (gate-denied, failed) and nested branch n1 (running), and join
	// j1 is waiting on n1.
	const liveID = "a0000077"
	live := autonomous.Automation{
		ID:            liveID,
		Name:          "fork live",
		Status:        autonomous.AutomationActive,
		EntryMemberID: "f1",
		ActiveCursors: []string{"n1"},
		Members: []autonomous.AutomationMember{
			{ID: "f1", Kind: autonomous.MemberKindFork, ForkMembers: []string{"b1", "n1"}, Status: autonomous.MemberCompleted, Outcome: "forked"},
			{ID: "b1", PRDID: livePRD, Status: autonomous.MemberFailed, Outcome: "gate-denied"},
			{ID: "n1", Kind: autonomous.MemberKindAutomation, AutomationID: child, Status: autonomous.MemberRunning},
			{ID: "j1", Kind: autonomous.MemberKindJoin, JoinMembers: []string{"b1", "n1"}, Status: autonomous.MemberRunning},
		},
	}
	all := []autonomous.Automation{}
	for _, a := range mgr.ListAutomations() {
		all = append(all, *a)
	}
	all = append(all, live)
	var file []byte
	for _, a := range all {
		line, err := json.Marshal(a)
		if err != nil {
			t.Fatalf("marshal automation %s: %v", a.ID, err)
		}
		file = append(file, line...)
		file = append(file, '\n')
	}
	var automationsFile string
	if err := filepath.Walk(dataDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == "automations.jsonl" {
			automationsFile = p
		}
		return err
	}); err != nil || automationsFile == "" {
		t.Fatalf("automations.jsonl not found under %s: %v", dataDir, err)
	}
	if err := os.WriteFile(automationsFile, file, 0o600); err != nil {
		t.Fatalf("seed automations.jsonl: %v", err)
	}

	// The restarted server reads the seeded records back.
	s2, _ := newAutomationRESTServer(t, dataDir)
	rr := httptest.NewRecorder()
	s2.handleAutonomousStatus(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var st struct {
		Automations []fjStatusRow `json:"automations"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	rows := map[string]fjStatusRow{}
	for _, r := range st.Automations {
		rows[r.ID] = r
	}

	lr, ok := rows[liveID]
	if !ok {
		t.Fatalf("status has no row for %s: %+v", liveID, st.Automations)
	}
	if len(lr.ActiveCursors) != 1 || lr.ActiveCursors[0] != (fjStatusCursor{MemberID: "n1", AutomationID: child, Status: "running"}) {
		t.Errorf("active_cursors = %+v, want one nested cursor n1 for %s running", lr.ActiveCursors, child)
	}
	if w := lr.JoinWait; w == nil || w.JoinMemberID != "j1" || w.Rule != "all_must_succeed" ||
		strings.Join(w.PendingBranches, ",") != "n1" || strings.Join(w.FailedBranches, ",") != "b1" {
		t.Errorf("join_wait = %+v, want j1 all_must_succeed pending [n1] failed [b1]", w)
	}
	if lr.CurrentMemberID != "" {
		t.Errorf("current_member_id = %q, want empty for a nested first cursor", lr.CurrentMemberID)
	}

	// The draft row carries active_cursors as an empty array and no join_wait key.
	dr, ok := rows[d.ID]
	if !ok {
		t.Fatalf("status has no row for draft %s", d.ID)
	}
	if dr.Status != "draft" || dr.ActiveCursors == nil || len(dr.ActiveCursors) != 0 || dr.JoinWait != nil {
		t.Errorf("draft row = %+v, want status draft, active_cursors [], no join_wait", dr)
	}
	var raw struct {
		Automations []map[string]json.RawMessage `json:"automations"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw status: %v", err)
	}
	for _, row := range raw.Automations {
		var id string
		_ = json.Unmarshal(row["id"], &id)
		if id != d.ID {
			continue
		}
		if _, has := row["join_wait"]; has {
			t.Errorf("draft row carries join_wait key: %s", row["join_wait"])
		}
		if string(row["active_cursors"]) != "[]" {
			t.Errorf("draft row active_cursors = %s, want []", row["active_cursors"])
		}
	}
}
