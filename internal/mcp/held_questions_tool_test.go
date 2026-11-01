// Read-only held questions MCP tool tests (autonomous_held_questions).
// The fake daemon serves the /api/autonomous/held-questions report and
// records the request; the tool is a thin proxy, so the tests pin the
// passthrough (path, method, filters) and the tool-side shape (name,
// parameters, read-only hint, capability).

package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

const heldOnBody = `{
  "mode": "on",
  "states": ["blocked", "review_held", "revision_requested", "awaiting_decision", "pending_insert"],
  "open": [
    {"question_id": "q_held_1", "state": "awaiting_decision", "prd_id": "prd-7", "task_id": "t_9", "automation_id": "bot-scribe", "title": "Scribe Bot", "reason": "spec step 4 is ambiguous", "recommended_option": "revise", "default": "hold", "created_at": "2026-01-02T03:04:05Z"},
    {"question_id": "q_held_2", "state": "blocked", "prd_id": "prd-3", "reason": "two backends unreachable", "recommended_option": "escalate", "default": "hold"}
  ],
  "shadow": []
}`

// heldQuestionsRec is one recorded daemon request.
type heldQuestionsRec struct {
	method string
	path   string
	query  string
}

func heldQuestionsDaemon(t *testing.T, fixture string) (*Server, *heldQuestionsRec) {
	t.Helper()
	rec := &heldQuestionsRec{}
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		rec.method, rec.path, rec.query = r.Method, r.URL.Path, r.URL.RawQuery
		if r.Method != http.MethodGet {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte("method not allowed: only GET is supported here"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fixture))
	})
	return s, rec
}

func TestHeldQuestionsToolIsListed(t *testing.T) {
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
	found := false
	for _, tl := range tools {
		found = found || tl.Name == "autonomous_held_questions"
	}
	if !found {
		t.Error("tool \"autonomous_held_questions\" is not registered")
	}
}

func TestHeldQuestionsToolSchemaAndCapability(t *testing.T) {
	tool := (&Server{}).toolHeldQuestions()
	if tool.Name != "autonomous_held_questions" {
		t.Fatalf("name = %q, want autonomous_held_questions", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("autonomous_held_questions must be marked read-only")
	}
	props := tool.InputSchema.Properties
	if len(props) != 2 {
		t.Fatalf("want exactly prd_id and state, got %v", props)
	}
	if props["prd_id"] == nil || props["state"] == nil {
		t.Fatalf("want prd_id and state parameters, got %v", props)
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Fatalf("neither filter is required, got required %v", tool.InputSchema.Required)
	}
	if got := federation.MCPToolCap["autonomous_held_questions"]; got != federation.CapAutonomousRead {
		t.Fatalf("capability = %q, want %q", got, federation.CapAutonomousRead)
	}
}

func TestHeldQuestionsToolProxiesTheRead(t *testing.T) {
	s, rec := heldQuestionsDaemon(t, heldOnBody)
	res, err := s.handleHeldQuestions(context.Background(), callReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.IsError {
		t.Fatal(automationText(t, res), "want tool success")
	}
	if rec.method != http.MethodGet || rec.path != "/api/autonomous/held-questions" {
		t.Fatalf("proxied %s %s, want GET /api/autonomous/held-questions", rec.method, rec.path)
	}
	if rec.query != "" {
		t.Fatalf("no filters given, want an empty query, got %q", rec.query)
	}
	text := automationText(t, res)
	for _, want := range []string{`"mode": "on"`, `"question_id": "q_held_1"`, `"state": "awaiting_decision"`, `"reason": "spec step 4 is ambiguous"`, `"recommended_option": "revise"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("body = %s, want it to carry %s", text, want)
		}
	}
}

func TestHeldQuestionsToolSendsTheFilters(t *testing.T) {
	s, rec := heldQuestionsDaemon(t, heldOnBody)
	res, err := s.handleHeldQuestions(context.Background(), callReq(map[string]any{
		"prd_id": "prd-7",
		"state":  "blocked",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.IsError {
		t.Fatal(automationText(t, res), "want tool success")
	}
	for _, want := range []string{"prd_id=prd-7", "state=blocked"} {
		if !strings.Contains(rec.query, want) {
			t.Fatalf("query = %q, want it to carry %s", rec.query, want)
		}
	}
}

func TestHeldQuestionsToolReportsARejection(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("state must be one of the held question state ids: blocked, review_held, revision_requested, awaiting_decision, pending_insert (got \"nope\")"))
	})
	res, err := s.handleHeldQuestions(context.Background(), callReq(map[string]any{"state": "nope"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("err=%v res=%+v, want a tool error", err, res)
	}
	if !strings.Contains(automationText(t, res), "pending_insert") {
		t.Fatal(res, "want the rejected-state message, naming the held state ids")
	}
}

func TestHeldQuestionsToolReportsTheSwitchOff(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("operator questions are disabled: set autonomous.questions.enabled to true first (held questions live under the operator-question subsystem switch)"))
	})
	res, err := s.handleHeldQuestions(context.Background(), callReq(nil))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("err=%v res=%+v, want a tool error", err, res)
	}
	if !strings.Contains(automationText(t, res), "autonomous.questions.enabled") {
		t.Fatal(res, "want the switch-off message naming the switch")
	}
}

func TestHeldQuestionsToolEmptyLists(t *testing.T) {
	s, rec := heldQuestionsDaemon(t, `{
	  "mode": "on",
	  "states": ["blocked", "review_held", "revision_requested", "awaiting_decision", "pending_insert"],
	  "open": [],
	  "shadow": []
	}`)
	res, err := s.handleHeldQuestions(context.Background(), callReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.IsError {
		t.Fatal(automationText(t, res), "want tool success")
	}
	if rec.query != "" {
		t.Fatalf("no filters given, want an empty query, got %q", rec.query)
	}
	if text := automationText(t, res); !strings.Contains(text, `"open": []`) || !strings.Contains(text, `"shadow": []`) {
		t.Fatal(text, "want the empty open and shadow lists passed through")
	}
}
