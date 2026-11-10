// The in_force array on the GET /api/config worker profile read: one entry per
// worker role and task kind, resolved from the live daemon values with the same
// rules a real launch uses. Each entry names its scope, key, applied profile,
// tier and reason.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/workerprofile"
)

type inForceRow struct {
	Scope   string `json:"scope"`
	Key     string `json:"key"`
	Profile string `json:"profile"`
	Tier    string `json:"tier"`
	Reason  string `json:"reason"`
}

// getInForce returns the in_force rows from GET /api/config for the server's
// current config and fails the test when the block is missing.
func getInForce(t *testing.T, s *Server) []inForceRow {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous map[string]json.RawMessage `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	raw, ok := got.Autonomous["in_force"]
	if !ok {
		t.Fatalf("GET /api/config autonomous block lacks in_force")
	}
	var rows []inForceRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("in_force must be an array, got %s: %v", raw, err)
	}
	return rows
}

// findInForce returns the row for scope/key, failing the test when absent.
func findInForce(t *testing.T, rows []inForceRow, scope, key string) inForceRow {
	t.Helper()
	for _, r := range rows {
		if r.Scope == scope && r.Key == key {
			return r
		}
	}
	t.Fatalf("in_force has no %s %q entry: %+v", scope, key, rows)
	return inForceRow{}
}

func TestWorkerProfileInForce_BlockPresent(t *testing.T) {
	s := newWorkerProfileServer(t)
	rows := getInForce(t, s)
	if rows == nil {
		t.Fatalf("in_force must be an array, not null")
	}
	// The three existing worker profile keys stay alongside it.
	profilesRaw, defaultsRaw, nameRaw := getWorkerProfileView(t, s)
	if len(profilesRaw) == 0 || len(defaultsRaw) == 0 || len(nameRaw) == 0 {
		t.Fatalf("existing worker profile keys must remain present")
	}
}

func TestWorkerProfileInForce_ListsEveryRoleAndKind(t *testing.T) {
	s := newWorkerProfileServer(t)
	rows := getInForce(t, s)
	wantScope := []struct{ scope, key string }{
		{workerprofile.ScopeRole, "worker"},
		{workerprofile.ScopeRole, "verifier"},
		{workerprofile.ScopeRole, "judge"},
		{workerprofile.ScopeKind, "implementation"},
		{workerprofile.ScopeKind, "docs"},
		{workerprofile.ScopeKind, "note"},
		{workerprofile.ScopeKind, "final_gate"},
	}
	if len(rows) != len(wantScope) {
		t.Fatalf("want %d in_force entries, got %d: %+v", len(wantScope), len(rows), rows)
	}
	for i, w := range wantScope {
		if rows[i].Scope != w.scope || rows[i].Key != w.key {
			t.Errorf("entry %d: want %s %q, got %s %q", i, w.scope, w.key, rows[i].Scope, rows[i].Key)
		}
		if rows[i].Tier == "" {
			t.Errorf("entry %d (%s %q) has an empty tier", i, rows[i].Scope, rows[i].Key)
		}
	}
}

func TestWorkerProfileInForce_DefaultsReadAsBuiltIn(t *testing.T) {
	s := newWorkerProfileServer(t)
	rows := getInForce(t, s)
	// With nothing configured, every entry comes from the built-in tables and
	// no daemon-level profile is reported.
	for _, r := range rows {
		if r.Tier == workerprofile.TierDaemon {
			t.Errorf("%s %q: unconfigured daemon must not report tier daemon", r.Scope, r.Key)
		}
	}
	if w := findInForce(t, rows, workerprofile.ScopeRole, "worker"); w.Tier != workerprofile.TierBuiltIn && w.Tier != workerprofile.TierNone {
		t.Errorf("worker role default tier = %q, want built-in or none", w.Tier)
	}
}

func TestWorkerProfileInForce_RoleOverrideReadsAsRole(t *testing.T) {
	s := newWorkerProfileServer(t)
	s.cfg.Autonomous.WorkerProfileDefaults.Role = map[string]string{"verifier": "minimal"}
	rows := getInForce(t, s)
	v := findInForce(t, rows, workerprofile.ScopeRole, "verifier")
	if v.Profile != "minimal" || v.Tier != "role" {
		t.Errorf("verifier with role override: want profile minimal tier role, got %+v", v)
	}
	if v.Reason == "" {
		t.Errorf("verifier entry must carry a reason")
	}
}

func TestWorkerProfileInForce_KindOverrideReadsAsKind(t *testing.T) {
	s := newWorkerProfileServer(t)
	s.cfg.Autonomous.WorkerProfileDefaults.Kind = map[string]string{"docs": "minimal"}
	rows := getInForce(t, s)
	d := findInForce(t, rows, workerprofile.ScopeKind, "docs")
	if d.Profile != "minimal" || d.Tier != "kind" {
		t.Errorf("docs with kind override: want profile minimal tier kind, got %+v", d)
	}
}

func TestWorkerProfileInForce_GlobalProfileReadsAsDaemon(t *testing.T) {
	s := newWorkerProfileServer(t)
	s.cfg.Autonomous.WorkerProfiles = map[string]workerprofile.WorkerProfile{
		"triage": {Name: "triage", Context: "digest"},
	}
	s.cfg.Autonomous.WorkerProfile = "triage"
	rows := getInForce(t, s)
	w := findInForce(t, rows, workerprofile.ScopeRole, "worker")
	if w.Profile != "triage" || w.Tier != workerprofile.TierDaemon {
		t.Errorf("worker with global profile: want profile triage tier daemon, got %+v", w)
	}
}

func TestWorkerProfileInForce_UnknownGlobalFallsBackToRole(t *testing.T) {
	s := newWorkerProfileServer(t)
	s.cfg.Autonomous.WorkerProfile = "no-such-profile"
	rows := getInForce(t, s)
	for _, r := range rows {
		if r.Tier == workerprofile.TierDaemon {
			t.Errorf("%s %q: an unresolved global name must not read as tier daemon", r.Scope, r.Key)
		}
	}
}
