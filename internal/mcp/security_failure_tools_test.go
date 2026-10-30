package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	dwserver "github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

// securityFailureFakeStore stands in for the daemon's record store: the
// tests write to it only through this fake.
type securityFailureFakeStore struct {
	records []*autonomous.RepoCIWatchRecord
}

func (f *securityFailureFakeStore) ListRepoCIWatchRecords() []*autonomous.RepoCIWatchRecord {
	return f.records
}

func securityFailureStoredRecords() []*autonomous.RepoCIWatchRecord {
	base := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	return []*autonomous.RepoCIWatchRecord{
		{
			Repo: "dmz006/datawatch", RefKind: "main", RefName: "main", SHA: "abc123",
			Workflow: "security.yml", RunID: "9001",
			RunURL: "https://github.com/dmz006/datawatch/actions/runs/9001",
			Status: "failing", Classification: "security", FirstSeenAt: base, UpdatedAt: base,
			Security: &autonomous.SecurityFailureReport{
				Dependency: "golang.org/x/net", AdvisoryIDs: []string{"GHSA-aaaa-bbbb-cccc"},
				Workflow: "security.yml", RunID: 9001, SHA: "abc123", FixedVersion: "0.40.1",
				Recommendation: "upgrade", Mode: "on", Action: "stopped", AssessedAt: "2026-10-29T09:00:00Z",
			},
		},
		{
			Repo: "dmz006/other", RefKind: "tag", RefName: "v1.2.0", SHA: "fed789",
			Workflow: "codeql.yml", RunID: "9002", Status: "superseded", Classification: "security",
			FirstSeenAt: base, UpdatedAt: base.Add(time.Minute),
			Security: &autonomous.SecurityFailureReport{
				Workflow: "codeql.yml", RunID: 9002, Recommendation: "unknown", Mode: "shadow", Action: "report_only",
			},
		},
	}
}

// securityFailureRouteServer serves the real route handler over the
// fake store and returns an MCP server pointed at it.
func securityFailureRouteServer(t *testing.T, store *securityFailureFakeStore) (*Server, string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/api/autonomous/security-failures", dwserver.SecurityFailureHandler(store))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	port, _ := strconv.Atoi(strings.Split(ts.URL, ":")[2])
	return &Server{webPort: port}, ts.URL
}

func securityFailureRouteBody(t *testing.T, base, query string) string {
	t.Helper()
	resp, err := http.Get(base + "/api/autonomous/security-failures" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestSecurityFailureMCPToolIsListed(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tl := range tools {
		if tl.Name == "security_failure_list" {
			found = true
		}
	}
	if !found {
		t.Error("tool \"security_failure_list\" is not registered")
	}
	if got, ok := federation.RequiredCapForMCPTool("security_failure_list"); !ok || got != federation.CapAutonomousRead {
		t.Errorf("security_failure_list capability = %q (listed %v), want %q", got, ok, federation.CapAutonomousRead)
	}
}

func TestSecurityFailureMCPSchema(t *testing.T) {
	tool := (&Server{}).toolSecurityFailureList()
	if tool.Name != "security_failure_list" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("security_failure_list must be marked read-only")
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("all parameters are optional, got required %v", tool.InputSchema.Required)
	}
	for _, name := range []string{"repo", "status"} {
		if _, ok := tool.InputSchema.Properties[name]; !ok {
			t.Errorf("lacks the %s parameter", name)
		}
	}
	if strings.Contains(strings.ToLower(tool.Description), "prd") {
		t.Errorf("description says PRD: %s", tool.Description)
	}
}

func TestSecurityFailureMCPReturnsSameJSONShapeAsRoute(t *testing.T) {
	s, base := securityFailureRouteServer(t, &securityFailureFakeStore{records: securityFailureStoredRecords()})
	for _, tc := range []struct {
		name  string
		args  map[string]any
		query string
	}{
		{"all", map[string]any{}, ""},
		{"repo", map[string]any{"repo": "dmz006/other"}, "?repo=dmz006%2Fother"},
		{"status", map[string]any{"status": "failing"}, "?status=failing"},
		{"both", map[string]any{"repo": "dmz006/datawatch", "status": "failing"}, "?repo=dmz006%2Fdatawatch&status=failing"},
		{"no match", map[string]any{"repo": "nobody/nothing"}, "?repo=nobody%2Fnothing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := s.handleSecurityFailureList(context.Background(), callReq(tc.args))
			if err != nil || res == nil || res.IsError {
				t.Fatalf("err=%v res=%+v", err, res)
			}
			got := panelText(t, res)
			want := securityFailureRouteBody(t, base, tc.query)
			if got != want {
				t.Errorf("tool output differs from the route:\ntool:  %s\nroute: %s", got, want)
			}
		})
	}
	// The unfiltered body is the two stored reports, newest first.
	res, _ := s.handleSecurityFailureList(context.Background(), callReq(nil))
	body := panelText(t, res)
	if i, j := strings.Index(body, `"run_id":"9002"`), strings.Index(body, `"run_id":"9001"`); i < 0 || j < 0 || i > j {
		t.Errorf("want runs 9002 then 9001 in %s", body)
	}
}

func TestSecurityFailureMCPEmptyStoreAndBadStatus(t *testing.T) {
	s, _ := securityFailureRouteServer(t, &securityFailureFakeStore{})
	res, err := s.handleSecurityFailureList(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if got := strings.TrimSpace(panelText(t, res)); got != "[]" {
		t.Errorf("empty store: %s, want []", got)
	}
	res, err = s.handleSecurityFailureList(context.Background(), callReq(map[string]any{"status": "bogus"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("bad status must be a tool error: err=%v res=%+v", err, res)
	}
}
