// BL24+BL25 (v3.10.0) — `datawatch autonomous ...` CLI subcommand
// surface. Thin REST proxies via daemonGet/daemonJSON.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func newAutonomousCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autonomous",
		Short: "Autonomous Automata planning + verification",
		Long: `LLM-driven Product Requirements Document → Stories → Tasks planning,
each Task spawned as a worker session with independent verification.

Subcommands:
  status                          Loop status (running, active PRDs, queued/running tasks)
  progress [--json] [--automation ID] [--tag ID] [--since DATE] [--until DATE]
                                  Progress counts, rates, work left and the completion range
  progress-trend [--days N] [--json]  How the completion range moved over the saved daily snapshots
  lane-fill [--limit N] [--json]  Lane fill: each lane, queued eligible counts and the newest decisions
  triage [--automation ID] [--prd ID] [--status failed|blocked] [--json]
                                  Triage read: the failed and blocked Automata waiting for operator triage,
                                  newest first, beside the alert threshold — the same text the comm verb
                                  (autonomous triage) and the triage alert show
  decisions [automaton-id] [--state open|answered|withdrawn|expired] [--limit N] [--json]
                                  Decision line: the operator decisions, newest first, one line each with
                                  the question, the answer and any task held on it — the same text the
                                  comm verb (autonomous decisions) shows
  repo-ci-watch [--repo P] [--status S] [--json]
                                  Repo CI watch read: the mode, the alert threshold in whole minutes,
                                  the watched repositories, the last poll, and the stored records
                                  (repository, ref, workflow, status, classification, first seen,
                                  alerted), newest first
  merge [--json]                  Merge state: the mode, the alert threshold and the finished Automata whose
                                  work branch is not on main yet (read only)
  safe-to-delete [--json]         Automaton branches and worktrees that are safe to delete, and those held back
                                  (report only, nothing is deleted)
  overlap [--title T] [--spec S | <spec>] [--json]
                                  Search before you create: the Automata, registry rows and branches
                                  that already cover a plan's title and spec, best match first
  config-get                      Read current autonomous config
  config-set <json>               Merge fields into autonomous config, e.g.
                                  config-set '{"max_concurrent_tasks":2}'
                                  config-set '{"max_engines_per_node":2}'
                                  config-set '{"gate_disagreement":"block"}'
                                  config-set --capacity-rate-requests 60 --capacity-rate-tokens 2000000 --capacity-rate-window-seconds 3600
                                  config-set --verifier-policy-mode prefer-free --verifier-policy-backends qwen,claude-code
                                  config-set --judge-policy qwen,claude-code --judge-timeout-seconds 1800
                                  config-set '{"resume_max_age_seconds":1800}'
                                  config-set '{"plan_tier_max_inflight":{"pro":1,"max_5x":2,"max_20x":4,"api":0}}' (tier inflight caps; 0 = unlimited; unknown tiers rejected)
  verifier-policy                 Show the global verifier routing policy
  verifier-policy-set             Set it by flag (--verifier-policy-mode, --verifier-policy-backends, ...)
  prd-set-verifier-policy <id>    Override verifier routing for one Automaton (--mode, --backends)
  prd-set-task-verifier-policy <id> --task <task-id>  Override verifier routing for one task
  prd-list                        List all PRDs
  prd-create <spec>               Create a draft PRD (--type, --kind, --guided-mode, --skills)
  prd-get <id>                    Fetch one PRD with story+task tree
  prd-plan <id>                   Run the LLM planning phase for a PRD (alias: prd-decompose)
  prd-decompose <id>              Run the LLM planning phase for a PRD (alias: prd-plan)
  prd-run <id>                    Kick the executor for a PRD
  prd-cancel <id>                 Cancel + archive a PRD
  failure-escalation show|set|clear|global  Show, set or clear the failure escalation policy (per Automaton, per task, or the daemon default)
  resolve-review <id> --task T --action retry|cancel  Retry or cancel a task held for operator review (status lists held tasks under review_needed)
  prd-delete <id>                 Hard-delete a PRD + descendants
  prd-edit <id>                   Edit PRD title + spec
  prd-approve <id>                Approve planned PRD
  prd-plan-review <id>            Re-run the plan reviewer for a PRD (alias: plan-review)
  prd-resume <id> [--note ...]    Resume a paused or boot-parked PRD (alias: resume)
  prd-reject <id>                 Reject a PRD plan
  prd-coverage <id>               Show the stored plan-requirement coverage report (mode, min_ratio, report, ack, findings)
  prd-ack-coverage <id>           Acknowledge a failing coverage report (--actor, --note) so approve in block mode is allowed
  prd-request-revision <id>       Request fresh planning run
  prd-edit-task <id>              Rewrite a task spec before approving
  prd-edit-story <id>             Rewrite a story's title/description before approving
  prd-add-story <id>              Add a new story without re-running decompose
  prd-remove-story <id>           Remove a story (and its tasks) without re-running decompose
  prd-add-task <id>               Add a new task to an existing story without re-running decompose
  prd-remove-task <id>            Remove one task from a story without re-running decompose
  prd-set-type <id> <type>        Set automaton type (BL221 Phase 4)
  prd-set-kind <id> <kind>        Set the Automaton kind: feature, bug or other
  prd-set-lane-preference <id> gpu|claude|any  Set which lane an Automaton may take
  prd-set-guided-mode <id> on|off Enable/disable Guided Mode (BL221 Phase 4)
  tag-list [--counts]             List the tag tree (--counts adds the Automata and session totals per tag)
  tag-create <name>               Create a tag (--parent, --color, --description)
  tag-rename <id> <new-name>      Rename a tag
  tag-move <id> --parent <id|root>  Move a tag under another parent
  tag-color <id> <color>          Set a tag's color
  tag-delete <id>                 Delete a tag
  tag-registry                    Show the tag registry status (role, keeper, staleness, revision, notes, last error)
  tag-registry-dismiss <note-id>  Dismiss one tag registry note
  prd-set-tags <id> [--tags a,b] [--primary <tag-id>]  Set an Automaton's tags and its primary tag
  attention                       List the Automata that need attention (blocked, needs review, paused)
  prd-set-notify-events <id> [kinds]  Set the event kinds that produce notifications for an Automaton (inherit or omit to clear)
  prd-set-git <id> on|off         Set AutoPR (+--base-branch override) for a PRD (BL407 Phase 5)
  prd-set-skills <id> <csv>       Assign skills to PRD (BL221 Phase 4)
  prd-scan <id>                   Trigger security scan (BL221 Phase 3)
  prd-scan-result <id>            Get latest scan result (BL221 Phase 3)
  prd-scan-fix <id>               Create fix child PRD (BL221 Phase 3b)
  prd-scan-rules <id>             Propose AGENT.md rule edits (BL221 Phase 3b)
  scan-config-get                 Read scan config (BL221 Phase 3)
  scan-config-set <k=v,...>       Update scan config fields (BL221 Phase 3)
  types-list                      List automaton type registry (BL221 Phase 4)
  type-register                   Register or update an automaton type (BL221 Phase 4)
  template-list                   List template store (BL221 Phase 2)
  template-create                 Create a template (BL221 Phase 2)
  template-get <id>               Fetch one template (BL221 Phase 2)
  template-update <id>            Update a template (BL221 Phase 2)
  template-delete <id>            Delete a template (BL221 Phase 2)
  template-instantiate <id>       Create PRD from template (BL221 Phase 2)
  template-clone <prd-id>         Clone PRD to template (BL221 Phase 2)
  prd-instantiate <tmpl-id>       Instantiate legacy YAML template (BL191)
  learnings                       List extracted post-task learnings
  subscriptions [--owner X]       List persisted lifecycle event subscriptions (BL427)
  subscribe --on <kind> ...       Subscribe to lifecycle events → sink: comm|session|topic|alert|mcp_queue (BL427)
                                  shorthand: --task <id> --on session_started --send "<text>"
  unsubscribe <id>                Delete a lifecycle event subscription by id (BL427)
  events --queue <name>           Read pending events from an mcp_queue queue [--since <cursor>] [--limit n] [--json]
`,
	}
	cmd.AddCommand(
		newAutonomousProgressCmd(),
		newAutonomousProgressTrendCmd(),
		newAutonomousLaneFillCmd(),
		newAutonomousStatusCmd(),
		newAutonomousConfigGetCmd(),
		newAutonomousConfigSetCmd(),
		newAutonomousPRDListCmd(),
		newAutonomousPRDCreateCmd(),
		newAutonomousPRDGetCmd(),
		newAutonomousPRDPlanCmd(),
		newAutonomousPRDRunCmd(),
		newAutonomousPRDCancelCmd(),
		newAutonomousPRDApproveCmd(),
		newAutonomousPRDRejectCmd(),
		newAutonomousPRDCoverageCmd(),
		newAutonomousPRDAckCoverageCmd(),
		newAutonomousPRDPlanReviewCmd(),
		newAutonomousPRDResumeCmd(),
		newAutonomousPRDRequestRevisionCmd(),
		newAutonomousPRDEditTaskCmd(),
		newAutonomousPRDEditStoryCmd(),
		newAutonomousPRDAddStoryCmd(),
		newAutonomousPRDRemoveStoryCmd(),
		newAutonomousPRDAddTaskCmd(),
		newAutonomousPRDRemoveTaskCmd(),
		newAutonomousPRDInstantiateCmd(),
		newAutonomousPRDSetLLMCmd(),
		newAutonomousPRDSetStoryLLMCmd(),
		newAutonomousPRDSetTaskLLMCmd(),
		newAutonomousFailureEscalationCmd(),
		newAutonomousResolveReviewCmd(),
		newAutonomousPRDSetThinkingLevelCmd(),
		newAutonomousPRDSetStoryThinkingLevelCmd(),
		newAutonomousPRDSetTaskThinkingLevelCmd(),
		newAutonomousPRDSetTaskProjectDirCmd(),
		newAutonomousPRDSetTaskVerificationLLMCmd(),
		newAutonomousPRDSetStoryVerificationLLMCmd(),
		newAutonomousPRDSetVerificationLLMCmd(),
		newAutonomousVerifierPolicyCmd(),
		newAutonomousVerifierPolicySetCmd(),
		newAutonomousPRDSetVerifierPolicyCmd(),
		newAutonomousPRDSetTaskVerifierPolicyCmd(),
		newAutonomousLearningsCmd(),
		newAutonomousPRDChildrenCmd(),
		newAutonomousPRDDeleteCmd(),
		newAutonomousPRDEditCmd(),
		// BL221 Phase 3 — scan framework.
		newAutonomousPRDScanCmd(),
		newAutonomousPRDScanResultCmd(),
		newAutonomousPRDScanFixCmd(),
		newAutonomousPRDScanRulesCmd(),
		newAutonomousScanConfigGetCmd(),
		newAutonomousScanConfigSetCmd(),
		// BL221 Phase 4 — type registry, Guided Mode, skills.
		newAutonomousPRDSetTypeCmd(),
		newAutonomousPRDSetKindCmd(),
		newAutonomousPRDSetLanePreferenceCmd(),
		newAutonomousTagListCmd(),
		newAutonomousTagCreateCmd(),
		newAutonomousTagRenameCmd(),
		newAutonomousTagMoveCmd(),
		newAutonomousTagColorCmd(),
		newAutonomousTagDeleteCmd(),
		newAutonomousTagRegistryCmd(),
		newAutonomousTagRegistryDismissCmd(),
		newAutonomousAttentionCmd(),
		newAutonomousPRDSetTagsCmd(),
		newAutonomousPRDSetGuidedModeCmd(),
		newAutonomousPRDSetNotifyEventsCmd(),
		newAutonomousPRDSetGitCmd(),
		newAutonomousPRDSetSkillsCmd(),
		newAutonomousTypesListCmd(),
		newAutonomousTypeRegisterCmd(),
		// BL221 Phase 2 — template store.
		newAutonomousTemplateListCmd(),
		newAutonomousTemplateCreateCmd(),
		newAutonomousTemplateGetCmd(),
		newAutonomousTemplateUpdateCmd(),
		newAutonomousTemplateDeleteCmd(),
		newAutonomousTemplateInstantiateCmd(),
		newAutonomousTemplateCloneCmd(),
		// BL427 lifecycle-event subscriptions.
		newAutonomousSubscriptionsCmd(),
		newAutonomousSubscribeCmd(),
		newAutonomousUnsubscribeCmd(),
		newAutonomousSubscriptionUpdateCmd(),
		newAutonomousSubscriptionTestCmd(),
		newAutonomousEventsCmd(),
		// Triage read (docs/plans/2026-10-26-autonomous-triage-read-route.md).
		newAutonomousTriageCmd(),
		// Repo CI watch read.
		newAutonomousRepoCIWatchCmd(),
		newAutonomousDecisionsCmd(),
		newAutonomousMergeCmd(),
		newAutonomousSafeToDeleteCmd(),
		// Search-before-create read (docs/plans/2026-10-29-search-before-create.md).
		newAutonomousOverlapCmd(),
	)
	return cmd
}

func newAutonomousStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Loop status snapshot",
		Long: `Loop status snapshot as JSON.

The review_needed array lists every task held for operator review across all
Automata, oldest hold first: automaton_id, automaton_title, task_id, task_title,
failure_count, last_backend, last_model and held_at. It is omitted when no task
is held. Decide a held task with: datawatch autonomous resolve-review <id> --task <task-id> --action retry|cancel`,
		RunE: func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/status") },
	}
}

func newAutonomousConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config-get",
		Short: "Read autonomous config",
		Long: `Read the current autonomous config.

The concurrency keys are max_concurrent_tasks (tasks of one Automaton that
run at once; 1 = sequential) and max_engines_per_node (engine sessions one
compute node runs at once; 1 = one engine per node, the default).

resume_max_age_seconds is the boot-resume age limit in seconds (default
3600): a completed task older than that is no longer eligible for resume.
0 = never resume, -1 = no age limit.

gate_disagreement (default "warn") applies when the verifier passed a task
but the quality gate found a regression: "warn" records task.gate_disagreement
and a PRD decision-log entry and the task still completes; "block" fails the
task with a "gate_disagreement: …" error. An invalid gate never blocks.`,
		RunE: func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/config") },
	}
}

// autonomousRateFlags maps each rate-ceiling flag of config-set to the config
// key it writes. The flags are sent only when set, so an omitted flag leaves
// the key unchanged and an explicit 0 clears that ceiling.
var autonomousRateFlags = map[string]string{
	"capacity-rate-requests":       "capacity_rate_requests",
	"capacity-rate-tokens":         "capacity_rate_tokens",
	"capacity-rate-window-seconds": "capacity_rate_window_seconds",
}

// applyAutonomousRateFlags copies the rate-ceiling flags that were set onto
// body, rejecting negative values before anything is sent.
func applyAutonomousRateFlags(cmd *cobra.Command, body map[string]any) error {
	for flag, key := range autonomousRateFlags {
		if !cmd.Flags().Changed(flag) {
			continue
		}
		n, _ := cmd.Flags().GetInt(flag)
		if n < 0 {
			return fmt.Errorf("--%s must be >= 0", flag)
		}
		body[key] = n
	}
	return nil
}

// applyAutonomousJudgeFlags copies the judge flags that were set onto body:
// --judge-policy (an ordered comma list; an explicit empty value clears it so
// the judges follow the verifier routing policy) and --judge-timeout-seconds (the
// time limit; a negative value is rejected before anything is sent) and
// --judge-size-threshold-lines (the changed-line count above which the judges
// route by change size; 0 turns the rule off, and a negative value is rejected).
// A flag that was not given adds no key to body.
func applyAutonomousJudgeFlags(cmd *cobra.Command, body map[string]any) error {
	if cmd.Flags().Changed("judge-policy") {
		s, _ := cmd.Flags().GetString("judge-policy")
		body["judge_policy"] = splitVerifierBackends(s)
	}
	if cmd.Flags().Changed("judge-timeout-seconds") {
		n, _ := cmd.Flags().GetInt("judge-timeout-seconds")
		if n < 0 {
			return fmt.Errorf("--judge-timeout-seconds must be >= 0")
		}
		body["judge_timeout_seconds"] = n
	}
	if cmd.Flags().Changed("judge-size-threshold-lines") {
		n, _ := cmd.Flags().GetInt("judge-size-threshold-lines")
		if n < 0 {
			return fmt.Errorf("--judge-size-threshold-lines must be >= 0")
		}
		body["judge_size_threshold_lines"] = n
	}
	return nil
}

