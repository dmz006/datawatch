package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// The Automaton get and list tools proxy the REST body, so the read-only
// merge_state field the daemon derives reaches the caller untouched.
func TestMergeStateMCPResultCarriesField(t *testing.T) {
	field := `"merge_state":"finished_not_merged"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/autonomous/prds" {
			_, _ = w.Write([]byte(`{"prds":[{"id":"p1","status":"completed",` + field + `},{"id":"p2"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"p1","status":"completed",` + field + `}`))
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	res, err := s.handleAutonomousPRDGet(context.Background(), callReq(map[string]any{"id": "p1"}))
	text := resultText(t, res, err)
	if !strings.Contains(text, `"merge_state":"finished_not_merged"`) {
		t.Errorf("get result missing merge_state: %s", text)
	}
	res, err = s.handleAutonomousPRDList(context.Background(), callReq(map[string]any{}))
	text = resultText(t, res, err)
	if !strings.Contains(text, `"merge_state":"finished_not_merged"`) {
		t.Errorf("list result missing merge_state: %s", text)
	}
}
