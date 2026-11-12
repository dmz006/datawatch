// autonomous_prd_get passes the PRD's push state (push_state /
// push_warning) through its REST proxy on both the summary default and the
// full detail level. No new MCP tool is introduced: the same tool proxies
// the fields it already returns.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestMCPPrdGetPushStatus(t *testing.T) {
	prd := &autonomous.PRD{
		ID:     "abcd1234",
		Status: autonomous.PRDRunning,
		Git: autonomous.GitWorkflow{
			Branch:      "automaton/abcd1234",
			URL:         "https://example.invalid/org/repo",
			PushState:   autonomous.PushStatePushed,
			PushedRef:   "refs/heads/automaton/abcd1234",
			PushedSHA:   "0123456789abcdef",
			PushWarning: "",
		},
	}
	prdNotPushed := &autonomous.PRD{
		ID:     "efgh5678",
		Status: autonomous.PRDRunning,
		Git: autonomous.GitWorkflow{
			Branch:      "automaton/efgh5678",
			PushState:   autonomous.PushStateNotPushed,
			PushWarning: "no origin remote",
		},
	}
	summaryJSON, err := json.Marshal(autonomous.BuildSummaryView(prdNotPushed))
	if err != nil {
		t.Fatalf("summary view: %v", err)
	}
	fullJSON, err := json.Marshal(prd)
	if err != nil {
		t.Fatalf("marshal prd: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/autonomous/prds/abcd1234":
			w.Write(fullJSON) //nolint:errcheck
		case "/api/autonomous/prds/efgh5678":
			w.Write(summaryJSON) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	t.Run("full detail carries the pushed_* fields", func(t *testing.T) {
		res, err := s.handleAutonomousPRDGet(context.Background(), callReq(map[string]any{"id": "abcd1234", "detail": "full"}))
		text := resultText(t, res, err)
		for _, want := range []string{`"push_state":"pushed"`, `"pushed_ref":"refs/heads/automaton/abcd1234"`, `"pushed_sha":"0123456789abcdef"`} {
			if !strings.Contains(text, want) {
				t.Errorf("full detail response missing %s: %s", want, text)
			}
		}
	})

	t.Run("summary default carries push_state and push_warning", func(t *testing.T) {
		res, err := s.handleAutonomousPRDGet(context.Background(), callReq(map[string]any{"id": "efgh5678"}))
		text := resultText(t, res, err)
		if !strings.Contains(text, `"push_state":"not_pushed"`) {
			t.Errorf("summary response missing push_state: %s", text)
		}
		if !strings.Contains(text, `"push_warning":"no origin remote"`) {
			t.Errorf("summary response missing push_warning: %s", text)
		}
	})
}
