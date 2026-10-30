package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func TestQuestionStatsToolIsListed(t *testing.T) {
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
		found = found || tl.Name == "question_stats"
	}
	if !found {
		t.Error("tool \"question_stats\" is not registered")
	}
}

func TestQuestionStatsToolSchemaAndCapability(t *testing.T) {
	tool := (&Server{}).toolQuestionStats()
	if tool.Name != "question_stats" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("question_stats must be marked read-only")
	}
	if len(tool.InputSchema.Properties) != 0 {
		t.Errorf("question_stats takes no parameters, got %v", tool.InputSchema.Properties)
	}
	if got := federation.MCPToolCap["question_stats"]; got != federation.CapAutonomousRead {
		t.Errorf("capability = %q, want %q", got, federation.CapAutonomousRead)
	}
}

func TestQuestionStatsToolProxiesTheRESTRead(t *testing.T) {
	var path, method string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_ = json.NewEncoder(w).Encode(map[string]any{"raised": 3, "worker_marker_enabled": true})
	})
	res, err := s.handleQuestionStats(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if path != "/api/questions/stats" || method != http.MethodGet {
		t.Fatalf("proxied %s %s, want GET /api/questions/stats", method, path)
	}
	if text := automationText(t, res); !strings.Contains(text, `"raised":3`) {
		t.Fatalf("body = %s, want it passed through", text)
	}
}

func TestQuestionStatsToolReportsAFailedRead(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	res, err := s.handleQuestionStats(context.Background(), callReq(nil))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("err=%v res=%+v, want a tool error", err, res)
	}
}
