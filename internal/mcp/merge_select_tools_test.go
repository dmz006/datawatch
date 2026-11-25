package mcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMergeSelectMCP_RegisteredAndProxies(t *testing.T) {
	registered := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		registered[n] = true
	}
	for _, n := range MergeSelectToolNames() {
		if !registered[n] {
			t.Errorf("%s is not registered", n)
		}
	}
	var method, path, query, body string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, path, query, body = r.Method, r.URL.Path, r.URL.RawQuery, string(b)
		_, _ = w.Write([]byte(`{}`))
	})
	res, err := s.handleAutonomousPRDAcceptForMerge(context.Background(), callReq(map[string]any{"id": "a1", "commit": "abc", "reason": "why"}))
	if err != nil || res.IsError {
		t.Fatalf("accept: %v %+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/prds/a1/landing/accept" || !strings.Contains(body, `"commit":"abc"`) || !strings.Contains(body, `"reason":"why"`) {
		t.Errorf("accept proxied %s %s %s", method, path, body)
	}
	if res, _ := s.handleAutonomousPRDAcceptForMerge(context.Background(), callReq(map[string]any{"id": "a1", "reason": "why"})); !res.IsError {
		t.Error("accept without a commit was sent")
	}
	if res, err = s.handleAutonomousPRDMergeSelection(context.Background(), callReq(map[string]any{"id": "a1"})); err != nil || res.IsError {
		t.Fatalf("selection: %v %+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/merge" || !strings.Contains(query, "selection=true") || !strings.Contains(query, "automaton=a1") {
		t.Errorf("selection proxied %s %s?%s", method, path, query)
	}
}