// verifierPolicyFlagDefs describes the verifier routing flags: the flag name,
// the autonomous.verifier_policy field it writes and its kind. They are shared
// by config-set and verifier-policy-set.
var verifierPolicyFlagDefs = []struct{ flag, field, kind string }{
	{"verifier-policy-mode", "mode", "string"},
	{"verifier-policy-backends", "backends", "list"},
	{"verifier-policy-claude-session-cap", "claude_session_cap", "int"},
	{"verifier-policy-claude-cap-window-minutes", "claude_cap_window_minutes", "int"},
	{"verifier-policy-force-stronger-on-retry", "force_stronger_on_retry", "bool"},
	{"verifier-policy-final-gate-stronger", "final_gate_stronger", "bool"},
	{"verifier-policy-stronger-backend", "stronger_backend", "string"},
	{"verifier-policy-stronger-model", "stronger_model", "string"},
}

// addVerifierPolicyFlags registers the verifier routing flags on cmd.
func addVerifierPolicyFlags(cmd *cobra.Command) {
	cmd.Flags().String("verifier-policy-mode", "", "Verifier routing mode: fixed, prefer-free, balance, gpu-only or claude-only (empty = fixed)")
	cmd.Flags().String("verifier-policy-backends", "", "Ordered comma list of eligible verifier backends (empty = use verification_backends)")
	cmd.Flags().Int("verifier-policy-claude-session-cap", 0, "Most Claude verifier sessions started per window (0 = unlimited)")
	cmd.Flags().Int("verifier-policy-claude-cap-window-minutes", 0, "Window length in minutes for the Claude session cap (0 = default 60)")
	cmd.Flags().Bool("verifier-policy-force-stronger-on-retry", false, "Send a retried verification to the stronger verifier")
	cmd.Flags().Bool("verifier-policy-final-gate-stronger", false, "Send the final verification gate to the stronger verifier")
	cmd.Flags().String("verifier-policy-stronger-backend", "", "Stronger verifier backend (empty = claude-code)")
	cmd.Flags().String("verifier-policy-stronger-model", "", "Stronger verifier model (empty = the default Sonnet model)")
}

// applyVerifierPolicyFlags collects the verifier routing flags that were set
// into a verifier_policy object on body. An omitted flag leaves its field
// unchanged and an explicit empty value, 0 or false clears it. A negative
// number is rejected before anything is sent. Fields already in a JSON
// verifier_policy object on body are kept.
func applyVerifierPolicyFlags(cmd *cobra.Command, body map[string]any) error {
	vp := map[string]any{}
	if cur, ok := body["verifier_policy"].(map[string]any); ok {
		vp = cur // flags win over the same field given in the JSON body
	}
	for _, d := range verifierPolicyFlagDefs {
		if !cmd.Flags().Changed(d.flag) {
			continue
		}
		switch d.kind {
		case "int":
			n, _ := cmd.Flags().GetInt(d.flag)
			if n < 0 {
				return fmt.Errorf("--%s must be >= 0", d.flag)
			}
			vp[d.field] = n
		case "bool":
			b, _ := cmd.Flags().GetBool(d.flag)
			vp[d.field] = b
		case "list":
			s, _ := cmd.Flags().GetString(d.flag)
			vp[d.field] = splitVerifierBackends(s)
		default:
			s, _ := cmd.Flags().GetString(d.flag)
			vp[d.field] = s
		}
	}
	if len(vp) > 0 {
		body["verifier_policy"] = vp
	}
	return nil
}

// splitVerifierBackends splits a comma list of backend names. An empty string
// is an empty list (clear); empty entries are kept so the daemon rejects them
// rather than a stray comma quietly shrinking the list.
func splitVerifierBackends(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func newAutonomousVerifierPolicyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verifier-policy",
		Short: "Show the global verifier routing policy (autonomous.verifier_policy)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			raw, err := daemonJSONBody(http.MethodGet, "/api/autonomous/config", nil)
			if err != nil {
				return err
			}
			var cfg map[string]json.RawMessage
			if json.Unmarshal(raw, &cfg) != nil {
				prettyPrint(raw)
				return nil
			}
			if vp, ok := cfg["verifier_policy"]; ok {
				prettyPrint(vp)
				return nil
			}
			fmt.Println("{}")
			return nil
		},
	}
}

func newAutonomousVerifierPolicySetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verifier-policy-set",
		Short: "Set the global verifier routing policy; only the flags you give change",
		Long: `Set fields of autonomous.verifier_policy, which chooses the backend that
runs each verification. Fields you do not give are left unchanged; an explicit
empty value, 0 or false clears that field. The change takes effect on the next
verification, with no restart.

  datawatch autonomous verifier-policy-set --verifier-policy-mode prefer-free --verifier-policy-backends qwen,claude-code
  datawatch autonomous verifier-policy-set --verifier-policy-claude-session-cap 6 --verifier-policy-claude-cap-window-minutes 60
  datawatch autonomous verifier-policy-set --verifier-policy-final-gate-stronger --verifier-policy-stronger-backend claude-code

Modes: fixed (first eligible backend only, the default), prefer-free (first
backend with a free slot), balance (lowest relative load), gpu-only (local
backends only), claude-only (Claude backends only).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{}
			if err := applyVerifierPolicyFlags(cmd, body); err != nil {
				return err
			}
			if len(body) == 0 {
				return fmt.Errorf("give at least one --verifier-policy-* flag")
			}
			return daemonJSON(http.MethodPut, "/api/autonomous/config", body)
		},
	}
	addVerifierPolicyFlags(cmd)
	return cmd
}

// verifierPolicyOverrideCLIBody builds a set_verifier_policy /
// set_task_verifier_policy body. mode and backends are sent only when their
// flag was set, so an omitted flag leaves the field unchanged and an explicit
// empty value clears it.
func verifierPolicyOverrideCLIBody(cmd *cobra.Command, base map[string]any) map[string]any {
	body := map[string]any{}
	for k, v := range base {
		body[k] = v
	}
	if cmd.Flags().Changed("mode") {
		v, _ := cmd.Flags().GetString("mode")
		body["mode"] = v
	}
	if cmd.Flags().Changed("backends") {
		v, _ := cmd.Flags().GetString("backends")
		body["backends"] = splitVerifierBackends(v)
	}
	return body
}

func addVerifierPolicyOverrideFlags(cmd *cobra.Command, scope string) {
	cmd.Flags().String("mode", "", "verifier routing mode for "+scope+" (fixed, prefer-free, balance, gpu-only, claude-only); empty = clear, omit = unchanged")
	cmd.Flags().String("backends", "", "ordered comma list of verifier backends for "+scope+"; empty = clear, omit = unchanged")
}

func newAutonomousPRDSetVerifierPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prd-set-verifier-policy <prd-id> [--mode M --backends a,b]",
		Short: "Override the verifier routing of an Automaton. Precedence: task > Automaton > global verifier_policy. Omitted flags are unchanged; an empty value clears.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := verifierPolicyOverrideCLIBody(cmd, map[string]any{"actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_verifier_policy", body)
		},
	}
	addVerifierPolicyOverrideFlags(cmd, "this Automaton")
	return cmd
}

func newAutonomousPRDSetTaskVerifierPolicyCmd() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "prd-set-task-verifier-policy <prd-id> --task <task-id> [--mode M --backends a,b]",
		Short: "Override the verifier routing of one task. Precedence: task > Automaton > global verifier_policy. Omitted flags are unchanged; an empty value clears.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := verifierPolicyOverrideCLIBody(cmd, map[string]any{"task_id": taskID, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_task_verifier_policy", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to override (required)")
	addVerifierPolicyOverrideFlags(cmd, "this task")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newAutonomousConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config-set [json-or-@file]",
		Short: "Merge fields into autonomous config",
		Long: `Merge the given JSON fields onto the current autonomous config; fields
