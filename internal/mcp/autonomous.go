// BL24+BL25 (v3.10.0) — MCP-tool parity for autonomous PRD endpoints.
// Each tool wraps the corresponding REST endpoint via the in-process
// HTTP loopback (proxyGet/proxyJSON in sx_parity.go).

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/pipeline"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ----- autonomous_status -----------------------------------------------------

func (s *Server) toolAutonomousStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_status",
		mcpsdk.WithDescription("BL24 — return autonomous loop status (each Automaton row whose completion review blocked carries rules_review_outcome: action, findings, retries; running, active PRDs, queued/running tasks) plus an `automations` array with one row per non-terminal Automation (status, current member and Automaton, pending members, idle time, blocked reason). Each row carries `active_cursors` (one entry per live member: member_id, prd_id or automation_id, status; always present, empty when idle) and `join_wait` while a join waits on its branches (join_member_id, rule, pending_branches, failed_branches). The status also carries the per-backend usage_backoff section (back-off flags, effective config, and the patterns_version / patterns_stale / claude_cli_version pattern-drift guard); patterns_stale also turns true when the claude tool version text changes since the last recheck, and usage_backoff_recheck re-stores both. The `budgets` array carries the dollar budget state of every LLM that has a budget (same entries as budget_status; allocation_pct is an estimate that never blocks, peer_window is off by default). The `queue` array lists every queued or capacity-waiting task with the plain-words reason it waits (task_id, prd_id, prd_title, task_title, cause, reason, since, resets_at when known, holder when the reason names one); the `holdings` array lists every running task with the pools it holds (host, llm:, node: and any tree lock key), node, session_id and since. Both are omitted when empty. The `review_needed` array lists every task held for operator review across all Automata, oldest hold first: automaton_id, automaton_title, task_id, task_title, failure_count (failed attempts), last_backend and last_model (the worker of the last attempt), held_at, and the attempt record of that last attempt: last_backend_reason (why that backend was chosen), last_resumed (true when it resumed the previous session), last_resume_reason (the retry-resume decision) and no_retry_reason (why the task went to review without another attempt). The reason and resume fields are omitted when not recorded. It is omitted when no task is held; decide each one with autonomous_prd_resolve_task_review. The `memory_ceiling` object is the effective memory ceiling of a verification run with no project or task kind override: effective_mb (0 when none), source (default, project, task_kind or none), enforcement_path (scope for a systemd user scope, watchdog for the /proc watchdog, none when nothing enforces it) and the share_percent, floor_mb and cap_mb it is computed from; source is none while no ceiling settings are installed, and the settings are changed with config_set on resource_limits.memory_share_percent, resource_limits.memory_floor_mb and resource_limits.memory_cap_mb."))
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
		mcpsdk.WithDescription("Read the current autonomous (Automata) config: poll interval, max_concurrent_tasks (daemon-wide default for how many independent tasks of one Automaton run at once; 1 = sequential), resume_max_age_seconds (how old in seconds a completed task may be before it is no longer eligible for resume; 3600 = 1h default, 0 = never resume, -1 = no age limit), max_engines_per_node (local-model sessions allowed per compute node; default 1), host_load_alert (mode off/alert/reap, multiple, minutes: the host load check), backends, retries, capacity limits, guardrail lists and timeouts. "+
			"Automaton-level (per_prd_guardrails) guardrails accept \"rules-review\" and \"ci-watch\"; both are off unless listed. \"ci-watch\" makes an automaton, when it completes, wait (up to ci_watch_timeout_seconds, default 1500) for the CI runs of the commit it pushed; if any run failed, the configured LLM judges the failure against the project's rules file. It is skipped when nothing was pushed or the origin remote is not GitHub. "+
			"block_on_file_drift (default false) controls foreign-file drift: when false a task whose commit swept in files that were already uncommitted before it started is only flagged (warn-only); when true that task is blocked instead. An automaton's own setting or its guardrail profile overrides it; worktree-mode automata are never checked. "+
			"snapshot_never_stage lists the path patterns the autonomous snapshot must never `git add` (an explicit empty list = nothing is protected; unset = the built-in default list). "+
			"undeclared_edit_mode sets what happens when a task edits files outside its declared scope: off = skip the check, warn = record + alert (the default), block = hold the task. "+
			"gate_disagreement (default \"warn\") covers verifier/quality-gate disagreement: when the verifier passed a task but the quality gate found a regression, \"warn\" records task.gate_disagreement and a PRD decision-log entry and the task still completes, while \"block\" fails the task with a \"gate_disagreement: …\" error. An invalid gate never blocks. "+
			"default_quality_gates (BL367) is the gate config applied to new automata: enabled, test_command, timeout, block_on_regression, scope (full | touched-plus-smoke | touched; how much of the suite a gate task runs) and smoke_packages (package patterns for the touched-plus-smoke scope). "+
			"priority_by_kind (kind → integer table) sets how high in the capacity queue an automaton waits per PRD kind — the higher the number, the closer to the front; unset rows read the daemon defaults bug=80, maintenance=70, feature=20, other=0 (the last is the fallback row for any kind with no row of its own). "+
			"A priority set directly on a PRD (the autonomous_prd_set_priority tool) wins for that PRD no matter what the table says, and every queued entry carries its priority plus priority_source (explicit | kind | default) naming the rule that set it. "+
			"defect_intake_automation (string, empty by default) names the Automation that new kind:bug defect filings are recorded under; empty means the filing is recorded, not filed into any Automation. "+
			"scope_lint (off, warn or enforce; default enforce) controls the pre-spawn scope lint that checks a task's planned paths before its session starts: off skips it, warn spawns the task and records a scope_lint_warn decision, enforce blocks the task. "+
			"model_routing (autonomous.model_routing) holds the six model routing keys — refusal_mode, served_models_cache_seconds, verifier_follows_worker_node, unknown_node_policy, probe_timeout_seconds, alert_on_refusal; this call does not return them — read them under autonomous.model_routing with get_config — and set them with the model_routing_* arguments of autonomous_config_set. "+
			"inherited_tags_cache_seconds (autonomous.inherited_tags_cache_seconds) is the lifetime, in whole seconds, of the shared cache of tags inherited from the PRD (0 = the 15-second default, negative = cache off); it lives in the daemon config and is included in this response — set it with the inherited_tags_cache_seconds argument of autonomous_config_set. "+
			"The attempt-cost keys are included too: first_attempt (object: task kind implementation, docs, note or final_gate → {backend, model}; an empty entry keeps today's choice), first_attempt_escalate_after_failures (whole number 0 to 20, default 1) and retry_resume_max_context_tokens (whole number 0 to 2000000, default 150000; 0 = every retry starts a fresh session). Set them with the same-named arguments of autonomous_config_set. "+
			"lane_fill holds the lane fill settings; stalled_merge_minutes (whole minutes, default 30, 0 = rule off) is how long a verified task's merge may stay pending before the Automaton stops holding its lane. It is left out of lane_fill until it is set, so read the value in effect, with the default filled in, under autonomous.lane_fill with get_config; set it with lane_fill_stalled_merge_minutes on autonomous_config_set. How-to: docs/howto/automation-lane-fill.md. "+
			daemonTaskKeysGetDescription),
	)
}
func (s *Server) handleAutonomousConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	// The judge prompt delivery settings live in the daemon config, not in the
	// manager config /api/autonomous/config returns, so they are merged in
	// from /api/config (stored values; 0 = file route off / no extra wait).
	// The three daemon task settings (task template modes, script gate timeout,
	// project gate command) come from the same /api/config read.
	prompt, err := s.judgePromptSettings()
	if err != nil {
		return nil, err
	}
	out, err := s.proxyGet("/api/autonomous/config", nil)
	if err != nil {
		return nil, err
	}
	out, err = mergeJudgePromptSettings(out, prompt)
	if err != nil {
		return nil, err
	}
	out, err = mergeDaemonTaskSettings(out, prompt)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// judgePromptKeys are the two judge prompt delivery settings, each a whole
// number from 0 to the given maximum.
var judgePromptKeys = []struct {
	key string
	max int
}{
	{"judge_prompt_file_threshold_lines", 100000},
	{"judge_delivery_wait_per_thousand_lines_seconds", 3600},
}

func (s *Server) judgePromptSettings() (map[string]json.RawMessage, error) {
	raw, err := s.proxyGet("/api/config", nil)
	if err != nil {
		return nil, err
	}
	var full struct {
		Autonomous map[string]json.RawMessage `json:"autonomous"`
	}
	if err := json.Unmarshal(raw, &full); err != nil {
		return nil, fmt.Errorf("decode /api/config: %w", err)
	}
	return full.Autonomous, nil
}

func mergeJudgePromptSettings(autoCfg []byte, prompt map[string]json.RawMessage) ([]byte, error) {
	merged := map[string]json.RawMessage{}
	if err := json.Unmarshal(autoCfg, &merged); err != nil {
		return nil, fmt.Errorf("decode /api/autonomous/config: %w", err)
	}
	for _, k := range judgePromptKeys {
		if v, ok := prompt[k.key]; ok {
			merged[k.key] = v
		}
	}
	for _, key := range attemptCostKeyNames {
		if v, ok := prompt[key]; ok {
			merged[key] = v
		}
	}
	return json.Marshal(merged)
}

