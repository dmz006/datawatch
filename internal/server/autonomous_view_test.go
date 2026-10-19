package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
