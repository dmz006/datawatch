// Parser, formatter and loopback tests for the `project show|arcs|set|progress`
// comm-channel verbs.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const projectRecordBody = `{
  "name": "acme",
  "dir": "/work/acme",
  "default_backend": "claude-code",
  "description": "Acme platform",
  "policy": {"max_concurrent_tasks": 2},
  "owner": "platform",
  "default_branch": "main",
  "arcs": [
    {"automation_id": "auto-1", "priority": 1, "state": "active"},
    {"automation_id": "auto-2", "priority": 2}
  ],
  "target": {"name": "v1.0", "definition_of_done": "all arcs done"},
  "weekly_share": 20
}`

const projectRollupBody = `{
  "project": "acme",
  "window_days": 7,
  "points_priced": true,
  "arcs": [
    {"automation_id": "auto-1", "state": "active",
     "automata": [{"id": "prd-1"}, {"id": "prd-2"}],
     "tasks": {"done": 4, "total": 10}, "points": 3.5,
     "throughput": {"automata_finished": 1, "tasks_finished": 2, "automata_per_day": 0.14, "tasks_per_day": 0.29}}
  ],
  "unassigned": {"automata": [{"id": "prd-9"}], "tasks": {"done": 0, "total": 3}, "points": 0},
  "total": {"automata": [{"id": "prd-1"}, {"id": "prd-2"}, {"id": "prd-9"}],
     "tasks": {"done": 4, "total": 13}, "points": 3.5,
     "throughput": {"automata_finished": 1, "tasks_finished": 2, "automata_per_day": 0.14, "tasks_per_day": 0.29}}
}`

func TestProjectRecordParseVerbs(t *testing.T) {
	for _, in := range []string{
		"project show acme",
		"project arcs acme",
		"project set acme owner platform",
		"project progress acme",
	} {
		c := Parse(in)
		if c.Type != CmdProject {
			t.Errorf("Parse(%q) type=%v want %v", in, c.Type, CmdProject)
		}
	}
	if c := Parse("project set acme owner platform team"); c.Text != "set acme owner platform team" {
		t.Errorf("text=%q", c.Text)
	}
}

func TestProjectRecordUnknownVerbStillRejected(t *testing.T) {
	r := &Router{hostname: "h"}
	out := captureSends(r, func() {
		r.handleProjectCmd(Command{Type: CmdProject, Text: "frobnicate acme"})
	})
	if !strings.Contains(out, `Unknown project verb "frobnicate"`) {
		t.Errorf("unknown verb not reported:\n%s", out)
	}
	if !strings.Contains(out, "project show|arcs|progress <project-name>") {
		t.Errorf("usage does not list record verbs:\n%s", out)
	}
}

func TestProjectRecordUsageWithoutName(t *testing.T) {
	r := &Router{hostname: "h"}
	out := captureSends(r, func() {
		for _, verb := range []string{"show", "arcs", "progress", "set"} {
			r.handleProjectCmd(Command{Type: CmdProject, Text: verb})
		}
		r.handleProjectCmd(Command{Type: CmdProject, Text: "set acme owner"})
		r.handleProjectCmd(Command{Type: CmdProject, Text: "set acme nope x"})
		r.handleProjectCmd(Command{Type: CmdProject, Text: "set acme weekly_share lots"})
	})
	for _, want := range []string{
		"Usage: project show <project-name>",
		"Usage: project arcs <project-name>",
		"Usage: project progress <project-name>",
		"Usage: project set <project-name> <field> <value>",
		`unknown field "nope"`,
		"weekly_share must be a whole number",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestProjectRecordLoopbackUnconfigured(t *testing.T) {
	r := &Router{hostname: "h"}
	out := captureSends(r, func() {
		r.handleProjectCmd(Command{Type: CmdProject, Text: "show acme"})
		r.handleProjectCmd(Command{Type: CmdProject, Text: "arcs acme"})
		r.handleProjectCmd(Command{Type: CmdProject, Text: "progress acme"})
		r.handleProjectCmd(Command{Type: CmdProject, Text: "set acme owner x"})
	})
	if n := strings.Count(out, "REST loopback not configured"); n != 4 {
		t.Errorf("want 4 loopback failures, got %d in:\n%s", n, out)
	}
}

func TestFormatProjectShow(t *testing.T) {
	out := formatProjectShow(projectRecordBody)
	for _, want := range []string{
		"Project acme",
		"dir:",
		"/work/acme",
		"owner:",
		"platform",
		"repository:",
		"-",
		"weekly_share:",
		"20",
		"arcs:",
		"2",
		"target:",
		"v1.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := formatProjectShow("not json"); got != "not json" {
		t.Errorf("non-JSON should pass through, got %q", got)
	}
}

func TestFormatProjectArcs(t *testing.T) {
	out := formatProjectArcs("acme", projectRecordBody)
	for _, want := range []string{
		"Arcs for project acme (2):",
		"1. auto-1 priority=1 state=active",
		"2. auto-2 priority=2 state=planned",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := formatProjectArcs("bare", `{"name":"bare"}`); got != "No arcs for project bare." {
		t.Errorf("empty arcs: %q", got)
	}
	if got := formatProjectArcs("acme", "nope"); got != "nope" {
		t.Errorf("non-JSON should pass through, got %q", got)
	}
}

func TestFormatProjectProgress(t *testing.T) {
	out := formatProjectProgress("acme", projectRollupBody)
	for _, want := range []string{
		"Progress for project acme (7-day window)",
		"total: 3 automata, tasks 4/13, points 3.5",
		"throughput: 1 automata, 2 tasks finished (0.14 automata/day, 0.29 tasks/day)",
		"finished means completed, or cancelled or archived with its work branch on the default branch, or shipped",
		"arc auto-1 [active]: 2 automata, tasks 4/10",
		"unassigned: 1 automata, tasks 0/3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	unpriced := strings.Replace(projectRollupBody, `"points_priced": true`, `"points_priced": false`, 1)
	if !strings.Contains(formatProjectProgress("acme", unpriced), "points n/a") {
		t.Errorf("unpriced roll-up should read points n/a")
	}
	if got := formatProjectProgress("acme", "nope"); got != "nope" {
		t.Errorf("non-JSON should pass through, got %q", got)
	}
}

func TestProjectSetBodyKeepsRecord(t *testing.T) {
	out, err := projectSetBody(projectRecordBody, "owner", "infra")
	if err != nil {
		t.Fatalf("projectSetBody: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got["owner"] != "infra" {
		t.Errorf("owner=%v want infra", got["owner"])
	}
	if got["dir"] != "/work/acme" || got["default_backend"] != "claude-code" || got["description"] != "Acme platform" {
		t.Errorf("stored fields not echoed: %v", got)
	}
	if arcs, _ := got["arcs"].([]any); len(arcs) != 2 {
		t.Errorf("arcs not echoed: %v", got["arcs"])
	}
	if _, err := projectSetBody(`{"name":"x"}`, "owner", "y"); err == nil {
		t.Errorf("record without dir should be refused")
	}
	if _, err := projectSetBody("nope", "owner", "y"); err == nil {
		t.Errorf("unreadable record should be refused")
	}
}

func TestProjectRecordHandlersLoopback(t *testing.T) {
	var posted map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/projects/acme", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(projectRecordBody))
	})
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &posted); err != nil {
			t.Errorf("POST body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","name":"acme"}`))
	})
	mux.HandleFunc("/api/projects/acme/rollup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(projectRollupBody))
	})
	mux.HandleFunc("/api/projects/ghost", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "project not found", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	router := loopbackRouter(t, srv)
	out := strings.Join(router.HandleTestMessage("project show acme"), "\n")
	if !strings.Contains(out, "Project acme") || !strings.Contains(out, "/work/acme") {
		t.Errorf("show reply:\n%s", out)
	}
	out = strings.Join(router.HandleTestMessage("project arcs acme"), "\n")
	if !strings.Contains(out, "auto-1 priority=1 state=active") {
		t.Errorf("arcs reply:\n%s", out)
	}
	out = strings.Join(router.HandleTestMessage("project progress acme"), "\n")
	if !strings.Contains(out, "Progress for project acme") {
		t.Errorf("progress reply:\n%s", out)
	}
	out = strings.Join(router.HandleTestMessage("project set acme owner infra team"), "\n")
	if !strings.Contains(out, "owner = infra team") {
		t.Errorf("set reply:\n%s", out)
	}
	if posted["owner"] != "infra team" || posted["dir"] != "/work/acme" {
		t.Errorf("POST body did not carry the change and stored dir: %v", posted)
	}
	out = strings.Join(router.HandleTestMessage("project show ghost"), "\n")
	if !strings.Contains(out, "HTTP 404") {
		t.Errorf("404 not surfaced:\n%s", out)
	}
}
