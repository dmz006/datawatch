// Package mcp — Project + Cluster profile tools (F10 sprint 2 S2.3).
//
// Design: 6 MCP tools (list, get, create, update, delete, smoke) that
// each take a `kind` arg ("project" or "cluster"). Cleaner for the LLM
// surface than 12 near-duplicate tools and consistent with how the REST
// handlers share structure.
//
// All tools proxy to the local datawatch HTTP API on 127.0.0.1:$webPort,
// matching the pattern set by config_set. Auth token is injected by the
// proxy layer so the tool doesn't need to handle it.

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── Tool declarations ──────────────────────────────────────────────────

func (s *Server) toolProfileList() mcpsdk.Tool {
	return mcpsdk.NewTool("profile_list",
		mcpsdk.WithDescription("List all Project or Cluster Profiles."),
		mcpsdk.WithString("kind", mcpsdk.Required(),
			mcpsdk.Description("Profile kind: 'project' or 'cluster'"),
			mcpsdk.Enum("project", "cluster"),
		),
	)
}

func (s *Server) toolProfileGet() mcpsdk.Tool {
	return mcpsdk.NewTool("profile_get",
		mcpsdk.WithDescription("Fetch one Project or Cluster Profile by name."),
		mcpsdk.WithString("kind", mcpsdk.Required(),
			mcpsdk.Description("Profile kind: 'project' or 'cluster'"),
			mcpsdk.Enum("project", "cluster"),
		),
		mcpsdk.WithString("name", mcpsdk.Required(),
			mcpsdk.Description("Profile name (DNS label: [a-z0-9-])"),
		),
	)
}

func (s *Server) toolProfileCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("profile_create",
		mcpsdk.WithDescription("Create a new Project or Cluster Profile. Pass the full profile body as JSON in 'body'."),
		mcpsdk.WithString("kind", mcpsdk.Required(),
			mcpsdk.Description("Profile kind: 'project' or 'cluster'"),
			mcpsdk.Enum("project", "cluster"),
		),
		mcpsdk.WithString("body", mcpsdk.Required(),
			mcpsdk.Description(`JSON body. Project example:
{"name":"my-proj","git":{"url":"https://github.com/x/y","branch":"main"},
 "image_pair":{"agent":"agent-claude","sidecar":"lang-go"},
 "memory":{"mode":"sync-back"}}
Cluster example:
{"name":"my-k8s","kind":"k8s","context":"testing","namespace":"dw"}`),
		),
	)
}

func (s *Server) toolProfileUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("profile_update",
		mcpsdk.WithDescription("Update an existing Project or Cluster Profile. 'name' from the args wins over any name in the body."),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Enum("project", "cluster")),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("body", mcpsdk.Required(),
			mcpsdk.Description("Full updated JSON body (merge not supported)."),
		),
	)
}

func (s *Server) toolProfileDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("profile_delete",
		mcpsdk.WithDescription("Delete a Project or Cluster Profile by name."),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Enum("project", "cluster")),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}

func (s *Server) toolProfileSmoke() mcpsdk.Tool {
	return mcpsdk.NewTool("profile_smoke",
		mcpsdk.WithDescription("Run the validation smoke test against a profile. Returns Checks[], Warnings[], Errors[]. A profile is passing iff Errors is empty."),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Enum("project", "cluster")),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}

// ── Handlers ───────────────────────────────────────────────────────────

// profilePathForKind maps "project" → "projects" and "cluster" → "clusters"
// so the REST URL is correct. Returns an error for unknown kinds so the
// LLM gets a clear message rather than a silent 404.
func profilePathForKind(kind string) (string, error) {
	switch kind {
	case "project":
		return "projects", nil
	case "cluster":
		return "clusters", nil
	default:
		return "", fmt.Errorf("kind must be 'project' or 'cluster', got %q", kind)
	}
}

func (s *Server) handleProfileList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	kind, err := profilePathForKind(req.GetString("kind", ""))
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	resp, body, err := s.profileCall("GET", kind, "", nil)
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode >= 400 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("list: HTTP %d: %s", resp.StatusCode, body)), nil
	}
	return mcpsdk.NewToolResultText(body), nil
}

