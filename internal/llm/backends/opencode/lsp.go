package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	// can be routed to a specific compute node. Empty defaults to
	// http://localhost:11434 (matches internal/llm/backends/ollama.ListModels).
	// Only used when Model starts with "ollama/".
	OllamaURL string
}

const defaultOllamaURL = "http://localhost:11434"

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
			existing.LSP[name] = lspEntry(srv)
		}
	}

	// OpenCode has no built-in Ollama provider — it only recognizes
	// providers defined under "provider" using its generic openai-compatible
	// adapter (npm + options.baseURL + an explicit models map). A bare
	// {"apiUrl": ...} block (the old shape here) is silently ignored:
	// `opencode models` never lists the model and the session falls back
	// to the global default. Verified against opencode 1.18.25.
	if modelName, ok := strings.CutPrefix(opts.Model, "ollama/"); ok {
		baseURL := opts.OllamaURL
		if baseURL == "" {
			baseURL = defaultOllamaURL
		}
		if existing.Provider == nil {
			existing.Provider = make(map[string]any)
		}
		existing.Provider["ollama"] = map[string]any{
			"npm":  "@ai-sdk/openai-compatible",
			"name": "Ollama",
			"options": map[string]any{
				"baseURL": strings.TrimSuffix(baseURL, "/") + "/v1",
			},
			"models": map[string]any{
				modelName: map[string]any{},
			},
		}
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
