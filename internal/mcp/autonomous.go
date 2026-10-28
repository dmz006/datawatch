// BL24+BL25 (v3.10.0) — MCP-tool parity for autonomous PRD endpoints.
// Each tool wraps the corresponding REST endpoint via the in-process
// HTTP loopback (proxyGet/proxyJSON in sx_parity.go).

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ----- autonomous_status -----------------------------------------------------

func (s *Server) toolAutonomousStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_status",
		mcpsdk.WithDescription("BL24 — return autonomous loop status (running, active PRDs, queued/running tasks)."),
	)
}
func (s *Server) handleAutonomousStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/status", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_config_get / set -------------------------------------------

func (s *Server) toolAutonomousConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_config_get",
		mcpsdk.WithDescription("Read the current autonomous (Automata) config: poll interval, max_concurrent_tasks (daemon-wide default for how many independent tasks of one Automaton run at once; 1 = sequential), max_engines_per_node (local-model sessions allowed per compute node; default 1), backends, retries, capacity limits, guardrail lists and timeouts. "+
			"Automaton-level (per_prd_guardrails) guardrails accept \"rules-review\" and \"ci-watch\"; both are off unless listed. \"ci-watch\" makes an automaton, when it completes, wait (up to ci_watch_timeout_seconds, default 1500) for the CI runs of the commit it pushed; if any run failed, the configured LLM judges the failure against the project's rules file. It is skipped when nothing was pushed or the origin remote is not GitHub. "+
			"block_on_file_drift (default false) controls foreign-file drift: when false a task whose commit swept in files that were already uncommitted before it started is only flagged (warn-only); when true that task is blocked instead. An automaton's own setting or its guardrail profile overrides it; worktree-mode automata are never checked."),
	)
}
func (s *Server) handleAutonomousConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/config", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_config_set",
		mcpsdk.WithDescription("Update the autonomous (Automata) config. Merges onto the current config, so a partial body (e.g. just enabled) only changes the fields given and leaves the rest untouched. "+
			"Automaton-level (per_prd_guardrails) guardrails accept \"rules-review\" and \"ci-watch\"; both are off unless listed. \"ci-watch\" makes an automaton, when it completes, wait (up to ci_watch_timeout_seconds, default 1500) for the CI runs of the commit it pushed; if any run failed, the configured LLM judges the failure against the project's rules file. It is skipped when nothing was pushed or the origin remote is not GitHub. "+
			"task_override_statuses sets which PRD statuses accept task- and story-level LLM overrides; unknown status names are rejected."),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Toggle autonomous loop on/off")),
		mcpsdk.WithNumber("poll_interval_seconds", mcpsdk.Description("Background loop tick (default 30)")),
		mcpsdk.WithNumber("max_concurrent_tasks", mcpsdk.Description("Daemon-wide default for how many independent tasks of one Automaton run at once. An Automaton's own setting overrides it. 1 = sequential (the default); values below 1 are ignored.")),
		mcpsdk.WithNumber("max_parallel_tasks", mcpsdk.Description("DEPRECATED — accepted and ignored, has no effect; use max_concurrent_tasks. Will be removed in a future release.")),
		mcpsdk.WithNumber("max_engines_per_node", mcpsdk.Description("Number of local-model sessions allowed per compute node. Defaults to 1; values below 1 are clamped to 1.")),
		mcpsdk.WithString("planning_backend", mcpsdk.Description("LLM backend for the Automata planning call (empty = inherit); alias: decomposition_backend")),
		mcpsdk.WithString("verification_backend", mcpsdk.Description("BL25 verifier backend (empty = inherit; set to a different backend for cross-backend independence)")),
		mcpsdk.WithArray("verification_backends", mcpsdk.Description("v8.36.9 — ordered list of LLM registry names to load-balance verifier capacity across (e.g. [\"ollama-datawatch\", \"ollama-johnnyjohnny\"]); tries each in order, uses whichever has free node capacity right now. Empty = single-backend behavior via verification_backend above.")),
		mcpsdk.WithArray("task_override_statuses", mcpsdk.Description("PRD statuses that accept task/story-level LLM overrides; names: draft, planning, needs_review, approved, revisions_asked, active, running, completed, rejected, cancelled, archived, blocked, failed, paused; empty array restores the defaults (needs_review, revisions_asked, cancelled, blocked)")),
		mcpsdk.WithString("planning_effort", mcpsdk.Description("BL41 effort hint for planning; alias: decomposition_effort")),
		mcpsdk.WithString("verification_effort", mcpsdk.Description("BL41 effort hint for verifier")),
		mcpsdk.WithNumber("auto_fix_retries", mcpsdk.Description("Retries on verifier failure (default 1)")),
		mcpsdk.WithBoolean("security_scan", mcpsdk.Description("Run nightwire-port security scan before commit")),
		mcpsdk.WithNumber("verifier_diff_max_bytes", mcpsdk.Description("BL366 — max bytes of git diff injected into verifier prompt (0 = 8192 default)")),
		mcpsdk.WithBoolean("quality_gates_enabled", mcpsdk.Description("BL367 — enable quality gates by default for all PRDs")),
		mcpsdk.WithString("quality_gates_test_command", mcpsdk.Description("BL367 — default test command for quality gates (e.g. 'go test ./...')")),
		mcpsdk.WithNumber("quality_gates_timeout", mcpsdk.Description("BL367 — default gate timeout in seconds (0 = no limit)")),
		mcpsdk.WithBoolean("quality_gates_block_on_regression", mcpsdk.Description("BL367 — block task on regression by default")),
		mcpsdk.WithBoolean("injection_guard", mcpsdk.Description("BL369 — scan user-supplied PRD/task specs for prompt injection phrases (warn only by default)")),
		mcpsdk.WithBoolean("block_on_injection", mcpsdk.Description("BL369 — reject PRD/task create or edit when injection phrases are detected (requires injection_guard:true)")),
		mcpsdk.WithNumber("planning_timeout_seconds", mcpsdk.Description("Override effort-scaled decompose timeout (0 = default: 5 min normal, 15 min high/max, 2 min quick/low)")),
		mcpsdk.WithNumber("verification_timeout_seconds", mcpsdk.Description("Bound for one verifier/guardrail judgment session in seconds (default 1800 = 30m; use 0 or a negative value to mean \"leave unset\"); local models need more time than the old hard-coded 10m")),
		mcpsdk.WithNumber("session_idle_timeout_seconds", mcpsdk.Description("Bound for one autonomous worker/verifier session in seconds — a session sitting in waiting_input/idle with no live child process longer than this is treated as an infra failure and retried through the auto_fix_retries budget (default 900 = 15m; 0 or negative means use the default)")),
		mcpsdk.WithBoolean("capacity_enabled", mcpsdk.Description("Capacity-aware admission: tasks wait for a free host/node/LLM slot instead of failing (default true)")),
		mcpsdk.WithNumber("capacity_wait_timeout_seconds", mcpsdk.Description("Maximum seconds a task waits for capacity (default 14400)")),
		mcpsdk.WithNumber("ci_watch_timeout_seconds", mcpsdk.Description("Maximum seconds the ci-watch completion guardrail waits for the CI runs of an automaton's own pushed commit (default 1500). Runs still unfinished at the deadline leave the automaton complete with ci_watch.status \"pending\".")),
		mcpsdk.WithArray("per_prd_guardrails", mcpsdk.Description("Daemon-wide default guardrails run once when an automaton completes. Accepts \"rules-review\" (judge the automaton's diff against the project's rules file) and \"ci-watch\" (wait for and judge the CI runs of the commit it pushed). Empty list (the default) turns both off; an automaton's own list or guardrail profile overrides this. The value replaces the whole list, so read it with autonomous_config_get first and include the entries you want to keep."), mcpsdk.WithStringItems()),
		mcpsdk.WithBoolean("continue_on_story_failure", mcpsdk.Description("Daemon-wide default: false (halt) stops a PRD as soon as any story fails instead of continuing into later, independent stories; true restores the old continue-regardless behavior. Per-PRD override takes precedence.")),
		mcpsdk.WithBoolean("block_on_file_drift", mcpsdk.Description("Daemon-wide default for foreign-file-drift enforcement. false (the default) only flags a task whose own commit swept in files that were already uncommitted before it started and were not in its planned files (warn-only, recorded as task foreign_file_drift and a file_drift_detected decision); true blocks that task instead. An automaton's own setting or its guardrail profile overrides this; worktree-mode automata are never checked.")),
		mcpsdk.WithBoolean("default_auto_pr", mcpsdk.Description("BL407 Phase 5 — daemon-wide default for a new PRD's Git.AutoPR (push+open a PR on completion). Applied once at PRD creation; the per-PRD autonomous_prd_set_git tool overrides it after that. Default false.")),
	)
}
func (s *Server) handleAutonomousConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{}
	for _, k := range []string{"enabled", "security_scan"} {
		if v := req.GetBool(k, false); v {
			body[k] = v
		}
	}
	for _, k := range []string{"poll_interval_seconds", "max_parallel_tasks", "auto_fix_retries"} {
		if v := req.GetFloat(k, 0); v != 0 {
			body[k] = int(v)
		}
	}
	if v := req.GetFloat("max_concurrent_tasks", -1); v >= 1 {
		body["max_concurrent_tasks"] = int(v)
	}
	// Presence-aware and forwarded as given (0 included): the REST write
	// clamps anything below 1 to 1, so the clamp lives in one place.
	if args := req.GetArguments(); args != nil {
		if _, ok := args["max_engines_per_node"]; ok {
			body["max_engines_per_node"] = int(req.GetFloat("max_engines_per_node", 0))
		}
	}
	// BL366 — verifier diff max bytes.
	if v := req.GetFloat("verifier_diff_max_bytes", -1); v >= 0 {
		body["verifier_diff_max_bytes"] = int(v)
	}
	// BL304: accept new planning_* params; also check legacy decomposition_* for back-compat.
	for _, pair := range [][2]string{
		{"planning_backend", "decomposition_backend"},
		{"planning_effort", "decomposition_effort"},
	} {
		newKey, oldKey := pair[0], pair[1]
		if v := req.GetString(newKey, ""); v != "" {
			body[newKey] = v
		} else if v := req.GetString(oldKey, ""); v != "" {
			body[newKey] = v
		}
	}
	for _, k := range []string{"verification_backend", "verification_effort"} {
		if v := req.GetString(k, ""); v != "" {
			body[k] = v
		}
	}
	// B124 — presence-aware, not len()>0: an operator explicitly clearing
	// this list back to empty (e.g. to stop it silently overriding
	// verification_backend with an ask-compatible fallback) must be able
	// to express that. len(arr)>0 dropped the key from the PUT body
	// entirely for an empty array, so "clear it" was indistinguishable
	// from "didn't mention it" and never took effect — found live
	// debugging why configuring verification_backend to a tool-capable
	// backend still silently used an ollama candidate from this list.
	if args := req.GetArguments(); args != nil {
		if _, ok := args["verification_backends"]; ok {
			body["verification_backends"] = req.GetStringSlice("verification_backends", nil)
		}
	}
	// v9.5.5 — presence-aware: an explicit [] reverts to the default list,
	// an absent key leaves the stored list untouched.
	if args := req.GetArguments(); args != nil {
		if _, ok := args["task_override_statuses"]; ok {
			body["task_override_statuses"] = req.GetStringSlice("task_override_statuses", []string{})
		}
	}
	// BL367 — default quality gates nested under default_quality_gates.
	qg := map[string]any{}
	if v := req.GetBool("quality_gates_enabled", false); v {
		qg["enabled"] = v
	}
	if v := req.GetString("quality_gates_test_command", ""); v != "" {
		qg["test_command"] = v
	}
	if v := req.GetFloat("quality_gates_timeout", -1); v >= 0 {
		qg["timeout"] = int(v)
	}
	if v := req.GetBool("quality_gates_block_on_regression", false); v {
		qg["block_on_regression"] = v
	}
	if len(qg) > 0 {
		body["default_quality_gates"] = qg
	}
	// BL369 — injection guard config.
	if v := req.GetBool("injection_guard", false); v {
		body["injection_guard"] = v
	}
	if v := req.GetBool("block_on_injection", false); v {
		body["block_on_injection"] = v
	}
	// v8.25.1 — configurable planning timeout.
	if v := req.GetFloat("planning_timeout_seconds", -1); v >= 0 {
		body["planning_timeout_seconds"] = int(v)
	}
	if v := req.GetFloat("verification_timeout_seconds", -1); v >= 0 {
		body["verification_timeout_seconds"] = int(v)
	}
	if v := req.GetFloat("session_idle_timeout_seconds", -1); v >= 0 {
		body["session_idle_timeout_seconds"] = int(v)
	}
	if args := req.GetArguments(); args != nil {
		if _, ok := args["capacity_enabled"]; ok {
			body["capacity_enabled"] = req.GetBool("capacity_enabled", true)
		}
	}
	if v := req.GetFloat("capacity_wait_timeout_seconds", -1); v >= 0 {
		body["capacity_wait_timeout_seconds"] = int(v)
	}
	if v := req.GetFloat("ci_watch_timeout_seconds", -1); v >= 0 {
		body["ci_watch_timeout_seconds"] = int(v)
	}
	if args := req.GetArguments(); args != nil {
		// Presence-aware like verification_backends: an explicit empty
		// list must be able to turn every completion guardrail back off.
		if _, ok := args["per_prd_guardrails"]; ok {
			body["per_prd_guardrails"] = req.GetStringSlice("per_prd_guardrails", []string{})
		}
		if _, ok := args["continue_on_story_failure"]; ok {
			body["continue_on_story_failure"] = req.GetBool("continue_on_story_failure", false)
		}
		if _, ok := args["default_auto_pr"]; ok {
			body["default_auto_pr"] = req.GetBool("default_auto_pr", false)
		}
		if _, ok := args["block_on_file_drift"]; ok {
			body["block_on_file_drift"] = req.GetBool("block_on_file_drift", false)
		}
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/autonomous/config", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prds list / create / get / decompose / run / cancel --------

func (s *Server) toolAutonomousPRDList() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_list",
		mcpsdk.WithDescription("BL24 — list all PRDs, newest first."),
	)
}
func (s *Server) handleAutonomousPRDList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/prds", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_create",
		mcpsdk.WithDescription("BL24 — create a draft PRD from a feature description. Call autonomous_prd_decompose next to populate stories+tasks."),
		mcpsdk.WithString("spec", mcpsdk.Required(), mcpsdk.Description("Free-text feature description")),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Project directory the PRD targets (defaults to operator default)")),
		mcpsdk.WithString("backend", mcpsdk.Description("LLM backend override")),
		mcpsdk.WithString("model", mcpsdk.Description("LLM model override (e.g. ollama/qwen3.8:27b, opencode/big-pickle). Applied to every task session.")),
		mcpsdk.WithString("effort", mcpsdk.Description("BL41 effort hint (low/medium/high/max)")),
	)
}
func (s *Server) handleAutonomousPRDCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"spec":        req.GetString("spec", ""),
		"project_dir": req.GetString("project_dir", ""),
		"backend":     req.GetString("backend", ""),
		"model":       req.GetString("model", ""),
		"effort":      req.GetString("effort", ""),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_get",
		mcpsdk.WithDescription("Fetch one automaton. The detail argument picks how much comes back; the default is summary. "+
			"summary: ids, titles, statuses and counts for the automaton, its stories and its tasks. "+
			"tasks: everything in summary, plus each task's status, error and verification summary. "+
			"full: everything, including specs and logs. "+
			"When the ci-watch completion guardrail ran, the response includes ci_watch {status, sha, repo, runs[], summary, issues, checked_at} at every detail level. "+
			"status: skipped = nothing to watch (HEAD not pushed yet, origin not GitHub, not a git repository, or the gh CLI is missing; summary says which); "+
			"pass = every CI run for the pushed commit succeeded; "+
			"pending = runs were still unfinished when ci_watch_timeout_seconds elapsed, so CI is unconfirmed; "+
			"failed_policy = a run failed and the project's own rules say an operator must decide; nothing is auto-fixed; "+
			"failed_content = a run failed with a fixable finding; a corrective task may have been appended automatically; "+
			"failed_infra = runs were cancelled, timed out or never started and the infra retry budget is spent; "+
			"warn = the judge flagged a non-blocking concern; error = the judge itself could not run. "+
			"runs[] gives each CI run's id, name, status, conclusion and url; issues lists the specific problems found. "+
			"The automaton's own status is blocked after failed_policy, failed_infra, or failed_content with no corrective task; completed otherwise. "+
			"To resume a blocked automaton: commit your decision (fix, upgrade, or accepted-risk entry) to the repo first, "+
			"then autonomous_prd_reset_task with force=true on the task that pushed, then autonomous_prd_run; "+
			"ci-watch then watches the new commit's CI. If you finished the work by hand, leave it blocked as the record or autonomous_prd_cancel it. "+
			"Each task may carry foreign_file_drift (full detail only; summary and tasks omit it): a list of files the task's own commit swept in that were already uncommitted before the task started and were not in its planned files. "+
			"This is warn-only (the task still completes) unless block_on_file_drift is on for the automaton, its guardrail profile, or the daemon, in which case the task is blocked; worktree-mode automata are never checked. "+
			"Each finding is also recorded in the decision log with kind file_drift_detected. "+
			"Tasks that are actively running (status in_progress or verifying) additionally carry progress {last_event_at, assistant_turns, tool_calls, last_tool_name, last_tool_args, last_summary, tokens} — what the worker has been doing, parsed from its own session log — and, when the worker appears to re-issue the same tool call on consecutive polls, a looping_warning string. "+
			"The absence of the progress key means the task's worker is not currently running (it is stripped the moment a task leaves the active statuses)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("detail", mcpsdk.Enum("summary", "tasks", "full"),
			mcpsdk.Description("Response level: summary (default), tasks, or full. See the tool description for what each level returns.")),
	)
}
func (s *Server) handleAutonomousPRDGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	detail := req.GetString("detail", "summary")
	switch detail {
	case "summary", "tasks", "full":
	default:
		return mcpsdk.NewToolResultError(fmt.Sprintf("detail must be one of summary, tasks, full (got %q)", detail)), nil
	}
	out, err := s.proxyGet("/api/autonomous/prds/"+id, url.Values{"detail": {detail}})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDDecompose() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_decompose",
		mcpsdk.WithDescription("BL328 — start async LLM planning for a PRD. Returns 202 with task_id and stream_url for SSE progress. Poll /decompose/status or stream /decompose/stream to watch progress."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDDecompose(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/decompose", nil)
	if err != nil {
		return nil, err
	}
	// BL328: response is now 202 { task_id, stream_url }. Return it as-is
	// so callers can see the stream_url for SSE progress.
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDRun() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_run",
		mcpsdk.WithDescription("BL24 — kick the executor for a PRD (or restart on crash)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/run", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDCancel() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_cancel",
		mcpsdk.WithDescription("BL24 — cancel + archive a PRD. Kills the PRD's worker sessions; the response JSON includes id, status, killed_sessions (count) and killed_session_ids (array of the session IDs a kill was attempted on)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDCancel(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyJSON(http.MethodDelete, "/api/autonomous/prds/"+id, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDResetToDraft() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_reset_to_draft",
		mcpsdk.WithDescription("v8.20.1 — reset a cancelled PRD to draft so it can be reconfigured and re-decomposed."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDResetToDraft(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": "operator"})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/reset_to_draft", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- BL191 (v5.2.0) review/approve/reject/edit-task/instantiate ----------

func (s *Server) toolAutonomousPRDApprove() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_approve",
		mcpsdk.WithDescription("BL191 — approve a planned PRD; required before autonomous_prd_run."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("note", mcpsdk.Description("optional free-form note saved on the Decision row")),
	)
}
func (s *Server) handleAutonomousPRDApprove(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": "operator", "note": req.GetString("note", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/approve", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDReject() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_reject",
		mcpsdk.WithDescription("BL191 — reject a planned PRD; the plan stays for inspection but the loop won't run it."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("reason", mcpsdk.Description("optional rejection reason")),
	)
}
func (s *Server) handleAutonomousPRDReject(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": "operator", "reason": req.GetString("reason", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/reject", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDRequestRevision() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_request_revision",
		mcpsdk.WithDescription("BL191 — ask for a fresh planning run; status moves back to revisions_asked."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("note", mcpsdk.Description("what's wrong with the current plan")),
	)
}
func (s *Server) handleAutonomousPRDRequestRevision(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": "operator", "note": req.GetString("note", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/request_revision", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDEditTask() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_edit_task",
		mcpsdk.WithDescription("BL191 — rewrite a task's spec before approving (needs_review / revisions_asked) or after cancelling. BL421: also works on a blocked PRD for the failed/blocked task that halted it — edit, then autonomous_prd_reset_task to retry without re-decomposing. Completed tasks are immutable in every status."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID to rewrite")),
		mcpsdk.WithString("new_spec", mcpsdk.Required(), mcpsdk.Description("New spec text")),
	)
}
func (s *Server) handleAutonomousPRDEditTask(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"task_id":  req.GetString("task_id", ""),
		"new_spec": req.GetString("new_spec", ""),
		"actor":    "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/edit_task", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDEditStory() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_edit_story",
		mcpsdk.WithDescription("Rewrite a story's title/description before approving."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story ID to rewrite")),
		mcpsdk.WithString("new_title", mcpsdk.Description("New title (leave empty to keep unchanged)")),
		mcpsdk.WithString("new_description", mcpsdk.Description("New description (leave empty to keep unchanged)")),
	)
}
func (s *Server) handleAutonomousPRDEditStory(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"story_id":        req.GetString("story_id", ""),
		"new_title":       req.GetString("new_title", ""),
		"new_description": req.GetString("new_description", ""),
		"actor":           "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/edit_story", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- structural edits: add/remove a story or task without re-decomposing --

func (s *Server) toolAutonomousPRDAddStory() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_add_story",
		mcpsdk.WithDescription("Add a new, empty story to a PRD without re-running decompose. Only allowed pre-approval (needs_review/revisions_asked)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("title", mcpsdk.Required(), mcpsdk.Description("Story title")),
		mcpsdk.WithString("description", mcpsdk.Description("Story description")),
	)
}
func (s *Server) handleAutonomousPRDAddStory(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"title":       req.GetString("title", ""),
		"description": req.GetString("description", ""),
		"actor":       "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/add_story", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDRemoveStory() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_remove_story",
		mcpsdk.WithDescription("Delete a story (and its tasks) from a PRD without re-running decompose. Only allowed pre-approval (needs_review/revisions_asked)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story ID to remove")),
	)
}
func (s *Server) handleAutonomousPRDRemoveStory(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"story_id": req.GetString("story_id", ""),
		"actor":    "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/remove_story", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDAddTask() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_add_task",
		mcpsdk.WithDescription("Add a new task to an existing story without re-running decompose. Only allowed pre-approval (needs_review/revisions_asked)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story ID to add the task to")),
		mcpsdk.WithString("title", mcpsdk.Required(), mcpsdk.Description("Task title")),
		mcpsdk.WithString("spec", mcpsdk.Description("Task spec — concrete instructions for the worker session")),
		mcpsdk.WithArray("depends_on", mcpsdk.Description("Task IDs (within the same PRD) that must complete before this one becomes eligible. Each must already exist."), mcpsdk.WithStringItems()),
	)
}
func (s *Server) handleAutonomousPRDAddTask(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]any{
		"story_id":   req.GetString("story_id", ""),
		"title":      req.GetString("title", ""),
		"spec":       req.GetString("spec", ""),
		"actor":      "operator",
		"depends_on": req.GetStringSlice("depends_on", nil),
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/add_task", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDRemoveTask() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_remove_task",
		mcpsdk.WithDescription("Delete one task from a story without re-running decompose (distinct from cancel_task, which stops a running/queued task without removing it). Only allowed pre-approval (needs_review/revisions_asked)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story ID the task belongs to")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID to remove")),
	)
}
func (s *Server) handleAutonomousPRDRemoveTask(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"story_id": req.GetString("story_id", ""),
		"task_id":  req.GetString("task_id", ""),
		"actor":    "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/remove_task", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDResetTask() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_reset_task",
		mcpsdk.WithDescription("v8.23.0 — reset a failed/blocked task to pending so the autonomous loop retries it. BL382: pass force=true to requeue a completed or cancelled task. BL451: force=true on an already-pending task in a terminal PRD reopens the PRD to running."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID to reset")),
		mcpsdk.WithBoolean("force", mcpsdk.Description("Set true to requeue completed or cancelled tasks (not just failed/blocked)")),
		mcpsdk.WithBoolean("clean_restart", mcpsdk.Description("Set true to start the next attempt from the pre-task tree: the preserved work of earlier attempts is not restored for that one attempt. Default false (restore stays on)")),
	)
}
func (s *Server) handleAutonomousPRDResetTask(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]any{
		"task_id":       req.GetString("task_id", ""),
		"actor":         "operator",
		"force":         req.GetBool("force", false),
		"clean_restart": req.GetBool("clean_restart", false),
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/reset_task", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDRepairDependsOn() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_repair_depends_on",
		mcpsdk.WithDescription("v8.36.6 — resolve DependsOn entries in an existing PRD: entries may be given as real story/task IDs, sibling story/task titles, or positional codes (e.g. 's1-t2' = the 2nd task in story 1; a plain number means the Nth story, 1-based). Full-resolution-or-nothing: if any entry is unresolvable/unknown or the resolved plan has a dependency cycle, the call is rejected, the error names the exact bad entry (or the cycle), and nothing is partially persisted. A no-op if the PRD is already resolved."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDRepairDependsOn(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": "operator"})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/repair_depends_on", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDCancelStory() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_cancel_story",
		mcpsdk.WithDescription("BL382 — cancel an individual story without cancelling the whole PRD. In-progress task sessions are killed. Returns 409 if the story is already in a terminal state."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story ID to cancel")),
		mcpsdk.WithString("reason", mcpsdk.Description("Optional cancellation reason")),
	)
}
func (s *Server) handleAutonomousPRDCancelStory(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"story_id": req.GetString("story_id", ""),
		"reason":   req.GetString("reason", ""),
		"actor":    "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/cancel_story", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDCancelTask() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_cancel_task",
		mcpsdk.WithDescription("BL382 — cancel an individual task without cancelling its story or PRD. The running session (if any) is killed. Returns 409 if the task is already in a terminal state."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID to cancel")),
		mcpsdk.WithString("reason", mcpsdk.Description("Optional cancellation reason")),
	)
}
func (s *Server) handleAutonomousPRDCancelTask(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"task_id": req.GetString("task_id", ""),
		"reason":  req.GetString("reason", ""),
		"actor":   "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/cancel_task", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDApproveTask() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_approve_task",
		mcpsdk.WithDescription("BL406 Phase 5 — approve an individual task gated by guided_mode_source=\"operator\" so the executor runs it. Only meaningful for a task currently in awaiting_approval status."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID to approve")),
	)
}
func (s *Server) handleAutonomousPRDApproveTask(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"task_id": req.GetString("task_id", ""),
		"actor":   "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/approve_task", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDInstantiate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_instantiate",
		mcpsdk.WithDescription("BL191 — instantiate a template PRD with caller-supplied vars; returns a fresh executable PRD."),
		mcpsdk.WithString("template_id", mcpsdk.Required(), mcpsdk.Description("Template PRD ID")),
		mcpsdk.WithString("vars_json", mcpsdk.Description(`JSON object of {"varname":"value", …}; defaults fill missing optionals.`)),
	)
}
func (s *Server) handleAutonomousPRDInstantiate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("template_id", "")
	rawVars := req.GetString("vars_json", "{}")
	vars := map[string]string{}
	_ = json.Unmarshal([]byte(rawVars), &vars)
	body, _ := json.Marshal(map[string]any{"vars": vars, "actor": "operator"})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/instantiate", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- BL203 (v5.4.0) flexible LLM overrides -------------------------------

func (s *Server) toolAutonomousPRDSetLLM() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_llm",
		mcpsdk.WithDescription("BL203 — set the PRD-level worker LLM. Tasks inherit unless they have their own override. Pass only the fields to change — an omitted parameter leaves the PRD field unchanged, an empty value clears that field (falling back to the global default). decomposition_profile sets the planning/decompose LLM — any configured LLM works: opencode/claude-code spawn a full session with codebase tool access; ollama/openwebui use headless planning. backend sets the task-execution backend."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("backend", mcpsdk.Description("Task-execution backend (claude-code / opencode / goose / ollama / etc.)")),
		mcpsdk.WithString("effort", mcpsdk.Description("low / medium / high / max / quick / normal / thorough")),
		mcpsdk.WithString("model", mcpsdk.Description("specific model name (e.g. qwen3.8:27b)")),
		mcpsdk.WithString("decomposition_profile", mcpsdk.Description("Planning/decompose backend — any configured LLM name (opencode/claude-code = session with full codebase access; ollama/openwebui = headless); empty = use global autonomous.planning_backend")),
		mcpsdk.WithString("guardrail_backend", mcpsdk.Description(guardrailBackendParamDesc)),
		mcpsdk.WithString("guardrail_model", mcpsdk.Description(guardrailModelParamDesc)),
	)
}
func (s *Server) handleAutonomousPRDSetLLM(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, err := prdSetLLMBody(req)
	if err != nil {
		return nil, err
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_llm", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// prdSetLLMBody builds the PRD-level set_llm proxy body with per-field
// presence: actor is always included (defaulting to "operator"), and each
// worker-LLM field — backend, effort, model, decomposition_profile plus
// guardrail_backend / guardrail_model — is forwarded only when that key
// actually appears in the call arguments. A present key may be "", which
// clears the field; an absent key leaves the PRD field unchanged.
func prdSetLLMBody(req mcpsdk.CallToolRequest) (map[string]any, error) {
	args := req.GetArguments()
	body := map[string]any{"actor": req.GetString("actor", "operator")}
	if body["actor"] == "" {
		body["actor"] = "operator"
	}
	for _, k := range []string{"backend", "effort", "model", "decomposition_profile", "guardrail_backend", "guardrail_model"} {
		if _, ok := args[k]; ok {
			body[k] = req.GetString(k, "")
		}
	}
	return body, nil
}

func (s *Server) toolAutonomousPRDSetStoryLLM() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_story_llm",
		mcpsdk.WithDescription("BL381 — override the worker LLM for all tasks in one story. Empty string = clear (falls back to PRD then global). Resolution order: per-task → per-story → per-PRD → global."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story ID")),
		mcpsdk.WithString("backend", mcpsdk.Description("LLM backend name; empty = inherit PRD then global")),
		mcpsdk.WithString("effort", mcpsdk.Description("effort level; empty = inherit")),
		mcpsdk.WithString("model", mcpsdk.Description("specific model name; empty = backend default")),
	)
}
func (s *Server) handleAutonomousPRDSetStoryLLM(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"story_id": req.GetString("story_id", ""),
		"backend":  req.GetString("backend", ""),
		"effort":   req.GetString("effort", ""),
		"model":    req.GetString("model", ""),
		"actor":    "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_story_llm", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDSetTaskLLM() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_task_llm",
		mcpsdk.WithDescription("BL203 — override the worker LLM for a single task. Empty string = clear (falls back to PRD then global)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID")),
		mcpsdk.WithString("backend", mcpsdk.Description("LLM backend name; empty = inherit PRD then global")),
		mcpsdk.WithString("effort", mcpsdk.Description("effort level; empty = inherit")),
		mcpsdk.WithString("model", mcpsdk.Description("specific model name; empty = backend default")),
		mcpsdk.WithString("guardrail_backend", mcpsdk.Description(guardrailBackendParamDesc)),
		mcpsdk.WithString("guardrail_model", mcpsdk.Description(guardrailModelParamDesc)),
	)
}
func (s *Server) handleAutonomousPRDSetTaskLLM(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"task_id": req.GetString("task_id", ""),
		"backend": req.GetString("backend", ""),
		"effort":  req.GetString("effort", ""),
		"model":   req.GetString("model", ""),
		"actor":   "operator",
	})
	if gb := guardrailLLMBody(req, map[string]string{"task_id": req.GetString("task_id", ""), "actor": "operator"}, "backend", "effort", "model"); gb != nil {
		body = gb
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_task_llm", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDSetTaskProjectDir() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_task_project_dir",
		mcpsdk.WithDescription("BL447 — override the project directory for a single task (task > Automaton). Diff capture, scope, file drift, quality gates and the verifier all follow it. Empty string = clear (inherits the Automaton's project_dir)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID")),
		mcpsdk.WithString("project_dir", mcpsdk.Description("absolute directory; empty = inherit the Automaton's project_dir")),
	)
}
func (s *Server) handleAutonomousPRDSetTaskProjectDir(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"task_id":     req.GetString("task_id", ""),
		"project_dir": req.GetString("project_dir", ""),
		"actor":       "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_task_project_dir", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

const (
	guardrailBackendParamDesc = "Guardrail/verification judge LLM override (any configured LLM name). Inherits task -> automaton -> global autonomous.verification_backend. Empty = clear; omit to leave unchanged."
	guardrailModelParamDesc   = "Guardrail/verification judge model override. Inherits task -> automaton -> global autonomous.verification_model. Empty = clear; omit to leave unchanged."
)

// guardrailLLMBody (BL416) builds the set_llm / set_task_llm proxy body
// when the caller supplied guardrail_backend and/or guardrail_model.
// Only keys present in the arguments are forwarded, so "" clears and an
// absent key leaves the field unchanged. When no legacy key was supplied
// the legacy keys are omitted so the REST guardrail-only path runs and
// the worker LLM is not overwritten. Returns nil when no guardrail key
// was supplied — the caller keeps its legacy body.
func guardrailLLMBody(req mcpsdk.CallToolRequest, base map[string]string, legacyKeys ...string) []byte {
	args := req.GetArguments()
	body := map[string]string{}
	for _, k := range []string{"guardrail_backend", "guardrail_model"} {
		if _, ok := args[k]; ok {
			body[k] = req.GetString(k, "")
		}
	}
	if len(body) == 0 {
		return nil
	}
	for k, v := range base {
		body[k] = v
	}
	for _, k := range legacyKeys {
		if _, ok := args[k]; ok {
			for _, lk := range legacyKeys {
				body[lk] = req.GetString(lk, "")
			}
			break
		}
	}
	out, _ := json.Marshal(body)
	return out
}

// ----- per-unit verification LLM overrides ----------------------------------

const (
	verificationBackendParamDesc = "LLM that judges (verifies) the work — any configured LLM name. Precedence: task > story > Automaton > global autonomous verification_backend. Empty string = clear (inherit the next level); omit to leave unchanged."
	verificationModelParamDesc   = "Specific model for the verifier. Empty string = clear (inherit); omit to leave unchanged."
	verificationEffortParamDesc  = "Effort level for the verifier (resolves independently of backend/model). Empty string = clear (inherit); omit to leave unchanged."
)

func (s *Server) toolAutonomousPRDSetTaskVerificationLLM() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_task_verification_llm",
		mcpsdk.WithDescription("Override which LLM judges (verifies) one task's work. Precedence: task > story > Automaton > global autonomous verification_backend. Only the fields you pass change; an empty string clears that field."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID")),
		mcpsdk.WithString("verification_backend", mcpsdk.Description(verificationBackendParamDesc)),
		mcpsdk.WithString("verification_model", mcpsdk.Description(verificationModelParamDesc)),
		mcpsdk.WithString("verification_effort", mcpsdk.Description(verificationEffortParamDesc)),
		mcpsdk.WithString("actor", mcpsdk.Description("Who made the change (default operator)")),
	)
}
func (s *Server) handleAutonomousPRDSetTaskVerificationLLM(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.proxyVerificationLLM(req, "set_task_verification_llm", "task_id")
}

