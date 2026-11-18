// REST creation of an Automaton with a parent: parent_prd_id reaches the
// Manager's create-with-parent path, an unknown parent is refused with a 404
// before anything is created, and a parent id stands in for project_dir.

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

type childInheritSpy struct {
	fakeOrchAutonomous
	known                       map[string]bool
	plain, withParent           int
	parent, projectDir, backend string
	model                       string
}

func (s *childInheritSpy) GetPRD(id string) (any, bool) {
	if s.known[id] {
		return map[string]any{"id": id}, true
	}
	return nil, false
}

func (s *childInheritSpy) CreatePRD(spec, projectDir, backend, model, effort string) (any, error) {
	s.plain++
	s.projectDir, s.backend, s.model = projectDir, backend, model
	return map[string]any{"id": "new-1", "status": "draft"}, nil
}

func (s *childInheritSpy) CreatePRDWithParent(spec, projectDir, backend, model, effort, parent string) (any, error) {
	s.withParent++
	s.parent, s.projectDir, s.backend, s.model = parent, projectDir, backend, model
	return map[string]any{"id": "new-1", "status": "draft", "parent_prd_id": parent}, nil
}

func childInheritPost(s *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)
	return rr
}

func TestChildInheritParity_REST_ForwardsParentAndExplicitValues(t *testing.T) {
	spy := &childInheritSpy{known: map[string]bool{"parent-1": true}}
	s := &Server{autonomousMgr: spy, cfg: &config.Config{}}
	rr := childInheritPost(s, `{"spec":"child work","parent_prd_id":" parent-1 ","project_dir":"/explicit","backend":"b","model":"m"}`)
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if spy.withParent != 1 || spy.plain != 0 {
		t.Fatalf("want one create-with-parent call, got withParent=%d plain=%d", spy.withParent, spy.plain)
	}
	if spy.parent != "parent-1" || spy.projectDir != "/explicit" || spy.backend != "b" || spy.model != "m" {
		t.Fatalf("forwarded %q %q %q %q", spy.parent, spy.projectDir, spy.backend, spy.model)
	}
}

func TestChildInheritParity_REST_ParentStandsInForProjectDir(t *testing.T) {
	spy := &childInheritSpy{known: map[string]bool{"parent-1": true}}
	s := &Server{autonomousMgr: spy, cfg: &config.Config{}}
	rr := childInheritPost(s, `{"spec":"child work","parent_prd_id":"parent-1"}`)
	if rr.Code >= 300 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if spy.withParent != 1 || spy.projectDir != "" {
		t.Fatalf("withParent=%d projectDir=%q", spy.withParent, spy.projectDir)
	}
}

func TestChildInheritParity_REST_UnknownParentRefused(t *testing.T) {
	spy := &childInheritSpy{known: map[string]bool{}}
	s := &Server{autonomousMgr: spy, cfg: &config.Config{}}
	rr := childInheritPost(s, `{"spec":"child work","parent_prd_id":"nope","project_dir":"/x"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404: %s", rr.Code, rr.Body.String())
	}
	if spy.withParent != 0 || spy.plain != 0 {
		t.Fatalf("nothing should be created, got %d/%d", spy.withParent, spy.plain)
	}
}

func TestChildInheritParity_REST_NoParentIsPlainCreate(t *testing.T) {
	spy := &childInheritSpy{known: map[string]bool{}}
	s := &Server{autonomousMgr: spy, cfg: &config.Config{}}
	rr := childInheritPost(s, `{"spec":"plain work","project_dir":"/x"}`)
	if rr.Code >= 300 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if spy.plain != 1 || spy.withParent != 0 {
		t.Fatalf("plain=%d withParent=%d", spy.plain, spy.withParent)
	}
}
