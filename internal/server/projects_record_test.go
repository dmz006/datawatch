// Project record fields on the /api/projects endpoints.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// recordDo sends one request to handleProjects and returns the recorder.
func recordDo(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rr := httptest.NewRecorder()
	s.handleProjects(rr, req)
	return rr
}

const recordFullBody = `{
  "name": "acme",
  "dir": "/srv/acme",
  "default_backend": "claude-code",
  "owner": "platform",
  "repository": "github.com/acme/app",
  "default_branch": "main",
  "arcs": [
    {"automation_id": "auto-b", "priority": 2, "state": "active"},
    {"automation_id": "auto-a", "priority": 1, "state": "planned"}
  ],
  "target": {"name": "v2 launch", "definition_of_done": "all arcs done"},
  "lane_defaults": {"lane": "claude", "backend": "claude-code", "model": "sonnet"},
  "weekly_share": 40,
  "branch_retention_days": 14,
  "major_trigger": "milestone"
}`

func TestProjectRecord_RoundTrip(t *testing.T) {
	s := bl90Server(t)
	if rr := recordDo(t, s, http.MethodPost, "/api/projects", recordFullBody); rr.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr := recordDo(t, s, http.MethodGet, "/api/projects/acme", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got projectListItem
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Owner != "platform" || got.Repository != "github.com/acme/app" || got.DefaultBranch != "main" {
		t.Errorf("scalar record fields = %+v", got)
	}
	if len(got.Arcs) != 2 || got.Arcs[0].AutomationID != "auto-b" || got.Arcs[0].State != config.ArcStateActive {
		t.Errorf("arcs = %+v", got.Arcs)
	}
	if got.Target == nil || got.Target.Name != "v2 launch" {
		t.Errorf("target = %+v", got.Target)
	}
	if got.LaneDefaults == nil || got.LaneDefaults.Model != "sonnet" {
		t.Errorf("lane_defaults = %+v", got.LaneDefaults)
	}
	if got.WeeklyShare == nil || *got.WeeklyShare != 40 {
		t.Errorf("weekly_share = %v", got.WeeklyShare)
	}
	if got.BranchRetentionDays == nil || *got.BranchRetentionDays != 14 {
		t.Errorf("branch_retention_days = %v", got.BranchRetentionDays)
	}
	if got.MajorTrigger != "milestone" {
		t.Errorf("major_trigger = %q", got.MajorTrigger)
	}

	// The list carries the same record.
	rr = recordDo(t, s, http.MethodGet, "/api/projects", "")
	var list []projectListItem
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].Owner != "platform" || len(list[0].Arcs) != 2 {
		t.Errorf("list record = %+v", list)
	}
}

