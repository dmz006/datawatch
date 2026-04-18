// F10 sprint 3 S3.3 follow-up — MCP tools for agent operations.
//
// Five tools (spawn/list/get/logs/terminate). Each is a thin proxy to
// the local REST API, same pattern as the profile_* and config_set
// tools. Keeps auth + validation in one place (the REST handlers).

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolAgentSpawn() mcpsdk.Tool {
	return mcpsdk.NewTool("agent_spawn",
		mcpsdk.WithDescription("Spawn a new ephemeral agent worker. Resolves (project_profile, cluster_profile) and brings up a container via the matching cluster driver. Returns the agent record; the bootstrap token is never exposed."),
		mcpsdk.WithString("project_profile", mcpsdk.Required(),
			mcpsdk.Description("Name of an existing Project Profile (see profile_list kind=project)")),
		mcpsdk.WithString("cluster_profile", mcpsdk.Required(),
			mcpsdk.Description("Name of an existing Cluster Profile")),
		mcpsdk.WithString("task",
			mcpsdk.Description("Optional task description injected into the worker's env")),
	)
}

func (s *Server) toolAgentList() mcpsdk.Tool {
	return mcpsdk.NewTool("agent_list",
		mcpsdk.WithDescription("List every tracked agent + its state (pending/starting/ready/running/failed/stopped)."),
	)
}

func (s *Server) toolAgentGet() mcpsdk.Tool {
	return mcpsdk.NewTool("agent_get",
		mcpsdk.WithDescription("Fetch one agent by ID."),
		mcpsdk.WithString("id", mcpsdk.Required(),
			mcpsdk.Description("Agent ID (16-byte hex)")),
	)
}

func (s *Server) toolAgentLogs() mcpsdk.Tool {
	return mcpsdk.NewTool("agent_logs",
		mcpsdk.WithDescription("Fetch recent container logs (stdout+stderr merged) for an agent."),
		mcpsdk.WithString("id", mcpsdk.Required(),
			mcpsdk.Description("Agent ID")),
		mcpsdk.WithNumber("lines",
			mcpsdk.Description("Max tail lines (default 200, max 10000)")),
	)
}

func (s *Server) toolAgentTerminate() mcpsdk.Tool {
	return mcpsdk.NewTool("agent_terminate",
		mcpsdk.WithDescription("Terminate an agent. Forcefully removes the worker container; safe to call repeatedly."),
		mcpsdk.WithString("id", mcpsdk.Required(),
			mcpsdk.Description("Agent ID")),
	)
}

func (s *Server) toolSessionBindAgent() mcpsdk.Tool {
	return mcpsdk.NewTool("session_bind_agent",
		mcpsdk.WithDescription("Bind a session to a parent-spawned worker agent (F10 sprint 3.6). After binding, session reads forward through /api/proxy/agent/{agent_id}/.... Pass agent_id=\"\" to unbind."),
		mcpsdk.WithString("session_id", mcpsdk.Required(),
			mcpsdk.Description("Session ID (4-char hex or full host-id form)")),
		mcpsdk.WithString("agent_id",
			mcpsdk.Description("Agent ID to bind to; empty string unbinds")),
	)
}

// ── Handlers ───────────────────────────────────────────────────────────

func (s *Server) handleAgentSpawn(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]string{
		"project_profile": req.GetString("project_profile", ""),
		"cluster_profile": req.GetString("cluster_profile", ""),
		"task":            req.GetString("task", ""),
	}
	if body["project_profile"] == "" || body["cluster_profile"] == "" {
		return mcpsdk.NewToolResultError("project_profile and cluster_profile are required"), nil
	}
	resp, respBody, err := s.agentCall("POST", "", body)
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode >= 400 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("spawn: HTTP %d: %s", resp.StatusCode, respBody)), nil
	}
	return mcpsdk.NewToolResultText(respBody), nil
}

func (s *Server) handleAgentList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	resp, body, err := s.agentCall("GET", "", nil)
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode >= 400 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("list: HTTP %d: %s", resp.StatusCode, body)), nil
	}
	return mcpsdk.NewToolResultText(body), nil
}

func (s *Server) handleAgentGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultError("id is required"), nil
	}
	resp, body, err := s.agentCall("GET", id, nil)
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode >= 400 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("get: HTTP %d: %s", resp.StatusCode, body)), nil
	}
	return mcpsdk.NewToolResultText(body), nil
}

func (s *Server) handleAgentLogs(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultError("id is required"), nil
	}
	n := int(req.GetFloat("lines", 200))
	if n <= 0 {
		n = 200
	}
	path := fmt.Sprintf("%s/logs?lines=%d", id, n)
	resp, body, err := s.agentCall("GET", path, nil)
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode >= 400 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("logs: HTTP %d: %s", resp.StatusCode, body)), nil
	}
	return mcpsdk.NewToolResultText(body), nil
}

func (s *Server) handleAgentTerminate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultError("id is required"), nil
	}
	resp, body, err := s.agentCall("DELETE", id, nil)
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode >= 400 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("terminate: HTTP %d: %s", resp.StatusCode, body)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Agent %s terminated", id)), nil
}

func (s *Server) handleSessionBindAgent(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("session_id", "")
	if id == "" {
		return mcpsdk.NewToolResultError("session_id is required"), nil
	}
	body := map[string]string{
		"id":       id,
		"agent_id": req.GetString("agent_id", ""),
	}
	resp, respBody, err := s.sessionsCall("POST", "bind", body)
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode >= 400 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("bind: HTTP %d: %s", resp.StatusCode, respBody)), nil
	}
	return mcpsdk.NewToolResultText(respBody), nil
}

// sessionsCall is the REST round-trip for /api/sessions/{action}.
// Mirrors agentCall but targets the session endpoints.
func (s *Server) sessionsCall(method, action string, body interface{}) (*http.Response, string, error) {
	if s.webPort <= 0 {
		return nil, "", fmt.Errorf("web server not available for session operations")
	}
	u := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", s.webPort),
		Path:   "/api/sessions/" + action,
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u.String(), reader)
	if err != nil {
		return nil, "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	buf := &bytes.Buffer{}
	_, _ = io.Copy(buf, resp.Body)
	return resp, buf.String(), nil
}

// agentCall is the REST round-trip. pathSuffix is "" for the
// collection, "<id>" for the named resource, "<id>/logs" for logs.
// body, if non-nil, is JSON-encoded.
func (s *Server) agentCall(method, pathSuffix string, body interface{}) (*http.Response, string, error) {
	if s.webPort <= 0 {
		return nil, "", fmt.Errorf("web server not available for agent operations")
	}
	u := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", s.webPort),
		Path:   "/api/agents",
	}
	if pathSuffix != "" {
		// pathSuffix may already contain ?query for logs
		u.Path += "/" + pathSuffix
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u.String(), reader)
	if err != nil {
		return nil, "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	buf := &bytes.Buffer{}
	_, _ = io.Copy(buf, resp.Body)
	return resp, buf.String(), nil
}
