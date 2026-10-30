// MCP tools for Automation records (a chain of Automatons with per-outcome
// edges). Mirrors internal/mcp/retry_tools.go's shape exactly.
//
// Tools:
//   automation_list   — list Automations
//   automation_get    — get one Automation by ID
//   automation_create — create an Automation
//   automation_update — update an Automation
//   automation_delete — delete an Automation
//   automation_import_graph — convert an orchestrator graph to a draft Automation
//   automation_activate — start a draft Automation
//   automation_set_llm / automation_set_verification_llm — Automation LLM defaults
//   automation_set_decision_assistant — optional local-model decision assistant

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── automation_list ─────────────────────────────────────────────────────

func (s *Server) toolAutomationList() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_list",
		mcpsdk.WithDescription("List all Automations. An Automation is a chain of Automatons (PRDs) connected by per-outcome edges. Each row carries its budget view (spend against the ceiling, window, reset time and whether it resumes by itself) when a budget policy is in play. Activate a draft with automation_activate."),
	)
}

func (s *Server) handleAutomationList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	data, err := s.proxyJSON(http.MethodGet, "/api/automations", nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── automation_create ───────────────────────────────────────────────────

// automationMembersJSONDesc documents the members_json argument of
// automation_create and automation_update: the linear member set plus the
// Phase 3a kinds (fork, join, nested automation) and their own fields.
const automationMembersJSONDesc = `JSON array of members. Each member has: id, prd_id, on_complete, on_blocked, on_failed (member IDs or "" = stop), added_reason. Optional kind picks the member shape: "" or "prd" (default) = a member Automaton named by prd_id; "automation" = a nested Automation named by automation_id; "fork" = starts the branch members listed in fork_members together (at least 2 member IDs); "join" = waits for the branch members listed in join_members, which must equal the paired fork's fork_members, then applies on_branch_failure: "all_must_succeed" (default), "any_one_is_enough" or "continue_anyway". Set only the fields that belong to a member's kind; fork_members and join_members are arrays of member IDs.`

func (s *Server) toolAutomationCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_create",
		mcpsdk.WithDescription("Create a new Automation — a named chain of Automatons with per-outcome edges, an entry member, optional budgets and execution/verification defaults. Members can also be fork/join control nodes or nested Automations. The Automation stays a draft until automation_activate."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Automation name.")),
		mcpsdk.WithString("description", mcpsdk.Description("Optional description.")),
		mcpsdk.WithString("entry_member_id", mcpsdk.Description("Member ID where the chain starts.")),
		mcpsdk.WithString("members_json", mcpsdk.Description(automationMembersJSONDesc)),
		mcpsdk.WithString("budget_policy_json", mcpsdk.Description("Optional JSON object — budget policy for this Automation.")),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Optional project directory (informational).")),
		mcpsdk.WithString("backend", mcpsdk.Description("Optional execution backend default.")),
		mcpsdk.WithString("model", mcpsdk.Description("Optional execution model default.")),
		mcpsdk.WithString("effort", mcpsdk.Description("Optional execution effort default.")),
		mcpsdk.WithString("verification_backend", mcpsdk.Description("Optional verification backend default.")),
		mcpsdk.WithString("verification_model", mcpsdk.Description("Optional verification model default.")),
		mcpsdk.WithString("verification_effort", mcpsdk.Description("Optional verification effort default.")),
	)
}

// automationProxyBody converts the MCP tool arguments into a REST request
// body, forwarding only the fields the caller actually provided. Empty
// strings, nulls, an empty members array and an empty budget object are
// dropped so the server treats them as "unchanged" (partial updates) rather
// than as explicit empty values, which would fail server-side validation
// (e.g. a zero-valued budget policy has no ceiling type).
func automationProxyBody(req mcpsdk.CallToolRequest) map[string]any {
	body := map[string]any{}
	args := req.GetArguments()
	for k, v := range args {
		if v == nil {
			continue
		}
		switch k {
		case "members_json":
			var m []map[string]any
			if err := json.Unmarshal([]byte(asJSONString(v)), &m); err == nil && len(m) > 0 {
				body["members"] = m
			}
		case "budget_policy_json":
			var b map[string]any
			if err := json.Unmarshal([]byte(asJSONString(v)), &b); err == nil && len(b) > 0 {
				body["budget_policy"] = b
			}
		case "id":
			// path parameter, not part of the body
		default:
			if str, ok := v.(string); ok && str == "" {
				continue
			}
			body[k] = v
		}
	}
	return body
}

