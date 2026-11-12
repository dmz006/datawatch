package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ----- per-unit verifier policy overrides ------------------------------------

const (
	verifierPolicyModeParamDesc     = "Verifier policy mode: fixed (first eligible backend only), prefer-free (first backend with a free slot), balance (lowest relative load), gpu-only (local backends only) or claude-only (Claude backends only). Precedence: task > Automaton > global autonomous verifier_policy. Empty string = clear (inherit); omit to leave unchanged. Unknown modes are rejected."
	verifierPolicyBackendsParamDesc = "Ordered list of eligible verifier backends (LLM registry names). Empty list = clear (inherit); omit to leave unchanged. Empty entries are rejected."
)

func (s *Server) toolAutonomousPRDSetVerifierPolicy() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_verifier_policy",
		mcpsdk.WithDescription("Override which verifier backend runs the verifications of a whole Automaton: the policy mode and the ordered backend list (a task-level override still wins). Only the fields you pass change; an empty string or empty list clears that field. Accepted only while the Automaton is in needs_review, revisions_asked or cancelled."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("mode", mcpsdk.Description(verifierPolicyModeParamDesc)),
		mcpsdk.WithArray("backends", mcpsdk.Description(verifierPolicyBackendsParamDesc), mcpsdk.WithStringItems()),
		mcpsdk.WithString("actor", mcpsdk.Description("Who made the change (default operator)")),
	)
}
func (s *Server) handleAutonomousPRDSetVerifierPolicy(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.proxyVerifierPolicy(req, "set_verifier_policy", "")
}

func (s *Server) toolAutonomousPRDSetTaskVerifierPolicy() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_task_verifier_policy",
		mcpsdk.WithDescription("Override which verifier backend runs the verifications of one task: the policy mode and the ordered backend list (the most specific level). Only the fields you pass change; an empty string or empty list clears that field. Accepted while the Automaton's status is in autonomous.task_override_statuses."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID")),
		mcpsdk.WithString("mode", mcpsdk.Description(verifierPolicyModeParamDesc)),
		mcpsdk.WithArray("backends", mcpsdk.Description(verifierPolicyBackendsParamDesc), mcpsdk.WithStringItems()),
		mcpsdk.WithString("actor", mcpsdk.Description("Who made the change (default operator)")),
	)
}
func (s *Server) handleAutonomousPRDSetTaskVerifierPolicy(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.proxyVerifierPolicy(req, "set_task_verifier_policy", "task_id")
}

// proxyVerifierPolicy POSTs a verifier policy override to the REST action.
// Only the keys the caller actually supplied are forwarded, so an absent key
// leaves the field unchanged and ""/[] clears it.
func (s *Server) proxyVerifierPolicy(req mcpsdk.CallToolRequest, action, unitKey string) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+req.GetString("id", "")+"/"+action, verifierPolicyBody(req, unitKey))
	return llmSetterResult(out, err)
}

func verifierPolicyBody(req mcpsdk.CallToolRequest, unitKey string) []byte {
	args := req.GetArguments()
	body := map[string]any{"actor": req.GetString("actor", "operator")}
	if body["actor"] == "" {
		body["actor"] = "operator"
	}
	if unitKey != "" {
		body[unitKey] = req.GetString(unitKey, "")
	}
	if _, ok := args["mode"]; ok {
		body["mode"] = req.GetString("mode", "")
	}
	if _, ok := args["backends"]; ok {
		body["backends"] = req.GetStringSlice("backends", []string{})
	}
	out, _ := json.Marshal(body)
	return out
}

// verifierPolicyConfigTools adds the autonomous.verifier_policy.* parameters
// to the autonomous_config_set tool.
func verifierPolicyConfigTools() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithString("verifier_policy_mode", mcpsdk.Description("Global verifier policy mode (autonomous.verifier_policy.mode): fixed (the default: first eligible backend only), prefer-free (first backend with a free slot), balance (lowest relative load), gpu-only (local backends only) or claude-only (Claude backends only). Empty = fixed. Unknown modes are rejected. Takes effect on the next verification.")),
		mcpsdk.WithArray("verifier_policy_backends", mcpsdk.Description("Ordered list of eligible verifier backends (autonomous.verifier_policy.backends). Empty list = fall back to verification_backends, then verification_backend. Empty entries are rejected. The value replaces the whole list."), mcpsdk.WithStringItems()),
		mcpsdk.WithNumber("verifier_policy_claude_session_cap", mcpsdk.Description("Most Claude verifier sessions started per window (autonomous.verifier_policy.claude_session_cap); 0 = unlimited. Negative values are rejected.")),
		mcpsdk.WithNumber("verifier_policy_claude_cap_window_minutes", mcpsdk.Description("Window length in minutes for verifier_policy_claude_session_cap (autonomous.verifier_policy.claude_cap_window_minutes); 0 = default 60. Negative values are rejected.")),
		mcpsdk.WithBoolean("verifier_policy_force_stronger_on_retry", mcpsdk.Description("Send a retried verification to the stronger verifier (autonomous.verifier_policy.force_stronger_on_retry).")),
		mcpsdk.WithBoolean("verifier_policy_final_gate_stronger", mcpsdk.Description("Send the final verification gate to the stronger verifier (autonomous.verifier_policy.final_gate_stronger).")),
		mcpsdk.WithString("verifier_policy_stronger_backend", mcpsdk.Description("Stronger verifier backend (autonomous.verifier_policy.stronger_backend); empty = claude-code.")),
		mcpsdk.WithString("verifier_policy_stronger_model", mcpsdk.Description("Stronger verifier model (autonomous.verifier_policy.stronger_model); empty = the default Sonnet model.")),
	}
}

// verifierPolicyConfigBody collects the verifier_policy_* tool arguments the
// caller supplied into the verifier_policy object of the config body. Every
// key is presence-aware so an explicit 0, false, "" or [] is forwarded and
// the REST side is the one place that validates.
func verifierPolicyConfigBody(req mcpsdk.CallToolRequest) map[string]any {
	args := req.GetArguments()
	vp := map[string]any{}
	if _, ok := args["verifier_policy_mode"]; ok {
		vp["mode"] = req.GetString("verifier_policy_mode", "")
	}
	if _, ok := args["verifier_policy_backends"]; ok {
		vp["backends"] = req.GetStringSlice("verifier_policy_backends", []string{})
	}
	for _, k := range []string{"claude_session_cap", "claude_cap_window_minutes"} {
		if _, ok := args["verifier_policy_"+k]; ok {
			vp[k] = req.GetFloat("verifier_policy_"+k, 0)
		}
	}
	for _, k := range []string{"force_stronger_on_retry", "final_gate_stronger"} {
		if _, ok := args["verifier_policy_"+k]; ok {
			vp[k] = req.GetBool("verifier_policy_"+k, false)
		}
	}
	for _, k := range []string{"stronger_backend", "stronger_model"} {
		if _, ok := args["verifier_policy_"+k]; ok {
			vp[k] = req.GetString("verifier_policy_"+k, "")
		}
	}
	return vp
}
