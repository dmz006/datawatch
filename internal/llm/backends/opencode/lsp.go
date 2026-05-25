package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// projectConfig is the subset of opencode.json that datawatch manages.
// OpenCode merges project-level config over the global ~/.config/opencode/opencode.jsonc.
type projectConfig struct {
	Model    string              `json:"model,omitempty"`
	LSP      map[string]lspEntry `json:"lsp,omitempty"`
	Provider map[string]any      `json:"provider,omitempty"`
}

// lspEntry is one entry inside the "lsp" object of opencode.json.
// Fields match the OpenCode config schema (https://opencode.ai/config.json).
type lspEntry struct {
	Command    []string          `json:"command"`
	Extensions []string          `json:"extensions,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

// LSPServer is the public type callers pass to WriteProjectConfig.
type LSPServer struct {
	Command    []string
	Extensions []string
	Env        map[string]string
}

// ProjectConfigOpts groups all datawatch-managed opencode.json fields.
type ProjectConfigOpts struct {
	// Model sets the active model in provider/model format
	// (e.g. "anthropic/claude-sonnet-4-6", "ollama/llama3").
	// Empty leaves the global opencode.jsonc model in effect.
	Model string

	// LSPServers maps language name → server definition.
	// Only the named servers are written; the rest of the "lsp" object
	// is preserved from any prior operator-written opencode.json.
	LSPServers map[string]LSPServer

	// OllamaURL overrides the Ollama provider base URL so an Ollama model
	// can be routed to a specific compute node.
	// Set only when Model starts with "ollama/" and a non-local node is selected.
	OllamaURL string
}

// WriteProjectConfig writes <projectDir>/opencode.json with the provided
// opts. Idempotent: reads the existing file first and merges, so
// operator-added settings outside the managed keys are preserved.
//
// Called from the session pre-launch hook in cmd/datawatch/main.go for
// OpenCode and OpenCode-ACP backends. Cleaned up at session end via
// CleanupArtifacts (tooling.BackendArtifacts["opencode"]).
func WriteProjectConfig(projectDir string, opts ProjectConfigOpts) error {
	if projectDir == "" {
		return nil
	}
	if opts.Model == "" && len(opts.LSPServers) == 0 && opts.OllamaURL == "" {
		return nil
	}

	path := filepath.Join(projectDir, "opencode.json")

	existing := projectConfig{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing) //nolint:errcheck
	}

	if opts.Model != "" {
		existing.Model = opts.Model
	}

	if len(opts.LSPServers) > 0 {
		if existing.LSP == nil {
			existing.LSP = make(map[string]lspEntry)
		}
		for name, srv := range opts.LSPServers {
			existing.LSP[name] = lspEntry{
				Command:    srv.Command,
				Extensions: srv.Extensions,
				Env:        srv.Env,
			}
		}
	}

	if opts.OllamaURL != "" {
		if existing.Provider == nil {
			existing.Provider = make(map[string]any)
		}
		existing.Provider["ollama"] = map[string]any{
			"apiUrl": opts.OllamaURL,
		}
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
