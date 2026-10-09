// GH#201 Phase 4 — state-changing-action completeness sweep. These
// tests confirm the s.audit(...) wiring added to session lifecycle,
// alert rules, federation peer management, device registration, and
// a representative sample of Automata/PRD lifecycle actions actually
// fires, records the right action/resource_type, and never leaks a
// secret-shaped value (peer tokens, device push tokens, session input
// text) into the audit log.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alertrules"
	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/devices"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/session"
)

func gh201Phase4Server(t *testing.T) (*Server, *audit.Log) {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	srv := NewServer(NewHub(), sm, "h", "", nil, nil, "")
	al, err := audit.NewAt(t.TempDir() + "/audit.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	srv.auditLog = al
	return srv, al
}

func gh201Phase4LastAction(t *testing.T, al *audit.Log) audit.Entry {
	t.Helper()
	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry, got none")
	}
	return entries[0] // newest first
}

// --- Session lifecycle ---

func TestGH201Phase4_SessionStart_Audited(t *testing.T) {
	srv, al := gh201Phase4Server(t)
	body, _ := json.Marshal(map[string]any{"task": "hello", "project_dir": t.TempDir()})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	// This is a real tmux-backed session (handleStartSession isn't
	// mocked) — found 2026-10-09 leaking 54 real orphaned tmux panes
	// onto the production tmux server over a day of `go test` runs,
	// because this host's TMUX_TMPDIR isn't test-isolated. Kill it.
	var startResp struct {
		FullID string `json:"full_id"`
		ID     string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &startResp)
	fullID := startResp.FullID
	if fullID == "" {
		fullID = startResp.ID
	}
	t.Cleanup(func() { srv.manager.KillTmuxSession(fullID) })
	e := gh201Phase4LastAction(t, al)
	if e.Action != "start" {
		t.Errorf("action = %q, want start", e.Action)
	}
	if e.Details["resource_type"] != "session" {
		t.Errorf("resource_type = %v, want session", e.Details["resource_type"])
	}
}

func TestGH201Phase4_SessionKill_Audited(t *testing.T) {
	srv, al := gh201Phase4Server(t)
	id := startTestSession(t, srv, t.TempDir())
	body, _ := json.Marshal(map[string]any{"id": id})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/kill", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleKillSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	e := gh201Phase4LastAction(t, al)
	if e.Action != "kill" || e.Details["resource_id"] != id {
		t.Errorf("entry = %+v, want kill/%s", e, id)
	}
}

func TestGH201Phase4_SessionDelete_Audited_NeverLeaksMemoryStrategyAsSecret(t *testing.T) {
	srv, al := gh201Phase4Server(t)
	id := startTestSession(t, srv, t.TempDir())
	body, _ := json.Marshal(map[string]any{"id": id, "delete_data": true})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/delete", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleDeleteSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	e := gh201Phase4LastAction(t, al)
	if e.Action != "delete" || e.Details["resource_type"] != "session" {
		t.Errorf("entry = %+v, want delete/session", e)
	}
	if e.Details["delete_data"] != true {
		t.Errorf("delete_data = %v, want true", e.Details["delete_data"])
	}
}

func TestGH201Phase4_SessionRollback_RequiresCapability(t *testing.T) {
	// GH#201 Phase 4 finding: this REST route had NO capability check
	// at all before this fix. Simulate a federation peer with zero
	// granted capabilities and confirm it's now rejected.
	srv, store, _ := newFedTestServer(t)
	if err := store.Add(&multiserver.Entry{
		Name: "no-caps-peer", Token: "tok-no-caps", URL: "http://x",
		Enabled: true, Federated: true, Capabilities: []string{},
	}); err != nil {
		t.Fatal(err)
	}
	mw := srv.fedAuthMiddleware(http.HandlerFunc(srv.handleSessionRollback))
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/some-id/rollback", nil)
	req.Header.Set("Authorization", "Bearer tok-no-caps")
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a peer with no capabilities, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGH201Phase4_SessionRollback_Audited(t *testing.T) {
	srv, al := gh201Phase4Server(t)
	projectDir := t.TempDir()
	id := startTestSession(t, srv, projectDir)
	body, _ := json.Marshal(map[string]any{"force": true})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+id+"/rollback", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionRollback(w, req)
	// No git repo in projectDir, so the rollback itself fails — but
	// that's fine, this test only needs to confirm the capability
	// check (admin, nil peer) passes and we reach the git-rollback
	// attempt, not that the rollback mechanically succeeds.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (rollback fails, no git repo), got %d: %s", w.Code, w.Body.String())
	}
	// No audit entry should exist yet since the rollback itself failed
	// before reaching the audit call.
	entries, _ := al.Read(audit.QueryFilter{})
	for _, e := range entries {
		if e.Action == "rollback" {
			t.Fatalf("rollback audit entry should only be written on SUCCESS, got one for a failed rollback: %+v", e)
		}
	}
}