not given are left unchanged.

Example — let up to 2 independent tasks of one Automaton run at once
(daemon-wide default; an Automaton's own setting overrides it; 1 = sequential):

  datawatch autonomous config-set '{"max_concurrent_tasks":2}'

Example — let each compute node run up to 2 engine sessions at once
(default 1; values below 1 are clamped to 1):

  datawatch autonomous config-set '{"max_engines_per_node":2}'

Example — make completed tasks older than 30 minutes no longer eligible
for resume (default 3600; 0 = never resume, -1 = no age limit):

  datawatch autonomous config-set '{"resume_max_age_seconds":1800}'

Example — verifier passed a task but the quality gate found a regression:
fail the task instead of completing it with a recorded gate_disagreement.
Only "warn" (the default; record and complete) or "block" is accepted:

  datawatch autonomous config-set '{"gate_disagreement":"block"}'

tier inflight-cap table (see "datawatch llm add|update --help" for --plan-tier/--max-inflight):
  datawatch autonomous config-set '{"plan_tier_max_inflight":{"max_5x":2,"api":0}}'
      Ties each subscription plan tier (pro | max_5x | max_20x | api) to an
      inflight cap (0 = unlimited). An explicit per-LLM max_inflight > 0 wins
      over the tier cap; clear the table with {}.

Judge routing, which chooses the backends the judges (Automaton guardrail,
plan review and other autonomous judges) try. --judge-policy is an ordered comma
list, one attempt per backend; an empty value clears it, and the judges then
follow the verifier routing policy. --judge-timeout-seconds is the time limit
for one judge run (default 1800 = 30 minutes; 0 = default; the wait for a
capacity slot does not count):

  datawatch autonomous config-set --judge-policy qwen,claude-code --judge-timeout-seconds 1800

  judge_policy                    ordered judge backends (empty = follow the verifier routing policy)
  judge_timeout_seconds           time limit for one judge run (default 1800)
  judge_size_threshold_lines      changed-line count above which the judges route by change size (default 400; 0 = off)

Rate ceiling on each LLM's pool, by flag or by JSON key (0 = no ceiling,
the default; negative values are rejected):

  datawatch autonomous config-set --capacity-rate-requests 60 --capacity-rate-tokens 2000000 --capacity-rate-window-seconds 3600

  capacity_rate_requests          most task/verifier starts per LLM per window
  capacity_rate_tokens            most tokens per LLM per window
  capacity_rate_window_seconds    rolling window length (default 3600)

Verifier routing, which chooses the backend that runs each verification, by
flag or by the verifier_policy JSON object. Fields you do not give are left
unchanged; the change takes effect on the next verification:

  datawatch autonomous config-set --verifier-policy-mode prefer-free --verifier-policy-backends qwen,claude-code
  datawatch autonomous config-set '{"verifier_policy":{"claude_session_cap":6}}'

  verifier_policy.mode                       fixed (default), prefer-free, balance, gpu-only, claude-only
  verifier_policy.backends                   ordered list of eligible verifier backends
  verifier_policy.claude_session_cap         most Claude verifier sessions per window (0 = unlimited)
  verifier_policy.claude_cap_window_minutes  window length for that cap (0 = default 60)
  verifier_policy.force_stronger_on_retry    send a retried verification to the stronger verifier
  verifier_policy.final_gate_stronger        send the final verification gate to the stronger verifier
  verifier_policy.stronger_backend           stronger verifier backend (empty = claude-code)
  verifier_policy.stronger_model             stronger verifier model (empty = the default Sonnet model)

Example — set the pre-spawn scope lint to warn (spawn the task and record a
scope_lint_warn decision) instead of blocking it:

  datawatch autonomous config-set '{"scope_lint":"warn"}'

scope_lint takes one of three values: off (skip the lint), warn (spawn the
task and record a scope_lint_warn decision) or enforce (block the task before
its session starts; the default). Any other value is rejected.

max_parallel_tasks is deprecated and ignored; use max_concurrent_tasks.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if len(args) == 1 {
				raw := args[0]
				if len(raw) > 1 && raw[0] == '@' {
					b, err := os.ReadFile(raw[1:])
					if err != nil {
						return err
					}
					raw = string(b)
				}
				if err := json.Unmarshal([]byte(raw), &body); err != nil {
					return fmt.Errorf("parse JSON: %w", err)
				}
			}
			if err := applyAutonomousRateFlags(cmd, body); err != nil {
				return err
			}
			if err := applyAutonomousJudgeFlags(cmd, body); err != nil {
				return err
			}
			if err := applyVerifierPolicyFlags(cmd, body); err != nil {
				return err
			}
			if len(body) == 0 {
				return fmt.Errorf("give a JSON body or at least one flag")
			}
			resp, err := daemonJSONBody(http.MethodPut, "/api/autonomous/config", body)
			if err != nil {
				return err
			}
			prettyPrint(resp)
			printDeprecatedFields(resp)
			return nil
		},
	}
	cmd.Flags().Int("capacity-rate-requests", 0, "Most task/verifier starts per LLM pool per rolling window (0 = no requests ceiling)")
	cmd.Flags().Int("capacity-rate-tokens", 0, "Most tokens per LLM pool per rolling window (0 = no tokens ceiling)")
	cmd.Flags().Int("capacity-rate-window-seconds", 0, "Rolling window length in seconds for the rate ceilings (0 = default 3600)")
	cmd.Flags().String("judge-policy", "", "Ordered comma list of judge backends, one attempt each (empty = follow the verifier routing policy)")
	cmd.Flags().Int("judge-timeout-seconds", 0, "Time limit in seconds for one judge run (0 = default 1800 = 30m)")
	cmd.Flags().Int("judge-size-threshold-lines", 400, "Changed-line count above which the judges route by change size (whole number; 0 = rule off)")
	addVerifierPolicyFlags(cmd)
	return cmd
}

// printDeprecatedFields writes any deprecated_fields note from a config
// PUT response to stderr so a deprecated key never fails silently.
func printDeprecatedFields(resp []byte) {
	var r struct {
		DeprecatedFields map[string]string `json:"deprecated_fields"`
	}
	if json.Unmarshal(resp, &r) != nil {
		return
	}
	for k, note := range r.DeprecatedFields {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", k, note)
	}
}

func newAutonomousPRDListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-list",
		Short: "List all PRDs (newest first)",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/prds?full=1") },
	}
}

func newAutonomousPRDCreateCmd() *cobra.Command {
	var projectDir, backend, effort, typ, skillsCSV, kind, lanePref string
	var guidedMode bool
	cmd := &cobra.Command{
		Use:   "prd-create <spec>",
		Short: "Create a draft PRD from a feature description",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			spec := joinArgs(args)
			body := map[string]any{
				"spec":        spec,
				"project_dir": projectDir,
				"backend":     backend,
				"effort":      effort,
			}
			if typ != "" {
				body["type"] = typ
			}
			if kind != "" {
				k, err := parseKindArg(kind)
				if err != nil {
					return err
				}
				body["kind"] = k
			}
			if lanePref != "" {
				p, err := parseLanePreferenceArg(lanePref)
				if err != nil {
					return err
				}
				body["lane_preference"] = p
			}
			if guidedMode {
				body["guided_mode"] = true
			}
			if skillsCSV != "" {
				var skills []string
				for _, s := range strings.Split(skillsCSV, ",") {
					if s = strings.TrimSpace(s); s != "" {
						skills = append(skills, s)
					}
				}
				body["skills"] = skills
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds", body)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory; omit to let the daemon apply its default (worktree mode) instead of assuming the current directory")
	cmd.Flags().StringVar(&backend, "backend", "", "LLM backend override")
	cmd.Flags().StringVar(&effort, "effort", "", "Effort hint (low/medium/high/max)")
	cmd.Flags().StringVar(&typ, "type", "", "Automaton type (software|research|operational|personal or custom)")
	cmd.Flags().StringVar(&kind, "kind", "", "Automaton kind (feature|bug|other); omit to derive it from the title and spec")
	cmd.Flags().StringVar(&lanePref, "lane-preference", "", "Lane the Automaton may take (gpu|claude|any); omit for any")
	cmd.Flags().BoolVar(&guidedMode, "guided-mode", false, "Enable Guided Mode (step-by-step operator checkpoints)")
	cmd.Flags().StringVar(&skillsCSV, "skills", "", "Comma-separated skill IDs (e.g. git,docker,pytest)")
	return cmd
}

func newAutonomousPRDGetCmd() *cobra.Command {
	var detail string
	cmd := &cobra.Command{
		Use:   "prd-get <id>",
		Short: "Fetch one PRD with its story+task tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := "/api/autonomous/prds/" + args[0]
			if detail != "" {
				path += "?" + url.Values{"detail": {detail}}.Encode()
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().StringVar(&detail, "detail", "", "Detail level: summary, tasks or full (default: the daemon's full view)")
	return cmd
}

func newAutonomousPRDPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "prd-plan <id>",
		Short:   "Run the LLM planning phase for a PRD (alias: prd-decompose)",
		Aliases: []string{"prd-decompose"},
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/decompose", nil)
		},
	}
}

func newAutonomousPRDRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-run <id>",
		Short: "Kick the executor for a PRD",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/run", nil)
		},
	}
}

func newAutonomousPRDCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-cancel <id>",
		Short: "Cancel + archive a PRD",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/autonomous/prds/"+args[0], nil)
		},
	}
}

func newAutonomousLearningsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "learnings",
		Short: "List extracted post-task learnings",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/learnings") },
	}
}

// v5.19.0 — full CRUD: prd-delete (hard) + prd-edit (title/spec).
func newAutonomousPRDDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-delete <id>",
		Short: "Permanently remove a PRD + its SpawnPRD descendants (use prd-cancel for reversible cancel)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/autonomous/prds/"+args[0]+"?hard=true", nil)
		},
	}
}

func newAutonomousPRDEditCmd() *cobra.Command {
	var title, spec string
	cmd := &cobra.Command{
		Use:   "prd-edit <id>",
		Short: "Edit PRD title + spec (non-running PRDs only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{"actor": "operator"}
			if title != "" {
				body["title"] = title
			}
			if spec != "" {
				body["spec"] = spec
			}
			return daemonJSON(http.MethodPatch, "/api/autonomous/prds/"+args[0], body)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "New PRD title")
	cmd.Flags().StringVar(&spec, "spec", "", "New PRD spec (full replacement)")
	return cmd
}

// BL191 Q4 (v5.9.0) — recursion: list child PRDs spawned from a parent's
// SpawnPRD tasks. Genealogy tree visibility from CLI without crawling
// the full PRD list.
func newAutonomousPRDChildrenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-children <id>",
		Short: "List child PRDs spawned from a parent PRD's SpawnPRD tasks",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/autonomous/prds/" + args[0] + "/children")
		},
	}
}

// BL191 (v5.2.0) — review/approve/reject/edit-task/instantiate-template.

func newAutonomousPRDApproveCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "prd-approve <id>",
		Short: "Approve a planned PRD so Run is allowed",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"actor": "operator", "note": note})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/approve", body)
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "free-form note saved on the Decision row")
	return cmd
}

func newAutonomousPRDRejectCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "prd-reject <id>",
		Short: "Reject a PRD; the plan stays for inspection but never runs",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"actor": "operator", "reason": reason})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/reject", body)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "operator-supplied rejection reason")
	return cmd
}

// newAutonomousPRDCoverageCmd — show the PRD's stored plan-requirement
// coverage report (the plan-coverage gate record: the mode/min_ratio
// the PRD was pinned to, the coverage report, any structural findings
// and the operator acknowledgement of a failing surface, if present).
func newAutonomousPRDCoverageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-coverage <id>",
		Short: "Show the PRD's stored plan-coverage report (gate mode, ratio, report, ack, findings)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, err := daemonJSONBody(http.MethodGet, "/api/autonomous/prds/"+args[0]+"?detail=full", nil)
			if err != nil {
				return err
			}
			var prd struct {
				ID           string          `json:"id"`
				Status       string          `json:"status"`
				PlanCoverage json.RawMessage `json:"plan_coverage"`
			}
			if err := json.Unmarshal(body, &prd); err != nil {
				return fmt.Errorf("decode PRD: %w", err)
			}
			if len(prd.PlanCoverage) == 0 || string(prd.PlanCoverage) == "null" {
				fmt.Printf("No plan-coverage report stored for %s (run the planning phase first; the deterministic coverage check is computed at decompose time).\n", prd.ID)
				return nil
			}
			var report any
			if err := json.Unmarshal(prd.PlanCoverage, &report); err != nil {
				return fmt.Errorf("decode plan_coverage: %w", err)
			}
			out, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return fmt.Errorf("re-marshal plan_coverage: %w", err)
			}
			prettyPrint(out)
			return nil
		},
	}
}

// newAutonomousPRDAckCoverageCmd — operator-acknowledge the PRD's
// failing plan-coverage report (the block-mode gate). The manager pins
// the ack to the exact failing surface (fingerprint); a non-empty note
// is required. See the REST endpoint
// POST /api/autonomous/prds/{id}/plan_coverage/acknowledge.
func newAutonomousPRDAckCoverageCmd() *cobra.Command {
	var actor, note string
	cmd := &cobra.Command{
		Use:   "prd-ack-coverage <id>",
		Short: "Acknowledge a failing plan-coverage report so approve is allowed (block mode)",
		Long: "Operator-acknowledge the PRD's stored failing plan-requirement coverage report. " +
			"The ack is pinned to the exact failing surface at the time it is recorded (unmatched requirements + structural findings), " +
			"so approving after the plan changed needs a fresh ack. " +
			"Use prd-coverage <id> to read the report first.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if strings.TrimSpace(note) == "" {
				return fmt.Errorf("a non-empty --note is required to acknowledge a failing plan-coverage report")
			}
			if actor == "" {
				actor = "operator"
			}
			body, _ := json.Marshal(map[string]string{"actor": actor, "note": note})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/plan_coverage/acknowledge", body)
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "operator", "operator identity recorded on the ack")
	cmd.Flags().StringVar(&note, "note", "", "non-empty justification for accepting the failing coverage surface")
	return cmd
}

func newAutonomousPRDRequestRevisionCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "prd-request-revision <id>",
		Short: "Ask for a fresh planning run; status moves back to revisions_asked",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"actor": "operator", "note": note})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/request_revision", body)
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "what's wrong with the current plan")
	return cmd
}

func newAutonomousPRDEditTaskCmd() *cobra.Command {
	var taskID, newSpec string
	cmd := &cobra.Command{
		Use:   "prd-edit-task <prd-id> --task <task-id> --spec <new-spec>",
		Short: "Rewrite a task's spec before approving",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"task_id": taskID, "new_spec": newSpec, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/edit_task", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to rewrite (required)")
	cmd.Flags().StringVar(&newSpec, "spec", "", "new task spec text (required)")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("spec")
	return cmd
}

func newAutonomousPRDEditStoryCmd() *cobra.Command {
	var storyID, newTitle, newDescription string
	cmd := &cobra.Command{
		Use:   "prd-edit-story <prd-id> --story <story-id> [--title <t>] [--description <d>]",
		Short: "Rewrite a story's title/description before approving",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"story_id": storyID, "new_title": newTitle, "new_description": newDescription, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/edit_story", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to rewrite (required)")
	cmd.Flags().StringVar(&newTitle, "title", "", "new title (empty = keep unchanged)")
	cmd.Flags().StringVar(&newDescription, "description", "", "new description (empty = keep unchanged)")
	_ = cmd.MarkFlagRequired("story")
	return cmd
}

