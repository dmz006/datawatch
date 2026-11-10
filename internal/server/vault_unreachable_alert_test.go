package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func vaultAlertSeconds(t *testing.T, s *Server) int {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var out struct {
		Secrets struct {
			Vault struct {
				N *int `json:"unreachable_alert_after_seconds"`
			} `json:"vault"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil || out.Secrets.Vault.N == nil {
		t.Fatalf("GET /api/config missing secrets.vault.unreachable_alert_after_seconds: %v %s", err, rr.Body.String())
	}
	return *out.Secrets.Vault.N
}

func newVaultAlertServer(t *testing.T) *Server {
	t.Helper()
	return &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
}

func TestVaultUnreachableAlertConfigDefault(t *testing.T) {
	if got := vaultAlertSeconds(t, newVaultAlertServer(t)); got != 120 {
		t.Fatalf("default = %d, want 120", got)
	}
}

func TestVaultUnreachableAlertConfigRoundTrip(t *testing.T) {
	s := newVaultAlertServer(t)
	key := "secrets.vault.unreachable_alert_after_seconds"
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{key: 45})
	if got := vaultAlertSeconds(t, s); got != 45 {
		t.Fatalf("after PUT 45: %d", got)
	}
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{key: 0})
	if got := vaultAlertSeconds(t, s); got != 0 {
		t.Fatalf("after PUT 0 (disabled): %d", got)
	}
}

func TestVaultUnreachableAlertConfigRejectsNegative(t *testing.T) {
	s := newVaultAlertServer(t)
	key := "secrets.vault.unreachable_alert_after_seconds"
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{key: 60})
	for _, bad := range []any{-1, "soon", 1.5} {
		pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{key: bad})
	}
	if got := vaultAlertSeconds(t, s); got != 60 {
		t.Fatalf("rejected PUT changed value: %d, want 60", got)
	}
}
