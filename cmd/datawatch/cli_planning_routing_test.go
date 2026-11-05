package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const planningRoutingCLIView = `{"enabled":true,"shadow":false,` +
	`"rules":[{"name":"sec","match":{},"planners":[{"backend":"claude-code","model":"opus"}],"ladder":"strict"}],` +
	`"ladders":{"default":{"touch_up_max":1,"replan_max":1,"escalate_after_attempts":2},"strict":{"touch_up_max":2,"replan_max":1,"escalate_after_attempts":3}},` +
	`"classes":{"security":{}},"validation":{"valid":true}}`

func TestPlanningRoutingCLIRegisteredUnderAutonomous(t *testing.T) {
	var group string
	for _, c := range newAutonomousCmd().Commands() {
		if c.Name() == "planning-routing" {
			group = c.Name()
			names := map[string]bool{}
			for _, sub := range c.Commands() {
				names[sub.Name()] = true
			}
			for _, want := range []string{"get", "set", "test", "scorecard"} {
				if !names[want] {
					t.Errorf("planning-routing lacks the %s subcommand", want)
				}
			}
		}
	}
	if group == "" {
		t.Error("planning-routing is not registered under autonomous")
	}
}

func TestPlanningRoutingCLIGetPrintsConfig(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, planningRoutingCLIView)
	out, err := runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), []string{"get"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/planning-routing" {
		t.Errorf("path = %s", d.path)
	}
	for _, want := range []string{
		"Planning routing: enabled=true shadow=false",
		"Config: valid",
		"Rule 1 sec: planners=claude-code/opus ladder=strict",
		"Ladder strict: touch_up_max=2",
		"Classes: security",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestPlanningRoutingCLIGetJSONIsUntouched(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, planningRoutingCLIView)
	out, err := runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), []string{"get", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != planningRoutingCLIView {
		t.Errorf("--json must print the body untouched, got %q", out)
	}
}

func TestPlanningRoutingCLIGetDaemonError(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	_, err := runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), []string{"get"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Errorf("err = %v, want the daemon's 503", err)
	}
}

// planningRoutingSetDaemon answers GET with the current view and records the PUT.
func planningRoutingSetDaemon(t *testing.T) (put *map[string]any, method *string) {
	t.Helper()
	got := map[string]any{}
	m := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			m = r.Method
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &got)
		}
		_, _ = w.Write([]byte(planningRoutingCLIView))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatal(err)
	}
	return &got, &m
}

func TestPlanningRoutingCLISetMergesFlagsOntoCurrent(t *testing.T) {
	put, method := planningRoutingSetDaemon(t)
	out, err := runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), []string{"set", "--enabled=false", "--shadow=true"})
	if err != nil {
		t.Fatal(err)
	}
	if *method != http.MethodPut {
		t.Fatal("set must PUT")
	}
	if (*put)["enabled"] != false || (*put)["shadow"] != true {
		t.Errorf("flags not sent: %v", *put)
	}
	if rs, _ := (*put)["rules"].([]any); len(rs) != 1 {
		t.Errorf("a field left out must keep its current value (rules): %v", (*put)["rules"])
	}
	if _, ok := (*put)["validation"]; ok {
		t.Error("the validation member must not be sent back")
	}
	if !strings.Contains(out, "Planning routing:") {
		t.Errorf("set must print the resulting config:\n%s", out)
	}
}

