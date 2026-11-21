package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

// The set-up routes answered 503 "supervisor set-up unavailable" in a running
// daemon because nothing installed their dependencies. Wired as main.go wires
// them, over a real schedule store and a temp data directory, they work. No
// model is called: set-up only writes schedules.
func TestSupervisorSetupWiredNotUnavailable(t *testing.T) {
	dir := t.TempDir()
	projDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DataDir = dir
	cfg.Projects = map[string]config.ProjectConfigEntry{"demo": {Dir: projDir}}
	sched, err := session.NewScheduleStore(filepath.Join(dir, "schedule.json"))
	if err != nil {
		t.Fatal(err)
	}
	hs := server.New(&cfg.Server, cfg, "", dir, nil, "test-host", nil)
	wireSupervisorSetup(hs, cfg, sched, nil, nil)
	h := hs.Handler()

	do := func(method, path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	const ok = `{"project":"demo","model":"claude-sonnet-5-5","schedules":[{"name":"watch","cron":"*/20 * * * *"}]}`

	code, first := do(http.MethodPost, "/api/supervisor-setup", ok)
	if code != http.StatusOK {
		t.Fatalf("set-up = %d, want 200 (503 means not wired): %v", code, first)
	}
	if first["already_set_up"] != false {
		t.Fatalf("first run already_set_up = %v, want false", first["already_set_up"])
	}
	code, second := do(http.MethodPost, "/api/supervisor-setup", ok)
	if code != http.StatusOK || second["already_set_up"] != true {
		t.Fatalf("second run = %d %v, want 200 and already_set_up", code, second)
	}
	if n := len(sched.List()); n != 1 {
		t.Fatalf("schedules after two runs = %d, want 1", n)
	}
	for _, model := range []string{"claude-opus-5-5", ""} {
		body := `{"project":"demo","model":"` + model + `"}`
		if code, _ := do(http.MethodPost, "/api/supervisor-setup", body); code != http.StatusBadRequest {
			t.Fatalf("model %q = %d, want 400", model, code)
		}
	}
	if code, _ := do(http.MethodGet, "/api/supervisor-setup/state?project=demo", ""); code != http.StatusOK {
		t.Fatalf("GET state = %d, want 200", code)
	}
	if code, _ := do(http.MethodGet, "/api/supervisor-setup/corrections?project=demo", ""); code != http.StatusOK {
		t.Fatalf("GET corrections = %d, want 200", code)
	}
	if code, _ := do(http.MethodPost, "/api/supervisor-setup/state",
		`{"project":"demo","section":"work","id":"a","text":"t","who":"op","why":"test"}`); code != http.StatusOK {
		t.Fatalf("POST state = %d, want 200", code)
	}
	rec, _, _ := (&supervisorPinnedFile{path: filepath.Join(dir, "supervisor_state.json")}).LoadPinned("supervisor-state/demo/supervisor")
	if rec == "" {
		t.Fatal("state record was not persisted under the data directory")
	}
}
