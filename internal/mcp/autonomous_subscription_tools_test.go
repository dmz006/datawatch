// BL427 (BL430 Phase 5) — MCP tool tests for the lifecycle-event
// subscription surface. The handler under test proxies REST calls to an
// httptest server standing in for the daemon (same pattern as
// bl331_channel_routing_tools_test.go).
package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// bl427Port splits an httptest URL into its port for &Server{webPort: ...}.
func bl427Port(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	parts := strings.Split(srv.URL, ":")
	if len(parts) < 3 {
		t.Fatalf("unexpected test server URL %q", srv.URL)
	}
	port, err := strconv.Atoi(parts[2])
	if err != nil {
		t.Fatalf("parse port %q: %v", parts[2], err)
	}
	return port
}

func TestBL427_ToolNames(t *testing.T) {
	s := &Server{}
	if got := s.toolAutonomousSubscribe().Name; got != "autonomous_subscribe" {
		t.Errorf("subscribe tool name = %q", got)
	}
	if got := s.toolAutonomousUnsubscribe().Name; got != "autonomous_unsubscribe" {
		t.Errorf("unsubscribe tool name = %q", got)
	}
	if got := s.toolAutonomousSubscriptions().Name; got != "autonomous_subscriptions" {
		t.Errorf("subscriptions tool name = %q", got)
	}
}