func TestProjectRecord_ArcsPUTRoundTrip(t *testing.T) {
	s := bl90Server(t)
	recordDo(t, s, http.MethodPost, "/api/projects", `{"name":"acme","dir":"/srv/acme"}`)

	body := `[{"automation_id":"a1","priority":1,"state":"done"},{"automation_id":"a2","priority":2}]`
	rr := recordDo(t, s, http.MethodPut, "/api/projects/acme/arcs", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	var put projectListItem
	_ = json.Unmarshal(rr.Body.Bytes(), &put)
	if len(put.Arcs) != 2 || put.Arcs[0].AutomationID != "a1" || put.Arcs[0].State != config.ArcStateDone {
		t.Fatalf("PUT response arcs = %+v", put.Arcs)
	}

	// Stored in order and visible through GET.
	rr = recordDo(t, s, http.MethodGet, "/api/projects/acme", "")
	var got projectListItem
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Arcs) != 2 || got.Arcs[1].AutomationID != "a2" || got.Arcs[1].Priority != 2 {
		t.Errorf("stored arcs = %+v", got.Arcs)
	}

	// Wrapped form replaces the list; an empty list clears it.
	if rr := recordDo(t, s, http.MethodPut, "/api/projects/acme/arcs", `{"arcs":[{"automation_id":"only"}]}`); rr.Code != http.StatusOK {
		t.Fatalf("wrapped PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := recordDo(t, s, http.MethodPut, "/api/projects/acme/arcs", `[]`); rr.Code != http.StatusOK {
		t.Fatalf("empty PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := s.cfg.Projects["acme"].Arcs; len(got) != 0 {
		t.Errorf("arcs after empty PUT = %+v", got)
	}
}

func TestProjectRecord_InvalidInputRejected(t *testing.T) {
	s := bl90Server(t)
	recordDo(t, s, http.MethodPost, "/api/projects", `{"name":"acme","dir":"/srv/acme"}`)

	posts := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"unknown arc state", `{"name":"acme","dir":"/srv/acme","arcs":[{"automation_id":"a","state":"blocked"}]}`, "unknown state"},
		{"empty arc id", `{"name":"acme","dir":"/srv/acme","arcs":[{"priority":1}]}`, "automation_id is required"},
		{"duplicate arc id", `{"name":"acme","dir":"/srv/acme","arcs":[{"automation_id":"a"},{"automation_id":"a"}]}`, "duplicate automation_id"},
		{"weekly share above 100", `{"name":"acme","dir":"/srv/acme","weekly_share":101}`, "outside 0-100"},
		{"negative retention", `{"name":"acme","dir":"/srv/acme","branch_retention_days":-1}`, "negative"},
		{"unknown major trigger", `{"name":"acme","dir":"/srv/acme","major_trigger":"weekly"}`, "unknown major_trigger"},
		{"wrong type", `{"name":"acme","dir":"/srv/acme","weekly_share":"forty"}`, "bad request"},
	}
	for _, tc := range posts {
		t.Run("POST "+tc.name, func(t *testing.T) {
			rr := recordDo(t, s, http.MethodPost, "/api/projects", tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want 400 body=%s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tc.wantMsg) {
				t.Errorf("body %q does not mention %q", rr.Body.String(), tc.wantMsg)
			}
		})
	}

	// A rejected POST must not change the stored entry.
	if e := s.cfg.Projects["acme"]; e.WeeklyShare != nil || len(e.Arcs) != 0 {
		t.Errorf("rejected POST changed stored entry: %+v", e)
	}

	puts := []struct {
		name    string
		path    string
		body    string
		status  int
		wantMsg string
	}{
		{"unknown project", "/api/projects/nope/arcs", `[]`, http.StatusNotFound, "project not found"},
		{"malformed JSON", "/api/projects/acme/arcs", `[{"automation_id":`, http.StatusBadRequest, "bad request"},
		{"empty body", "/api/projects/acme/arcs", ``, http.StatusBadRequest, "arc list"},
		{"duplicate arc", "/api/projects/acme/arcs", `[{"automation_id":"x"},{"automation_id":"x"}]`, http.StatusBadRequest, "duplicate automation_id"},
		{"bad state", "/api/projects/acme/arcs", `[{"automation_id":"x","state":"paused"}]`, http.StatusBadRequest, "unknown state"},
	}
	for _, tc := range puts {
		t.Run("PUT "+tc.name, func(t *testing.T) {
			rr := recordDo(t, s, http.MethodPut, tc.path, tc.body)
			if rr.Code != tc.status {
				t.Fatalf("status=%d want %d body=%s", rr.Code, tc.status, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tc.wantMsg) {
				t.Errorf("body %q does not mention %q", rr.Body.String(), tc.wantMsg)
			}
		})
	}
	if e := s.cfg.Projects["acme"]; len(e.Arcs) != 0 {
		t.Errorf("rejected PUT changed stored arcs: %+v", e.Arcs)
	}
}

func TestProjectRecord_BackwardCompatibleBodies(t *testing.T) {
	s := bl90Server(t)

	// Old-style body with no record fields still creates the project.
	if rr := recordDo(t, s, http.MethodPost, "/api/projects", `{"name":"legacy","dir":"/srv/legacy","default_backend":"claude-code","description":"old"}`); rr.Code != http.StatusOK {
		t.Fatalf("legacy POST status=%d body=%s", rr.Code, rr.Body.String())
	}

	// A plain project's JSON carries none of the new keys.
	rr := recordDo(t, s, http.MethodGet, "/api/projects/legacy", "")
	var raw map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &raw)
	for _, key := range []string{"owner", "repository", "default_branch", "arcs", "target", "lane_defaults", "weekly_share", "branch_retention_days", "major_trigger"} {
		if _, present := raw[key]; present {
			t.Errorf("plain project JSON has unexpected key %q: %s", key, rr.Body.String())
		}
	}
	if raw["description"] != "old" || raw["default_backend"] != "claude-code" {
		t.Errorf("legacy fields lost: %s", rr.Body.String())
	}

	// Record fields set through the new API survive an old-style re-POST
	// that omits them, and the re-POST may still update the directory.
	recordDo(t, s, http.MethodPut, "/api/projects/legacy/arcs", `[{"automation_id":"keep-me","priority":1}]`)
	recordDo(t, s, http.MethodPost, "/api/projects", `{"name":"legacy","dir":"/srv/legacy2"}`)
	e := s.cfg.Projects["legacy"]
	if e.Dir != "/srv/legacy2" {
		t.Errorf("dir not updated: %q", e.Dir)
	}
	if len(e.Arcs) != 1 || e.Arcs[0].AutomationID != "keep-me" {
		t.Errorf("arcs cleared by old-style POST: %+v", e.Arcs)
	}

	// An explicit field in the body overrides the stored one.
	recordDo(t, s, http.MethodPost, "/api/projects", `{"name":"legacy","dir":"/srv/legacy2","owner":"new-owner"}`)
	if got := s.cfg.Projects["legacy"].Owner; got != "new-owner" {
		t.Errorf("owner = %q, want new-owner", got)
	}
}

func TestProjectRecord_DeleteStillWorks(t *testing.T) {
	s := bl90Server(t)
	recordDo(t, s, http.MethodPost, "/api/projects", recordFullBody)
	if rr := recordDo(t, s, http.MethodDelete, "/api/projects/acme", ""); rr.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := recordDo(t, s, http.MethodGet, "/api/projects/acme", ""); rr.Code != http.StatusNotFound {
		t.Errorf("get after delete status=%d", rr.Code)
	}
}

func TestProjectRecord_ArcsMethodNotAllowed(t *testing.T) {
	s := bl90Server(t)
	recordDo(t, s, http.MethodPost, "/api/projects", `{"name":"acme","dir":"/srv/acme"}`)
	if rr := recordDo(t, s, http.MethodPost, "/api/projects/acme/arcs", `[]`); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /arcs status=%d, want 405", rr.Code)
	}
}
