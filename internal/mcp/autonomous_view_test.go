// Autonomous PRD write calls request the compact response view.

package mcp

import (
	"context"
	"net/http"
	"net/url"
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

func TestMCPPRDGetDetailArgument(t *testing.T) {
	var queries []string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"id":"p1"}`))
	})

	detailOf := func(q string) string {
		v, err := url.ParseQuery(q)
		if err != nil {
			t.Fatalf("parse query %q: %v", q, err)
		}
		return v.Get("detail")
	}

	// Omitted detail defaults to summary on the wire.
	if _, err := s.handleAutonomousPRDGet(context.Background(), callReq(map[string]any{"id": "p1"})); err != nil {
		t.Fatalf("get without detail: %v", err)
	}
	if len(queries) != 1 || detailOf(queries[0]) != "summary" {
		t.Fatalf("omitted detail: queries = %v, want detail=summary", queries)
	}

	// Explicit full is passed through.
	queries = nil
	if _, err := s.handleAutonomousPRDGet(context.Background(), callReq(map[string]any{"id": "p1", "detail": "full"})); err != nil {
		t.Fatalf("get detail=full: %v", err)
	}
	if len(queries) != 1 || detailOf(queries[0]) != "full" {
		t.Fatalf("detail=full: queries = %v, want detail=full", queries)
	}

	// Unknown values return an error result and send no request.
	queries = nil
	res, err := s.handleAutonomousPRDGet(context.Background(), callReq(map[string]any{"id": "p1", "detail": "bogus"}))
	if err != nil {
		t.Fatalf("get detail=bogus: unexpected Go error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("detail=bogus: want error result, got %+v", res)
	}
	if len(queries) != 0 {
		t.Fatalf("detail=bogus: sent %d requests, want none", len(queries))
	}
}