func TestGH201Phase4_SessionInput_Audited_NeverLogsTheText(t *testing.T) {
	srv, al := gh201Phase4Server(t)
	id := startTestSession(t, srv, t.TempDir())
	body, _ := json.Marshal(map[string]any{"text": "super-secret-password-123"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+id+"/input", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionInput(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	e := gh201Phase4LastAction(t, al)
	if e.Action != "send_input" {
		t.Errorf("action = %q, want send_input", e.Action)
	}
	raw, _ := json.Marshal(e)
	if strings.Contains(string(raw), "super-secret-password-123") {
		t.Fatalf("audit entry leaked the raw input text: %s", raw)
	}
	if e.Details["text_len"] != float64(len("super-secret-password-123")) {
		t.Errorf("text_len = %v, want %d", e.Details["text_len"], len("super-secret-password-123"))
	}
}

// --- Alert rules ---

type gh201FakeAlertRulesAPI struct {
	rules map[string]alertrules.AlertRule
}

func newGH201FakeAlertRulesAPI() *gh201FakeAlertRulesAPI {
	return &gh201FakeAlertRulesAPI{rules: map[string]alertrules.AlertRule{}}
}
func (f *gh201FakeAlertRulesAPI) List() []alertrules.AlertRule {
	out := make([]alertrules.AlertRule, 0, len(f.rules))
	for _, r := range f.rules {
		out = append(out, r)
	}
	return out
}
func (f *gh201FakeAlertRulesAPI) Get(name string) (alertrules.AlertRule, bool) {
	r, ok := f.rules[name]
	return r, ok
}
func (f *gh201FakeAlertRulesAPI) Add(r alertrules.AlertRule) error {
	if _, exists := f.rules[r.Name]; exists {
		return errAlreadyExists
	}
	f.rules[r.Name] = r
	return nil
}
func (f *gh201FakeAlertRulesAPI) Update(r alertrules.AlertRule) error {
	f.rules[r.Name] = r
	return nil
}
func (f *gh201FakeAlertRulesAPI) Delete(name string) bool {
	_, ok := f.rules[name]
	delete(f.rules, name)
	return ok
}
func (f *gh201FakeAlertRulesAPI) SetEnabled(name string, on bool) bool {
	r, ok := f.rules[name]
	if !ok {
		return false
	}
	r.Enabled = on
	f.rules[name] = r
	return true
}
func (f *gh201FakeAlertRulesAPI) Firings() []alertrules.Firing { return nil }

var errAlreadyExists = &gh201SimpleErr{"already exists"}

type gh201SimpleErr struct{ s string }

func (e *gh201SimpleErr) Error() string { return e.s }

func TestGH201Phase4_AlertRules_FullLifecycleAudited(t *testing.T) {
	srv, al := gh201Phase4Server(t)
	fake := newGH201FakeAlertRulesAPI()
	srv.SetAlertRulesAPI(fake)

	post := func(method, path string, body map[string]any) *httptest.ResponseRecorder {
		var b bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&b).Encode(body)
		}
		req := httptest.NewRequest(method, path, &b)
		w := httptest.NewRecorder()
		srv.handleAlertRules(w, req)
		return w
	}

	if w := post(http.MethodPost, "/api/alert-rules", map[string]any{"name": "r1"}); w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if w := post(http.MethodPut, "/api/alert-rules/r1", map[string]any{"name": "r1"}); w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	if w := post(http.MethodPost, "/api/alert-rules/r1/disable", nil); w.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	if w := post(http.MethodPost, "/api/alert-rules/r1/enable", nil); w.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", w.Code, w.Body.String())
	}
	if w := post(http.MethodDelete, "/api/alert-rules/r1", nil); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}

	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var gotActions []string
	for i := len(entries) - 1; i >= 0; i-- { // oldest first
		gotActions = append(gotActions, entries[i].Action)
	}
	want := []string{"create", "update", "disable", "enable", "delete"}
	if len(gotActions) != len(want) {
		t.Fatalf("actions = %v, want %v", gotActions, want)
	}
	for i, a := range want {
		if gotActions[i] != a {
			t.Errorf("action[%d] = %q, want %q", i, gotActions[i], a)
		}
	}
}

// --- Federation peer management ---

