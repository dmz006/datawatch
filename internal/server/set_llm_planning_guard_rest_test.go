// REST mapping of the planning guard on the Automation, story and task LLM
// setters: a call made while an Automaton is planning is a 409 (retry later),
// not a 400, and changes nothing.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestAutomationRESTSetLLM_RefusedWhilePlanning(t *testing.T) {
	s, mgr, _ := newAutomationAuditServer(t)
	id := createDraftAutomation(t, s, mgr)
	var a struct {
		Version int64 `json:"version"`
		Members []struct {
			PRDID string `json:"prd_id"`
		} `json:"members"`
	}
	if err := json.Unmarshal([]byte(doAutomation(t, s, http.MethodGet, "/api/automations/"+id, nil, http.StatusOK)), &a); err != nil || len(a.Members) == 0 {
		t.Fatalf("decode automation: %v %+v", err, a)
	}
	prd, ok := mgr.Store().GetPRD(a.Members[0].PRDID)
	if !ok {
		t.Fatal("member Automaton missing")
	}
	prd.Status = autonomous.PRDPlanning
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}

	body := doAutomation(t, s, http.MethodPost, "/api/automations/"+id+"/set_llm",
		map[string]any{"expected_version": a.Version, "backend": ""}, http.StatusConflict)
	if !strings.Contains(strings.ToLower(body), "planning") {
		t.Fatalf("409 body should say the member is planning, got %q", body)
	}
	if strings.HasPrefix(body, "409") {
		t.Fatalf("the 409 prefix must be stripped from the message, got %q", body)
	}

	// The refused call changed nothing: same version, so the same call with the
	// plan done is accepted against the version the caller already holds.
	prd.Status = autonomous.PRDNeedsReview
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	doAutomation(t, s, http.MethodPost, "/api/automations/"+id+"/set_llm",
		map[string]any{"expected_version": a.Version, "backend": ""}, http.StatusOK)
}

func TestStoryAndTaskRESTSetLLM_RefusedWhilePlanning(t *testing.T) {
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := mgr.CreatePRD("Build the thing.", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Story = []autonomous.Story{{ID: "s1", Title: "story", Tasks: []autonomous.Task{{ID: "t1", Title: "task"}}}}
	prd.Status = autonomous.PRDPlanning
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	srv := &Server{}
	srv.SetAutonomousAPI(autonomous.NewAPI(mgr))

	for _, tc := range []struct{ route, body string }{
		{"set_story_llm", `{"story_id":"s1","model":"claude-haiku-5-5"}`},
		{"set_task_llm", `{"task_id":"t1","model":"claude-haiku-5-5"}`},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/"+tc.route, strings.NewReader(tc.body))
		rr := httptest.NewRecorder()
		srv.handleAutonomousPRDs(rr, req)
		if rr.Code != http.StatusConflict {
			t.Fatalf("%s while planning: want 409, got %d body=%s", tc.route, rr.Code, rr.Body.String())
		}
		if !strings.Contains(strings.ToLower(rr.Body.String()), "planning") {
			t.Fatalf("%s: body should mention planning, got %q", tc.route, rr.Body.String())
		}
	}
}
