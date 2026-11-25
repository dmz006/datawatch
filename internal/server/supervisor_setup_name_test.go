package server

import (
	"net/http"
	"strings"
	"testing"
)

// A schedule name becomes part of a file name under the project, so one that
// is not a single path segment is refused before anything is created.
func TestSupervisorSetupRefusesUnsafeScheduleName(t *testing.T) {
	for _, name := range []string{"../../escaped", "x/../../y", "a/b", `a\b`, "..", ".", "a\nb", "a\x00b"} {
		s, sch, _ := newSetupServer()
		body := map[string]any{"project": "demo", "model": "claude-haiku-5-5",
			"schedules": []map[string]string{{"name": "watch"}, {"name": name}}}
		rr := setupCall(s.handleSupervisorSetup, http.MethodPost, "/api/supervisor-setup", body)
		if rr.Code != 400 || !strings.Contains(rr.Body.String(), "schedule name") {
			t.Errorf("name %q: code %d %s", name, rr.Code, rr.Body)
		}
		if len(sch.opts) != 0 {
			t.Errorf("name %q: %d schedules were added", name, len(sch.opts))
		}
	}
}

// The names in use, with underscores, dots and spaces, stay accepted.
func TestSupervisorSetupAcceptsPlainScheduleNames(t *testing.T) {
	s, sch, _ := newSetupServer()
	body := map[string]any{"project": "demo", "model": "claude-haiku-5-5",
		"schedules": []map[string]string{{"name": "watch"}, {"name": "window_guard"}, {"name": "full"}, {"name": "nightly pass v1.2"}}}
	rr := setupCall(s.handleSupervisorSetup, http.MethodPost, "/api/supervisor-setup", body)
	if rr.Code != 200 || len(sch.opts) != 4 {
		t.Fatalf("code %d, %d schedules: %s", rr.Code, len(sch.opts), rr.Body)
	}
}