// TestBL427_Subscribe_ShortKindBuildsSendInput verifies the MCP flag→REST
// body mapping the watch-and-inject shorthand expands to.
func TestBL427_Subscribe_ShortKindBuildsSendInput(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/autonomous/subscriptions" {
			t.Errorf("route = %s %s, want POST /api/autonomous/subscriptions", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"sub-001"}`))
	}))
	defer srv.Close()

	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "autonomous_subscribe"
	req.Params.Arguments = map[string]any{
		"kinds":       "task.session_started", // shorthand for task session start
		"task_id":     "task-7",
		"sink_type":   "session",
		"sink_target": "pi-host",
		"action_text": "Task task-7 session started — pick up the build.",
		"one_shot":    true,
	}
	res, err := s.handleAutonomousSubscribe(t.Context(), req)
	if err != nil {
		t.Fatalf("handleAutonomousSubscribe: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported error: %v", res.Content)
	}
	text := resContentText(t, res)
	if !strings.Contains(text, "sub-001") {
		t.Errorf("response should echo the daemon body (has sub-001), got %q", text)
	}

	filter, _ := gotBody["filter"].(map[string]any)
	if filter == nil {
		t.Fatalf("body missing filter: %v", gotBody)
	}
	wantKinds := []string{"task.session_started"}
	gotKinds, _ := filter["kinds"].([]any)
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("filter.kinds = %v, want %v", filter["kinds"], wantKinds)
	}
	if filter["task_id"] != "task-7" {
		t.Errorf("filter.task_id = %v, want task-7", filter["task_id"])
	}
	sink, _ := gotBody["sink"].(map[string]any)
	if sink == nil || sink["type"] != "session" || sink["target"] != "pi-host" {
		t.Errorf("sink = %v, want {type:session target:pi-host}", gotBody["sink"])
	}
	action, _ := gotBody["action"].(map[string]any)
	if action == nil || action["type"] != "send_input" {
		t.Errorf("action = %v, want type send_input", gotBody["action"])
	}
	if !boolField(t, gotBody, "one_shot") {
		t.Errorf("one_shot should be true, got %v", gotBody["one_shot"])
	}
}

// TestBL427_Subscribe_KindsAsArrayAndCSV checks both accepted kinds encodings
// and the coalesce / expires mapping.
func TestBL427_Subscribe_KindsAsArrayAndCSV(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "sub-002"})
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	ctx := t.Context()

	// CSV string with a full and a shorthand kind, plus prd scope.
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "autonomous_subscribe"
	req.Params.Arguments = map[string]any{
		"kinds":     "task.completed,prd.blocked",
		"prd_id":    "prd-9",
		"sink_type": "comm",
		"level":     "prd",
		"coalesce":  true,
	}
	if res, err := s.handleAutonomousSubscribe(ctx, req); err != nil {
		t.Fatalf("handler: %v", err)
	} else if res.IsError {
		t.Fatalf("tool reported error: %v", res.Content)
	}
	filter := mapField(t, gotBody, "filter")
	if got := stringSliceField(t, filter, "kinds"); len(got) != 2 || got[0] != "task.completed" || got[1] != "prd.blocked" {
		t.Errorf("filter.kinds = %v", got)
	}
	if filter["prd_id"] != "prd-9" || filter["level"] != "prd" {
		t.Errorf("filter scope = %v", filter)
	}
	if !boolField(t, gotBody, "coalesce") {
		t.Errorf("coalesce should be true")
	}
	if sink := mapField(t, gotBody, "sink"); sink["type"] != "comm" {
		t.Errorf("sink = %v", gotBody["sink"])
	}

	// JSON array encoding also accepted.
	req2 := mcpsdk.CallToolRequest{}
	req2.Params.Name = "autonomous_subscribe"
	req2.Params.Arguments = map[string]any{
		"kinds":       []any{"task.session_started", "task.prd_updated"},
		"sink_type":   "topic",
		"sink_target": "mobile",
	}
	if res, err := s.handleAutonomousSubscribe(ctx, req2); err != nil {
		t.Fatalf("handler (array kinds): %v", err)
	} else if res.IsError {
		t.Fatalf("tool reported error: %v", res.Content)
	}
	filter = mapField(t, gotBody, "filter")
	if got := stringSliceField(t, filter, "kinds"); len(got) != 2 || got[0] != "task.session_started" {
		t.Errorf("filter.kinds (array) = %v", got)
	}
}

// TestBL427_Subscribe_UpstreamErrorSurfaces checks the daemon's validation
// errors are proxied back as a tool error (isError + "autonomous_subscribe:").
func TestBL427_Subscribe_UpstreamErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("filter must set at least one of kinds or level"))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "autonomous_subscribe"
	req.Params.Arguments = map[string]any{"sink_type": "comm"}
	res, err := s.handleAutonomousSubscribe(t.Context(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected a tool error, got %v", res.Content)
	}
	text := resContentText(t, res)
	for _, want := range []string{"autonomous_subscribe:", "filter must set at least one of kinds or level"} {
		if !strings.Contains(text, want) {
			t.Errorf("error should contain %q, got %q", want, text)
		}
	}
}

// TestBL427_Unsubscribe_RoutesByID verifies DELETE routing and the 204 echo.
func TestBL427_Unsubscribe_RoutesByID(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "autonomous_unsubscribe"
	req.Params.Arguments = map[string]any{"id": "sub-001"}
	res, err := s.handleAutonomousUnsubscribe(t.Context(), req)
	if err != nil {
		t.Fatalf("handleAutonomousUnsubscribe: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported error: %v", res.Content)
	}
	if gotPath != "/api/autonomous/subscriptions/sub-001" {
		t.Errorf("path = %q, want /api/autonomous/subscriptions/sub-001", gotPath)
	}
	if text := resContentText(t, res); !strings.Contains(text, "sub-001") {
		t.Errorf("204 echo should name the id, got %q", text)
	}
}

// TestBL427_Unsubscribe_MissingID errors client-side without a call.
func TestBL427_Unsubscribe_MissingID(t *testing.T) {
	s := &Server{}
	res, err := s.handleAutonomousUnsubscribe(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected a tool error for empty id")
	}
	if text := resContentText(t, res); !strings.Contains(text, "autonomous_unsubscribe:") {
		t.Errorf("error should be prefixed with the tool name, got %q", text)
	}
}

// TestBL427_Subscriptions_OwnerQuery verifies the owner filter lands in the
// query string.
func TestBL427_Subscriptions_OwnerQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.URL.Path != "/api/autonomous/subscriptions" || r.Method != http.MethodGet {
			t.Errorf("route = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"sub-001","one_shot":true}]`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "autonomous_subscriptions"
	req.Params.Arguments = map[string]any{"owner": "operator"}
	res, err := s.handleAutonomousSubscriptions(t.Context(), req)
	if err != nil {
		t.Fatalf("handleAutonomousSubscriptions: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported error: %v", res.Content)
	}
	if !strings.Contains(gotQuery, "owner=operator") {
		t.Errorf("query = %q, want owner=operator", gotQuery)
	}
	if text := resContentText(t, res); !strings.Contains(text, "sub-001") {
		t.Errorf("response should echo the daemon list, got %q", text)
	}

	// No owner → no query string.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv2.Close()
	s2 := &Server{webPort: bl427Port(t, srv2)}
	if res, err := s2.handleAutonomousSubscriptions(t.Context(), mcpsdk.CallToolRequest{}); err != nil {
		t.Fatalf("handler (no owner): %v", err)
	} else if res.IsError {
		t.Fatalf("tool reported error: %v", res.Content)
	}
}

