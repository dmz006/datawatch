// Shared validation for the autonomous.requirements min-Ollama overrides:
// the one rule set every config surface (REST PUT, autonomous config, CLI
// config set, comm configure, web settings) reuses.

package autonomous

import (
	"strings"
	"testing"
)

// TestValidateMinOllamaOverrides accepts the legal shapes and refuses the
// illegal ones, naming the offending capability and value.
func TestValidateMinOllamaOverrides(t *testing.T) {
	valid := []map[string]string{
		{},  // empty clears all overrides
		nil, // nil is equivalent
		{"classification_verdict": "0.9.1"},
		{"classification_verdict": "  0.35.0 "}, // whitespace-trimmed
		{"classification_verdict": ""},          // empty clears the floor
	}
	for i, m := range valid {
		if err := ValidateMinOllamaOverrides(m); err != nil {
			t.Errorf("valid case %d (%v): %v", i, m, err)
		}
	}

	bad := []struct {
		m    map[string]string
		want string
	}{
		{map[string]string{"not_a_capability": "0.35.0"}, `unknown capability "not_a_capability"`},
		{map[string]string{CapabilityClassificationVerdict: "abc"}, `invalid minimum Ollama "abc"`},
		{map[string]string{CapabilityClassificationVerdict: "3"}, `invalid minimum Ollama "3"`},
		{map[string]string{"": "0.35.0"}, "capability key must not be empty"},
	}
	for i, c := range bad {
		err := ValidateMinOllamaOverrides(c.m)
		if err == nil {
			t.Errorf("invalid case %d (%v): want error, got nil", i, c.m)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("invalid case %d (%v): error %q, want substring %q", i, c.m, err, c.want)
		}
	}
}

// TestNormalizeMinOllamaOverrides trims keys and values, keeps empty-valued
// (clear) entries, does not mutate the caller's map, and carries the
// validation error through unchanged.
func TestNormalizeMinOllamaOverrides(t *testing.T) {
	got, err := NormalizeMinOllamaOverrides(map[string]string{
		" classification_verdict ": " 0.9.1 ",
	})
	if err != nil {
		t.Fatalf("valid normalize: %v", err)
	}
	if got[CapabilityClassificationVerdict] != "0.9.1" {
		t.Errorf("classification_verdict = %q, want 0.9.1 (trimmed)", got[CapabilityClassificationVerdict])
	}

	// Empty-valued (clear) entries survive normalization.
	empty, err := NormalizeMinOllamaOverrides(map[string]string{CapabilityClassificationVerdict: " "})
	if err != nil {
		t.Fatalf("clear normalize: %v", err)
	}
	if v, ok := empty[CapabilityClassificationVerdict]; !ok || v != "" {
		t.Errorf("clear entry = %q (%v), want empty string", v, ok)
	}

	// The caller's map must not be mutated.
	src := map[string]string{" classification_verdict ": " 0.9.1 "}
	if _, err := NormalizeMinOllamaOverrides(src); err != nil {
		t.Fatalf("valid normalize: %v", err)
	}
	if src[" classification_verdict "] != " 0.9.1 " {
		t.Errorf("input map mutated: %v", src)
	}

	if _, err := NormalizeMinOllamaOverrides(nil); err != nil {
		t.Errorf("nil input: %v", err)
	}
	if _, err := NormalizeMinOllamaOverrides(map[string]string{"bogus": "0.1"}); err == nil {
		t.Error("unknown capability: want error")
	}
}
