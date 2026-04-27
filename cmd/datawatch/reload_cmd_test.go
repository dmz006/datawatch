// v5.26.3 — newReloadCmd test (audit gap from 2026-04-26 retrospective).
// The CLI was added in v5.7.0 to fill a config-parity gap (BL17 already
// had SIGHUP + POST /api/reload + the MCP `reload` tool) but shipped
// without test coverage. The cobra-shape + RunE-success path are
// straightforward to cover; the daemon-reachability path is exercised
// indirectly by the broader cli_sx_parity_test suite.

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeReloadTestConfig(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func TestNewReloadCmd_CobraShape(t *testing.T) {
	c := newReloadCmd()
	if c.Use != "reload" {
		t.Errorf("Use=%q want reload", c.Use)
	}
	if c.Short == "" {
		t.Error("Short help missing")
	}
	if !strings.Contains(strings.ToLower(c.Long), "hot-reload") {
		t.Errorf("Long help should mention hot-reload; got: %q", c.Long)
	}
	if c.RunE == nil {
		t.Error("RunE not wired")
	}
}

// TestNewReloadCmd_HitsReloadEndpoint stands up a fake daemon and
// points the CLI at it via a config file in a temp $HOME, then runs
// the command's RunE and asserts POST /api/reload was hit.
func TestNewReloadCmd_HitsReloadEndpoint(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/reload" && r.Method == http.MethodPost {
			hits++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfgYAML := "server:\n  port: " + strconv.Itoa(port) + "\n"
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", cfgYAML); err != nil {
		t.Fatalf("write config: %v", err)
	}

	c := newReloadCmd()
	if err := c.RunE(c, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if hits != 1 {
		t.Errorf("/api/reload hits = %d, want 1", hits)
	}
}

// TestNewReloadCmd_PropagatesErrors ensures non-2xx responses surface
// as an error rather than printing "ok" silently.
func TestNewReloadCmd_PropagatesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+strconv.Itoa(port)+"\n"); err != nil {
		t.Fatalf("write config: %v", err)
	}

	c := newReloadCmd()
	err := c.RunE(c, nil)
	if err == nil {
		t.Fatal("RunE: want error on 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention HTTP 500; got: %v", err)
	}
}
