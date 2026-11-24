// MCP surface tests for autonomous_prd_set_outcome, autonomous_outcome_check,
// autonomous_deploy_record and the outcome arguments of autonomous_prd_cancel:
// each proxies the matching REST route.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

type outcomeSeen struct {
	method, path, query string
	body                map[string]any
}

func outcomeServer(t *testing.T) (*Server, *outcomeSeen) {
	seen := &outcomeSeen{}
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen.method, seen.path, seen.query = r.Method, r.URL.Path, r.URL.RawQuery
		seen.body = nil
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &seen.body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	return s, seen
}

func TestClosedOutcomeMCP_SetOutcome(t *testing.T) {
	s, seen := outcomeServer(t)
	res, err := s.handleAutonomousPRDSetOutcome(context.Background(), callReq(map[string]any{
		"id": "p1", "outcome": "delivered", "delivered_via": "p2", "note": "twin",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if seen.method != http.MethodPost || seen.path != "/api/autonomous/prds/p1/outcome" {
		t.Fatalf("proxied %s %s", seen.method, seen.path)
	}
	if seen.body["outcome"] != "delivered" || seen.body["delivered_via"] != "p2" || seen.body["note"] != "twin" {
		t.Errorf("body = %+v", seen.body)
	}
	if _, err := s.handleAutonomousPRDSetOutcome(context.Background(), callReq(map[string]any{"id": "p1", "outcome": "dropped", "archive": true})); err != nil {
		t.Fatal(err)
	}
	if seen.path != "/api/autonomous/prds/p1/archive" || seen.body["outcome"] != "dropped" {
		t.Errorf("archive=true must post to .../archive, got %s %+v", seen.path, seen.body)
	}
	res, _ = s.handleAutonomousPRDSetOutcome(context.Background(), callReq(map[string]any{"id": "p1"}))
	if res == nil || !res.IsError {
		t.Error("a call with no outcome must be a tool error")
	}
}

func TestClosedOutcomeMCP_OutcomeCheckAndDeployRecord(t *testing.T) {
	s, seen := outcomeServer(t)
	if _, err := s.handleAutonomousOutcomeCheck(context.Background(), callReq(map[string]any{})); err != nil {
		t.Fatal(err)
	}
	if seen.method != http.MethodGet || seen.path != "/api/autonomous/outcome-check" {
		t.Fatalf("read proxied %s %s", seen.method, seen.path)
	}
	if _, err := s.handleAutonomousOutcomeCheck(context.Background(), callReq(map[string]any{"run": true})); err != nil {
		t.Fatal(err)
	}
	if seen.method != http.MethodPost || seen.path != "/api/autonomous/outcome-check" {
		t.Fatalf("run proxied %s %s", seen.method, seen.path)
	}
	res, err := s.handleAutonomousDeployRecord(context.Background(), callReq(map[string]any{"release": "v9.1.0", "host": "h1", "health": "passed"}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if seen.method != http.MethodPost || seen.path != "/api/autonomous/deploy/record" || seen.body["release"] != "v9.1.0" || seen.body["host"] != "h1" || seen.body["health"] != "passed" {
		t.Errorf("proxied %s %s %+v", seen.method, seen.path, seen.body)
	}
	if res, _ := s.handleAutonomousDeployRecord(context.Background(), callReq(map[string]any{})); res == nil || !res.IsError {
		t.Error("a deploy record with no release must be a tool error")
	}
}

func TestClosedOutcomeMCP_CancelCarriesOutcome(t *testing.T) {
	s, seen := outcomeServer(t)
	if _, err := s.handleAutonomousPRDCancel(context.Background(), callReq(map[string]any{"id": "p1", "outcome": "superseded", "delivered_via": "p2"})); err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(seen.query)
	if seen.method != http.MethodDelete || seen.path != "/api/autonomous/prds/p1" || q.Get("delivered_via") != "p2" || q.Get("outcome") != "superseded" {
		t.Fatalf("proxied %s %s ?%s", seen.method, seen.path, seen.query)
	}
	if _, err := s.handleAutonomousPRDCancel(context.Background(), callReq(map[string]any{"id": "p1"})); err != nil {
		t.Fatal(err)
	}
	q, _ = url.ParseQuery(seen.query)
	if q.Get("outcome") != "" || q.Get("delivered_via") != "" {
		t.Errorf("a plain cancel must send no outcome, got %q", seen.query)
	}
}

func TestClosedOutcomeMCP_ToolsDeclared(t *testing.T) {
	s, _ := outcomeServer(t)
	for name, tool := range map[string]mcpsdk.Tool{
		"autonomous_prd_set_outcome": s.toolAutonomousPRDSetOutcome(),
		"autonomous_outcome_check":   s.toolAutonomousOutcomeCheck(),
		"autonomous_deploy_record":   s.toolAutonomousDeployRecord(),
	} {
		if tool.Name != name {
			t.Errorf("tool %s declared as %s", name, tool.Name)
		}
		if strings.Contains(tool.Description, "PRD") {
			t.Errorf("tool %s description says PRD; user-facing text says Automaton", name)
		}
	}
}
