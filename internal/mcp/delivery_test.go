package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

type deliveryHit struct {
	method, path, caller, auth string
	body                       map[string]any
}

type deliveryFakeREST struct {
	mu     sync.Mutex
	hits   []deliveryHit
	status int
	reply  string
}

func (f *deliveryFakeREST) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	hit := deliveryHit{method: r.Method, path: r.URL.RequestURI(),
		caller: r.Header.Get(federation.HeaderDeliveryCaller), auth: r.Header.Get("Authorization")}
	data, _ := io.ReadAll(r.Body)
	if len(data) > 0 {
		_ = json.Unmarshal(data, &hit.body)
	}
	f.hits = append(f.hits, hit)
	if f.status != 0 {
		http.Error(w, f.reply, f.status)
		return
	}
	_, _ = io.WriteString(w, f.reply)
}

func newDeliveryMCP(t *testing.T) (*Server, *deliveryFakeREST) {
	t.Helper()
	f := &deliveryFakeREST{reply: `{"changed":1}`}
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return &Server{webPort: port, token: "admin-tok", cfg: &config.MCPConfig{}}, f
}

type deliveryTool = func(*Server, context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)

var deliveryToolCalls = []struct {
	name, method, path string
	fn                 deliveryTool
	args, body         map[string]any
}{
	{"delivery_status", "GET", "/api/delivery", (*Server).handleDeliveryStatus, nil, nil},
	{"delivery_check", "POST", "/api/delivery/check", (*Server).handleDeliveryCheck, nil, nil},
	{"delivery_ack", "POST", "/api/delivery/ack", (*Server).handleDeliveryAck,
		map[string]any{"keys": []any{"task:t1:failed", "bug:b1:open"}},
		map[string]any{"keys": []any{"task:t1:failed", "bug:b1:open"}}},
	{"delivery_accept", "POST", "/api/delivery/accept", (*Server).handleDeliveryAccept,
		map[string]any{"kind": "automaton", "state": "held", "match_reason": "no_review_date", "reason": "parked", "until": "2026-12-01"},
		map[string]any{"kind": "automaton", "state": "held", "match_reason": "no_review_date", "reason": "parked", "until": "2026-12-01"}},
	{"delivery_drop", "POST", "/api/delivery/drop", (*Server).handleDeliveryDrop,
		map[string]any{"backlog": true, "reason": "obsolete"},
		map[string]any{"backlog": true, "reason": "obsolete"}},
	{"delivery_test_message", "POST", "/api/delivery/test-message", (*Server).handleDeliveryTestMessage, nil, nil},
}