func (s *Server) toolAutonomousPRDSetStoryVerificationLLM() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_story_verification_llm",
		mcpsdk.WithDescription("Override which LLM judges (verifies) the work of every task in one story (a task-level override still wins). Precedence: task > story > Automaton > global autonomous verification_backend. Only the fields you pass change; an empty string clears that field."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story ID")),
		mcpsdk.WithString("verification_backend", mcpsdk.Description(verificationBackendParamDesc)),
		mcpsdk.WithString("verification_model", mcpsdk.Description(verificationModelParamDesc)),
		mcpsdk.WithString("verification_effort", mcpsdk.Description(verificationEffortParamDesc)),
		mcpsdk.WithString("actor", mcpsdk.Description("Who made the change (default operator)")),
	)
}
func (s *Server) handleAutonomousPRDSetStoryVerificationLLM(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.proxyVerificationLLM(req, "set_story_verification_llm", "story_id")
}

func (s *Server) toolAutonomousPRDSetVerificationLLM() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_verification_llm",
		mcpsdk.WithDescription("Override which LLM judges (verifies) the work of a whole Automaton (story- and task-level overrides still win). Precedence: task > story > Automaton > global autonomous verification_backend. Only the fields you pass change; an empty string clears that field."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("verification_backend", mcpsdk.Description(verificationBackendParamDesc)),
		mcpsdk.WithString("verification_model", mcpsdk.Description(verificationModelParamDesc)),
		mcpsdk.WithString("verification_effort", mcpsdk.Description(verificationEffortParamDesc)),
		mcpsdk.WithString("actor", mcpsdk.Description("Who made the change (default operator)")),
	)
}
func (s *Server) handleAutonomousPRDSetVerificationLLM(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.proxyVerificationLLM(req, "set_verification_llm", "")
}

