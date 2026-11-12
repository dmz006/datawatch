package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

type budgetCall struct {
	method string
	path   string
	query  string
	body   map[string]any
}

// budgetRecorder serves canned bodies by path and records every request.
type budgetRecorder struct {
	mu     sync.Mutex
	calls  []budgetCall
	bodies map[string]string
}

func (r *budgetRecorder) handler(w http.ResponseWriter, req *http.Request) {
	raw, _ := io.ReadAll(req.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	r.mu.Lock()
	r.calls = append(r.calls, budgetCall{req.Method, req.URL.Path, req.URL.RawQuery, body})
	r.mu.Unlock()
	if out, ok := r.bodies[req.URL.Path]; ok {
		_, _ = w.Write([]byte(out))
		return
	}
	_, _ = w.Write([]byte(`{}`))
}

func (r *budgetRecorder) find(method, path string) *budgetCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.calls {
		if r.calls[i].method == method && r.calls[i].path == path {
			return &r.calls[i]
		}
	}
	return nil
}

func budgetResultText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("empty tool result")
	}
	tc, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want TextContent", res.Content[0])
	}
	return tc.Text
}

const budgetStatusBody = `{"budgets":[{"llm":"claude","ceiling_usd":50,"spend_usd":12.5}]}`

func TestBudgetMCPStatusTool(t *testing.T) {
	rec := &budgetRecorder{bodies: map[string]string{"/api/budget": budgetStatusBody}}
	s := planTierWebServer(t, rec.handler)

	tool := s.toolBudgetStatus()
	if tool.Name != "budget_status" {
		t.Fatalf("tool name = %q, want budget_status", tool.Name)
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("required = %v, want none", tool.InputSchema.Required)
	}
	if _, ok := tool.InputSchema.Properties["llm"]; !ok {
		t.Errorf("budget_status has no llm argument")
	}
	for _, phrase := range []string{"never blocks", "off by default"} {
		if !strings.Contains(tool.Description, phrase) {
			t.Errorf("description missing %q", phrase)
		}
	}

	res, err := s.handleBudgetStatus(context.Background(), callReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("budget_status: err=%v result=%+v", err, res)
	}
	if got := budgetResultText(t, res); got != budgetStatusBody {
		t.Errorf("body = %s, want the REST body verbatim %s", got, budgetStatusBody)
	}
	c := rec.find(http.MethodGet, "/api/budget")
	if c == nil || c.query != "" {
		t.Fatalf("GET /api/budget call = %+v, want no query", c)
	}

	res, err = s.handleBudgetStatus(context.Background(), callReq(map[string]any{"llm": " claude "}))
	if err != nil || res.IsError {
		t.Fatalf("budget_status llm: err=%v result=%+v", err, res)
	}
	rec.mu.Lock()
	last := rec.calls[len(rec.calls)-1]
	rec.mu.Unlock()
	if last.query != "llm=claude" {
		t.Errorf("query = %q, want llm=claude", last.query)
	}

	// A REST failure (no budget for the LLM) surfaces as a tool error.
	notFound := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no budget for llm plain", http.StatusNotFound)
	})
	res, err = notFound.handleBudgetStatus(context.Background(), callReq(map[string]any{"llm": "plain"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("404 should be a tool error: err=%v result=%+v", err, res)
	}
	if !strings.Contains(budgetResultText(t, res), "no budget for llm plain") {
		t.Errorf("error text lost the REST message: %s", budgetResultText(t, res))
	}
}

func TestBudgetLLMTools(t *testing.T) {
	s := &Server{}
	budgetArg := map[string]any{
		"ceiling_usd": 50, "weekday": "monday", "time": "09:00", "zone": "UTC",
		"allocation_pct": true, "peer_window": []any{"edge"},
	}

	for name, tool := range map[string]mcpsdk.Tool{"llm_add": s.toolLLMAdd(), "llm_update": s.toolLLMUpdate()} {
		prop, ok := tool.InputSchema.Properties["budget"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no budget argument", name)
		}
		if prop["type"] != "object" {
			t.Errorf("%s budget type = %v, want object", name, prop["type"])
		}
		members, _ := prop["properties"].(map[string]any)
		for _, f := range []string{"ceiling_usd", "hours", "weekday", "time", "zone", "allocation_pct", "peer_window"} {
			if _, ok := members[f]; !ok {
				t.Errorf("%s budget object missing member %s", name, f)
			}
		}
		if !strings.Contains(tool.Description, "never blocks") || !strings.Contains(tool.Description, "off by default") {
			t.Errorf("%s description must say allocation_pct never blocks and peer_window is off by default: %s", name, tool.Description)
		}
	}
	for name, desc := range map[string]string{"llm_get": s.toolLLMGet().Description, "llm_list": s.toolLLMList().Description} {
		if !strings.Contains(desc, "budget") {
			t.Errorf("%s description does not mention the budget it returns", name)
		}
	}

	// llm_add / llm_update forward the budget object under the REST name.
	rec := &budgetRecorder{bodies: map[string]string{
		"/api/llms/claude": `{"name":"claude","budget":{"ceiling_usd":50,"weekday":"monday"}}`,
		"/api/llms":        `[{"name":"claude","budget":{"ceiling_usd":50,"weekday":"monday"}}]`,
	}}
	srv := planTierWebServer(t, rec.handler)
	if res, err := srv.handleLLMAddMCP(context.Background(), callReq(map[string]any{"name": "claude", "kind": "claude", "budget": budgetArg})); err != nil || res.IsError {
		t.Fatalf("llm_add: err=%v result=%+v", err, res)
	}
	add := rec.find(http.MethodPost, "/api/llms")
	if add == nil {
		t.Fatal("llm_add did not POST /api/llms")
	}
	got, _ := add.body["budget"].(map[string]any)
	if got["ceiling_usd"] != float64(50) || got["weekday"] != "monday" || got["time"] != "09:00" || got["zone"] != "UTC" || got["allocation_pct"] != true {
		t.Errorf("llm_add budget = %v, want the REST fields", got)
	}
	if peers, _ := got["peer_window"].([]any); len(peers) != 1 || peers[0] != "edge" {
		t.Errorf("llm_add peer_window = %v, want [edge]", got["peer_window"])
	}

	if res, err := srv.handleLLMUpdateMCP(context.Background(), callReq(map[string]any{"name": "claude", "kind": "claude", "budget": `{"ceiling_usd":10,"hours":24}`})); err != nil || res.IsError {
		t.Fatalf("llm_update: err=%v result=%+v", err, res)
	}
	upd := rec.find(http.MethodPut, "/api/llms/claude")
	if upd == nil {
		t.Fatal("llm_update did not PUT /api/llms/claude")
	}
	got, _ = upd.body["budget"].(map[string]any)
	if got["ceiling_usd"] != float64(10) || got["hours"] != float64(24) {
		t.Errorf("llm_update budget (JSON-text form) = %v", got)
	}

	// Omitted keeps the stored budget (no key sent); explicit null clears it.
	rec.calls = nil
	_, _ = srv.handleLLMUpdateMCP(context.Background(), callReq(map[string]any{"name": "claude", "kind": "claude"}))
	if c := rec.find(http.MethodPut, "/api/llms/claude"); c == nil {
		t.Fatal("llm_update without budget did not PUT")
	} else if _, present := c.body["budget"]; present {
		t.Errorf("budget key sent although not given: %v", c.body["budget"])
	}
	rec.calls = nil
	_, _ = srv.handleLLMUpdateMCP(context.Background(), callReq(map[string]any{"name": "claude", "kind": "claude", "budget": nil}))
	if c := rec.find(http.MethodPut, "/api/llms/claude"); c == nil {
		t.Fatal("llm_update with null budget did not PUT")
	} else if v, present := c.body["budget"]; !present || v != nil {
		t.Errorf("null budget must reach REST as null, got present=%v value=%v", present, v)
	}

	// A malformed budget argument is a tool error and writes nothing.
	rec.calls = nil
	res, err := srv.handleLLMAddMCP(context.Background(), callReq(map[string]any{"name": "x", "kind": "claude", "budget": 5}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("bad budget should be a tool error: err=%v result=%+v", err, res)
	}
	if len(rec.calls) != 0 {
		t.Errorf("bad budget still reached REST: %+v", rec.calls)
	}

	// llm_get / llm_list pass the budget through untouched.
	for _, tc := range []struct {
		out string
		run func() (*mcpsdk.CallToolResult, error)
	}{
		{rec.bodies["/api/llms/claude"], func() (*mcpsdk.CallToolResult, error) {
			return srv.handleLLMGetMCP(context.Background(), callReq(map[string]any{"name": "claude"}))
		}},
		{rec.bodies["/api/llms"], func() (*mcpsdk.CallToolResult, error) {
			return srv.handleLLMListMCP(context.Background(), callReq(nil))
		}},
	} {
		res, err := tc.run()
		if err != nil {
			t.Fatal(err)
		}
		if budgetResultText(t, res) != tc.out || !strings.Contains(budgetResultText(t, res), `"budget"`) {
			t.Errorf("result = %s, want %s", budgetResultText(t, res), tc.out)
		}
	}

	// The allocation table through autonomous_config_set: object form is the
	// whole table, pair form sets single tiers; both go through /api/config.
	cfgTool := s.toolAutonomousConfigSet()
	if _, ok := cfgTool.InputSchema.Properties["plan_tier_allocation_usd"]; !ok {
		t.Fatal("autonomous_config_set has no plan_tier_allocation_usd argument")
	}
	if !strings.Contains(cfgTool.Description+propDesc(cfgTool, "plan_tier_allocation_usd"), "never blocks") {
		t.Errorf("plan_tier_allocation_usd must state that allocation is an estimate that never blocks")
	}

	cfgRec := &budgetRecorder{}
	cfgSrv := planTierWebServer(t, cfgRec.handler)
	if res, err := cfgSrv.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"plan_tier_allocation_usd": `{"max_5x":100,"pro":20.5}`})); err != nil || res.IsError {
		t.Fatalf("config_set object form: err=%v result=%+v", err, res)
	}
	put := cfgRec.find(http.MethodPut, "/api/config")
	if put == nil {
		t.Fatal("object form did not PUT /api/config")
	}
	table, _ := put.body["autonomous.plan_tier_allocation_usd"].(map[string]any)
	if table["max_5x"] != float64(100) || table["pro"] != 20.5 || len(table) != 2 {
		t.Errorf("object form table = %v, want max_5x:100 pro:20.5", table)
	}
	if cfgRec.find(http.MethodPut, "/api/autonomous/config") != nil {
		t.Errorf("allocation alone must not PUT the manager config")
	}

	cfgRec.calls = nil
	if res, err := cfgSrv.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"plan_tier_allocation_usd": "api=0, max_20x=400", "max_concurrent_tasks": 2,
	})); err != nil || res.IsError {
		t.Fatalf("config_set pair form: err=%v result=%+v", err, res)
	}
	put = cfgRec.find(http.MethodPut, "/api/config")
	if put == nil {
		t.Fatal("pair form did not PUT /api/config")
	}
	if put.body["autonomous.plan_tier_allocation_usd.api"] != float64(0) || put.body["autonomous.plan_tier_allocation_usd.max_20x"] != float64(400) || len(put.body) != 2 {
		t.Errorf("pair form body = %v, want one key per tier", put.body)
	}
	if cfgRec.find(http.MethodPut, "/api/autonomous/config") == nil {
		t.Errorf("other config fields in the same call must still PUT the manager config")
	}

	// Bad values are rejected before anything is written.
	for _, bad := range []string{"gold=5", "pro=-1", "pro=abc", `{"pro":"x"}`, `{"gold":1}`, "pro", "{", " "} {
		cfgRec.calls = nil
		res, err := cfgSrv.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"plan_tier_allocation_usd": bad}))
		if bad == " " {
			// Whitespace-only counts as not given, same as the inflight table.
			continue
		}
		if err != nil || res == nil || !res.IsError {
			t.Errorf("%q: want a tool error, got err=%v result=%+v", bad, err, res)
		}
		if len(cfgRec.calls) != 0 {
			t.Errorf("%q: invalid value still reached REST: %+v", bad, cfgRec.calls)
		}
	}
}