func asJSONString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Server) handleAutomationCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := automationProxyBody(req)
	data, err := s.proxyJSON(http.MethodPost, "/api/automations", body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── automation_get ──────────────────────────────────────────────────────

func (s *Server) toolAutomationGet() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_get",
		mcpsdk.WithDescription("Get an Automation by ID (status, members, edges, budget policy, execution defaults) together with its budget view: spend against the ceiling, the window and reset time, whether it resumes by itself, and the estimate and peer notes. The result also lists pending_inserts (each with id, which is the member id to pass to automation_approve_insert, prd_id, reason and actor) for inserts held for the operator's approval."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automation ID.")),
	)
}

func (s *Server) handleAutomationGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	data, err := s.proxyJSON(http.MethodGet, "/api/automations/"+id, nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── automation_update ───────────────────────────────────────────────────

func (s *Server) toolAutomationUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_update",
		mcpsdk.WithDescription("Update an Automation (name, description, entry member, members, budgets, defaults). Provide only the fields to change. The Automation stays a draft until automation_activate."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automation ID.")),
		mcpsdk.WithNumber("expected_version", mcpsdk.Description("Optional — the version you read; the update is rejected if the record moved since (optimistic concurrency).")),
		mcpsdk.WithString("reason", mcpsdk.Description("Reason for this update (recorded alongside the change).")),
		mcpsdk.WithString("name", mcpsdk.Description("New name.")),
		mcpsdk.WithString("description", mcpsdk.Description("New description.")),
		mcpsdk.WithString("entry_member_id", mcpsdk.Description("New entry member ID.")),
		mcpsdk.WithString("members_json", mcpsdk.Description("New members. "+automationMembersJSONDesc)),
		mcpsdk.WithString("budget_policy_json", mcpsdk.Description("New budget policy as a JSON object.")),
		mcpsdk.WithString("project_dir", mcpsdk.Description("New project directory.")),
		mcpsdk.WithString("backend", mcpsdk.Description("New execution backend default.")),
		mcpsdk.WithString("model", mcpsdk.Description("New execution model default.")),
		mcpsdk.WithString("effort", mcpsdk.Description("New execution effort default.")),
		mcpsdk.WithString("verification_backend", mcpsdk.Description("New verification backend default.")),
		mcpsdk.WithString("verification_model", mcpsdk.Description("New verification model default.")),
		mcpsdk.WithString("verification_effort", mcpsdk.Description("New verification effort default.")),
	)
}

func (s *Server) handleAutomationUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body := automationProxyBody(req)
	data, err := s.proxyJSON(http.MethodPut, "/api/automations/"+id, body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── automation_delete ───────────────────────────────────────────────────

func (s *Server) toolAutomationDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_delete",
		mcpsdk.WithDescription("Delete an Automation by ID. Its member Automatons are not affected — the back-reference is cleared and each keeps running or sitting as an ordinary Automaton."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automation ID.")),
	)
}

func (s *Server) handleAutomationDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	_, err := s.proxyJSON(http.MethodDelete, "/api/automations/"+id, nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(`{"status":"deleted"}`), nil
}

// ── automation_activate ─────────────────────────────────────────────────

func (s *Server) toolAutomationActivate() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_activate",
		mcpsdk.WithDescription("Activate a draft Automation: validate it, start its entry member through the normal approval gate and let it advance member to member as each finishes. If the entry member cannot start the Automation goes blocked instead. Only a draft can be activated."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automation ID.")),
		mcpsdk.WithString("actor", mcpsdk.Description("Who is activating (recorded in the audit trail). Defaults to operator.")),
		mcpsdk.WithString("reason", mcpsdk.Description("Reason for activating (recorded alongside the audit entry).")),
	)
}

