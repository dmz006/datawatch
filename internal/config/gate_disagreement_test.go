package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGateDisagreementConfigDefaultsAndValidation —
// autonomous.gate_disagreement must:
//   - default to "warn" in DefaultConfig() and when omitted from a
//     loaded config;
//   - accept "warn" and "block" through the load path;
//   - reject any other value on the load/write path;
//   - round-trip through YAML and JSON.
func TestGateDisagreementConfigDefaultsAndValidation(t *testing.T) {
	// Default value on a fresh config. Fresh DefaultConfig() leaves
	// ProjectWriteSerialization unset (its default is applied at Load
	// time), so only check gate_disagreement here.
	fresh := DefaultConfig()
	if got := fresh.Autonomous.GateDisagreement; got != "warn" {
		t.Fatalf("DefaultConfig().Autonomous.GateDisagreement = %q, want \"warn\"", got)
	}

	// Omitted in a saved config → Load must default it to "warn".
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	cfg.Autonomous.GateDisagreement = "" // simulate a key dropped by hand
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load with omitted gate_disagreement: %v (want default applied, no error)", err)
	}
	if got := loaded.Autonomous.GateDisagreement; got != "warn" {
		t.Fatalf("loaded gate_disagreement = %q, want \"warn\" (default)", got)
	}
	// Load must not clobber sibling autonomous settings: the load-time
	// default for ProjectWriteSerialization still applies.
	if got := loaded.Autonomous.ProjectWriteSerialization; got != "advisory" {
		t.Fatalf("unrelated setting clobbered on load: ProjectWriteSerialization = %q", got)
	}

	// "block" accepted.
	cfg = DefaultConfig()
	cfg.Autonomous.GateDisagreement = "block"
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save block: %v", err)
	}
	loaded, err = Load(p)
	if err != nil {
		t.Fatalf("Load with block: %v", err)
	}
	if got := loaded.Autonomous.GateDisagreement; got != "block" {
		t.Fatalf("loaded gate_disagreement = %q, want \"block\"", got)
	}
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), "gate_disagreement") {
		t.Fatalf("saved YAML missing gate_disagreement key:\n%s", raw)
	}

	// Unrecognized values rejected on the load path (same function the
	// write paths run).
	cfg = DefaultConfig()
	cfg.Autonomous.GateDisagreement = "sometimes"
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save invalid: %v", err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("Load with gate_disagreement=sometimes succeeded, want error")
	}
	cfg.Autonomous.GateDisagreement = "BLOCK" // case-sensitive
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save invalid case: %v", err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("Load with gate_disagreement=BLOCK succeeded, want error (values are case-sensitive)")
	}
}

// TestGateDisagreementValidateRejectsUnknown — unit check on the
// validator itself: only "warn"/"block" (and empty, which callers
// default) are valid.
func TestGateDisagreementValidateRejectsUnknown(t *testing.T) {
	for _, valid := range []string{"", "warn", "block"} {
		if err := validateGateDisagreement(valid); err != nil {
			t.Fatalf("validateGateDisagreement(%q) = %v, want nil", valid, err)
		}
	}
	for _, invalid := range []string{"sometimes", "BLOCK", "Warn", " ", "blocker"} {
		if err := validateGateDisagreement(invalid); err == nil {
			t.Fatalf("validateGateDisagreement(%q) = nil, want error", invalid)
		} else if !strings.Contains(err.Error(), "gate_disagreement") {
			t.Fatalf("validateGateDisagreement(%q) error %q missing setting name", invalid, err)
		}
	}
}
