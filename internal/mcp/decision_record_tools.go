// decision_record_tools.go — MCP tools that proxy the decision-record REST
// routes of the daemon (internal/server/decision_records.go). The record is
// the source and the queue question is built from it: creating, updating or
// withdrawing the record goes to the daemon, which re-derives the question
// and answers it back on the record.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// decisionRecordsBase is the REST collection the tools proxy.
const decisionRecordsBase = "/api/autonomous/decision-records"

// ----- autonomous_decision_record_list ------------------------------------

func (s *Server) toolDecisionRecordList() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_decision_record_list",
		mcpsdk.WithDescription("List decision records, most recently updated first, together with the daemon's folder-watch status. A record holds the label, the title, the situation, what is being decided, the options, the author's recommendation, the default option and the links, plus the bookkeeping the store owns: state (open, answered, withdrawn), the question_id, the answer and the doc change that came with it. The record is the source — the daemon builds the queue question from it — so listing it never changes anything. Read-only: it never creates, updates or withdraws a record. Returns the same object as GET /api/autonomous/decision-records."),
		mcpsdk.WithString("state", mcpsdk.Description("Only records in this state: open, answered or withdrawn (anything else is refused by the daemon)")),
		mcpsdk.WithString("prd_id", mcpsdk.Description("Only the records linked to this Automaton")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("At most this many records; default 100, maximum 1000")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleDecisionRecordList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := strings.TrimSpace(req.GetString("state", "")); v != "" {
		q.Set("state", v)
	}
	if v := strings.TrimSpace(req.GetString("prd_id", "")); v != "" {
		q.Set("prd_id", v)
	}
	if n, ok := getNumberArg(req, "limit"); ok {
		if n < 1 || n != float64(int(n)) {
			return toolErrorf("autonomous_decision_record_list", "limit must be a positive whole number"), nil
		}
		q.Set("limit", strconv.Itoa(int(n)))
	}
	out, err := s.proxyGet(decisionRecordsBase, q)
	if err != nil {
		return toolError("autonomous_decision_record_list", err), nil
	}
	return textOK(string(out)), nil
}

// ----- autonomous_decision_record_get -------------------------------------

func (s *Server) toolDecisionRecordGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_decision_record_get",
		mcpsdk.WithDescription("Read one decision record by id: the label, the title, the situation, what is being decided, the options, the author's recommendation, the default option, the links, and the bookkeeping the store owns (state, question_id, answer, doc change). The record is the source — the daemon builds the queue question from it — so reading it never changes anything. Read-only. Returns the same object as GET /api/autonomous/decision-records/{id}."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("The decision record id")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleDecisionRecordGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("autonomous_decision_record_get", "id is required"), nil
	}
	out, err := s.proxyGet(decisionRecordsBase+"/"+url.PathEscape(id), url.Values{})
	if err != nil {
		return toolError("autonomous_decision_record_get", err), nil
	}
	return textOK(string(out)), nil
}

// ----- record content helpers ---------------------------------------------

// decisionRecordOptionsArg reads the options argument as a JSON array of
// objects: either a real array, or a JSON array string such as
// `[{"id":"a","label":"A"}]`. It reports whether any options came through.
func decisionRecordOptionsArg(req mcpsdk.CallToolRequest) ([]any, bool) {
	switch v := req.GetArguments()["options"].(type) {
	case []any:
		if len(v) > 0 {
			return v, true
		}
	case string:
		var arr []any
		if err := json.Unmarshal([]byte(v), &arr); err == nil && len(arr) > 0 {
			return arr, true
		}
	}
	return nil, false
}

// decisionRecordObjectArg reads an object-valued argument (recommendation,
// links) as a map: either a real JSON object, or a JSON object string. It
// reports whether the field came through.
func decisionRecordObjectArg(req mcpsdk.CallToolRequest, key string) (map[string]any, bool) {
	switch v := req.GetArguments()[key].(type) {
	case map[string]any:
		if len(v) > 0 {
			return v, true
		}
	case string:
		var m map[string]any
		if err := json.Unmarshal([]byte(v), &m); err == nil && len(m) > 0 {
			return m, true
		}
	}
	return nil, false
}

// decisionRecordContentBody builds the REST create/update body from the
// arguments that came through; a field that was not given is left to the
// daemon's rules.
func decisionRecordContentBody(req mcpsdk.CallToolRequest) map[string]any {
	body := map[string]any{}
	for _, field := range []string{"label", "title", "situation", "deciding", "default"} {
		if v := strings.TrimSpace(req.GetString(field, "")); v != "" {
			body[field] = v
		}
	}
	if opts, ok := decisionRecordOptionsArg(req); ok {
		body["options"] = opts
	}
	if rec, ok := decisionRecordObjectArg(req, "recommendation"); ok {
		body["recommendation"] = rec
	}
	if links, ok := decisionRecordObjectArg(req, "links"); ok {
		body["links"] = links
	}
	return body
}

