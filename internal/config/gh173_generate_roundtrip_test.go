// GH#173 — `datawatch config generate` wrote the four detection pattern
// lists as the literal YAML string "[]" instead of a real empty list
// (fieldi(&b, "prompt_patterns", "[]", ...) passed a Go string; yamlVal
// quotes a string containing '[]', producing `prompt_patterns: "[]"`,
// which the loader then refuses to unmarshal into []string). Repro:
// `datawatch config generate > cfg.yaml && datawatch config show
// --config cfg.yaml` failed with "cannot unmarshal !!str `[]` into
// []string".

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYamlVal_EmptyStringSlice_RendersAsEmptyList(t *testing.T) {
	got := yamlVal([]string{})
	if got != "[]" {
		t.Fatalf("yamlVal([]string{}) = %q, want %q (unquoted empty list)", got, "[]")
	}
	if got == `"[]"` {
		t.Fatal("regression: rendered as the literal string \"[]\" instead of a real empty list")
	}
}

func TestYamlVal_NonEmptyStringSlice_RendersAsFlowSequence(t *testing.T) {
	got := yamlVal([]string{"waiting for input", "simple"})
	want := `["waiting for input", simple]`
	if got != want {
		t.Fatalf("yamlVal([]string{...}) = %q, want %q", got, want)
	}
}

func TestYamlVal_StringSliceElementWithComma_IsQuoted(t *testing.T) {
	// A bare comma inside a flow-sequence element would split it into
	// two elements; it must be quoted even though a comma is harmless
	// in a plain (non-sequence) scalar.
	got := yamlVal([]string{"a,b"})
	if got != `["a,b"]` {
		t.Fatalf("yamlVal([]string{\"a,b\"}) = %q, want %q", got, `["a,b"]`)
	}
}

// TestGenerateAnnotatedConfig_RoundTrips is exactly the issue's own
// suggested regression test: generate a config, then load it back.
func TestGenerateAnnotatedConfig_RoundTrips(t *testing.T) {
	yamlStr := GenerateAnnotatedConfig(DefaultConfig())

	for _, key := range []string{"prompt_patterns", "completion_patterns", "rate_limit_patterns", "input_needed_patterns"} {
		if strings.Contains(yamlStr, key+`: "[]"`) {
			t.Errorf("generated config still emits %s as the literal string \"[]\"", key)
		}
		if !strings.Contains(yamlStr, key+": []") {
			t.Errorf("generated config missing expected %s: [] (empty list)", key)
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "generated.yaml")
	if err := os.WriteFile(path, []byte(yamlStr), 0o600); err != nil {
		t.Fatalf("write generated config: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("a generated config must load back cleanly: %v", err)
	}
	if len(loaded.Detection.PromptPatterns) != 0 {
		t.Errorf("PromptPatterns = %v, want empty", loaded.Detection.PromptPatterns)
	}
}
