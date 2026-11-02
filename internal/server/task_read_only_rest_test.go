package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func postTaskWrite(t *testing.T, s *Server, id, action, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+id+"/"+action, strings.NewReader(body))
	s.handleAutonomousPRDs(rec, req)
	return rec
}

func storedTask(t *testing.T, s *Server, id, taskID string) autonomous.Task {
	t.Helper()
	v, ok := s.autonomousMgr.GetPRD(id)
	if !ok || v == nil {
		t.Fatal("GetPRD: not found")
	}
	prd := v.(*autonomous.PRD)
	for _, st := range prd.Story {
		for _, tk := range st.Tasks {
			if tk.ID == taskID {
				return tk
			}
		}
	}
	t.Fatalf("task %q not found", taskID)
	return autonomous.Task{}
}

func TestRESTEditTaskSetsReadOnlyWithoutNewSpec(t *testing.T) {
	s, id := newWriteCasesTestServer(t, autonomous.PRDNeedsReview)
	if rec := postTaskWrite(t, s, id, "edit_task", `{"task_id":"t1","read_only":true}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if !storedTask(t, s, id, "t1").ReadOnly {
		t.Fatal("task t1 not read_only after the edit")
	}
	if rec := postTaskWrite(t, s, id, "edit_task", `{"task_id":"t1","read_only":false}`); rec.Code != http.StatusOK {
		t.Fatalf("clear status = %d, body %q", rec.Code, rec.Body.String())
	}
	if storedTask(t, s, id, "t1").ReadOnly {
		t.Fatal("task t1 still read_only after clearing")
	}
}

func TestRESTEditTaskRequiresSpecOrReadOnly(t *testing.T) {
	s, id := newWriteCasesTestServer(t, autonomous.PRDNeedsReview)
	if rec := postTaskWrite(t, s, id, "edit_task", `{"task_id":"t1"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRESTEditTaskSpecOnlyLeavesReadOnlyAlone(t *testing.T) {
	s, id := newWriteCasesTestServer(t, autonomous.PRDNeedsReview)
	_ = postTaskWrite(t, s, id, "edit_task", `{"task_id":"t1","read_only":true}`)
	if rec := postTaskWrite(t, s, id, "edit_task", `{"task_id":"t1","new_spec":"reworded"}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	tk := storedTask(t, s, id, "t1")
	if !tk.ReadOnly || tk.Spec != "reworded" {
		t.Fatalf("read_only=%v spec=%q, want true and reworded", tk.ReadOnly, tk.Spec)
	}
}

func TestRESTAddTaskAcceptsReadOnly(t *testing.T) {
	s, id := newWriteCasesTestServer(t, autonomous.PRDNeedsReview)
	rec := postTaskWrite(t, s, id, "add_task", `{"story_id":"s1","title":"Run the gate","read_only":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	v, _ := s.autonomousMgr.GetPRD(id)
	var found, plain bool
	for _, tk := range v.(*autonomous.PRD).Story[0].Tasks {
		if tk.Title == "Run the gate" {
			found = tk.ReadOnly
		}
	}
	if !found {
		t.Fatal("added task is not read_only")
	}
	if rec := postTaskWrite(t, s, id, "add_task", `{"story_id":"s1","title":"Write code"}`); rec.Code != http.StatusOK {
		t.Fatalf("plain add status = %d", rec.Code)
	}
	v, _ = s.autonomousMgr.GetPRD(id)
	for _, tk := range v.(*autonomous.PRD).Story[0].Tasks {
		if tk.Title == "Write code" {
			plain = !tk.ReadOnly
		}
	}
	if !plain {
		t.Fatal("a task added without read_only came out read_only")
	}
}

func TestRESTEditTaskReadOnlyHonoursLockedStatus(t *testing.T) {
	s, id := newWriteCasesTestServer(t, autonomous.PRDRunning)
	if rec := postTaskWrite(t, s, id, "edit_task", `{"task_id":"t1","read_only":true}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 on a running PRD", rec.Code)
	}
}

func TestOpenAPIDocumentsTaskReadOnly(t *testing.T) {
	for _, p := range []string{"web/openapi.yaml"} {
		raw, err := webFS.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		s := string(raw)
		for _, want := range []string{"/api/autonomous/prds/{id}/edit_task:", "/api/autonomous/prds/{id}/add_task:", "read_only:"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s lacks %q", p, want)
			}
		}
	}
}
