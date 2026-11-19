package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestCompletedUnmergedToolReadsRoute(t *testing.T) {
	var path, method, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"completed_unmerged":[{"id":"a1","branch":"automaton/a1","detail":"x"}],"count":1}`))
	})
	res, err := s.handleAutonomousCompletedUnmerged(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/completed-unmerged" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s", method, path, rawQuery)
	}
	if text := automationText(t, res); !strings.Contains(text, `"automaton/a1"`) {
		t.Errorf("result lost the daemon body: %s", text)
	}
}

func TestCompletedUnmergedToolIsReadOnly(t *testing.T) {
	tool := (&Server{}).toolAutonomousCompletedUnmerged()
	if tool.Name != "autonomous_completed_unmerged" {
		t.Errorf("name = %q", tool.Name)
	}
	if h := tool.Annotations.ReadOnlyHint; h == nil || !*h {
		t.Error("tool must be marked read-only")
	}
	if len(tool.InputSchema.Properties) != 0 {
		t.Errorf("a read-only list takes no arguments, got %v", tool.InputSchema.Properties)
	}
}

func TestCompletedUnmergedToolSurfacesDaemonError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
	})
	res, err := s.handleAutonomousCompletedUnmerged(context.Background(), callReq(nil))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want a tool error, err=%v res=%+v", err, res)
	}
}