// recordContentToolParams returns the shared content parameters of the create
// and update tools.
func recordContentToolParams() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithString("label", mcpsdk.Description("A short name for the decision (optional)")),
		mcpsdk.WithString("title", mcpsdk.Description("The decision in one line")),
		mcpsdk.WithString("situation", mcpsdk.Description("What is true right now, in plain words")),
		mcpsdk.WithString("deciding", mcpsdk.Description("The decision the operator is being asked to make")),
		mcpsdk.WithString("options", mcpsdk.Description("The options, a JSON array of {\"id\",\"label\",\"tradeoff\"} objects; the ids are what the answer picks")),
		mcpsdk.WithString("recommendation", mcpsdk.Description("The author's pick and why, a JSON object {\"option_id\",\"reason\"} (optional)")),
		mcpsdk.WithString("default", mcpsdk.Description("The option id chosen when the decision times out, or the word hold (required)")),
		mcpsdk.WithString("links", mcpsdk.Description("What the record is about, a JSON object with any of {\"doc\",\"automaton\",\"registry_row\"} (optional)")),
	}
}

// ----- autonomous_decision_record_create ----------------------------------

func (s *Server) toolDecisionRecordCreate() mcpsdk.Tool {
	opts := []mcpsdk.ToolOption{
		mcpsdk.WithDescription("Create a decision record: the structured decision the operator answers — the label, the title, the situation, what is being decided, the options, the author's recommendation, the default option and the links. The record is the source: the daemon builds the queue question from it and raises it, and the answer comes back on the record. Returns the stored record with its question_id and the quality warnings."),
	}
	opts = append(opts, recordContentToolParams()...)
	opts = append(opts, mcpsdk.WithReadOnlyHintAnnotation(false))
	return mcpsdk.NewTool("autonomous_decision_record_create", opts...)
}

func (s *Server) handleDecisionRecordCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, decisionRecordsBase, decisionRecordContentBody(req))
	if err != nil {
		return toolError("autonomous_decision_record_create", err), nil
	}
	return textOK(string(out)), nil
}

// ----- autonomous_decision_record_update ----------------------------------

func (s *Server) toolDecisionRecordUpdate() mcpsdk.Tool {
	opts := []mcpsdk.ToolOption{
		mcpsdk.WithDescription("Update the content of a decision record (label, title, situation, deciding, options, recommendation, default and links). The record is the source: the daemon rebuilds the queue question from the updated record. Returns the record with its question_id and the quality warnings. Updating an answered or withdrawn record is refused by the daemon."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("The decision record id")),
	}
	opts = append(opts, recordContentToolParams()...)
	opts = append(opts, mcpsdk.WithReadOnlyHintAnnotation(false))
	return mcpsdk.NewTool("autonomous_decision_record_update", opts...)
}

func (s *Server) handleDecisionRecordUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("autonomous_decision_record_update", "id is required"), nil
	}
	out, err := s.proxyJSON(http.MethodPut, decisionRecordsBase+"/"+url.PathEscape(id), decisionRecordContentBody(req))
	if err != nil {
		return toolError("autonomous_decision_record_update", err), nil
	}
	return textOK(string(out)), nil
}

// ----- autonomous_decision_record_withdraw --------------------------------

func (s *Server) toolDecisionRecordWithdraw() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_decision_record_withdraw",
		mcpsdk.WithDescription("Withdraw a decision record: mark it withdrawn so it can no longer be answered. The daemon's queue question comes from the record, so a withdrawn record stops asking. Returns the record as it stands after the withdrawal."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("The decision record id")),
		mcpsdk.WithString("reason", mcpsdk.Description("What the operator said when withdrawing (optional)")),
		mcpsdk.WithReadOnlyHintAnnotation(false),
	)
}

func (s *Server) handleDecisionRecordWithdraw(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("autonomous_decision_record_withdraw", "id is required"), nil
	}
	var body any
	if reason := strings.TrimSpace(req.GetString("reason", "")); reason != "" {
		body = map[string]any{"reason": reason}
	}
	out, err := s.proxyJSON(http.MethodPost, decisionRecordsBase+"/"+url.PathEscape(id)+"/withdraw", body)
	if err != nil {
		return toolError("autonomous_decision_record_withdraw", err), nil
	}
	return textOK(string(out)), nil
}

// DecisionRecordToolNames lists the decision record tools, for the parity
// test.
func DecisionRecordToolNames() []string {
	return []string{
		"autonomous_decision_record_create",
		"autonomous_decision_record_update",
		"autonomous_decision_record_withdraw",
		"autonomous_decision_record_get",
		"autonomous_decision_record_list",
	}
}
