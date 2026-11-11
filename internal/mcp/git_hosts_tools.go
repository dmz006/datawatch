// MCP tools for the configured git hosts.
//
// git_hosts_status: GET  /api/git/hosts            — readiness of each host
// git_hosts_test:   POST /api/git/hosts/{name}/test — test connection for one host
// git_hosts_stats:  GET  /api/git/hosts/stats       — git host operation counters
//
// Neither tool returns a token: the REST handlers expose only the account
// name, scope names and a vault secret NAME.
package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolGitHostsStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("git_hosts_status",
		mcpsdk.WithDescription("List every configured git host (git.hosts) with its readiness: logged-in state, account, token scopes, fix command and any problem. Never returns a token."),
	)
}

func (s *Server) handleGitHostsStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body, err := s.proxyGet("/api/git/hosts", url.Values{})
	if err != nil {
		return toolError("git_hosts_status", err), nil
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) toolGitHostsTest() mcpsdk.Tool {
	return mcpsdk.NewTool("git_hosts_test",
		mcpsdk.WithDescription("Run the test connection for one configured git host by name. Returns ok, account, a plain-language message and a fix command when it fails."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Configured git host name (see git_hosts_status)")),
	)
}

func (s *Server) handleGitHostsTest(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := strings.TrimSpace(req.GetString("name", ""))
	if name == "" {
		return toolErrorf("git_hosts_test", "name is required"), nil
	}
	body, err := s.proxyJSON(http.MethodPost, "/api/git/hosts/"+url.PathEscape(name)+"/test", nil)
	if err != nil {
		return toolError("git_hosts_test", err), nil
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) toolGitHostsStats() mcpsdk.Tool {
	return mcpsdk.NewTool("git_hosts_stats",
		mcpsdk.WithDescription("Git host counters: configured host count, the window in seconds, operations by kind (push, pull_request, merge, tag, ci_read), failed operations and the last success time. Same data as GET /api/git/hosts/stats."),
	)
}

func (s *Server) handleGitHostsStats(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body, err := s.proxyGet("/api/git/hosts/stats", url.Values{})
	if err != nil {
		return toolError("git_hosts_stats", err), nil
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}
