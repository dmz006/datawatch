package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/orchestrator"
)

func TestOrchestratorCommDeprecationNotice(t *testing.T) {
	orchestrator.ResetDeprecationWarnedForTest()
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"g1"}`))
	})
	r := setupRetryRouter(t, port)

	cases := []struct {
		in   string
		tool string
	}{
		{"orchestrator create title /tmp/proj", "orchestrator_graph_create"},
		{"orchestrator plan g1", "orchestrator_graph_plan"},
		{"orchestrator run g1", "orchestrator_graph_run"},
		{"orchestrator get g1", ""},
		{"orchestrator", ""},
		{"orchestrator config", ""},
		{"orchestrator verdicts", ""},
		{"orchestrator delete g1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			resp := strings.Join(r.HandleTestMessage(tc.in), "\n")
			if tc.tool == "" {
				if strings.Contains(resp, "deprecated") {
					t.Errorf("unexpected notice: %q", resp)
				}
				return
			}
			if !strings.Contains(resp, orchestrator.DeprecationNotice(tc.tool)) {
				t.Errorf("reply %q lacks notice for %s", resp, tc.tool)
			}
			if !strings.Contains(resp, "automation import-graph") {
				t.Errorf("reply does not name automation import-graph: %q", resp)
			}
		})
	}
}