// judgePromptPatch reads the judge prompt delivery arguments, rejecting any
// that is not a whole number in range, and returns the /api/config patch.
func judgePromptPatch(req mcpsdk.CallToolRequest) (map[string]any, error) {
	patch := map[string]any{}
	args := req.GetArguments()
	for _, k := range judgePromptKeys {
		raw, ok := args[k.key]
		if !ok {
			continue
		}
		f, isNum := raw.(float64)
		if !isNum || f != math.Trunc(f) || f < 0 || f > float64(k.max) {
			return nil, fmt.Errorf("%s: %v is invalid; use a whole number from 0 to %d", k.key, raw, k.max)
		}
		patch["autonomous."+k.key] = int(f)
	}
	return patch, nil
}

func (s *Server) toolAutonomousConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_config_set", append([]mcpsdk.ToolOption{
		mcpsdk.WithDescription("Update the autonomous (Automata) config. Merges onto the current config, so a partial body (e.g. just enabled) only changes the fields given and leaves the rest untouched. " +
			daemonTaskKeysSetDescription +
			"Automaton-level (per_prd_guardrails) guardrails accept \"rules-review\" and \"ci-watch\"; both are off unless listed. \"ci-watch\" makes an automaton, when it completes, wait (up to ci_watch_timeout_seconds, default 1500) for the CI runs of the commit it pushed; if any run failed, the configured LLM judges the failure against the project's rules file. It is skipped when nothing was pushed or the origin remote is not GitHub. " +
			"task_override_statuses sets which PRD statuses accept task- and story-level LLM overrides; unknown status names are rejected. " +
			"override_queued_tasks (default on; unset = on) is the master switch for those overrides on a locked Automaton status: when on, a story- or task-level set-llm call applies to the tasks that are still queued and lists the ones left alone (running/finished); when off, the same call is refused on a locked status. " +
			"snapshot_never_stage sets the path patterns the autonomous snapshot must never `git add` (e.g. secret files, out-of-scope vendor trees); blank patterns are rejected, and when unset the built-in default list applies. " +
			"undeclared_edit_mode sets what happens when a task edits files outside its declared scope: off = skip the check, warn = record + alert (the default), block = hold the task. " +
			"The verifier_policy_* arguments set autonomous.verifier_policy, which chooses the backend that runs each verification; they take effect on the next verification with no restart. " +
			"The attempt-cost arguments are checked before anything is written, and a refused value changes nothing: first_attempt (object of task kind implementation, docs, note or final_gate → {backend, model}; replaces the whole map; backends pi, pi-prompt and ollama need a model), first_attempt_escalate_after_failures (whole number 0 to 20, default 1) and retry_resume_max_context_tokens (whole number 0 to 2000000, default 150000; 0 = every retry starts a fresh session)."),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Toggle autonomous loop on/off")),
		mcpsdk.WithNumber("poll_interval_seconds", mcpsdk.Description("Background loop tick (default 30)")),
		mcpsdk.WithNumber("max_concurrent_tasks", mcpsdk.Description("Daemon-wide default for how many independent tasks of one Automaton run at once. An Automaton's own setting overrides it. 1 = sequential (the default); values below 1 are ignored.")),
		mcpsdk.WithNumber("resume_max_age_seconds", mcpsdk.Description("How old (in seconds) a completed task may be before it is no longer eligible for resume (default 3600). 0 = never resume; -1 = no age limit.")),
		mcpsdk.WithNumber("max_parallel_tasks", mcpsdk.Description("DEPRECATED — accepted and ignored, has no effect; use max_concurrent_tasks. Will be removed in a future release.")),
		mcpsdk.WithNumber("max_engines_per_node", mcpsdk.Description("Number of local-model sessions allowed per compute node. Defaults to 1; values below 1 are clamped to 1.")),
		mcpsdk.WithString("host_load_alert_mode", mcpsdk.Enum("off", "alert", "reap"), mcpsdk.Description("Host load check (autonomous.host_load_alert.mode): off = do nothing, alert = one alert per sustained load breach naming the busiest process groups (the default), reap = also end the leftover processes of ended sessions that are proven orphaned.")),
		mcpsdk.WithNumber("host_load_alert_multiple", mcpsdk.Description("Host load check: load average counts as a breach above this multiple of the core count (default 1.5; 0 = default; negative is rejected).")),
		mcpsdk.WithNumber("host_load_alert_minutes", mcpsdk.Description("Host load check: minutes a breach must last before it alerts (default 10; 0 = default; negative is rejected).")),
		mcpsdk.WithString("planning_backend", mcpsdk.Description("LLM backend for the Automata planning call (empty = inherit); alias: decomposition_backend")),
		mcpsdk.WithString("verification_backend", mcpsdk.Description("BL25 verifier backend (empty = inherit; set to a different backend for cross-backend independence)")),
		mcpsdk.WithArray("verification_backends", mcpsdk.Description("v8.36.9 — ordered list of LLM registry names to load-balance verifier capacity across (e.g. [\"ollama-datawatch\", \"ollama-local-node\"]); tries each in order, uses whichever has free node capacity right now. Empty = single-backend behavior via verification_backend above.")),
		mcpsdk.WithArray("task_override_statuses", mcpsdk.Description("PRD statuses that accept task/story-level LLM overrides; names: draft, planning, needs_review, approved, revisions_asked, active, running, completed, rejected, cancelled, archived, blocked, failed, paused; empty array restores the defaults (needs_review, revisions_asked, cancelled, blocked)")),
		mcpsdk.WithBoolean("override_queued_tasks", mcpsdk.Description("Master switch for task/story-level LLM overrides on a locked Automaton status. On (the default; unset reads as on) a set-llm call applies to the still-queued tasks and lists the ones left alone; off refuses such calls on a locked status. Absent key = leave the stored value as-is.")),
		mcpsdk.WithArray("snapshot_never_stage", mcpsdk.Description("Path patterns the autonomous snapshot must never `git add` (e.g. '.env', 'node_modules/'). Blank patterns are rejected; an explicit empty list clears the list (nothing is protected); when unset the built-in default list applies."), mcpsdk.WithStringItems()),
		mcpsdk.WithObject(attemptCostFirstAttemptKey, mcpsdk.Description(attemptCostFirstAttemptDesc)),
		mcpsdk.WithNumber(attemptCostEscalateKey, mcpsdk.Description(attemptCostEscalateDesc)),
		mcpsdk.WithNumber(attemptCostResumeKey, mcpsdk.Description(attemptCostResumeDesc)),
		mcpsdk.WithString("plan_coverage_check", mcpsdk.Description("Plan requirement-coverage gate mode: off (no gate), warn (failing coverage is reported on the PRD but approval proceeds), block (the default: approving a PRD with a failing plan-coverage report is blocked until the operator acknowledges that exact failing surface with autonomous_prd_plan_coverage_acknowledge)")),
		mcpsdk.WithNumber("automation_idle_alert_minutes", mcpsdk.Description("Minutes an active Automation may sit idle (a member finished and no next member started) before one idle alert is raised. Whole number >= 0; 0 turns the alert off.")),
		mcpsdk.WithNumber("automation_triage_alert_minutes", mcpsdk.Description("Minutes an Automation may sit waiting before a triage alert is raised. Whole number >= 0; 0 turns the triage alert off, unset keeps the 30-minute default.")),
		mcpsdk.WithBoolean("automation_child_inherit", mcpsdk.Description("Whether a child Automaton (one created with a parent) inherits its parent's project, priority, tags and Automation membership (autonomous.automation.child_inherit). On by default; values set on the child always win. Omit to keep the current value.")),
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
		mcpsdk.WithNumber("lane_fill_stale_after_hours", mcpsdk.Description("Hours an Automaton may sit queued before lane fill treats it as stale and does not start it. Whole number >= 0; 0 turns the stale rule off, unset keeps the 72-hour default.")),
		mcpsdk.WithNumber("lane_fill_host_down_after_minutes", mcpsdk.Description("Minutes a pool host may stay unreachable before lane fill treats it as down and judges the lanes from the readable hosts. Whole number >= 0, default 10; 0 = never treat an unreachable host as down.")),
		mcpsdk.WithNumber("lane_fill_stalled_merge_minutes", mcpsdk.Description("Whole minutes a verified task's merge into the Automaton branch may stay pending with no progress before the Automaton stops holding its lane; it then appears in the lane fill status waiting list with the reason merge_stalled and an alert is raised. Whole number >= 0, default 30; 0 turns the rule off (the lane is kept). Read the current value with autonomous_config_get (lane_fill.stalled_merge_minutes) or GET /api/config. How-to: docs/howto/automation-lane-fill.md.")),
		mcpsdk.WithBoolean("lane_fill_require_confirmed_proposal", mcpsdk.Description("Lane fill starts an Automaton made from a proposal only after the operator has confirmed that proposal (default true).")),
		mcpsdk.WithBoolean("lane_fill_check_main_carries", mcpsdk.Description("Lane fill checks whether the default branch already carries an Automaton's work before counting it as eligible (default true).")),
		mcpsdk.WithBoolean("lane_fill_require_worktree", mcpsdk.Description("Lane fill counts an Automaton as eligible only when it has its own worktree (default true).")),
		mcpsdk.WithBoolean("lane_fill_lane_counts_from_caps", mcpsdk.Description("Lane fill takes its lane counts from the in-flight caps instead of gpu_lanes and claude_lanes only (default true).")),
		mcpsdk.WithString("lane_fill_gpu_mode", mcpsdk.Description("Lane fill mode for the GPU lanes only: off, shadow or on. Empty inherits lane_fill_mode.")),
		mcpsdk.WithString("lane_fill_claude_mode", mcpsdk.Description("Lane fill mode for the Claude lanes only: off, shadow or on. Empty inherits lane_fill_mode.")),
		mcpsdk.WithString("automation_create_search_mode", mcpsdk.Description("Search-before-create mode: off never searches before creating an Automation, shadow (the default) searches and surfaces over-duplicate warnings, on searches and holds an over-duplicate creation back to the operator.")),
		mcpsdk.WithNumber("automation_create_search_threshold", mcpsdk.Description("Over-duplication threshold in the open-closed interval (0, 1]; unset keeps the 0.5 default.")),
		mcpsdk.WithString("automation_cleanup_mode", mcpsdk.Description("Safe-to-delete cleanup mode: off (no cleanup pass), report (the default; records and reports only) or on. An empty or unrecognized value reads as report, unlike the other enum modes.")),
		mcpsdk.WithString("automation_uncommitted_work_policy", mcpsdk.Description("What happens when a task finishes with uncommitted work in its worktree: hold (the task is held and a question is raised), warn or off (autonomous.automation.uncommitted_work_policy). Any other value is rejected.")),
		mcpsdk.WithString("automation_uncommitted_work_scope", mcpsdk.Description("Which uncommitted files the check counts: task_files (the task's declared files) or worktree (the whole worktree) (autonomous.automation.uncommitted_work_scope). Any other value is rejected.")),
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
		mcpsdk.WithBoolean("plan_coverage_suppress_created_files", mcpsdk.Description("On (the default), a requirement that appears only on the 'create Y' side of an 'X or create Y' alternative does not count as an unmatched requirement for coverage. Pass false to count every numbered requirement regardless.")),
		mcpsdk.WithBoolean("plan_coverage_scoped_requirements", mcpsdk.Description("On (the default), only the numbered requirements inside the configured plan-coverage requirement sections count toward the coverage gate. Pass false to count numbered requirements from the whole spec.")),
		mcpsdk.WithString("plan_coverage_requirement_sections", mcpsdk.Description("Comma-separated spec section names scanned for numbered requirements (default: scope,constraints; empty = that same default)")),
		mcpsdk.WithNumber("plan_coverage_min_requirement_chars", mcpsdk.Description("Minimum characters a numbered line needs to be treated as a requirement (whole number >= 0; 0 = the 30-character default)")),
		mcpsdk.WithString("plan_tier_max_inflight", mcpsdk.Description("Subscription plan tier inflight caps — tiers pro|max_5x|max_20x|api mapped to integer >= 0 (0 = unlimited). A PRD (or LLM) on a tier is admission-capped by that tier's value unless it or its LLM carries an explicit max_inflight > 0, which wins. Accepts a JSON object (e.g. {\"max_5x\":2}) or tier=value pairs (e.g. 'max_5x=2,api=0').")),
		mcpsdk.WithString("priority_by_kind", mcpsdk.Description("Kind → capacity-priority table (autonomous.priority_by_kind): the PRD kind names (bug, maintenance, feature, or your installation's own kind names) mapped to an integer >= 0. The higher the number, the closer to the front of the capacity queue; the shipped rows are bug=80 (fast lane for fixes), maintenance=70, feature=20, other=0 (the fallback row for a kind with no row of its own) — give a kind its own row to move it. A priority set directly on a PRD (the autonomous_prd_set_priority tool) always wins for that PRD, and every queued entry carries its priority plus priority_source (explicit | kind | default) naming the rule that set it. Accepts a JSON object (e.g. {\"bug\":90}) or kind=value pairs (e.g. 'bug=90,maintenance=50'); integer values only — a non-number, a fraction or a boolean is refused with a clear message. An empty value clears the table back to the daemon defaults; omit the argument to leave the stored table unchanged.")),
		mcpsdk.WithString("defect_intake_automation", mcpsdk.Description("The Automation that new kind:bug defect filings flow into (autonomous.defect_intake_automation). Empty by default (each installation chooses its own; a filing without one is recorded, not filed anywhere). Pass the empty string to clear it back to the unset state.")),
		mcpsdk.WithString("plan_tier_allocation_usd", mcpsdk.Description("Notional dollar allocation per subscription plan tier (autonomous.plan_tier_allocation_usd) — tiers pro|max_5x|max_20x|api mapped to a number >= 0 (0 = not declared). It only feeds the allocation_pct estimate on a budget, which is informational and never blocks. A JSON object (e.g. {\"max_5x\":100}) replaces the whole table; tier=value pairs (e.g. 'pro=20,max_5x=100') set just those tiers and leave the others alone.")),
		mcpsdk.WithString("planning_effort", mcpsdk.Description("BL41 effort hint for planning; alias: decomposition_effort")),
		mcpsdk.WithString("verification_effort", mcpsdk.Description("BL41 effort hint for verifier")),
		mcpsdk.WithNumber("auto_fix_retries", mcpsdk.Description("Content auto-fix retries per Automaton (default 1). When the completion review still blocks after these are spent, the Automaton goes to needs_review with the findings attached.")),
		mcpsdk.WithBoolean("security_scan", mcpsdk.Description("Run nightwire-port security scan before commit")),
		mcpsdk.WithNumber("verifier_diff_max_bytes", mcpsdk.Description("BL366 — max bytes of git diff injected into verifier prompt (0 = 8192 default)")),
		mcpsdk.WithNumber("verifier_min_free_mem_mb", mcpsdk.Description("Free GPU memory (MB) a node must keep with extra verifier slots running; gates verifier caps above 1 and verifier_slots writes above 1. Unset = 1024 default; 0 turns the check off. Negative values are refused.")),
		mcpsdk.WithNumber("verifier_headroom_max_age_hours", mcpsdk.Description("How old, in hours, a headroom measurement may be and still count as fresh for the verifier cap gates. 0 or unset = the default of 24 hours. Negative values are refused.")),
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
		mcpsdk.WithNumber("judge_prompt_file_threshold_lines", mcpsdk.Description("Judge prompt length in lines above which the prompt is handed to the judge as a file instead of sent in full (default 300). 0 always sends the full prompt. Must be a whole number from 0 to 100000 (autonomous.judge_prompt_file_threshold_lines).")),
		mcpsdk.WithNumber("judge_delivery_wait_per_thousand_lines_seconds", mcpsdk.Description("Extra seconds the daemon waits for the main prompt to be delivered for every 1000 lines it contains (default 60). 0 adds no extra wait. Must be a whole number from 0 to 3600 (autonomous.judge_delivery_wait_per_thousand_lines_seconds).")),
		mcpsdk.WithString("judge_strong_backend", mcpsdk.Description("The strong judge backend a large diff is routed to first when the size-aware rule fires. Empty (the default) = the last entry of the judge policy (autonomous.judge_strong_backend).")),
		mcpsdk.WithBoolean("judge_verdict_cache_enabled", mcpsdk.Description("Per-commit judge verdict cache: pass/block verdicts replay by (commit, judge policy, rules), and a verified commit is held on a judge timeout instead of re-run (default true). Give false to re-judge every verified commit fresh (autonomous.judge_verdict_cache_enabled).")),
		mcpsdk.WithNumber("verifier_no_progress_timeout_seconds", mcpsdk.Description("Bound in seconds for a verifier session that shows no output file, no pane activity, no live runner process and no held capacity slot before it is ended as an infra failure (default 900 = 15m; 0 or negative means use the default)")),
		mcpsdk.WithNumber("verifier_max_run_seconds", mcpsdk.Description("Hard maximum run time in whole seconds for one verifier run, so a single verifier cannot hold a worker slot for hours (0 means no cap; default 2700 = 45m; negative or fractional values are refused)")),
		mcpsdk.WithNumber("session_idle_timeout_seconds", mcpsdk.Description("Bound for one autonomous worker/verifier session in seconds — a session sitting in waiting_input/idle with no live child process longer than this is treated as an infra failure and retried through the auto_fix_retries budget (default 900 = 15m; 0 or negative means use the default)")),
		mcpsdk.WithNumber("descendant_stall_seconds", mcpsdk.Description("Bound in seconds for a pi-prompt autonomous worker whose only liveness is its work child process — no output, no file change and no log write for this long means the session is stalled and is reaped as an infra failure (default 2700 = 45 minutes; 0 or negative means use the default)")),
		mcpsdk.WithNumber("verifier_infra_escalation_threshold", mcpsdk.Description("Consecutive infra verifier failures on one task after which its next verification runs on fallback_verification_backend (default 2; 0 = default)")),
		mcpsdk.WithString("fallback_verification_backend", mcpsdk.Description("Backend used for the one escalated verification of a task (default claude-code; empty = default)")),
		mcpsdk.WithNumber("worker_idle_nudge_seconds", mcpsdk.Description("How long a PRD-owned claude-code worker may sit idle, in seconds, before it is sent one nudge asking it to print its completion line or continue (default 180; 0 or negative means use the default)")),
		mcpsdk.WithNumber("worker_idle_nudge_stall_seconds", mcpsdk.Description("How long, in seconds counted from the nudge, a nudged worker may stay silent before it is stopped and retried (default 600; 0 or negative means use the default)")),
		mcpsdk.WithNumber("session_start_confirm_seconds", mcpsdk.Description("Bound in seconds for a claude-code autonomous session to produce its first assistant output after start — a session still in waiting_input with no assistant output longer than this is a 'session never started' failure (default 120 = 2m; 0 or negative means use the default)")),
		mcpsdk.WithNumber("stuck_running_seconds", mcpsdk.Description("How long, in seconds, an Automaton may stay running with every task finished, no live worker session and no live executor before the stuck-running watchdog re-fires its completion roll-up once, and then blocks it after a second full bound; when the only blockers are cancelled tasks (every task completed or cancelled, at least one completed) that first step advances it to the completion review and records the skipped cancelled tasks (default 900 = 15m; 0 or negative means use the default)")),
		mcpsdk.WithNumber("loop_detect_turns", mcpsdk.Description("Turn window the live loop detector looks over when watching a worker session (default 3; 0 turns loop detection off entirely; negative values are rejected)")),
		mcpsdk.WithNumber("task_max_attempt_seconds", mcpsdk.Description("Wall-clock cap on one task attempt in seconds (0 = off, the default; recommended 5400)")),
		mcpsdk.WithNumber("task_no_progress_seconds", mcpsdk.Description("Seconds one task attempt may go without progress (0 = off, the default; recommended 1800)")),
		mcpsdk.WithBoolean("capacity_enabled", mcpsdk.Description("Capacity-aware admission: tasks wait for a free host/node/LLM slot instead of failing (default true)")),
		mcpsdk.WithNumber("capacity_wait_timeout_seconds", mcpsdk.Description("Maximum seconds a task waits for capacity (default 14400)")),
		mcpsdk.WithNumber("archive_grace_minutes", mcpsdk.Description("Whole minutes a cancelled Automaton is kept before the supervisor loop archives it (default 60; 0 archives at once; negative or fractional values are rejected)")),
		mcpsdk.WithBoolean("capacity_fallthrough", mcpsdk.Description("Pool fall-through: when a task's requested LLM pool is full, admit it on another idle allowed pool (default false)")),
		mcpsdk.WithBoolean("capacity_strict_node_limit", mcpsdk.Description("Strict per-node limit: verifier and resumed workers never over-admit a compute node (default true; false restores the earlier behaviour)")),
		mcpsdk.WithBoolean("capacity_count_pool_by_node", mcpsdk.Description("Count an LLM pool by node: sessions piled on one node use at most that node's limit of the pool (default true)")),
		mcpsdk.WithBoolean("capacity_adopt_before_admit", mcpsdk.Description("After a restart, admit resumed workers before any new work (default true)")),
		mcpsdk.WithNumber("capacity_adoption_wait_seconds", mcpsdk.Description("Longest wait, in seconds, for resumed workers to be adopted after a restart (default 30; negative values are rejected)")),
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
		mcpsdk.WithString("planning_restart_policy", mcpsdk.Enum("reattach", "restart", "draft"), mcpsdk.Description("autonomous.planning_restart_policy — what a daemon restart does to an Automaton that is still in planning (decompose running): reattach (the default) keeps planning running and re-attaches it to the restarted daemon; restart restarts planning once after the restart and records a decision saying so; draft resets the Automaton to draft, as the daemon did before this setting existed. Any other value is refused. It is sent on PUT /api/config. See docs/howto/restart-with-work-running.md.")),
		mcpsdk.WithNumber("inherited_tags_cache_seconds", mcpsdk.Description("autonomous.inherited_tags_cache_seconds — lifetime, in whole seconds, of the shared cache of tags inherited from the PRD (default 15); 0 = the default, negative = cache off. Not an autonomous-manager key: it is sent on PUT /api/config and takes effect on the next tag read.")),
	}, append(append(append(append(append(append(append(daemonTaskKeysSetOptions(), verifierPolicyConfigTools()...), infraRetryConfigOptions()...), requestWatchConfigOptions()...), mergePipelineConfigOptions()...), parallelConfigOptions()...), modelRoutingConfigOptions()...), pendingItemConfigOptions()...)...)...)
}
func (s *Server) handleAutonomousConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{}
	// Validate before anything is written, so a bad value changes nothing.
	promptPatch, perr := judgePromptPatch(req)
	if perr != nil {
		return mcpsdk.NewToolResultError(perr.Error()), nil
	}
	// planning_restart_policy is a daemon-config key (PUT /api/config), so it
	// joins the same validated patch. An invalid value is refused here, before
	// anything is sent.
	planningPatch, planningErr := planningRestartPolicyPatch(req)
	if planningErr != nil {
		return mcpsdk.NewToolResultError(planningErr.Error()), nil
	}
	for k, v := range planningPatch {
		promptPatch[k] = v
	}
	// Attempt-cost keys go to the same /api/config PUT as the judge prompt keys,
	// after the shared validator has accepted the whole candidate.
	attemptPatch, aerr := s.attemptCostSetPatch(req)
	if aerr != nil {
		return mcpsdk.NewToolResultError(aerr.Error()), nil
	}
	for k, v := range attemptPatch {
		promptPatch[k] = v
	}
	// The daemon task settings (task template modes, script gate timeout,
	// project gate command) ride in the same /api/config PUT as the judge
	// prompt keys. They are validated here too, before anything is written.
	daemonPatch, derr := daemonTaskKeysPatch(req)
	if derr != nil {
		return toolError("autonomous_config_set", derr), nil
	}
	for k, v := range daemonPatch {
		promptPatch[k] = v
	}
	runCap, runCapSet, rcerr := verifierMaxRunSeconds(req)
	if rcerr != nil {
		return mcpsdk.NewToolResultError(rcerr.Error()), nil
	}
	if runCapSet {
		body["verifier_max_run_seconds"] = runCap
	}
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
	// Verifier headroom gates: a present negative value is refused with a tool
	// error, not dropped, so the caller sees the refusal the descriptions name.
	// Presence is checked on the arguments, so 0 is forwarded (0 = check off for
	// the floor, default 24 hours for the age).
	if args := req.GetArguments(); args != nil {
		for _, k := range []string{"verifier_min_free_mem_mb", "verifier_headroom_max_age_hours"} {
			if _, ok := args[k]; !ok {
				continue
			}
			v := req.GetFloat(k, 0)
			if v < 0 {
				return mcpsdk.NewToolResultError(k + " must be >= 0"), nil
			}
			body[k] = int(v)
		}
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
		// Presence-aware boolean: an explicit false is forwarded (off is
		// meaningful), an absent key leaves the stored value as the REST side
		// already resolves it (unset = on).
		if _, ok := args["override_queued_tasks"]; ok {
			body["override_queued_tasks"] = req.GetBool("override_queued_tasks", false)
		}
	}
	// Presence-aware like task_override_statuses above: an explicit []
	// clears the never-stage list (no protected patterns), an absent key
	// leaves the stored list (or the built-in defaults) untouched.
	if args := req.GetArguments(); args != nil {
		if _, ok := args["snapshot_never_stage"]; ok {
			body["snapshot_never_stage"] = req.GetStringSlice("snapshot_never_stage", []string{})
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
		// The filter switches are presence-aware: an explicit false is sent
		// (an off is meaningful — it counts every numbered requirement).
		for _, key := range []string{"plan_coverage_suppress_created_files", "plan_coverage_scoped_requirements"} {
			if _, ok := args[key]; ok {
				body[key] = req.GetBool(key, false)
			}
		}
		if _, ok := args["plan_coverage_requirement_sections"]; ok {
			body["plan_coverage_requirement_sections"] = req.GetString("plan_coverage_requirement_sections", "")
		}
		if _, ok := args["plan_coverage_min_requirement_chars"]; ok {
			if n := req.GetInt("plan_coverage_min_requirement_chars", 0); n >= 0 {
				body["plan_coverage_min_requirement_chars"] = n
			}
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
	if v := req.GetString("automation_uncommitted_work_policy", ""); v != "" {
		auto["uncommitted_work_policy"] = v
	}
	if v := req.GetString("automation_uncommitted_work_scope", ""); v != "" {
		auto["uncommitted_work_scope"] = v
	}
	if args := req.GetArguments(); args != nil {
		if _, ok := args["automation_child_inherit"]; ok {
			auto["child_inherit"] = req.GetBool("automation_child_inherit", true)
		}
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
	// Pending item keys (autonomous.automation.pending_item_policy,
	// pending_item_auto_resume and the pending_item_source fields). Each value
	// is checked with config.ValidatePendingItemKey before anything is sent.
	pendingItem, err := pendingItemConfigArgs(req)
	if err != nil {
		return toolError("autonomous_config_set", err), nil
	}
	for k, v := range pendingItem {
		auto[k] = v
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
	// priority_by_kind — the kind → capacity-priority table (kind → integer).
	// It is a table in the REST surface, so parse the tool's string argument to
	// an object here through the shared validator: non-integer values, empty
	// kind keys and the like are refused with a clear message before anything
	// is written, and the /api/autonomous/config PUT guard rejects the same
	// way. Presence-aware: an explicit empty string clears the table (the
	// daemon defaults then apply); omitting the argument leaves the stored
	// table unchanged.
	if args := req.GetArguments(); args != nil {
		if pkb, ok := args["priority_by_kind"]; ok {
			str, isString := pkb.(string)
			if !isString {
				return toolError("autonomous_config_set", fmt.Errorf("priority_by_kind must be a string: a JSON object (kind → integer priority, e.g. {\"bug\":90}) or kind=value pairs (e.g. 'bug=90,maintenance=50')")), nil
			}
			table, err := parsePriorityByKindTable(str)
			if err != nil {
				return toolError("autonomous_config_set", err), nil
			}
			body["priority_by_kind"] = table
		}
		if di, ok := args["defect_intake_automation"]; ok {
			str, isString := di.(string)
			if !isString {
				return toolError("autonomous_config_set", fmt.Errorf("defect_intake_automation must be a string (the Automation name; the empty string clears it)")), nil
			}
			body["defect_intake_automation"] = strings.TrimSpace(str)
		}
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
	// Infrastructure retry (autonomous.infra_retry.*) lives in the daemon
	// config as well, so it rides in the same config PUT.
	if entries := infraRetryConfigArgs(req); len(entries) > 0 {
		if allocation == nil {
			allocation = map[string]any{}
		}
		for k, v := range entries {
			allocation[k] = v
		}
	}
	// Parallel execution (autonomous.parallel.*) lives in the daemon config
	// too, so it rides in the same config PUT.
	if entries := parallelConfigArgs(req); len(entries) > 0 {
		if allocation == nil {
			allocation = map[string]any{}
		}
		for k, v := range entries {
			allocation[k] = v
		}
	}
	// The merge pipeline settings (autonomous.merge_pipeline.*) ride in the
	// same config PUT; a bad project argument is refused here.
	mpEntries, mpErr := mergePipelineConfigArgs(req)
	if mpErr != nil {
		return toolError("autonomous_config_set", mpErr), nil
	}
	mpSent := len(mpEntries) > 0
	if mpSent {
		if allocation == nil {
			allocation = map[string]any{}
		}
		for k, v := range mpEntries {
			allocation[k] = v
		}
	}
	// Request watch (autonomous.request_watch.*) lives in the daemon config
	// too, so it rides in the same config PUT.
	if entries := requestWatchConfigArgs(req); len(entries) > 0 {
		if allocation == nil {
			allocation = map[string]any{}
		}
		for k, v := range entries {
			allocation[k] = v
		}
	}
	// Model routing (autonomous.model_routing.*) lives in the daemon config as
	// well, so it rides in the same config PUT.
	if entries := modelRoutingConfigArgs(req); len(entries) > 0 {
		if allocation == nil {
			allocation = map[string]any{}
		}
		for k, v := range entries {
			allocation[k] = v
		}
	}
	// Inherited tags cache lifetime (autonomous.inherited_tags_cache_seconds) lives
	// in the daemon config as well, so it rides in the same config PUT. Presence-
	// aware, so an explicit 0 or a negative value (cache off) is sent as given.
	if v, ok := req.GetArguments()["inherited_tags_cache_seconds"]; ok {
		if allocation == nil {
			allocation = map[string]any{}
		}
		allocation["autonomous.inherited_tags_cache_seconds"] = v
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
	// Presence-aware like judge_policy: an explicit empty judge_strong_backend
	// (resolve to the last judge-policy entry) and an explicit false (cache
	// off) must both reach the daemon.
	if args := req.GetArguments(); args != nil {
		if _, ok := args["judge_policy"]; ok {
			body["judge_policy"] = req.GetStringSlice("judge_policy", []string{})
		}
		if _, ok := args["judge_strong_backend"]; ok {
			body["judge_strong_backend"] = strings.TrimSpace(req.GetString("judge_strong_backend", ""))
		}
		if _, ok := args["judge_verdict_cache_enabled"]; ok {
			body["judge_verdict_cache_enabled"] = req.GetBool("judge_verdict_cache_enabled", true)
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
		if _, ok := args["capacity_fallthrough"]; ok {
			body["capacity_fallthrough"] = req.GetBool("capacity_fallthrough", false)
		}
		if _, ok := args["capacity_enabled"]; ok {
			body["capacity_enabled"] = req.GetBool("capacity_enabled", true)
		}
	}
	if args := req.GetArguments(); args != nil {
		for _, k := range []string{"capacity_strict_node_limit", "capacity_count_pool_by_node", "capacity_adopt_before_admit"} {
			if _, ok := args[k]; ok {
				body[k] = req.GetBool(k, true)
			}
		}
		if _, ok := args["capacity_adoption_wait_seconds"]; ok {
			body["capacity_adoption_wait_seconds"] = int(req.GetFloat("capacity_adoption_wait_seconds", 0))
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
	if args := req.GetArguments(); args != nil {
		if _, ok := args["archive_grace_minutes"]; ok {
			body["archive_grace_minutes"] = req.GetFloat("archive_grace_minutes", 0)
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
	// Host load alert (autonomous.host_load_alert.*), nested under one object.
	// Presence-aware; the REST write validates the values.
	if args := req.GetArguments(); args != nil {
		hla := map[string]any{}
		if _, ok := args["host_load_alert_mode"]; ok {
			hla["mode"] = req.GetString("host_load_alert_mode", "")
		}
		if _, ok := args["host_load_alert_multiple"]; ok {
			hla["multiple"] = req.GetFloat("host_load_alert_multiple", 0)
		}
		if _, ok := args["host_load_alert_minutes"]; ok {
			hla["minutes"] = req.GetFloat("host_load_alert_minutes", 0)
		}
		if len(hla) > 0 {
			body["host_load_alert"] = hla
		}
	}
	// Verifier policy (autonomous.verifier_policy.*), nested under one object.
	if vp := verifierPolicyConfigBody(req); len(vp) > 0 {
		body["verifier_policy"] = vp
	}
	var promptOut []byte
	if len(promptPatch) > 0 {
		o, err := s.proxyJSON(http.MethodPut, "/api/config", promptPatch)
		if err != nil {
			return toolError("autonomous_config_set", err), nil
		}
		promptOut = o
		if len(body) == 0 && allocation == nil {
			return textOK(string(promptOut)), nil
		}
	}
	if allocation != nil && len(body) == 0 {
		out, err := s.putPlanTierAllocation(allocation)
		if err != nil {
			return configWriteRefusal(mpSent, err)
		}
		return textOK(string(out)), nil
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/autonomous/config", body)
	if err != nil {
		return nil, err
	}
	if allocation != nil {
		if _, err := s.putPlanTierAllocation(allocation); err != nil {
			return configWriteRefusal(mpSent, err)
		}
	}
	return textOK(string(out)), nil
}

// configWriteRefusal answers a daemon refusal of a config write. A refusal of
// the merge pipeline settings is a tool error carrying the daemon's message,
// which names the value to use (for example the cadence or version scheme that
// is not available yet); other config writes keep their existing reply.
func configWriteRefusal(mergePipeline bool, err error) (*mcpsdk.CallToolResult, error) {
	if mergePipeline {
		return toolError("autonomous_config_set", err), nil
	}
	return nil, err
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
		mcpsdk.WithDescription("BL24 — list all PRDs, newest first. When a PRD's push outcome is recorded, its item also carries git.push_state (pushed / not_pushed), with the pushed ref/sha/time (pushed) or the push reason (not_pushed). Narrow the list without changing any stored state: status takes comma-separated statuses and may include shipped — the shipped-state bucket selected by a non-empty merge commit, independent of the stored status; shipped true keeps only PRDs that carry shipped state, shipped false only ones that do not. When neither is given the full list is returned (full=1); when one is, the summary list under the in-force autonomous.shipped settings applies instead, and shipped=true matches only while enabled."),
		mcpsdk.WithString("status", mcpsdk.Description("Comma-separated statuses to keep; matched against the displayed status (an Automaton whose work landed shows as completed); shipped is still accepted and selects records that carry a shipped record.")),
		mcpsdk.WithString("shipped", mcpsdk.Enum("true", "false"), mcpsdk.Description("true: only PRDs that carry shipped state (non-empty merge commit); false: only ones that do not. Default: the autonomous.shipped.filter_default view (all / shipped / unshipped).")),
	)
}
func (s *Server) handleAutonomousPRDList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	shipped := strings.ToLower(strings.TrimSpace(req.GetString("shipped", "")))
	status := strings.Trim(strings.TrimSpace(req.GetString("status", "")), ",")
	q := url.Values{}
	if shipped == "" && status == "" {
		q.Set("full", "1")
	}
	if shipped != "" {
		if shipped != "true" && shipped != "false" {
			return mcpsdk.NewToolResultError("shipped must be \"true\" or \"false\""), nil
		}
		q.Set("shipped", shipped)
	}
	if status != "" {
		q.Set("status", status)
	}
	out, err := s.proxyGet("/api/autonomous/prds", q)
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
		mcpsdk.WithString("parent_prd_id", mcpsdk.Description("Optional parent Automaton id. The new Automaton inherits the parent's project, Automation, backend, model, tags and priority; an explicit project_dir, backend or model wins. An unknown parent is refused.")),
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
	if parent := strings.TrimSpace(req.GetString("parent_prd_id", "")); parent != "" {
		body["parent_prd_id"] = parent
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds", body)
	return withSpecCheckText(llmSetterResult(out, err))
}

// withSpecCheckText adds the spec-check lines as a second text item after
// the JSON reply (which already carries the spec_check array), so a reader
// of the plain text sees the same lines every other surface prints.
func withSpecCheckText(res *mcpsdk.CallToolResult, err error) (*mcpsdk.CallToolResult, error) {
	if err != nil || res == nil || res.IsError || len(res.Content) == 0 {
		return res, err
	}
	tc, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		return res, err
	}
	if block := autonomous.SpecCheckBlockFromReply([]byte(tc.Text)); block != "" {
		res.Content = append(res.Content, mcpsdk.NewTextContent(block))
	}
	return res, err
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

func (s *Server) toolAutonomousPRDSetReleaseSize() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_release_size",
		mcpsdk.WithDescription("Set the release size of an Automaton: patch, minor, major or none. An empty size clears the setting so the size is resolved again from the Automation, the project's kind map and the built-in default. A major is raised by the operator here or by the Automation milestone rule; it is never inferred from a diff."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("release_size", mcpsdk.Required(), mcpsdk.Enum("patch", "minor", "major", "none", ""), mcpsdk.Description("patch, minor, major or none; empty to resolve again")),
	)
}
func (s *Server) handleAutonomousPRDSetReleaseSize(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"release_size": req.GetString("release_size", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_release_size", body)
	return llmSetterResult(out, err)
}

func (s *Server) toolAutonomousPRDSetBreakingChange() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_breaking_change",
		mcpsdk.WithDescription("Set or clear the breaking-change flag of an Automaton. A breaking change raises its release to the major approval mode, so it is never merged quietly."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithBoolean("breaking_change", mcpsdk.Required(), mcpsdk.Description("true when the change breaks users; false clears the flag")),
	)
}
func (s *Server) handleAutonomousPRDSetBreakingChange(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	v, present := req.GetArguments()["breaking_change"]
	if !present {
		return mcpsdk.NewToolResultError("breaking_change is required (true or false)"), nil
	}
	b, ok := v.(bool)
	if !ok {
		return mcpsdk.NewToolResultError("breaking_change must be true or false"), nil
	}
	body, _ := json.Marshal(map[string]bool{"breaking_change": b})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_breaking_change", body)
	return llmSetterResult(out, err)
}

// toolAutonomousShippedRecord + handler serve the operator/LLM "record shipped
// state" action for one Automaton (PRD) or one Automation. It posts to the
// server's POST /.../shipped endpoint, which writes the four shipped fields
// (merged_in_release, merged_at, merge_commit, shipped_source) and never
// touches status. Idempotent per merge commit.
func (s *Server) toolAutonomousShippedRecord() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_shipped_record",
		mcpsdk.WithDescription("Record the shipped state of an Automaton (PRD) or an Automation: the release it was merged into, the merge time, the merge commit and how the record was written (merge_step or backfill). Recording never changes the entity's status and is idempotent per merge commit. Set automation=true to record an Automation; by default a PRD is addressed."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton (PRD) ID, or Automation ID when automation=true")),
		mcpsdk.WithString("merge_commit", mcpsdk.Required(), mcpsdk.Description("The merge commit")),
		mcpsdk.WithString("merged_in_release", mcpsdk.Description("Release the work was merged into, e.g. v1.2.0")),
		mcpsdk.WithString("merged_at", mcpsdk.Description("Merge time, RFC 3339 (default now)")),
		mcpsdk.WithString("source", mcpsdk.Enum("merge_step", "backfill"), mcpsdk.Description("How the record was written (default backfill)")),
		mcpsdk.WithBoolean("automation", mcpsdk.Description("Record an Automation instead of a PRD")),
	)
}
func (s *Server) handleAutonomousShippedRecord(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	commit := strings.TrimSpace(req.GetString("merge_commit", ""))
	if commit == "" {
		return mcpsdk.NewToolResultError("merge_commit is required"), nil
	}
	body := map[string]any{"merge_commit": commit}
	if v := req.GetString("merged_in_release", ""); v != "" {
		body["merged_in_release"] = v
	}
	if v := req.GetString("merged_at", ""); v != "" {
		body["merged_at"] = v
	}
	if src := req.GetString("source", ""); src != "" {
		body["source"] = src
	} else {
		body["source"] = "backfill" // the daemon demands one of the two; same default as the CLI and comm verbs
	}
	path := "/api/autonomous/prds/" + url.PathEscape(id) + "/shipped"
	if req.GetBool("automation", false) {
		path = "/api/automations/" + url.PathEscape(id) + "/shipped"
	}
	b, _ := json.Marshal(body)
	out, err := s.proxyJSON(http.MethodPost, path, b)
	return llmSetterResult(out, err)
}

// toolAutonomousShippedBackfill + handler serve the operator/LLM "backfill
// shipped state" action: it proxies the server's
// POST /api/autonomous/shipped/backfill endpoint, which runs one
// shipped-state backfill pass over the base repository's main branch
// (source backfill) and returns the report. dry_run=true scans and
// computes the report without writing any shipped record; absent (the
// default, like every other surface) the pass writes.
func (s *Server) toolAutonomousShippedBackfill() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_shipped_backfill",
		mcpsdk.WithDescription("Backfill the shipped state from the base repository's merge history: scans the main branch (MAIN_BRANCH when set) for merge commits that name an automaton branch (automaton/<8-hex id>) and records shipped state (source backfill) on the matching PRDs and on every Automation whose non-skipped member PRDs are all shipped. The release attribution is the first release at or after each merge. No status is ever touched: a cancelled PRD whose merge commit is on the branch is marked shipped while staying cancelled; one with no merge stays plain cancelled (what left_cancelled counts). A dry run and the real run report the same prds_marked and prds_would_mark_cancelled. Returns the report — prds_scanned, prds_marked, prds_would_mark_cancelled, prds_already_marked, automations_marked, left_cancelled, unmatched_ids, errors. Idempotent per merge commit — a repeat run reports the already-recorded ones as prds_already_marked. Set dry_run=true to scan and compute the report without writing any shipped record; absent or false the pass writes (the default)."),
		mcpsdk.WithBoolean("dry_run", mcpsdk.Description("true: scan and compute the report without writing; false or absent: the pass writes (default)")),
	)
}
func (s *Server) handleAutonomousShippedBackfill(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	path := "/api/autonomous/shipped/backfill"
	if req.GetBool("dry_run", false) {
		path += "?dry_run=true"
	}
	out, err := s.proxyJSON(http.MethodPost, path, nil)
	return llmSetterResult(out, err)
}

// toolAutonomousCompletedMigrate + handler serve the operator/LLM "move
// cancelled, merged Automata to Completed" action: it proxies the server's
// POST /api/autonomous/completed/migrate endpoint.
func (s *Server) toolAutonomousCompletedMigrate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_completed_migrate",
		mcpsdk.WithDescription("Move Automata (and Automations) that were cancelled but whose work is already merged to the base branch to status Completed, so they stop showing as cancelled. Never kills a session and never touches an Automaton whose work is not merged. Records are taken oldest first. A real run is refused while a final gate is running. Returns the report: considered, changed, would_complete (dry run) or completed (real run) with each record's title and merge commit, already_completed, and any errors. Set dry_run=true to list what would change without writing anything; limit caps how many records one run changes (0 or absent: no limit); id restricts the run to one Automaton or Automation."),
		mcpsdk.WithBoolean("dry_run", mcpsdk.Description("true: list what would change without writing; false or absent: the run writes (default)")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("most records one run changes; 0 or absent means no limit")),
		mcpsdk.WithString("id", mcpsdk.Description("optional: restrict the run to this one Automaton or Automation id")),
	)
}
func (s *Server) handleAutonomousCompletedMigrate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	v := url.Values{}
	if req.GetBool("dry_run", false) {
		v.Set("dry_run", "true")
	}
	if n := req.GetInt("limit", 0); n != 0 {
		v.Set("limit", strconv.Itoa(n))
	}
	if id := strings.TrimSpace(req.GetString("id", "")); id != "" {
		v.Set("id", id)
	}
	path := "/api/autonomous/completed/migrate"
	if enc := v.Encode(); enc != "" {
		path += "?" + enc
	}
	out, err := s.proxyJSON(http.MethodPost, path, nil)
	return llmSetterResult(out, err)
}

// toolAutonomousShippedCounts + handler read the per-status shipped counts
// (the PRD and Automation buckets) computed under the in-force
// autonomous.shipped settings. It proxies the server's
// GET /api/autonomous/shipped/counts endpoint — read-only, no writes, no
// status touched.
func (s *Server) toolAutonomousShippedCounts() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_shipped_counts",
		mcpsdk.WithDescription("Read the per-status counts — the PRD and Automation buckets — computed under the in-force autonomous.shipped settings. One count per displayed status, while the shipped surface is enabled: a PRD/Automation cancelled without its work landing counts as cancelled, one whose work landed (non-empty merge commit) counts as completed together with the stored-completed ones, even when its stored status is still cancelled (there is no separate shipped bucket). The response also echoes the effective settings (enabled, show_chip, filter_default, count_separately, backfill_on_start) it was computed under. Read-only: it never writes any shipped record and touches no status. Same numbers as the dashboard's shipped card and the autonomous shipped counts CLI verb."),
	)
}
func (s *Server) handleAutonomousShippedCounts(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/shipped/counts", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
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
		mcpsdk.WithDescription("Fetch one automaton. When the completion review blocked it, the response carries rules_review_outcome (action, findings, retries). The detail argument picks how much comes back; the default is summary. "+
			"summary: ids, titles, statuses and counts for the automaton, its stories and its tasks. "+
			"tasks: everything in summary, plus each task's status, error and verification summary. "+
			"full: everything, including specs and logs; the PRD spec, every story description and every task spec always come back whole and uncut at full — that is the level to read from before editing them, and an edit carrying a capped view's truncation marker is refused — while summary and tasks deliberately carry no spec text. "+
			"Parallel execution metrics are reported at every detail level once the automaton has completed: total_tasks, read_only_tasks and speedup (the sum of the task durations divided by the wall-clock time); full also carries concurrent_tasks, wall_clock_seconds and serial_seconds. "+
			"The automaton's git push state is reported at every detail level: summary and tasks carry push_state (pushed / not_pushed) and, for a refused push, the push reason (push_warning) at the top level; full carries the full git object with push_state plus the pushed ref, sha and time (pushed) or the push reason (not_pushed). "+
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
			"It also exposes review_state {state, cause, action} computed from its guardrail and verdict records: state passed and pending say nothing further; blocked pairs a judge's blocking finding with the fix action; timed_out pairs a judge timeout (a timeout is not a block) with the retry or reroute action. "+
			"To resume a blocked automaton: commit your decision (fix, upgrade, or accepted-risk entry) to the repo first, "+
			"then autonomous_prd_reset_task with force=true on the task that pushed, then autonomous_prd_run; "+
			"ci-watch then watches the new commit's CI. If you finished the work by hand, leave it blocked as the record or autonomous_prd_cancel it. "+
			"Each task may carry foreign_file_drift (full detail only; summary and tasks omit it): a list of files the task's own commit swept in that were already uncommitted before the task started and were not in its planned files. "+
			"This is warn-only (the task still completes) unless block_on_file_drift is on for the automaton, its guardrail profile, or the daemon, in which case the task is blocked; worktree-mode automata are never checked. "+
			"Each finding is also recorded in the decision log with kind file_drift_detected. "+
			"Tasks that are actively running (status in_progress or verifying) additionally carry progress {last_event_at, assistant_turns, tool_calls, last_tool_name, last_tool_args, last_summary, tokens} — what the worker has been doing, parsed from its own session log — and, when the worker appears to re-issue the same tool call on consecutive polls, a looping_warning string. "+
			"The absence of the progress key means the task's worker is not currently running (it is stripped the moment a task leaves the active statuses). "+
			"Every detail level also carries cost — the automaton's spend over the whole ledger (tokens, time, sessions, tool calls, billing and window notes), the same row cost_report returns for group_by=automaton; an automaton with no recorded spend gets a zero row."),
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
		mcpsdk.WithDescription("BL328 — start async LLM planning for a PRD. Returns 202 with task_id and stream_url for SSE progress. When the planning call lands on a different pool than the one it was queued for (e.g. you just changed the planning_profile on a running PRD), the 202 body also carries a top-level planning field with the requeue outcome (requeued / deferred_to_next_run / unchanged, plus old_pools and new_pools). Poll /decompose/status or stream /decompose/stream to watch progress."),
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
		mcpsdk.WithDescription("Cancel and archive an Automaton. Kills the PRD's worker sessions; the response JSON includes id, status, killed_sessions (count of sessions a kill was attempted on), killed_live_sessions (how many of those were still live when the cancel began) and killed_session_ids (array of the session IDs a kill was attempted on)."),
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

// Plan-requirements coverage gate — read: fetch one Automaton's stored
// coverage report (proxies GET /api/autonomous/prds/{id}/plan_coverage).
func (s *Server) toolAutonomousPRDPlanCoverageGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_plan_coverage_get",
		mcpsdk.WithDescription("Read one Automaton's stored plan-requirements coverage report (the plan-coverage gate record). Returns the report for that exact decomposed plan — matched and unmatched numbered requirements, any structural findings, and the overall verdict — plus the operator's acknowledgement, and the counts of findings the plan-coverage filters suppressed from the report (suppressed.created_files, suppressed.requirements). Read-only: it changes nothing and runs no gate, judge or scan."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
	)
}
func (s *Server) handleAutonomousPRDPlanCoverageGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("autonomous_prd_plan_coverage_get", "id is required"), nil
	}
	out, err := s.proxyGet("/api/autonomous/prds/"+id+"/plan_coverage", nil)
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
	return withSpecCheckText(textOK(string(out)), nil)
}

