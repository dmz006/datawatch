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
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/pipeline"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ----- autonomous_status -----------------------------------------------------

func (s *Server) toolAutonomousStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_status",
		mcpsdk.WithDescription("BL24 — return autonomous loop status (running, active PRDs, queued/running tasks) plus an `automations` array with one row per non-terminal Automation (status, current member and Automaton, pending members, idle time, blocked reason). Each row carries `active_cursors` (one entry per live member: member_id, prd_id or automation_id, status; always present, empty when idle) and `join_wait` while a join waits on its branches (join_member_id, rule, pending_branches, failed_branches). The status also carries the per-backend usage_backoff section (back-off flags, effective config, and the patterns_version / patterns_stale / claude_cli_version pattern-drift guard); patterns_stale also turns true when the claude tool version text changes since the last recheck, and usage_backoff_recheck re-stores both. The `budgets` array carries the dollar budget state of every LLM that has a budget (same entries as budget_status; allocation_pct is an estimate that never blocks, peer_window is off by default). The `queue` array lists every queued or capacity-waiting task with the plain-words reason it waits (task_id, prd_id, prd_title, task_title, cause, reason, since, resets_at when known, holder when the reason names one); the `holdings` array lists every running task with the pools it holds (host, llm:, node: and any tree lock key), node, session_id and since. Both are omitted when empty. The `review_needed` array lists every task held for operator review across all Automata, oldest hold first: automaton_id, automaton_title, task_id, task_title, failure_count (failed attempts), last_backend and last_model (the worker of the last attempt) and held_at. It is omitted when no task is held; decide each one with autonomous_prd_resolve_task_review."))
}
func (s *Server) handleAutonomousStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/status", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- automation_merged_members ---------------------------------------------

func (s *Server) toolAutomationMergedMembers() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_merged_members",
		mcpsdk.WithDescription("Read the merged-member decisions of one Automaton, or of every Automaton (Automata) when no id is given. A decision is written when a member of an Automaton ended cancelled or completed and its work was found already merged into the project default branch. Each row has automation_id, automation_name, member, prd_id, branch, default_branch, proving_commit (the commit that proves the merge), policy (the merged-member policy in force: completed, manual or block), outcome (advanced when the chain moved on, blocked when the Automaton stayed blocked for the operator) and at (time), oldest first. Read-only; the same object as GET /api/automations/{id}/merged-members, or GET /api/automations/merged-members for all Automata. Nothing is changed, merged or resumed."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
		mcpsdk.WithString("id", mcpsdk.Description("Automaton ID. Empty or omitted lists the merged-member decisions of every Automaton.")),
	)
}

func (s *Server) handleAutomationMergedMembers(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	path := "/api/automations/merged-members"
	if id := strings.TrimSpace(req.GetString("id", "")); id != "" {
		path = "/api/automations/" + url.PathEscape(id) + "/merged-members"
	}
	out, err := s.proxyGet(path, nil)
	if err != nil {
		return toolError("automation_merged_members", err), nil
	}
	return textOK(string(out)), nil
}

// ----- autonomous_config_get / set -------------------------------------------

