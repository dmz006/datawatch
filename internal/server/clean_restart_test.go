package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cleanRestartAPI records the cleanRestart argument ResetTaskWithOptions gets.
type cleanRestartAPI struct {
	fakeOrchAutonomous
	called       bool
	cleanRestart bool
}

func (a *cleanRestartAPI) ResetTaskWithOptions(prdID, taskID, actor string, force, cleanRestart bool) (any, error) {
	a.called = true
	a.cleanRestart = cleanRestart
	return map[string]any{"id": prdID}, nil
}

// TestCleanRestartReachesRESTAndMCP checks that clean_restart true in the REST
// reset_task body reaches the autonomous manager.
func TestCleanRestartReachesRESTAndMCP(t *testing.T) {
	api := &cleanRestartAPI{}
	s := &Server{autonomousMgr: api}

	body := `{"task_id":"task-1","actor":"operator","clean_restart":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-1/reset_task", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !api.called || !api.cleanRestart {
		t.Fatalf("manager got called=%v cleanRestart=%v; want called with true", api.called, api.cleanRestart)
	}
}