func (s *Server) handleAutomationActivate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body := map[string]any{}
	if v := req.GetString("actor", ""); v != "" {
		body["actor"] = v
	}
	if v := req.GetString("reason", ""); v != "" {
		body["reason"] = v
	}
	data, err := s.proxyJSON(http.MethodPost, "/api/automations/"+id+"/activate", body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── automation_import_graph ─────────────────────────────────────────────

func (s *Server) toolAutomationImportGraph() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_import_graph",
		mcpsdk.WithDescription("Convert an orchestrator graph into a new draft Automation. The graph is only read, never changed. With dry_run the converted Automation input and any warnings are returned and nothing is created. A graph an Automation cannot express is refused with the reason."),
		mcpsdk.WithString("graph_id", mcpsdk.Required(), mcpsdk.Description("Orchestrator graph ID.")),
		mcpsdk.WithBoolean("dry_run", mcpsdk.Description("Return what would be created without creating it.")),
	)
}

func (s *Server) handleAutomationImportGraph(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("graph_id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("error: graph_id is required"), nil
	}
	body := map[string]any{"dry_run": req.GetBool("dry_run", false)}
	data, err := s.proxyJSON(http.MethodPost, "/api/automations/import-graph/"+url.PathEscape(id), body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── operator control: pause, resume, cancel, skip, insert, remove, reorder, approve ──
//
//   automation_pause / automation_resume / automation_cancel
//   automation_skip_member / automation_insert_member / automation_remove_member
//   automation_reorder_edges / automation_approve_insert
//
// Each proxies to POST /api/automations/{id}/<op>. Every one takes the
// automation_id and the expected_version the caller read; a stale version comes
// back as a conflict error.

// automationControlBase returns the options every control tool shares.
func automationControlBase(desc string) []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithDescription(desc),
		mcpsdk.WithString("automation_id", mcpsdk.Required(), mcpsdk.Description("Automation ID.")),
		mcpsdk.WithNumber("expected_version", mcpsdk.Required(), mcpsdk.Description("The Automation version you read (automation_get). The call is refused with a conflict if the record moved since.")),
		mcpsdk.WithString("actor", mcpsdk.Description("Who is acting (recorded in the audit trail). Defaults to operator.")),
		mcpsdk.WithString("reason", mcpsdk.Description("Reason for the change (recorded alongside the audit entry).")),
	}
}

// automationControlCall posts the common fields plus extra to
// /api/automations/{automation_id}/{op} and returns the tool result.
func (s *Server) automationControlCall(req mcpsdk.CallToolRequest, op string, extra map[string]any) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("automation_id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("error: automation_id is required"), nil
	}
	args := req.GetArguments()
	version, ok := controlVersion(args["expected_version"])
	if !ok {
		return mcpsdk.NewToolResultText("error: expected_version is required (the Automation version you read)"), nil
	}
	body := map[string]any{"expected_version": version}
	if v := req.GetString("actor", ""); v != "" {
		body["actor"] = v
	}
	if v := req.GetString("reason", ""); v != "" {
		body["reason"] = v
	}
	for k, v := range extra {
		body[k] = v
	}
	data, err := s.proxyJSON(http.MethodPost, "/api/automations/"+id+"/"+op, body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// controlVersion reads expected_version out of decoded tool arguments.
func controlVersion(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

// optionalString adds key to extra when the argument is a non-empty string.
func optionalString(req mcpsdk.CallToolRequest, extra map[string]any, key string) {
	if v := req.GetString(key, ""); v != "" {
		extra[key] = v
	}
}

func (s *Server) toolAutomationPause() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_pause", automationControlBase("Pause an active Automation. Members already running keep running and nothing is ended; a paused Automation moves on when a member finishes but starts nothing until automation_resume.")...)
}

func (s *Server) handleAutomationPause(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.automationControlCall(req, "pause", nil)
}

func (s *Server) toolAutomationResume() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_resume", automationControlBase("Resume a paused Automation: members that finished while it was paused are routed on and the next member is started.")...)
}

func (s *Server) handleAutomationResume(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.automationControlCall(req, "resume", nil)
}

func (s *Server) toolAutomationCancel() mcpsdk.Tool {
	opts := append(automationControlBase("Cancel an Automation. cancel_in_flight is required and has no default: true ends the session of every running member, false leaves running members as they are. Members not yet run become cancelled either way."),
		mcpsdk.WithBoolean("cancel_in_flight", mcpsdk.Required(), mcpsdk.Description("Required. true ends the session of every in-flight member; false leaves in-flight members running.")),
	)
	return mcpsdk.NewTool("automation_cancel", opts...)
}

