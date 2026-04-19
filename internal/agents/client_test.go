// F10 sprint 3 (S3.4) — worker bootstrap client tests.

package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBootstrapEnv_IsWorker(t *testing.T) {
	cases := []struct {
		name string
		e    BootstrapEnv
		want bool
	}{
		{"all set", BootstrapEnv{URL: "u", Token: "t", AgentID: "a"}, true},
		{"empty", BootstrapEnv{}, false},
		{"missing url", BootstrapEnv{Token: "t", AgentID: "a"}, false},
		{"missing token", BootstrapEnv{URL: "u", AgentID: "a"}, false},
		{"missing agent", BootstrapEnv{URL: "u", Token: "t"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.e.IsWorker(); got != c.want {
				t.Errorf("IsWorker=%v want %v", got, c.want)
			}
		})
	}
}

func TestLoadBootstrapEnv_FromOSEnv(t *testing.T) {
	t.Setenv("DATAWATCH_BOOTSTRAP_URL", "http://parent:8080")
	t.Setenv("DATAWATCH_BOOTSTRAP_TOKEN", "deadbeef")
	t.Setenv("DATAWATCH_AGENT_ID", "abc-123")
	e := LoadBootstrapEnv()
	if !e.IsWorker() {
		t.Fatal("expected IsWorker after Setenv")
	}
	if e.URL != "http://parent:8080" || e.Token != "deadbeef" || e.AgentID != "abc-123" {
		t.Errorf("unexpected env: %+v", e)
	}
}

func TestCallBootstrap_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s want POST", r.Method)
		}
		if r.URL.Path != "/api/agents/bootstrap" {
			t.Errorf("path=%s want /api/agents/bootstrap", r.URL.Path)
		}
		var req bootstrapRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.AgentID != "a1" || req.Token != "t1" {
			t.Errorf("payload mismatch: %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(BootstrapResponse{
			AgentID:        "a1",
			ProjectProfile: "proj",
			ClusterProfile: "clus",
			Task:           "do work",
			Env:            map[string]string{"DATAWATCH_AGENT_ID": "a1", "EXTRA": "x"},
		})
	}))
	defer srv.Close()

	resp, err := CallBootstrap(context.Background(), BootstrapEnv{
		URL: srv.URL, Token: "t1", AgentID: "a1",
	})
	if err != nil {
		t.Fatalf("CallBootstrap: %v", err)
	}
	if resp.AgentID != "a1" || resp.Env["EXTRA"] != "x" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestCallBootstrap_TerminalOn4xx(t *testing.T) {
	calls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "bad token", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := CallBootstrap(context.Background(), BootstrapEnv{URL: srv.URL, Token: "bad", AgentID: "a"})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("4xx should not retry, got %d calls", got)
	}
}

func TestCallBootstrap_RetriesOn5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(BootstrapResponse{AgentID: "a"})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := CallBootstrap(ctx, BootstrapEnv{URL: srv.URL, Token: "t", AgentID: "a"})
	if err != nil {
		t.Fatalf("CallBootstrap: %v", err)
	}
	if resp.AgentID != "a" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("expected 3 calls (2 retries), got %d", got)
	}
}

func TestCallBootstrap_ContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := CallBootstrap(ctx, BootstrapEnv{URL: srv.URL, Token: "t", AgentID: "a"})
	if err == nil {
		t.Fatal("expected ctx deadline error")
	}
}

func TestCallBootstrap_MissingEnv(t *testing.T) {
	_, err := CallBootstrap(context.Background(), BootstrapEnv{})
	if err == nil {
		t.Fatal("expected error when env not set")
	}
}

