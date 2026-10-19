// Autonomous PRD write calls request the compact response view.

package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestMCPWriteToolsRequestCompactView(t *testing.T) {
	var queries []string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	if _, err := s.handleAutonomousPRDResetTask(context.Background(), callReq(map[string]any{"id": "p1", "task_id": "t1"})); err != nil {
		t.Fatalf("reset_task: %v", err)
	}
	if _, err := s.handleAutonomousPRDRun(context.Background(), callReq(map[string]any{"id": "p1"})); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("got %d requests, want 2", len(queries))
	}
	for i, q := range queries {
		if !strings.Contains(q, "view=compact") {
			t.Errorf("request %d query = %q, want view=compact", i, q)
		}
	}

	// A non-autonomous write path must not get the compact view.
	queries = nil
	if _, err := s.proxyJSON(http.MethodPost, "/api/ask", map[string]string{"question": "q"}); err != nil {
		t.Fatalf("proxyJSON /api/ask: %v", err)
	}
	if len(queries) != 1 || strings.Contains(queries[0], "view=compact") {
		t.Errorf("non-autonomous write query = %v, want no view=compact", queries)
	}
}
