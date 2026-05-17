// BL302 S3 — unit tests for TriggerRegistry.
package mcp

import (
	"testing"
)

// TestTriggerRegistry_BuiltinTriggers verifies that all 5 built-in trigger
// constants are registered in the global registry.
func TestTriggerRegistry_BuiltinTriggers(t *testing.T) {
	r := NewTriggerRegistry()

	builtins := []string{
		TriggerAlertTriage,
		TriggerAnomalyAnalysis,
		TriggerMorningBriefing,
		TriggerCouncilDeliberation,
		TriggerAutomatonDecision,
	}

	for _, name := range builtins {
		tmpl, ok := r.Get(name)
		if !ok {
			t.Errorf("builtin trigger %q not registered", name)
			continue
		}
		if tmpl.PromptTemplate == "" {
			t.Errorf("trigger %q has empty PromptTemplate", name)
		}
		if tmpl.DefaultMaxTokens <= 0 {
			t.Errorf("trigger %q has non-positive DefaultMaxTokens: %d", name, tmpl.DefaultMaxTokens)
		}
	}
}

// TestTriggerRegistry_Names verifies Names() returns at least the 5 builtins.
func TestTriggerRegistry_Names(t *testing.T) {
	r := NewTriggerRegistry()
	names := r.Names()
	if len(names) < 5 {
		t.Errorf("expected at least 5 trigger names, got %d", len(names))
	}
}

// TestTriggerRegistry_Register verifies that custom triggers can be registered
// and retrieved.
func TestTriggerRegistry_Register(t *testing.T) {
	r := NewTriggerRegistry()
	r.Register(TriggerTemplate{
		Name:             "custom_trigger",
		PromptTemplate:   "Custom: {{.Value}}",
		DefaultMaxTokens: 128,
	})

	tmpl, ok := r.Get("custom_trigger")
	if !ok {
		t.Fatal("custom_trigger not found after Register")
	}
	if tmpl.DefaultMaxTokens != 128 {
		t.Errorf("expected DefaultMaxTokens 128, got %d", tmpl.DefaultMaxTokens)
	}
}

// TestTriggerRegistry_GlobalNotNil verifies GlobalTriggerRegistry is pre-populated.
func TestTriggerRegistry_GlobalNotNil(t *testing.T) {
	if GlobalTriggerRegistry == nil {
		t.Fatal("GlobalTriggerRegistry is nil")
	}
	names := GlobalTriggerRegistry.Names()
	if len(names) == 0 {
		t.Fatal("GlobalTriggerRegistry has no triggers")
	}
}

// TestTriggerRegistry_UnknownReturnsNotFound verifies Get returns false for
// unknown trigger names.
func TestTriggerRegistry_UnknownReturnsNotFound(t *testing.T) {
	r := NewTriggerRegistry()
	_, ok := r.Get("does_not_exist")
	if ok {
		t.Error("expected ok=false for unknown trigger")
	}
}
