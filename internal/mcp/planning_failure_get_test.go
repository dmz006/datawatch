package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// The Automaton get and list tools proxy the REST body, so the
// planning_failure object the daemon returns reaches the caller untouched.
func TestPlanningFailureMCPResultCarriesObject(t *testing.T) {
	obj := `"planning_failure":{"reason":"spec holds a fixed version number","category":"content","source":"release-version-fixed","phrases":["v9.1.0"],"retryable":false,"retry_counts":{"content":1},"retry_budget":3}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/autonomous/prds" {
			_, _ = w.Write([]byte(`{"prds":[{"id":"p1",` + obj + `},{"id":"p2"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"p1",` + obj + `}`))
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	res, err := s.handleAutonomousPRDGet(context.Background(), callReq(map[string]any{"id": "p1"}))
	text := resultText(t, res, err)
	for _, want := range []string{`"planning_failure"`, `"reason":"spec holds a fixed version number"`, `"phrases":["v9.1.0"]`, `"retry_budget":3`} {
		if !strings.Contains(text, want) {
			t.Errorf("get result missing %s: %s", want, text)
		}
	}
	res, err = s.handleAutonomousPRDList(context.Background(), callReq(map[string]any{}))
	text = resultText(t, res, err)
	if !strings.Contains(text, `"planning_failure"`) || !strings.Contains(text, `"category":"content"`) {
		t.Errorf("list result missing planning_failure: %s", text)
	}
}
