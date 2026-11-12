package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestVaultUnreachableAlertCLIRoundTrip(t *testing.T) {
	const key = "secrets.vault.unreachable_alert_after_seconds"
	var mu sync.Mutex
	n := 120
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			raw, _ := io.ReadAll(r.Body)
			var patch map[string]any
			_ = json.Unmarshal(raw, &patch)
			f, ok := patch[key].(float64)
			if !ok || f != float64(int(f)) || f < 0 {
				http.Error(w, key+": must be a whole number >= 0", http.StatusBadRequest)
				return
			}
			n = int(f)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"secrets": map[string]any{"vault": map[string]any{"unreachable_alert_after_seconds": n}}})
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })

	for _, v := range []string{"45", "0"} {
		if _, err := captureEscalationStdout(t, func() error { return runConfigSet(&config.Config{}, key, v) }); err != nil {
			t.Fatalf("set %s: %v", v, err)
		}
		out, err := captureEscalationStdout(t, func() error { return runConfigGet(&config.Config{}, key) })
		if err != nil || strings.TrimSpace(out) != v {
			t.Fatalf("read back %s = %q, %v", v, out, err)
		}
	}
	for _, bad := range []string{"-1", "1.5", "soon"} {
		err := runConfigSet(&config.Config{}, key, bad)
		if err == nil || !strings.Contains(err.Error(), "whole number >= 0") {
			t.Errorf("%s: want the daemon's refusal, got %v", bad, err)
		}
	}
}

func TestVaultUnreachableAlertCLIHelpListsKey(t *testing.T) {
	if !strings.Contains(newConfigSetCmd().Long, "secrets.vault.unreachable_alert_after_seconds") {
		t.Error("config set help does not name the key")
	}
}
