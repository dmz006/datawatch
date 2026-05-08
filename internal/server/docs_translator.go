// BL274 Sprint 4, v6.19.0 — LLM-translation fallback for docs_apply.
//
// Per Q4 design: critical howtos carry hand-authored exec_steps front-matter;
// non-curated howtos fall back to LLM translation that turns prose into a
// deterministic MCP-call sequence. Every translated step is marked
// provenance="llm_translated" so docsApplyPlan force-enables risk_gate
// (we don't trust the model to decide what's safe to run un-gated).
//
// Translator wraps the operator's existing Ollama/OpenWebUI backend
// (whichever is configured) — no new transport surface.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/docsindex"
)

// docsTranslator implements docsindex.LLMTranslator against the daemon's
// Ask backend (Ollama default, OpenWebUI fallback). Holds only the cfg
// pointer so it works equally from HTTPServer + Server callers.
type docsTranslator struct {
	cfg *config.Config
}

// NewDocsTranslator returns a translator backed by the operator's
// configured LLM. Returns nil if no backend is configured (caller should
// not attach in that case).
func NewDocsTranslator(cfg *config.Config) docsindex.LLMTranslator {
	if cfg == nil {
		return nil
	}
	if cfg.Ollama.Host == "" && cfg.OpenWebUI.URL == "" {
		return nil
	}
	return &docsTranslator{cfg: cfg}
}

const translatorSystemPrompt = `You are a docs-to-MCP-call translator for the datawatch operator-control-plane.

Your job: read a how-to written in prose and emit a JSON array of MCP-tool calls
that perform the how-to's recommended action sequence.

Output rules (STRICT):
- Output ONLY a single JSON array. No prose, no markdown, no code-fence.
- Each element is an object: {"tool": "<mcp_tool_name>", "args": {...}, "description": "<short>", "read_only": <bool>}.
- read_only=true for inspection/list/get/read calls; false for create/update/delete/run.
- Use {{params.X}} placeholders inside string args when the operator should supply the value.
- Use only tools the operator has access to (you may use any documented MCP tool name).
- Keep the sequence minimal — 1 to 6 steps.
- If the howto is purely informational and has no actions, emit []`

const translatorUserPromptTemplate = `Howto path: %s
Operator-supplied params: %s

Howto body:
---
%s
---

Emit the JSON array now.`

// Translate asks the configured LLM to produce a sequence of MCP-call steps
// for the given howto. Returns parsed steps with provenance="llm_translated".
func (t *docsTranslator) Translate(ctx context.Context, howtoPath, body string, params map[string]string, _ []docsindex.ToolDescriptor) ([]docsindex.ExecStep, error) {
	if t == nil || t.cfg == nil {
		return nil, fmt.Errorf("docs translator not initialized")
	}
	paramsJSON, _ := json.Marshal(params)
	prompt := translatorSystemPrompt + "\n\n" + fmt.Sprintf(translatorUserPromptTemplate, howtoPath, string(paramsJSON), body)

	req := AskRequest{
		Question: prompt,
		Backend:  "ollama",
	}
	if t.cfg.Ollama.Host == "" {
		req.Backend = "openwebui"
	}
	// askOllama / askOpenWebUI take *Server but only read .cfg.Ollama /
	// .cfg.OpenWebUI; build a minimal stub.
	stub := &Server{cfg: t.cfg}

	var (
		answer string
		err    error
	)
	switch req.Backend {
	case "ollama":
		answer, err = askOllama(stub, req)
	case "openwebui":
		answer, err = askOpenWebUI(stub, req)
	}
	if err != nil {
		return nil, fmt.Errorf("llm translation failed: %w", err)
	}

	// Extract the JSON array from the response — models sometimes wrap with
	// stray markdown or chatter despite the strict prompt.
	steps, perr := extractStepArray(answer)
	if perr != nil {
		return nil, fmt.Errorf("translator output parse: %w (raw: %s)", perr, truncateForLog(answer, 240))
	}
	for i := range steps {
		if steps[i].Provenance == "" {
			steps[i].Provenance = "llm_translated"
		}
	}
	return steps, nil
}

// extractStepArray finds the first balanced JSON array in s and unmarshals
// it. Tolerates pre/post chatter from the model.
func extractStepArray(s string) ([]docsindex.ExecStep, error) {
	s = strings.TrimSpace(s)
	// Strip common code-fence wrappers.
	for _, prefix := range []string{"```json", "```JSON", "```"} {
		s = strings.TrimPrefix(s, prefix)
	}
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array found")
	}
	js := s[start : end+1]
	var raw []map[string]any
	if err := json.Unmarshal([]byte(js), &raw); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	out := make([]docsindex.ExecStep, 0, len(raw))
	for _, r := range raw {
		step := docsindex.ExecStep{}
		if v, ok := r["tool"].(string); ok {
			step.Tool = v
		}
		if v, ok := r["description"].(string); ok {
			step.Description = v
		}
		if v, ok := r["read_only"].(bool); ok {
			step.ReadOnly = v
		}
		if v, ok := r["args"].(map[string]any); ok {
			step.Args = v
		}
		if step.Tool == "" {
			continue // skip malformed entries
		}
		out = append(out, step)
	}
	return out, nil
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