func (s *Server) handleAutomationCancel(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	raw, present := req.GetArguments()["cancel_in_flight"]
	choice, isBool := raw.(bool)
	if !present || raw == nil || !isBool {
		return mcpsdk.NewToolResultText("error: cancel_in_flight is required and has no default: pass cancel_in_flight=true (end the session of every in-flight member) or cancel_in_flight=false (leave in-flight members running)"), nil
	}
	return s.automationControlCall(req, "cancel", map[string]any{"cancel_in_flight": choice})
}

func (s *Server) toolAutomationSkipMember() mcpsdk.Tool {
	opts := append(automationControlBase("Skip an active member and move on along the chosen edge, as if it had ended that way. A session the member is running is not ended. In a forked Automation with several active branches, member_id names the branch."),
		mcpsdk.WithString("member_id", mcpsdk.Description("The active member to skip. Required when more than one branch is active.")),
		mcpsdk.WithString("edge", mcpsdk.Description("Edge to follow: on_complete (default), on_blocked or on_failed.")),
	)
	return mcpsdk.NewTool("automation_skip_member", opts...)
}

func (s *Server) handleAutomationSkipMember(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	extra := map[string]any{}
	optionalString(req, extra, "member_id")
	optionalString(req, extra, "edge")
	return s.automationControlCall(req, "skip_member", extra)
}

func (s *Server) toolAutomationInsertMember() mcpsdk.Tool {
	opts := append(automationControlBase("Insert a PRD as a new member of an Automation. On a draft it lands in the graph at once; on a running Automation it is held until the operator approves it (automation_approve_insert). reason is required."),
		mcpsdk.WithString("prd_id", mcpsdk.Required(), mcpsdk.Description("The member PRD to insert.")),
		mcpsdk.WithString("name", mcpsdk.Description("Optional display name for the inserted member.")),
		mcpsdk.WithBoolean("urgent", mcpsdk.Description("Take the next slot instead of the end of the chain. Only one urgent insert may be pending at a time.")),
		mcpsdk.WithString("after", mcpsdk.Description("Insert directly after this member ID. Leave empty for the end of the chain; an urgent insert must leave it empty.")),
	)
	return mcpsdk.NewTool("automation_insert_member", opts...)
}

func (s *Server) handleAutomationInsertMember(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if req.GetString("reason", "") == "" {
		return mcpsdk.NewToolResultText("error: reason is required to insert a member"), nil
	}
	member := map[string]any{"prd_id": req.GetString("prd_id", "")}
	if v := req.GetString("name", ""); v != "" {
		member["name"] = v
	}
	extra := map[string]any{"member": member}
	if req.GetBool("urgent", false) {
		extra["urgent"] = true
	}
	optionalString(req, extra, "after")
	return s.automationControlCall(req, "insert_member", extra)
}

func (s *Server) toolAutomationRemoveMember() mcpsdk.Tool {
	opts := append(automationControlBase("Remove a member that has not started and has no outgoing edges. The entry member, running or finished members, forks, joins and fork or join branches are refused."),
		mcpsdk.WithString("member_id", mcpsdk.Required(), mcpsdk.Description("The member to remove.")),
	)
	return mcpsdk.NewTool("automation_remove_member", opts...)
}

func (s *Server) handleAutomationRemoveMember(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	extra := map[string]any{}
	optionalString(req, extra, "member_id")
	return s.automationControlCall(req, "remove_member", extra)
}

func (s *Server) toolAutomationReorderEdges() mcpsdk.Tool {
	opts := append(automationControlBase("Rewire the outgoing edges of a member that has not started. Each edge argument is the member ID that edge now names; leave one out to clear it."),
		mcpsdk.WithString("member_id", mcpsdk.Required(), mcpsdk.Description("The member whose edges are rewired.")),
		mcpsdk.WithString("on_complete", mcpsdk.Description("Member ID to run when this member completes; empty for none.")),
		mcpsdk.WithString("on_blocked", mcpsdk.Description("Member ID to run when this member is blocked; empty for none.")),
		mcpsdk.WithString("on_failed", mcpsdk.Description("Member ID to run when this member fails; empty for none.")),
	)
	return mcpsdk.NewTool("automation_reorder_edges", opts...)
}

