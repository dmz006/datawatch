// BL447 — POST /api/autonomous/prds/{id}/set_task_project_dir.

package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

type taskProjectDirTestOrch struct {
	fakeOrchAutonomous
	calls           int
	gotTask, gotDir string
	err             error
}

func (o *taskProjectDirTestOrch) SetTaskProjectDir(_, taskID, dir, _ string) (any, error) {
	o.calls++
	o.gotTask, o.gotDir = taskID, dir
	if o.err != nil {
		return nil, o.err
	}
	return &autonomous.PRD{ID: "p1"}, nil
}

func bl447Post(t *testing.T, orch *taskProjectDirTestOrch, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv := &Server{autonomousMgr: orch}
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/p1/set_task_project_dir", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)
	return rr
}

func TestBL447_SetTaskProjectDir_Set(t *testing.T) {
	orch := &taskProjectDirTestOrch{}
	rr := bl447Post(t, orch, `{"task_id":"t1","project_dir":"/srv/proj/sub"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if orch.calls != 1 || orch.gotTask != "t1" || orch.gotDir != "/srv/proj/sub" {
		t.Errorf("calls=%d task=%q dir=%q", orch.calls, orch.gotTask, orch.gotDir)
	}
}

func TestBL447_SetTaskProjectDir_Clear(t *testing.T) {
	orch := &taskProjectDirTestOrch{}
	rr := bl447Post(t, orch, `{"task_id":"t1","project_dir":""}`)
	if rr.Code != http.StatusOK || orch.calls != 1 || orch.gotDir != "" {
		t.Fatalf("code=%d calls=%d dir=%q", rr.Code, orch.calls, orch.gotDir)
	}
}

func TestBL447_SetTaskProjectDir_Invalid(t *testing.T) {
	orch := &taskProjectDirTestOrch{err: errors.New("project dir must be absolute")}
	rr := bl447Post(t, orch, `{"task_id":"t1","project_dir":"rel/dir"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "absolute") {
		t.Fatalf("want 400 with validation error, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestBL447_SetTaskProjectDir_UnknownTask(t *testing.T) {
	orch := &taskProjectDirTestOrch{err: errors.New(`task "nope" not found in prd "p1"`)}
	rr := bl447Post(t, orch, `{"task_id":"nope","project_dir":"/srv/x"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "not found") {
		t.Fatalf("want 400 not found, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestBL447_SetTaskProjectDir_MissingTaskID(t *testing.T) {
	orch := &taskProjectDirTestOrch{}
	rr := bl447Post(t, orch, `{"project_dir":"/srv/x"}`)
	if rr.Code != http.StatusBadRequest || orch.calls != 0 {
		t.Fatalf("want 400 w/o setter call, got %d calls=%d", rr.Code, orch.calls)
	}
}
