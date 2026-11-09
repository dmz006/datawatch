package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// standardTasksMux serves the standard-task route the way server.go
// registers it, so the test drives the same handler the daemon serves.
func standardTasksMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/autonomous/standard-tasks", s.handleAutonomousStandardTasks)
	return mux
}

// TestStandardTaskRESTRead calls GET /api/autonomous/standard-tasks and checks
// the envelope, the snake_case field names of each row, and the built-in ids.
func TestStandardTaskRESTRead(t *testing.T) {
	cfg := config.DefaultConfig()
	s := &Server{cfg: cfg}
	mux := standardTasksMux(s)

	// The route is registered next to the other /api/autonomous routes.
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	if !strings.Contains(string(src), `apiMux.HandleFunc("/api/autonomous/standard-tasks", api.handleAutonomousStandardTasks)`) {
		t.Fatalf("server.go does not register /api/autonomous/standard-tasks")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/standard-tasks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", rec.Code, rec.Body.String())
	}

	// Field names are checked on the raw JSON so a renamed tag fails here.
	var raw struct {
		Enabled *bool                        `json:"enabled"`
		Tasks   []map[string]json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v body %s", err, rec.Body.String())
	}
	if raw.Enabled == nil || *raw.Enabled != cfg.Autonomous.TaskTemplates.Enabled {
		t.Fatalf("enabled = %v, want global %v", raw.Enabled, cfg.Autonomous.TaskTemplates.Enabled)
	}
	if raw.Tasks == nil {
		t.Fatalf("tasks is null, want an array")
	}
	wantFields := []string{"id", "title", "kind", "summary", "offered_when", "enabled", "project_override", "mode"}
	for _, row := range raw.Tasks {
		if len(row) != len(wantFields) {
			t.Errorf("row has %d fields, want %d: %v", len(row), len(wantFields), row)
		}
		for _, f := range wantFields {
			if _, ok := row[f]; !ok {
				t.Errorf("row missing field %q", f)
			}
		}
	}

	// The built-in ids appear once each, in config order.
	builtIn := autonomous.BuiltInTaskTemplates()
	if len(raw.Tasks) != len(builtIn) {
		t.Fatalf("tasks = %d, want %d", len(raw.Tasks), len(builtIn))
	}
	for i, def := range builtIn {
		var id string
		if err := json.Unmarshal(raw.Tasks[i]["id"], &id); err != nil {
			t.Fatalf("row %d id: %v", i, err)
		}
		if id != def.ID {
			t.Errorf("row %d id = %q, want %q", i, id, def.ID)
		}
	}
	wantIDs := []string{
		autonomous.TemplateChangelogEntry, autonomous.TemplateParityIssueDraft,
		autonomous.TemplateDocsSet, autonomous.TemplateBoundedLoadStep,
		autonomous.TemplateFinalGate, autonomous.TemplateVersionBumpLeftToMerge,
	}
	for _, want := range wantIDs {
		found := false
		for _, row := range raw.Tasks {
			var id string
			_ = json.Unmarshal(row["id"], &id)
			if id == want {
				found = true
			}
		}
		if !found {
			t.Errorf("built-in id %q missing from listing", want)
		}
	}

	// A project query selects its override; the mode is the constant.
	req = httptest.NewRequest(http.MethodGet, "/api/autonomous/standard-tasks?project=/work/none", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("project GET status = %d", rec.Code)
	}
	var rows struct {
		Tasks []autonomous.StandardTask `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	for _, row := range rows.Tasks {
		if row.Mode != autonomous.StandardTaskModeModelSession {
			t.Errorf("row %q mode = %q", row.ID, row.Mode)
		}
		if row.ProjectOverride != nil {
			t.Errorf("row %q override = %v, want nil with no project override", row.ID, *row.ProjectOverride)
		}
	}

	// Only GET is served.
	req = httptest.NewRequest(http.MethodPost, "/api/autonomous/standard-tasks", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
}