func propDesc(tool mcpsdk.Tool, name string) string {
	prop, _ := tool.InputSchema.Properties[name].(map[string]any)
	desc, _ := prop["description"].(string)
	return desc
}

func TestCapacityStatusIncludesBudgets(t *testing.T) {
	const capacityBody = `{"pools":[],"leases":[],"waiting":[],"warnings":[],"backoff":[],"budgets":[{"llm":"claude","ceiling_usd":50,"spend_usd":12.5}]}`
	const statusBody = `{"running":true,"budgets":[{"llm":"claude","ceiling_usd":50,"spend_usd":12.5}]}`
	rec := &budgetRecorder{bodies: map[string]string{
		"/api/capacity":          capacityBody,
		"/api/autonomous/status": statusBody,
	}}
	s := planTierWebServer(t, rec.handler)

	res, err := s.handleCapacityStatus(context.Background(), callReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	var capOut struct {
		Budgets []map[string]any `json:"budgets"`
	}
	if err := json.Unmarshal([]byte(budgetResultText(t, res)), &capOut); err != nil {
		t.Fatalf("capacity_status result is not JSON: %v", err)
	}
	if len(capOut.Budgets) != 1 || capOut.Budgets[0]["llm"] != "claude" || capOut.Budgets[0]["spend_usd"] != 12.5 {
		t.Errorf("capacity_status budgets = %v, want the claude entry", capOut.Budgets)
	}

	res, err = s.handleAutonomousStatus(context.Background(), callReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	var stOut struct {
		Budgets []map[string]any `json:"budgets"`
	}
	if err := json.Unmarshal([]byte(budgetResultText(t, res)), &stOut); err != nil {
		t.Fatalf("autonomous_status result is not JSON: %v", err)
	}
	if len(stOut.Budgets) != 1 || stOut.Budgets[0]["llm"] != "claude" {
		t.Errorf("autonomous_status budgets = %v, want the claude entry passed through", stOut.Budgets)
	}

	for name, desc := range map[string]string{
		"capacity_status":   s.toolCapacityStatus().Description,
		"autonomous_status": s.toolAutonomousStatus().Description,
	} {
		for _, phrase := range []string{"budgets", "never blocks", "off by default"} {
			if !strings.Contains(desc, phrase) {
				t.Errorf("%s description missing %q", name, phrase)
			}
		}
	}
}
