// BL260 v6.11.0 — MCP tools for Council Mode (multi-agent debate).
// All proxy to REST.
//
//	council_personas    — list registered personas
//	council_run         — execute council on a proposal
//	council_list_runs   — list past runs
//	council_get_run     — fetch one run

package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolCouncilPersonas() mcpsdk.Tool {
	return mcpsdk.NewTool("council_personas",
		mcpsdk.WithDescription("BL260 — list registered Council personas (built-in 12 by default + operator-added)."),
	)
}

// BL296 — council_personas_get: fetch one persona by name.
func (s *Server) toolCouncilPersonasGet() mcpsdk.Tool {
	return mcpsdk.NewTool("council_personas_get",
		mcpsdk.WithDescription("BL296 — fetch a single Council persona by name, returning its role + system_prompt."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("persona name (e.g. security-skeptic, privacy, platform-engineer)")),
	)
}

// BL296 — council_personas_set: update an existing persona's system_prompt (and optionally role).
func (s *Server) toolCouncilPersonasSet() mcpsdk.Tool {
	return mcpsdk.NewTool("council_personas_set",
		mcpsdk.WithDescription("BL296 — update an existing Council persona's system_prompt (and optionally role). Persists to ~/.datawatch/council/personas/<name>.yaml."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("persona name to update")),
		mcpsdk.WithString("system_prompt", mcpsdk.Required(), mcpsdk.Description("new system prompt text")),
		mcpsdk.WithString("role", mcpsdk.Description("optional: new role title")),
	)
}

func (s *Server) toolCouncilRun() mcpsdk.Tool {
	return mcpsdk.NewTool("council_run",
		mcpsdk.WithDescription("BL260 — run a multi-persona Council debate on a proposal. Two modes: debate (3 rounds), quick (1 round). v6.11.0 ships the framework with stubbed LLM responses; real per-persona inference is a v6.11.x follow-up."),
		mcpsdk.WithString("proposal", mcpsdk.Required(), mcpsdk.Description("the question or design proposal to debate")),
		mcpsdk.WithString("personas", mcpsdk.Description("comma-separated persona names; empty = all registered")),
		mcpsdk.WithString("mode", mcpsdk.Description("debate (3 rounds) or quick (1 round); default quick")),
	)
}
func (s *Server) toolCouncilListRuns() mcpsdk.Tool {
	return mcpsdk.NewTool("council_list_runs",
		mcpsdk.WithDescription("BL260 — list past Council runs (most recent first)."),
		mcpsdk.WithString("limit", mcpsdk.Description("max runs to return")),
	)
}
func (s *Server) toolCouncilGetRun() mcpsdk.Tool {
	return mcpsdk.NewTool("council_get_run",
		mcpsdk.WithDescription("BL260 — fetch one persisted Council run by id."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("run id")),
	)
}

