package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newResetTaskTestServer builds a real autonomous manager over a temp store
// holding one running PRD whose single task "t1" has failed, and returns the
// server plus that PRD's ID.
func newResetTaskTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []autonomous.Story{
		{ID: "s1", Title: "Story", Tasks: []autonomous.Task{{ID: "t1", Title: "Task"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = mgr.Store().GetPRD(prd.ID)
	prd.Status = autonomous.PRDRunning
	prd.Story[0].Tasks[0].Status = autonomous.TaskFailed
	prd.Story[0].Tasks[0].Error = "boom"
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return &Server{autonomousMgr: autonomous.NewAPI(mgr)}, prd.ID
}

func TestResetTaskCompactReply(t *testing.T) {
	const body = `{"task_id":"t1"}`

	t.Run("view=compact returns write view", func(t *testing.T) {
		s, id := newResetTaskTestServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+id+"/reset_task?view=compact", strings.NewReader(body))
		s.handleAutonomousPRDs(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		if rec.Body.Len() >= 1024 {
			t.Errorf("compact body is %d bytes, want < 1024", rec.Body.Len())
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode reply: %v", err)
		}
		if got["changed_task_id"] != "t1" {
			t.Errorf("changed_task_id = %v, want t1", got["changed_task_id"])
		}
		// ResetTask clears the task status to "" (the zero value, which means
		// pending) and WriteView omits empty statuses, so the key may be absent.
		if st, ok := got["changed_task_status"]; ok && st != string(autonomous.TaskPending) {
			t.Errorf("changed_task_status = %v, want %q or absent", st, autonomous.TaskPending)
		}
	})

	t.Run("no view query returns full PRD", func(t *testing.T) {
		s, id := newResetTaskTestServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+id+"/reset_task", strings.NewReader(body))
		s.handleAutonomousPRDs(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode reply: %v", err)
		}
		if _, ok := got["stories"]; !ok {
			t.Errorf("full reply missing stories key; body %s", rec.Body.String())
		}
	})
}

// newWriteCasesTestServer builds the same real manager as newResetTaskTestServer,
// but with a PRD in the given status holding two stories (s1: t1, t2; s2: t3)
// so every task- and story-scoped write case has a target.
func newWriteCasesTestServer(t *testing.T, status autonomous.PRDStatus) (*Server, string) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []autonomous.Story{
		{ID: "s1", Title: "Story one", Tasks: []autonomous.Task{{ID: "t1", Title: "Task one"}, {ID: "t2", Title: "Task two"}}},
		{ID: "s2", Title: "Story two", Tasks: []autonomous.Task{{ID: "t3", Title: "Task three"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = mgr.Store().GetPRD(prd.ID)
	prd.Status = status
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return &Server{autonomousMgr: autonomous.NewAPI(mgr)}, prd.ID
}

// TestWriteCasesCompactTaskLevel checks that each task- or story-scoped write
// case returns the compact write view when asked for view=compact.
func TestWriteCasesCompactTaskLevel(t *testing.T) {
	tests := []struct {
		name   string
		action string
		body   string
	}{
		{"edit_task", "edit_task", `{"task_id":"t1","new_spec":"reworded spec"}`},
		{"edit_story", "edit_story", `{"story_id":"s1","new_title":"Renamed story"}`},
		{"add_story", "add_story", `{"title":"New story"}`},
		{"remove_story", "remove_story", `{"story_id":"s2"}`},
		{"add_task", "add_task", `{"story_id":"s1","title":"New task"}`},
		{"remove_task", "remove_task", `{"story_id":"s1","task_id":"t2"}`},
		{"set_story_profile", "set_story_profile", `{"story_id":"s1","profile":""}`},
		{"approve_story", "approve_story", `{"story_id":"s1"}`},
		{"approve_task", "approve_task", `{"task_id":"t1"}`},
		{"reject_story", "reject_story", `{"story_id":"s1","reason":"not ready"}`},
		{"set_story_files", "set_story_files", `{"story_id":"s1","files":["a.go"]}`},
		{"set_task_files", "set_task_files", `{"task_id":"t1","files":["a.go"]}`},
		{"record_task_files_touched", "record_task_files_touched", `{"task_id":"t1","files":["a.go"]}`},
		{"cancel_story", "cancel_story", `{"story_id":"s1","reason":"dropped"}`},
		{"cancel_task", "cancel_task", `{"task_id":"t1","reason":"dropped"}`},
		{"set_story_llm", "set_story_llm", `{"story_id":"s1","backend":""}`},
		{"set_task_llm", "set_task_llm", `{"task_id":"t1","backend":""}`},
		{"set_task_project_dir", "set_task_project_dir", `{"task_id":"t1","project_dir":""}`},
		{"set_task_verification_llm", "set_task_verification_llm", `{"task_id":"t1"}`},
		{"set_story_verification_llm", "set_story_verification_llm", `{"story_id":"s1"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Approvals need an approved PRD; every other write needs needs_review.
			status := autonomous.PRDNeedsReview
			if tc.action == "approve_story" || tc.action == "approve_task" {
				status = autonomous.PRDApproved
			}
			s, id := newWriteCasesTestServer(t, status)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+id+"/"+tc.action+"?view=compact", strings.NewReader(tc.body))
			s.handleAutonomousPRDs(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
			}
			if rec.Body.Len() >= 1024 {
				t.Errorf("compact body is %d bytes, want < 1024", rec.Body.Len())
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode reply: %v", err)
			}
			for _, k := range []string{"id", "status", "rev"} {
				if _, ok := got[k]; !ok {
					t.Errorf("reply missing key %q; body %s", k, rec.Body.String())
				}
			}
		})
	}
}

func TestWriteAutonomousResult(t *testing.T) {
	prd := &autonomous.PRD{
		ID:     "abcd1234",
		Status: autonomous.PRDDraft,
		Rev:    7,
		Story: []autonomous.Story{
			{ID: "s1", Tasks: []autonomous.Task{{ID: "t1", Status: autonomous.TaskPending}}},
		},
	}

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantKeys   []string
		noKeys     []string
	}{
		{
			name:       "view absent returns full",
			query:      "",
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id", "stories"},
			noKeys:     []string{"message"},
		},
		{
			name:       "view=full returns full",
			query:      "?view=full",
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id", "stories"},
			noKeys:     []string{"message"},
		},
		{
			name:       "view=compact returns write view",
			query:      "?view=compact",
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id", "status", "rev", "message"},
			noKeys:     []string{"stories"},
		},
		{
			name:       "view=bogus is rejected",
			query:      "?view=bogus",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/abcd1234/tasks/t1"+tc.query, nil)
			rec := httptest.NewRecorder()

			s.writeAutonomousResult(rec, req, prd, "t1", "", "task updated")

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				return
			}

			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode reply: %v", err)
			}
			for _, k := range tc.wantKeys {
				if _, ok := body[k]; !ok {
					t.Errorf("reply missing key %q; body %s", k, rec.Body.String())
				}
			}
			for _, k := range tc.noKeys {
				if _, ok := body[k]; ok {
					t.Errorf("reply has unexpected key %q; body %s", k, rec.Body.String())
				}
			}
		})
	}
}

// TestWriteCasesCompactPRDLevel checks that each PRD-level write case returns
// the compact write view when asked for view=compact. The path holds {id}
// where the PRD goes. Each subtest builds its own PRD, so an earlier action
// cannot change the status a later one sees. The create case posts to the
// collection route, so it builds its PRD inside the handler.
func TestWriteCasesCompactPRDLevel(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status autonomous.PRDStatus
	}{
		{"run", http.MethodPost, "/api/autonomous/prds/{id}/run", ``, autonomous.PRDApproved},
		{"cancel", http.MethodDelete, "/api/autonomous/prds/{id}", ``, autonomous.PRDNeedsReview},
		{"approve", http.MethodPost, "/api/autonomous/prds/{id}/approve", `{"actor":"tester"}`, autonomous.PRDNeedsReview},
		{"reject", http.MethodPost, "/api/autonomous/prds/{id}/reject", `{"reason":"not ready"}`, autonomous.PRDNeedsReview},
		{"reset_to_draft", http.MethodPost, "/api/autonomous/prds/{id}/reset_to_draft", ``, autonomous.PRDCancelled},
		{"archive", http.MethodPost, "/api/autonomous/prds/{id}/archive", ``, autonomous.PRDCompleted},
		{"request_revision", http.MethodPost, "/api/autonomous/prds/{id}/request_revision", `{"note":"rework"}`, autonomous.PRDNeedsReview},
		{"set_llm", http.MethodPost, "/api/autonomous/prds/{id}/set_llm", `{"backend":""}`, autonomous.PRDNeedsReview},
		{"set_type", http.MethodPost, "/api/autonomous/prds/{id}/set_type", `{"type":""}`, autonomous.PRDNeedsReview},
		{"set_guided_mode", http.MethodPost, "/api/autonomous/prds/{id}/set_guided_mode", `{"guided_mode":true}`, autonomous.PRDNeedsReview},
		{"set_skills", http.MethodPost, "/api/autonomous/prds/{id}/set_skills", `{"skills":[]}`, autonomous.PRDNeedsReview},
		{"set_concurrency", http.MethodPost, "/api/autonomous/prds/{id}/set_concurrency", `{"max_concurrent_tasks":1}`, autonomous.PRDNeedsReview},
		{"set_priority", http.MethodPost, "/api/autonomous/prds/{id}/set_priority", `{"priority":1}`, autonomous.PRDNeedsReview},
		{"set_dirs", http.MethodPost, "/api/autonomous/prds/{id}/set_dirs", `{"read_dirs":[],"write_dirs":[]}`, autonomous.PRDNeedsReview},
		{"set_quality_gates", http.MethodPost, "/api/autonomous/prds/{id}/set_quality_gates", `{"enabled":false}`, autonomous.PRDNeedsReview},
		{"set_continue_on_story_failure", http.MethodPost, "/api/autonomous/prds/{id}/set_continue_on_story_failure", `{"continue_on_story_failure":true}`, autonomous.PRDNeedsReview},
		{"set_git", http.MethodPost, "/api/autonomous/prds/{id}/set_git", `{"auto_pr":false,"base_branch":"main"}`, autonomous.PRDNeedsReview},
		{"set_verification_llm", http.MethodPost, "/api/autonomous/prds/{id}/set_verification_llm", `{}`, autonomous.PRDNeedsReview},
		{"set_memory_seed", http.MethodPost, "/api/autonomous/prds/{id}/set_memory_seed", `{"enabled":false}`, autonomous.PRDNeedsReview},
		{"set_memory_harvest", http.MethodPost, "/api/autonomous/prds/{id}/set_memory_harvest", `{"enabled":false}`, autonomous.PRDNeedsReview},
		{"set_permission_mode", http.MethodPost, "/api/autonomous/prds/{id}/set_permission_mode", `{"permission_mode":""}`, autonomous.PRDNeedsReview},
		{"set_retry_policy", http.MethodPut, "/api/autonomous/prds/{id}/set_retry_policy", `{}`, autonomous.PRDNeedsReview},
		{"create", http.MethodPost, "/api/autonomous/prds", `{"spec":"new spec","project_dir":"{dir}"}`, autonomous.PRDNeedsReview},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, id := newWriteCasesTestServer(t, tc.status)
			path := strings.Replace(tc.path, "{id}", id, 1) + "?view=compact"
			body := strings.Replace(tc.body, "{dir}", t.TempDir(), 1)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, path, strings.NewReader(body))
			s.handleAutonomousPRDs(rec, req)

			if rec.Code < 200 || rec.Code > 299 {
				t.Fatalf("status = %d, want 2xx (body %q)", rec.Code, rec.Body.String())
			}
			if rec.Body.Len() >= 1024 {
				t.Errorf("compact body is %d bytes, want < 1024", rec.Body.Len())
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode reply: %v", err)
			}
			for _, k := range []string{"id", "status", "rev"} {
				if _, ok := got[k]; !ok {
					t.Errorf("reply missing key %q; body %s", k, rec.Body.String())
				}
			}
		})
	}
}

// newPRDDetailTestServer builds a real manager with one PRD holding a story
// whose task has failed, a story description and a task spec, plus one
// decision, so each detail level has something distinct to show.
func newPRDDetailTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	prd, err := mgr.CreatePRD("detail spec text", t.TempDir(), "", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []autonomous.Story{
		{ID: "s1", Title: "Story", Description: "story description", Tasks: []autonomous.Task{
			{ID: "t1", Title: "Task", Spec: "task spec text"},
		}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = mgr.Store().GetPRD(prd.ID)
	prd.Status = autonomous.PRDRunning
	prd.Story[0].Tasks[0].Status = autonomous.TaskFailed
	prd.Story[0].Tasks[0].Error = "boom"
	prd.Decisions = []autonomous.Decision{{Kind: "run"}}
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return &Server{autonomousMgr: autonomous.NewAPI(mgr)}, prd.ID
}

// TestGetPRDDetailLevels checks the detail query parameter on the PRD GET route.
func TestGetPRDDetailLevels(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantStatus int
		check      func(t *testing.T, body map[string]json.RawMessage)
	}{
		{
			name:       "absent returns full",
			query:      "",
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]json.RawMessage) {
				for _, k := range []string{"spec", "stories", "decisions"} {
					if _, ok := body[k]; !ok {
						t.Errorf("full reply missing key %q", k)
					}
				}
			},
		},
		{
			name:       "detail=full returns full with stories spec text",
			query:      "?detail=full",
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]json.RawMessage) {
				var stories []struct {
					Tasks []struct {
						Spec string `json:"spec"`
					} `json:"tasks"`
				}
				if err := json.Unmarshal(body["stories"], &stories); err != nil {
					t.Fatalf("decode stories: %v", err)
				}
				if len(stories) != 1 || len(stories[0].Tasks) != 1 || stories[0].Tasks[0].Spec != "task spec text" {
					t.Errorf("full stories do not carry task spec text: %s", body["stories"])
				}
			},
		},
		{
			name:       "detail=summary omits spec and decisions",
			query:      "?detail=summary",
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]json.RawMessage) {
				for _, k := range []string{"spec", "decisions"} {
					if _, ok := body[k]; ok {
						t.Errorf("summary reply has unexpected key %q", k)
					}
				}
				if _, ok := body["stories"]; !ok {
					t.Errorf("summary reply missing stories key")
				}
			},
		},
		{
			name:       "detail=tasks includes error on failed task",
			query:      "?detail=tasks",
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]json.RawMessage) {
				var tasks []map[string]json.RawMessage
				if err := json.Unmarshal(body["tasks"], &tasks); err != nil {
					t.Fatalf("decode tasks: %v", err)
				}
				if len(tasks) != 1 {
					t.Fatalf("tasks has %d entries, want 1", len(tasks))
				}
				if _, ok := tasks[0]["error"]; !ok {
					t.Errorf("failed task entry missing error key; body %s", body["tasks"])
				}
			},
		},
		{
			name:       "detail=bogus is rejected",
			query:      "?detail=bogus",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, id := newPRDDetailTestServer(t)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id+tc.query, nil)
			s.handleAutonomousPRDs(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.check == nil {
				return
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode reply: %v", err)
			}
			tc.check(t, body)
		})
	}
}

// decodeAnyMap decodes a JSON object into map[string]any so two bodies can be
// compared with reflect.DeepEqual, which ignores key order.
func decodeAnyMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return m
}

// TestPRDRESTDefaultUnchanged is a regression guard: with no view or detail
// query, the PRD GET route and the reset_task POST route must return the PRD
// exactly as the manager holds it. It also checks that the spec text reaches
// the default GET body.
//
// The one allowed divergence from the stored PRD is notify_events_effective —
// the computed read-time field that is filled on the API response copy and
// must never be persisted. The subtests strip it (and assert it is present
// and well-formed) before comparing the rest of the body.
func stripNotifyEffective(t *testing.T, got map[string]any) {
	t.Helper()
	eff, present := got["notify_events_effective"]
	if !present {
		t.Fatalf("body is missing notify_events_effective: %v", got)
	}
	m, ok := eff.(map[string]any)
	if !ok {
		t.Fatalf("notify_events_effective is not an object: %v", eff)
	}
	src, _ := m["source"].(string)
	ev, ok := m["events"].([]any)
	if !ok || src == "" || len(ev) == 0 {
		t.Fatalf("notify_events_effective is not well-formed: %v", eff)
	}
	delete(got, "notify_events_effective")
}

func TestPRDRESTDefaultUnchanged(t *testing.T) {
	const specText = "task spec text"

	t.Run("GET no query equals stored PRD", func(t *testing.T) {
		s, id := newPRDDetailTestServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id, nil)
		s.handleAutonomousPRDs(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		stored, ok := s.autonomousMgr.GetPRD(id)
		if !ok {
			t.Fatalf("store has no PRD %q", id)
		}
		want, err := json.Marshal(stored)
		if err != nil {
			t.Fatalf("marshal stored PRD: %v", err)
		}
		got := decodeAnyMap(t, rec.Body.Bytes())
		wantMap := decodeAnyMap(t, want)
		stripNotifyEffective(t, got)
		stripNotifyEffective(t, wantMap)
		if !reflect.DeepEqual(got, wantMap) {
			t.Errorf("default GET body differs from stored PRD (notify_events_effective aside)\n got: %s\nwant: %s", rec.Body.String(), want)
		}
		if !strings.Contains(rec.Body.String(), specText) {
			t.Errorf("default GET body does not contain task spec text %q; body %s", specText, rec.Body.String())
		}
	})

	t.Run("POST reset_task no view equals stored PRD", func(t *testing.T) {
		s, id := newPRDDetailTestServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+id+"/reset_task", strings.NewReader(`{"task_id":"t1"}`))
		s.handleAutonomousPRDs(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		stored, ok := s.autonomousMgr.GetPRD(id)
		if !ok {
			t.Fatalf("store has no PRD %q", id)
		}
		want, err := json.Marshal(stored)
		if err != nil {
			t.Fatalf("marshal stored PRD: %v", err)
		}
		got := decodeAnyMap(t, rec.Body.Bytes())
		wantMap := decodeAnyMap(t, want)
		// The reset_task write path returns the stored (unfilled) PRD, so the
		// body is the stored PRD verbatim; only the API-copied side carries
		// the computed field, which is stripped from the comparison.
		if _, ok := got["notify_events_effective"]; ok {
			t.Fatalf("reset_task body must not carry notify_events_effective (never-persisted field): %v", got["notify_events_effective"])
		}
		stripNotifyEffective(t, wantMap)
		if !reflect.DeepEqual(got, wantMap) {
			t.Errorf("default reset_task body differs from stored PRD (notify_events_effective aside)\n got: %s\nwant: %s", rec.Body.String(), want)
		}
	})
}