// Structural edits (operator-requested: add/remove a story or task
// without re-running decompose and handing the structure back to the
// LLM). Only allowed pre-approval (needs_review/revisions_asked).
func newAutonomousPRDAddStoryCmd() *cobra.Command {
	var title, description string
	cmd := &cobra.Command{
		Use:   "prd-add-story <prd-id> --title <t> [--description <d>]",
		Short: "Add a new story without re-running decompose",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"title": title, "description": description, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/add_story", body)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "story title (required)")
	cmd.Flags().StringVar(&description, "description", "", "story description")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newAutonomousPRDRemoveStoryCmd() *cobra.Command {
	var storyID string
	cmd := &cobra.Command{
		Use:   "prd-remove-story <prd-id> --story <story-id>",
		Short: "Remove a story (and its tasks) without re-running decompose",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"story_id": storyID, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/remove_story", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to remove (required)")
	_ = cmd.MarkFlagRequired("story")
	return cmd
}

func newAutonomousPRDAddTaskCmd() *cobra.Command {
	var storyID, title, spec, dependsOnCSV string
	cmd := &cobra.Command{
		Use:   "prd-add-task <prd-id> --story <story-id> --title <t> [--spec <s>] [--depends-on <task-id,...>]",
		Short: "Add a new task to an existing story without re-running decompose",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{"story_id": storyID, "title": title, "spec": spec, "actor": "operator"}
			if dependsOnCSV != "" {
				body["depends_on"] = splitCSV(dependsOnCSV)
			}
			bodyJSON, _ := json.Marshal(body)
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/add_task", bodyJSON)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to add the task to (required)")
	cmd.Flags().StringVar(&title, "title", "", "task title (required)")
	cmd.Flags().StringVar(&spec, "spec", "", "task spec — concrete instructions for the worker session")
	cmd.Flags().StringVar(&dependsOnCSV, "depends-on", "", "comma-separated task IDs (within the same PRD) that must complete before this one becomes eligible")
	_ = cmd.MarkFlagRequired("story")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newAutonomousPRDRemoveTaskCmd() *cobra.Command {
	var storyID, taskID string
	cmd := &cobra.Command{
		Use:   "prd-remove-task <prd-id> --story <story-id> --task <task-id>",
		Short: "Remove one task from a story without re-running decompose (distinct from cancel-task, which stops a running/queued task without removing it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"story_id": storyID, "task_id": taskID, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/remove_task", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID the task belongs to (required)")
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to remove (required)")
	_ = cmd.MarkFlagRequired("story")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newAutonomousPRDSetLLMCmd() *cobra.Command {
	var backend, effort, model, decompositionProfile, guardrailBackend, guardrailModel string
	cmd := &cobra.Command{
		Use:   "prd-set-llm <id>",
		Short: "Set the PRD-level worker LLM (backend / effort / model / decomposition-profile) and/or guardrail judge LLM. Tasks inherit unless overridden.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(prdSetLLMCLIBody(cmd))
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_llm", body)
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "", "Worker LLM name (empty value clears to fallback; omit the flag to leave unchanged)")
	cmd.Flags().StringVar(&effort, "effort", "", "Worker LLM effort (empty value clears to fallback; omit the flag to leave unchanged)")
	cmd.Flags().StringVar(&model, "model", "", "Worker LLM model (empty value clears to fallback; omit the flag to leave unchanged)")
	cmd.Flags().StringVar(&decompositionProfile, "decomposition-profile", "", "Worker LLM decomposition profile (empty value clears to fallback; omit the flag to leave unchanged)")
	addGuardrailLLMFlags(cmd, &guardrailBackend, &guardrailModel)
	return cmd
}

func newAutonomousPRDSetStoryLLMCmd() *cobra.Command {
	var storyID, backend, effort, model string
	cmd := &cobra.Command{
		Use:   "prd-set-story-llm <prd-id> --story <story-id> [--backend B --effort E --model M]",
		Short: "Override the worker LLM for all tasks in one story. Empty value clears the override (falls back to PRD then global).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"story_id": storyID, "backend": backend, "effort": effort, "model": model, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_story_llm", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to override (required)")
	cmd.Flags().StringVar(&backend, "backend", "", "story-level LLM backend; empty = inherit PRD then global")
	cmd.Flags().StringVar(&effort, "effort", "", "effort level for all tasks in this story; empty = inherit")
	cmd.Flags().StringVar(&model, "model", "", "specific model name for this story; empty = backend default")
	_ = cmd.MarkFlagRequired("story")
	return cmd
}

func newAutonomousPRDSetTaskLLMCmd() *cobra.Command {
	var taskID, backend, effort, model, guardrailBackend, guardrailModel string
	cmd := &cobra.Command{
		Use:   "prd-set-task-llm <prd-id> --task <task-id> [--backend B --effort E --model M] [--guardrail-backend GB --guardrail-model GM]",
		Short: "Override the worker LLM and/or guardrail judge LLM for one task. Empty value clears the override (falls back to PRD then global).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			legacy := map[string]string{"backend": backend, "effort": effort, "model": model}
			body, _ := json.Marshal(setLLMCLIBody(cmd, legacy, map[string]string{"task_id": taskID, "actor": "operator"}, guardrailBackend, guardrailModel, "backend", "effort", "model"))
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_task_llm", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to override (required)")
	cmd.Flags().StringVar(&backend, "backend", "", "task-specific LLM backend; empty = inherit PRD then global")
	cmd.Flags().StringVar(&effort, "effort", "", "effort level for this task; empty = inherit")
	cmd.Flags().StringVar(&model, "model", "", "specific model name for this task; empty = backend default")
	addGuardrailLLMFlags(cmd, &guardrailBackend, &guardrailModel)
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

// newAutonomousPRDSetThinkingLevelCmd — set the PRD-level pi --thinking
// level override (off|minimal|low|medium|high; empty clears back to the
// global pi.thinking_level config). Tasks inherit unless overridden.
func newAutonomousPRDSetThinkingLevelCmd() *cobra.Command {
	var level string
	cmd := &cobra.Command{
		Use:   "prd-set-thinking-level <id> --level <off|minimal|low|medium|high|>",
		Short: "Set the PRD-level pi --thinking level override. Tasks inherit unless overridden.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"thinking_level": level, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_thinking_level", body)
		},
	}
	cmd.Flags().StringVar(&level, "level", "", "thinking level: off|minimal|low|medium|high (empty clears to the global pi.thinking_level config)")
	_ = cmd.MarkFlagRequired("level")
	return cmd
}

func newAutonomousPRDSetStoryThinkingLevelCmd() *cobra.Command {
	var storyID, level string
	cmd := &cobra.Command{
		Use:   "prd-set-story-thinking-level <prd-id> --story <story-id> --level L",
		Short: "Override the pi --thinking level for one story. Empty level clears (falls back to PRD then global).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"story_id": storyID, "thinking_level": level, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_story_thinking_level", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to override (required)")
	cmd.Flags().StringVar(&level, "level", "", "thinking level; empty = inherit PRD then global")
	_ = cmd.MarkFlagRequired("story")
	_ = cmd.MarkFlagRequired("level")
	return cmd
}

func newAutonomousPRDSetTaskThinkingLevelCmd() *cobra.Command {
	var taskID, level string
	cmd := &cobra.Command{
		Use:   "prd-set-task-thinking-level <prd-id> --task <task-id> --level L",
		Short: "Override the pi --thinking level for one task. Empty level clears (falls back to story, PRD, then global).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"task_id": taskID, "thinking_level": level, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_task_thinking_level", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to override (required)")
	cmd.Flags().StringVar(&level, "level", "", "thinking level; empty = inherit story, PRD then global")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("level")
	return cmd
}

func newAutonomousPRDSetTaskProjectDirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-set-task-project-dir <prd-id> <task-id> [dir]",
		Short: "Override the project directory for one task (task > Automaton). Omitting dir clears the override (inherits the Automaton's project_dir).",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) == 3 {
				dir = args[2]
			}
			body, _ := json.Marshal(map[string]string{"task_id": args[1], "project_dir": dir, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_task_project_dir", body)
		},
	}
}

func newAutonomousPRDSetTaskVerificationLLMCmd() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "prd-set-task-verification-llm <prd-id> --task <task-id> [--backend B --model M --effort E]",
		Short: "Override the LLM that verifies one task of an Automaton. Precedence: task > story > Automaton > global verification_backend. Omitted flags are unchanged; an empty value clears.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(verificationLLMCLIBody(cmd, map[string]string{"task_id": taskID, "actor": "operator"}))
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_task_verification_llm", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to override (required)")
	addVerificationLLMFlags(cmd, "this task")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newAutonomousPRDSetStoryVerificationLLMCmd() *cobra.Command {
	var storyID string
	cmd := &cobra.Command{
		Use:   "prd-set-story-verification-llm <prd-id> --story <story-id> [--backend B --model M --effort E]",
		Short: "Override the LLM that verifies every task in one story of an Automaton. Precedence: task > story > Automaton > global verification_backend. Omitted flags are unchanged; an empty value clears.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(verificationLLMCLIBody(cmd, map[string]string{"story_id": storyID, "actor": "operator"}))
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_story_verification_llm", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to override (required)")
	addVerificationLLMFlags(cmd, "this story")
	_ = cmd.MarkFlagRequired("story")
	return cmd
}

func newAutonomousPRDSetVerificationLLMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prd-set-verification-llm <prd-id> [--backend B --model M --effort E]",
		Short: "Override the LLM that verifies all tasks of an Automaton. Precedence: task > story > Automaton > global verification_backend. Omitted flags are unchanged; an empty value clears.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(verificationLLMCLIBody(cmd, map[string]string{"actor": "operator"}))
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_verification_llm", body)
		},
	}
	addVerificationLLMFlags(cmd, "this Automaton")
	return cmd
}

// addVerificationLLMFlags registers --backend / --model / --effort on the
// prd-set-*-verification-llm commands. Values are read back via
// cmd.Flags().GetString in verificationLLMCLIBody.
func addVerificationLLMFlags(cmd *cobra.Command, scope string) {
	cmd.Flags().String("backend", "", "verification LLM backend for "+scope+"; empty = clear, omit = unchanged")
	cmd.Flags().String("model", "", "verification model for "+scope+"; empty = clear, omit = unchanged")
	cmd.Flags().String("effort", "", "verification effort for "+scope+"; empty = clear, omit = unchanged")
}

// verificationLLMCLIBody builds a set_*_verification_llm body. A
// verification_* key is sent only when its flag was set, so an unset flag
// leaves the field unchanged and an explicit "" clears it.
func verificationLLMCLIBody(cmd *cobra.Command, base map[string]string) map[string]string {
	body := map[string]string{}
	for k, v := range base {
		body[k] = v
	}
	for _, f := range []string{"backend", "model", "effort"} {
		if cmd.Flags().Changed(f) {
			v, _ := cmd.Flags().GetString(f)
			body["verification_"+f] = v
		}
	}
	return body
}

// addGuardrailLLMFlags (BL416) registers --guardrail-backend /
// --guardrail-model on prd-set-llm and prd-set-task-llm.
func addGuardrailLLMFlags(cmd *cobra.Command, backend, model *string) {
	cmd.Flags().StringVar(backend, "guardrail-backend", "", "guardrail/verification judge LLM override; inherits task -> automaton -> global autonomous.verification_backend; empty = clear, omit = unchanged")
	cmd.Flags().StringVar(model, "guardrail-model", "", "guardrail/verification judge model override; empty = clear, omit = unchanged")
}

// prdSetLLMCLIBody builds the set_llm body for prd-set-llm. Each legacy
// (backend / effort / model / decomposition-profile) and guardrail key is
// included only when its flag was Changed, so an omitted flag leaves the
// setting unchanged while an explicit --flag "" clears it. actor is always
// sent.
func prdSetLLMCLIBody(cmd *cobra.Command) map[string]string {
	body := map[string]string{"actor": "operator"}
	for flag, key := range map[string]string{
		"backend":               "backend",
		"effort":                "effort",
		"model":                 "model",
		"decomposition-profile": "decomposition_profile",
		"guardrail-backend":     "guardrail_backend",
		"guardrail-model":       "guardrail_model",
	} {
		if cmd.Flags().Changed(flag) {
			v, _ := cmd.Flags().GetString(flag)
			body[key] = v
		}
	}
	return body
}

// setLLMCLIBody (BL416) builds a set_llm / set_task_llm body. Guardrail
// keys are sent only when their flag was set ("" clears). When only
// guardrail flags were set the legacy keys are omitted so the REST
// guardrail-only path runs; otherwise the legacy keys are sent as before.
func setLLMCLIBody(cmd *cobra.Command, legacy, base map[string]string, guardrailBackend, guardrailModel string, legacyFlags ...string) map[string]string {
	body := map[string]string{}
	for k, v := range base {
		body[k] = v
	}
	hasGuardrail := false
	if cmd.Flags().Changed("guardrail-backend") {
		body["guardrail_backend"] = guardrailBackend
		hasGuardrail = true
	}
	if cmd.Flags().Changed("guardrail-model") {
		body["guardrail_model"] = guardrailModel
		hasGuardrail = true
	}
	hasLegacy := false
	for _, f := range legacyFlags {
		if cmd.Flags().Changed(f) {
			hasLegacy = true
			break
		}
	}
	if hasLegacy || !hasGuardrail {
		for k, v := range legacy {
			body[k] = v
		}
	}
	return body
}

func newAutonomousPRDInstantiateCmd() *cobra.Command {
	var varsCSV string
	cmd := &cobra.Command{
		Use:   "prd-instantiate <template-id>",
		Short: "Create a fresh PRD from a template (--vars k=v,k=v)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			vars := map[string]string{}
			if varsCSV != "" {
				for _, kv := range strings.Split(varsCSV, ",") {
					if i := strings.IndexByte(kv, '='); i > 0 {
						vars[strings.TrimSpace(kv[:i])] = strings.TrimSpace(kv[i+1:])
					}
				}
			}
			body, _ := json.Marshal(map[string]any{"vars": vars, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/instantiate", body)
		},
	}
	cmd.Flags().StringVar(&varsCSV, "vars", "", "comma-separated k=v list of template variable values")
	return cmd
}

// BL221 (v6.2.0) Phase 3 — scan framework CLI commands.

func newAutonomousPRDScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-scan <id>",
		Short: "Trigger security scan on a PRD (SAST + secrets + deps)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/scan", nil)
		},
	}
}

func newAutonomousPRDScanResultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-scan-result <id>",
		Short: "Get the latest scan result for a PRD",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/autonomous/prds/" + args[0] + "/scan")
		},
	}
}

func newAutonomousPRDScanFixCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-scan-fix <id>",
		Short: "Create a child fix PRD from scan violations",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/scan/fix", nil)
		},
	}
}

func newAutonomousPRDScanRulesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-scan-rules <id>",
		Short: "Propose AGENT.md rule edits to prevent recurrence of scan findings",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/scan/rules", nil)
		},
	}
}

func newAutonomousScanConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan-config-get",
		Short: "Read the autonomous scan configuration",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/scan/config") },
	}
}

func newAutonomousScanConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan-config-set <key=value,...>",
		Short: "Update scan config fields (enabled, sast_enabled, secrets_enabled, deps_enabled, fail_on_severity, max_findings, grader_enabled, fix_loop_enabled, fix_loop_max_retries)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{}
			for _, kv := range args {
				if i := strings.IndexByte(kv, '='); i > 0 {
					k, v := strings.TrimSpace(kv[:i]), strings.TrimSpace(kv[i+1:])
					var parsed any
					if err := json.Unmarshal([]byte(v), &parsed); err == nil {
						body[k] = parsed
					} else {
						body[k] = v
					}
				}
			}
			return daemonJSON(http.MethodPut, "/api/autonomous/scan/config", body)
		},
	}
}

// BL221 (v6.2.0) Phase 4 — type registry, Guided Mode, skills CLI commands.

func newAutonomousPRDSetTypeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-set-type <id> <type>",
		Short: "Set the automaton type on a PRD (software|research|operational|personal or custom)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"type": args[1]})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_type", body)
		},
	}
}

// parseKindArg checks an Automaton kind typed on the command line.
func parseKindArg(arg string) (string, error) {
	k, ok := autonomous.NormalizePRDKind(arg)
	if !ok || k == "" {
		return "", fmt.Errorf("unknown Automaton kind %q: use feature, bug or other", strings.TrimSpace(arg))
	}
	return k, nil
}

func newAutonomousPRDSetKindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-set-kind <id> <feature|bug|other>",
		Short: "Set the Automaton kind (feature, bug or other)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			kind, err := parseKindArg(args[1])
			if err != nil {
				return err
			}
			body, _ := json.Marshal(map[string]string{"kind": kind})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_kind", body)
		},
	}
}

func newAutonomousPRDSetGuidedModeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-set-guided-mode <id> <on|off>",
		Short: "Enable or disable Guided Mode on a PRD",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			enabled := args[1] == "on" || args[1] == "true" || args[1] == "1"
			body, _ := json.Marshal(map[string]bool{"guided_mode": enabled})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_guided_mode", body)
		},
	}
}

// Per-Automaton notify event kinds: which lifecycle events produce
// notifications for one Automaton. An empty list (inherit) falls back
// to the guardrail profile, then the global default set.
func newAutonomousPRDSetNotifyEventsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-set-notify-events <id> [kinds]",
		Short: "Set the lifecycle event kinds that produce notifications for an Automaton",
		Long: `Set the notify event kinds for one Automaton.

kinds is a comma-separated list of lifecycle event kinds, e.g.:
  prd-set-notify-events a1 prd.blocked,task.session_started

Use inherit (or omit the kinds argument) to clear the Automaton's list
and inherit from its guardrail profile or the global default set.

Valid kinds:
  prd.active prd.approved prd.archived prd.blocked prd.cancelled prd.completed
  prd.draft prd.failed prd.needs_review prd.paused prd.planning prd.rejected
  prd.revisions_asked prd.running
  task.awaiting_approval task.blocked task.cancelled task.completed task.failed
  task.in_progress task.pending task.queued task.review_needed task.running_tests
  task.session_cleared task.session_started task.verifying task.waiting_capacity
  scope_warning guardrail_warn
  question.answered question.withdrawn

Opt-in kinds (off by default, list them to turn them on):
  task.session_started task.session_cleared scope_warning guardrail_warn

Default set (used when the Automaton and its guardrail profile both inherit):
  prd.needs_review prd.blocked prd.failed prd.completed prd.cancelled
`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			events := []string{}
			if len(args) == 2 && !strings.EqualFold(args[1], "inherit") {
				for _, k := range strings.Split(args[1], ",") {
					if t := strings.TrimSpace(k); t != "" {
						events = append(events, t)
					}
				}
			}
			body, _ := json.Marshal(map[string]any{"notify_events": events})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_notify_events", body)
		},
	}
}

func newAutonomousPRDSetGitCmd() *cobra.Command {
	var baseBranch string
	cmd := &cobra.Command{
		Use:   "prd-set-git <id> <on|off>",
		Short: "Set AutoPR (push+open a PR on completion) and base-branch override for a PRD (BL407)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			autoPR := args[1] == "on" || args[1] == "true" || args[1] == "1"
			body, _ := json.Marshal(map[string]any{"auto_pr": autoPR, "base_branch": baseBranch})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_git", body)
		},
	}
	cmd.Flags().StringVar(&baseBranch, "base-branch", "", "PR base branch override (empty = repo's own default branch)")
	return cmd
}

func newAutonomousPRDSetSkillsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-set-skills <id> <skill1,skill2,...>",
		Short: "Assign skills to a PRD (passed to spawned task sessions)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			var skills []string
			for _, s := range strings.Split(args[1], ",") {
				if s = strings.TrimSpace(s); s != "" {
					skills = append(skills, s)
				}
			}
			body, _ := json.Marshal(map[string]any{"skills": skills})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_skills", body)
		},
	}
}

func newAutonomousTypesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "types-list",
		Short: "List the automaton type registry (builtins + operator-registered)",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/types") },
	}
}

func newAutonomousTypeRegisterCmd() *cobra.Command {
	var label, description, color string
	cmd := &cobra.Command{
		Use:   "type-register <id>",
		Short: "Register or update an automaton type in the registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{
				"id":          args[0],
				"label":       label,
				"description": description,
				"color":       color,
			})
			return daemonJSON(http.MethodPost, "/api/autonomous/types", body)
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "Human-readable label (required)")
	cmd.Flags().StringVar(&description, "description", "", "Optional description")
	cmd.Flags().StringVar(&color, "color", "", "Optional CSS color for badges (e.g. #6366f1)")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

// BL221 (v6.2.0) Phase 2 — template store CRUD CLI commands.

func newAutonomousTemplateListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "template-list",
		Short: "List all templates in the automaton template store",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/templates") },
	}
}

func newAutonomousTemplateCreateCmd() *cobra.Command {
	var title, spec, description, typ, tagsCSV string
	cmd := &cobra.Command{
		Use:   "template-create",
		Short: "Create a new automaton template",
		RunE: func(_ *cobra.Command, _ []string) error {
			var tags []string
			for _, t := range strings.Split(tagsCSV, ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
			body, _ := json.Marshal(map[string]any{
				"title":       title,
				"spec":        spec,
				"description": description,
				"type":        typ,
				"tags":        tags,
			})
			return daemonJSON(http.MethodPost, "/api/autonomous/templates", body)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "Template title (required)")
	cmd.Flags().StringVar(&spec, "spec", "", "Template spec text (required)")
	cmd.Flags().StringVar(&description, "description", "", "Optional description")
	cmd.Flags().StringVar(&typ, "type", "", "Automaton type")
	cmd.Flags().StringVar(&tagsCSV, "tags", "", "Comma-separated tag list")
	_ = cmd.MarkFlagRequired("title")
	_ = cmd.MarkFlagRequired("spec")
	return cmd
}

func newAutonomousTemplateGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "template-get <id>",
		Short: "Fetch one automaton template",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/autonomous/templates/" + args[0])
		},
	}
}

func newAutonomousTemplateUpdateCmd() *cobra.Command {
	var title, spec, description, typ, tagsCSV string
	cmd := &cobra.Command{
		Use:   "template-update <id>",
		Short: "Update an existing automaton template",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var tags []string
			for _, t := range strings.Split(tagsCSV, ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
			body, _ := json.Marshal(map[string]any{
				"title":       title,
				"spec":        spec,
				"description": description,
				"type":        typ,
				"tags":        tags,
			})
			return daemonJSON(http.MethodPut, "/api/autonomous/templates/"+args[0], body)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "New title (empty = keep existing)")
	cmd.Flags().StringVar(&spec, "spec", "", "New spec (empty = keep existing)")
	cmd.Flags().StringVar(&description, "description", "", "New description")
	cmd.Flags().StringVar(&typ, "type", "", "New automaton type")
	cmd.Flags().StringVar(&tagsCSV, "tags", "", "New comma-separated tag list")
	return cmd
}

func newAutonomousTemplateDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "template-delete <id>",
		Short: "Permanently delete an automaton template",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/autonomous/templates/"+args[0], nil)
		},
	}
}

func newAutonomousTemplateInstantiateCmd() *cobra.Command {
	var projectDir, backend, effort, varsCSV string
	cmd := &cobra.Command{
		Use:   "template-instantiate <id>",
		Short: "Create a new PRD from an automaton template (template store version)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			vars := map[string]string{}
			for _, kv := range strings.Split(varsCSV, ",") {
				if i := strings.IndexByte(kv, '='); i > 0 {
					vars[strings.TrimSpace(kv[:i])] = strings.TrimSpace(kv[i+1:])
				}
			}
			body, _ := json.Marshal(map[string]any{
				"project_dir": projectDir,
				"vars":        vars,
				"backend":     backend,
				"effort":      effort,
				"actor":       "operator",
			})
			return daemonJSON(http.MethodPost, "/api/autonomous/templates/"+args[0]+"/instantiate", body)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory; omit to let the daemon apply its default (worktree mode) instead of assuming the current directory")
	cmd.Flags().StringVar(&backend, "backend", "", "LLM backend override")
	cmd.Flags().StringVar(&effort, "effort", "", "Effort hint (low/medium/high/max)")
	cmd.Flags().StringVar(&varsCSV, "vars", "", "Comma-separated k=v variable substitutions")
	return cmd
}

func newAutonomousTemplateCloneCmd() *cobra.Command {
	var description string
	cmd := &cobra.Command{
		Use:   "template-clone <prd-id>",
		Short: "Save a completed PRD as a reusable template in the template store",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"description": description, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/clone_to_template", body)
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "Optional description for the new template")
	return cmd
}