func (s *Server) toolAutonomousConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_config_get",
		mcpsdk.WithDescription("Read the current autonomous (Automata) config: poll interval, max_concurrent_tasks (daemon-wide default for how many independent tasks of one Automaton run at once; 1 = sequential), resume_max_age_seconds (how old in seconds a completed task may be before it is no longer eligible for resume; 3600 = 1h default, 0 = never resume, -1 = no age limit), max_engines_per_node (local-model sessions allowed per compute node; default 1), backends, retries, capacity limits, guardrail lists and timeouts. "+
			"Automaton-level (per_prd_guardrails) guardrails accept \"rules-review\" and \"ci-watch\"; both are off unless listed. \"ci-watch\" makes an automaton, when it completes, wait (up to ci_watch_timeout_seconds, default 1500) for the CI runs of the commit it pushed; if any run failed, the configured LLM judges the failure against the project's rules file. It is skipped when nothing was pushed or the origin remote is not GitHub. "+
			"block_on_file_drift (default false) controls foreign-file drift: when false a task whose commit swept in files that were already uncommitted before it started is only flagged (warn-only); when true that task is blocked instead. An automaton's own setting or its guardrail profile overrides it; worktree-mode automata are never checked. "+
			"undeclared_edit_mode sets what happens when a task edits files outside its declared scope: off = skip the check, warn = record + alert (the default), block = hold the task. "+
			"gate_disagreement (default \"warn\") covers verifier/quality-gate disagreement: when the verifier passed a task but the quality gate found a regression, \"warn\" records task.gate_disagreement and a PRD decision-log entry and the task still completes, while \"block\" fails the task with a \"gate_disagreement: …\" error. An invalid gate never blocks. "+
			"default_quality_gates (BL367) is the gate config applied to new automata: enabled, test_command, timeout, block_on_regression, scope (full | touched-plus-smoke | touched; how much of the suite a gate task runs) and smoke_packages (package patterns for the touched-plus-smoke scope). "+
			"scope_lint (off, warn or enforce; default enforce) controls the pre-spawn scope lint that checks a task's planned paths before its session starts: off skips it, warn spawns the task and records a scope_lint_warn decision, enforce blocks the task."),
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
	return mcpsdk.NewTool("autonomous_config_set", append([]mcpsdk.ToolOption{
		mcpsdk.WithDescription("Update the autonomous (Automata) config. Merges onto the current config, so a partial body (e.g. just enabled) only changes the fields given and leaves the rest untouched. " +
			"Automaton-level (per_prd_guardrails) guardrails accept \"rules-review\" and \"ci-watch\"; both are off unless listed. \"ci-watch\" makes an automaton, when it completes, wait (up to ci_watch_timeout_seconds, default 1500) for the CI runs of the commit it pushed; if any run failed, the configured LLM judges the failure against the project's rules file. It is skipped when nothing was pushed or the origin remote is not GitHub. " +
			"task_override_statuses sets which PRD statuses accept task- and story-level LLM overrides; unknown status names are rejected. " +
			"undeclared_edit_mode sets what happens when a task edits files outside its declared scope: off = skip the check, warn = record + alert (the default), block = hold the task. " +
			"The verifier_policy_* arguments set autonomous.verifier_policy, which chooses the backend that runs each verification; they take effect on the next verification with no restart."),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Toggle autonomous loop on/off")),
		mcpsdk.WithNumber("poll_interval_seconds", mcpsdk.Description("Background loop tick (default 30)")),
		mcpsdk.WithNumber("max_concurrent_tasks", mcpsdk.Description("Daemon-wide default for how many independent tasks of one Automaton run at once. An Automaton's own setting overrides it. 1 = sequential (the default); values below 1 are ignored.")),
		mcpsdk.WithNumber("resume_max_age_seconds", mcpsdk.Description("How old (in seconds) a completed task may be before it is no longer eligible for resume (default 3600). 0 = never resume; -1 = no age limit.")),
		mcpsdk.WithNumber("max_parallel_tasks", mcpsdk.Description("DEPRECATED — accepted and ignored, has no effect; use max_concurrent_tasks. Will be removed in a future release.")),
		mcpsdk.WithNumber("max_engines_per_node", mcpsdk.Description("Number of local-model sessions allowed per compute node. Defaults to 1; values below 1 are clamped to 1.")),
		mcpsdk.WithString("planning_backend", mcpsdk.Description("LLM backend for the Automata planning call (empty = inherit); alias: decomposition_backend")),
		mcpsdk.WithString("verification_backend", mcpsdk.Description("BL25 verifier backend (empty = inherit; set to a different backend for cross-backend independence)")),
		mcpsdk.WithArray("verification_backends", mcpsdk.Description("v8.36.9 — ordered list of LLM registry names to load-balance verifier capacity across (e.g. [\"ollama-datawatch\", \"ollama-local-node\"]); tries each in order, uses whichever has free node capacity right now. Empty = single-backend behavior via verification_backend above.")),
		mcpsdk.WithArray("task_override_statuses", mcpsdk.Description("PRD statuses that accept task/story-level LLM overrides; names: draft, planning, needs_review, approved, revisions_asked, active, running, completed, rejected, cancelled, archived, blocked, failed, paused; empty array restores the defaults (needs_review, revisions_asked, cancelled, blocked)")),
		mcpsdk.WithString("plan_coverage_check", mcpsdk.Description("Plan requirement-coverage gate mode: off (no gate), warn (failing coverage is reported on the PRD but approval proceeds), block (the default: approving a PRD with a failing plan-coverage report is blocked until the operator acknowledges that exact failing surface with autonomous_prd_plan_coverage_acknowledge)")),
		mcpsdk.WithNumber("automation_idle_alert_minutes", mcpsdk.Description("Minutes an active Automation may sit idle (a member finished and no next member started) before one idle alert is raised. Whole number >= 0; 0 turns the alert off.")),
		mcpsdk.WithNumber("automation_triage_alert_minutes", mcpsdk.Description("Minutes an Automation may sit waiting before a triage alert is raised. Whole number >= 0; 0 turns the triage alert off, unset keeps the 30-minute default.")),
		mcpsdk.WithString("automation_merge_pending_mode", mcpsdk.Enum("off", "shadow", "on"), mcpsdk.Description("Merge-pending check of finished Automaton branches: off (the default, no check), shadow (records and reports only) or on. Empty keeps the current value.")),
		mcpsdk.WithNumber("automation_merge_pending_alert_minutes", mcpsdk.Description("Minutes a finished Automaton branch may sit unmerged before a merge-pending alert is raised. Whole number >= 0; 0 turns the merge-pending alert off, unset keeps the 30-minute default.")),
		mcpsdk.WithString("automation_merged_member_policy", mcpsdk.Enum("completed", "manual", "block"), mcpsdk.Description("What an Automaton does when a member ends cancelled or completed and its work is already merged into the project default branch: completed (the default) treats the member as completed and the chain follows its on_complete edge, manual keeps the blocked outcome and adds the merge proof to the block reason for the operator, block keeps the blocked outcome and runs no git check. A branch that is not merged always keeps the blocked outcome. Empty keeps the current value.")),
		mcpsdk.WithString("lane_fill_mode", mcpsdk.Enum("off", "shadow", "on"), mcpsdk.Description("Lane fill mode: off (no planning), shadow (plans and records every decision but starts nothing) or on (starts the planned work). Empty is shadow.")),
		mcpsdk.WithNumber("lane_fill_gpu_lanes", mcpsdk.Description("Number of GPU lanes lane fill plans for. Whole number >= 0.")),
		mcpsdk.WithNumber("lane_fill_claude_lanes", mcpsdk.Description("Number of Claude lanes lane fill plans for. Whole number >= 0; 0 means no Claude lane.")),
		mcpsdk.WithNumber("lane_fill_idle_alert_minutes", mcpsdk.Description("Minutes a lane may sit idle with eligible work queued before one alert is raised. Whole number >= 0; 0 turns the alert off.")),
		mcpsdk.WithNumber("lane_fill_decompose_ahead_min", mcpsdk.Description("Minimum number of decomposed Automata kept ready per lane. Whole number >= 0.")),
		mcpsdk.WithNumber("lane_fill_poll_seconds", mcpsdk.Description("Seconds between lane fill planner polls. Whole number, at least 5.")),
		mcpsdk.WithString("lane_fill_gpu_backend", mcpsdk.Description("LLM backend the GPU lanes run (default pi-prompt).")),
		mcpsdk.WithString("lane_fill_gpu_model", mcpsdk.Description("Model the GPU lanes run. Empty leaves the model to the backend.")),
		mcpsdk.WithString("lane_fill_claude_backend", mcpsdk.Description("LLM backend the Claude lanes run (default claude-code).")),
		mcpsdk.WithString("automation_create_search_mode", mcpsdk.Description("Search-before-create mode: off never searches before creating an Automation, shadow (the default) searches and surfaces over-duplicate warnings, on searches and holds an over-duplicate creation back to the operator.")),
		mcpsdk.WithNumber("automation_create_search_threshold", mcpsdk.Description("Over-duplication threshold in the open-closed interval (0, 1]; unset keeps the 0.5 default.")),
		mcpsdk.WithString("automation_cleanup_mode", mcpsdk.Description("Safe-to-delete cleanup mode: off (no cleanup pass), report (the default; records and reports only) or on. An empty or unrecognized value reads as report, unlike the other enum modes.")),
		mcpsdk.WithNumber("automation_cleanup_retention_hours", mcpsdk.Description("How long a removal candidate may sit unused in whole hours before it counts as retained for cleanup. Zero or negative keeps the 24-hour default.")),
		mcpsdk.WithNumber("automation_cleanup_max_removals_per_pass", mcpsdk.Description("Maximum removals one cleanup pass may perform. Zero or negative keeps the 10 default.")),
		mcpsdk.WithNumber("automation_disk_alert_percent", mcpsdk.Description("Disk-usage percent at or above which a disk alert is raised. Unset keeps the 85 default; other values are clamped to 1-100.")),
		mcpsdk.WithString("automation_agent_insert_policy", mcpsdk.Description("What happens when an agent session inserts a member into an Automation: allow (the default) lets a draft insert land at once, require_operator holds every agent insert until an operator approves it.")),
		mcpsdk.WithNumber("progress_lanes", mcpsdk.Description("Number of parallel lanes the progress figures assume (autonomous.progress.lanes). Whole number >= 0; 0 = 5.")),
		mcpsdk.WithNumber("progress_inactive_gap_hours", mcpsdk.Description("Hours without activity that count as an inactive window in the progress figures (autonomous.progress.inactive_gap_hours). Number >= 0; 0 = 3.")),
		mcpsdk.WithString("progress_backlog_path", mcpsdk.Description("Backlog file the progress figures read (autonomous.progress.backlog_path). A relative path resolves against the Automata base repository when set, else the daemon working directory. Empty = docs/plans/README.md.")),
		mcpsdk.WithString("progress_since", mcpsdk.Description("First day the progress figures count, as YYYY-MM-DD (autonomous.progress.since). Empty = the first record's creation date.")),
		mcpsdk.WithNumber("progress_snapshot_retention_days", mcpsdk.Description("Days saved progress snapshots are kept (autonomous.progress.snapshot_retention_days). Whole number >= 0; 0 = 90.")),
		mcpsdk.WithString("usage_backoff", mcpsdk.Description("Per-backend usage back-off mode: pause_backend (the default) holds autonomous dispatch to a backend while one of its sessions is rate-limited, until the parsed reset time or the maximum back-off; off disables only that automatic path (manual usage_backoff_set still works). Interactive session starts are never blocked.")),
		mcpsdk.WithNumber("backoff_max_wait_minutes", mcpsdk.Description("Maximum usage back-off in whole minutes (>= 1, default 60), applied when a rate-limit message carries no reset time.")),
		mcpsdk.WithNumber("plan_coverage_min_ratio", mcpsdk.Description("Minimum covered/total requirement ratio for a plan to pass the coverage gate; a number in (0,1] (e.g. 0.9 = 90%). Zero/negative and values above 1 both clamp to 1.0 (require full coverage — the safe default). The gate also fails on any structural finding regardless of ratio.")),
		mcpsdk.WithString("plan_tier_max_inflight", mcpsdk.Description("Subscription plan tier inflight caps — tiers pro|max_5x|max_20x|api mapped to integer >= 0 (0 = unlimited). A PRD (or LLM) on a tier is admission-capped by that tier's value unless it or its LLM carries an explicit max_inflight > 0, which wins. Accepts a JSON object (e.g. {\"max_5x\":2}) or tier=value pairs (e.g. 'max_5x=2,api=0').")),
		mcpsdk.WithString("plan_tier_allocation_usd", mcpsdk.Description("Notional dollar allocation per subscription plan tier (autonomous.plan_tier_allocation_usd) — tiers pro|max_5x|max_20x|api mapped to a number >= 0 (0 = not declared). It only feeds the allocation_pct estimate on a budget, which is informational and never blocks. A JSON object (e.g. {\"max_5x\":100}) replaces the whole table; tier=value pairs (e.g. 'pro=20,max_5x=100') set just those tiers and leave the others alone.")),
		mcpsdk.WithString("planning_effort", mcpsdk.Description("BL41 effort hint for planning; alias: decomposition_effort")),
		mcpsdk.WithString("verification_effort", mcpsdk.Description("BL41 effort hint for verifier")),
		mcpsdk.WithNumber("auto_fix_retries", mcpsdk.Description("Retries on verifier failure (default 1)")),
		mcpsdk.WithBoolean("security_scan", mcpsdk.Description("Run nightwire-port security scan before commit")),
		mcpsdk.WithNumber("verifier_diff_max_bytes", mcpsdk.Description("BL366 — max bytes of git diff injected into verifier prompt (0 = 8192 default)")),
		mcpsdk.WithBoolean("quality_gates_enabled", mcpsdk.Description("BL367 — enable quality gates by default for all PRDs")),
		mcpsdk.WithString("quality_gates_test_command", mcpsdk.Description("BL367 — default test command for quality gates (e.g. 'go test ./...')")),
		mcpsdk.WithNumber("quality_gates_timeout", mcpsdk.Description("BL367 — default gate timeout in seconds (0 = no limit)")),
		mcpsdk.WithBoolean("quality_gates_block_on_regression", mcpsdk.Description("BL367 — block task on regression by default")),
		mcpsdk.WithString("quality_gates_scope", mcpsdk.Description("BL367 — gate scope by default: full | touched-plus-smoke | touched. Controls how much of the test suite a gate task runs (empty = inherit the full default)")),
		mcpsdk.WithString("quality_gates_smoke_packages", mcpsdk.Description("BL367 — smoke package patterns (comma-separated or JSON array) used by the touched-plus-smoke scope (empty = the built-in set)")),
		mcpsdk.WithBoolean("injection_guard", mcpsdk.Description("BL369 — scan user-supplied PRD/task specs for prompt injection phrases (warn only by default)")),
		mcpsdk.WithBoolean("block_on_injection", mcpsdk.Description("BL369 — reject PRD/task create or edit when injection phrases are detected (requires injection_guard:true)")),
		mcpsdk.WithString("plan_review_backend", mcpsdk.Description("Plan review gate — LLM backend for the pre-approval reviewer judge (empty = inherit planning_backend)")),
		mcpsdk.WithString("plan_review_model", mcpsdk.Description("Plan review gate — model for the pre-approval reviewer judge (empty = planning_model)")),
		mcpsdk.WithString("plan_review_mode", mcpsdk.Description("Plan review gate — off (default, no review), warn (findings recorded, approval allowed), block (high-critical findings block approval). Unknown modes are rejected.")),
		mcpsdk.WithString("smoke_verification_backend", mcpsdk.Description("Smoke verification — reviewer LLM backend for smoke tasks (empty = feature off)")),
		mcpsdk.WithString("smoke_verification_model", mcpsdk.Description("Smoke verification — reviewer model; use a different model than the task for cross-model independence")),
		mcpsdk.WithNumber("planning_timeout_seconds", mcpsdk.Description("Override effort-scaled decompose timeout (0 = default: 5 min normal, 15 min high/max, 2 min quick/low)")),
		mcpsdk.WithNumber("verification_timeout_seconds", mcpsdk.Description("Bound for one verifier/guardrail judgment session in seconds (default 1800 = 30m; use 0 or a negative value to mean \"leave unset\"); local models need more time than the old hard-coded 10m")),
		mcpsdk.WithArray("judge_policy", mcpsdk.Description("Ordered list of LLM backends the judges (Automaton guardrail, plan review and other autonomous judges) try, one attempt per backend in order. Empty list = the judges follow the verifier routing policy, one attempt per backend it offers. A judge moves to the next backend only when its run hits the time limit.")),
		mcpsdk.WithNumber("judge_timeout_seconds", mcpsdk.Description("Time limit in seconds for one judge run (default 1800 = 30m). It counts the judge run only, not the wait for a capacity slot. 0 or a negative value means the default.")),
		mcpsdk.WithNumber("judge_size_threshold_lines", mcpsdk.Description("Changed-line count above which the judges route by change size (default 400). 0 turns size-aware routing off. Must be a whole number of 0 or more.")),
		mcpsdk.WithNumber("verifier_no_progress_timeout_seconds", mcpsdk.Description("Bound in seconds for a verifier session that shows no output file, no pane activity, no live runner process and no held capacity slot before it is ended as an infra failure (default 900 = 15m; 0 or negative means use the default)")),
		mcpsdk.WithNumber("session_idle_timeout_seconds", mcpsdk.Description("Bound for one autonomous worker/verifier session in seconds — a session sitting in waiting_input/idle with no live child process longer than this is treated as an infra failure and retried through the auto_fix_retries budget (default 900 = 15m; 0 or negative means use the default)")),
		mcpsdk.WithNumber("descendant_stall_seconds", mcpsdk.Description("Bound in seconds for a pi-prompt autonomous worker whose only liveness is its work child process — no output, no file change and no log write for this long means the session is stalled and is reaped as an infra failure (default 2700 = 45 minutes; 0 or negative means use the default)")),
		mcpsdk.WithNumber("verifier_infra_escalation_threshold", mcpsdk.Description("Consecutive infra verifier failures on one task after which its next verification runs on fallback_verification_backend (default 2; 0 = default)")),
		mcpsdk.WithString("fallback_verification_backend", mcpsdk.Description("Backend used for the one escalated verification of a task (default claude-code; empty = default)")),
		mcpsdk.WithNumber("worker_idle_nudge_seconds", mcpsdk.Description("How long a PRD-owned claude-code worker may sit idle, in seconds, before it is sent one nudge asking it to print its completion line or continue (default 180; 0 or negative means use the default)")),
		mcpsdk.WithNumber("worker_idle_nudge_stall_seconds", mcpsdk.Description("How long, in seconds counted from the nudge, a nudged worker may stay silent before it is stopped and retried (default 600; 0 or negative means use the default)")),
		mcpsdk.WithNumber("session_start_confirm_seconds", mcpsdk.Description("Bound in seconds for a claude-code autonomous session to produce its first assistant output after start — a session still in waiting_input with no assistant output longer than this is a 'session never started' failure (default 120 = 2m; 0 or negative means use the default)")),
		mcpsdk.WithNumber("stuck_running_seconds", mcpsdk.Description("How long, in seconds, an Automaton may stay running with every task finished, no live worker session and no live executor before the stuck-running watchdog re-fires its completion roll-up once, and then blocks it after a second full bound (default 900 = 15m; 0 or negative means use the default)")),
		mcpsdk.WithNumber("loop_detect_turns", mcpsdk.Description("Turn window the live loop detector looks over when watching a worker session (default 3; 0 turns loop detection off entirely; negative values are rejected)")),
		mcpsdk.WithNumber("task_max_attempt_seconds", mcpsdk.Description("Wall-clock cap on one task attempt in seconds (0 = off, the default; recommended 5400)")),
		mcpsdk.WithNumber("task_no_progress_seconds", mcpsdk.Description("Seconds one task attempt may go without progress (0 = off, the default; recommended 1800)")),
		mcpsdk.WithBoolean("capacity_enabled", mcpsdk.Description("Capacity-aware admission: tasks wait for a free host/node/LLM slot instead of failing (default true)")),
		mcpsdk.WithNumber("capacity_wait_timeout_seconds", mcpsdk.Description("Maximum seconds a task waits for capacity (default 14400)")),
		mcpsdk.WithNumber("capacity_rate_requests", mcpsdk.Description("Rate ceiling on each LLM: most task/verifier starts it may take per rolling window; 0 = no requests ceiling (default); negative values are rejected")),
		mcpsdk.WithNumber("capacity_rate_tokens", mcpsdk.Description("Rate ceiling on each LLM: most tokens it may use per rolling window; 0 = no tokens ceiling (default); negative values are rejected")),
		mcpsdk.WithNumber("capacity_rate_window_seconds", mcpsdk.Description("Rolling window length in seconds for the two rate ceilings (0 = default 3600)")),
		mcpsdk.WithNumber("ci_watch_timeout_seconds", mcpsdk.Description("Maximum seconds the ci-watch completion guardrail waits for the CI runs of an automaton's own pushed commit (default 1500). Runs still unfinished at the deadline leave the automaton complete with ci_watch.status \"pending\".")),
		mcpsdk.WithArray("per_prd_guardrails", mcpsdk.Description("Daemon-wide default guardrails run once when an automaton completes. Accepts \"rules-review\" (judge the automaton's diff against the project's rules file) and \"ci-watch\" (wait for and judge the CI runs of the commit it pushed). Empty list (the default) turns both off; an automaton's own list or guardrail profile overrides this. The value replaces the whole list, so read it with autonomous_config_get first and include the entries you want to keep."), mcpsdk.WithStringItems()),
		mcpsdk.WithBoolean("continue_on_story_failure", mcpsdk.Description("Daemon-wide default: false (halt) stops a PRD as soon as any story fails instead of continuing into later, independent stories; true restores the old continue-regardless behavior. Per-PRD override takes precedence.")),
		mcpsdk.WithString("gate_disagreement", mcpsdk.Description("Policy for verifier/quality-gate disagreement: applies when the verifier passed a task but the quality gate found a regression. Only \"warn\" (the default; records task.gate_disagreement and a PRD decision-log entry, task still completes) or \"block\" (fails the task with a \"gate_disagreement: …\" error) are accepted. An invalid gate never blocks.")),
		mcpsdk.WithBoolean("block_on_file_drift", mcpsdk.Description("Daemon-wide default for foreign-file-drift enforcement. false (the default) only flags a task whose own commit swept in files that were already uncommitted before it started and were not in its planned files (warn-only, recorded as task foreign_file_drift and a file_drift_detected decision); true blocks that task instead. An automaton's own setting or its guardrail profile overrides this; worktree-mode automata are never checked.")),
		mcpsdk.WithString("undeclared_edit_mode", mcpsdk.Description("Daemon-wide default for undeclared edits (a task editing files outside its declared scope): off = skip the check, warn = record + alert (the default), block = hold the task.")),
		mcpsdk.WithString("scope_lint", mcpsdk.Description("Pre-spawn scope lint mode. off skips the lint; warn spawns a task whose planned paths fail it and records a scope_lint_warn decision; enforce (the default) blocks that task before its session starts."), mcpsdk.Enum("off", "warn", "enforce")),
		mcpsdk.WithBoolean("default_auto_pr", mcpsdk.Description("BL407 Phase 5 — daemon-wide default for a new PRD's Git.AutoPR (push+open a PR on completion). Applied once at PRD creation; the per-PRD autonomous_prd_set_git tool overrides it after that. Default false.")),
	}, verifierPolicyConfigTools()...)...)
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
	// B165 — resume_max_age_seconds: 0 (never resume) and -1 (no age
	// limit) are meaningful sentinels, so the sentinel for "unset" is -2,
	// the first value below the valid domain; any value >= -1 is
	// forwarded as given (0 and -1 pass through).
	if v := req.GetFloat("resume_max_age_seconds", -2); v >= -1 {
		body["resume_max_age_seconds"] = int(v)
	}
	// Presence-aware and forwarded as given (0 included): the REST write
	// clamps anything below 1 to 1, so the clamp lives in one place.
	if args := req.GetArguments(); args != nil {
		if _, ok := args["max_engines_per_node"]; ok {
			body["max_engines_per_node"] = int(req.GetFloat("max_engines_per_node", 0))
		}
	}
	// gate_disagreement — "warn" or "block"; the REST PUT rejects anything
	// else with a 400, so no local validation is needed here.
	if v := req.GetString("gate_disagreement", ""); v != "" {
		body["gate_disagreement"] = v
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
	// Plan-coverage gate — off|warn|block mode (empty = send nothing, let
	// the REST side keep its stored value) plus the presence-aware
	// minimum-coverage ratio so an operator can explicitly set 0 (which
	// normalizes to the 1.0 safe default) rather than having it dropped.
	if v := req.GetString("plan_coverage_check", ""); v != "" {
		body["plan_coverage_check"] = v
	}
	if args := req.GetArguments(); args != nil {
		if _, ok := args["plan_coverage_min_ratio"]; ok {
			body["plan_coverage_min_ratio"] = req.GetFloat("plan_coverage_min_ratio", 0)
		}
	}
	// Usage back-off (autonomous.usage_backoff / backoff_max_wait_minutes);
	// the mode is omitted when empty, the maximum is presence-aware so the
	// REST side validates whatever the caller sent.
	if v := req.GetString("usage_backoff", ""); v != "" {
		body["usage_backoff"] = v
	}
	if args := req.GetArguments(); args != nil {
		if _, ok := args["backoff_max_wait_minutes"]; ok {
			body["backoff_max_wait_minutes"] = req.GetFloat("backoff_max_wait_minutes", 0)
		}
	}
	// Automation idle-alert + triage-alert thresholds (autonomous.automation.*);
	// presence-aware so an explicit 0 (off) is sent. The agent insert policy
	// (autonomous.automation.agent_insert_policy) and the search-before-create
	// keys (create_search_mode, create_search_threshold) ride in the same object.
	auto := map[string]any{}
	if args := req.GetArguments(); args != nil {
		if _, ok := args["automation_idle_alert_minutes"]; ok {
			auto["idle_alert_minutes"] = req.GetFloat("automation_idle_alert_minutes", 0)
		}
		if _, ok := args["automation_triage_alert_minutes"]; ok {
			auto["triage_alert_minutes"] = req.GetFloat("automation_triage_alert_minutes", 0)
		}
		if _, ok := args["automation_create_search_threshold"]; ok {
			auto["create_search_threshold"] = req.GetFloat("automation_create_search_threshold", 0)
		}
		// Cleanup keys (autonomous.automation.cleanup_*, disk_alert_percent).
		// Presence-aware so an explicit 0 is sent: the server-side Effective*
		// accessors resolve 0/unset to the defaults (24 hours, 10 removals,
		// 85 percent) and an unknown mode word to report.
		if _, ok := args["automation_cleanup_retention_hours"]; ok {
			auto["cleanup_retention_hours"] = req.GetFloat("automation_cleanup_retention_hours", 0)
		}
		if _, ok := args["automation_cleanup_max_removals_per_pass"]; ok {
			auto["cleanup_max_removals_per_pass"] = req.GetFloat("automation_cleanup_max_removals_per_pass", 0)
		}
		if _, ok := args["automation_disk_alert_percent"]; ok {
			auto["disk_alert_percent"] = req.GetFloat("automation_disk_alert_percent", 0)
		}
	}
	if v := req.GetString("automation_create_search_mode", ""); v != "" {
		auto["create_search_mode"] = v
	}
	if v := req.GetString("automation_cleanup_mode", ""); v != "" {
		auto["cleanup_mode"] = v
	}
	if v := req.GetString("automation_merge_pending_mode", ""); v != "" {
		auto["merge_pending_mode"] = v
	}
	if args := req.GetArguments(); args != nil {
		if _, ok := args["automation_merge_pending_alert_minutes"]; ok {
			auto["merge_pending_alert_minutes"] = req.GetFloat("automation_merge_pending_alert_minutes", 0)
		}
	}
	if v := req.GetString("automation_merged_member_policy", ""); v != "" {
		auto["merged_member_policy"] = v
	}
	if v := req.GetString("automation_agent_insert_policy", ""); v != "" {
		auto["agent_insert_policy"] = v
	}
	if len(auto) > 0 {
		body["automation"] = auto
	}
	// Lane fill (autonomous.lane_fill.*) rides in its own nested object,
	// sent only when one of the lane_fill_* arguments is given.
	if lane := laneFillConfigArgs(req); len(lane) > 0 {
		body["lane_fill"] = lane
	}
	// plan_tier_max_inflight — the per-tier inflight cap table (tier → integer).
	// It is a table in the REST surface, so parse the tool's string argument to
	// an object here; the /api/autonomous/config PUT guard rejects unknown
	// tiers with an actionable message.
	if v := req.GetString("plan_tier_max_inflight", ""); v != "" {
		table, err := parseTierInflightTable(v)
		if err != nil {
			return toolError("autonomous_config_set", err), nil
		}
		body["plan_tier_max_inflight"] = table
	}
	// plan_tier_allocation_usd lives in the daemon config, not the Automata
	// manager config, so it goes through the config endpoint (object form =
	// whole table, pair form = single-tier keys). Parse it before anything is
	// written so a bad value changes nothing.
	var allocation map[string]any
	if v := req.GetString("plan_tier_allocation_usd", ""); v != "" {
		entries, err := parsePlanTierAllocationArg(v)
		if err != nil {
			return toolError("autonomous_config_set", err), nil
		}
		allocation = entries
	}
	// The progress parameters (autonomous.progress.*) also live in the daemon
	// config. They are presence-aware so an explicit 0 or empty string is sent,
	// and ride in the same all-or-nothing config PUT as the allocation table.
	if args := req.GetArguments(); args != nil {
		for _, k := range []string{"lanes", "inactive_gap_hours", "backlog_path", "since", "snapshot_retention_days"} {
			raw, ok := args["progress_"+k]
			if !ok {
				continue
			}
			if allocation == nil {
				allocation = map[string]any{}
			}
			allocation["autonomous.progress."+k] = raw
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
	if v := req.GetString("quality_gates_scope", ""); v != "" {
		// REST 400s on a bad value; fail early with the named valid set so
		// the caller gets one clean error.
		if _, err := pipeline.ParseGateScope(v); err != nil {
			return toolError("autonomous_config_set", err), nil
		}
		qg["scope"] = v
	}
	if args := req.GetArguments(); args != nil {
		if v, ok := args["quality_gates_smoke_packages"]; ok {
			qg["smoke_packages"] = qualityGatesSmokePackages(v)
		}
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
	for _, k := range []string{"plan_review_backend", "plan_review_model", "plan_review_mode", "smoke_verification_backend", "smoke_verification_model"} {
		if v := req.GetString(k, ""); v != "" {
			body[k] = v
		}
	}
	// v8.25.1 — configurable planning timeout.
	if v := req.GetFloat("planning_timeout_seconds", -1); v >= 0 {
		body["planning_timeout_seconds"] = int(v)
	}
	if v := req.GetFloat("verification_timeout_seconds", -1); v >= 0 {
		body["verification_timeout_seconds"] = int(v)
	}
	// Presence-aware like verification_backends: an explicit [] clears the
	// judge policy so the judges follow the verifier routing policy again.
	if args := req.GetArguments(); args != nil {
		if _, ok := args["judge_policy"]; ok {
			body["judge_policy"] = req.GetStringSlice("judge_policy", []string{})
		}
	}
	if v := req.GetFloat("judge_timeout_seconds", -1); v >= 0 {
		body["judge_timeout_seconds"] = int(v)
	}
	// Same present-versus-absent check as judge_timeout_seconds, so an
	// explicit 0 (size-aware routing off) is sent.
	if v := req.GetFloat("judge_size_threshold_lines", -1); v >= 0 {
		body["judge_size_threshold_lines"] = int(v)
	}
	if v := req.GetFloat("session_idle_timeout_seconds", -1); v >= 0 {
		body["session_idle_timeout_seconds"] = int(v)
	}
	if v := req.GetFloat("descendant_stall_seconds", -1); v >= 0 {
		body["descendant_stall_seconds"] = int(v)
	}
	if v := req.GetFloat("verifier_infra_escalation_threshold", -1); v >= 0 {
		body["verifier_infra_escalation_threshold"] = int(v)
	}
	if v := req.GetString("fallback_verification_backend", ""); v != "" {
		body["fallback_verification_backend"] = v
	}
	if v := req.GetFloat("verifier_no_progress_timeout_seconds", -1); v >= 0 {
		body["verifier_no_progress_timeout_seconds"] = int(v)
	}
	if v := req.GetFloat("worker_idle_nudge_seconds", -1); v >= 0 {
		body["worker_idle_nudge_seconds"] = int(v)
	}
	if v := req.GetFloat("worker_idle_nudge_stall_seconds", -1); v >= 0 {
		body["worker_idle_nudge_stall_seconds"] = int(v)
	}
	if v := req.GetFloat("session_start_confirm_seconds", -1); v >= 0 {
		body["session_start_confirm_seconds"] = int(v)
	}
	if v := req.GetFloat("stuck_running_seconds", -1); v >= 0 {
		body["stuck_running_seconds"] = int(v)
	}
	if v := req.GetFloat("loop_detect_turns", -1); v >= 0 {
		body["loop_detect_turns"] = int(v)
	}
	if v := req.GetFloat("task_max_attempt_seconds", -1); v >= 0 {
		body["task_max_attempt_seconds"] = int(v)
	}
	if v := req.GetFloat("task_no_progress_seconds", -1); v >= 0 {
		body["task_no_progress_seconds"] = int(v)
	}
	if args := req.GetArguments(); args != nil {
		if _, ok := args["capacity_enabled"]; ok {
			body["capacity_enabled"] = req.GetBool("capacity_enabled", true)
		}
	}
	// Presence-aware and forwarded as given (negatives included) so the REST
	// write is the one place that rejects them.
	if args := req.GetArguments(); args != nil {
		for _, k := range []string{"capacity_rate_requests", "capacity_rate_tokens", "capacity_rate_window_seconds"} {
			if _, ok := args[k]; ok {
				body[k] = int(req.GetFloat(k, 0))
			}
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
		if _, ok := args["undeclared_edit_mode"]; ok {
			body["undeclared_edit_mode"] = req.GetString("undeclared_edit_mode", "")
		}
		if _, ok := args["scope_lint"]; ok {
			body["scope_lint"] = req.GetString("scope_lint", "")
		}
	}
	// Verifier policy (autonomous.verifier_policy.*), nested under one object.
	if vp := verifierPolicyConfigBody(req); len(vp) > 0 {
		body["verifier_policy"] = vp
	}
	if allocation != nil && len(body) == 0 {
		out, err := s.putPlanTierAllocation(allocation)
		if err != nil {
			return nil, err
		}
		return textOK(string(out)), nil
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/autonomous/config", body)
	if err != nil {
		return nil, err
	}
	if allocation != nil {
		if _, err := s.putPlanTierAllocation(allocation); err != nil {
			return nil, err
		}
	}
	return textOK(string(out)), nil
}

// qualityGatesSmokePackages normalises the quality_gates_smoke_packages / smoke_packages
// argument (JSON array of strings, comma-separated string, or a single
// pattern) into the REST body list. Whitespace is trimmed; empty input
// yields an empty list ("fall back to the default smoke set").
func qualityGatesSmokePackages(v any) []string {
	out := []string{}
	var list []string
	switch val := v.(type) {
	case []any:
		for _, item := range val {
			if s, ok := item.(string); ok {
				list = append(list, s)
			}
		}
	case string:
		list = strings.Split(val, ",")
	}
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ----- autonomous_prds list / create / get / decompose / run / cancel --------

func (s *Server) toolAutonomousPRDList() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_list",
		mcpsdk.WithDescription("BL24 — list all PRDs, newest first."),
	)
}
func (s *Server) handleAutonomousPRDList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/prds", url.Values{"full": {"1"}})
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
		mcpsdk.WithString("kind", mcpsdk.Enum("feature", "bug", "other"), mcpsdk.Description("Optional Automaton kind; omit to derive it from the title and spec")),
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
	if kind := req.GetString("kind", ""); kind != "" {
		body["kind"] = kind
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds", body)
	return llmSetterResult(out, err)
}

func (s *Server) toolAutonomousPRDSetKind() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_kind",
		mcpsdk.WithDescription("Set the kind of an Automaton: feature, bug or other. The kind feeds the progress figures. An empty kind clears the setting so the kind is derived again from the title and spec."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Enum("feature", "bug", "other", ""), mcpsdk.Description("feature, bug or other; empty to derive")),
	)
}
func (s *Server) handleAutonomousPRDSetKind(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"kind": req.GetString("kind", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_kind", body)
	return llmSetterResult(out, err)
}

// llmSetterResult turns a proxied LLM setter response into a tool result.
// A refusal from the REST handler (non-2xx, such as a model/backend
// pairing the backend cannot serve) becomes a tool error carrying the REST
// error text, so the caller sees a failure rather than a success.
func llmSetterResult(out []byte, err error) (*mcpsdk.CallToolResult, error) {
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_get",
		mcpsdk.WithDescription("Fetch one automaton. The detail argument picks how much comes back; the default is summary. "+
			"summary: ids, titles, statuses and counts for the automaton, its stories and its tasks. "+
			"tasks: everything in summary, plus each task's status, error and verification summary. "+
			"full: everything, including specs and logs. "+
			"full also exposes plan_coverage — the plan-requirement coverage gate report stored at decompose time: the settings the PRD was pinned to (mode + min_ratio), the report (covered/missing/weak requirement counts, each unmatched requirement with its verdict), any findings (structural findings), and the operator acknowledgment ack {by, at, note, fingerprint} when one exists. Absent when no report was recorded yet. "+
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

func (s *Server) toolAutonomousPRDResume() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_resume",
		mcpsdk.WithDescription("Resume a PRD the boot-resume gate parked after a daemon restart; errors if the PRD is not parked."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("note", mcpsdk.Description("optional free-form note saved on the Decision row")),
	)
}
func (s *Server) handleAutonomousPRDResume(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": "operator", "note": req.GetString("note", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/resume", body)
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

// Plan-coverage gate — operator acknowledgment of a failing coverage
// report. The manager pins the ack to the exact failing surface (the
// fingerprint of the stored report), so acknowledging an old or
// different failing surface is refused; re-decompose to fix the plan or
// acknowledge the new surface.
func (s *Server) toolAutonomousPRDPlanCoverageAcknowledge() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_plan_coverage_acknowledge",
		mcpsdk.WithDescription("Operator-acknowledge a failing plan-requirement coverage report on a PRD (plan_coverage_check=block gate). Required arguments: id and a non-empty note explaining why the failing surface is accepted. The ack is pinned to the exact failing surface of the stored report (unmatched requirements + structural findings), so approving after the plan changed needs a fresh ack. Read the report first with the full detail of autonomous_prd_get (plan_coverage field)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("actor", mcpsdk.Description("Operator identity recorded on the ack (defaults to the caller)")),
		mcpsdk.WithString("note", mcpsdk.Required(), mcpsdk.Description("Non-empty justification for accepting the failing coverage surface")),
	)
}
func (s *Server) handleAutonomousPRDPlanCoverageAcknowledge(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": req.GetString("actor", "operator"), "note": req.GetString("note", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/plan_coverage/acknowledge", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// Plan review gate: re-run the pre-approval reviewer judge for a stored
// PRD. The response is the PRD with its plan_review verdict replaced, so a
// passing verdict clears a blocking one; with plan_review_mode off the PRD
// is returned as stored.
func (s *Server) toolAutonomousPRDPlanReview() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_plan_review",
		mcpsdk.WithDescription("Plan review gate — re-run the pre-approval reviewer judge for a PRD and return the PRD with its new plan_review verdict (ok, severity, summary, findings). Re-running replaces the stored verdict, so a passing verdict clears a blocking one; with plan_review_mode off the PRD is returned as stored."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("actor", mcpsdk.Description("Operator identity recorded on the audit entry (defaults to operator)")),
		mcpsdk.WithString("note", mcpsdk.Description("Optional operator note for the audit trail")),
	)
}
func (s *Server) handleAutonomousPRDPlanReview(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": req.GetString("actor", "operator"), "note": req.GetString("note", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/plan-review", body)
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

func (s *Server) toolAutonomousPRDRejudge() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_rejudge",
		mcpsdk.WithDescription("Re-judge the completion guardrail of a blocked automaton (PRD) whose tasks are all completed, without resetting any task. Use it when the guardrail verdict that blocked the PRD deserves a fresh evaluation (new evidence, revised gate settings) but no task needs another attempt. Refused otherwise: on PRDs that are not blocked, or on blocked PRDs that still have incomplete tasks — use autonomous_prd_reset_task instead."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDRejudge(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": "operator"})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/rejudge", body)
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
	return llmSetterResult(out, err)
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
	return llmSetterResult(out, err)
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
	return llmSetterResult(out, err)
}

// ----- thinking-level overrides (per-task → per-story → per-PRD → pi.thinking_level) -----

func (s *Server) toolAutonomousPRDSetThinkingLevel() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_thinking_level",
		mcpsdk.WithDescription("Set the PRD-level pi --thinking level override (off|minimal|low|medium|high). Empty string clears it (falls back to the global pi.thinking_level config). Tasks inherit unless overridden."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("thinking_level", mcpsdk.Description("thinking level: off|minimal|low|medium|high; empty clears to the global config")),
		mcpsdk.WithString("actor", mcpsdk.Description("actor for the audit log (default operator)")),
	)
}
func (s *Server) handleAutonomousPRDSetThinkingLevel(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"thinking_level": req.GetString("thinking_level", ""),
		"actor":          req.GetString("actor", "operator"),
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_thinking_level", body)
	return llmSetterResult(out, err)
}

func (s *Server) toolAutonomousPRDSetStoryThinkingLevel() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_story_thinking_level",
		mcpsdk.WithDescription("Override the pi --thinking level for all tasks in one story (off|minimal|low|medium|high). Empty string clears (falls back to PRD then global)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story ID")),
		mcpsdk.WithString("thinking_level", mcpsdk.Description("thinking level; empty = inherit PRD then global")),
		mcpsdk.WithString("actor", mcpsdk.Description("actor for the audit log (default operator)")),
	)
}
func (s *Server) handleAutonomousPRDSetStoryThinkingLevel(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"story_id":       req.GetString("story_id", ""),
		"thinking_level": req.GetString("thinking_level", ""),
		"actor":          req.GetString("actor", "operator"),
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_story_thinking_level", body)
	return llmSetterResult(out, err)
}

