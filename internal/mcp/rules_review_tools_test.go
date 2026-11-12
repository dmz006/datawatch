package mcp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestRulesReviewToolDescriptionsCarryNewWording(t *testing.T) {
	s := &Server{}
	add := s.toolAutonomousPRDAddTask()
	if d := add.Description; !strings.Contains(d, "needs_review, revisions_asked and cancelled") || !strings.Contains(d, "parked in needs_review") {
		t.Fatalf("add_task description lacks new wording: %q", d)
	}
	story := s.toolAutonomousPRDAddStory()
	if !strings.Contains(story.Description, "parked in needs_review") {
		t.Fatalf("add_story description: %q", story.Description)
	}
	if !strings.Contains(s.toolAutonomousAttentionList().Description, "rules_review_outcome") {
		t.Fatal("attention list description lacks rules_review_outcome")
	}
	if !strings.Contains(s.toolAutonomousPRDGet().Description, "rules_review_outcome") {
		t.Fatal("prd_get description lacks rules_review_outcome")
	}
	if !strings.Contains(s.toolAutonomousStatus().Description, "rules_review_outcome") {
		t.Fatal("status description lacks rules_review_outcome")
	}
	cfg := s.toolAutonomousConfigSet()
	raw, ok := cfg.InputSchema.Properties["auto_fix_retries"].(map[string]any)
	if !ok {
		t.Fatalf("auto_fix_retries missing from config_set schema")
	}
	if raw["type"] != "number" {
		t.Fatalf("auto_fix_retries type = %v, want number", raw["type"])
	}
	if d, _ := raw["description"].(string); !strings.Contains(d, "goes to needs_review with the findings attached") {
		t.Fatalf("auto_fix_retries description: %q", d)
	}
}

func TestRulesReviewAttentionListReturnsOutcomeUntouched(t *testing.T) {
	const reply = `{"statuses":["needs_review"],"items":[{"id":"a1","rules_review_outcome":{"action":"hold","findings":["missing tests"],"retries":1}}]}`
	var gotMethod, gotPath string
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	defer rest.Close()
	_, portStr, _ := net.SplitHostPort(rest.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	s := &Server{webPort: port}
	res, err := s.handleAutonomousAttentionList(context.Background(), tagReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("attention list: %v %v", err, res)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/autonomous/attention" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if text != reply {
		t.Fatalf("reply changed: %s", text)
	}
}
