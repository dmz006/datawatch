// "Done when" list tools. They proxy the loopback REST surface
// (/api/acceptance) so names and validation match REST, the command line and
// the comm channel. They are registered only when acceptance.mode is report.
//
// The loopback call carries the daemon's own token, so every write says it came
// from a model tool, with the calling session when it is known, so that an
// answer from a recorded holder reads as a claim. The fields are not verified:
// who counts is recorded, not enforced, until the authentication work. No tool
// takes command text, submits a verdict or sets an item met.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolAcceptanceView() mcpsdk.Tool {
	return mcpsdk.NewTool("acceptance_view",
		mcpsdk.WithDescription("Read \"done when\" lists. With id: that request's list, how each item reads (met, not met, cannot tell, unchecked), what changed since it was first fixed, weak-list flags and the list hash. Without id: every list in short with open items grouped by reason. A request with no list reads \"no criteria\"; never treat \"completed\" as done."),
		mcpsdk.WithString("id", mcpsdk.Description("Request id (an Automaton id or another request)")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAcceptanceView(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if id := strings.TrimSpace(req.GetString("id", "")); id != "" {
		q.Set("id", id)
	}
	out, err := s.proxyGet("/api/acceptance", q)
	if err != nil {
		return toolError("acceptance_view", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAcceptanceEdit() mcpsdk.Tool {
	return mcpsdk.NewTool("acceptance_edit",
		mcpsdk.WithDescription("Create or change a request's \"done when\" list. A draft changes freely; after it is fixed every change is recorded as a revision and shown against the first version. To fix a draft send only id, freeze=true and the hash you read. Items cannot carry command or script text: a command item names a project check the operator registered."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Request id")),
		mcpsdk.WithString("source", mcpsdk.Description("New list only: where the request came from (message, intake, handoff, question, report, research, design, docs, automaton, bug, code, operation, rule, schedule). It only adds that source's default items; the list is held to every rule whatever you name")),
		mcpsdk.WithString("ask", mcpsdk.Description("New list only: the ask, word for word")),
		mcpsdk.WithString("add_ask", mcpsdk.Description("Later words added to the ask; they open new items")),
		mcpsdk.WithString("items", mcpsdk.Description("JSON array of items to add or replace: {id, statement, from, method, project_check, inputs, expect:[{pass,fail}], state:{path,contains,want,target}, wait:{after_seconds,window_seconds,max_gap_seconds}, before, change, negative, report_only, effect}")),
		mcpsdk.WithArray("remove", mcpsdk.WithStringItems(), mcpsdk.Description("Item ids to remove")),
		mcpsdk.WithString("needed_by", mcpsdk.Description("RFC 3339 time the result is needed by")),
		mcpsdk.WithString("words", mcpsdk.Description("Why, or the words that were said; recorded with the change")),
		mcpsdk.WithString("hash", mcpsdk.Description("The list hash you read; the change is refused if the list moved")),
		mcpsdk.WithBoolean("freeze", mcpsdk.Description("Fix the list named by hash before work starts")),
		mcpsdk.WithString("actor", mcpsdk.Description("Name to record beside the observed caller")),
	)
}

// acceptanceCallerBody marks a write as coming from a model tool.
func (s *Server) acceptanceCallerBody(ctx context.Context, req mcpsdk.CallToolRequest, body map[string]any) {
	body["via"] = "mcp"
	if sid, scoped := s.scopedCallerSessionID(ctx); scoped && sid != "" {
		body["via_session"] = sid
	}
	if a := strings.TrimSpace(req.GetString("actor", "")); a != "" {
		body["actor"] = a
	}
}

func copyStringArgs(req mcpsdk.CallToolRequest, body map[string]any, keys ...string) {
	for _, k := range keys {
		if v := req.GetString(k, ""); strings.TrimSpace(v) != "" {
			body[k] = v
		}
	}
}

func (s *Server) acceptanceWrite(op, path string, body map[string]any) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, path, body)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleAcceptanceEdit(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "acceptance_edit"
	if strings.TrimSpace(req.GetString("id", "")) == "" {
		return toolErrorf(op, "id is required"), nil
	}
	body := map[string]any{}
	copyStringArgs(req, body, "id", "source", "ask", "add_ask", "needed_by", "words", "hash")
	if raw := strings.TrimSpace(req.GetString("items", "")); raw != "" {
		var items []map[string]any
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return toolErrorf(op, "items must be a JSON array of objects: %v", err), nil
		}
		body["items"] = items
	}
	if ids := req.GetStringSlice("remove", nil); len(ids) > 0 {
		body["remove"] = ids
	}
	if req.GetBool("freeze", false) {
		body["freeze"] = true
	}
	s.acceptanceCallerBody(ctx, req, body)
	return s.acceptanceWrite(op, "/api/acceptance/edit", body)
}

func (s *Server) toolAcceptanceConfirm() mcpsdk.Tool {
	return mcpsdk.NewTool("acceptance_confirm",
		mcpsdk.WithDescription("Record a person's answer on a confirm item, or on item \"list\" that the list covers the whole ask (needs hash). The words that were said are required. An answer datawatch can see may be the doer's is kept as a claim and does not count; who is answering is not verified. This cannot set an item that is checked another way."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Request id")),
		mcpsdk.WithString("item", mcpsdk.Required(), mcpsdk.Description("Item id, or list")),
		mcpsdk.WithString("answer", mcpsdk.Required(), mcpsdk.Description("yes or no")),
		mcpsdk.WithString("words", mcpsdk.Required(), mcpsdk.Description("The words that were said")),
		mcpsdk.WithString("relayed_for", mcpsdk.Description("Who said it, when you are passing on an outside person's answer")),
		mcpsdk.WithString("hash", mcpsdk.Description("The list hash you read (required for item list)")),
		mcpsdk.WithString("actor", mcpsdk.Description("Name to record beside the observed caller")),
	)
}

func (s *Server) handleAcceptanceConfirm(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "acceptance_confirm"
	for _, k := range []string{"id", "item", "answer", "words"} {
		if strings.TrimSpace(req.GetString(k, "")) == "" {
			return toolErrorf(op, "%s is required", k), nil
		}
	}
	body := map[string]any{}
	copyStringArgs(req, body, "id", "item", "answer", "words", "relayed_for", "hash")
	s.acceptanceCallerBody(ctx, req, body)
	return s.acceptanceWrite(op, "/api/acceptance/confirm", body)
}

func (s *Server) toolAcceptanceAccept() mcpsdk.Tool {
	return mcpsdk.NewTool("acceptance_accept",
		mcpsdk.WithDescription("Record that an item is accepted without proof (operator action, or a delegate passing on the operator's words). It never reads met: the request shows \"accepted without proof\". A recorded doer's own acceptance is kept as a claim and does not count."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Request id")),
		mcpsdk.WithString("item", mcpsdk.Required(), mcpsdk.Description("Item id")),
		mcpsdk.WithString("reason", mcpsdk.Required(), mcpsdk.Description("Why it is accepted without proof")),
		mcpsdk.WithString("words", mcpsdk.Required(), mcpsdk.Description("The words that were said")),
		mcpsdk.WithString("hash", mcpsdk.Description("The list hash you read")),
		mcpsdk.WithString("actor", mcpsdk.Description("Name to record beside the observed caller")),
	)
}

func (s *Server) handleAcceptanceAccept(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "acceptance_accept"
	for _, k := range []string{"id", "item", "reason", "words"} {
		if strings.TrimSpace(req.GetString(k, "")) == "" {
			return toolErrorf(op, "%s is required", k), nil
		}
	}
	body := map[string]any{}
	copyStringArgs(req, body, "id", "item", "reason", "words", "hash")
	s.acceptanceCallerBody(ctx, req, body)
	return s.acceptanceWrite(op, "/api/acceptance/accept", body)
}

func (s *Server) toolAcceptanceReport() mcpsdk.Tool {
	return mcpsdk.NewTool("acceptance_report",
		mcpsdk.WithDescription("The standing check of \"done when\" lists. It only reports and blocks nothing: requests started without a fixed list (I18), done without current proof or on a list changed since it was first fixed (I19), and changes without a record or without words (I20), plus open items grouped by reason."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAcceptanceReport(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/acceptance/report", nil)
	if err != nil {
		return toolError("acceptance_report", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAcceptanceCatalog() mcpsdk.Tool {
	return mcpsdk.NewTool("acceptance_catalog",
		mcpsdk.WithDescription("What a \"done when\" list can be built from: check methods, default items per kind, optional packs, and the names of the project checks the operator registered (never their commands)."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAcceptanceCatalog(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/acceptance/catalog", nil)
	if err != nil {
		return toolError("acceptance_catalog", err), nil
	}
	return textOK(string(out)), nil
}