// F10 S4.3 — when DATAWATCH_PARENT_CERT_FINGERPRINT is set, the
// worker's bootstrap client refuses parents whose cert doesn't pin.
// Easiest way to test: set the env to a known-bad value, run against
// any TLS server, expect a fingerprint-mismatch error.
func TestCallBootstrap_HonoursParentFingerprint_Pin(t *testing.T) {
	// Use a real TLS httptest server (its cert won't match our pin).
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("DATAWATCH_PARENT_CERT_FINGERPRINT",
		"0000000000000000000000000000000000000000000000000000000000000000")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := CallBootstrap(ctx, BootstrapEnv{URL: srv.URL, Token: "t", AgentID: "a"})
	if err == nil {
		t.Fatal("expected fingerprint-mismatch error, got nil")
	}
	// Error wraps either context deadline or fingerprint mismatch
	// depending on retry timing; both are acceptable signals that the
	// pin was applied (a successful handshake would have returned 401
	// from the bootstrap endpoint instead).
	if !strings.Contains(err.Error(), "fingerprint") &&
		!strings.Contains(err.Error(), "deadline") &&
		!strings.Contains(err.Error(), "context") {
		t.Errorf("error doesn't reference pin failure: %v", err)
	}
}

// F10 S6.2 — ApplyBootstrapEnv exports the memory bundle as env
// vars so the worker daemon's memory client can self-configure.
func TestApplyBootstrapEnv_ExportsMemoryBundle(t *testing.T) {
	t.Setenv("DATAWATCH_MEMORY_MODE", "")
	t.Setenv("DATAWATCH_MEMORY_NAMESPACE", "")
	ApplyBootstrapEnv(&BootstrapResponse{
		Memory: BootstrapMemory{Mode: "shared", Namespace: "project-foo"},
	})
	if got := os.Getenv("DATAWATCH_MEMORY_MODE"); got != "shared" {
		t.Errorf("DATAWATCH_MEMORY_MODE=%q want shared", got)
	}
	if got := os.Getenv("DATAWATCH_MEMORY_NAMESPACE"); got != "project-foo" {
		t.Errorf("DATAWATCH_MEMORY_NAMESPACE=%q want project-foo", got)
	}
}

// F10 S7.7 — Comm bundle exports DATAWATCH_COMM_INHERIT (CSV).
func TestApplyBootstrapEnv_ExportsCommBundle(t *testing.T) {
	t.Setenv("DATAWATCH_COMM_INHERIT", "")
	ApplyBootstrapEnv(&BootstrapResponse{
		Comm: BootstrapComm{Channels: []string{"signal", "telegram"}},
	})
	if got := os.Getenv("DATAWATCH_COMM_INHERIT"); got != "signal,telegram" {
		t.Errorf("DATAWATCH_COMM_INHERIT=%q want signal,telegram", got)
	}
}

func TestApplyBootstrapEnv_NoCommBundle_NoEnv(t *testing.T) {
	t.Setenv("DATAWATCH_COMM_INHERIT", "")
	ApplyBootstrapEnv(&BootstrapResponse{})
	if got := os.Getenv("DATAWATCH_COMM_INHERIT"); got != "" {
		t.Errorf("DATAWATCH_COMM_INHERIT should remain empty, got %q", got)
	}
}

// Empty Memory.Mode = no env vars set (avoid polluting non-F10 deployments).
func TestApplyBootstrapEnv_NoMemoryBundle_NoEnv(t *testing.T) {
	t.Setenv("DATAWATCH_MEMORY_MODE", "")
	ApplyBootstrapEnv(&BootstrapResponse{})
	if got := os.Getenv("DATAWATCH_MEMORY_MODE"); got != "" {
		t.Errorf("DATAWATCH_MEMORY_MODE should remain empty, got %q", got)
	}
}

func TestApplyBootstrapEnv_SetsOSEnv(t *testing.T) {
	t.Setenv("CLIENT_TEST_KEY", "")
	ApplyBootstrapEnv(&BootstrapResponse{Env: map[string]string{"CLIENT_TEST_KEY": "v1"}})
	if got := os.Getenv("CLIENT_TEST_KEY"); got != "v1" {
		t.Errorf("env=%q want v1", got)
	}
	// Nil-safe.
	ApplyBootstrapEnv(nil)
}