// --- small shared assertions ----------------------------------------------

func resContentText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	tc, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("first content is %T, not TextContent", res.Content[0])
	}
	return tc.Text
}

func mapField(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("body missing %q map (got %T)", key, m[key])
	}
	return v
}

func boolField(t *testing.T, m map[string]any, key string) bool {
	t.Helper()
	v, _ := m[key].(bool)
	return v
}

func stringSliceField(t *testing.T, m map[string]any, key string) []string {
	t.Helper()
	v, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%s is %T, not []any", key, m[key])
	}
	out := make([]string, len(v))
	for i, e := range v {
		s, ok := e.(string)
		if !ok {
			t.Fatalf("%s[%d] is %T", key, i, e)
		}
		out[i] = s
	}
	return out
}

// TestBL427_Subscribe_WarningKindsAreOptIn pins the MCP surface for the
// new opt-in warning kinds: the handler accepts scope_warning /
// guardrail_warn bare (they are warnings, not task statuses — never
// expanded to task.*), and the tool + kinds descriptions name them as
// warnings that never block work.
func TestBL427_Subscribe_WarningKindsAreOptIn(t *testing.T) {
	// 1) The tool definitions advertise the opt-in warning kinds.
	def := (&Server{}).toolAutonomousSubscribe()
	desc := def.Description
	props, _ := def.InputSchema.Properties["kinds"].(map[string]any)
	kindDesc, _ := props["description"].(string)
	for _, want := range []string{"scope_warning", "guardrail_warn", "never block"} {
		if !strings.Contains(desc, want) && !strings.Contains(kindDesc, want) {
			t.Errorf("autonomous_subscribe descriptions missing %q (tool: %q; kinds: %q)", want, desc, kindDesc)
		}
	}

	// 2) The handler passes the warning kinds through unchanged.
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/autonomous/subscriptions" {
			t.Errorf("route = %s %s, want POST /api/autonomous/subscriptions", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"sub-warn-1"}`))
	}))
	defer srv.Close()

	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "autonomous_subscribe"
	req.Params.Arguments = map[string]any{
		"kinds":       "scope_warning,guardrail_warn",
		"prd_id":      "prd-guard",
		"sink_type":   "alert",
		"sink_target": "router-1",
	}
	res, err := s.handleAutonomousSubscribe(t.Context(), req)
	if err != nil {
		t.Fatalf("handleAutonomousSubscribe: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported error: %v", res.Content)
	}
	if text := resContentText(t, res); !strings.Contains(text, "sub-warn-1") {
		t.Errorf("response should echo the daemon body (has sub-warn-1), got %q", text)
	}

	filter, _ := gotBody["filter"].(map[string]any)
	if filter == nil {
		t.Fatalf("body missing filter: %v", gotBody)
	}
	gotKinds := stringSliceField(t, filter, "kinds")
	if len(gotKinds) != 2 || gotKinds[0] != "scope_warning" || gotKinds[1] != "guardrail_warn" {
		t.Errorf("filter.kinds = %v, want [scope_warning guardrail_warn] (bare — never task.*)", gotKinds)
	}
	if filter["prd_id"] != "prd-guard" {
		t.Errorf("filter.prd_id = %v, want prd-guard", filter["prd_id"])
	}
}
