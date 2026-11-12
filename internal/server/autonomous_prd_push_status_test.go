package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newPRDGitStateServer builds a real autonomous manager over a temp store
// with one PRD whose GitWorkflow is set to the given state (exactly what
// recordPushOutcome writes on success/failure) and returns the server plus
// the PRD's ID.
func newPRDGitStateServer(t *testing.T, git autonomous.GitWorkflow) (*Server, string) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd, ok := mgr.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatalf("GetPRD: not found")
	}
	prd.Git = git
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return &Server{autonomousMgr: autonomous.NewAPI(mgr)}, prd.ID
}

func TestPRDGetPushStatus(t *testing.T) {
	t.Run("pushed", func(t *testing.T) {
		s, id := newPRDGitStateServer(t, autonomous.GitWorkflow{
			Branch:    "automaton/abcd1234",
			PushState: autonomous.PushStatePushed,
			PushedRef: "refs/heads/automaton/abcd1234",
			PushedSHA: "0123456789abcdef0123456789abcdef01234567",
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id, nil)
		s.handleAutonomousPRDs(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode reply: %v", err)
		}
		git, _ := got["git"].(map[string]any)
		if git == nil {
			t.Fatalf("no git object in reply: %v", got)
		}
		if git["push_state"] != "pushed" {
			t.Errorf("git.push_state = %v, want pushed", git["push_state"])
		}
		if git["pushed_ref"] != "refs/heads/automaton/abcd1234" {
			t.Errorf("git.pushed_ref = %v", git["pushed_ref"])
		}
		if git["pushed_sha"] != "0123456789abcdef0123456789abcdef01234567" {
			t.Errorf("git.pushed_sha = %v", git["pushed_sha"])
		}
		if _, ok := git["push_warning"]; ok {
			t.Errorf("pushed PRD should not carry push_warning: %v", git)
		}
	})

	t.Run("not_pushed with warning", func(t *testing.T) {
		s, id := newPRDGitStateServer(t, autonomous.GitWorkflow{
			Branch:      "automaton/abcd1234",
			PushState:   autonomous.PushStateNotPushed,
			PushWarning: "push refused: branch is on the protected list",
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id, nil)
		s.handleAutonomousPRDs(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode reply: %v", err)
		}
		git, _ := got["git"].(map[string]any)
		if git == nil {
			t.Fatalf("no git object in reply: %v", got)
		}
		if git["push_state"] != "not_pushed" {
			t.Errorf("git.push_state = %v, want not_pushed", git["push_state"])
		}
		if git["push_warning"] != "push refused: branch is on the protected list" {
			t.Errorf("git.push_warning = %v", git["push_warning"])
		}
		if _, ok := git["pushed_sha"]; ok {
			t.Errorf("not_pushed PRD should not carry pushed_sha: %v", git)
		}
	})
}

func TestPRDListPushStatus(t *testing.T) {
	s, id := newPRDGitStateServer(t, autonomous.GitWorkflow{
		PushState:   autonomous.PushStateNotPushed,
		PushWarning: "no origin remote",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds", nil)
	s.handleAutonomousPRDs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var got struct {
		PRDs []map[string]any `json:"prds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	var found map[string]any
	for _, p := range got.PRDs {
		if p["id"] == id {
			found = p
			break
		}
	}
	if found == nil {
		t.Fatalf("prd %s not in list payload: %s", id, rec.Body.String())
	}
	// The list returns the trimmed summary view, which carries the push
	// outcome at the top level of each item (the full read nests it under git).
	if found["push_state"] != "not_pushed" {
		t.Errorf("list push_state = %v, want not_pushed", found["push_state"])
	}
	if found["push_warning"] != "no origin remote" {
		t.Errorf("list push_warning = %v", found["push_warning"])
	}
}

// TestPRDDetailSummaryPushStatus pins the trimmed summary detail (the MCP
// default and the compact web read) on carrying push_state + push_warning.
func TestPRDDetailSummaryPushStatus(t *testing.T) {
	s, id := newPRDGitStateServer(t, autonomous.GitWorkflow{
		Branch:      "automaton/abcd1234",
		PushState:   autonomous.PushStateNotPushed,
		PushWarning: "push refused by daemon policy",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id+"?detail=summary", nil)
	s.handleAutonomousPRDs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if got["push_state"] != "not_pushed" {
		t.Errorf("summary push_state = %v, want not_pushed (body %q)", got["push_state"], rec.Body.String())
	}
	if got["push_warning"] != "push refused by daemon policy" {
		t.Errorf("summary push_warning = %v", got["push_warning"])
	}
	if got["git"] != nil {
		t.Errorf("summary view should not carry the full git object: %v", got["git"])
	}
}
