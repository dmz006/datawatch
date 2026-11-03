// Parser + loopback tests for the `project policy` comm-channel command.

package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectPolicyParseBare(t *testing.T) {
	c := Parse("project")
	if c.Type != CmdProject {
		t.Fatalf("type=%v want %v", c.Type, CmdProject)
	}
	if c.Text != "" {
		t.Errorf("text=%q want empty", c.Text)
	}
}

func TestProjectPolicyParseName(t *testing.T) {
	c := Parse("project policy acme")
	if c.Type != CmdProject {
		t.Fatalf("type=%v", c.Type)
	}
	if c.Text != "policy acme" {
		t.Errorf("text=%q want %q", c.Text, "policy acme")
	}
}

func TestProjectPolicyParseNameAndAutomaton(t *testing.T) {
	c := Parse("project policy acme prd-1234")
	if c.Type != CmdProject || c.Text != "policy acme prd-1234" {
		t.Fatalf("got %+v", c)
	}
}

func TestProjectPolicyHandlerLoopbackUnconfigured(t *testing.T) {
	r := &Router{hostname: "h"}
	out := captureSends(r, func() {
		r.handleProjectCmd(Command{Type: CmdProject, Text: "policy acme"})
	})
	if !strings.Contains(out, "REST loopback not configured") {
		t.Errorf("expected loopback failure reply, got:\n%s", out)
	}
}

func TestProjectPolicyHandlerUsage(t *testing.T) {
	r := &Router{hostname: "h"}
	out := captureSends(r, func() {
		r.handleProjectCmd(Command{Type: CmdProject, Text: ""})
		r.handleProjectCmd(Command{Type: CmdProject, Text: "frobnicate acme"})
		r.handleProjectCmd(Command{Type: CmdProject, Text: "policy"})
	})
	for _, want := range []string{
		"Usage: project policy",
		`Unknown project verb "frobnicate"`,
		"Missing project name",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestProjectPolicyHandlerFullMessage(t *testing.T) {
	body := `{
	  "project": "acme",
	  "automaton": "prd-1234",
	  "policy": {
	    "plan_review_mode": {"value": "block", "source": "automaton"},
	    "max_concurrent_tasks": {"value": 2, "source": "project"},
	    "uncommitted_work_policy": {"value": null, "source": "daemon"},
	    "retry_policy": {"value": {"infra": 3, "code": 1}, "source": "daemon"}
	  }
	}`
	gotAutomaton := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/projects/acme/policy/effective", func(w http.ResponseWriter, r *http.Request) {
		if a := r.URL.Query().Get("automaton"); a != "" {
			gotAutomaton = true
			if a != "prd-1234" {
				t.Errorf("automaton query=%q want prd-1234", a)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	router := loopbackRouter(t, srv)
	out := strings.Join(router.HandleTestMessage("project policy acme prd-1234"), "\n")
	if !gotAutomaton {
		t.Errorf("automaton query parameter was not forwarded")
	}
	for _, want := range []string{
		"Effective policy for project acme",
		"with Automaton prd-1234 overrides",
		"plan_review_mode",
		"block  (automaton)",
		"max_concurrent_tasks",
		"2  (project)",
		"uncommitted_work_policy",
		"<unset>  (daemon)",
		"retry_policy",
		"map[code:1 infra:3]  (daemon)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
