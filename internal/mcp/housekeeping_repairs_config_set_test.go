package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestHousekeepingRepairs_ConfigSetSendsAndShowsRefusal checks MCP config_set
// sends an autonomous.housekeeping_repairs key to PUT /api/config and returns
// the daemon's refusal text for a bad value (the fake daemon runs the shared
// validator, as the real handler does).
func TestHousekeepingRepairs_ConfigSetSendsAndShowsRefusal(t *testing.T) {
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var patch map[string]interface{}
		if err := json.Unmarshal(raw, &patch); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest) // config_set retries with the value quoted
			return
		}
		for k, v := range patch {
			if err := config.ValidateHousekeepingRepairsSetting(k, v); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		got = patch
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}

	if _, err := s.handleConfigSet(context.Background(), callReq(map[string]any{
		"key": "autonomous.housekeeping_repairs.leftovers", "value": "on",
	})); err != nil {
		t.Fatalf("handleConfigSet: %v", err)
	}
	if got["autonomous.housekeeping_repairs.leftovers"] != "on" {
		t.Fatalf("daemon received %v", got)
	}

	want := config.ValidateHousekeepingRepairsSetting("autonomous.housekeeping_repairs.task_chain", "sometimes").Error()
	res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{
		"key": "autonomous.housekeeping_repairs.task_chain", "value": "sometimes",
	}))
	if err != nil {
		t.Fatalf("handleConfigSet: %v", err)
	}
	if text := automationText(t, res); !strings.Contains(text, want) {
		t.Fatalf("config_set text = %q, want it to carry %q", text, want)
	}
}
