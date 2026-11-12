// Tests for the failure escalation MCP tools: set_failure_escalation and
// resolve_task_review. Each handler proxies one REST call; these check the
// route, the body it sends and the refusals it makes before proxying.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestFailureEscalationToolsNames(t *testing.T) {
	s := &Server{}
	set := s.toolAutonomousPRDSetFailureEscalation()
	if set.Name != "autonomous_prd_set_failure_escalation" {
		t.Errorf("set tool name = %q", set.Name)
	}
	if !contains(set.InputSchema.Required, "id") {
		t.Errorf("set tool must require id, required = %v", set.InputSchema.Required)
	}
	resolve := s.toolAutonomousPRDResolveTaskReview()
	if resolve.Name != "autonomous_prd_resolve_task_review" {
		t.Errorf("resolve tool name = %q", resolve.Name)
	}
	for _, want := range []string{"id", "task_id", "action"} {
		if !contains(resolve.InputSchema.Required, want) {
			t.Errorf("resolve tool must require %s, required = %v", want, resolve.InputSchema.Required)
		}
	}
}

func TestFailureEscalationToolsSetProxiesPolicy(t *testing.T) {
	var c subUpdateCapture
	s := subUpdateServer(t, &c, http.StatusOK, `{"id":"p1"}`)
	req := subUpdateReq("autonomous_prd_set_failure_escalation", map[string]any{
		"id":     "p1",
		"policy": map[string]any{"max_failures": float64(4)},
	})
	if _, err := s.handleAutonomousPRDSetFailureEscalation(context.Background(), req); err != nil {
		t.Fatalf("set: %v", err)
	}
	if c.method != http.MethodPost || c.path != "/api/autonomous/prds/p1/set_failure_escalation" {
		t.Fatalf("request = %s %s", c.method, c.path)
	}
	policy, _ := c.body["policy"].(map[string]any)
	if policy["max_failures"] != float64(4) {
		t.Errorf("policy = %v", c.body["policy"])
	}
	if c.body["actor"] != "operator" {
		t.Errorf("actor = %v", c.body["actor"])
	}
}

func TestFailureEscalationToolsClearSendsNullPolicy(t *testing.T) {
	var c subUpdateCapture
	s := subUpdateServer(t, &c, http.StatusOK, `{"id":"p1"}`)
	req := subUpdateReq("autonomous_prd_set_failure_escalation", map[string]any{
		"id":      "p1",
		"task_id": "t2",
		"clear":   true,
		"policy":  map[string]any{"max_failures": float64(9)},
	})
	if _, err := s.handleAutonomousPRDSetFailureEscalation(context.Background(), req); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if p, ok := c.body["policy"]; !ok || p != nil {
		t.Fatalf("clear must send policy null even when a policy is given, body = %v", c.body)
	}
	if c.body["task_id"] != "t2" {
		t.Errorf("task_id = %v", c.body["task_id"])
	}
}

func TestFailureEscalationToolsSetRequiresPolicyOrClear(t *testing.T) {
	var c subUpdateCapture
	s := subUpdateServer(t, &c, http.StatusOK, `{}`)
	req := subUpdateReq("autonomous_prd_set_failure_escalation", map[string]any{"id": "p1"})
	if _, err := s.handleAutonomousPRDSetFailureEscalation(context.Background(), req); err == nil {
		t.Fatal("a set with neither policy nor clear must be refused")
	}
	if c.calls != 0 {
		t.Fatalf("a refused set reached the daemon %d times", c.calls)
	}
}

func TestFailureEscalationToolsResolveRetryProxiesChanges(t *testing.T) {
	var c subUpdateCapture
	s := subUpdateServer(t, &c, http.StatusOK, `{"id":"p1"}`)
	req := subUpdateReq("autonomous_prd_resolve_task_review", map[string]any{
		"id":      "p1",
		"task_id": "t1",
		"action":  "retry",
		"model":   "claude-opus-5-5",
		"files":   []any{"a.go", "b.go"},
		"note":    "narrow it",
	})
	if _, err := s.handleAutonomousPRDResolveTaskReview(context.Background(), req); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if c.path != "/api/autonomous/prds/p1/resolve_task_review" {
		t.Fatalf("path = %s", c.path)
	}
	if c.body["action"] != "retry" || c.body["model"] != "claude-opus-5-5" || c.body["note"] != "narrow it" {
		t.Errorf("body = %v", c.body)
	}
	files, _ := c.body["files"].([]any)
	if len(files) != 2 || files[0] != "a.go" {
		t.Errorf("files = %v", c.body["files"])
	}
}

func TestFailureEscalationToolsResolveNotHeldIsAnError(t *testing.T) {
	var c subUpdateCapture
	s := subUpdateServer(t, &c, http.StatusConflict, "task is not awaiting review")
	req := subUpdateReq("autonomous_prd_resolve_task_review", map[string]any{
		"id": "p1", "task_id": "t1", "action": "cancel",
	})
	if _, err := s.handleAutonomousPRDResolveTaskReview(context.Background(), req); err == nil {
		t.Fatal("a 409 from the daemon must reach the caller as an error")
	}
}

