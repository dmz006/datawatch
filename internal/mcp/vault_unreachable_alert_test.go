package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const vaultAlertKey = "secrets.vault.unreachable_alert_after_seconds"

// vaultAlertStubDaemon mirrors the daemon's rule for the Vault threshold: a
// whole number >= 0.
func vaultAlertStubDaemon(t *testing.T) *Server {
	t.Helper()
	var mu sync.Mutex
	n := 120
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			raw, _ := io.ReadAll(r.Body)
			var patch map[string]any
			if err := json.Unmarshal(raw, &patch); err != nil {
				http.Error(w, "bad JSON", http.StatusBadRequest)
				return
			}
			f, ok := patch[vaultAlertKey].(float64)
			if !ok || f != float64(int(f)) || f < 0 {
				http.Error(w, vaultAlertKey+": must be a whole number >= 0 (0 = off, default 120)", http.StatusBadRequest)
				return
			}
			n = int(f)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"secrets": map[string]any{"vault": map[string]any{"unreachable_alert_after_seconds": n}}})
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: true}}
}

func vaultAlertCall(t *testing.T, s *Server, v string) string {
	t.Helper()
	req := callReq(map[string]any{"key": vaultAlertKey, "value": v})
	res, err := s.handleConfigSet(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return automationText(t, res)
}

func TestVaultUnreachableAlertMCPRoundTrip(t *testing.T) {
	s := vaultAlertStubDaemon(t)
	for _, v := range []string{"45", "0"} {
		if out := vaultAlertCall(t, s, v); !strings.Contains(out, "Set "+vaultAlertKey+" = "+v) {
			t.Fatalf("set %s: %q", v, out)
		}
		res, err := s.handleGetConfig(context.Background(), callReq(map[string]any{"section": "secrets"}))
		if err != nil {
			t.Fatal(err)
		}
		if got := automationText(t, res); !strings.Contains(got, `"unreachable_alert_after_seconds": `+v) {
			t.Fatalf("read back %s: %q", v, got)
		}
	}
}

func TestVaultUnreachableAlertMCPRefusals(t *testing.T) {
	s := vaultAlertStubDaemon(t)
	for _, bad := range []string{"-1", "1.5", "soon"} {
		if out := vaultAlertCall(t, s, bad); !strings.Contains(out, "whole number >= 0") {
			t.Errorf("%s: want the daemon's refusal, got %q", bad, out)
		}
	}
}

func TestVaultUnreachableAlertMCPDocumented(t *testing.T) {
	if !strings.Contains((&Server{}).toolConfigSet().Description, vaultAlertKey) {
		t.Error("config_set description does not name " + vaultAlertKey)
	}
}
