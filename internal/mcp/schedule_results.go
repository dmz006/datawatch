// MCP tools for the scheduled-job result panel (GH#204): parity with the
// REST endpoints POST /api/sessions/{session_id}/result and
// GET /api/schedule-results[/{schedule_name}].
//
// These handlers intentionally reuse the same building blocks as the REST
// handlers in internal/server/session_result_endpoint.go rather than
// duplicating any logic:
//
//   - session.ValidateResultPayload (internal/session/schedule_result_store.go)
//     — the exact same payload contract and typed validation errors;
//   - session.ScheduleResultStore.Add — the same per-schedule history
//     store, limit handling, atomic persistence, and truncation;
//   - dwserver.SetSessionResult — the same session status-board result
//     slot the REST endpoint writes (internal/server/hook_events.go).
//
// So a result posted over MCP is byte-for-byte what a REST post produces:
// same stored entry, same board slot, same validation rejections.

package mcp

import (
	"context"
	"encoding/json"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	dwserver "github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

// --------------------------------------------------------------------------
// post_result
// --------------------------------------------------------------------------

func (s *Server) toolPostResult() mcpsdk.Tool {
	return mcpsdk.NewTool("post_result",
		mcpsdk.WithDescription("Post the structured result for a session (the scheduled-job result panel). Same payload contract as the REST endpoint POST /api/sessions/{session_id}/result: a required title, an ok/warn/error status, and optional summary, metrics, tables, and markdown report. When the session belongs to a scheduled job the result is also appended to that schedule's retained history; it is always reflected in the session's status-board result slot."),
		mcpsdk.WithString("session_id",
			mcpsdk.Required(),
			mcpsdk.Description("Session posting the result — normally the calling session's own ID (its full hostname-id or short ID).")),
		mcpsdk.WithString("title",
			mcpsdk.Required(),
			mcpsdk.Description("One-line headline, e.g. 'Build succeeded — 847 tests passed'. Required.")),
		mcpsdk.WithString("status",
			mcpsdk.Description("Result status: ok, warn, or error. Defaults to ok.")),
		mcpsdk.WithString("summary",
			mcpsdk.Description("Short human-readable summary.")),
		mcpsdk.WithString("metrics",
			mcpsdk.Description(`JSON object of free-form key/value metrics. Example: {"score": 92.4, "builds": 12}`)),
		mcpsdk.WithString("tables",
			mcpsdk.Description(`JSON array of up to 20 tables. Example: [{"columns": ["job","result"], "rows": [["unit","pass"]]}]`)),
		mcpsdk.WithString("markdown",
			mcpsdk.Description("Full markdown report (long-form detail).")),
	)
}

func (s *Server) handlePostResult(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	sessID := strings.TrimSpace(req.GetString("session_id", ""))
	if sessID == "" {
		return toolErrorf("post_result", "session_id is required"), nil
	}

	// Build the payload from arguments — the same ResultPayload struct and
	// the same ValidateResultPayload call the REST handler makes on its
	// request body, so error messages and rejections are identical.
	payload := &session.ResultPayload{
		Title:    req.GetString("title", ""),
		Status:   req.GetString("status", ""),
		Summary:  req.GetString("summary", ""),
		Markdown: req.GetString("markdown", ""),
	}
	if raw := strings.TrimSpace(req.GetString("metrics", "")); raw != "" {
		metrics := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &metrics); err != nil {
			return toolErrorf("post_result", "metrics must be a JSON object: %v", err), nil
		}
		payload.Metrics = metrics
	}
	if raw := strings.TrimSpace(req.GetString("tables", "")); raw != "" {
		tables := []session.ResultTable{}
		if err := json.Unmarshal([]byte(raw), &tables); err != nil {
			return toolErrorf("post_result", "tables must be a JSON array of {columns, rows}: %v", err), nil
		}
		payload.Tables = tables
	}

	if err := session.ValidateResultPayload(payload); err != nil {
		return toolErrorf("post_result", "%v", err), nil
	}

	// Resolve the session the same way the REST handler resolves the path
	// session. resultSessionLookup is an injectable seam for tests; the
	// default path is the daemon's session manager.
	var sess *session.Session
	lookup := s.resultSessionLookup
	if lookup != nil {
		sess, _ = lookup(sessID)
	} else if s.manager != nil {
		sess, _ = s.manager.GetSession(sessID)
	} else {
		return toolErrorf("post_result", "Session manager unavailable"), nil
	}
	if sess == nil {
		return toolErrorf("post_result", "Session not found: %s", sessID), nil
	}

	boardID := sess.FullID
	if boardID == "" {
		boardID = sess.ID
	}

	resp := map[string]any{
		"ok":            true,
		"session_id":    sess.ID,
		"full_id":       sess.FullID,
		"schedule_name": sess.ScheduleName,
	}
	if sess.ScheduleName != "" {
		if s.scheduleResultStore == nil {
			return toolErrorf("post_result", "schedule result store not available on this server"), nil
		}
		entry := &session.ScheduleResultEntry{
			Payload:      *payload,
			SessionID:    boardID,
			ScheduleName: sess.ScheduleName,
		}
		if err := s.scheduleResultStore.Add(entry, s.scheduleResultsLimit()); err != nil {
			return toolErrorf("post_result", "failed to add schedule result: %v", err), nil
		}
		resp["stored"] = true
	} else {
		// Same as the REST endpoint: a session without a schedule still
		// gets its status-board result slot updated; nothing is persisted
		// to a per-schedule history.
		resp["stored"] = false
	}
	resp["count"] = s.scheduleResultsCount()

	// Same board slot the REST endpoint writes.
	dwserver.SetSessionResult(boardID, payload)

	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return toolErrorf("post_result", "%v", err), nil
	}
	return mcpsdk.NewToolResultText(string(b)), nil
}

