package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// verifierRunCapLoadIsolate points HOME and the XDG dirs at the test's own
// temporary directory, so the side loaders that run inside Load/LoadSecure
// (shipped, tags, defect queue, auto-tags) never read the operator's data or
// config folders. It returns the path of a fresh config file under that
// directory, with the given YAML body written to it.
func verifierRunCapLoadIsolate(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestVerifierRunCapConfigLoadOmittedKeyDefaults2700(t *testing.T) {
	// The file sets an unrelated autonomous key but omits verifier_max_run_seconds.
	path := verifierRunCapLoadIsolate(t, "autonomous:\n  enabled: true\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Autonomous.VerifierMaxRunSeconds; got != VerifierRunCapDefaultSeconds {
		t.Fatalf("Load with omitted key: VerifierMaxRunSeconds = %d, want %d", got, VerifierRunCapDefaultSeconds)
	}
}

func TestVerifierRunCapConfigLoadMissingFileDefaults2700(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	cfg, err := Load(filepath.Join(root, "absent.yaml"))
	if err != nil {
		t.Fatalf("Load absent file: %v", err)
	}
	if got := cfg.Autonomous.VerifierMaxRunSeconds; got != VerifierRunCapDefaultSeconds {
		t.Fatalf("Load absent file: VerifierMaxRunSeconds = %d, want %d", got, VerifierRunCapDefaultSeconds)
	}
}

func TestVerifierRunCapConfigLoadExplicitValueKept(t *testing.T) {
	path := verifierRunCapLoadIsolate(t, "autonomous:\n  verifier_max_run_seconds: 900\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Autonomous.VerifierMaxRunSeconds; got != 900 {
		t.Fatalf("Load explicit 900: VerifierMaxRunSeconds = %d, want 900", got)
	}
}

func TestVerifierRunCapConfigLoadExplicitZeroKept(t *testing.T) {
	// 0 means no cap and must not be replaced by the 2700 default.
	path := verifierRunCapLoadIsolate(t, "autonomous:\n  verifier_max_run_seconds: 0\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load explicit 0: %v", err)
	}
	if got := cfg.Autonomous.VerifierMaxRunSeconds; got != 0 {
		t.Fatalf("Load explicit 0: VerifierMaxRunSeconds = %d, want 0 (no cap)", got)
	}
}

func TestVerifierRunCapConfigLoadNegativeRefused(t *testing.T) {
	path := verifierRunCapLoadIsolate(t, "autonomous:\n  verifier_max_run_seconds: -5\n")
	cfg, err := Load(path)
	if err == nil {
		t.Fatalf("Load negative: got cfg=%v, want error", cfg != nil)
	}
	if cfg != nil {
		t.Fatalf("Load negative: returned a config alongside the error")
	}
	if !strings.Contains(err.Error(), "autonomous.verifier_max_run_seconds") {
		t.Fatalf("Load negative: error %q does not name autonomous.verifier_max_run_seconds", err)
	}
}

func TestVerifierRunCapConfigLoadSecureOmittedKeyDefaults2700(t *testing.T) {
	path := verifierRunCapLoadIsolate(t, "autonomous:\n  enabled: true\n")
	cfg, err := LoadSecure(path, nil)
	if err != nil {
		t.Fatalf("LoadSecure: %v", err)
	}
	if got := cfg.Autonomous.VerifierMaxRunSeconds; got != VerifierRunCapDefaultSeconds {
		t.Fatalf("LoadSecure with omitted key: VerifierMaxRunSeconds = %d, want %d", got, VerifierRunCapDefaultSeconds)
	}
}

func TestVerifierRunCapConfigLoadSecureNegativeRefused(t *testing.T) {
	path := verifierRunCapLoadIsolate(t, "autonomous:\n  verifier_max_run_seconds: -1\n")
	_, err := LoadSecure(path, nil)
	if err == nil {
		t.Fatalf("LoadSecure negative: want error")
	}
	if !strings.Contains(err.Error(), "autonomous.verifier_max_run_seconds") {
		t.Fatalf("LoadSecure negative: error %q does not name autonomous.verifier_max_run_seconds", err)
	}
}
