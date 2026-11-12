// Every spec/description write handler refuses text that carries the
// capped-view truncation marker (autonomous.HasTruncationMarker): it answers
// 400 naming the field before the Automaton manager is called, and a clean
// value passes validation and reaches the manager. No daemon, no network —
// the manager is a fake that records which of its methods it was called.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// truncatedSpec carries the marker capText appends to a cut field — the shape
// HasTruncationMarker looks for anywhere in the text.
const truncatedSpec = "implement the change\n…[truncated: kept 20 of 1000 bytes]"

// cleanSpec passes validation.
const cleanSpec = "implement the change properly"

// tmFake extends fakeOrchAutonomous (orchestrator_enrich_test.go) to record
// which manager methods it was called, so the tests can prove a 400 refused
// the request before anything was written. It implements failureEscalationAPI
// so resolve_task_review reaches its spec check, and returns a PRD with one
// task so the missing-automaton/task existence check passes.
type tmFake struct {
	fakeOrchAutonomous
	calls []string
}

func (f *tmFake) mark(m string) { f.calls = append(f.calls, m) }

func (f *tmFake) called(m string) bool {
	for _, c := range f.calls {
		if c == m {
			return true
		}
	}
	return false
}

func (f *tmFake) CreatePRD(string, string, string, string, string) (any, error) {
	f.mark("CreatePRD")
	return nil, nil
}
func (f *tmFake) EditPRDFields(string, string, string, string) (any, error) {
	f.mark("EditPRDFields")
	return nil, nil
}
func (f *tmFake) EditStory(string, string, string, string, string) (any, error) {
	f.mark("EditStory")
	return nil, nil
}
func (f *tmFake) AddStory(string, string, string, string) (any, error) {
	f.mark("AddStory")
	return nil, nil
}
func (f *tmFake) AddTaskReadOnly(string, string, string, string, string, []string, bool, string) (any, error) {
	f.mark("AddTaskReadOnly")
	return nil, nil
}
func (f *tmFake) EditTask(string, string, string, *bool, string, string) (any, error) {
	f.mark("EditTask")
	return nil, nil
}
func (f *tmFake) CreateTemplate(string, string, string, string, []string) (any, error) {
	f.mark("CreateTemplate")
	return nil, nil
}
func (f *tmFake) UpdateTemplate(string, string, string, string, string, []string) (any, error) {
	f.mark("UpdateTemplate")
	return nil, nil
}
func (f *tmFake) ResolveTaskReview(prdID, taskID string, req autonomous.ReviewResolution, actor string) (any, error) {
	f.mark("ResolveTaskReview")
	return nil, nil
}
func (f *tmFake) SetFailureEscalation(string, string, *autonomous.FailureEscalationPolicy, string) (any, error) {
	f.mark("SetFailureEscalation")
	return nil, nil
}
func (f *tmFake) GetPRD(string) (any, bool) {
	return &autonomous.PRD{
		ID:    "p1",
		Title: "automaton",
		Story: []autonomous.Story{{
			ID:    "s1",
			Title: "story",
			Tasks: []autonomous.Task{{ID: "t1", Title: "task", Spec: "original spec"}},
		}},
	}, true
}

func newTruncationServer(t *testing.T) (*Server, *tmFake) {
	t.Helper()
	srv := NewServer(NewHub(), nil, "h", "", nil, nil, "")
	f := &tmFake{}
	srv.SetAutonomousAPI(f)
	return srv, f
}

// run executes one request against the PRDs route handler (or the template
// handler when route is "templates") and returns the response.
func run(s *Server, method, route, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, route, strings.NewReader(body))
	w := httptest.NewRecorder()
	if strings.Contains(route, "/templates") {
		s.handleAutonomousTemplates(w, req)
		return w
	}
	s.handleAutonomousPRDs(w, req)
	return w
}

// assertRefused runs the marker request and proves the 400 named the field
// and never reached the manager, then runs the clean request and proves it
// was accepted and reached the manager.
func assertRefused(t *testing.T, f *tmFake, s *Server, method, route, field, mgr string, markerBody, cleanBody string) {
	t.Helper()
	wm := run(s, method, route, markerBody)
	if wm.Code != http.StatusBadRequest {
		t.Fatalf("%s: expected 400 for %s with truncation marker, got %d: %s", mgr, field, wm.Code, wm.Body.String())
	}
	if !strings.Contains(wm.Body.String(), field) {
		t.Fatalf("%s: 400 body must name %q, got: %s", mgr, field, wm.Body.String())
	}
	if f.called(mgr) {
		t.Fatalf("%s: manager %s must not be called for a marker-carrying %s", mgr, mgr, field)
	}
	wc := run(s, method, route, cleanBody)
	if wc.Code < 200 || wc.Code >= 300 {
		t.Fatalf("%s: clean %s should be accepted, got %d: %s", mgr, field, wc.Code, wc.Body.String())
	}
	if !f.called(mgr) {
		t.Fatalf("%s: manager %s should be called for a clean %s", mgr, mgr, field)
	}
}