func (s *Server) handleProfileGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	kind, err := profilePathForKind(req.GetString("kind", ""))
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	name := req.GetString("name", "")
	if name == "" {
		return mcpsdk.NewToolResultError("name is required"), nil
	}
	resp, body, err := s.profileCall("GET", kind, name, nil)
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode >= 400 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("get: HTTP %d: %s", resp.StatusCode, body)), nil
	}
	return mcpsdk.NewToolResultText(body), nil
}

func (s *Server) handleProfileCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	kind, err := profilePathForKind(req.GetString("kind", ""))
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	body := req.GetString("body", "")
	if body == "" {
		return mcpsdk.NewToolResultError("body is required"), nil
	}
	// Validate JSON client-side so we get a clear error path
	if err := json.Unmarshal([]byte(body), &map[string]interface{}{}); err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("body is not valid JSON: %v", err)), nil
	}
	resp, respBody, err := s.profileCall("POST", kind, "", strings.NewReader(body))
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode >= 400 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("create: HTTP %d: %s", resp.StatusCode, respBody)), nil
	}
	return mcpsdk.NewToolResultText(respBody), nil
}

func (s *Server) handleProfileUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	kind, err := profilePathForKind(req.GetString("kind", ""))
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	name := req.GetString("name", "")
	body := req.GetString("body", "")
	if name == "" || body == "" {
		return mcpsdk.NewToolResultError("name and body are required"), nil
	}
	if err := json.Unmarshal([]byte(body), &map[string]interface{}{}); err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("body is not valid JSON: %v", err)), nil
	}
	resp, respBody, err := s.profileCall("PUT", kind, name, strings.NewReader(body))
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode >= 400 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("update: HTTP %d: %s", resp.StatusCode, respBody)), nil
	}
	return mcpsdk.NewToolResultText(respBody), nil
}

func (s *Server) handleProfileDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	kind, err := profilePathForKind(req.GetString("kind", ""))
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	name := req.GetString("name", "")
	if name == "" {
		return mcpsdk.NewToolResultError("name is required"), nil
	}
	resp, respBody, err := s.profileCall("DELETE", kind, name, nil)
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode >= 400 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("delete: HTTP %d: %s", resp.StatusCode, respBody)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Deleted %s profile %q", req.GetString("kind", ""), name)), nil
}

func (s *Server) handleProfileSmoke(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	kind, err := profilePathForKind(req.GetString("kind", ""))
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	name := req.GetString("name", "")
	if name == "" {
		return mcpsdk.NewToolResultError("name is required"), nil
	}
	// 422 (validation failed but profile exists) is a VALID result for
	// the caller — forward the body so they can see the errors.
	resp, respBody, err := s.profileCall("POST", kind, name+"/smoke", nil)
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode == 404 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("profile not found: %s", name)), nil
	}
	if resp.StatusCode >= 500 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("smoke: HTTP %d: %s", resp.StatusCode, respBody)), nil
	}
	// 200 or 422 both return the smoke result body
	return mcpsdk.NewToolResultText(respBody), nil
}

// ── HTTP plumbing ──────────────────────────────────────────────────────

// profileCall does the actual HTTP round-trip to the local REST API.
// Returns the response + body + error. Caller handles status codes.
func (s *Server) profileCall(method, kind, pathSuffix string, body io.Reader) (*http.Response, string, error) {
	if s.webPort <= 0 {
		return nil, "", fmt.Errorf("web server not available for profile operations (no webPort)")
	}
	u := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", s.webPort),
		Path:   "/api/profiles/" + kind,
	}
	if pathSuffix != "" {
		u.Path += "/" + pathSuffix
	}
	req, err := http.NewRequest(method, u.String(), body)
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
	bb := &bytes.Buffer{}
	_, _ = io.Copy(bb, resp.Body)
	return resp, bb.String(), nil
}
