// MCP tool tests for the question channel: each handler proxies its REST
// route to an httptest server standing in for the daemon.
package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestQuestionToolDefinitions(t *testing.T) {
	s := &Server{}
	tools := map[string]mcpsdk.Tool{
		"question_raise":    s.toolQuestionRaise(),
		"question_list":     s.toolQuestionList(),
		"question_get":      s.toolQuestionGet(),
		"question_answer":   s.toolQuestionAnswer(),
		"question_withdraw": s.toolQuestionWithdraw(),
	}
	for name, tool := range tools {
		if tool.Name != name {
			t.Errorf("tool name = %q, want %q", tool.Name, name)
		}
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("%s: missing description", name)
		}
	}
	// At least two descriptions must name the disabled-switch 503.
	fiftyThird := 0
	for name, tool := range tools {
		if strings.Contains(tool.Description, "503") && strings.Contains(tool.Description, "autonomous.questions.enabled") {
			fiftyThird++
		} else {
			t.Logf("%s description does not mention the disabled-switch 503: %s", name, tool.Description)
		}
	}
	if fiftyThird < 2 {
		t.Errorf("only %d descriptions mention the autonomous.questions.enabled 503, want >= 2", fiftyThird)
	}

	required := func(name string, want []string) {
		t.Helper()
		got := make([]string, 0, len(tools[name].InputSchema.Required))
		for _, r := range tools[name].InputSchema.Required {
			got = append(got, r)
		}
		if len(got) != len(want) {
			t.Errorf("%s: required = %v, want %v", name, got, want)
			return
		}
		for i, w := range want {
			if got[i] != w {
				t.Errorf("%s: required[%d] = %q, want %q", name, i, got[i], w)
			}
		}
	}
	required("question_raise", []string{"text", "options"})
	required("question_list", nil)
	required("question_get", []string{"id"})
	required("question_answer", []string{"id"})
	required("question_withdraw", []string{"id", "actor"})
}

// proxyCapture stands in for the daemon: records the request and returns
// a fixed body with the passed status.
func proxyCapture(status int, body string) (*httptest.Server, *capturedREST) {
	cap := &capturedREST{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	return srv, cap
}

type capturedREST struct {
	method string
	path   string
	query  string
	body   string
}

func (c *capturedREST) record(r *http.Request) {
	c.method = r.Method
	c.path = r.URL.Path
	c.query = r.URL.RawQuery
	b, _ := io.ReadAll(r.Body)
	c.body = string(b)
}

// wantToolError asserts the handler refused without touching the daemon.
func wantToolError(t *testing.T, res *mcpsdk.CallToolResult, err error, args map[string]any, called bool) {
	t.Helper()
	if err != nil || res == nil || !res.IsError {
		t.Errorf("args %v: want a tool error, got err=%v res=%v", args, err, res)
	}
	if called {
		t.Error("an invalid call reached the daemon")
	}
}

func TestQuestionRaiseProxiesREST(t *testing.T) {
	srv, cap := proxyCapture(http.StatusCreated, `{"id":"q9","text":"Ship it?","state":"open"}`)
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"text":        "Ship it?",
		"options":     "yes,no",
		"asker":       "operator",
		"context":     "prd",
		"prd_id":      "p1",
		"asker_kind":  "supervisor",
		"return_path": true,
	}
	res, err := s.handleQuestionRaise(t.Context(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if cap.method != http.MethodPost || cap.path != "/api/questions" {
		t.Fatalf("got %s %s, want POST /api/questions", cap.method, cap.path)
	}
	for _, want := range []string{
		`"text":"Ship it?"`,
		`"asked_by":"operator"`,
		`"context":"prd"`,
		`"prd_id":"p1"`,
		`"asker_kind":"supervisor"`,
		`"return_path":true`,
		`"options":[`,
		`"id":"yes","label":"yes"`,
		`"id":"no","label":"no"`,
	} {
		if !strings.Contains(cap.body, want) {
			t.Errorf("body %q lacks %q", cap.body, want)
		}
	}
	if out := resContentText(t, res); !strings.Contains(out, `"q9"`) {
		t.Errorf("result = %s", out)
	}
}

func TestQuestionRaiseObjectOptions(t *testing.T) {
	srv, cap := proxyCapture(http.StatusCreated, `{"id":"q10"}`)
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"text": "How much headroom?",
		"options": []any{
			"auto",
			map[string]any{"id": "tight", "label": "Tight", "tradeoffs": "faster, riskier"},
		},
	}
	if res, err := s.handleQuestionRaise(t.Context(), req); err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	for _, want := range []string{
		`"id":"auto","label":"auto"`,
		`"id":"tight","label":"Tight","tradeoffs":"faster, riskier"`,
	} {
		if !strings.Contains(cap.body, want) {
			t.Errorf("body %q lacks %q", cap.body, want)
		}
	}
}

func TestQuestionRaiseValidatesArgs(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}

	cases := []map[string]any{
		{},
		{"text": "  "},
		{"text": "Question?"}, // no options
		{"text": "Question?", "options": []any{}},                 // empty options
		{"text": "Question?", "options": "   "},                   // blank options
		{"text": "Question?", "options": []any{map[string]any{}}}, // option without an id
	}
	for _, args := range cases {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = args
		res, err := s.handleQuestionRaise(t.Context(), req)
		wantToolError(t, res, err, args, called)
	}
	if called {
		t.Error("an invalid call reached the daemon")
	}
}

