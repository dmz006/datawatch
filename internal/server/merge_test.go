package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// mergeSpy is the double for the merge read routes: a fixed status and a
// fixed safe-to-delete report, with call counts so the tests can prove a
// refused request never reaches the manager.
type mergeSpy struct {
	fakeOrchAutonomous
	status      autonomous.MergeStatus
	report      autonomous.SafeDeleteReport
	reportErr   error
	statusCalls int
	reportCalls int
}

func (s *mergeSpy) MergeStatus() autonomous.MergeStatus {
	s.statusCalls++
	return s.status
}

func (s *mergeSpy) MergeSafeToDelete(context.Context) (autonomous.SafeDeleteReport, error) {
	s.reportCalls++
	return s.report, s.reportErr
}

func mergeRequest(t *testing.T, s *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	switch path {
	case "/api/autonomous/merge":
		s.handleAutonomousMerge(rec, req)
	case "/api/autonomous/safe-to-delete":
		s.handleAutonomousSafeToDelete(rec, req)
	default:
		t.Fatalf("unknown merge route %s", path)
	}
	return rec
}

func mergeKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not a JSON object: %v: %s", err, raw)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mergeWantKeys(t *testing.T, what string, raw json.RawMessage, want ...string) {
	t.Helper()
	sort.Strings(want)
	got := mergeKeys(t, raw)
	if len(got) != len(want) {
		t.Fatalf("%s keys = %v, want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s keys = %v, want %v", what, got, want)
		}
	}
}

func mergeFixtureStatus() autonomous.MergeStatus {
	at := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	alerted := at.Add(30 * time.Minute)
	yes := true
	return autonomous.MergeStatus{
		Mode:         "shadow",
		AlertMinutes: 30,
		Automata: []autonomous.MergeStatusEntry{
			{AutomatonID: "prd-a", Name: "Reports", Branch: "automaton/prd-a", State: autonomous.MergeStatePending,
				CommitsAhead: 3, PendingSince: &at, AlertedAt: &alerted, WouldAlert: &yes},
			{AutomatonID: "prd-b", Name: "Sync", Branch: "automaton/prd-b", State: autonomous.MergeStateMerged},
		},
		ShadowDecisions: []autonomous.MergeShadowDecision{
			{At: at, AutomatonID: "prd-a", Name: "Reports", Branch: "automaton/prd-a",
				State: autonomous.MergeStatePending, CommitsAhead: 3, WouldAlert: true, RepeatCount: 2, LastSeenAt: alerted},
		},
	}
}

func TestMergeRoute_StateListShape(t *testing.T) {
	spy := &mergeSpy{status: mergeFixtureStatus()}
	s := &Server{autonomousMgr: spy}
	rec := mergeRequest(t, s, http.MethodGet, "/api/autonomous/merge")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var body struct {
		Mode            string           `json:"mode"`
		AlertMinutes    int              `json:"alert_minutes"`
		Automata        []map[string]any `json:"automata"`
		ShadowDecisions []map[string]any `json:"shadow_decisions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if body.Mode != "shadow" || body.AlertMinutes != 30 {
		t.Fatalf("mode/alert_minutes = %q/%d, want shadow/30", body.Mode, body.AlertMinutes)
	}
	if len(body.Automata) != 2 || len(body.ShadowDecisions) != 1 {
		t.Fatalf("got %d Automata and %d shadow decisions, want 2 and 1", len(body.Automata), len(body.ShadowDecisions))
	}
	a := body.Automata[0]
	if a["automaton_id"] != "prd-a" || a["name"] != "Reports" || a["branch"] != "automaton/prd-a" ||
		a["state"] != "pending" || a["commits_ahead"] != float64(3) || a["would_alert"] != true {
		t.Fatalf("first Automaton entry wrong: %v", a)
	}
	if a["pending_since"] == nil || a["alerted_at"] == nil {
		t.Fatalf("pending Automaton lost its times: %v", a)
	}
	// a merged Automaton has null times and, here, no shadow flag
	b := body.Automata[1]
	if b["pending_since"] != nil || b["alerted_at"] != nil {
		t.Fatalf("merged Automaton must have null times: %v", b)
	}
	if _, has := b["would_alert"]; has {
		t.Fatalf("would_alert must be absent when the pass did not set it: %v", b)
	}
	if d := body.ShadowDecisions[0]; d["automaton_id"] != "prd-a" || d["would_alert"] != true || d["repeat_count"] != float64(2) {
		t.Fatalf("shadow decision wrong: %v", d)
	}
	if spy.statusCalls != 1 {
		t.Fatalf("status reads = %d, want 1", spy.statusCalls)
	}
}

func TestMergeRoute_SafeToDeleteShape(t *testing.T) {
	spy := &mergeSpy{report: autonomous.SafeDeleteReport{
		Safe:      []autonomous.SafeDeleteEntry{{Branch: "automaton/done", Worktree: "/wt/done", Reason: "all commits on main", Commits: 2}},
		Held:      []autonomous.SafeDeleteHeld{{Branch: "automaton/open", Worktree: "/wt/open", State: "pending", Dirty: true, Note: "Automaton worktree has uncommitted changes"}},
		HeldDirty: 1,
	}}
	s := &Server{autonomousMgr: spy}
	rec := mergeRequest(t, s, http.MethodGet, "/api/autonomous/safe-to-delete")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var body struct {
		Safe      []map[string]any `json:"safe"`
		Held      []map[string]any `json:"held"`
		HeldDirty int              `json:"held_dirty"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if len(body.Safe) != 1 || body.Safe[0]["branch"] != "automaton/done" || body.Safe[0]["commits"] != float64(2) ||
		body.Safe[0]["reason"] != "all commits on main" || body.Safe[0]["worktree"] != "/wt/done" {
		t.Fatalf("safe list wrong: %v", body.Safe)
	}
	if len(body.Held) != 1 || body.Held[0]["state"] != "pending" || body.Held[0]["dirty"] != true || body.HeldDirty != 1 {
		t.Fatalf("held list wrong: %v dirty=%d", body.Held, body.HeldDirty)
	}

	// no base repo configured: 503, not a 500
	spy.reportErr = autonomous.ErrMergeNoBaseRepo
	if rec := mergeRequest(t, s, http.MethodGet, "/api/autonomous/safe-to-delete"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no base repo: status = %d, want 503", rec.Code)
	}
	spy.reportErr = errors.New("automaton safe-delete: list branches failed")
	if rec := mergeRequest(t, s, http.MethodGet, "/api/autonomous/safe-to-delete"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("git failure: status = %d, want 500", rec.Code)
	}
}

func TestMergeRoute_ModeOffReturnsEmptyList(t *testing.T) {
	spy := &mergeSpy{status: autonomous.MergeStatus{
		Mode:            "off",
		AlertMinutes:    30,
		Automata:        []autonomous.MergeStatusEntry{},
		ShadowDecisions: []autonomous.MergeShadowDecision{},
	}}
	s := &Server{autonomousMgr: spy}
	rec := mergeRequest(t, s, http.MethodGet, "/api/autonomous/merge")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	// the lists must be empty arrays, never null, so clients can iterate them
	for _, k := range []string{"automata", "shadow_decisions"} {
		if string(raw[k]) != "[]" {
			t.Fatalf("%s = %s, want []", k, raw[k])
		}
	}
	if string(raw["mode"]) != `"off"` {
		t.Fatalf("mode = %s, want \"off\"", raw["mode"])
	}
}

func TestMergeRoute_WriteMethodsRejected(t *testing.T) {
	for _, path := range []string{"/api/autonomous/merge", "/api/autonomous/safe-to-delete"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			spy := &mergeSpy{}
			s := &Server{autonomousMgr: spy}
			rec := mergeRequest(t, s, method, path)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s: status = %d, want 405", method, path, rec.Code)
			}
			if got := rec.Header().Get("Allow"); got != http.MethodGet {
				t.Fatalf("%s %s: Allow = %q, want GET", method, path, got)
			}
			if spy.statusCalls != 0 || spy.reportCalls != 0 {
				t.Fatalf("%s %s reached the manager", method, path)
			}
		}
	}
}

