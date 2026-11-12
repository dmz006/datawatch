package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

type mergedMemberBody struct {
	AutomationID  string                       `json:"automation_id"`
	Count         int                          `json:"count"`
	MergedMembers []autonomous.MergedMemberRow `json:"merged_members"`
}

// seedMergedMemberAutomation creates an Automation over a fresh PRD and writes
// one merged_member Decision into its log, the way the coordinator does.
func seedMergedMemberAutomation(t *testing.T, s *Server, mgr *autonomous.Manager, name string, outcome string, withDecision bool) string {
	t.Helper()
	prd, err := mgr.CreatePRD(name+" spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	created := doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": name, "actor": "operator", "members": []any{map[string]any{"prd_id": prd.ID}},
	}, http.StatusCreated)
	var a struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal([]byte(created), &a); err != nil || a.ID == "" {
		t.Fatalf("decode create: %v (%s)", err, created)
	}
	if withDecision {
		_, err = mgr.AutomationStore().Update(a.ID, a.Version, func(cur *autonomous.Automation) error {
			cur.Decisions = append(cur.Decisions, autonomous.Decision{
				At: time.Now(), Kind: "merged_member", Actor: "coordinator", Note: "member " + name + ": branch automaton/x is merged",
				MergedMember: &autonomous.MergedMemberDecision{
					Member: "m1", PRDID: prd.ID, Branch: "automaton/x", DefaultBranch: "trunk",
					Commit: "abc1234def", Policy: "manual", Outcome: outcome,
				},
			})
			return nil
		})
		if err != nil {
			t.Fatalf("seed decision: %v", err)
		}
	}
	return a.ID
}

func getMergedMembers(t *testing.T, s *Server, path string, want int) mergedMemberBody {
	t.Helper()
	body := doAutomation(t, s, http.MethodGet, path, nil, want)
	var out mergedMemberBody
	if want == http.StatusOK {
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("decode %s: %v (%s)", path, err, body)
		}
	}
	return out
}

func TestMergedMemberRoute(t *testing.T) {
	s, mgr, _ := newAutomationAuditServer(t)
	blockedID := seedMergedMemberAutomation(t, s, mgr, "blocked-one", "blocked", true)
	quietID := seedMergedMemberAutomation(t, s, mgr, "quiet-one", "", false)

	// one Automaton: the proof row, every field the surfaces show
	one := getMergedMembers(t, s, "/api/automations/"+blockedID+"/merged-members", http.StatusOK)
	if one.AutomationID != blockedID || one.Count != 1 || len(one.MergedMembers) != 1 {
		t.Fatalf("one = %+v", one)
	}
	r := one.MergedMembers[0]
	if r.AutomationID != blockedID || r.AutomationName != "blocked-one" || r.Member != "m1" ||
		r.Branch != "automaton/x" || r.DefaultBranch != "trunk" || r.ProvingCommit != "abc1234def" ||
		r.Policy != "manual" || r.Outcome != autonomous.MergedMemberBlocked || r.At.IsZero() {
		t.Fatalf("row = %+v", r)
	}

	// an Automaton with no such decision: an empty list, not null and not 404
	raw := doAutomation(t, s, http.MethodGet, "/api/automations/"+quietID+"/merged-members", nil, http.StatusOK)
	var generic map[string]any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		t.Fatal(err)
	}
	if list, ok := generic["merged_members"].([]any); !ok || len(list) != 0 {
		t.Fatalf("quiet Automaton body = %s, want an empty merged_members list", raw)
	}

	// all Automata: only the Automaton with a decision contributes
	all := getMergedMembers(t, s, "/api/automations/merged-members", http.StatusOK)
	if all.AutomationID != "" || all.Count != 1 || all.MergedMembers[0].AutomationID != blockedID {
		t.Fatalf("all = %+v", all)
	}

	// unknown Automaton
	getMergedMembers(t, s, "/api/automations/nope0000/merged-members", http.StatusNotFound)
}

func TestMergedMemberRoute_ReadOnlyAndRegistered(t *testing.T) {
	s, _, _ := newAutomationAuditServer(t)
	for _, path := range []string{"/api/automations/merged-members", "/api/automations/abcd1234/merged-members"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			rr := httptest.NewRecorder()
			s.handleAutomations(rr, httptest.NewRequest(method, path, nil))
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, path, rr.Code)
			}
		}
	}

	// the all-Automata path is its own mux entry, so it is not read as an id
	rr := httptest.NewRecorder()
	s.handleMergedMembers(rr, httptest.NewRequest(http.MethodGet, "/api/automations/merged-members", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("direct handler = %d %s", rr.Code, rr.Body.String())
	}

	// autonomous disabled
	off := &Server{}
	rr = httptest.NewRecorder()
	off.handleMergedMembers(rr, httptest.NewRequest(http.MethodGet, "/api/automations/merged-members", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled = %d, want 503", rr.Code)
	}
}
