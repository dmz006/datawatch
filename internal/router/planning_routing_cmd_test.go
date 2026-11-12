package router

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

const planningRoutingCommView = `{"enabled":true,"shadow":false,` +
	`"rules":[{"name":"sec","match":{},"planners":[{"backend":"claude-code","model":"opus"},{"backend":"pi-prompt","model":"qwen"}],"ladder":"strict"}],` +
	`"ladders":{"default":{"touch_up_max":1,"replan_max":1,"escalate_after_attempts":2},"strict":{"touch_up_max":2,"replan_max":1,"escalate_after_attempts":3}},` +
	`"classes":{"design":{},"security":{}},"validation":{"valid":true}}`

func TestPlanningRoutingCommShow(t *testing.T) {
	if !slices.Contains(PlanningRoutingCommVerbs(), "planning-routing") {
		t.Fatalf("PlanningRoutingCommVerbs() = %v", PlanningRoutingCommVerbs())
	}
	if c := Parse("autonomous planning-routing show"); c.Type != CmdAutonomous {
		t.Errorf("Parse type = %v, want CmdAutonomous", c.Type)
	}
	log, port := laneFillCommDaemon(t, http.StatusOK, planningRoutingCommView)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous planning-routing show"), "\n")
	if log.path != "/api/autonomous/planning-routing" || log.calls != 1 {
		t.Errorf("path=%s calls=%d", log.path, log.calls)
	}
	for _, want := range []string{
		"Planning routing: enabled=true shadow=false",
		"Config: valid",
		"Rule 1 sec: planners=claude-code/opus,pi-prompt/qwen ladder=strict",
		"Ladder strict: touch_up_max=2 replan_max=1 escalate_after_attempts=3",
		"Classes: design, security",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
	// The underscore alias answers the same read.
	log, port = laneFillCommDaemon(t, http.StatusOK, planningRoutingCommView)
	r = setupPeerRouter(t, port)
	_ = r.HandleTestMessage("autonomous planning_routing show")
	if log.path != "/api/autonomous/planning-routing" {
		t.Errorf("alias path = %s", log.path)
	}
}

func TestPlanningRoutingCommShowInvalidAndEmpty(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK,
		`{"enabled":false,"shadow":false,"rules":[],"ladders":{},"classes":{},"validation":{"valid":false,"error":"duplicate rule name"}}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous planning-routing show"), "\n")
	for _, want := range []string{"enabled=false", "Config: INVALID: duplicate rule name", "Rules: none"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestPlanningRoutingCommTestSendsInputAndRendersDecision(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK,
		`{"enabled":true,"rule":"sec","classes":["security"],"planners":[{"backend":"claude-code","model":"opus"},{"backend":"pi-prompt","model":"qwen"}],`+
			`"ladder":"strict","shadow":false,"fallback":false,"reason":"rule-matched"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous planning-routing test kind=feature tags=security,ui spec_bytes=900 files=a.go,b.go project=p"), "\n")
	if log.path != "/api/autonomous/planning-routing/test" {
		t.Errorf("path = %s", log.path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(log.body), &body); err != nil {
		t.Fatal(err)
	}
	if body["kind"] != "feature" || body["project"] != "p" || body["spec_bytes"] != float64(900) {
		t.Errorf("body = %v", body)
	}
	if tags, _ := body["tags"].([]any); len(tags) != 2 {
		t.Errorf("tags = %v", body["tags"])
	}
	for _, want := range []string{
		"Routing enabled: true",
		"Reason: rule-matched",
		"Planners: claude-code/opus -> pi-prompt/qwen",
		"Ladder: strict",
		"Rule: sec",
		"Classes: security",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestPlanningRoutingCommTestFallbackAndShadow(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK,
		`{"enabled":false,"rule":"","classes":[],"planners":[],"ladder":"","shadow":false,"fallback":true,"reason":"routing-disabled"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous planning-routing test"), "\n")
	for _, want := range []string{"Routing enabled: false", "Reason: routing-disabled", "global planning backend and model (fallback)"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
	_, port = laneFillCommDaemon(t, http.StatusOK,
		`{"enabled":true,"rule":"sec","classes":[],"planners":[{"backend":"b","model":"m"}],"ladder":"default","shadow":true,"fallback":false,"reason":"rule-matched"}`)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous planning-routing test kind=bug"), "\n")
	if !strings.Contains(got, "Shadow: decision recorded only") {
		t.Errorf("shadow decision must say so: %q", got)
	}
}

func TestPlanningRoutingCommTestRejectsBadArgs(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{}`)
	r := setupPeerRouter(t, port)
	for _, msg := range []string{
		"autonomous planning-routing test kind",
		"autonomous planning-routing test colour=red",
		"autonomous planning-routing test spec_bytes=abc",
		"autonomous planning-routing test spec_bytes=-4",
	} {
		got := strings.Join(r.HandleTestMessage(msg), "\n")
		if !strings.Contains(got, "failed") {
			t.Errorf("%q must fail: %q", msg, got)
		}
	}
	if log.calls != 0 {
		t.Errorf("a bad argument must not reach the daemon, got %d calls", log.calls)
	}
}

func TestPlanningRoutingCommScorecard(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK,
		`{"window":"36h0m0s","report":"Planning gate report (window 36h0m0s)\ndecomposes=3 first_attempt=2\n"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous planning-routing scorecard 36h"), "\n")
	if log.path != "/api/autonomous/planning-routing/scorecard" || log.query != "window=36h" {
		t.Errorf("path=%s query=%q", log.path, log.query)
	}
	if !strings.Contains(got, "Planning gate report (window 36h0m0s)") || !strings.Contains(got, "decomposes=3") {
		t.Errorf("reply = %q", got)
	}
	log, port = laneFillCommDaemon(t, http.StatusOK, `{"report":"x"}`)
	r = setupPeerRouter(t, port)
	_ = r.HandleTestMessage("autonomous planning-routing scorecard")
	if log.query != "" {
		t.Errorf("no window must send no query, got %q", log.query)
	}
}

func TestPlanningRoutingCommUsageAndErrors(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusBadRequest, "window \"x\": use a duration")
	r := setupPeerRouter(t, port)
	for _, msg := range []string{"autonomous planning-routing", "autonomous planning-routing bogus", "autonomous planning-routing scorecard a b"} {
		got := strings.Join(r.HandleTestMessage(msg), "\n")
		if !strings.Contains(got, "usage: autonomous planning-routing show") {
			t.Errorf("%q must show usage: %q", msg, got)
		}
	}
	if log.calls != 0 {
		t.Errorf("usage errors must not reach the daemon: %d", log.calls)
	}
	// A daemon error comes back in the reply.
	got := strings.Join(r.HandleTestMessage("autonomous planning-routing scorecard x"), "\n")
	if !strings.Contains(got, "failed") || !strings.Contains(got, "HTTP 400") {
		t.Errorf("daemon rejection must surface: %q", got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous planning-routing show"), "\n")
	if !strings.Contains(got, "failed") {
		t.Errorf("show over a rejected read must fail: %q", got)
	}
}
