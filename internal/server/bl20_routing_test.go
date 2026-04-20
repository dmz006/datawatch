// BL20 — routing rules tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestBL20_MatchRoutingRule_FirstMatchWins(t *testing.T) {
	rules := []config.RoutingRule{
		{Pattern: `(?i)test`, Backend: "claude-code"},
		{Pattern: `(?i)quick`, Backend: "ollama"},
		{Pattern: `.*`, Backend: "shell"},
	}
	if r := MatchRoutingRule(rules, "add tests"); r == nil || r.Backend != "claude-code" {
		t.Errorf("test should match claude-code: %+v", r)
	}
	if r := MatchRoutingRule(rules, "quick lookup"); r == nil || r.Backend != "ollama" {
		t.Errorf("quick should match ollama: %+v", r)
	}
	if r := MatchRoutingRule(rules, "anything else"); r == nil || r.Backend != "shell" {
		t.Errorf("catch-all .* should match: %+v", r)
	}
}

func TestBL20_MatchRoutingRule_NoMatch(t *testing.T) {
	rules := []config.RoutingRule{{Pattern: `^build$`, Backend: "claude-code"}}
	if r := MatchRoutingRule(rules, "rebuild"); r != nil {
		t.Errorf("anchored pattern should not match: %+v", r)
	}
}

func TestBL20_MatchRoutingRule_InvalidRegexSkipped(t *testing.T) {
	rules := []config.RoutingRule{
		{Pattern: `(unbalanced`, Backend: "skipped"},
		{Pattern: `valid`, Backend: "kept"},
	}
	if r := MatchRoutingRule(rules, "valid"); r == nil || r.Backend != "kept" {
		t.Errorf("invalid regex should be skipped, valid one kept: %+v", r)
	}
}

func TestBL20_RoutingRules_GetEmpty(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/routing-rules", nil)
	rr := httptest.NewRecorder()
	s.handleRoutingRules(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	// rules can be nil or empty array
	if rules, ok := got["rules"].([]any); ok && len(rules) != 0 {
		t.Errorf("expected empty rules: %+v", got)
	}
}

func TestBL20_RoutingRules_PostSavesRules(t *testing.T) {
	s := bl90Server(t)
	body := `{"rules":[{"pattern":"test","backend":"claude-code","description":"test work"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/routing-rules", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleRoutingRules(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("post: %d %s", rr.Code, rr.Body.String())
	}
	if len(s.cfg.Session.RoutingRules) != 1 {
		t.Errorf("rules not persisted: %+v", s.cfg.Session.RoutingRules)
	}
}

func TestBL20_RoutingRules_PostInvalidRegexRejected(t *testing.T) {
	s := bl90Server(t)
	body := `{"rules":[{"pattern":"(broken","backend":"claude-code"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/routing-rules", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleRoutingRules(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 for invalid regex, got %d", rr.Code)
	}
}

func TestBL20_RoutingRules_TestEndpoint(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Session.RoutingRules = []config.RoutingRule{
		{Pattern: `(?i)deploy`, Backend: "claude-code"},
	}
	body := `{"task":"deploy the app"}`
	req := httptest.NewRequest(http.MethodPost, "/api/routing-rules/test", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleRoutingRulesTest(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got["matched"] != true {
		t.Errorf("expected matched=true, got %+v", got)
	}
	if got["backend"] != "claude-code" {
		t.Errorf("backend mismatch: %+v", got)
	}
}

func TestBL20_RoutingRules_TestNoMatch(t *testing.T) {
	s := bl90Server(t)
	body := `{"task":"random task"}`
	req := httptest.NewRequest(http.MethodPost, "/api/routing-rules/test", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleRoutingRulesTest(rr, req)
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got["matched"] != false {
		t.Errorf("expected matched=false, got %+v", got)
	}
}
