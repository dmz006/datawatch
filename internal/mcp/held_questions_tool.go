package mcp

import (
	"context"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The held questions tool is read-only and proxies
// GET /api/autonomous/held-questions: the held-question mode in effect
// (off, shadow or on), the held state ids it covers, the held questions
// raised in "on" mode and the shadow entries logged in "shadow" mode.
// Acting on a hold is a worker question answer (question_answer), not
// this tool.

func (s *Server) toolHeldQuestions() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_held_questions",
		mcpsdk.WithDescription("Read the held-question report: the mode in effect (off, shadow or on) and the held states it covers, plus the held questions raised now (in on mode, each with its held state, the owning Automaton and title, the reason it is held, the question's own text, the recommended option and the forced default — answer it with question_answer) and the shadow entries the daemon logged instead of raising (in shadow mode, each with the owning Automaton, the held state, the recommended option and the time). Optional: prd_id (one Automaton only), state (one of the five held state ids). Read-only; the same object as GET /api/autonomous/held-questions."),
		mcpsdk.WithString("prd_id",
			mcpsdk.Description("Optional — only that Automaton's held questions and shadow entries.")),
		mcpsdk.WithString("state",
			mcpsdk.Description("Optional — only one of the five held state ids: \"blocked\", \"review_held\", \"revision_requested\", \"awaiting_decision\" or \"pending_insert\" (the daemon rejects anything else).")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// HeldQuestionsToolNames lists the held-question tool, for parity tests.
func HeldQuestionsToolNames() []string {
	return []string{"autonomous_held_questions"}
}

func (s *Server) handleHeldQuestions(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := req.GetString("prd_id", ""); v != "" {
		q.Set("prd_id", v)
	}
	if v := req.GetString("state", ""); v != "" {
		q.Set("state", v)
	}
	out, err := s.proxyGet("/api/autonomous/held-questions", q)
	if err != nil {
		return toolError("autonomous_held_questions", err), nil
	}
	return textOK(string(out)), nil
}
