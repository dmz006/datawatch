// MCP tools for the operator question channel of the Automaton. They
// proxy the daemon's REST routes on the loopback exactly like the
// BL427 lifecycle-event subscription tools (autonomous_subscription_tools.go)
// — the MCP server holds no question state of its own; the daemon's
// question store is the source of truth:
//
//	POST   /api/questions              → question_raise
//	GET    /api/questions?state=&context=&asker=&text=&limit= → question_list
//	GET    /api/questions/{id}         → question_get
//	POST   /api/questions/{id}/answer  → question_answer
//	POST   /api/questions/{id}/withdraw → question_withdraw
//
// The daemon's handlers (internal/server/questions.go) are the
// authoritative validators: they produce the 400/404/409/413/422 messages
// that these handlers pass back verbatim, and any of them 503s while the
// autonomous.questions.enabled switch is off (it is off by default).
// The capability check is the one the subscription tools use: each
// registered tool has a federation.MCPToolCap entry (raise/answer/
// withdraw on autonomous:write, list/get on autonomous:read, mirroring
// the REST gate), enforced at the call chokepoint — the admin caller
// passes unrestricted.
package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// questionsPathBase is the collection route the raise and list tools proxy.
const questionsPathBase = "/api/questions"

// questionContexts describes the valid values for the context arguments
// (the question contexts and the open/answered/withdrawn/expired state);
// shared by the raise, list and get tool descriptions.
const questionContextsDesc = "one of none, prd, task, proposal or registry_row"

// --- Tool definitions ------------------------------------------------------

// toolQuestionRaise raises an operator question on the daemon.
func (s *Server) toolQuestionRaise() mcpsdk.Tool {
	return mcpsdk.NewTool("question_raise",
		mcpsdk.WithDescription("Raise a question to the operator on this daemon (the Automaton operator-question channel), so an automation blocked on a decision gets an answer back through its return path (event plus the polled GET, and into the asking session when return_path is set). Requires non-empty text and at least one option (a choice the operator can pick). asker: who is asking (default: the caller of record). context: "+questionContextsDesc+" (the daemon's store validates it and names the field in the 400). Returns 201 with the stored question (including its id, for question_get / question_answer). Proxies POST /api/questions; while the autonomous.questions.enabled switch is off the daemon 503s and names the switch. The CLI/PWA equivalents are the daemon's own question surfaces."),
		mcpsdk.WithString("text",
			mcpsdk.Required(),
			mcpsdk.Description("The question itself, one line of what decision is needed."),
		),
		mcpsdk.WithString("options",
			mcpsdk.Required(),
			mcpsdk.Description("At least one option: a string (used as both id and label, e.g. \"yes\"), a comma-separated string (\"yes,no,hold\"), or a JSON array of strings / {\"id\",\"label\",\"tradeoffs\"} objects. The ids are what question_answer passes back as chosen_option_id."),
		),
		mcpsdk.WithString("asker",
			mcpsdk.Description("Who is asking (default: the caller of record); e.g. the automation's session or \"operator\"."),
		),
		mcpsdk.WithString("asker_kind",
			mcpsdk.Description("Who raised it: session, supervisor, operator or system (default: operator)."),
		),
		mcpsdk.WithString("context",
			mcpsdk.Description("What the question is about: "+questionContextsDesc+". Required by the daemon's store; a value outside the set is a 400 naming the field."),
		),
		mcpsdk.WithString("prd_id",
			mcpsdk.Description("The Automaton (PRD) id the question is about, when context is prd or task."),
		),
		mcpsdk.WithString("task_id",
			mcpsdk.Description("The task id the question is about, when context is task."),
		),
		mcpsdk.WithString("label",
			mcpsdk.Description("Optional short type label for the question."),
		),
		mcpsdk.WithString("risk",
			mcpsdk.Description("Optional note of what goes wrong if the wrong option is taken."),
		),
		mcpsdk.WithString("priority",
			mcpsdk.Description("normal or high (high opts the question into alert fan-out). Default: normal."),
		),
		mcpsdk.WithBoolean("return_path",
			mcpsdk.Description("Also deliver the answer line into the asking session (default false; the event plus polled GET return path is always there)."),
		),
	)
}

