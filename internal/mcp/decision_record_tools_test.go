package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// decisionRecordTestDaemon is a stand-in daemon for the decision-record tools:
// it records method, path, query and body of the last proxied request and
// answers from canned JSON.
type decisionRecordTestDaemon struct {
	s         *Server
	method    string
	path      string
	query     map[string][]string
	rawBody   string
	status    int
	statusMsg string
}

func newDecisionRecordTestDaemon(t *testing.T, status int, msg string) *decisionRecordTestDaemon {
	t.Helper()
	d := &decisionRecordTestDaemon{status: status, statusMsg: msg}
	d.s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		d.method, d.path, d.query = r.Method, r.URL.Path, r.URL.Query()
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			d.rawBody = string(b)
		}
		if d.status != 0 {
			w.WriteHeader(d.status)
			_, _ = w.Write([]byte(d.statusMsg))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"record":{"id":"rec-1","state":"open","question_id":"q-1"},"question_id":"q-1"}`))
	})
	return d
}

func (d *decisionRecordTestDaemon) call(t *testing.T, handler func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error), args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := handler(context.Background(), callReq(args))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return res
}

func (d *decisionRecordTestDaemon) bodyMap(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(d.rawBody), &m); err != nil {
		t.Fatalf("decode body %q: %v", d.rawBody, err)
	}
	return m
}

// TestDecisionRecordMCP_Registered checks the five tools exist, are read-only
// where expected, and sit in the federation capability map with the same
// split as the REST gate.
func TestDecisionRecordMCP_Registered(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]bool{}
	for _, tl := range tools {
		byName[tl.Name] = true
	}
	for _, name := range DecisionRecordToolNames() {
		if !byName[name] {
			t.Errorf("tool %q is not registered", name)
		}
	}
	// Read-only hints live on the tool itself, not on the compact descriptors
	// of ListTools, so check the built tools directly (the pattern of
	// TestDecisionsMCP).
	built := map[string]mcpsdk.Tool{
		"autonomous_decision_record_list":     s.toolDecisionRecordList(),
		"autonomous_decision_record_get":      s.toolDecisionRecordGet(),
		"autonomous_decision_record_create":   s.toolDecisionRecordCreate(),
		"autonomous_decision_record_update":   s.toolDecisionRecordUpdate(),
		"autonomous_decision_record_withdraw": s.toolDecisionRecordWithdraw(),
	}
	wantRO := map[string]bool{
		"autonomous_decision_record_list":     true,
		"autonomous_decision_record_get":      true,
		"autonomous_decision_record_create":   false,
		"autonomous_decision_record_update":   false,
		"autonomous_decision_record_withdraw": false,
	}
	for name, want := range wantRO {
		hint := built[name].Annotations.ReadOnlyHint
		if hint == nil || *hint != want {
			t.Errorf("%q read-only hint = %v, want %v", name, hint, want)
		}
	}
	wantCap := map[string]string{
		"autonomous_decision_record_list":     federation.CapAutonomousRead,
		"autonomous_decision_record_get":      federation.CapAutonomousRead,
		"autonomous_decision_record_create":   federation.CapAutonomousWrite,
		"autonomous_decision_record_update":   federation.CapAutonomousWrite,
		"autonomous_decision_record_withdraw": federation.CapAutonomousWrite,
	}
	for name, cap := range wantCap {
		if got, ok := federation.MCPToolCap[name]; !ok {
			t.Errorf("federation.MCPToolCap[%q] missing", name)
		} else if got != cap {
			t.Errorf("federation.MCPToolCap[%q] = %q, want %q", name, got, cap)
		}
	}
}

// TestDecisionRecordMCP_ListForwardsFilters checks the list tool proxies GET
// with state/prd_id/limit and passes the daemon's body through.
func TestDecisionRecordMCP_ListForwardsFilters(t *testing.T) {
	d := newDecisionRecordTestDaemon(t, 0, "")
	res := d.call(t, d.s.handleDecisionRecordList, map[string]any{
		"state": "open", "prd_id": "d0442737d38c", "limit": 25,
	})
	if res.IsError {
		t.Fatalf("tool error: %v", res)
	}
	if d.method != http.MethodGet || d.path != "/api/autonomous/decision-records" {
		t.Errorf("proxied %s %s", d.method, d.path)
	}
	for key, want := range map[string]string{"state": "open", "prd_id": "d0442737d38c", "limit": "25"} {
		if q := d.query[key]; len(q) != 1 || q[0] != want {
			t.Errorf("query %s = %v, want [%s]", key, q, want)
		}
	}
	if d.rawBody != "" {
		t.Errorf("list sent a body: %q", d.rawBody)
	}
}

// TestDecisionRecordMCP_ListRefusesFractionalLimit checks a non-whole limit
// is refused before anything is proxied.
func TestDecisionRecordMCP_ListRefusesFractionalLimit(t *testing.T) {
	d := newDecisionRecordTestDaemon(t, 0, "")
	res := d.call(t, d.s.handleDecisionRecordList, map[string]any{"limit": 1.5})
	if !res.IsError {
		t.Fatal("a fractional limit must come back as a tool error")
	}
	if d.method != "" {
		t.Errorf("refused locally but the daemon was hit: %s %s", d.method, d.path)
	}
}

// TestDecisionRecordMCP_CreateForwardsContent checks the create tool proxies
// POST with the content fields, options, recommendation and links.
func TestDecisionRecordMCP_CreateForwardsContent(t *testing.T) {
	d := newDecisionRecordTestDaemon(t, 0, "")
	res := d.call(t, d.s.handleDecisionRecordCreate, map[string]any{
		"label":          "storage choice",
		"title":          "Pick the question storage",
		"situation":      "The question store is in-memory",
		"deciding":       "Which store to move to",
		"options":        `[{"id":"jsonl","label":"JSONL","tradeoff":"simple, single file"},{"id":"sqlite","label":"SQLite","tradeoff":"new dependency"}]`,
		"recommendation": `{"option_id":"jsonl","reason":"no new dependency"}`,
		"default":        "jsonl",
		"links":          `{"automaton":"d0442737d38c"}`,
	})
	if res.IsError {
		t.Fatalf("tool error: %v", res)
	}
	if d.method != http.MethodPost || d.path != "/api/autonomous/decision-records" {
		t.Errorf("proxied %s %s", d.method, d.path)
	}
	body := d.bodyMap(t)
	for key, want := range map[string]any{
		"label": "storage choice", "title": "Pick the question storage",
		"situation": "The question store is in-memory", "deciding": "Which store to move to",
		"default": "jsonl",
	} {
		if body[key] != want {
			t.Errorf("body[%s] = %v, want %v", key, body[key], want)
		}
	}
	opts, ok := body["options"].([]any)
	if !ok || len(opts) != 2 {
		t.Fatalf("options = %v, want two entries", body["options"])
	}
	if first, _ := opts[0].(map[string]any); first["id"] != "jsonl" {
		t.Errorf("options[0] = %v", opts[0])
	}
	rec, _ := body["recommendation"].(map[string]any)
	if rec["option_id"] != "jsonl" || rec["reason"] != "no new dependency" {
		t.Errorf("recommendation = %v", body["recommendation"])
	}
	links, _ := body["links"].(map[string]any)
	if links["automaton"] != "d0442737d38c" {
		t.Errorf("links = %v", body["links"])
	}
}

// TestDecisionRecordMCP_UpdateHitsPutPath checks the update tool proxies PUT
// to the item path and requires an id.
func TestDecisionRecordMCP_UpdateHitsPutPath(t *testing.T) {
	d := newDecisionRecordTestDaemon(t, 0, "")
	res := d.call(t, d.s.handleDecisionRecordUpdate, map[string]any{
		"id":      "rec-1",
		"title":   "Pick the question storage",
		"options": `[{"id":"jsonl","label":"JSONL"}]`,
	})
	if res.IsError {
		t.Fatalf("tool error: %v", res)
	}
	if d.method != http.MethodPut || d.path != "/api/autonomous/decision-records/rec-1" {
		t.Errorf("proxied %s %s", d.method, d.path)
	}
	body := d.bodyMap(t)
	if body["title"] != "Pick the question storage" {
		t.Errorf("body[title] = %v", body["title"])
	}
	if _, ok := body["options"].([]any); !ok {
		t.Errorf("options = %v, want an array", body["options"])
	}
	// No id: refused locally, the daemon is never hit.
	d2 := newDecisionRecordTestDaemon(t, 0, "")
	res2 := d2.call(t, d2.s.handleDecisionRecordUpdate, map[string]any{"title": "t"})
	if !res2.IsError {
		t.Fatal("an update without id must come back as a tool error")
	}
	if d2.method != "" {
		t.Errorf("refused locally but the daemon was hit: %s %s", d2.method, d2.path)
	}
}

// TestDecisionRecordMCP_WithdrawHitsPostPath checks the withdraw tool proxies
// POST to the withdraw path and sends the reason when given.
func TestDecisionRecordMCP_WithdrawHitsPostPath(t *testing.T) {
	d := newDecisionRecordTestDaemon(t, 0, "")
	res := d.call(t, d.s.handleDecisionRecordWithdraw, map[string]any{
		"id": "rec-1", "reason": "superseded by a newer plan",
	})
	if res.IsError {
		t.Fatalf("tool error: %v", res)
	}
	if d.method != http.MethodPost || d.path != "/api/autonomous/decision-records/rec-1/withdraw" {
		t.Errorf("proxied %s %s", d.method, d.path)
	}
	body := d.bodyMap(t)
	if body["reason"] != "superseded by a newer plan" {
		t.Errorf("body[reason] = %v", body["reason"])
	}
	// Without a reason the body must be empty: the daemon accepts an empty
	// withdrawal body and attribution is the caller of record.
	d2 := newDecisionRecordTestDaemon(t, 0, "")
	res2 := d2.call(t, d2.s.handleDecisionRecordWithdraw, map[string]any{"id": "rec-1"})
	if res2.IsError {
		t.Fatalf("tool error: %v", res2)
	}
	if d2.rawBody != "" {
		t.Errorf("withdraw without reason sent a body: %q", d2.rawBody)
	}
}

// TestDecisionRecordMCP_DaemonErrorsComeThrough checks daemon 404/503
// failures surface as tool errors carrying the daemon's message, and the get
// tool escapes the id on its way to the daemon.
func TestDecisionRecordMCP_DaemonErrorsComeThrough(t *testing.T) {
	d := newDecisionRecordTestDaemon(t, http.StatusNotFound, `{"error":"unknown decision record"}`)
	res := d.call(t, d.s.handleDecisionRecordGet, map[string]any{"id": "nope"})
	if !res.IsError {
		t.Fatal("a 404 from the daemon must come back as a tool error")
	}

	d2 := newDecisionRecordTestDaemon(t, http.StatusServiceUnavailable, `{"error":"decision records are not available"}`)
	res2 := d2.call(t, d2.s.handleDecisionRecordList, nil)
	if !res2.IsError {
		t.Fatal("a 503 from the daemon must come back as a tool error")
	}
}

// TestDecisionRecordMCP_QuestionAnswerForwardsFollowUp checks the new
// follow_up parameter of question_answer lands in the proxied body only when
// it was given.
func TestDecisionRecordMCP_QuestionAnswerForwardsFollowUp(t *testing.T) {
	d := newDecisionRecordTestDaemon(t, 0, "")
	res := d.call(t, d.s.handleQuestionAnswer, map[string]any{
		"id": "q-1", "quote": "go with jsonl", "follow_up": "open PR",
	})
	if res.IsError {
		t.Fatalf("tool error: %v", res)
	}
	if d.method != http.MethodPost || d.path != "/api/questions/q-1/answer" {
		t.Errorf("proxied %s %s", d.method, d.path)
	}
	body := d.bodyMap(t)
	if body["follow_up"] != "open PR" {
		t.Errorf("body[follow_up] = %v", body["follow_up"])
	}

	d2 := newDecisionRecordTestDaemon(t, 0, "")
	res2 := d2.call(t, d2.s.handleQuestionAnswer, map[string]any{"id": "q-1", "quote": "go with jsonl"})
	if res2.IsError {
		t.Fatalf("tool error: %v", res2)
	}
	if _, ok := d2.bodyMap(t)["follow_up"]; ok {
		t.Errorf("follow_up sent when the argument was not given: %v", d2.bodyMap(t))
	}
}
