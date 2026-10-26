// REST-level tests for /api/automations (docs/plans/2026-10-14-automation-orchestration-design.md —
// "Data model", "Validation", "Concurrency between Automations"): they drive
// the real autonomous manager through the Server handler with httptest, so
// the JSON wire shape, the status mapping (201/200/204/400/404/405/409) and
// persistence across a daemon restart are all exercised end to end.

package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// newAutomationRESTServer wires a real autonomous manager (its data under
// dataDir, per the usual <data_dir>/autonomous/ layout) into a Server, the
// same adapter main.go uses. Building a second one on the same dataDir is
// the daemon restart.
func newAutomationRESTServer(t *testing.T, dataDir string) (*Server, *autonomous.Manager) {
	t.Helper()
	mgr, err := autonomous.NewManager(dataDir, autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return &Server{cfg: config.DefaultConfig(), autonomousMgr: newTestAPI(mgr)}, mgr
}

// makeMemberPRD creates one PRD in the manager usable as an Automation
// member (the member's PRD must exist and be unowned — see "Validation").
func makeMemberPRD(t *testing.T, mgr *autonomous.Manager) string {
	t.Helper()
	prd, err := mgr.CreatePRD("automation member", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	return prd.ID
}

func memberWire(prdID string) []any {
	return []any{map[string]any{"prd_id": prdID}}
}

func TestAutomationRESTCRUDRoundTrip(t *testing.T) {
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	members := memberWire(makeMemberPRD(t, mgr))

	// POST -> 201, and the created record is version 1 in draft state.
	created := doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "nightly sweep", "actor": "operator", "reason": "round trip", "members": members,
	}, http.StatusCreated)
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

	// GET (list) contains the new Automation.
	listBody := doAutomation(t, s, http.MethodGet, "/api/automations", nil, http.StatusOK)
	var list []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(listBody), &list); err != nil {
		t.Fatalf("decode list: %v (body=%q)", err, listBody)
	}
	inList := false
	for _, x := range list {
		if x.ID == a.ID {
			inList = true
		}
	}
	if !inList {
		t.Fatalf("list did not contain %s: %v", a.ID, list)
	}

	// GET by id returns the same record.
	got := doAutomation(t, s, http.MethodGet, "/api/automations/"+a.ID, nil, http.StatusOK)
	var one struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(got), &one); err != nil || one.Name != "nightly sweep" {
		t.Fatalf("get by id: name=%q err=%v (body=%q)", one.Name, err, got)
	}

	// PUT with the version we just saw -> 200 and version 2.
	updated := doAutomation(t, s, http.MethodPut, "/api/automations/"+a.ID, map[string]any{
		"name": "nightly sweep v2", "actor": "operator", "reason": "rename",
		"expected_version": 1, "members": members,
	}, http.StatusOK)
	var up struct {
		Name    string `json:"name"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal([]byte(updated), &up); err != nil || up.Name != "nightly sweep v2" || up.Version != 2 {
		t.Fatalf("update: name=%q version=%d err=%v, want v2 / 2 (body=%q)", up.Name, up.Version, err, updated)
	}

	// DELETE -> 204, and the record is then gone.
	doAutomation(t, s, http.MethodDelete, "/api/automations/"+a.ID, nil, http.StatusNoContent)
	doAutomation(t, s, http.MethodGet, "/api/automations/"+a.ID, nil, http.StatusNotFound)
}

func TestAutomationRESTStatusMapping(t *testing.T) {
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	prd := makeMemberPRD(t, mgr)
	created := doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "owner", "actor": "operator", "reason": "status mapping", "members": memberWire(prd),
	}, http.StatusCreated)
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(created), &a); err != nil || a.ID == "" {
		t.Fatalf("decode create response: %v (body=%q)", err, created)
	}

	t.Run("stale expected version is 409", func(t *testing.T) {
		doAutomation(t, s, http.MethodPut, "/api/automations/"+a.ID, map[string]any{
			"name": "stale", "actor": "operator", "reason": "race",
			"expected_version": 99, "members": memberWire(prd),
		}, http.StatusConflict)
	})

	t.Run("member already owned by another automation is 400", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
			"name": "contested", "actor": "operator", "reason": "double enroll", "members": memberWire(prd),
		}, http.StatusBadRequest)
	})

	// Two PRDs, two members, and an OnFailed edge back to the earlier
	// member: a cycle the validator must reject at create time.
	t.Run("cycle is 400", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
			"name": "looping", "actor": "operator", "reason": "cycle",
			"members": []any{
				map[string]any{"id": "step-1", "prd_id": makeMemberPRD(t, mgr), "on_complete": "step-2"},
				map[string]any{"id": "step-2", "prd_id": makeMemberPRD(t, mgr), "on_failed": "step-1"},
			},
		}, http.StatusBadRequest)
	})

	t.Run("get unknown id is 404", func(t *testing.T) {
		doAutomation(t, s, http.MethodGet, "/api/automations/no-such-id", nil, http.StatusNotFound)
	})

	t.Run("delete unknown id is 404", func(t *testing.T) {
		doAutomation(t, s, http.MethodDelete, "/api/automations/no-such-id", nil, http.StatusNotFound)
	})

	t.Run("unsupported method is 405", func(t *testing.T) {
		doAutomation(t, s, http.MethodPatch, "/api/automations", nil, http.StatusMethodNotAllowed)
	})
}

func TestAutomationRESTSurvivesRestart(t *testing.T) {
	dataDir := t.TempDir()
	s, mgr := newAutomationRESTServer(t, dataDir)
	created := doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "persistent", "actor": "operator", "reason": "restart", "members": memberWire(makeMemberPRD(t, mgr)),
	}, http.StatusCreated)
	var a struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal([]byte(created), &a); err != nil || a.ID == "" {
		t.Fatalf("decode create response: %v (body=%q)", err, created)
	}

	// A second manager and server over the same data directory is the
	// restart: the Automation must still be retrievable, unchanged.
	s2, _ := newAutomationRESTServer(t, dataDir)
	got := doAutomation(t, s2, http.MethodGet, "/api/automations/"+a.ID, nil, http.StatusOK)
	var one struct {
		Name    string `json:"name"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal([]byte(got), &one); err != nil || one.Name != "persistent" || one.Version != a.Version {
		t.Fatalf("get after restart: name=%q version=%d err=%v (body=%q)", one.Name, one.Version, err, got)
	}
}