// toolQuestionList lists stored questions, optionally filtered.
func (s *Server) toolQuestionList() mcpsdk.Tool {
	return mcpsdk.NewTool("question_list",
		mcpsdk.WithDescription("List the daemon's stored operator questions, optionally filtered, so a client that cannot subscribe can poll for new or changed ones. state: open, answered, withdrawn, expired or all (default all). context: "+questionContextsDesc+" or all. asker: substring/name of the asking party (omit or \"all\" for any). text: substring to match in the question text. limit: maximum questions to return (default 50, capped at 200 by the daemon). Returns {\"count\":<n>,\"questions\":[...]} — the question records, but not the answers: use question_get for one question's answer metadata. While the autonomous.questions.enabled switch is off the daemon 503s and names the switch. Proxies GET /api/questions."),
		mcpsdk.WithString("state",
			mcpsdk.Description("Question state filter: open, answered, withdrawn, expired or all (default all)."),
		),
		mcpsdk.WithString("context",
			mcpsdk.Description("Context filter: "+questionContextsDesc+" or all (default all)."),
		),
		mcpsdk.WithString("asker",
			mcpsdk.Description("Only questions asked by this asker (omit or \"all\" for any)."),
		),
		mcpsdk.WithString("text",
			mcpsdk.Description("Only questions whose text contains this substring."),
		),
		mcpsdk.WithNumber("limit",
			mcpsdk.Description("Maximum questions to return. Default 50, capped at 200 by the daemon."),
		),
	)
}

// toolQuestionGet returns one question and its current answer metadata.
func (s *Server) toolQuestionGet() mcpsdk.Tool {
	return mcpsdk.NewTool("question_get",
		mcpsdk.WithDescription("Return one stored operator question by id, plus its current answer metadata: answer_count (every answer ever recorded, superseded ones included), current_answer_id (the effective answer's id, \"\" when none) and the effective answer's summary (chosen option id, verbatim quote, actor, surface, created_at) — the default return path for an asker that cannot subscribe. 404 for an unknown id; while the autonomous.questions.enabled switch is off the daemon 503s and names the switch. Proxies GET /api/questions/{id}."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Question id (from question_raise or question_list)."),
		),
	)
}

// toolQuestionAnswer records an answer to a stored question.
func (s *Server) toolQuestionAnswer() mcpsdk.Tool {
	return mcpsdk.NewTool("question_answer",
		mcpsdk.WithDescription("Record an answer to a stored operator question (surfaces as answered to the asker's return path). At least one of chosen_option_id or a non-blank quote is required: pick an option by the id the question carries, or answer in the operator's own words with a quote (stored verbatim, capped at 16 KiB). An answer is immutable — a changed mind is a new answer that supersedes the old one (supersedes: the id of the effective answer to replace). 409 when the question is no longer open or the supersedes target is wrong, 422 when the answer text looks like a secret (store it in the datawatch secrets store and refer to it as ${secret:name} instead). Returns 201 with the stored answer and the question's new state. Proxies POST /api/questions/{id}/answer with surface set to mcp."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Question id to answer."),
		),
		mcpsdk.WithString("chosen_option_id",
			mcpsdk.Description("The option being chosen: one of the question's option ids. At least one of this or a non-blank quote is required."),
		),
		mcpsdk.WithString("quote",
			mcpsdk.Description("The answer in the operator's own words, stored verbatim. At least one of this or chosen_option_id is required."),
		),
		mcpsdk.WithString("supersedes",
			mcpsdk.Description("Id of the current effective answer to replace (a changed mind); required by the daemon when answering a question that already has a current answer. Omit for the first answer."),
		),
		mcpsdk.WithString("actor",
			mcpsdk.Description("Who is answering (default: the caller of record)."),
		),
		mcpsdk.WithString("source_message_id",
			mcpsdk.Description("Optional id of the upstream message carrying the answer (comm channels; provenance only)."),
		),
	)
}

