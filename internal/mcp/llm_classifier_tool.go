// llm_classifier_tool.go — the classification judge's one MCP tool. There is no
// existing LLM or Automaton tool that reads probe or calibration status, lists
// the publisher allowlist, adds to it under explicit confirmation or shows a
// verdict record, so this single tool covers them all by action. It proxies the
// loopback REST surface:
//
//	status     → GET  /api/llm/classifier
//	publishers → GET  /api/llm/classifier/publishers
//	add_publisher → POST /api/llm/classifier/publishers
//	verdicts   → GET  /api/llm/classifier/verdicts
//
// The verification_mode setting itself rides the existing llm_add and
// llm_update tools. A low-confidence block is decided with the existing
// autonomous_prd_resolve_task_review tool.

package mcp

import (
	"context"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolLLMClassifier() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_classifier",
		mcpsdk.WithDescription("The classification judge's status and records. action=status returns the verification mode of every LLM, the capability probe status per node, model and path, the calibration status per fingerprint (threshold, test metrics, age) and the publisher allowlist. action=publishers lists the allowlist of model publishers a classify-mode judge may come from. action=add_publisher adds one and needs confirm=true: it widens which models may judge, so it is refused without the explicit confirmation and every addition is audited. action=verdicts returns an Automaton's verdict records (mode, score vector, route, outcome, category); low_confidence=true keeps only the blocks the classifier reached for low confidence, which are decided with autonomous_prd_resolve_task_review. Set verification_mode with llm_add or llm_update."),
		mcpsdk.WithString("action", mcpsdk.Required(), mcpsdk.Description("status | publishers | add_publisher | verdicts")),
		mcpsdk.WithString("publisher", mcpsdk.Description("add_publisher only: the publisher name to allow")),
		mcpsdk.WithBoolean("confirm", mcpsdk.Description("add_publisher only: must be true; the operator's explicit confirmation")),
		mcpsdk.WithString("automaton_id", mcpsdk.Description("verdicts only: Automaton ID")),
		mcpsdk.WithString("task_id", mcpsdk.Description("verdicts only: narrow to one task")),
		mcpsdk.WithBoolean("low_confidence", mcpsdk.Description("verdicts only: keep only low-confidence blocks")),
	)
}

func (s *Server) handleLLMClassifier(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	var (
		out []byte
		err error
	)
	switch action := req.GetString("action", ""); action {
	case "status":
		out, err = s.proxyGet("/api/llm/classifier", nil)
	case "publishers":
		out, err = s.proxyGet("/api/llm/classifier/publishers", nil)
	case "add_publisher":
		out, err = s.proxyJSON(http.MethodPost, "/api/llm/classifier/publishers", map[string]any{
			"publisher": req.GetString("publisher", ""),
			"confirm":   req.GetBool("confirm", false),
			"actor":     "mcp",
		})
	case "verdicts":
		q := url.Values{}
		q.Set("automaton", req.GetString("automaton_id", ""))
		if t := req.GetString("task_id", ""); t != "" {
			q.Set("task", t)
		}
		if req.GetBool("low_confidence", false) {
			q.Set("low_confidence", "true")
		}
		out, err = s.proxyGet("/api/llm/classifier/verdicts", q)
	default:
		return toolError("llm_classifier", errBadClassifierAction(action)), nil
	}
	if err != nil {
		return toolError("llm_classifier", err), nil
	}
	return textOK(string(out)), nil
}

type errBadClassifierAction string

func (e errBadClassifierAction) Error() string {
	return "action must be status, publishers, add_publisher or verdicts (got \"" + string(e) + "\")"
}
