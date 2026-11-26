package opencode

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/toolsettings"
)

// projectConfig is the subset of opencode.json that datawatch manages.
// OpenCode merges project-level config over the global ~/.config/opencode/opencode.jsonc.
type projectConfig struct {
	Model    string              `json:"model,omitempty"`
	LSP      map[string]lspEntry `json:"lsp,omitempty"`
	Provider map[string]any      `json:"provider,omitempty"`
	Agent    map[string]any      `json:"agent,omitempty"`
	// Permission (2026-10-15, corrected same night) — opencode V2's
	// config schema deprecates this singular key in favor of plural
	// "permissions", but the INSTALLED binary actually in production use
	// tonight is V1 (1.19.2 — see the CHANGELOG/memory note on why: V2
	// restructured its REST API incompatibly with this codebase's ACP
	// client and had to be reverted), and V1 hard-rejects the plural key
	// with ConfigInvalidError ("V2 permissions are not supported by
	// OpenCode V1"), breaking every opencode-acp session outright. Use
	// the singular V1-correct key unless/until V2's REST API is actually
	// supported by acpbackend.go. Deliberately NOT a deny-by-default
	// allow-list — see ProjectConfigOpts.DenyTools' doc comment for why.
	Permission map[string]string `json:"permission,omitempty"`
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
	// (e.g. "anthropic/claude-sonnet-5", "ollama/llama3").
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

	// ChunkTimeoutSec / HeaderTimeoutSec (B89) — written into the ollama
	// provider's options.chunkTimeout / options.headerTimeout (converted to
	// milliseconds). 0 leaves opencode's own default (300s each) in effect.
	// Only used when Model starts with "ollama/".
	ChunkTimeoutSec  int
	HeaderTimeoutSec int

	// BuildTemperature (2026-10-15, live reliability investigation) —
	// overrides the "build" agent's sampling temperature via
	// agent.build.temperature in opencode.json (schema:
	// https://opencode.ai/config.json, $defs/AgentConfig.temperature,
	// type number). 0 leaves opencode's/the model's own default in
	// effect. Candidate fix for small open models (confirmed live:
	// qwen3.8:27b) stating a one-line plan and then going idle at
	// "awaiting_input" without ever calling a tool, at Ollama's default
	// temperature=1.0 for this model — not yet proven, being tested live.
	BuildTemperature float64

	// DenyTools (2026-10-15, live-validated) — writes opencode.json's
	// current "permissions" field (NOT the deprecated singular
	// "permission"/"tools" fields) with "deny" for each named tool, every
	// other tool left completely untouched (opencode's own default
	// applies). Deliberately NOT a deny-by-default allow-list: a
	// hand-picked allow-list risks blocking a tool a specific task
	// genuinely needs (confirmed live: an early manual test's allow-list
	// missed "list" and "todowrite" from opencode's real tool set —
	// read/edit/glob/grep/list/bash/task/external_directory/todowrite/
	// question/webfetch/websearch/lsp/skill, per opencode's own config
	// schema). Callers should only name tools that are safe to deny for
	// ANY one-shot autonomous task regardless of content (e.g. "task" —
	// recursive sub-agent spawning — and "external_directory" — escaping
	// the stated scope boundary already in every task prompt), not an
	// attempt at full per-task tool scoping (that's decompose-emitted
	// per-task tool lists, a separate, not-yet-built design — see BL431's
	// doc, "Local-model decision assistant" section).
	DenyTools []string
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
	if opts.Model == "" && len(opts.LSPServers) == 0 && opts.OllamaURL == "" && opts.BuildTemperature == 0 && len(opts.DenyTools) == 0 {
		return nil
	}

	path := filepath.Join(projectDir, "opencode.json")

	// Only the keys datawatch manages are changed. Everything else in the
	// project's file is kept, and a file that cannot be read in full (a
	// comment included) is left as it is.
	real, doc, mode, err := toolsettings.ReadThroughLink(path)
	if err != nil {
		return fmt.Errorf("opencode config not written: %w", err)
	}
	if mode == 0 {
		mode = 0o644
	}
	object := func(parent map[string]any, key string) (map[string]any, error) {
		o, err := toolsettings.Object(parent, key)
		if err != nil {
			return nil, fmt.Errorf("opencode config not written: %s: %w", path, err)
		}
		return o, nil
	}

	if opts.Model != "" {
		doc["model"] = opts.Model
	}

	if opts.BuildTemperature != 0 {
		agent, err := object(doc, "agent")
		if err != nil {
			return err
		}
		build, err := object(agent, "build")
		if err != nil {
			return err
		}
		build["temperature"] = opts.BuildTemperature
	}

	if len(opts.DenyTools) > 0 {
		permission, err := object(doc, "permission")
		if err != nil {
			return err
		}
		for _, tool := range opts.DenyTools {
			permission[tool] = "deny"
		}
	}

	if len(opts.LSPServers) > 0 {
		lsp, err := object(doc, "lsp")
		if err != nil {
			return err
		}
		for name, srv := range opts.LSPServers {
			lsp[name] = lspEntry(srv)
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
		// Guard against scheme-less addresses (e.g. "host:11434" with no "http://")
		// which opencode rejects immediately as an unparseable URL.
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			baseURL = "http://" + baseURL
		}
		provider, err := object(doc, "provider")
		if err != nil {
			return err
		}
		providerOpts := map[string]any{
			"baseURL": strings.TrimSuffix(baseURL, "/") + "/v1",
		}
		// B89 — override opencode's default 300s chunk/header timeouts,
		// which are too short for long "thinking" pauses and cold-load
		// times on large locally-hosted models (see ProjectConfigOpts doc).
		if opts.ChunkTimeoutSec > 0 {
			providerOpts["chunkTimeout"] = opts.ChunkTimeoutSec * 1000
		}
		if opts.HeaderTimeoutSec > 0 {
			providerOpts["headerTimeout"] = opts.HeaderTimeoutSec * 1000
		}
		provider["ollama"] = map[string]any{
			"npm":     "@ai-sdk/openai-compatible",
			"name":    "Ollama",
			"options": providerOpts,
			"models": map[string]any{
				modelName: map[string]any{},
			},
		}
	}

	return toolsettings.Write(real, doc, mode)
}