// toolQuestionWithdraw withdraws an open question.
func (s *Server) toolQuestionWithdraw() mcpsdk.Tool {
	return mcpsdk.NewTool("question_withdraw",
		mcpsdk.WithDescription("Withdraw an open operator question (marked withdrawn; it can no longer be answered). actor is the one withdrawing (required: a withdrawal is an attribution-bearing action, unlike an answer, which defaults to the caller of record). reason is optional. 409 when the question is not open (already answered, withdrawn, or expired), 404 for an unknown id. Returns the question with its new state. Proxies POST /api/questions/{id}/withdraw."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Question id to withdraw."),
		),
		mcpsdk.WithString("actor",
			mcpsdk.Required(),
			mcpsdk.Description("Who is withdrawing the question."),
		),
		mcpsdk.WithString("reason",
			mcpsdk.Description("Optional reason for the withdrawal, recorded on the question."),
		),
	)
}

// --- Handlers ---------------------------------------------------------------

// questionOptions parses the options argument: a string (one option, or a
// comma-separated option list), a []string, or a JSON array of strings or
// objects. Each option normalises to {"id","label"[,"tradeoffs"]}; ok is
// false when no usable option came through.
func questionOptions(v any) (opts []map[string]any, ok bool) {
	strOpt := func(s string) map[string]any {
		return map[string]any{"id": s, "label": s}
	}
	appendOpt := func(o any) bool {
		switch opt := o.(type) {
		case string:
			if opt = strings.TrimSpace(opt); opt != "" {
				opts = append(opts, strOpt(opt))
				return true
			}
		case map[string]any:
			id, _ := opt["id"].(string)
			label, _ := opt["label"].(string)
			if id = strings.TrimSpace(id); id == "" {
				return false
			}
			n := map[string]any{"id": id}
			if label = strings.TrimSpace(label); label != "" {
				n["label"] = label
			}
			if t, _ := opt["tradeoffs"].(string); t != "" {
				n["tradeoffs"] = t
			}
			opts = append(opts, n)
			return true
		}
		return false
	}
	switch a := v.(type) {
	case string:
		for _, part := range strings.Split(a, ",") {
			appendOpt(part)
		}
	case []string:
		for _, s := range a {
			appendOpt(s)
		}
	case []any:
		for _, item := range a {
			appendOpt(item)
		}
	}
	return opts, len(opts) > 0
}

// handleQuestionRaise proxies POST /api/questions.
func (s *Server) handleQuestionRaise(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	text := req.GetString("text", "")
	if text == "" {
		return toolErrorf("question_raise", "text is required"), nil
	}
	opts, ok := questionOptions(req.GetArguments()["options"])
	if !ok {
		return toolErrorf("question_raise", "options is required: pass at least one option (a string, a comma-separated list, or an array of {\"id\",\"label\"} objects)"), nil
	}
	body := map[string]any{"text": text, "options": opts}
	if asker := strings.TrimSpace(req.GetString("asker", "")); asker != "" {
		body["asked_by"] = asker
	}
	if kind := strings.TrimSpace(req.GetString("asker_kind", "")); kind != "" {
		body["asker_kind"] = kind
	}
	if context := strings.TrimSpace(req.GetString("context", "")); context != "" {
		body["context"] = context
	}
	if prdID := strings.TrimSpace(req.GetString("prd_id", "")); prdID != "" {
		body["prd_id"] = prdID
	}
	if taskID := strings.TrimSpace(req.GetString("task_id", "")); taskID != "" {
		body["task_id"] = taskID
	}
	if label := strings.TrimSpace(req.GetString("label", "")); label != "" {
		body["label"] = label
	}
	if risk := strings.TrimSpace(req.GetString("risk", "")); risk != "" {
		body["risk"] = risk
	}
	if priority := strings.TrimSpace(req.GetString("priority", "")); priority != "" {
		body["priority"] = priority
	}
	if req.GetBool("return_path", false) {
		body["return_path"] = true
	}
	respBody, err := s.proxyJSON(http.MethodPost, questionsPathBase, body)
	if err != nil {
		return toolError("question_raise", err), nil
	}
	return textOK(string(respBody)), nil
}