func TestTruncationMarker_CreatePRD(t *testing.T) {
	srv, f := newTruncationServer(t)
	marker, _ := json.Marshal(map[string]string{"spec": truncatedSpec, "project_dir": "/tmp/p"})
	clean, _ := json.Marshal(map[string]string{"spec": cleanSpec, "project_dir": "/tmp/p"})
	assertRefused(t, f, srv, http.MethodPost, "/api/autonomous/prds", "spec", "CreatePRD", string(marker), string(clean))
}

func TestTruncationMarker_EditPRD(t *testing.T) {
	srv, f := newTruncationServer(t)
	marker, _ := json.Marshal(map[string]string{"spec": truncatedSpec})
	clean, _ := json.Marshal(map[string]string{"spec": cleanSpec})
	assertRefused(t, f, srv, http.MethodPatch, "/api/autonomous/prds/p1", "spec", "EditPRDFields", string(marker), string(clean))
}

func TestTruncationMarker_EditTask(t *testing.T) {
	srv, f := newTruncationServer(t)
	marker, _ := json.Marshal(map[string]string{"task_id": "t1", "new_spec": truncatedSpec})
	clean, _ := json.Marshal(map[string]string{"task_id": "t1", "new_spec": cleanSpec})
	assertRefused(t, f, srv, http.MethodPost, "/api/autonomous/prds/p1/edit_task", "new_spec", "EditTask", string(marker), string(clean))
}

func TestTruncationMarker_EditStory(t *testing.T) {
	srv, f := newTruncationServer(t)
	marker, _ := json.Marshal(map[string]string{"story_id": "s1", "new_description": truncatedSpec})
	clean, _ := json.Marshal(map[string]string{"story_id": "s1", "new_description": cleanSpec})
	assertRefused(t, f, srv, http.MethodPost, "/api/autonomous/prds/p1/edit_story", "new_description", "EditStory", string(marker), string(clean))
}

func TestTruncationMarker_AddStory(t *testing.T) {
	srv, f := newTruncationServer(t)
	marker, _ := json.Marshal(map[string]string{"title": "a story", "description": truncatedSpec})
	clean, _ := json.Marshal(map[string]string{"title": "a story", "description": cleanSpec})
	assertRefused(t, f, srv, http.MethodPost, "/api/autonomous/prds/p1/add_story", "description", "AddStory", string(marker), string(clean))
}

func TestTruncationMarker_AddTask(t *testing.T) {
	srv, f := newTruncationServer(t)
	marker, _ := json.Marshal(map[string]string{"story_id": "s1", "title": "a task", "spec": truncatedSpec})
	clean, _ := json.Marshal(map[string]string{"story_id": "s1", "title": "a task", "spec": cleanSpec})
	assertRefused(t, f, srv, http.MethodPost, "/api/autonomous/prds/p1/add_task", "spec", "AddTaskReadOnly", string(marker), string(clean))
}

func TestTruncationMarker_Templates(t *testing.T) {
	// Create: a marker in description is refused naming description, a marker
	// in spec naming spec; clean values reach the manager.
	srv, f := newTruncationServer(t)
	markerDesc, _ := json.Marshal(map[string]string{"title": "a template", "description": truncatedSpec, "spec": cleanSpec})
	markerSpec, _ := json.Marshal(map[string]string{"title": "a template", "description": cleanSpec, "spec": truncatedSpec})
	clean, _ := json.Marshal(map[string]string{"title": "a template", "description": cleanSpec, "spec": cleanSpec})
	w := run(srv, http.MethodPost, "/api/autonomous/templates", string(markerDesc))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "description") {
		t.Fatalf("create template, marker in description: expected 400 naming description, got %d: %s", w.Code, w.Body.String())
	}
	w = run(srv, http.MethodPost, "/api/autonomous/templates", string(markerSpec))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "spec") {
		t.Fatalf("create template, marker in spec: expected 400 naming spec, got %d: %s", w.Code, w.Body.String())
	}
	if f.called("CreateTemplate") {
		t.Fatal("create template: CreateTemplate must not be called for marker-carrying fields")
	}
	wc := run(srv, http.MethodPost, "/api/autonomous/templates", string(clean))
	if wc.Code < 200 || wc.Code >= 300 {
		t.Fatalf("create template, clean: expected 2xx, got %d: %s", wc.Code, wc.Body.String())
	}
	if !f.called("CreateTemplate") {
		t.Fatal("create template: CreateTemplate should be called for clean fields")
	}

	// Update: same refusal on the PUT route.
	srv2, f2 := newTruncationServer(t)
	assertRefused(t, f2, srv2, http.MethodPut, "/api/autonomous/templates/t1", "spec", "UpdateTemplate",
		string(markerSpec), string(clean))
}

func TestTruncationMarker_ResolveTaskReview(t *testing.T) {
	srv, f := newTruncationServer(t)
	marker, _ := json.Marshal(map[string]string{"task_id": "t1", "action": "retry", "spec": truncatedSpec})
	clean, _ := json.Marshal(map[string]string{"task_id": "t1", "action": "retry", "spec": cleanSpec})
	assertRefused(t, f, srv, http.MethodPost, "/api/autonomous/prds/p1/resolve_task_review", "spec", "ResolveTaskReview", string(marker), string(clean))
}
