// Tests for the subscription update / test MCP tools. The handlers proxy
// REST calls to an httptest server standing in for the daemon.
package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// subUpdateCapture records the one request a handler makes and replies
// with the configured status and body.
type subUpdateCapture struct {
	method string
	path   string
	body   map[string]any
	calls  int
}

func subUpdateServer(t *testing.T, c *subUpdateCapture, code int, reply string) *Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.calls++
		c.method = r.Method
		c.path = r.URL.Path
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&c.body)
		}
		if code >= 400 {
			http.Error(w, reply, code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return &Server{webPort: bl427Port(t, srv)}
}

func subUpdateReq(name string, args map[string]any) mcpsdk.CallToolRequest {
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	return req
}

func TestSubscriptionSurface_ToolNames(t *testing.T) {
	s := &Server{}
	upd := s.toolAutonomousSubscriptionUpdate()
	if upd.Name != "autonomous_subscription_update" {
		t.Errorf("update tool name = %q", upd.Name)
	}
	if !contains(upd.InputSchema.Required, "id") {
		t.Errorf("update tool must require id, required = %v", upd.InputSchema.Required)
	}
	tst := s.toolAutonomousSubscriptionTest()
	if tst.Name != "autonomous_subscription_test" {
		t.Errorf("test tool name = %q", tst.Name)
	}
	if !contains(tst.InputSchema.Required, "id") {
		t.Errorf("test tool must require id, required = %v", tst.InputSchema.Required)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestSubscriptionSurface_UpdateRequiresID(t *testing.T) {
	c := &subUpdateCapture{}
	s := subUpdateServer(t, c, http.StatusOK, `{}`)
	for _, id := range []any{nil, "", "   "} {
		args := map[string]any{"kinds": "prd.blocked", "sink_type": "comm", "sink_target": "r"}
		if id != nil {
			args["id"] = id
		}
		res, err := s.handleAutonomousSubscriptionUpdate(t.Context(), subUpdateReq("autonomous_subscription_update", args))
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !res.IsError || !strings.Contains(resContentText(t, res), "id is required") {
			t.Errorf("id=%v: want an 'id is required' tool error, got %v", id, res.Content)
		}
	}
	if c.calls != 0 {
		t.Errorf("daemon was called %d times for invalid input, want 0", c.calls)
	}
}

func TestSubscriptionSurface_UpdateRejectsBadExpiry(t *testing.T) {
	c := &subUpdateCapture{}
	s := subUpdateServer(t, c, http.StatusOK, `{}`)
	for _, v := range []any{float64(0), float64(-5), "soon"} {
		res, err := s.handleAutonomousSubscriptionUpdate(t.Context(), subUpdateReq("autonomous_subscription_update", map[string]any{
			"id": "sub-1", "kinds": "prd.blocked", "sink_type": "comm", "sink_target": "r",
			"expires_in_seconds": v,
		}))
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !res.IsError || !strings.Contains(resContentText(t, res), "expires_in_seconds") {
			t.Errorf("expires_in_seconds=%v: want a validation tool error, got %v", v, res.Content)
		}
	}
	if c.calls != 0 {
		t.Errorf("daemon was called %d times for invalid input, want 0", c.calls)
	}
}

func TestSubscriptionSurface_UpdateProxiesPut(t *testing.T) {
	c := &subUpdateCapture{}
	s := subUpdateServer(t, c, http.StatusOK, `{"id":"sub-9","one_shot":true}`)
	before := time.Now().UTC()
	res, err := s.handleAutonomousSubscriptionUpdate(t.Context(), subUpdateReq("autonomous_subscription_update", map[string]any{
		"id":                 "sub-9",
		"kinds":              []any{"task.session_started", "prd.blocked"},
		"level":              "task",
		"prd_id":             "prd-1",
		"task_id":            "task-7",
		"sink_type":          "session",
		"sink_target":        "pi-host",
		"action_text":        "pick up the build",
		"one_shot":           true,
		"coalesce":           true,
		"expires_in_seconds": float64(3600),
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported error: %v", res.Content)
	}
	if !strings.Contains(resContentText(t, res), "sub-9") {
		t.Errorf("response should echo the daemon record, got %q", resContentText(t, res))
	}
	if c.method != http.MethodPut || c.path != "/api/autonomous/subscriptions/sub-9" {
		t.Fatalf("route = %s %s, want PUT /api/autonomous/subscriptions/sub-9", c.method, c.path)
	}

	filter := mapField(t, c.body, "filter")
	kinds, _ := filter["kinds"].([]any)
	if len(kinds) != 2 || kinds[0] != "task.session_started" || kinds[1] != "prd.blocked" {
		t.Errorf("filter.kinds = %v", filter["kinds"])
	}
	if filter["level"] != "task" || filter["prd_id"] != "prd-1" || filter["task_id"] != "task-7" {
		t.Errorf("filter = %v", filter)
	}
	sink := mapField(t, c.body, "sink")
	if sink["type"] != "session" || sink["target"] != "pi-host" {
		t.Errorf("sink = %v", sink)
	}
	action := mapField(t, c.body, "action")
	if action["type"] != "send_input" || action["text"] != "pick up the build" {
		t.Errorf("action = %v", action)
	}
	if c.body["one_shot"] != true || c.body["coalesce"] != true {
		t.Errorf("one_shot/coalesce = %v/%v", c.body["one_shot"], c.body["coalesce"])
	}
	exp, _ := c.body["expires_at"].(string)
	at, perr := time.Parse(time.RFC3339, exp)
	if perr != nil {
		t.Fatalf("expires_at %q is not RFC3339: %v", exp, perr)
	}
	if d := at.Sub(before); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("expires_at is %v from now, want about 1h", d)
	}
}

func TestSubscriptionSurface_UpdateOmitsUnsetFields(t *testing.T) {
	c := &subUpdateCapture{}
	s := subUpdateServer(t, c, http.StatusOK, `{"id":"sub-2"}`)
	_, err := s.handleAutonomousSubscriptionUpdate(t.Context(), subUpdateReq("autonomous_subscription_update", map[string]any{
		"id": "sub id/2", "kinds": "prd.blocked", "sink_type": "comm", "sink_target": "r",
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if c.path != "/api/autonomous/subscriptions/sub%20id%2F2" && c.path != "/api/autonomous/subscriptions/sub id/2" {
		t.Errorf("id must be path-escaped, path = %q", c.path)
	}
	for _, k := range []string{"action", "one_shot", "coalesce", "expires_at"} {
		if _, ok := c.body[k]; ok {
			t.Errorf("unset field %q should not be sent: %v", k, c.body)
		}
	}
}

func TestSubscriptionSurface_UpdateErrorPassthrough(t *testing.T) {
	cases := []struct {
		name string
		code int
		body string
		want string
	}{
		{"validation 400", http.StatusBadRequest, "filter kind \"nope\" is not a known event kind", "not a known event kind"},
		{"unknown id 404", http.StatusNotFound, "subscription gone: subscription not found", "subscription not found"},
	}
	for _, tc := range cases {
		c := &subUpdateCapture{}
		s := subUpdateServer(t, c, tc.code, tc.body)
		res, err := s.handleAutonomousSubscriptionUpdate(t.Context(), subUpdateReq("autonomous_subscription_update", map[string]any{
			"id": "gone", "kinds": "prd.blocked", "sink_type": "comm", "sink_target": "r",
		}))
		if err != nil {
			t.Fatalf("%s: handler error: %v", tc.name, err)
		}
		text := resContentText(t, res)
		if !res.IsError || !strings.Contains(text, tc.want) || !strings.Contains(text, "autonomous_subscription_update:") {
			t.Errorf("%s: want error containing %q, got isError=%v %q", tc.name, tc.want, res.IsError, text)
		}
	}
}

func TestSubscriptionSurface_UpdateNoLoopback(t *testing.T) {
	s := &Server{}
	res, err := s.handleAutonomousSubscriptionUpdate(t.Context(), subUpdateReq("autonomous_subscription_update", map[string]any{
		"id": "x", "kinds": "prd.blocked", "sink_type": "comm", "sink_target": "r",
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Errorf("want a tool error without a web server, got %v", res.Content)
	}
}

func TestSubscriptionSurface_TestRequiresID(t *testing.T) {
	c := &subUpdateCapture{}
	s := subUpdateServer(t, c, http.StatusOK, `{}`)
	res, err := s.handleAutonomousSubscriptionTest(t.Context(), subUpdateReq("autonomous_subscription_test", map[string]any{"id": "  "}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError || !strings.Contains(resContentText(t, res), "id is required") {
		t.Errorf("want 'id is required', got %v", res.Content)
	}
	if c.calls != 0 {
		t.Errorf("daemon was called for invalid input")
	}
}

func TestSubscriptionSurface_TestProxiesPost(t *testing.T) {
	c := &subUpdateCapture{}
	s := subUpdateServer(t, c, http.StatusOK, `{"ok":true,"sink":"comm:r"}`)
	res, err := s.handleAutonomousSubscriptionTest(t.Context(), subUpdateReq("autonomous_subscription_test", map[string]any{"id": "sub-4"}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported error: %v", res.Content)
	}
	if c.method != http.MethodPost || c.path != "/api/autonomous/subscriptions/sub-4/test" {
		t.Errorf("route = %s %s, want POST /api/autonomous/subscriptions/sub-4/test", c.method, c.path)
	}
	if text := resContentText(t, res); !strings.Contains(text, `"ok":true`) {
		t.Errorf("response should echo the daemon answer, got %q", text)
	}
}

func TestSubscriptionSurface_TestErrorPassthrough(t *testing.T) {
	for _, tc := range []struct {
		code int
		body string
	}{
		{http.StatusNotFound, "subscription nope: subscription not found"},
		{http.StatusServiceUnavailable, "no engine attached to deliver test events"},
	} {
		c := &subUpdateCapture{}
		s := subUpdateServer(t, c, tc.code, tc.body)
		res, err := s.handleAutonomousSubscriptionTest(t.Context(), subUpdateReq("autonomous_subscription_test", map[string]any{"id": "nope"}))
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		text := resContentText(t, res)
		if !res.IsError || !strings.Contains(text, tc.body) || !strings.Contains(text, "autonomous_subscription_test:") {
			t.Errorf("code %d: want error containing %q, got isError=%v %q", tc.code, tc.body, res.IsError, text)
		}
	}
}