func (s *Server) toolAutonomousPRDEditTask() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_edit_task",
		mcpsdk.WithDescription("BL191 — rewrite a task's spec before approving (needs_review / revisions_asked) or after cancelling. BL421: also works on a blocked PRD for the failed/blocked task that halted it, or a task held for review (review_needed) — edit, then autonomous_prd_reset_task (failed/blocked) or autonomous_prd_resolve_task_review action=retry (held) to retry without re-decomposing. Editing never changes the task's status. Completed tasks are immutable in every status. Pass the full spec text: read the existing spec first with the full detail level (autonomous_prd_get, detail=full, which returns task specs whole and uncut); a new_spec carrying a capped view's truncation marker is refused (the daemon answers the same refusal with a 400)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID to rewrite")),
		mcpsdk.WithString("new_spec", mcpsdk.Description("New spec text (omit to leave the spec unchanged when only changing read_only) — the full spec text: read the existing spec first from autonomous_prd_get with detail=full, which returns task specs whole and uncut; a spec carrying a capped view's truncation marker is refused (the daemon answers the same refusal with a 400)")),
		mcpsdk.WithBoolean("read_only", mcpsdk.Description("Mark the task read-only (it changes no files, so it may skip the tree lock) or clear the mark. Omit to leave unchanged.")),
		mcpsdk.WithString("report_path", mcpsdk.Description("Optional: the one repo-relative file the read-only task may write, under .datawatch/reports/. The task must be read_only (set it via read_only in the same call if needed) or it is rejected.")),
		mcpsdk.WithString("verification_script", mcpsdk.Description("Optional: the task's verification script, one repo-relative script file (for example scripts/verify.sh). A command string, an absolute path, a path with a .. segment or the project root is rejected. The path is also added to the task's files_generated when not yet listed. Omit to leave unchanged.")),
		mcpsdk.WithString("verification_script_reason", mcpsdk.Description("Optional: why the task carries no verification script (the exemption the plan check accepts in place of a script), for example a documentation-only change. Can be set with or without verification_script. Omit to leave unchanged.")),
	)
}
func (s *Server) handleAutonomousPRDEditTask(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	// Text copied out of a capped view carries the truncation marker and
	// is not the full spec: refuse it with the daemon's own wording before
	// it is sent (the 400 says the same).
	if newSpec := req.GetString("new_spec", ""); newSpec != "" {
		if msg, refuse := autonomous.TruncationRefusal("new_spec", newSpec); refuse {
			return toolErrorf("autonomous_prd_edit_task", "%s", msg), nil
		}
	}
	editBody := map[string]any{
		"task_id":  req.GetString("task_id", ""),
		"new_spec": req.GetString("new_spec", ""),
		"actor":    "operator",
	}
	if _, present := req.GetArguments()["read_only"]; present {
		editBody["read_only"] = req.GetBool("read_only", false)
	}
	if v := req.GetString("report_path", ""); v != "" {
		editBody["report_path"] = v
	}
	if v := req.GetString("verification_script", ""); v != "" {
		editBody["verification_script"] = v
	}
	if v := req.GetString("verification_script_reason", ""); v != "" {
		editBody["verification_script_reason"] = v
	}
	body, _ := json.Marshal(editBody)
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/edit_task", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDEditStory() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_edit_story",
		mcpsdk.WithDescription("Rewrite a story's title/description before approving. Pass the full description text: read the existing description first with the full detail level (autonomous_prd_get, detail=full, which returns story descriptions whole and uncut); a new_description carrying a capped view's truncation marker is refused (the daemon answers the same refusal with a 400)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story ID to rewrite")),
		mcpsdk.WithString("new_title", mcpsdk.Description("New title (leave empty to keep unchanged)")),
		mcpsdk.WithString("new_description", mcpsdk.Description("New description (leave empty to keep unchanged) — the full description text: read the existing description first from autonomous_prd_get with detail=full, which returns story descriptions whole and uncut; a description carrying a capped view's truncation marker is refused (the daemon answers the same refusal with a 400)")),
	)
}
func (s *Server) handleAutonomousPRDEditStory(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	// Text copied out of a capped view carries the truncation marker and
	// is not the full description: refuse it with the daemon's own wording
	// before it is sent (the 400 says the same).
	if newDescription := req.GetString("new_description", ""); newDescription != "" {
		if msg, refuse := autonomous.TruncationRefusal("new_description", newDescription); refuse {
			return toolErrorf("autonomous_prd_edit_story", "%s", msg), nil
		}
	}
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
		mcpsdk.WithDescription("Add a new, empty story to a PRD without re-running decompose. Structural edits are accepted in needs_review, revisions_asked, cancelled and failed. An Automaton whose completion review blocked with no retry left (or whose fix task could not be created) is parked in needs_review for exactly this purpose, so you can add work and run it again."),
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
		mcpsdk.WithDescription("Delete a story (and its tasks) from a PRD without re-running decompose. Accepted in needs_review, revisions_asked, cancelled and failed; an Automaton parked in needs_review after a blocked completion review can be edited this way."),
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
		mcpsdk.WithDescription("Add a new task to an existing story without re-running decompose. Structural edits are accepted in needs_review, revisions_asked, cancelled and failed. An Automaton whose completion review blocked with no retry left (or whose fix task could not be created) is parked in needs_review for exactly this purpose, so you can add work and run it again."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story ID to add the task to")),
		mcpsdk.WithString("title", mcpsdk.Required(), mcpsdk.Description("Task title")),
		mcpsdk.WithString("spec", mcpsdk.Description("Task spec — concrete instructions for the worker session")),
		mcpsdk.WithArray("depends_on", mcpsdk.Description("Task IDs (within the same PRD) that must complete before this one becomes eligible. Each must already exist."), mcpsdk.WithStringItems()),
		mcpsdk.WithBoolean("read_only", mcpsdk.Description("Mark the task read-only: it changes no files, so it may skip the tree lock. Default false.")),
		mcpsdk.WithString("report_path", mcpsdk.Description("Optional: the one repo-relative file the read-only task may write, under .datawatch/reports/. Requires read_only=true.")),
	)
}
func (s *Server) handleAutonomousPRDAddTask(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	addBody := map[string]any{
		"story_id":   req.GetString("story_id", ""),
		"title":      req.GetString("title", ""),
		"spec":       req.GetString("spec", ""),
		"actor":      "operator",
		"depends_on": req.GetStringSlice("depends_on", nil),
		"read_only":  req.GetBool("read_only", false),
	}
	if v := req.GetString("report_path", ""); v != "" {
		addBody["report_path"] = v
	}
	body, _ := json.Marshal(addBody)
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/add_task", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDRemoveTask() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_remove_task",
		mcpsdk.WithDescription("Delete one task from a story without re-running decompose (distinct from cancel_task, which stops a running/queued task without removing it). Accepted in needs_review, revisions_asked, cancelled and failed. While the Automaton is running, a task that is not running and not queued (cancelled, completed, failed, blocked or never started) can also be removed, and other tasks stop depending on it; a running, queued or waiting task is refused with an error naming its state. An Automaton parked in needs_review after a blocked completion review can be edited this way."),
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
		mcpsdk.WithDescription("v8.23.0 — reset a failed/blocked/waiting-for-capacity task to pending so the autonomous loop retries it. A waiting-for-capacity task needs no force: the daemon re-admits it (same queue position, retargeted pool if the worker resolution changed, fresh wait budget). BL382: pass force=true to requeue a completed or cancelled task. BL451: force=true on an already-pending task in a terminal PRD reopens the PRD to running."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID to reset")),
		mcpsdk.WithBoolean("force", mcpsdk.Description("Set true to requeue completed or cancelled tasks (failed, blocked and waiting-for-capacity tasks never need force)")),
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
		mcpsdk.WithDescription("BL203 — set the PRD-level worker LLM. Tasks inherit unless they have their own override. Pass only the fields to change — an omitted parameter leaves the PRD field unchanged, an empty value clears that field (falling back to the global default). decomposition_profile sets the planning/decompose LLM — any configured LLM works: opencode/claude-code spawn a full session with codebase tool access; ollama/openwebui use headless planning. backend sets the task-execution backend. When a planning field changes the pool the decomposition runs on, the reply also carries a top-level planning field with the requeue outcome (requeued / deferred_to_next_run / unchanged, plus the old and new pool) — on a running Automaton the worker change flows to the tasks that are still queued (override_queued_tasks, on by default), never to tasks already started or finished."),
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
		mcpsdk.WithDescription("BL381 — override the worker LLM for all tasks in one story. Empty string = clear (falls back to PRD then global). Resolution order: per-task → per-story → per-PRD → global. On a locked Automaton status (e.g. running) the call is accepted only when override_queued_tasks is on (the default); the reply then carries a top-level override field listing the changed tasks and the left_alone tasks with a reason each (running/finished are always left alone). When override_queued_tasks is off the same call is refused on a locked status."),
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
		mcpsdk.WithDescription("BL203 — override the worker LLM for a single task. Empty string = clear (falls back to PRD then global). The call is applied only when the task is still queued (a running/finished task is left alone); on a locked Automaton status it requires override_queued_tasks on (the default). The reply carries a top-level override field listing the changed tasks and the left_alone tasks with a reason each."),
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

// parsePriorityByKindTable parses the priority_by_kind tool argument into the
// kind → integer table the REST PUT expects. It runs through the shared
// autonomous.ParsePriorityByKindValue so the MCP, REST and daemon surfaces
// all reject exactly the same values (integer values only — a non-number,
// a fraction, a boolean or out-of-range priority, and an empty kind key,
// are all refused with a clear message).
func parsePriorityByKindTable(v string) (map[string]int, error) {
	return autonomous.ParsePriorityByKindValue(v)
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

// Generated artefacts — four tools proxying the REST routes in
// internal/server/generated_artefacts_rest.go.

func (s *Server) toolGeneratedArtefactsGet() mcpsdk.Tool {
	return mcpsdk.NewTool("generated_artefacts_get",
		mcpsdk.WithDescription("Read the generated-artefact settings in effect (autonomous.generated_artefacts.*) plus each artefact kind with its format description and tracked path. Proxies GET /api/autonomous/generated-artefacts. Read-only."),
	)
}
func (s *Server) handleGeneratedArtefactsGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/generated-artefacts", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDChangeRecord() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_change_record",
		mcpsdk.WithDescription("Read the stored merged change record of one Automaton (files, packages, settings, routes, locale keys, tests, commands run). Proxies GET /api/autonomous/prds/{id}/change-record. Read-only."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
	)
}
func (s *Server) handleAutonomousPRDChangeRecord(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("autonomous_prd_change_record", "id is required"), nil
	}
	out, err := s.proxyGet("/api/autonomous/prds/"+url.PathEscape(id)+"/change-record", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDGeneratedArtefact() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_generated_artefact",
		mcpsdk.WithDescription("Preview one generated artefact for an Automaton: the rendered text, its tracked path and whether the kind is enabled. Nothing is written. Proxies GET /api/autonomous/prds/{id}/generated/{kind}."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Description("Artefact kind: gate_report, changelog_draft, parity_draft, tracker_rows or registry_row")),
	)
}
func (s *Server) handleAutonomousPRDGeneratedArtefact(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	kind := strings.TrimSpace(req.GetString("kind", ""))
	if id == "" || kind == "" {
		return toolErrorf("autonomous_prd_generated_artefact", "id and kind are required"), nil
	}
	out, err := s.proxyGet("/api/autonomous/prds/"+url.PathEscape(id)+"/generated/"+url.PathEscape(kind), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDRegenerateArtefact() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_regenerate_artefact",
		mcpsdk.WithDescription("Re-render one generated artefact for an Automaton and write it to its tracked path. A file a person edited is kept: the result is skipped with the reason. Proxies POST /api/autonomous/prds/{id}/generated/{kind}/regenerate."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Description("Artefact kind: gate_report, changelog_draft, parity_draft, tracker_rows or registry_row")),
	)
}
func (s *Server) handleAutonomousPRDRegenerateArtefact(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	kind := strings.TrimSpace(req.GetString("kind", ""))
	if id == "" || kind == "" {
		return toolErrorf("autonomous_prd_regenerate_artefact", "id and kind are required"), nil
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(id)+"/generated/"+url.PathEscape(kind)+"/regenerate", map[string]any{})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// verifierMaxRunSeconds reads the optional verifier_max_run_seconds argument.
// It reports whether the argument was present; a negative or non-whole value
// is refused rather than truncated.
func verifierMaxRunSeconds(req mcpsdk.CallToolRequest) (int, bool, error) {
	raw, ok := req.GetArguments()["verifier_max_run_seconds"]
	if !ok || raw == nil {
		return 0, false, nil
	}
	v, isNum := raw.(float64)
	if !isNum {
		if n, isInt := raw.(int); isInt {
			v, isNum = float64(n), true
		}
	}
	if !isNum || v < 0 || v != math.Trunc(v) || v > math.MaxInt32 {
		return 0, true, fmt.Errorf("verifier_max_run_seconds: %v is invalid; use a whole number of seconds, 0 for no cap", raw)
	}
	return int(v), true, nil
}