func (s *Server) handleAutomationReorderEdges(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	// All three edges are sent: an omitted edge means "no edge", not "keep".
	extra := map[string]any{
		"member_id":   req.GetString("member_id", ""),
		"on_complete": req.GetString("on_complete", ""),
		"on_blocked":  req.GetString("on_blocked", ""),
		"on_failed":   req.GetString("on_failed", ""),
	}
	return s.automationControlCall(req, "reorder_edges", extra)
}

func (s *Server) toolAutomationApproveInsert() mcpsdk.Tool {
	opts := append(automationControlBase("Approve a pending inserted member, promoting it into the graph at its reserved position. Only the operator may approve."),
		mcpsdk.WithString("member_id", mcpsdk.Required(), mcpsdk.Description("The pending member ID returned by automation_insert_member.")),
	)
	return mcpsdk.NewTool("automation_approve_insert", opts...)
}

func (s *Server) handleAutomationApproveInsert(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	extra := map[string]any{}
	optionalString(req, extra, "member_id")
	return s.automationControlCall(req, "approve_insert", extra)
}

// automationLLMBase returns the options the Automation LLM setters share.
// expected_version is optional here (omitted or 0 skips the version check), so
// automationControlBase is not used.
func automationLLMBase(desc string) []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithDescription(desc),
		mcpsdk.WithString("automation_id", mcpsdk.Required(), mcpsdk.Description("Automation ID.")),
		mcpsdk.WithNumber("expected_version", mcpsdk.Description("Optional. The Automation version you read (automation_get); a stale value is refused with a conflict. Omit or pass 0 to skip the check.")),
		mcpsdk.WithString("actor", mcpsdk.Description("Who is acting (recorded in the audit trail). Defaults to operator.")),
		mcpsdk.WithString("reason", mcpsdk.Description("Reason for the change (recorded alongside the audit entry).")),
	}
}

