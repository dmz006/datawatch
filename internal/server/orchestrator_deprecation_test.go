package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/orchestrator"
)

// stubOrchestrator returns fixed maps so the REST wrapper can be checked
// without a runner.
type stubOrchestrator struct{ OrchestratorAPI }

func (stubOrchestrator) CreateGraph(title, dir string, ids []string) (any, error) {
	return map[string]any{"id": "g1", "title": title}, nil
}
func (stubOrchestrator) PlanGraph(id string, _ map[string][]string) (any, error) {
	return map[string]any{"id": id, "planned": true}, nil
}
func (stubOrchestrator) RunGraph(context.Context, string) error { return nil }
func (stubOrchestrator) ListGraphs() []any                      { return []any{} }
func (stubOrchestrator) GetGraph(id string) (any, bool) {
	return map[string]any{"id": id}, true
}

func TestOrchestratorRESTDeprecationNotice(t *testing.T) {
	orchestrator.ResetDeprecationWarnedForTest()
	s := &Server{}
	s.SetOrchestratorAPI(stubOrchestrator{})

	do := func(method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		s.handleOrchestratorGraphs(w, r)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return w, m
	}

	for _, c := range []struct {
		name, method, path, body, tool, field string
	}{
		{"create", http.MethodPost, "/api/orchestrator/graphs", `{"title":"t","prd_ids":["a"]}`, "orchestrator_graph_create", "title"},
		{"plan", http.MethodPost, "/api/orchestrator/graphs/g1/plan", `{}`, "orchestrator_graph_plan", "planned"},
		{"run", http.MethodPost, "/api/orchestrator/graphs/g1/run", ``, "orchestrator_graph_run", "status"},
	} {
		w, m := do(c.method, c.path, c.body)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d", c.name, w.Code)
		}
		if w.Header().Get("Deprecation") != "true" {
			t.Errorf("%s: missing Deprecation header", c.name)
		}
		if m["deprecated"] != true {
			t.Errorf("%s: deprecated = %v", c.name, m["deprecated"])
		}
		if n, _ := m["deprecation"].(string); n != orchestrator.DeprecationNotice(c.tool) {
			t.Errorf("%s: deprecation = %q", c.name, n)
		}
		if _, ok := m[c.field]; !ok {
			t.Errorf("%s: existing field %q dropped: %v", c.name, c.field, m)
		}
	}

	for _, c := range []struct{ name, path string }{
		{"list", "/api/orchestrator/graphs"},
		{"get", "/api/orchestrator/graphs/g1"},
	} {
		w, m := do(http.MethodGet, c.path, "")
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d", c.name, w.Code)
		}
		if w.Header().Get("Deprecation") != "" {
			t.Errorf("%s: unexpected Deprecation header", c.name)
		}
		if _, ok := m["deprecated"]; ok {
			t.Errorf("%s: carries deprecated field", c.name)
		}
		if _, ok := m["deprecation"]; ok {
			t.Errorf("%s: carries deprecation field", c.name)
		}
	}
}