func TestMergeRoute_JSONKeyNames(t *testing.T) {
	spy := &mergeSpy{status: mergeFixtureStatus(), report: autonomous.SafeDeleteReport{
		Safe: []autonomous.SafeDeleteEntry{{Branch: "automaton/done"}},
		Held: []autonomous.SafeDeleteHeld{{Branch: "automaton/open", State: "pending", Dirty: true, Note: "n"}},
	}}
	s := &Server{autonomousMgr: spy}

	var status map[string]json.RawMessage
	rec := mergeRequest(t, s, http.MethodGet, "/api/autonomous/merge")
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	mergeWantKeys(t, "merge body", rec.Body.Bytes(), "mode", "alert_minutes", "automata", "shadow_decisions")

	var automata []json.RawMessage
	if err := json.Unmarshal(status["automata"], &automata); err != nil || len(automata) != 2 {
		t.Fatalf("automata: %v %s", err, status["automata"])
	}
	mergeWantKeys(t, "shadow Automaton entry", automata[0],
		"automaton_id", "name", "branch", "state", "commits_ahead", "pending_since", "alerted_at", "would_alert")
	// without the shadow flag the other keys stay, with null times
	mergeWantKeys(t, "Automaton entry", automata[1],
		"automaton_id", "name", "branch", "state", "commits_ahead", "pending_since", "alerted_at")

	var decisions []json.RawMessage
	if err := json.Unmarshal(status["shadow_decisions"], &decisions); err != nil || len(decisions) != 1 {
		t.Fatalf("shadow_decisions: %v %s", err, status["shadow_decisions"])
	}
	mergeWantKeys(t, "shadow decision", decisions[0],
		"at", "automaton_id", "name", "branch", "state", "commits_ahead", "would_alert", "repeat_count", "last_seen_at")

	rec = mergeRequest(t, s, http.MethodGet, "/api/autonomous/safe-to-delete")
	var report map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	mergeWantKeys(t, "safe-to-delete body", rec.Body.Bytes(), "safe", "held", "held_dirty")
	var safe, held []json.RawMessage
	_ = json.Unmarshal(report["safe"], &safe)
	_ = json.Unmarshal(report["held"], &held)
	if len(safe) != 1 || len(held) != 1 {
		t.Fatalf("safe/held = %d/%d, want 1/1", len(safe), len(held))
	}
	mergeWantKeys(t, "safe entry", safe[0], "branch", "worktree", "reason", "commits")
	mergeWantKeys(t, "held entry", held[0], "branch", "worktree", "state", "dirty", "note")
}

func TestMergeRoute_Unavailable(t *testing.T) {
	// autonomous disabled
	if rec := mergeRequest(t, &Server{}, http.MethodGet, "/api/autonomous/merge"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled: status = %d, want 503", rec.Code)
	}
	// a manager without the merge reads
	s := &Server{autonomousMgr: &fakeOrchAutonomous{}}
	if rec := mergeRequest(t, s, http.MethodGet, "/api/autonomous/safe-to-delete"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no merge reads: status = %d, want 503", rec.Code)
	}
}