func (s *Server) handleCouncilPersonasMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/council/personas", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleCouncilPersonasGetMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := mustString(req, "name")
	out, err := s.proxyGet("/api/council/personas/"+name, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleCouncilPersonasSetMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := mustString(req, "name")
	body := map[string]any{
		"system_prompt": mustString(req, "system_prompt"),
	}
	if role := optString(req, "role"); role != "" {
		body["role"] = role
	}
	out, err := s.proxyJSON("PUT", "/api/council/personas/"+name, body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleCouncilRunMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"proposal": mustString(req, "proposal"),
		"mode":     optString(req, "mode"),
	}
	if names := splitCSV(optString(req, "personas")); len(names) > 0 {
		body["personas"] = names
	}
	out, err := s.proxyJSON("POST", "/api/council/run", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleCouncilListRunsMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	path := "/api/council/runs"
	if v := optString(req, "limit"); v != "" {
		path = path + "?limit=" + v
	}
	out, err := s.proxyGet(path, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleCouncilGetRunMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "id")
	out, err := s.proxyGet("/api/council/runs/"+id, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// v7.0.0 S3 — Council run cancellation.

func (s *Server) toolCouncilRunCancel() mcpsdk.Tool {
	return mcpsdk.NewTool("council_run_cancel",
		mcpsdk.WithDescription("v7.0.0 S3 — cancel an in-flight Council run; ctx propagates to in-flight LLM calls."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("run id")),
	)
}

func (s *Server) handleCouncilRunCancelMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "id")
	out, err := s.proxyJSON("POST", "/api/council/runs/"+id+"/cancel", map[string]any{})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// BL297 (v6.22.3) — Council "Add Persona" wizard MCP tools.
//
// MCP host scenarios are agentic; default to the one-shot path (no
// chat-style draft state). The full draft state-machine is also
// surfaced for hosts that want to drive a multi-step interview.

func (s *Server) toolCouncilPersonaOneShot() mcpsdk.Tool {
	return mcpsdk.NewTool("council_persona_oneshot",
		mcpsdk.WithDescription("BL297 — draft a Council persona YAML in one LLM call from operator-supplied interview answers. Does NOT register the persona; pair with council_personas POST or council_persona_save."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("persona name (kebab-case after drafting)")),
		mcpsdk.WithString("role", mcpsdk.Description("title or short role")),
		mcpsdk.WithString("focus", mcpsdk.Description("focus area / domain expertise")),
		mcpsdk.WithString("stance", mcpsdk.Description("stance: challenger / advocate / skeptic / etc.")),
		mcpsdk.WithString("tone", mcpsdk.Description("voice / tone")),
		mcpsdk.WithString("anti_patterns", mcpsdk.Description("what to push back on")),
		mcpsdk.WithString("examples", mcpsdk.Description("kinds of proposals to engage with")),
		mcpsdk.WithString("backend", mcpsdk.Description("ollama | openwebui (default: server policy)")),
	)
}

func (s *Server) toolCouncilPersonaDraftStart() mcpsdk.Tool {
	return mcpsdk.NewTool("council_persona_draft_start",
		mcpsdk.WithDescription("BL297 — begin a chat-style persona-wizard draft. Returns the draft ID + first interview question."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("persona name")),
		mcpsdk.WithString("role", mcpsdk.Description("optional title/role")),
		mcpsdk.WithString("backend", mcpsdk.Description("ollama | openwebui (default: server policy)")),
	)
}

func (s *Server) toolCouncilPersonaDraftAnswer() mcpsdk.Tool {
	return mcpsdk.NewTool("council_persona_draft_answer",
		mcpsdk.WithDescription("BL297 — patch one or more answers onto an in-progress persona draft."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("draft id")),
		mcpsdk.WithString("focus", mcpsdk.Description("focus area answer")),
		mcpsdk.WithString("stance", mcpsdk.Description("stance answer")),
		mcpsdk.WithString("tone", mcpsdk.Description("tone answer")),
		mcpsdk.WithString("anti_patterns", mcpsdk.Description("anti-patterns answer")),
		mcpsdk.WithString("examples", mcpsdk.Description("examples answer")),
	)
}

func (s *Server) toolCouncilPersonaDraftRefine() mcpsdk.Tool {
	return mcpsdk.NewTool("council_persona_draft_refine",
		mcpsdk.WithDescription("BL297 — call the LLM to (re)draft the persona YAML from the current answers. Optional instruction tunes the output."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("draft id")),
		mcpsdk.WithString("instruction", mcpsdk.Description("e.g. 'make it more skeptical' / 'shorter'")),
	)
}

func (s *Server) toolCouncilPersonaDraftSave() mcpsdk.Tool {
	return mcpsdk.NewTool("council_persona_draft_save",
		mcpsdk.WithDescription("BL297 — register the drafted persona with Council (after refine)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("draft id")),
	)
}

func (s *Server) toolCouncilPersonaDraftList() mcpsdk.Tool {
	return mcpsdk.NewTool("council_persona_draft_list",
		mcpsdk.WithDescription("BL297 — list every persona-wizard draft (in-progress + completed + abandoned)."),
	)
}

func (s *Server) toolCouncilPersonaDraftPurge() mcpsdk.Tool {
	return mcpsdk.NewTool("council_persona_draft_purge",
		mcpsdk.WithDescription("BL297 — delete ALL persona-wizard drafts (operator-controlled cleanup, ignores retention window)."),
	)
}

func (s *Server) handleCouncilPersonaOneShotMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"name":          mustString(req, "name"),
		"role":          optString(req, "role"),
		"focus":         optString(req, "focus"),
		"stance":        optString(req, "stance"),
		"tone":          optString(req, "tone"),
		"anti_patterns": optString(req, "anti_patterns"),
		"examples":      optString(req, "examples"),
		"backend":       optString(req, "backend"),
	}
	out, err := s.proxyJSON("POST", "/api/council/personas/oneshot", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleCouncilPersonaDraftStartMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"name":    mustString(req, "name"),
		"role":    optString(req, "role"),
		"backend": optString(req, "backend"),
	}
	out, err := s.proxyJSON("POST", "/api/council/personas/draft", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleCouncilPersonaDraftAnswerMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "id")
	body := map[string]any{}
	for _, k := range []string{"focus", "stance", "tone", "anti_patterns", "examples"} {
		if v := optString(req, k); v != "" {
			body[k] = v
		}
	}
	out, err := s.proxyJSON("PATCH", "/api/council/personas/draft/"+id, body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleCouncilPersonaDraftRefineMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "id")
	body := map[string]any{"instruction": optString(req, "instruction")}
	out, err := s.proxyJSON("POST", "/api/council/personas/draft/"+id+"/refine", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleCouncilPersonaDraftSaveMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "id")
	out, err := s.proxyJSON("POST", "/api/council/personas/draft/"+id+"/save", map[string]any{})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleCouncilPersonaDraftListMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/council/personas/drafts", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleCouncilPersonaDraftPurgeMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON("DELETE", "/api/council/personas/drafts", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// BL297 v6.22.4 — Council subsystem runtime config knobs.

func (s *Server) toolCouncilConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("council_config_get",
		mcpsdk.WithDescription("BL297 — read Council subsystem runtime config (currently: draft_retention_days for persona-wizard GC)."),
	)
}

func (s *Server) toolCouncilConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("council_config_set",
		mcpsdk.WithDescription("BL297/BL295 — update Council subsystem runtime config. Persists to cfg.yaml; live ticker re-reads on next sweep."),
		mcpsdk.WithString("draft_retention_days", mcpsdk.Description("persona-wizard draft GC retention in days (>=0; 0 disables auto-GC)")),
		mcpsdk.WithString("llm_ref", mcpsdk.Description("LLM registry entry name for Council debates (e.g. 'ollama', 'openwebui')")),
		mcpsdk.WithString("max_parallel", mcpsdk.Description("per-round persona concurrency cap (>=0; 0=serial)")),
		mcpsdk.WithString("comm_firehose", mcpsdk.Description("push every persona response to comm channels: true or false")),
	)
}

func (s *Server) handleCouncilConfigGetMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/council/config", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleCouncilConfigSetMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{}
	if v := optString(req, "draft_retention_days"); v != "" {
		body["draft_retention_days"] = v
	}
	if v := optString(req, "llm_ref"); v != "" {
		body["llm_ref"] = v
	}
	if v := optString(req, "max_parallel"); v != "" {
		body["max_parallel"] = v
	}
	if v := optString(req, "comm_firehose"); v != "" {
		body["comm_firehose"] = v == "true" || v == "1" || v == "yes"
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("at least one config key required (draft_retention_days, llm_ref, max_parallel, comm_firehose)")
	}
	out, err := s.proxyJSON("PATCH", "/api/council/config", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
