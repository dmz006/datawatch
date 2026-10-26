package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestTagSettings_ConfigSetShowsTheDaemonRefusal checks that MCP config_set
// returns the daemon's refusal text for a bad autonomous.tags value, the same
// text REST returns. The fake daemon refuses every PUT with the shared
// validator's message, as the real handler does.
func TestTagSettings_ConfigSetShowsTheDaemonRefusal(t *testing.T) {
	cases := []struct{ key, value string }{
		{"autonomous.tags.attention_statuses", "bogus"},
		{"autonomous.tags.default_match_mode", "sometimes"},
	}
	for _, tc := range cases {
		want := config.ValidateTagSetting(tc.key, tc.value).Error()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.ReadAll(r.Body)
			http.Error(w, want, http.StatusBadRequest)
		}))
		t.Cleanup(srv.Close)
		port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
		s := &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: true}}

		res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{
			"key": tc.key, "value": tc.value,
		}))
		if err != nil {
			t.Fatalf("handleConfigSet: %v", err)
		}
		text := automationText(t, res)
		if !strings.Contains(text, want) {
			t.Fatalf("config_set text = %q, want it to carry %q", text, want)
		}
	}
}
