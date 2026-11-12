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
// directory under internal/llm/backends, plus every session family name used
// as a Session.BackendFamily in internal/session (claude-code, pi-prompt,
// opencode-acp, opencode-prompt, goose-prompt, subprocess).
//
// A backend is marked rules_file only when it is confirmed to read the file
// datawatch writes. datawatch writes CLAUDE.local.md for claude-code's platform
// context and AGENT.md for every other backend (internal/session/tracker.go), but nothing in this
// repository confirms that any non-claude-code backend reads AGENT.md: the
// backend code never opens it, and docs/session-tracking.md only says
// datawatch writes it. This task could not read the backends' own upstream
// documentation, so those entries rest on the repo evidence alone and fall
// back to first_prompt, which does not depend on the backend's file handling.
func DeliveryModes() map[string]DeliveryMode {
	return map[string]DeliveryMode{
		// claude-code reads the untracked CLAUDE.local.md that datawatch
		// writes into the project directory and excludes from git; it also
		// receives the MCP instructions and the SessionStart hook.
		"claude-code": DeliveryRulesFile,
		// pi-prompt: pi sessions are driven by a prompt; no AGENT.md read is confirmed.
		"pi-prompt": DeliveryFirstPrompt,
		// aider: no AGENT.md read is confirmed in the backend code or repo docs.
		"aider": DeliveryFirstPrompt,
		// gemini: no AGENT.md read is confirmed in the backend code or repo docs.
		"gemini": DeliveryFirstPrompt,
		// goose: no AGENT.md read is confirmed in the backend code or repo docs.
		"goose": DeliveryFirstPrompt,
		// goose-prompt: session family of the goose backend; same reason as goose.
		"goose-prompt": DeliveryFirstPrompt,
		// ollama is a chat API with no file access: the prompt is the only channel.
		"ollama": DeliveryFirstPrompt,
		// opencode: no AGENT.md read is confirmed in the backend code or repo docs.
		"opencode": DeliveryFirstPrompt,
		// opencode-acp: ACP session family of opencode; same reason as opencode.
		"opencode-acp": DeliveryFirstPrompt,
		// opencode-prompt: prompt-driven session family of opencode; same reason.
		"opencode-prompt": DeliveryFirstPrompt,
		// openwebui is a chat API with no file access: the prompt is the only channel.
		"openwebui": DeliveryFirstPrompt,
		// pi: no AGENT.md read is confirmed in the backend code or repo docs.
		"pi": DeliveryFirstPrompt,
		// shell runs commands, not an LLM; the prompt is the only channel.
		"shell": DeliveryFirstPrompt,
		// subprocess sessions run a command via bash -c with no LLM to read a file.
		"subprocess": DeliveryFirstPrompt,
	}
}
