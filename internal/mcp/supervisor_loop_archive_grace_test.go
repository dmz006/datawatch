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
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// TestSupervisorLoopArchiveGraceMCP: the config_set description lists the
// archive-grace key, and config_set forwards it to PUT /api/config as a JSON
// number, with the daemon's refusal text shown for a negative value.
func TestSupervisorLoopArchiveGraceMCP(t *testing.T) {
	short := strings.TrimPrefix(supervisorloop.KeyArchiveCancelledAfterMinutes, "supervisor_loop.")
	desc := (&Server{}).toolConfigSet().Description
	if !strings.Contains(desc, short) {
		t.Errorf("config_set description does not list %q", short)
	}

	var gotBody string
	refusal := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		gotBody = string(raw)
		if refusal != "" {
			http.Error(w, refusal, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}

	key := "autonomous." + supervisorloop.KeyArchiveCancelledAfterMinutes
	res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{"key": key, "value": "15"}))
	if err != nil {
		t.Fatalf("handleConfigSet: %v", err)
	}
	if text := automationText(t, res); !strings.Contains(text, "Set "+key+" = 15") {
		t.Errorf("config_set text = %q", text)
	}
	if want := `{"` + key + `": 15}`; gotBody != want {
		t.Errorf("daemon got %q, want the number unquoted: %q", gotBody, want)
	}

	// A negative value: the daemon's refusal reaches the caller.
	refusal = supervisorloop.KeyArchiveCancelledAfterMinutes + ": must be >= 0, got -5"
	res, err = s.handleConfigSet(context.Background(), callReq(map[string]any{"key": key, "value": "-5"}))
	if err != nil {
		t.Fatalf("handleConfigSet: %v", err)
	}
	if text := automationText(t, res); !strings.Contains(text, refusal) {
		t.Errorf("negative: config_set text = %q, want it to carry %q", text, refusal)
	}
}