func deliveryRun(t *testing.T, s *Server, ctx context.Context, name string, fn deliveryTool, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := fn(s, ctx, mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{Name: name, Arguments: args}})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func deliveryText(res *mcpsdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestDeliveryToolsCallTheDaemon(t *testing.T) {
	for _, c := range deliveryToolCalls {
		s, f := newDeliveryMCP(t)
		res := deliveryRun(t, s, context.Background(), c.name, c.fn, c.args)
		if res.IsError || deliveryText(res) != `{"changed":1}` {
			t.Errorf("%s: error=%v text=%q", c.name, res.IsError, deliveryText(res))
			continue
		}
		if len(f.hits) != 1 {
			t.Errorf("%s: %d calls", c.name, len(f.hits))
			continue
		}
		hit := f.hits[0]
		if hit.method != c.method || hit.path != c.path || hit.auth != "Bearer admin-tok" {
			t.Errorf("%s: %s %s auth=%q", c.name, hit.method, hit.path, hit.auth)
		}
		if !reflect.DeepEqual(hit.body, c.body) {
			t.Errorf("%s: body = %v, want %v", c.name, hit.body, c.body)
		}
		if hit.caller != "" {
			t.Errorf("%s: an operator call named a caller: %q", c.name, hit.caller)
		}
	}
}

// A tool call made with a session or peer credential says so to the daemon,
// which would otherwise see only the tool server's admin token.
func TestDeliveryToolsNameTheSessionOrPeer(t *testing.T) {
	cases := []struct {
		name    string
		ctx     context.Context
		process string
		want    string
	}{
		{"session credential", federation.WithCallerSessionID(federation.ContextWithPrincipal(context.Background(), "session-scoped"), "host-1a2b"), "", "session:host-1a2b"},
		{"session subprocess", context.Background(), "host-9f9f", "session:host-9f9f"},
		{"unresolved session", federation.ContextWithPrincipal(context.Background(), "session-scoped"), "", "session:unknown"},
		{"peer", federation.ContextWithPrincipal(context.Background(), "peer:desk"), "", "peer:desk"},
		{"admin", federation.ContextWithPrincipal(context.Background(), "admin"), "", ""},
	}
	for _, c := range cases {
		for _, tool := range deliveryToolCalls {
			s, f := newDeliveryMCP(t)
			s.callerSessionID = c.process
			deliveryRun(t, s, c.ctx, tool.name, tool.fn, tool.args)
			if len(f.hits) != 1 || f.hits[0].caller != c.want {
				t.Errorf("%s %s: hits=%+v, want caller %q", c.name, tool.name, f.hits, c.want)
			}
		}
	}
}

// A misnamed or mistyped argument is refused: dropping it silently would widen
// what the action applies to. No argument can set who did it.
func TestDeliveryToolsRefuseBadArguments(t *testing.T) {
	bad := []map[string]any{
		{"kind": "task", "stat": "failed"},
		{"kind": "task", "keys": "task:t1:failed"},
		{"kind": "task", "keys": []any{"task:t1:failed", 7}},
		{"kind": "task", "state": 3},
		{"kind": "task", "backlog": "yes please"},
		{"backlog": true, "actor": "operator"},
		{"backlog": true, "by": "operator"},
	}
	for _, tool := range deliveryToolCalls {
		if tool.body == nil {
			continue
		}
		for _, args := range bad {
			s, f := newDeliveryMCP(t)
			res := deliveryRun(t, s, context.Background(), tool.name, tool.fn, args)
			if !res.IsError || len(f.hits) != 0 {
				t.Errorf("%s %v: error=%v calls=%d, want refused before sending", tool.name, args, res.IsError, len(f.hits))
			}
		}
	}
	// ack takes no reason and drop takes no date.
	s, f := newDeliveryMCP(t)
	if res := deliveryRun(t, s, context.Background(), "delivery_ack", (*Server).handleDeliveryAck, map[string]any{"backlog": true, "reason": "x"}); !res.IsError {
		t.Error("delivery_ack took a reason")
	}
	if res := deliveryRun(t, s, context.Background(), "delivery_drop", (*Server).handleDeliveryDrop, map[string]any{"backlog": true, "reason": "x", "until": "2026-12-01"}); !res.IsError {
		t.Error("delivery_drop took a review date")
	}
	if len(f.hits) != 0 {
		t.Errorf("refused calls reached the daemon: %+v", f.hits)
	}
}

func TestDeliveryToolErrorsAreErrors(t *testing.T) {
	for _, tool := range deliveryToolCalls {
		s, f := newDeliveryMCP(t)
		f.status, f.reply = http.StatusServiceUnavailable, "the delivery check is not available"
		res := deliveryRun(t, s, context.Background(), tool.name, tool.fn, tool.args)
		if !res.IsError || !strings.Contains(deliveryText(res), "503") || !strings.Contains(deliveryText(res), "the delivery check is not available") {
			t.Errorf("%s: error=%v %q", tool.name, res.IsError, deliveryText(res))
		}
	}
}

func TestDeliveryToolsAreRegisteredAndDocumented(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(mgr)
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, tool := range tools {
		registered[tool.Name] = true
	}
	documented := map[string]ToolDoc{}
	for _, d := range s.ToolDocs() {
		documented[d.Name] = d
	}
	for _, c := range deliveryToolCalls {
		if !registered[c.name] {
			t.Errorf("%s is not registered", c.name)
		}
		d, ok := documented[c.name]
		if !ok || d.Description == "" {
			t.Errorf("%s is not documented", c.name)
		}
		if strings.Contains(d.Description, "PRD") {
			t.Errorf("%s says PRD", c.name)
		}
	}
	if ro := documented["delivery_status"].Annotations.ReadOnlyHint; ro == nil || !*ro {
		t.Error("delivery_status should be marked read-only")
	}
}

func TestDeliveryConfigSetNamesTheKeys(t *testing.T) {
	desc := (&Server{}).toolConfigSet().Description
	for _, key := range []string{"delivery.enabled", "delivery.notify", "delivery.interval_seconds"} {
		if !strings.Contains(desc, key) {
			t.Errorf("config_set does not name %s", key)
		}
	}
}

// config_set sends the daemon a value of the right type.
func TestDeliveryConfigSetSendsTheKey(t *testing.T) {
	s, f := newDeliveryMCP(t)
	f.reply = `{"status":"ok"}`
	res := deliveryRun(t, s, context.Background(), "config_set", (*Server).handleConfigSet,
		map[string]any{"key": "delivery.interval_seconds", "value": "600"})
	if res.IsError || len(f.hits) != 1 || f.hits[0].method != "PUT" || f.hits[0].body["delivery.interval_seconds"] != float64(600) {
		t.Fatalf("error=%v %q hits=%+v", res.IsError, deliveryText(res), f.hits)
	}
}