// handleQuestionList proxies GET /api/questions?state=&context=&asker=&text=&limit=.
func (s *Server) handleQuestionList(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := strings.TrimSpace(req.GetString("state", "")); v != "" {
		q.Set("state", v)
	}
	if v := strings.TrimSpace(req.GetString("context", "")); v != "" {
		q.Set("context", v)
	}
	if v := strings.TrimSpace(req.GetString("asker", "")); v != "" {
		q.Set("asker", v)
	}
	if v := strings.TrimSpace(req.GetString("text", "")); v != "" {
		q.Set("text", v)
	}
	if limit, ok := getNumberArg(req, "limit"); ok {
		if limit < 1 || limit != float64(int(limit)) {
			return toolErrorf("question_list", "limit must be a positive whole number"), nil
		}
		q.Set("limit", strconv.Itoa(int(limit)))
	}
	path := questionsPathBase
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	respBody, err := s.proxyJSON(http.MethodGet, path, nil)
	if err != nil {
		return toolError("question_list", err), nil
	}
	return textOK(string(respBody)), nil
}

// handleQuestionGet proxies GET /api/questions/{id}.
func (s *Server) handleQuestionGet(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("question_get", "id is required"), nil
	}
	respBody, err := s.proxyJSON(http.MethodGet, questionsPathBase+"/"+url.PathEscape(id), nil)
	if err != nil {
		return toolError("question_get", err), nil
	}
	return textOK(string(respBody)), nil
}

// handleQuestionAnswer proxies POST /api/questions/{id}/answer with
// surface set to mcp; chosen_option_id / quote / supersedes / actor /
// source_message_id map 1:1 to the REST body.
func (s *Server) handleQuestionAnswer(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("question_answer", "id is required"), nil
	}
	chosen := strings.TrimSpace(req.GetString("chosen_option_id", ""))
	quote := req.GetString("quote", "")
	if chosen == "" && strings.TrimSpace(quote) == "" {
		return toolErrorf("question_answer", "at least one of chosen_option_id or a non-blank quote is required"), nil
	}
	body := map[string]any{"surface": "mcp"}
	if chosen != "" {
		body["chosen_option_id"] = chosen
	}
	if quote != "" {
		body["quote"] = quote
	}
	if supersedes := strings.TrimSpace(req.GetString("supersedes", "")); supersedes != "" {
		body["supersedes"] = supersedes
	}
	if actor := strings.TrimSpace(req.GetString("actor", "")); actor != "" {
		body["actor"] = actor
	}
	if msgID := strings.TrimSpace(req.GetString("source_message_id", "")); msgID != "" {
		body["source_message_id"] = msgID
	}
	respBody, err := s.proxyJSON(http.MethodPost, questionsPathBase+"/"+url.PathEscape(id)+"/answer", body)
	if err != nil {
		return toolError("question_answer", err), nil
	}
	return textOK(string(respBody)), nil
}

// handleQuestionWithdraw proxies POST /api/questions/{id}/withdraw.
func (s *Server) handleQuestionWithdraw(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("question_withdraw", "id is required"), nil
	}
	actor := strings.TrimSpace(req.GetString("actor", ""))
	if actor == "" {
		return toolErrorf("question_withdraw", "actor is required"), nil
	}
	body := map[string]any{"actor": actor}
	if reason := strings.TrimSpace(req.GetString("reason", "")); reason != "" {
		body["reason"] = reason
	}
	respBody, err := s.proxyJSON(http.MethodPost, questionsPathBase+"/"+url.PathEscape(id)+"/withdraw", body)
	if err != nil {
		return toolError("question_withdraw", err), nil
	}
	return textOK(string(respBody)), nil
}