func TestPlanningRoutingCLISetRulesFileAndJSONFlags(t *testing.T) {
	put, _ := planningRoutingSetDaemon(t)
	f := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(f, []byte(`[{"name":"x","planners":[{"backend":"b","model":"m"}]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), []string{"set", "--rules-file", f, "--ladders", `{"l":{"touch_up_max":3}}`, "--classes", `{"c":{"tags":["t"]}}`})
	if err != nil {
		t.Fatal(err)
	}
	rs, _ := (*put)["rules"].([]any)
	if len(rs) != 1 || rs[0].(map[string]any)["name"] != "x" {
		t.Errorf("rules-file not sent: %v", (*put)["rules"])
	}
	if l, _ := (*put)["ladders"].(map[string]any); l["l"] == nil {
		t.Errorf("ladders not sent: %v", (*put)["ladders"])
	}
	if c, _ := (*put)["classes"].(map[string]any); c["c"] == nil {
		t.Errorf("classes not sent: %v", (*put)["classes"])
	}
	if (*put)["enabled"] != true {
		t.Errorf("enabled must keep its current value true: %v", (*put)["enabled"])
	}
}

func TestPlanningRoutingCLISetRejectsBadInput(t *testing.T) {
	planningRoutingSetDaemon(t)
	for name, argv := range map[string][]string{
		"rules not json":   {"set", "--rules", "{"},
		"ladders not json": {"set", "--ladders", "x"},
		"missing file":     {"set", "--rules-file", "/nonexistent/rules.json"},
	} {
		if _, err := runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), argv); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestPlanningRoutingCLISetShowsDaemonRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			http.Error(w, `planning_routing: rule "x" has no planners`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(planningRoutingCLIView))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatal(err)
	}
	_, err := runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), []string{"set", "--enabled"})
	if err == nil || !strings.Contains(err.Error(), "no planners") {
		t.Errorf("err = %v, want the validation message", err)
	}
}

func TestPlanningRoutingCLITestSendsFlagsAndPrintsDecision(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK,
		`{"enabled":true,"rule":"sec","classes":["security"],"planners":[{"backend":"claude-code","model":"opus"}],"ladder":"strict","shadow":false,"fallback":false,"reason":"rule-matched"}`)
	out, err := runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), []string{
		"test", "--kind", "feature", "--tags", "security,ui", "--spec-bytes", "900", "--files", "a.go,b.go", "--project", "p"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/planning-routing/test" {
		t.Errorf("path = %s", d.path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(d.body), &body); err != nil {
		t.Fatal(err)
	}
	if body["kind"] != "feature" || body["project"] != "p" || body["spec_bytes"] != float64(900) {
		t.Errorf("body = %v", body)
	}
	if tags, _ := body["tags"].([]any); len(tags) != 2 {
		t.Errorf("tags = %v", body["tags"])
	}
	for _, want := range []string{"Routing enabled: true", "Reason: rule-matched", "Planners: claude-code/opus", "Ladder: strict", "Rule: sec", "Classes: security"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestPlanningRoutingCLITestDisabledFallbackAndJSON(t *testing.T) {
	reply := `{"enabled":false,"rule":"","classes":[],"planners":[],"ladder":"","shadow":false,"fallback":true,"reason":"routing-disabled"}`
	d := newLaneFillCLIDaemon(t, http.StatusOK, reply)
	out, err := runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Routing enabled: false") || !strings.Contains(out, "(fallback)") {
		t.Errorf("output:\n%s", out)
	}
	if !strings.Contains(d.body, `"tags":[]`) || !strings.Contains(d.body, `"files":[]`) {
		t.Errorf("empty lists must go out as []: %s", d.body)
	}
	out, err = runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), []string{"test", "--json"})
	if err != nil || out != reply {
		t.Errorf("--json must print the body untouched: %q, %v", out, err)
	}
}

func TestPlanningRoutingCLIScorecard(t *testing.T) {
	reply := `{"window":"36h0m0s","report":"Planning gate report (window 36h0m0s)\ndecomposes=3 first_attempt=2\n"}`
	d := newLaneFillCLIDaemon(t, http.StatusOK, reply)
	out, err := runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), []string{"scorecard", "--window", "36h"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/planning-routing/scorecard" || d.query != "window=36h" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	if !strings.Contains(out, "Planning gate report (window 36h0m0s)") || !strings.Contains(out, "decomposes=3") {
		t.Errorf("output:\n%s", out)
	}
	out, err = runLaneFillCmd(t, newAutonomousPlanningRoutingCmd(), []string{"scorecard", "--json"})
	if err != nil || out != reply {
		t.Errorf("--json must print the body untouched: %q, %v", out, err)
	}
	if d.query != "" {
		t.Errorf("no --window must send no query, got %q", d.query)
	}
}
