package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestPiDefaultModelReadsEachBackendsConfiguredModel pins that the lookup wired
// into the autonomous manager reports the model each pi backend launches on,
// reads the config on every call, and reports nothing for other backends.
func TestPiDefaultModelReadsEachBackendsConfiguredModel(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pi.Model = "ollama/interactive-model"
	cfg.PiPrompt.Model = "ollama/prompt-model"
	lookup := piDefaultModel(cfg)

	for backend, want := range map[string]string{
		"pi":          "ollama/interactive-model",
		"pi-prompt":   "ollama/prompt-model",
		"claude-code": "",
		"opencode":    "",
	} {
		if got := lookup(backend); got != want {
			t.Errorf("piDefaultModel(%q) = %q, want %q", backend, got, want)
		}
	}

	cfg.PiPrompt.Model = "ollama/changed"
	if got := lookup("pi-prompt"); got != "ollama/changed" {
		t.Errorf("after a config change piDefaultModel(pi-prompt) = %q, want the new value", got)
	}
}