// automationLLMCall posts an LLM setter to /api/automations/{automation_id}/{op}.
// Only the keys the caller passed are forwarded, so an empty string clears a
// field and an absent key leaves it unchanged. With none of fields present it
// returns an error text and sends nothing.
func (s *Server) automationLLMCall(req mcpsdk.CallToolRequest, op string, fields []string) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("automation_id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("error: automation_id is required"), nil
	}
	args := req.GetArguments()
	body := map[string]any{}
	for _, k := range fields {
		if v, ok := args[k].(string); ok {
			body[k] = v
		}
	}
	if len(body) == 0 {
		return mcpsdk.NewToolResultText("error: supply at least one of: " + strings.Join(fields, ", ")), nil
	}
	if v, ok := controlVersion(args["expected_version"]); ok && v != 0 {
		body["expected_version"] = v
	}
	optionalString(req, body, "actor")
	optionalString(req, body, "reason")
	data, err := s.proxyJSON(http.MethodPost, "/api/automations/"+id+"/"+op, body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

var (
	automationLLMFields             = []string{"backend", "model", "effort", "planning_backend", "planning_model"}
	automationVerificationLLMFields = []string{"backend", "model", "effort"}
)

func (s *Server) toolAutomationSetLLM() mcpsdk.Tool {
	opts := append(automationLLMBase("Set the Automation-level execution and planning defaults used by members that name none of their own. Supply at least one of backend, model, effort, planning_backend, planning_model: an omitted field is left unchanged and an empty string clears it. Allowed while the Automation is draft, active, paused or blocked."),
		mcpsdk.WithString("backend", mcpsdk.Description("Execution backend default; empty clears it.")),
		mcpsdk.WithString("model", mcpsdk.Description("Execution model default; empty clears it.")),
		mcpsdk.WithString("effort", mcpsdk.Description("Execution effort default; empty clears it.")),
		mcpsdk.WithString("planning_backend", mcpsdk.Description("Planning (decompose) backend default; empty clears it.")),
		mcpsdk.WithString("planning_model", mcpsdk.Description("Planning (decompose) model default; empty clears it.")),
	)
	return mcpsdk.NewTool("automation_set_llm", opts...)
}

func (s *Server) handleAutomationSetLLM(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.automationLLMCall(req, "set_llm", automationLLMFields)
}

func (s *Server) toolAutomationSetVerificationLLM() mcpsdk.Tool {
	opts := append(automationLLMBase("Set the Automation-level verification defaults used by members that name none of their own. Supply at least one of backend, model, effort: an omitted field is left unchanged and an empty string clears it. Allowed while the Automation is draft, active, paused or blocked."),
		mcpsdk.WithString("backend", mcpsdk.Description("Verification backend default; empty clears it.")),
		mcpsdk.WithString("model", mcpsdk.Description("Verification model default; empty clears it.")),
		mcpsdk.WithString("effort", mcpsdk.Description("Verification effort default; empty clears it.")),
	)
	return mcpsdk.NewTool("automation_set_verification_llm", opts...)
}

func (s *Server) handleAutomationSetVerificationLLM(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.automationLLMCall(req, "set_verification_llm", automationVerificationLLMFields)
}

// ── automation_set_decision_assistant ───────────────────────────────────

const automationSetDecisionAssistantDesc = "Set or clear the local-model decision assistant of an Automation, or of one member (member_id). " +
	"The assistant is off by default. When set, a configured backend and model picks among already-allowed options at the enabled hook points " +
	"(empty_edge, skip_member, insert_member, budget_breach); it never allows an action that is otherwise disallowed, and below a hook's confidence threshold it escalates to the operator. " +
	"mode shadow (the default) only records what the assistant would have done; live acts on a decision at or above the threshold. " +
	"The fields replace the whole setting; clear=true, or an empty backend, turns the assistant off. A member's setting wins over the Automation's. " +
	"Allowed while the Automation is draft, active, paused or blocked. automation_get and the status rows return the setting as decision_assistant."

func (s *Server) toolAutomationSetDecisionAssistant() mcpsdk.Tool {
	opts := append(automationLLMBase(automationSetDecisionAssistantDesc),
		mcpsdk.WithString("member_id", mcpsdk.Description("Set the override of this member instead of the Automation's own setting.")),
		mcpsdk.WithString("backend", mcpsdk.Description("LLM backend of the assistant (an LLM registry name); empty turns the assistant off.")),
		mcpsdk.WithString("model", mcpsdk.Description("Model of the assistant; required with a backend.")),
		mcpsdk.WithString("mode", mcpsdk.Description("shadow (the default; record only) or live (act on decisions at or above the threshold).")),
		mcpsdk.WithArray("hooks", mcpsdk.WithStringItems(), mcpsdk.Description("Hook points the assistant is enabled for: empty_edge, skip_member, insert_member, budget_breach. None enabled means no hook consults it.")),
		mcpsdk.WithObject("thresholds", mcpsdk.Description("Per-hook confidence threshold overrides, hook name to a number in (0,1]. A hook without one uses its conservative default (0.9, budget_breach 0.95).")),
		mcpsdk.WithBoolean("clear", mcpsdk.Description("Turn the assistant off. Cannot be combined with the setting fields.")),
	)
	return mcpsdk.NewTool("automation_set_decision_assistant", opts...)
}

func (s *Server) handleAutomationSetDecisionAssistant(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("automation_id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("error: automation_id is required"), nil
	}
	args := req.GetArguments()
	body := map[string]any{}
	// backend and model are forwarded when present, so an empty backend clears.
	for _, k := range []string{"backend", "model", "mode", "member_id"} {
		v, present := args[k]
		if !present || v == nil {
			continue
		}
		str, ok := v.(string)
		if !ok {
			return mcpsdk.NewToolResultText("error: " + k + " must be a string"), nil
		}
		if k == "backend" || k == "model" || str != "" {
			body[k] = str
		}
	}
	if v, present := args["hooks"]; present && v != nil {
		var hooks []string
		switch list := v.(type) {
		case []string:
			hooks = list
		case []any:
			for _, e := range list {
				h, isStr := e.(string)
				if !isStr {
					return mcpsdk.NewToolResultText("error: hooks must be an array of strings"), nil
				}
				hooks = append(hooks, h)
			}
		default:
			return mcpsdk.NewToolResultText("error: hooks must be an array of strings"), nil
		}
		if len(hooks) > 0 {
			body["hooks"] = hooks
		}
	}
	if v, present := args["thresholds"]; present && v != nil {
		obj, ok := v.(map[string]any)
		if !ok {
			return mcpsdk.NewToolResultText("error: thresholds must be an object of hook name to number"), nil
		}
		thresholds := make(map[string]float64, len(obj))
		for hook, tv := range obj {
			n, isNum := controlNumber(tv)
			if !isNum {
				return mcpsdk.NewToolResultText("error: threshold for " + hook + " must be a number"), nil
			}
			thresholds[hook] = n
		}
		if len(thresholds) > 0 {
			body["thresholds"] = thresholds
		}
	}
	if v, present := args["clear"]; present && v != nil {
		b, ok := v.(bool)
		if !ok {
			return mcpsdk.NewToolResultText("error: clear must be true or false"), nil
		}
		if b {
			body["clear"] = true
		}
	}
	if v, ok := controlVersion(args["expected_version"]); ok && v != 0 {
		body["expected_version"] = v
	}
	optionalString(req, body, "actor")
	optionalString(req, body, "reason")
	data, err := s.proxyJSON(http.MethodPost, "/api/automations/"+id+"/set_decision_assistant", body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── automation_set_budget ───────────────────────────────────────────────

const automationSetBudgetDesc = "Set, replace or clear the budget policy of an Automation, or the override of one member (member_id), as an operator control operation. " +
	"The policy is a spend ceiling (ceiling_type and ceiling_value) over a window, with a pacing mode and an on_breach rule; the fields replace the whole policy, and clear=true removes it instead and cannot be combined with them. " +
	"On a breach the Automation pauses and resumes by itself at the window rollover only when the reset time is known (a window the daemon defines itself, anchored at reset_time, reset_weekday, reset_day and reset_zone or at the calendar default, or a reset time parsed from a rate-limit message). When the reset time is unknown it goes to blocked and waits for the operator to resume it. An alert is raised either way. " +
	"allocation_estimate is optional and off by default: it shows the spend as an estimated share of the plan allocation, is always labelled estimated, and never pauses or blocks. " +
	"peer_window lists the peers whose spend counts toward the ceiling; another daemon's usage counts only for peers named there, it is off by default, and a shared account is not confirmed automatically. " +
	"estimated_cost_usd (member only) declares the expected cost of the member's Automaton; on its own it changes only the estimate and 0 removes it. " +
	"The Automation's status is not changed by this call. automation_get and automation_list return the computed budget view."

func (s *Server) toolAutomationSetBudget() mcpsdk.Tool {
	opts := append(automationControlBase(automationSetBudgetDesc),
		mcpsdk.WithString("member_id", mcpsdk.Description("Set the override of this member instead of the Automation's own policy.")),
		mcpsdk.WithString("ceiling_type", mcpsdk.Description("What the ceiling measures: dollar (a spend ceiling) or allocation_pct (an estimate only, never pauses or blocks).")),
		mcpsdk.WithNumber("ceiling_value", mcpsdk.Description("The ceiling, a positive number in the unit of ceiling_type.")),
		mcpsdk.WithString("window", mcpsdk.Description("The window the ceiling applies to: automation (the whole run), day, week or month.")),
		mcpsdk.WithString("pacing", mcpsdk.Description("Pacing within the window: none (the default) or even.")),
		mcpsdk.WithString("on_breach", mcpsdk.Description("What happens when the ceiling is reached: pause (the default) or block.")),
		mcpsdk.WithString("backend", mcpsdk.Description("The LLM backend whose spend is measured; empty measures the Automation's members.")),
		mcpsdk.WithBoolean("allocation_estimate", mcpsdk.Description("Show an estimated share of the plan allocation. An estimate only; it never pauses or blocks.")),
		mcpsdk.WithArray("peer_window", mcpsdk.WithStringItems(), mcpsdk.Description("Peers whose spend counts toward the ceiling. Off by default; a shared account is not confirmed automatically.")),
		mcpsdk.WithString("reset_time", mcpsdk.Description("Time of day the day, week or month window resets, such as 06:00, when the operator knows it.")),
		mcpsdk.WithString("reset_weekday", mcpsdk.Description("Weekday the window resets, monday to sunday; week window only.")),
		mcpsdk.WithNumber("reset_day", mcpsdk.Description("Day of the month the window resets, 1 to 28; month window only.")),
		mcpsdk.WithString("reset_zone", mcpsdk.Description("IANA time zone of the reset, such as America/New_York; day, week or month window only.")),
		mcpsdk.WithNumber("estimated_cost_usd", mcpsdk.Description("Member only: the expected cost in dollars of the member's Automaton. 0 removes the estimate.")),
		mcpsdk.WithBoolean("clear", mcpsdk.Description("Remove the policy instead of setting one. Cannot be combined with the policy fields.")),
	)
	return mcpsdk.NewTool("automation_set_budget", opts...)
}

// budgetStringArgs, budgetNumberArgs and budgetBoolArgs name the set_budget
// arguments forwarded as each JSON type.
var (
	budgetStringArgs = []string{"member_id", "ceiling_type", "window", "pacing", "on_breach", "backend", "reset_time", "reset_weekday", "reset_zone"}
	budgetNumberArgs = []string{"ceiling_value", "reset_day", "estimated_cost_usd"}
	budgetBoolArgs   = []string{"allocation_estimate", "clear"}
)

func (s *Server) handleAutomationSetBudget(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	extra := map[string]any{}
	for _, k := range budgetStringArgs {
		v, present := args[k]
		if !present || v == nil {
			continue
		}
		str, ok := v.(string)
		if !ok {
			return mcpsdk.NewToolResultText("error: " + k + " must be a string"), nil
		}
		if str != "" {
			extra[k] = str
		}
	}
	for _, k := range budgetNumberArgs {
		v, present := args[k]
		if !present || v == nil {
			continue
		}
		n, ok := controlNumber(v)
		if !ok {
			return mcpsdk.NewToolResultText("error: " + k + " must be a number"), nil
		}
		// Zero is a real value only for the estimate, where it removes it.
		if n != 0 || k == "estimated_cost_usd" {
			extra[k] = n
		}
	}
	for _, k := range budgetBoolArgs {
		v, present := args[k]
		if !present || v == nil {
			continue
		}
		b, ok := v.(bool)
		if !ok {
			return mcpsdk.NewToolResultText("error: " + k + " must be true or false"), nil
		}
		if b {
			extra[k] = true
		}
	}
	if v, present := args["peer_window"]; present && v != nil {
		list, ok := v.([]any)
		if !ok {
			if strs, isStrs := v.([]string); isStrs {
				for _, p := range strs {
					list = append(list, p)
				}
				ok = true
			}
		}
		if !ok {
			return mcpsdk.NewToolResultText("error: peer_window must be an array of strings"), nil
		}
		peers := make([]string, 0, len(list))
		for _, e := range list {
			p, isStr := e.(string)
			if !isStr {
				return mcpsdk.NewToolResultText("error: peer_window must be an array of strings"), nil
			}
			peers = append(peers, p)
		}
		if len(peers) > 0 {
			extra["peer_window"] = peers
		}
	}
	return s.automationControlCall(req, "set_budget", extra)
}

// controlNumber reads a JSON number out of decoded tool arguments.
func controlNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// AutomationToolNames returns the name of every Automation MCP tool, plus
// autonomous_status, which carries the automations[] array. It reads
// the names from the tool definitions, so it lists what is registered.
func AutomationToolNames() []string {
	s := &Server{}
	tools := []mcpsdk.Tool{
		s.toolAutomationList(), s.toolAutomationGet(), s.toolAutomationMergedMembers(), s.toolAutomationCreate(),
		s.toolAutomationUpdate(), s.toolAutomationDelete(), s.toolAutomationActivate(),
		s.toolAutomationPause(), s.toolAutomationResume(), s.toolAutomationCancel(),
		s.toolAutomationSkipMember(), s.toolAutomationInsertMember(), s.toolAutomationRemoveMember(),
		s.toolAutomationReorderEdges(), s.toolAutomationApproveInsert(), s.toolAutomationSetLLM(),
		s.toolAutomationSetVerificationLLM(), s.toolAutomationSetBudget(), s.toolAutomationSetDecisionAssistant(), s.toolAutomationImportGraph(),
		// The automations[] array on the status response.
		s.toolAutonomousStatus(),
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}
