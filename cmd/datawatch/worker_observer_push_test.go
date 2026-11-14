// worker_observer_push_test.go — unit tests for the TLS-posture wiring of
// the worker observer push and the federation pusher warning extraction.
// Every test drives the helpers with an in-memory fake getenv; the real
// process environment is never consulted.
package main

import (
	"strings"
	"testing"

	observerpkg "github.com/dmz006/datawatch/internal/observer"
	observerpeerpkg "github.com/dmz006/datawatch/internal/observerpeer"
)

// fakeEnv is an in-memory getenv for tests.
func fakeEnv(vars map[string]string) func(string) string {
	return func(key string) string {
		v, _ := vars[key]
		return v
	}
}

func TestWorkerObserverPushConfigPinned(t *testing.T) {
	pin := strings.Repeat("ab", 32)
	getenv := fakeEnv(map[string]string{
		observerpeerpkg.EnvParentCertFingerprint: pin,
		// Opt-out set too — the pin must win.
		observerpeerpkg.EnvPushInsecure: "1",
	})
	cfg, warnings := workerObserverPushConfig("worker-a", "https://parent.example:8443", getenv)
	if cfg.PinnedSHA256 != pin {
		t.Fatalf("PinnedSHA256 = %q, want %q", cfg.PinnedSHA256, pin)
	}
	if cfg.Insecure {
		t.Fatal("Insecure = true, want false when a pin is set")
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none when pinned", warnings)
	}
	if cfg.ParentURL != "https://parent.example:8443" || cfg.Name != "worker-a" || cfg.Shape != "A" {
		t.Fatalf("unexpected base config: %+v", cfg)
	}
}

func TestWorkerObserverPushConfigSystemTrust(t *testing.T) {
	cfg, warnings := workerObserverPushConfig("worker-a", "https://parent.example:8443", fakeEnv(nil))
	if cfg.Insecure {
		t.Fatal("Insecure = true, want false with no opt-out")
	}
	if strings.TrimSpace(cfg.PinnedSHA256) != "" {
		t.Fatalf("PinnedSHA256 = %q, want empty", cfg.PinnedSHA256)
	}
	if len(warnings) != 1 {
		t.Fatalf("len(warnings) = %d, want 1 (the system-trust notice)", len(warnings))
	}
	if !strings.Contains(warnings[0], "system trust store") {
		t.Fatalf("notice %q does not mention the system trust store", warnings[0])
	}
}

func TestWorkerObserverPushConfigInsecureOptOut(t *testing.T) {
	getenv := fakeEnv(map[string]string{
		observerpeerpkg.EnvPushInsecure: "true",
	})
	cfg, warnings := workerObserverPushConfig("worker-a", "https://parent.example:8443", getenv)
	if !cfg.Insecure {
		t.Fatal("Insecure = false, want true with the opt-out set")
	}
	if strings.TrimSpace(cfg.PinnedSHA256) != "" {
		t.Fatalf("PinnedSHA256 = %q, want empty", cfg.PinnedSHA256)
	}
	if len(warnings) != 1 {
		t.Fatalf("len(warnings) = %d, want 1 (the skip-verification warning)", len(warnings))
	}
	if !strings.Contains(warnings[0], "SKIPPED") {
		t.Fatalf("warning %q does not announce skipped verification", warnings[0])
	}
}

func TestWorkerObserverPushConfigHTTPNoTLSWarnings(t *testing.T) {
	getenv := fakeEnv(map[string]string{
		// Env noise that must not matter for an http parent.
		observerpeerpkg.EnvPushInsecure: "",
	})
	cfg, warnings := workerObserverPushConfig("worker-a", "http://parent.example:8080", getenv)
	if cfg.Insecure {
		t.Fatal("Insecure = true, want false for an http parent with no opt-out")
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none for an http parent", warnings)
	}
}

func TestFederationPushWarningOnlyWhenInsecure(t *testing.T) {
	secure := federationInsecureWarning(observerpkg.FederationCfg{
		ParentURL: "https://root.example:8443",
		Insecure:  false,
	})
	if secure != "" {
		t.Fatalf("warning = %q, want empty when verification is on", secure)
	}

	skipped := federationInsecureWarning(observerpkg.FederationCfg{
		ParentURL: "https://root.example:8443",
		Insecure:  true,
	})
	if skipped == "" {
		t.Fatal("warning = \"\", want a visible warning when insecure")
	}
	if !strings.Contains(skipped, "SKIPPED") || !strings.Contains(skipped, "root.example") {
		t.Fatalf("warning %q does not name the skipped verification or the root", skipped)
	}
}
