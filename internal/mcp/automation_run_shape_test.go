package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// MCP coverage for the Automation run-shape settings: max_parallel on
// automation_create and automation_update, the automation_set_max_parallel and
// automation_set_stage_gate tools, and the question argument of
// automation_insert_member. Each test serves the daemon side with a recording
// handler, so what is asserted is the REST request the tool sends.

// runShapeDaemon records the last request a tool sent to the daemon.
type runShapeDaemon struct {
	method, path string
	body         map[string]any
}

func newRunShapeServer(t *testing.T) (*Server, *runShapeDaemon) {
	t.Helper()
	d := &runShapeDaemon{}
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		d.method, d.path, d.body = r.Method, r.URL.Path, nil
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &d.body)
		_, _ = w.Write([]byte(`{"id":"a1","version":8}`))
	})
	return s, d
}

func TestAutomationMaxParallelMCP_CreateAndUpdateForwardTheLimit(t *testing.T) {
	s, d := newRunShapeServer(t)

	if _, err := s.handleAutomationCreate(context.Background(), callReq(map[string]any{"name": "shaped", "max_parallel": 3})); err != nil {
		t.Fatal(err)
	}
	if d.method != http.MethodPost || d.path != "/api/automations" || d.body["max_parallel"] != float64(3) {
		t.Fatalf("create: %s %s %v", d.method, d.path, d.body)
	}
	if _, err := s.handleAutomationCreate(context.Background(), callReq(map[string]any{"name": "plain"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.body["max_parallel"]; ok {
		t.Fatalf("omitted max_parallel must not be sent on create: %v", d.body)
	}

	// 0 is sent on update, so the limit can be cleared; omitted is not sent.
	if _, err := s.handleAutomationUpdate(context.Background(), callReq(map[string]any{"id": "a1", "max_parallel": 0})); err != nil {
		t.Fatal(err)
	}
	if v, ok := d.body["max_parallel"]; !ok || v != float64(0) {
		t.Fatalf("update: max_parallel = %v (sent %v), want 0 sent", v, ok)
	}
	if _, err := s.handleAutomationUpdate(context.Background(), callReq(map[string]any{"id": "a1", "name": "renamed"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.body["max_parallel"]; ok {
		t.Fatalf("omitted max_parallel must not be sent on update: %v", d.body)
	}
}

func TestAutomationMaxParallelMCP_ToolSchemasMentionTheNewFields(t *testing.T) {
	s := &Server{}
	for name, props := range map[string]map[string]any{
		"create": s.toolAutomationCreate().InputSchema.Properties,
		"update": s.toolAutomationUpdate().InputSchema.Properties,
	} {
		if _, ok := props["max_parallel"]; !ok {
			t.Errorf("automation_%s has no max_parallel argument", name)
		}
		if _, ok := props["stage_gate"]; !ok {
			t.Errorf("automation_%s has no stage_gate argument", name)
		}
	}
	if _, ok := s.toolAutomationInsertMember().InputSchema.Properties["question"]; !ok {
		t.Error("automation_insert_member has no question argument")
	}
	for _, want := range []string{"decision_wait", "question", "max_parallel"} {
		if !strings.Contains(automationMembersJSONDesc, want) {
			t.Errorf("members_json description lacks %q: %s", want, automationMembersJSONDesc)
		}
	}
}

func TestAutomationSetMaxParallelMCP(t *testing.T) {
	s, d := newRunShapeServer(t)

	res, err := s.handleAutomationSetMaxParallel(context.Background(), callReq(map[string]any{
		"automation_id": "a1", "expected_version": float64(7), "max_parallel": float64(3), "actor": "alice", "reason": "lanes freed",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if d.method != http.MethodPost || d.path != "/api/automations/a1/set_max_parallel" {
		t.Fatalf("request = %s %s", d.method, d.path)
	}
	if d.body["max_parallel"] != float64(3) || d.body["expected_version"] != float64(7) || d.body["actor"] != "alice" || d.body["reason"] != "lanes freed" {
		t.Fatalf("body = %v", d.body)
	}
	if _, ok := d.body["member_id"]; ok {
		t.Fatalf("member_id sent without being given: %v", d.body)
	}
	if !strings.Contains(toolText(t, res), `"version":8`) {
		t.Fatalf("result = %s", toolText(t, res))
	}

	// 0 clears and is sent; member_id names a fork.
	if _, err := s.handleAutomationSetMaxParallel(context.Background(), callReq(map[string]any{
		"automation_id": "a1", "expected_version": float64(8), "max_parallel": float64(0), "member_id": "split",
	})); err != nil {
		t.Fatal(err)
	}
	if v, ok := d.body["max_parallel"]; !ok || v != float64(0) || d.body["member_id"] != "split" {
		t.Fatalf("clear: body = %v", d.body)
	}

	// Refusals never reach the daemon.
	d.path = ""
	for name, args := range map[string]map[string]any{
		"missing":      {"automation_id": "a1", "expected_version": float64(1)},
		"fractional":   {"automation_id": "a1", "expected_version": float64(1), "max_parallel": 1.5},
		"negative":     {"automation_id": "a1", "expected_version": float64(1), "max_parallel": float64(-1)},
		"text":         {"automation_id": "a1", "expected_version": float64(1), "max_parallel": "three"},
		"no id":        {"expected_version": float64(1), "max_parallel": float64(2)},
		"no version":   {"automation_id": "a1", "max_parallel": float64(2)},
		"null limit":   {"automation_id": "a1", "expected_version": float64(1), "max_parallel": nil},
		"huge-ish nan": {"automation_id": "a1", "expected_version": float64(1), "max_parallel": "NaN"},
	} {
		res, err := s.handleAutomationSetMaxParallel(context.Background(), callReq(args))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out := toolText(t, res); !strings.Contains(out, "error:") {
			t.Errorf("%s: result = %s, want an error", name, out)
		}
		if d.path != "" {
			t.Errorf("%s: reached the daemon (%s)", name, d.path)
		}
	}
}

func TestAutomationSetStageGateMCP(t *testing.T) {
	s, d := newRunShapeServer(t)

	for _, on := range []bool{true, false} {
		if _, err := s.handleAutomationSetStageGate(context.Background(), callReq(map[string]any{
			"automation_id": "a1", "expected_version": float64(4), "stage_gate": on,
		})); err != nil {
			t.Fatal(err)
		}
		if d.method != http.MethodPost || d.path != "/api/automations/a1/set_stage_gate" {
			t.Fatalf("request = %s %s", d.method, d.path)
		}
		if v, ok := d.body["stage_gate"]; !ok || v != on {
			t.Fatalf("stage_gate = %v (sent %v), want %v sent", v, ok, on)
		}
	}

	d.path = ""
	for name, args := range map[string]map[string]any{
		"missing": {"automation_id": "a1", "expected_version": float64(1)},
		"text":    {"automation_id": "a1", "expected_version": float64(1), "stage_gate": "yes"},
		"null":    {"automation_id": "a1", "expected_version": float64(1), "stage_gate": nil},
	} {
		res, err := s.handleAutomationSetStageGate(context.Background(), callReq(args))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out := toolText(t, res); !strings.Contains(out, "stage_gate is required") {
			t.Errorf("%s: result = %s", name, out)
		}
		if d.path != "" {
			t.Errorf("%s: reached the daemon (%s)", name, d.path)
		}
	}
}

func TestAutomationInsertMemberMCP_Question(t *testing.T) {
	s, d := newRunShapeServer(t)

	if _, err := s.handleAutomationInsertMember(context.Background(), callReq(map[string]any{
		"automation_id": "a1", "expected_version": float64(5), "reason": "sign-off",
		"question": "  Ship it now?  ", "stage": "release", "after": "m1",
	})); err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/automations/a1/insert_member" {
		t.Fatalf("path = %s", d.path)
	}
	member, _ := d.body["member"].(map[string]any)
	if member["kind"] != "decision_wait" || member["question"] != "Ship it now?" || member["stage"] != "release" {
		t.Fatalf("member = %v", member)
	}
	if _, ok := member["prd_id"]; ok {
		t.Fatalf("a decision wait carries no prd_id: %v", member)
	}
	if d.body["after"] != "m1" || d.body["reason"] != "sign-off" {
		t.Fatalf("body = %v", d.body)
	}

	// The Automaton and pending item forms are unchanged.
	if _, err := s.handleAutomationInsertMember(context.Background(), callReq(map[string]any{
		"automation_id": "a1", "expected_version": float64(5), "reason": "r", "prd_id": "c9d0e1f2",
	})); err != nil {
		t.Fatal(err)
	}
	if m, _ := d.body["member"].(map[string]any); m["prd_id"] != "c9d0e1f2" || m["question"] != nil || m["kind"] != nil {
		t.Fatalf("prd member = %v", d.body["member"])
	}

	// More than one target, or none, is refused before the request.
	d.path = ""
	for name, args := range map[string]map[string]any{
		"question and prd":  {"prd_id": "c9d0e1f2", "question": "q?"},
		"question and item": {"pending_item_id": "i1", "question": "q?"},
		"all three":         {"prd_id": "c9d0e1f2", "pending_item_id": "i1", "question": "q?"},
		"none":              {},
		"blank question":    {"question": "   "},
	} {
		args["automation_id"], args["expected_version"], args["reason"] = "a1", float64(5), "r"
		res, err := s.handleAutomationInsertMember(context.Background(), callReq(args))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out := toolText(t, res); !strings.Contains(out, "error:") {
			t.Errorf("%s: result = %s, want an error", name, out)
		}
		if d.path != "" {
			t.Errorf("%s: reached the daemon", name)
		}
	}
}

func TestAutomationRunShapeToolsAreListedAndDescribed(t *testing.T) {
	names := map[string]bool{}
	for _, n := range AutomationToolNames() {
		names[n] = true
	}
	for _, n := range []string{"automation_set_max_parallel", "automation_set_stage_gate"} {
		if !names[n] {
			t.Errorf("AutomationToolNames lacks %s", n)
		}
	}
	s := &Server{}
	mp, sg := s.toolAutomationSetMaxParallel(), s.toolAutomationSetStageGate()
	for _, tool := range []struct{ name, desc string }{{mp.Name, mp.Description}, {sg.Name, sg.Description}} {
		if !strings.Contains(tool.desc, "docs/howto/automation-chains.md") {
			t.Errorf("%s description names no how-to: %s", tool.name, tool.desc)
		}
	}
	if len(mp.InputSchema.Required) == 0 || !contains(mp.InputSchema.Required, "max_parallel") {
		t.Errorf("max_parallel must be required: %v", mp.InputSchema.Required)
	}
	if !contains(sg.InputSchema.Required, "stage_gate") {
		t.Errorf("stage_gate must be required: %v", sg.InputSchema.Required)
	}
}
