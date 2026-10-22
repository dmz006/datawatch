// Package session classifies LLM backends by family: prompt-mode backends
// (named "<family>-prompt") are headless, one-run adapters that answer a
// single prompt and exit, while their interactive counterparts (claude-code,
// pi, goose, opencode, opencode-acp, aider, gemini, ollama, shell, ...) run
// as long-lived, interactive sessions.
package session

import "strings"

// IsPromptBackend reports whether backendName is a prompt-mode backend, i.e.
// its lower-cased, trimmed name ends in "-prompt" (pi-prompt, goose-prompt,
// opencode-prompt, ...). Interactive backends — claude-code, pi, opencode,
// opencode-acp, goose, aider, gemini, ollama, shell, and any other name not
// ending in the suffix — always return false.
func IsPromptBackend(backendName string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(backendName)), "-prompt")
}

// effectiveOneShot resolves the one-shot decision for a newly created
// session (BL474). Prompt backends are headless one-run adapters, so a
// session launched on one is one-shot by default; an explicitly requested
// one-shot always wins; and interactive backends stay interactive either
// way. Only the "-prompt" suffix ever adds one-shot — it is never a way to
// remove one. Decided only at session creation, never applied when loading
// or restoring stored sessions.
func effectiveOneShot(opt *StartOptions, backendName string) bool {
	if opt != nil && opt.OneShot {
		return true
	}
	return IsPromptBackend(backendName)
}
