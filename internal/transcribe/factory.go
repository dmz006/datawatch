// BL189 — backend factory: pick a transcribe implementation based
// on operator config. Defaults to the local Whisper venv (existing
// behavior); operators opt into the OpenAI-compatible HTTP path
// for OpenWebUI / standalone whisper-server / cloud OpenAI.

package transcribe

import (
	"fmt"
	"strings"
)

// BackendConfig is the minimum subset of WhisperConfig the factory
// needs. Repeated here so the factory doesn't pull in
// internal/config (avoids an import cycle).
type BackendConfig struct {
	Backend  string // "" / "whisper" | "openai" | "openai_compat"
	VenvPath string // for whisper backend
	Model    string
	Language string
	Endpoint string // for openai* backends
	APIKey   string // for openai* backends
}

// New constructs a Transcriber per the chosen backend. Empty
// Backend defaults to "whisper" so existing operator configs keep
// working without changes.
func NewFromConfig(cfg BackendConfig) (Transcriber, error) {
	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	switch backend {
	case "", "whisper", "local":
		venv := cfg.VenvPath
		if venv == "" {
			venv = ".venv"
		}
		model := cfg.Model
		if model == "" {
			model = "base"
		}
		return New(venv, model, cfg.Language)
	case "openai", "openai_compat", "openai-compat", "openwebui":
		return NewOpenAICompat(cfg.Endpoint, cfg.APIKey, cfg.Model, cfg.Language)
	default:
		return nil, fmt.Errorf("transcribe: unknown backend %q (want: whisper | openai | openai_compat)", cfg.Backend)
	}
}
