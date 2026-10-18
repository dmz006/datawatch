// Configuration surface round-trip for autonomous.task_override_statuses
// (v9.5.5): PUT /api/config accepts the array and comma-separated string
// wire shapes, rejects unknown PRD status names with a 400 before anything
// is stored, renders the effective (defaulted) list on GET, and propagates
// the stored list to the live autonomous manager.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func putRawConfig(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader([]byte(body))))
	return rr
}

func getTaskOverrideStatuses(t *testing.T, s *Server) []string {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["task_override_statuses"]
	if !ok {
		t.Fatalf("GET /api/config autonomous.task_override_statuses missing")
	}
	var got []string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode task_override_statuses %s: %v", raw, err)
	}
	return got
}

func TestTaskOverrideStatuses_PutUnknownNameRejected(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	rr := putRawConfig(t, s, `{"autonomous.task_override_statuses":["bogus"]}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT unknown status: want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `unknown PRD status "bogus"`) {
		t.Errorf("400 body lacks the unknown-status message: %s", body)
	}
	if !strings.Contains(body, "blocked") {
		t.Errorf("400 body lacks the known-status set: %s", body)
	}
	if !strings.HasPrefix(body, "autonomous.task_override_statuses:") {
		t.Errorf("400 body should start with the full key path: %s", body)
	}
	if len(s.cfg.Autonomous.TaskOverrideStatuses) != 0 || len(mgr.Config().TaskOverrideStatuses) != 0 {
		t.Errorf("rejected PUT must not store anything: cfg=%v mgr=%v",
			s.cfg.Autonomous.TaskOverrideStatuses, mgr.Config().TaskOverrideStatuses)
	}
}

func TestTaskOverrideStatuses_PutCSVWithUnknownNameRejected(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	rr := putRawConfig(t, s, `{"autonomous.task_override_statuses":"blocked,nope"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT CSV with unknown status: want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `unknown PRD status "nope"`) {
		t.Errorf("400 body lacks the unknown-status message: %s", rr.Body.String())
	}
}

func TestTaskOverrideStatuses_PutArrayRoundTripAndLivePropagation(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.task_override_statuses": []string{"blocked", "running"},
	})
	want := []string{"blocked", "running"}
	if !reflect.DeepEqual(s.cfg.Autonomous.TaskOverrideStatuses, want) {
		t.Fatalf("stored list = %v, want %v", s.cfg.Autonomous.TaskOverrideStatuses, want)
	}
	if got := getTaskOverrideStatuses(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET /api/config = %v, want %v", got, want)
	}
	if got := mgr.Config().TaskOverrideStatuses; !reflect.DeepEqual(got, want) {
		t.Fatalf("live manager list = %v, want %v", got, want)
	}
}

func TestTaskOverrideStatuses_PutCSVStringForm(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	if rr := putRawConfig(t, s, `{"autonomous.task_override_statuses":"blocked"}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT string form: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	want := []string{"blocked"}
	if !reflect.DeepEqual(s.cfg.Autonomous.TaskOverrideStatuses, want) {
		t.Fatalf("stored list = %v, want %v", s.cfg.Autonomous.TaskOverrideStatuses, want)
	}
	if got := mgr.Config().TaskOverrideStatuses; !reflect.DeepEqual(got, want) {
		t.Fatalf("live manager list = %v, want %v", got, want)
	}
}

func TestTaskOverrideStatuses_PutEmptyArrayRendersDefault(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.task_override_statuses": []string{"blocked"},
	})
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.task_override_statuses": []string{},
	})
	if len(s.cfg.Autonomous.TaskOverrideStatuses) != 0 {
		t.Fatalf("empty PUT should clear the stored list, got %v", s.cfg.Autonomous.TaskOverrideStatuses)
	}
	want := autonomous.TaskOverrideStatusesDefault()
	if got := getTaskOverrideStatuses(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET after empty PUT = %v, want the effective default %v", got, want)
	}
}

func TestTaskOverrideStatuses_GetDefaultWhenUnset(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	want := autonomous.TaskOverrideStatusesDefault()
	if got := getTaskOverrideStatuses(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET with unset list = %v, want %v", got, want)
	}
}