func TestQuestionListProxiesREST(t *testing.T) {
	srv, cap := proxyCapture(http.StatusOK, `{"count":1,"questions":[{"id":"q1"}]}`)
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"state": "open", "asker": "ops:1", "limit": float64(5)}
	res, err := s.handleQuestionList(t.Context(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if cap.method != http.MethodGet || cap.path != "/api/questions" {
		t.Fatalf("got %s %s, want GET /api/questions", cap.method, cap.path)
	}
	for _, want := range []string{"state=open", "asker=ops%3A1", "limit=5"} {
		if !strings.Contains(cap.query, want) {
			t.Errorf("query %q lacks %q", cap.query, want)
		}
	}
	if out := resContentText(t, res); !strings.Contains(out, `"q1"`) {
		t.Errorf("result = %s", out)
	}
}

func TestQuestionListNoFiltersProxiesBareCollection(t *testing.T) {
	srv, cap := proxyCapture(http.StatusOK, `{"count":0,"questions":[]}`)
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	if res, err := s.handleQuestionList(t.Context(), req); err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if cap.method != http.MethodGet || cap.path != "/api/questions" || cap.query != "" {
		t.Errorf("got %s %s?%s, want GET /api/questions with no query", cap.method, cap.path, cap.query)
	}
}

func TestQuestionListValidatesLimit(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	for _, args := range []map[string]any{
		{"limit": float64(0)},
		{"limit": float64(-3)},
		{"limit": 2.5},
	} {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = args
		res, err := s.handleQuestionList(t.Context(), req)
		wantToolError(t, res, err, args, called)
	}
}

func TestQuestionGetProxiesREST(t *testing.T) {
	srv, cap := proxyCapture(http.StatusOK, `{"id":"q-7","answer_count":2,"current_answer_id":"a1"}`)
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"id": "q-7"}
	res, err := s.handleQuestionGet(t.Context(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if cap.method != http.MethodGet || cap.path != "/api/questions/q-7" {
		t.Fatalf("got %s %s, want GET /api/questions/q-7", cap.method, cap.path)
	}
	if out := resContentText(t, res); !strings.Contains(out, `"current_answer_id":"a1"`) {
		t.Errorf("result = %s", out)
	}
}

func TestQuestionGetMissingID(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	for _, args := range []map[string]any{{}, {"id": "   "}} {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = args
		res, err := s.handleQuestionGet(t.Context(), req)
		wantToolError(t, res, err, args, called)
	}
}

func TestQuestionAnswerProxiesREST(t *testing.T) {
	srv, cap := proxyCapture(http.StatusCreated, `{"id":"a1","question_id":"q7","surface":"mcp"}`)
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"id":                "q7",
		"chosen_option_id":  "yes",
		"supersedes":        "a0",
		"actor":             "operator",
		"source_message_id": "msg-9",
	}
	res, err := s.handleQuestionAnswer(t.Context(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if cap.method != http.MethodPost || cap.path != "/api/questions/q7/answer" {
		t.Fatalf("got %s %s, want POST /api/questions/q7/answer", cap.method, cap.path)
	}
	for _, want := range []string{
		`"surface":"mcp"`,
		`"chosen_option_id":"yes"`,
		`"supersedes":"a0"`,
		`"actor":"operator"`,
		`"source_message_id":"msg-9"`,
	} {
		if !strings.Contains(cap.body, want) {
			t.Errorf("body %q lacks %q", cap.body, want)
		}
	}
	if strings.Contains(cap.body, "quote") {
		t.Errorf("body %q unexpectedly carries a quote", cap.body)
	}
	if out := resContentText(t, res); !strings.Contains(out, `"a1"`) {
		t.Errorf("result = %s", out)
	}
}

func TestQuestionAnswerQuoteOnly(t *testing.T) {
	srv, cap := proxyCapture(http.StatusCreated, `{"id":"a2"}`)
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"id": "q7", "quote": "yes, ship it"}
	if res, err := s.handleQuestionAnswer(t.Context(), req); err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if !strings.Contains(cap.body, `"quote":"yes, ship it"`) {
		t.Errorf("body %q lacks the quote", cap.body)
	}
	if strings.Contains(cap.body, "chosen_option_id") {
		t.Errorf("body %q unexpectedly carries a chosen_option_id", cap.body)
	}
}

func TestQuestionAnswerValidatesArgs(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	cases := []map[string]any{
		{},
		{"id": "  "},
		{"id": "q"}, // no choice and no words
		{"id": "q", "chosen_option_id": "  ", "quote": "   "},                // both blank
		{"id": "q", "chosen_option_id": "  ", "quote": "  ", "actor": "ops"}, // still neither
	}
	for _, args := range cases {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = args
		res, err := s.handleQuestionAnswer(t.Context(), req)
		wantToolError(t, res, err, args, called)
	}
	if called {
		t.Error("an invalid call reached the daemon")
	}
}

func TestQuestionWithdrawProxiesREST(t *testing.T) {
	srv, cap := proxyCapture(http.StatusOK, `{"id":"q7","state":"withdrawn","actor":"operator","reason":"duplicated"}`)
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"id": "q7", "actor": "operator", "reason": "duplicated"}
	res, err := s.handleQuestionWithdraw(t.Context(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if cap.method != http.MethodPost || cap.path != "/api/questions/q7/withdraw" {
		t.Fatalf("got %s %s, want POST /api/questions/q7/withdraw", cap.method, cap.path)
	}
	if !strings.Contains(cap.body, `"actor":"operator"`) || !strings.Contains(cap.body, `"reason":"duplicated"`) {
		t.Errorf("body %q lacks actor/reason", cap.body)
	}
	if out := resContentText(t, res); !strings.Contains(out, `"withdrawn"`) {
		t.Errorf("result = %s", out)
	}
}

func TestQuestionWithdrawValidatesArgs(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	cases := []map[string]any{
		{},
		{"id": "q"},
		{"actor": "operator"},
		{"id": "  ", "actor": "  "},
	}
	for _, args := range cases {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = args
		res, err := s.handleQuestionWithdraw(t.Context(), req)
		wantToolError(t, res, err, args, called)
	}
	if called {
		t.Error("an invalid call reached the daemon")
	}
}

// TestQuestionToolsSurfaceDisabled503: with the autonomous.questions.enabled
// switch off the daemon 503s; every handler must surface that as a tool
// error (not a success) and include the switch name in the message.
func TestQuestionToolsSurfaceDisabled503(t *testing.T) {
	const body = `{"error":"questions disabled (autonomous.questions.enabled is off)"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}

	calls := []struct {
		name string
		fn   func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
		args map[string]any
	}{{
		"question_raise", s.handleQuestionRaise,
		map[string]any{"text": "Ship it?", "options": "yes,no"},
	}, {
		"question_list", s.handleQuestionList,
		map[string]any{},
	}, {
		"question_get", s.handleQuestionGet,
		map[string]any{"id": "q1"},
	}, {
		"question_answer", s.handleQuestionAnswer,
		map[string]any{"id": "q1", "chosen_option_id": "yes"},
	}, {
		"question_withdraw", s.handleQuestionWithdraw,
		map[string]any{"id": "q1", "actor": "operator"},
	}}
	for _, c := range calls {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = c.args
		res, err := c.fn(t.Context(), req)
		if err != nil || res == nil || !res.IsError {
			t.Errorf("%s: want a tool error for the disabled switch, got err=%v res=%v", c.name, err, res)
			continue
		}
		out := resContentText(t, res)
		if !strings.Contains(out, "503") {
			t.Errorf("%s: error %q does not carry the 503 status", c.name, out)
		}
		if !strings.Contains(out, "autonomous.questions.enabled") {
			t.Errorf("%s: error %q does not name the autonomous.questions.enabled switch", c.name, out)
		}
	}
}