// scheduleResultsCount mirrors the REST server's count helper, safe when
// the store is not attached.
func (s *Server) scheduleResultsCount() int {
	if s.scheduleResultStore == nil {
		return 0
	}
	return s.scheduleResultStore.Count()
}

// scheduleResultsLimit is the per-schedule retained-entry count taken from
// results.schedule_history_limit when wired in, otherwise the session
// package default (same defaulting as the REST server).
func (s *Server) scheduleResultsLimit() int {
	if s.resultHistoryLimit > 0 {
		return s.resultHistoryLimit
	}
	return session.ScheduleResultDefaultLimit
}

// --------------------------------------------------------------------------
// schedule_results_list
// --------------------------------------------------------------------------

func (s *Server) toolScheduleResultsList() mcpsdk.Tool {
	return mcpsdk.NewTool("schedule_results_list",
		mcpsdk.WithDescription("List results posted by scheduled jobs (the scheduled-job result panel). Without schedule_name: one summary per schedule — the latest posted result, the retained entry count, and the recent status sequence for a sparkline. With schedule_name: the full retained history for just that schedule. Same data as GET /api/schedule-results and GET /api/schedule-results/{schedule_name}."),
		mcpsdk.WithString("schedule_name",
			mcpsdk.Description("Optional. When set, return the full retained history for just this schedule.")),
	)
}

func (s *Server) handleScheduleResultsList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.scheduleResultStore == nil {
		return toolErrorf("schedule_results_list", "schedule result store not available on this server"), nil
	}

	name := strings.TrimSpace(req.GetString("schedule_name", ""))
	if name != "" {
		hist := s.scheduleResultStore.History(name)
		if len(hist) == 0 {
			return mcpsdk.NewToolResultText("No results recorded for schedule: " + name), nil
		}
		b, err := json.MarshalIndent(hist, "", " ")
		if err != nil {
			return toolErrorf("schedule_results_list", "%v", err), nil
		}
		return mcpsdk.NewToolResultText(string(b)), nil
	}

	// Grouped view: one summary per schedule, same shape the REST
	// GET /api/schedule-results returns.
	summaries := s.scheduleResultStore.List()
	if len(summaries) == 0 {
		return mcpsdk.NewToolResultText("No schedule results recorded."), nil
	}

	const recentStatusLimit = 50
	out := make([]map[string]any, 0, len(summaries))
	for _, sum := range summaries {
		hist := s.scheduleResultStore.History(sum.ScheduleName)
		statuses := make([]string, 0, len(hist))
		for _, e := range hist {
			statuses = append(statuses, e.Payload.Status)
		}
		if len(statuses) > recentStatusLimit {
			statuses = statuses[len(statuses)-recentStatusLimit:]
		}
		out = append(out, map[string]any{
			"schedule_name":   sum.ScheduleName,
			"latest":          sum.Latest,
			"count":           sum.Count,
			"recent_statuses": statuses,
		})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return toolErrorf("schedule_results_list", "%v", err), nil
	}
	return mcpsdk.NewToolResultText(string(b)), nil
}