// proxyVerificationLLM POSTs a verification LLM override to the REST
// action. Only the keys the caller actually supplied are forwarded, so
// an absent key leaves the field unchanged and "" clears it.
func (s *Server) proxyVerificationLLM(req mcpsdk.CallToolRequest, action, unitKey string) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+req.GetString("id", "")+"/"+action, verificationLLMBody(req, unitKey))
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func verificationLLMBody(req mcpsdk.CallToolRequest, unitKey string) []byte {
	args := req.GetArguments()
	body := map[string]string{"actor": req.GetString("actor", "operator")}
	if body["actor"] == "" {
		body["actor"] = "operator"
	}
	if unitKey != "" {
		body[unitKey] = req.GetString(unitKey, "")
	}
	for _, k := range []string{"verification_backend", "verification_model", "verification_effort"} {
		if _, ok := args[k]; ok {
			body[k] = req.GetString(k, "")
		}
	}
	out, _ := json.Marshal(body)
	return out
}

// ----- autonomous_learnings --------------------------------------------------

func (s *Server) toolAutonomousLearnings() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_learnings",
		mcpsdk.WithDescription("BL24 — list extracted post-task learnings."),
	)
}
func (s *Server) handleAutonomousLearnings(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/learnings", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_children (BL191 Q4, v5.9.0) ----------------------------

func (s *Server) toolAutonomousPRDChildren() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_children",
		mcpsdk.WithDescription("BL191 Q4 — list child PRDs spawned from this PRD's SpawnPRD tasks (genealogy tree)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Parent PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDChildren(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyGet("/api/autonomous/prds/"+id+"/children", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
