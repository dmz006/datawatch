package opencode

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// ListBuiltinModels queries the OpenCode binary for its currently available
// models (free built-ins + any provider models the operator has logged into
// via `opencode providers login`). Returns a slice of model IDs, one per
// line of `opencode models` output. Returns nil (not an error) when the
// binary is missing or fails, so callers degrade gracefully to Ollama-only.
func ListBuiltinModels(binary string) []string {
	binary = resolveBinary(binary)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "models").Output()
	if err != nil {
		return nil
	}
	var models []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// Skip blank lines and ANSI/log noise (lines starting with '[').
		if line == "" || line[0] == '[' {
			continue
		}
		models = append(models, line)
	}
	return models
}

// ModelLabel generates a human-friendly display name from an OpenCode model ID.
//
//	"opencode/big-pickle"          → "Big Pickle (Free)"
//	"opencode/deepseek-v4-flash-free" → "Deepseek v4 Flash (Free)"
//	"opencode/nemotron-3-super-free"  → "Nemotron 3 Super (Free)"
//	"anthropic/claude-sonnet-4-6"     → "Claude Sonnet 4.6"
func ModelLabel(id string) string {
	parts := strings.SplitN(id, "/", 2)
	provider := ""
	name := id
	if len(parts) == 2 {
		provider = parts[0]
		name = parts[1]
	}

	// All opencode/* models are free built-ins; also flag explicit -free suffix.
	isFree := provider == "opencode" || strings.HasSuffix(name, "-free")
	name = strings.TrimSuffix(name, "-free")

	// Title-case dash-separated words; keep version tags (v1, v2…) lowercase.
	words := strings.Split(name, "-")
	for i, w := range words {
		if len(w) == 0 {
			continue
		}
		// Version tags like "v4", "v3" stay lowercase.
		if len(w) >= 2 && (w[0] == 'v' || w[0] == 'V') && isDigits(w[1:]) {
			words[i] = strings.ToLower(w[:1]) + w[1:]
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	label := strings.Join(words, " ")
	if isFree {
		label += " (Free)"
	}
	return label
}

// ProviderLabel returns a human-friendly group name for a provider prefix.
func ProviderLabel(provider string) string {
	switch provider {
	case "opencode":
		return "Free (Built-in)"
	case "ollama":
		return "Ollama (Local / Compute)"
	case "anthropic":
		return "Anthropic"
	case "openai":
		return "OpenAI"
	case "google":
		return "Google"
	default:
		if provider == "" {
			return "Other"
		}
		return strings.ToUpper(provider[:1]) + provider[1:]
	}
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
