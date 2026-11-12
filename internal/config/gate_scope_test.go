package config

// Gate-scope config (docs/plans/2026-10-23-quality-gate-scope.md), Setting
// model: the scope value is validated on load, the default scope is empty
// (inherit the built-in full default), and the built-in smoke list applies
// when smoke_packages is unset.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeGateScopeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

func TestGateScope_DefaultScopeEmptySmokeListBuiltin(t *testing.T) {
	p := writeGateScopeConfig(t, "autonomous:\n  enabled: true\n")

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load with omitted gate scope: %v (want default applied, no error)", err)
	}
	gates := cfg.Autonomous.DefaultQualityGates
	if gates.Scope != "" {
		t.Errorf("default scope = %q, want empty (empty inherits the built-in default)", gates.Scope)
	}
	if !reflect.DeepEqual(gates.SmokePackages, DefaultGateSmokePackages) {
		t.Errorf("default smoke_packages = %v, want the built-in list %v", gates.SmokePackages, DefaultGateSmokePackages)
	}
}

func TestGateScope_LoadAcceptsValidScopes(t *testing.T) {
	for _, scope := range []string{"full", "touched-plus-smoke", "touched"} {
		cfg, err := Load(writeGateScopeConfig(t,
			"autonomous:\n  default_quality_gates:\n    enabled: true\n    scope: "+scope+"\n"))
		if err != nil {
			t.Fatalf("Load(scope=%q) failed: %v", scope, err)
		}
		if got := cfg.Autonomous.DefaultQualityGates.Scope; got != scope {
			t.Errorf("scope = %q, want %q", got, scope)
		}
	}
}

func TestGateScope_LoadSecureAcceptsValidScopes(t *testing.T) {
	for _, scope := range []string{"full", "touched-plus-smoke", "touched"} {
		cfg, err := LoadSecure(writeGateScopeConfig(t,
			"autonomous:\n  default_quality_gates:\n    enabled: true\n    scope: "+scope+"\n"), nil)
		if err != nil {
			t.Fatalf("LoadSecure(scope=%q) failed: %v", scope, err)
		}
		if got := cfg.Autonomous.DefaultQualityGates.Scope; got != scope {
			t.Errorf("scope = %q, want %q", got, scope)
		}
	}
}

func TestGateScope_LoadRejectsInvalidScope(t *testing.T) {
	p := writeGateScopeConfig(t,
		"autonomous:\n  default_quality_gates:\n    enabled: true\n    scope: everything\n")
	_, err := Load(p)
	if err == nil {
		t.Fatal("Load with scope=everything succeeded, want error")
	}
	msg := err.Error()
	for _, want := range []string{"scope", "full", "touched-plus-smoke", "touched"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should name %q, got: %v", want, err)
		}
	}
}

func TestGateScope_LoadSecureRejectsInvalidScope(t *testing.T) {
	p := writeGateScopeConfig(t,
		"autonomous:\n  default_quality_gates:\n    enabled: true\n    scope: everything\n")
	if _, err := LoadSecure(p, nil); err == nil {
		t.Fatal("LoadSecure with scope=everything succeeded, want error")
	}
}

func TestGateScope_ValidateGateScopeNamesValidValues(t *testing.T) {
	valid := []string{"", "full", "touched-plus-smoke", "touched"}
	for _, v := range valid {
		if err := validateGateScope(v); err != nil {
			t.Errorf("validateGateScope(%q) failed: %v", v, err)
		}
	}
	if err := validateGateScope("BLOCK"); err == nil {
		t.Fatal("validateGateScope(BLOCK) succeeded, want error (values are case-sensitive)")
	}
}
