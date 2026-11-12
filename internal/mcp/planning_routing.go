// Planning routing MCP tools: planning_routing_get, planning_routing_set,
// planning_scorecard and planning_routing_test. Each proxies the REST routes
// under /api/autonomous/planning-routing over the loopback, like the other
// autonomous tools, so every surface answers from the same code.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

const planningRoutingPath = "/api/autonomous/planning-routing"

func (s *Server) toolPlanningRoutingGet() mcpsdk.Tool {
	return mcpsdk.NewTool("planning_routing_get",
		mcpsdk.WithDescription("Read the planning routing config (enabled, shadow, rules, ladders, classes) and its validation result. "+
			"Routing is off by default; when on it picks the planner for an Automaton decompose per rule."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handlePlanningRoutingGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet(planningRoutingPath, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPlanningRoutingSet() mcpsdk.Tool {
	return mcpsdk.NewTool("planning_routing_set",
		mcpsdk.WithDescription("Update the planning routing config. Every field is optional: a field you leave out keeps its current value. "+
			"The result is validated first and a bad config is rejected, nothing applied. enabled turns routing on; "+
			"shadow records the routed decision but keeps the planner unchanged."),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Turn planning routing on or off")),
		mcpsdk.WithBoolean("shadow", mcpsdk.Description("Record the routed decision only; keep the planner unchanged")),
		mcpsdk.WithString("rules", mcpsdk.Description(`JSON array of rules, replacing the current rules: [{"name","match":{"kind","tags","project","risk_class","spec_size_min","spec_size_max","override"},"planners":[{"backend","model"}],"ladder"}]`)),
		mcpsdk.WithString("ladders", mcpsdk.Description(`JSON object of ladders by name: {"default":{"touch_up_max","replan_max","escalate_after_attempts"}}`)),
		mcpsdk.WithString("classes", mcpsdk.Description(`JSON object of risk classes by name: {"security":{"tags":[],"path_globs":[]}}`)),
	)
}

func (s *Server) handlePlanningRoutingSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	cur, err := s.proxyGet(planningRoutingPath, nil)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(cur, &body); err != nil {
		return nil, fmt.Errorf("planning routing config: %w", err)
	}
	delete(body, "validation")
	for _, k := range []string{"enabled", "shadow"} {
		v, ok := args[k]
		if !ok {
			continue
		}
		b, isBool := v.(bool)
		if !isBool {
			return nil, fmt.Errorf("%s: must be true or false", k)
		}
		body[k], _ = json.Marshal(b)
	}
	for _, k := range []string{"rules", "ladders", "classes"} {
		v := req.GetString(k, "")
		if v == "" {
			continue
		}
		if !json.Valid([]byte(v)) {
			return nil, fmt.Errorf("%s: invalid JSON", k)
		}
		body[k] = json.RawMessage(v)
	}
	raw, _ := json.Marshal(body)
	out, err := s.proxyJSON(http.MethodPut, planningRoutingPath, raw)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPlanningScorecard() mcpsdk.Tool {
	return mcpsdk.NewTool("planning_scorecard",
		mcpsdk.WithDescription("Counts-only planning scorecard and gate report per planner and rule class over a window: "+
			"decomposes, passes by attempt, escalations, errors and shadow would-route counts. No spec or task text."),
		mcpsdk.WithString("window", mcpsdk.Description("Window such as 36h or 7d (default 7d; 0 = all records)")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handlePlanningScorecard(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if w := req.GetString("window", ""); w != "" {
		q.Set("window", w)
	}
	out, err := s.proxyGet(planningRoutingPath+"/scorecard", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPlanningRoutingTest() mcpsdk.Tool {
	return mcpsdk.NewTool("planning_routing_test",
		mcpsdk.WithDescription("Route one input under the current planning routing config and return the decision "+
			"(rule, planners, ladder, reason). No LLM is called and nothing changes."),
		mcpsdk.WithString("kind", mcpsdk.Description("Routing input kind, e.g. feature or bug")),
		mcpsdk.WithString("tags", mcpsdk.Description("Comma-separated tags")),
		mcpsdk.WithNumber("spec_bytes", mcpsdk.Description("Spec size in bytes")),
		mcpsdk.WithString("files", mcpsdk.Description("Comma-separated repo-relative paths the tasks touch")),
		mcpsdk.WithString("project", mcpsdk.Description("Project identifier")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handlePlanningRoutingTest(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"kind":       req.GetString("kind", ""),
		"tags":       splitCommaList(req.GetString("tags", "")),
		"spec_bytes": int(req.GetFloat("spec_bytes", 0)),
		"files":      splitCommaList(req.GetString("files", "")),
		"project":    req.GetString("project", ""),
	}
	out, err := s.proxyJSON(http.MethodPost, planningRoutingPath+"/test", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// splitCommaList splits a comma-separated list, trimming blanks. It returns
// an empty (not nil) slice so the REST body carries [] rather than null.
func splitCommaList(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