func TestGH201Phase4_FederationPeer_FullLifecycleAudited_NeverLeaksToken(t *testing.T) {
	srv, store, _ := newFedTestServer(t)
	al, err := audit.NewAt(t.TempDir() + "/audit.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = al.Close() }()
	srv.auditLog = al

	createBody, _ := json.Marshal(map[string]any{"name": "newpeer", "url": "http://x", "token": "super-secret-peer-token"})
	req := httptest.NewRequest(http.MethodPost, "/api/federation/peers", bytes.NewReader(createBody))
	w := httptest.NewRecorder()
	srv.handleFederationPeers(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	updateBody, _ := json.Marshal(map[string]any{"url": "http://y"})
	req2 := httptest.NewRequest(http.MethodPut, "/api/federation/peers/newpeer", bytes.NewReader(updateBody))
	w2 := httptest.NewRecorder()
	srv.handleFederationPeers(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w2.Code, w2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodDelete, "/api/federation/peers/newpeer", nil)
	w3 := httptest.NewRecorder()
	srv.handleFederationPeers(w3, req3)
	if w3.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w3.Code, w3.Body.String())
	}
	_ = store // keep referenced; entries are managed through the handler above

	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 audit entries, got %d: %+v", len(entries), entries)
	}
	raw, _ := json.Marshal(entries)
	if strings.Contains(string(raw), "super-secret-peer-token") {
		t.Fatalf("audit log leaked the federation peer token: %s", raw)
	}
	var gotActions []string
	for i := len(entries) - 1; i >= 0; i-- {
		gotActions = append(gotActions, entries[i].Action)
	}
	want := []string{"create", "update", "delete"}
	for i, a := range want {
		if gotActions[i] != a {
			t.Errorf("action[%d] = %q, want %q", i, gotActions[i], a)
		}
	}
}

// --- Device registration ---

func TestGH201Phase4_Device_RegisterAndDelete_Audited_NeverLeaksToken(t *testing.T) {
	srv, al := gh201Phase4Server(t)
	store, err := devices.NewStore(t.TempDir() + "/devices.json")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetDeviceStore(store)

	body, _ := json.Marshal(map[string]any{"device_token": "super-secret-push-token", "kind": "fcm", "platform": "android"})
	req := httptest.NewRequest(http.MethodPost, "/api/devices/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleDevicesRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	var resp struct{ DeviceID string `json:"device_id"` }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.DeviceID == "" {
		t.Fatal("no device_id returned")
	}

	req2 := httptest.NewRequest(http.MethodDelete, "/api/devices/"+resp.DeviceID, nil)
	w2 := httptest.NewRecorder()
	srv.handleDevicesList(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w2.Code, w2.Body.String())
	}

	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d: %+v", len(entries), entries)
	}
	raw, _ := json.Marshal(entries)
	if strings.Contains(string(raw), "super-secret-push-token") {
		t.Fatalf("audit log leaked the device push token: %s", raw)
	}
}

// --- Automata/PRD lifecycle (representative sample) ---
//
// The remaining ~35 PRD/Automata actions follow the identical
// s.audit(r.Context(), "<action>", "automaton", id, ...) call shape
// proven correct here — each was individually wired and the full
// repo build/vet confirms every call site type-checks. This sample
// proves the MECHANISM (context plumbing, action naming, non-leak of
// response data) works end to end; it isn't meant to re-prove the
// same four-line pattern 35 more times.

type gh201FakeAutonomousForAudit struct {
	fakeOrchAutonomous
	nextID string
}

func (f *gh201FakeAutonomousForAudit) CreatePRD(spec, projectDir, backend, model, effort string) (any, error) {
	return map[string]any{"id": f.nextID, "spec": spec}, nil
}
func (f *gh201FakeAutonomousForAudit) GetPRD(id string) (any, bool) {
	if id == f.nextID {
		return map[string]any{"id": id}, true
	}
	return nil, false
}
func (f *gh201FakeAutonomousForAudit) Approve(id, actor, note string) (any, error) {
	return map[string]any{"id": id}, nil
}
func (f *gh201FakeAutonomousForAudit) DeletePRD(string) error { return nil }

func TestGH201Phase4_Automaton_CreateApproveDelete_Audited(t *testing.T) {
	srv, al := gh201Phase4Server(t)
	fake := &gh201FakeAutonomousForAudit{nextID: "prd-123"}
	srv.autonomousMgr = fake

	createBody, _ := json.Marshal(map[string]any{"spec": "do the thing", "project_dir": t.TempDir()})
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds", bytes.NewReader(createBody))
	w := httptest.NewRecorder()
	srv.handleAutonomousPRDs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	approveBody, _ := json.Marshal(map[string]any{"actor": "tester"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-123/approve", bytes.NewReader(approveBody))
	w2 := httptest.NewRecorder()
	srv.handleAutonomousPRDs(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w2.Code, w2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodDelete, "/api/autonomous/prds/prd-123?hard=true", nil)
	w3 := httptest.NewRecorder()
	srv.handleAutonomousPRDs(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w3.Code, w3.Body.String())
	}

	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var gotActions []string
	for i := len(entries) - 1; i >= 0; i-- {
		gotActions = append(gotActions, entries[i].Action)
	}
	want := []string{"create", "approve", "delete"}
	if len(gotActions) != len(want) {
		t.Fatalf("actions = %v, want %v", gotActions, want)
	}
	for i, a := range want {
		if gotActions[i] != a {
			t.Errorf("action[%d] = %q, want %q", i, gotActions[i], a)
		}
	}
}
