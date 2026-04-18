package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestNewHealthCmd_Construction sanity-checks the cobra command shape so
// future flag changes don't break the HEALTHCHECK contract baked into
// the container images.
func TestNewHealthCmd_Construction(t *testing.T) {
	cmd := newHealthCmd()
	if cmd.Use != "health" {
		t.Errorf("Use=%q want health", cmd.Use)
	}
	if cmd.Run == nil {
		t.Error("Run function not set")
	}
	for _, want := range []string{"url", "endpoint", "timeout"} {
		if cmd.Flags().Lookup(want) == nil {
			t.Errorf("missing --%s flag (relied upon by Dockerfile HEALTHCHECK)", want)
		}
	}
	// Defaults that the container HEALTHCHECK uses without overriding
	if cmd.Flags().Lookup("url").DefValue != "http://127.0.0.1:8080" {
		t.Errorf("url default=%q want http://127.0.0.1:8080", cmd.Flags().Lookup("url").DefValue)
	}
	if cmd.Flags().Lookup("endpoint").DefValue != "/healthz" {
		t.Errorf("endpoint default=%q want /healthz", cmd.Flags().Lookup("endpoint").DefValue)
	}
}

// TestHealthCmd_Exits0OnHealthy spawns a fake healthz server, runs
// `datawatch health --url=...` as a subprocess, and asserts exit 0.
// Subprocess approach: newHealthCmd's Run calls os.Exit, so we can't
// invoke it in-process without aborting the test runner.
func TestHealthCmd_Exits0OnHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	bin := buildDatawatchBinary(t)
	out, err := exec.Command(bin, "health", "--url", srv.URL, "--timeout", "3").CombinedOutput()
	if err != nil {
		t.Fatalf("health exited non-zero: %v\noutput: %s", err, out)
	}
}

// TestHealthCmd_ExitsNonZeroOn5xx confirms a failing endpoint maps to
// non-zero exit so docker's HEALTHCHECK marks the container unhealthy.
func TestHealthCmd_ExitsNonZeroOn5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	bin := buildDatawatchBinary(t)
	// exit code is what HEALTHCHECK acts on; stderr text isn't reliable
	// after os.Exit (no flush). We assert non-zero exit only.
	out, err := exec.Command(bin, "health", "--url", srv.URL, "--timeout", "3").CombinedOutput()
	if err == nil {
		t.Fatalf("health should have failed on 503, output: %s", out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 0 {
			t.Errorf("expected non-zero exit, got 0")
		}
	}
}

// TestHealthCmd_ExitsNonZeroOnUnreachable confirms a refused connection
// maps to non-zero exit (avoids HEALTHCHECK silently passing on a dead
// daemon that hasn't bound the port yet).
func TestHealthCmd_ExitsNonZeroOnUnreachable(t *testing.T) {
	bin := buildDatawatchBinary(t)
	// localhost:1 is reserved tcpmux; nothing listens there in CI envs
	out, err := exec.Command(bin, "health", "--url", "http://127.0.0.1:1", "--timeout", "2").CombinedOutput()
	if err == nil {
		t.Fatalf("health should have failed on unreachable, output: %s", out)
	}
}

// buildDatawatchBinary compiles cmd/datawatch into a tmp dir once per
// test session. Cached so repeated subtests don't recompile.
var compiledBinary string

func buildDatawatchBinary(t *testing.T) string {
	t.Helper()
	if compiledBinary != "" {
		return compiledBinary
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "datawatch")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build datawatch binary (env may not have go): %v\n%s", err, out)
	}
	compiledBinary = bin
	return bin
}
