package main

import (
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// With TLS on, the CLI reaches the daemon over its own certificate. config get
// and config set must trust that certificate the way every other command does.
func TestConfigGetAndSetTrustTheDaemonCertificate(t *testing.T) {
	var mu sync.Mutex
	var puts []string
	var hostReads int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/git/hosts":
			hostReads++
			_, _ = w.Write([]byte(`{"hosts":[]}`))
		case r.Method == http.MethodPut:
			raw, _ := io.ReadAll(r.Body)
			puts = append(puts, string(raw))
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			_, _ = w.Write([]byte(`{"server":{"port":8080}}`))
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(certPath, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	yaml := "data_dir: " + dir + "\nserver:\n  tls_enabled: true\n  tls_cert: " + certPath + "\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Server.TLSEnabled || cfg.Server.TLSCert != certPath {
		t.Fatalf("fixture did not enable TLS: enabled=%v cert=%q", cfg.Server.TLSEnabled, cfg.Server.TLSCert)
	}
	oldURL, oldPath := serverURL, cfgPath
	serverURL, cfgPath = srv.URL, path
	t.Cleanup(func() { serverURL, cfgPath = oldURL, oldPath })

	if err := runConfigGet(cfg, "server.port"); err != nil {
		t.Errorf("config get over TLS: %v", err)
	}
	if err := runConfigSet(cfg, "server.port", "8080"); err != nil {
		t.Errorf("config set over TLS: %v", err)
	}
	if _, err := fetchGitHosts(srv.URL, ""); err != nil {
		t.Errorf("config set git host readiness read over TLS: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(puts) != 1 || hostReads != 1 {
		t.Errorf("daemon saw %d writes and %d git host reads, want 1 and 1", len(puts), hostReads)
	}
}