// TestFailureEscalationToolsResolveWhileAutomatonRunning: a retry on a held
// task goes through when another task of the Automaton is running, and the
// updated Automaton the daemon answers with reaches the caller as it is.
func TestFailureEscalationToolsResolveWhileAutomatonRunning(t *testing.T) {
	var c subUpdateCapture
	reply := `{"id":"p1","status":"running","stories":[{"id":"s1","tasks":[{"id":"t1","status":"pending"},{"id":"t2","status":"in_progress"}]}]}`
	s := subUpdateServer(t, &c, http.StatusOK, reply)
	req := subUpdateReq("autonomous_prd_resolve_task_review", map[string]any{
		"id": "p1", "task_id": "t1", "action": "retry", "note": "go again",
	})
	res, err := s.handleAutonomousPRDResolveTaskReview(context.Background(), req)
	if err != nil {
		t.Fatalf("retry while another task runs: %v", err)
	}
	if c.calls != 1 || c.path != "/api/autonomous/prds/p1/resolve_task_review" {
		t.Fatalf("daemon calls = %d, path = %s", c.calls, c.path)
	}
	if c.body["task_id"] != "t1" || c.body["action"] != "retry" || c.body["note"] != "go again" {
		t.Errorf("body = %v", c.body)
	}
	if res == nil || res.IsError || len(res.Content) != 1 {
		t.Fatalf("result = %+v, want one non-error content item", res)
	}
	text, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content = %T, want text", res.Content[0])
	}
	var got struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Stories []struct {
			Tasks []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"tasks"`
		} `json:"stories"`
	}
	if err := json.Unmarshal([]byte(text.Text), &got); err != nil {
		t.Fatalf("result is not the Automaton: %v: %s", err, text.Text)
	}
	if got.ID != "p1" || got.Status != "running" || len(got.Stories) != 1 || len(got.Stories[0].Tasks) != 2 ||
		got.Stories[0].Tasks[0].Status != "pending" || got.Stories[0].Tasks[1].Status != "in_progress" {
		t.Errorf("result Automaton = %+v", got)
	}
}

// TestReviewRetryReadOnlyToolProxiesFields: the resolve tool declares read_only
// and report_path, sends them to the daemon, and leaves read_only out when the
// caller did not pass it (so the daemon leaves the mark unchanged).
func TestReviewRetryReadOnlyToolProxiesFields(t *testing.T) {
	var c subUpdateCapture
	s := subUpdateServer(t, &c, http.StatusOK, `{"id":"p1"}`)
	props := s.toolAutonomousPRDResolveTaskReview().InputSchema.Properties
	for _, k := range []string{"read_only", "report_path"} {
		if _, ok := props[k]; !ok {
			t.Errorf("resolve tool has no %s parameter", k)
		}
	}
	req := subUpdateReq("autonomous_prd_resolve_task_review", map[string]any{
		"id": "p1", "task_id": "t1", "action": "retry",
		"read_only": true, "report_path": ".datawatch/reports/r.md",
	})
	if _, err := s.handleAutonomousPRDResolveTaskReview(context.Background(), req); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if c.body["read_only"] != true || c.body["report_path"] != ".datawatch/reports/r.md" {
		t.Errorf("body = %v", c.body)
	}
	// A fresh capture: decoding into the old one would keep its keys.
	var c2 subUpdateCapture
	s = subUpdateServer(t, &c2, http.StatusOK, `{"id":"p1"}`)
	req = subUpdateReq("autonomous_prd_resolve_task_review", map[string]any{"id": "p1", "task_id": "t1", "action": "retry"})
	if _, err := s.handleAutonomousPRDResolveTaskReview(context.Background(), req); err != nil {
		t.Fatalf("retry without the fields: %v", err)
	}
	if _, present := c2.body["read_only"]; present {
		t.Errorf("read_only sent although not passed: %v", c2.body)
	}
	if _, present := c2.body["report_path"]; present {
		t.Errorf("report_path sent although not passed: %v", c2.body)
	}
}

// TestHeldTaskEditToolProxiesFields: the edit tool carries read_only and
// report_path for a held task, and its description names the held-task case.
func TestHeldTaskEditToolProxiesFields(t *testing.T) {
	var c subUpdateCapture
	s := subUpdateServer(t, &c, http.StatusOK, `{"id":"p1"}`)
	if d := s.toolAutonomousPRDEditTask().Description; !strings.Contains(d, "review_needed") {
		t.Errorf("edit tool description does not mention held tasks: %s", d)
	}
	req := subUpdateReq("autonomous_prd_edit_task", map[string]any{
		"id": "p1", "task_id": "t1", "new_spec": "new",
		"read_only": true, "report_path": ".datawatch/reports/r.md",
	})
	if _, err := s.handleAutonomousPRDEditTask(context.Background(), req); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if c.path != "/api/autonomous/prds/p1/edit_task" || c.body["read_only"] != true || c.body["report_path"] != ".datawatch/reports/r.md" || c.body["new_spec"] != "new" {
		t.Errorf("path=%s body=%v", c.path, c.body)
	}
}
