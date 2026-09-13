package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readProjectConfig(t *testing.T, dir string) projectConfig {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	var cfg projectConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal opencode.json: %v", err)
	}
	return cfg
}

// TestWriteProjectConfig_OllamaProvider verifies the "provider" block
// written for an ollama/* model matches the shape OpenCode actually
// recognizes (npm + options.baseURL + models map) — a bare {"apiUrl": ...}
// block is silently ignored by opencode and the model never becomes
// selectable (verified against opencode 1.18.25 via `opencode models`).
func TestWriteProjectConfig_OllamaProvider(t *testing.T) {
	dir := t.TempDir()
	if err := WriteProjectConfig(dir, ProjectConfigOpts{
		Model:     "ollama/qwen3.8:27b",
		OllamaURL: "http://datawatch:11434",
	}); err != nil {
		t.Fatalf("WriteProjectConfig: %v", err)
	}
	cfg := readProjectConfig(t, dir)

	if cfg.Model != "ollama/qwen3.8:27b" {
		t.Errorf("Model = %q, want ollama/qwen3.8:27b", cfg.Model)
	}
	ollama, ok := cfg.Provider["ollama"].(map[string]any)
	if !ok {
		t.Fatalf("provider.ollama missing or wrong type: %#v", cfg.Provider["ollama"])
	}
	if ollama["npm"] != "@ai-sdk/openai-compatible" {
		t.Errorf("provider.ollama.npm = %v, want @ai-sdk/openai-compatible", ollama["npm"])
	}
	opts, ok := ollama["options"].(map[string]any)
	if !ok {
		t.Fatalf("provider.ollama.options missing or wrong type: %#v", ollama["options"])
	}
	if opts["baseURL"] != "http://datawatch:11434/v1" {
		t.Errorf("provider.ollama.options.baseURL = %v, want http://datawatch:11434/v1", opts["baseURL"])
	}
	models, ok := ollama["models"].(map[string]any)
	if !ok {
		t.Fatalf("provider.ollama.models missing or wrong type: %#v", ollama["models"])
	}
	if _, ok := models["qwen3.8:27b"]; !ok {
		t.Errorf("provider.ollama.models missing key %q: %#v", "qwen3.8:27b", models)
	}
}

// TestWriteProjectConfig_OllamaProvider_DefaultLocalURL covers the
// no-compute-node case: OllamaURL empty must still default to the local
// Ollama daemon so a bare "ollama/<model>" pick works without a pinned
// compute node.
func TestWriteProjectConfig_OllamaProvider_DefaultLocalURL(t *testing.T) {
	dir := t.TempDir()
	if err := WriteProjectConfig(dir, ProjectConfigOpts{Model: "ollama/qwen3:1.7b"}); err != nil {
		t.Fatalf("WriteProjectConfig: %v", err)
	}
	cfg := readProjectConfig(t, dir)
	ollama := cfg.Provider["ollama"].(map[string]any)
	opts := ollama["options"].(map[string]any)
	if opts["baseURL"] != "http://localhost:11434/v1" {
		t.Errorf("baseURL = %v, want http://localhost:11434/v1", opts["baseURL"])
	}
}

// TestWriteProjectConfig_OllamaProvider_SchemelessURL verifies that a compute
// node address stored without an "http://" scheme (e.g. "datawatch:11434") is
// auto-corrected to a valid URL so opencode never sees an unparseable baseURL.
// This mirrors the root cause of the v8.25.6 session-stall bug where the node
// address "127.0.0.1:53106" produced "127.0.0.1:53106/v1" which opencode
// immediately rejected, leaving every autonomous session in waiting_input.
func TestWriteProjectConfig_OllamaProvider_SchemelessURL(t *testing.T) {
	dir := t.TempDir()
	if err := WriteProjectConfig(dir, ProjectConfigOpts{
		Model:     "ollama/qwen3.8:27b",
		OllamaURL: "datawatch:11434", // no http:// scheme
	}); err != nil {
		t.Fatalf("WriteProjectConfig: %v", err)
	}
	cfg := readProjectConfig(t, dir)
	ollama, ok := cfg.Provider["ollama"].(map[string]any)
	if !ok {
		t.Fatalf("provider.ollama missing: %#v", cfg.Provider)
	}
	opts, ok := ollama["options"].(map[string]any)
	if !ok {
		t.Fatalf("provider.ollama.options missing: %#v", ollama)
	}
	got, _ := opts["baseURL"].(string)
	if got != "http://datawatch:11434/v1" {
		t.Errorf("baseURL = %q, want http://datawatch:11434/v1 (scheme-less must be auto-corrected)", got)
	}
}

// TestWriteProjectConfig_NonOllamaModel_NoProviderBlock ensures cloud/free
// builtin models (anthropic/*, opencode/*) never get an ollama provider
// block written — only "ollama/" prefixed models do.
func TestWriteProjectConfig_NonOllamaModel_NoProviderBlock(t *testing.T) {
	dir := t.TempDir()
	if err := WriteProjectConfig(dir, ProjectConfigOpts{Model: "anthropic/claude-sonnet-4-6"}); err != nil {
		t.Fatalf("WriteProjectConfig: %v", err)
	}
	cfg := readProjectConfig(t, dir)
	if cfg.Provider != nil {
		t.Errorf("Provider = %#v, want nil for non-ollama model", cfg.Provider)
	}
}
