package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func gitHostsTestServer(t *testing.T) *Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/git/hosts" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"hosts":[{"name":"github","kind":"github","readiness":{"host":"github","logged_in":true}}]}`))
		case r.URL.Path == "/api/git/hosts/stats" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"configured":1,"window_seconds":3600,"operations":{"push":2},"failed":0,"last_success_at":"0001-01-01T00:00:00Z"}`))
		case r.URL.Path == "/api/git/hosts/github/test" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"host":"github","ok":true,"message":"connected"}`))
		case strings.HasPrefix(r.URL.Path, "/api/git/hosts/"):
			http.Error(w, `{"error":"git host not found"}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &Server{webPort: port}
}

func gitHostsText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	return res.Content[0].(mcpsdk.TextContent).Text
}

func TestGitHostsMCP_List(t *testing.T) {
	s := gitHostsTestServer(t)
	if got := s.toolGitHostsStatus().Name; got != "git_hosts_status" {
		t.Fatalf("tool name = %q", got)
	}
	res, err := s.handleGitHostsStatus(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("unexpected failure: %v %+v", err, res)
	}
	if txt := gitHostsText(t, res); !strings.Contains(txt, `"logged_in":true`) || !strings.Contains(txt, "github") {
		t.Errorf("unexpected body: %s", txt)
	}
}

func TestGitHostsMCP_TestKnownHost(t *testing.T) {
	s := gitHostsTestServer(t)
	if got := s.toolGitHostsTest().Name; got != "git_hosts_test" {
		t.Fatalf("tool name = %q", got)
	}
	var req mcpsdk.CallToolRequest
	req.Params.Arguments = map[string]any{"name": "github"}
	res, err := s.handleGitHostsTest(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("unexpected failure: %v %+v", err, res)
	}
	if txt := gitHostsText(t, res); !strings.Contains(txt, `"ok":true`) {
		t.Errorf("unexpected body: %s", txt)
	}
}

func TestGitHostsMCP_UnknownName(t *testing.T) {
	s := gitHostsTestServer(t)
	var req mcpsdk.CallToolRequest
	req.Params.Arguments = map[string]any{"name": "nope"}
	res, err := s.handleGitHostsTest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError || !strings.Contains(gitHostsText(t, res), "git_hosts_test") {
		t.Errorf("expected tool error for unknown host, got %+v", res)
	}
	// missing name
	res, _ = s.handleGitHostsTest(context.Background(), mcpsdk.CallToolRequest{})
	if !res.IsError {
		t.Errorf("expected error for empty name")
	}
}

func TestGitHostsMCP_Stats(t *testing.T) {
	s := gitHostsTestServer(t)
	if got := s.toolGitHostsStats().Name; got != "git_hosts_stats" {
		t.Fatalf("tool name = %q", got)
	}
	res, err := s.handleGitHostsStats(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("unexpected failure: %v %+v", err, res)
	}
	if txt := gitHostsText(t, res); !strings.Contains(txt, `"configured":1`) || !strings.Contains(txt, `"push":2`) {
		t.Errorf("unexpected body: %s", txt)
	}
}
