// BL201 — voice/whisper backend inherits endpoint + API key from the
// matching LLM config so operators don't configure twice.

package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	transcribePkg "github.com/dmz006/datawatch/internal/transcribe"
)

func TestInheritWhisperEndpoint_OpenWebUI(t *testing.T) {
	cfg := &config.Config{
		OpenWebUI: config.OpenWebUIConfig{
			URL:    "https://owui.local:8080",
			APIKey: "owui-key-abc",
		},
	}
	in := transcribePkg.BackendConfig{Backend: "openwebui"}
	got := inheritWhisperEndpoint(in, cfg)

	if got.Endpoint != "https://owui.local:8080" {
		t.Fatalf("openwebui endpoint not inherited: got %q", got.Endpoint)
	}
	if got.APIKey != "owui-key-abc" {
		t.Fatalf("openwebui api_key not inherited: got %q", got.APIKey)
	}
}

func TestInheritWhisperEndpoint_OpenWebUI_ExplicitOverride(t *testing.T) {
	// Explicit whisper.endpoint must win over LLM-config inheritance —
	// the inherit step is purely a fallback for blank fields.
	cfg := &config.Config{
		OpenWebUI: config.OpenWebUIConfig{
			URL:    "https://owui.local:8080",
			APIKey: "owui-key-abc",
		},
	}
	in := transcribePkg.BackendConfig{
		Backend:  "openwebui",
		Endpoint: "https://override.example/api",
		APIKey:   "explicit-key",
	}
	got := inheritWhisperEndpoint(in, cfg)

	if got.Endpoint != "https://override.example/api" {
		t.Fatalf("explicit endpoint clobbered: got %q", got.Endpoint)
	}
	if got.APIKey != "explicit-key" {
		t.Fatalf("explicit api_key clobbered: got %q", got.APIKey)
	}
}

func TestInheritWhisperEndpoint_Ollama(t *testing.T) {
	cfg := &config.Config{
		Ollama: config.OllamaConfig{Host: "http://ollama.local:11434"},
	}
	in := transcribePkg.BackendConfig{Backend: "ollama"}
	got := inheritWhisperEndpoint(in, cfg)

	want := "http://ollama.local:11434/v1"
	if got.Endpoint != want {
		t.Fatalf("ollama endpoint: got %q want %q", got.Endpoint, want)
	}
	if got.APIKey != "" {
		t.Fatalf("ollama has no api key; got %q", got.APIKey)
	}
}

func TestInheritWhisperEndpoint_Ollama_TrimsTrailingSlash(t *testing.T) {
	cfg := &config.Config{
		Ollama: config.OllamaConfig{Host: "http://ollama.local:11434/"},
	}
	in := transcribePkg.BackendConfig{Backend: "ollama"}
	got := inheritWhisperEndpoint(in, cfg)

	if got.Endpoint != "http://ollama.local:11434/v1" {
		t.Fatalf("ollama endpoint should trim trailing slash before /v1: got %q", got.Endpoint)
	}
}

func TestInheritWhisperEndpoint_Whisper_NoOp(t *testing.T) {
	cfg := &config.Config{
		OpenWebUI: config.OpenWebUIConfig{URL: "https://owui.local:8080", APIKey: "x"},
	}
	in := transcribePkg.BackendConfig{
		Backend:  "whisper",
		VenvPath: ".venv",
		Model:    "base",
	}
	got := inheritWhisperEndpoint(in, cfg)

	if got.Endpoint != "" || got.APIKey != "" {
		t.Fatalf("whisper backend must not pull HTTP creds: got endpoint=%q api_key=%q", got.Endpoint, got.APIKey)
	}
}

func TestInheritWhisperEndpoint_OpenAI_NoLLMConfig(t *testing.T) {
	// There's no top-level OpenAI LLM config to inherit from. The
	// helper must leave endpoint/key alone so the operator's explicit
	// settings (or empty defaults that fail clearly at first call)
	// pass through unchanged.
	cfg := &config.Config{}
	in := transcribePkg.BackendConfig{
		Backend:  "openai",
		Endpoint: "https://api.openai.com/v1",
		APIKey:   "sk-explicit",
	}
	got := inheritWhisperEndpoint(in, cfg)

	if got.Endpoint != "https://api.openai.com/v1" || got.APIKey != "sk-explicit" {
		t.Fatalf("openai backend should pass through unchanged: %+v", got)
	}
}

func TestInheritWhisperEndpoint_BackendCaseInsensitive(t *testing.T) {
	cfg := &config.Config{
		OpenWebUI: config.OpenWebUIConfig{URL: "https://owui.local", APIKey: "k"},
	}
	in := transcribePkg.BackendConfig{Backend: "  OpenWebUI  "}
	got := inheritWhisperEndpoint(in, cfg)

	if got.Endpoint != "https://owui.local" {
		t.Fatalf("case-insensitive + whitespace trim failed: got %q", got.Endpoint)
	}
}
