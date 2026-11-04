package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// Model routing tools. compute_served_models proxies
// GET /api/compute/served-models; the two node pin tools proxy
// POST /api/autonomous/prds/{id}/set_task_node and set_story_node. The routing
// behaviour itself is set through the autonomous.model_routing.* keys, with
// config_set or the model_routing_* arguments of autonomous_config_set.

func (s *Server) toolComputeServedModels() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_served_models",
		mcpsdk.WithDescription("Read which models each compute node serves right now. Without arguments it lists every enabled node with the models it serves and whether that list could be read. Give model (and optionally node and backend) to also get a plain answer to \"would this node accept this model?\" — the same answer an Automaton gets when you pin a task or story to a node, so the two can never disagree. A node's list is kept for autonomous.model_routing.served_models_cache_seconds; refresh=true reads it from the node again. Read-only; same object as GET /api/compute/served-models."),
		mcpsdk.WithString("node", mcpsdk.Description("Optional — limit the answer to one compute node name")),
		mcpsdk.WithString("model", mcpsdk.Description("Optional — a model name to check against the node's served list")),
		mcpsdk.WithString("backend", mcpsdk.Description("Optional — the LLM backend the model would run on, used together with model")),
		mcpsdk.WithBoolean("refresh", mcpsdk.Description("Optional — true reads the served list from the node now instead of using the kept copy")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleComputeServedModels(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	args := req.GetArguments()
	for _, k := range []string{"node", "model", "backend"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			q.Set(k, strings.TrimSpace(v))
		}
	}
	switch v := args["refresh"].(type) {
	case bool:
		if v {
			q.Set("refresh", "true")
		}
	case string:
		if strings.EqualFold(strings.TrimSpace(v), "true") {
			q.Set("refresh", "true")
		}
	}
	out, err := s.proxyGet("/api/compute/served-models", q)
	if err != nil {
		return toolError("compute_served_models", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDSetTaskNode() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_task_node",
		mcpsdk.WithDescription("Pin one task of an Automaton to a compute node, so its worker runs there. An empty node clears the pin and the task goes back to normal node choice. A node that does not serve the task's model is refused (or only warned about) according to autonomous.model_routing.refusal_mode. The verification job follows the worker's node unless autonomous.model_routing.verifier_follows_worker_node is false."),
		mcpsdk.WithString("prd_id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID")),
		mcpsdk.WithString("node", mcpsdk.Description("compute node name to pin the task to; empty = clear the pin")),
	)
}

func (s *Server) handleAutonomousPRDSetTaskNode(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("prd_id", "")
	body, _ := json.Marshal(map[string]string{
		"task_id": req.GetString("task_id", ""),
		"node":    strings.TrimSpace(req.GetString("node", "")),
		"actor":   "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(id)+"/set_task_node", body)
	return llmSetterResult(out, err)
}

func (s *Server) toolAutonomousPRDSetStoryNode() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_story_node",
		mcpsdk.WithDescription("Pin every task of one story of an Automaton to a compute node. An empty node clears the pin. A task's own node pin wins over the story's. A node that does not serve a task's model is refused (or only warned about) according to autonomous.model_routing.refusal_mode."),
		mcpsdk.WithString("prd_id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story ID")),
		mcpsdk.WithString("node", mcpsdk.Description("compute node name to pin the story to; empty = clear the pin")),
	)
}

func (s *Server) handleAutonomousPRDSetStoryNode(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("prd_id", "")
	body, _ := json.Marshal(map[string]string{
		"story_id": req.GetString("story_id", ""),
		"node":     strings.TrimSpace(req.GetString("node", "")),
		"actor":    "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(id)+"/set_story_node", body)
	return llmSetterResult(out, err)
}

// modelRoutingConfigKeys lists the autonomous.model_routing keys, in the order
// the six autonomous_config_set arguments are documented.
var modelRoutingConfigKeys = []string{
	"refusal_mode", "served_models_cache_seconds", "verifier_follows_worker_node",
	"unknown_node_policy", "probe_timeout_seconds", "alert_on_refusal",
}

// modelRoutingConfigOptions are the autonomous_config_set arguments for the
// autonomous.model_routing block. They are sent as dotted keys on PUT
// /api/config, where the daemon checks each value and names the valid ones in a
// refusal.
func modelRoutingConfigOptions() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithString("model_routing_refusal_mode", mcpsdk.Enum("refuse", "warn"), mcpsdk.Description("autonomous.model_routing.refusal_mode — what happens when a task is pinned to a node that does not serve its model: refuse (the default) turns the pin down, warn accepts it and records a warning")),
		mcpsdk.WithNumber("model_routing_served_models_cache_seconds", mcpsdk.Description("autonomous.model_routing.served_models_cache_seconds — how long a node's served model list is kept, in whole seconds >= 0 (default 300); 0 reads it from the node every time")),
		mcpsdk.WithBoolean("model_routing_verifier_follows_worker_node", mcpsdk.Description("autonomous.model_routing.verifier_follows_worker_node — run a task's verification job on the node its worker ran on (default true)")),
		mcpsdk.WithString("model_routing_unknown_node_policy", mcpsdk.Enum("assume_serves", "assume_not"), mcpsdk.Description("autonomous.model_routing.unknown_node_policy — how a node whose served models cannot be read counts: assume_serves (the default) lets the pin through, assume_not treats the model as not served")),
		mcpsdk.WithNumber("model_routing_probe_timeout_seconds", mcpsdk.Description("autonomous.model_routing.probe_timeout_seconds — seconds to wait when reading a node's served models, a whole number >= 1 (default 8)")),
		mcpsdk.WithBoolean("model_routing_alert_on_refusal", mcpsdk.Description("autonomous.model_routing.alert_on_refusal — raise an alert when a node pin is refused (default true)")),
	}
}

// modelRoutingConfigArgs maps the model_routing_* arguments of
// autonomous_config_set into PUT /api/config entries keyed
// autonomous.model_routing.<name>. Every argument is presence-aware, so an
// explicit false or 0 is sent as given.
func modelRoutingConfigArgs(req mcpsdk.CallToolRequest) map[string]any {
	out := map[string]any{}
	args := req.GetArguments()
	for _, k := range modelRoutingConfigKeys {
		if v, ok := args["model_routing_"+k]; ok {
			out["autonomous.model_routing."+k] = v
		}
	}
	return out
}
