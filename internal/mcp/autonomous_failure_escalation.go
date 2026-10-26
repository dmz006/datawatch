// MCP tools for failure escalation on one Automaton: set or clear the
// failure escalation override (on the Automaton or one task), and resolve a
// task held for operator review. Each tool proxies to the matching REST route
// under /api/autonomous/prds/{id}/.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// failureEscalationPolicyParamDesc describes the policy object of
// autonomous_prd_set_failure_escalation.
const failureEscalationPolicyParamDesc = "Override policy: enabled (boolean), max_failures (whole number, 1 or more) and steps (array of objects with backend, model, effort and attempts; an empty backend means the task's normally resolved worker). Omitted fields inherit from the next level up. Ignored when clear is true."

func (s *Server) toolAutonomousPRDSetFailureEscalation() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_failure_escalation",
		mcpsdk.WithDescription("Set or clear the failure escalation override on an Automaton, or on one task of it. Each field resolves task, then Automaton, then the global default. Set clear=true to remove the override at this level."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("task_id", mcpsdk.Description("Task ID; empty = the Automaton level")),
		mcpsdk.WithObject("policy", mcpsdk.Description(failureEscalationPolicyParamDesc)),
		mcpsdk.WithBoolean("clear", mcpsdk.Description("true removes the override at this level")),
	)
}

func (s *Server) handleAutonomousPRDSetFailureEscalation(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	var policy any
	if !req.GetBool("clear", false) {
		policy = req.GetArguments()["policy"]
		if policy == nil {
			return nil, fmt.Errorf("policy is required; pass an object, or clear=true to remove the override")
		}
	}
	body, _ := json.Marshal(map[string]any{
		"task_id": req.GetString("task_id", ""),
		"policy":  policy,
		"actor":   "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_failure_escalation", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDResolveTaskReview() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_resolve_task_review",
		mcpsdk.WithDescription("The operator's decision on a task held for review (status review_needed). action=retry requeues it, optionally with a changed spec, worker (backend, model, effort) or narrowed file scope (files). action=cancel cancels it. It works while the Automaton is running and other tasks are live. A task that is not held answers 409."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID held for review")),
		mcpsdk.WithString("action", mcpsdk.Required(), mcpsdk.Description("retry or cancel")),
		mcpsdk.WithString("spec", mcpsdk.Description("retry only: replacement task spec")),
		mcpsdk.WithString("backend", mcpsdk.Description("retry only: worker backend for the task")),
		mcpsdk.WithString("model", mcpsdk.Description("retry only: worker model for the task")),
		mcpsdk.WithString("effort", mcpsdk.Description("retry only: worker effort for the task")),
		mcpsdk.WithArray("files", mcpsdk.Description("retry only: narrowed list of files the task may change"), mcpsdk.WithStringItems()),
		mcpsdk.WithString("note", mcpsdk.Description("operator note, recorded on the decision log and given to the next attempt")),
	)
}

func (s *Server) handleAutonomousPRDResolveTaskReview(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]any{
		"task_id": req.GetString("task_id", ""),
		"action":  req.GetString("action", ""),
		"spec":    req.GetString("spec", ""),
		"backend": req.GetString("backend", ""),
		"model":   req.GetString("model", ""),
		"effort":  req.GetString("effort", ""),
		"files":   stringsArg(req.GetArguments()["files"]),
		"note":    req.GetString("note", ""),
		"actor":   "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/resolve_task_review", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// stringsArg converts an MCP array argument to a string slice. Anything that is
// not an array of strings yields nil.
func stringsArg(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if str, ok := item.(string); ok {
			out = append(out, str)
		}
	}
	return out
}
