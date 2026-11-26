// The audit log records that a schedule was created, never the command it
// will type.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/session"
)

func TestScheduleCreateAuditCarriesNoCommandText(t *testing.T) {
	const command = "deploy with the SECRET-COMMAND"
	routes := map[string]struct {
		serve func(s *Server, w http.ResponseWriter, r *http.Request)
		body  map[string]any
	}{
		"/api/schedule": {func(s *Server, w http.ResponseWriter, r *http.Request) { s.handlePostSchedule(w, r) },
			map[string]any{"session_id": "ab12", "command": command}},
		"/api/schedules command": {func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleSchedules(w, r) },
			map[string]any{"type": "command", "session_id": "ab12", "command": command}},
		"/api/schedules spawn": {func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleSchedules(w, r) },
			map[string]any{"type": "spawn", "command": command, "run_at": "2030-01-01T00:00:00Z"}},
		"/api/schedules new session": {func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleSchedules(w, r) },
			map[string]any{"type": "new_session", "command": command, "run_at": "2030-01-01T00:00:00Z"}},
	}
	for name, c := range routes {
		t.Run(name, func(t *testing.T) {
			srv, al := gh201Phase4Server(t)
			store, err := session.NewScheduleStore(filepath.Join(t.TempDir(), "schedule.json"))
			if err != nil {
				t.Fatal(err)
			}
			srv.schedStore = store
			body, _ := json.Marshal(c.body)
			w := httptest.NewRecorder()
			c.serve(srv, w, httptest.NewRequest(http.MethodPost, "/api/schedules", bytes.NewReader(body)))
			if w.Code != http.StatusCreated {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			entries, err := al.Read(audit.QueryFilter{})
			if err != nil || len(entries) != 1 {
				t.Fatalf("audit entries = %v, %v; want one", entries, err)
			}
			raw, _ := json.Marshal(entries[0])
			if strings.Contains(string(raw), "SECRET-COMMAND") {
				t.Fatalf("the audit entry carries the command: %s", raw)
			}
			if entries[0].Action != "create" || entries[0].Details["resource_type"] != "schedule" || entries[0].Details["resource_id"] == "" {
				t.Fatalf("audit entry = %s", raw)
			}
			if got := entries[0].Details["command_length"]; got != float64(len(command)) {
				t.Fatalf("command_length = %v, want %d", got, len(command))
			}
		})
	}
}
