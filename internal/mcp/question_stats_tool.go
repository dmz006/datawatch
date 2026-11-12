package mcp

import (
	"context"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The question stats tool is read-only and proxies GET /api/questions/stats:
// the worker question marker's counters since daemon start and the open
// marker and blocking questions now.

func (s *Server) toolQuestionStats() mcpsdk.Tool {
	return mcpsdk.NewTool("question_stats",
		mcpsdk.WithDescription("Read the worker question marker's counters since daemon start: raised (marker lines stored as questions), duplicate (a repeat of a question still open), refused (lines that failed validation), parked (tasks parked on a blocking question) and resumed (parked tasks resumed after the decision), plus open_marker_questions and open_blocking_questions now and the two switches questions_enabled and worker_marker_enabled. Read-only; the same object as GET /api/questions/stats."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleQuestionStats(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/questions/stats", nil)
	if err != nil {
		return toolError("question_stats", err), nil
	}
	return textOK(string(out)), nil
}

// The question quality tool is read-only and proxies GET /api/questions/quality:
// the effective policy the daemon checks every raised question against.

func (s *Server) toolQuestionQuality() mcpsdk.Tool {
	return mcpsdk.NewTool("question_quality",
		mcpsdk.WithDescription("Read the question quality policy the daemon checks every raised question against: mode (off, warn or refuse), min_text_chars (the shortest question text accepted), tracker_id_pattern (the planning-registry id pattern a question must not contain) and queue_preview_lines (how many lines of a question the queue shows). Read it before question_raise to write a question that passes. Read-only; the same object as GET /api/questions/quality. While the autonomous.questions.enabled switch is off the daemon 503s and names the switch."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleQuestionQuality(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/questions/quality", nil)
	if err != nil {
		return toolError("question_quality", err), nil
	}
	return textOK(string(out)), nil
}