func (s *Server) toolAutonomousPRDSetTaskThinkingLevel() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_task_thinking_level",
		mcpsdk.WithDescription("Override the pi --thinking level for a single task (most specific in the chain). Empty string clears (falls back to story, PRD, then global config)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID")),
		mcpsdk.WithString("thinking_level", mcpsdk.Description("thinking level; empty = inherit story, PRD then global")),
		mcpsdk.WithString("actor", mcpsdk.Description("actor for the audit log (default operator)")),
	)
}
func (s *Server) handleAutonomousPRDSetTaskThinkingLevel(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"task_id":        req.GetString("task_id", ""),
		"thinking_level": req.GetString("thinking_level", ""),
		"actor":          req.GetString("actor", "operator"),
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_task_thinking_level", body)
	return llmSetterResult(out, err)
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
	return llmSetterResult(out, err)
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

// parseTierInflightTable parses the plan_tier_max_inflight tool argument into
// a tier → limit table. Accepted forms: a JSON object ({"max_5x":2}, any
// value kind JSON accepts) or tier=value pairs ("max_5x=2,pro=1"). Keys must
// be known plan tiers; values must be integers >= 0. Returns an actionable
// error otherwise — the same contract the REST surface's parsePlanTierTable
// enforces (kept local because the mcp package must not import internal/server).
func parseTierInflightTable(v string) (map[string]int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, fmt.Errorf("plan_tier_max_inflight: empty value; omit the argument to leave the stored table unchanged")
	}
	if strings.HasPrefix(v, "{") {
		var raw map[string]json.Number
		if err := json.Unmarshal([]byte(v), &raw); err != nil {
			return nil, fmt.Errorf("plan_tier_max_inflight: JSON object parse: %v", err)
		}
		return finishTierTable(raw)
	}
	raw := map[string]json.Number{}
	for _, pair := range strings.Split(v, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, val, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("plan_tier_max_inflight: %q is not tier=value nor a JSON object", pair)
		}
		iv, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil || iv < 0 {
			return nil, fmt.Errorf("plan_tier_max_inflight: limit for tier %q must be an integer >= 0", strings.TrimSpace(key))
		}
		raw[strings.TrimSpace(key)] = json.Number(strconv.Itoa(iv))
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("plan_tier_max_inflight: no tier=value pairs parsed from %q", v)
	}
	return finishTierTable(raw)
}

func finishTierTable(raw map[string]json.Number) (map[string]int, error) {
	out := make(map[string]int, len(raw))
	for k, n := range raw {
		iv, err := strconv.Atoi(n.String())
		if err != nil || iv < 0 {
			return nil, fmt.Errorf("plan_tier_max_inflight: limit for tier %q must be an integer >= 0", k)
		}
		if !inference.ValidPlanTier(inference.PlanTier(k)) {
			return nil, fmt.Errorf("plan_tier_max_inflight: unknown plan tier %q (expected pro, max_5x, max_20x, or api)", k)
		}
		out[k] = iv
	}
	return out, nil
}
