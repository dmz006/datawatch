package platformcontext

// DeliveryMode names how a backend family receives the platform context.
type DeliveryMode string

const (
	// DeliveryRulesFile: the backend reads the guardrails file itself.
	DeliveryRulesFile DeliveryMode = "rules_file"
	// DeliveryFirstPrompt: the context is placed in the session's first prompt.
	DeliveryFirstPrompt DeliveryMode = "first_prompt"
	// DeliveryRulesFileAndFirstPrompt: both channels carry the context.
	DeliveryRulesFileAndFirstPrompt DeliveryMode = "rules_file_and_first_prompt"
)

// DeliveryModes returns the delivery mode of every backend family: each
// directory under internal/llm/backends, plus the claude-code and pi-prompt
// session families (the BackendFamily values in internal/session).
//
// A backend is marked rules_file only when its code or documentation confirms
// it reads AGENT.md. The repository's backend code and docs confirm no such
// read for any backend other than claude-code, so every other entry is
// first_prompt, which does not depend on the backend's file handling.
func DeliveryModes() map[string]DeliveryMode {
	return map[string]DeliveryMode{
		// claude-code reads CLAUDE.md natively; it also receives the MCP
		// instructions and the SessionStart hook.
		"claude-code": DeliveryRulesFile,
		// pi-prompt: pi sessions are driven by a prompt; no AGENT.md read is confirmed.
		"pi-prompt": DeliveryFirstPrompt,
		// aider reads files named with --read or its config; AGENT.md is not
		// among them in the backend code or docs.
		"aider": DeliveryFirstPrompt,
		// gemini reads GEMINI.md, not AGENT.md; nothing here confirms otherwise.
		"gemini": DeliveryFirstPrompt,
		// goose reads .goosehints; no AGENT.md read is confirmed.
		"goose": DeliveryFirstPrompt,
		// ollama is a chat API with no file access: the prompt is the only channel.
		"ollama": DeliveryFirstPrompt,
		// opencode's own convention is AGENTS.md; an AGENT.md read is not confirmed.
		"opencode": DeliveryFirstPrompt,
		// openwebui is a chat API with no file access: the prompt is the only channel.
		"openwebui": DeliveryFirstPrompt,
		// pi: no AGENT.md read is confirmed in the backend code or docs.
		"pi": DeliveryFirstPrompt,
		// shell runs commands, not an LLM; the prompt is the only channel.
		"shell": DeliveryFirstPrompt,
	}
}
