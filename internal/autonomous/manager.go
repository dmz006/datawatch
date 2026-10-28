// BL24 — central coordinator. Wires the store, decomposer, and the
// background loop. Mirrors nightwire/autonomous/manager.py shape.

package autonomous

import (
	"context"
	"errors"
	"fmt"
	"github.com/dmz006/datawatch/internal/capacity"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous/scan"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/git"
	"github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/pipeline"
)

// maxDecisionsPerPRD (BL291, v5.5.0) — cap on PRD.Decisions to prevent
// unbounded growth on long-lived PRDs. The autonomous loop appends a
// row on every decompose / approve / run / verify / edit / set_llm /
// set_task_llm action; without a cap a PRD that's been re-decomposed +
// re-run hundreds of times grows to multi-MB JSONL rows that bloat
// every Store.SavePRD write. Trim keeps the most recent N entries.
const maxDecisionsPerPRD = 200

// trimDecisions returns ds capped at maxDecisionsPerPRD entries (most
// recent kept). Caller passes the slice after appending; safe on nil
// or empty.
func trimDecisions(ds []Decision) []Decision {
	if len(ds) <= maxDecisionsPerPRD {
		return ds
	}
	return ds[len(ds)-maxDecisionsPerPRD:]
}

// markGateInvalid records an unusable quality-gate baseline on the PRD:
// status + plain reason, a decision-log entry, and a store save so it
// survives a restart. It never fails, blocks, or retries anything — a gate
// we cannot trust warns only.
func (m *Manager) markGateInvalid(prdID string, prd *PRD, reason string) {
	prd.QualityGateStatus = "invalid"
	prd.QualityGateInvalidReason = reason
	prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
		At:    time.Now(),
		Kind:  "quality_gate_invalid",
		Actor: "autonomous",
		Note:  reason,
	}))
	prd.UpdatedAt = time.Now()
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s markGateInvalid: save: %v", prdID, err)
	}
}

// checkGateBaseline runs the gate's test command once as a baseline in dir
// and classifies the result:
//
//	("", "")       — nothing to check (gates off / no command) or dir is empty
//	("valid", "")  — the command completed and produced a usable baseline
//	("invalid", r) — unusable; r is the plain-language reason
//
// It does not mutate the PRD; callers record the verdict.
func (m *Manager) checkGateBaseline(dir string, cfg pipeline.QualityGateConfig) (string, string) {
	if !cfg.Enabled || strings.TrimSpace(cfg.TestCommand) == "" {
		return "", ""
	}
	if strings.TrimSpace(dir) == "" {
		return "", ""
	}
	r := runQualityGateTests(cfg.TestCommand, dir, cfg.EffectiveTimeout(), m.allowedProjectRoots())
	if valid, reason := pipeline.ClassifyBaseline(r); !valid {
		return "invalid", reason
	}
	return "valid", ""
}

// Config is the operator-tunable knobs for the autonomous system.
// Mirrors session.* config fields per the no-hard-coded-config rule.
// JSON tags use snake_case so REST + YAML payloads can be applied
// directly via SetConfig.
type Config struct {
	Enabled             bool `json:"enabled"`
	PollIntervalSeconds int  `json:"poll_interval_seconds,omitempty"`
	// MaxParallelTasks is deprecated and has no effect: no executor code
	// reads it. Kept for one release so old configs still load; use
	// MaxConcurrentTasks.
	MaxParallelTasks int `json:"max_parallel_tasks,omitempty"`
	// BL304: renamed from DecompositionBackend; JSON key updated to planning_backend.
	// Old key "decomposition_backend" still accepted via SetConfig migration.
	PlanningBackend     string `json:"planning_backend,omitempty"`
	VerificationBackend string `json:"verification_backend,omitempty"`
	// ExecutionBackend is the fallback backend used when a task and its
	// parent PRD both have an empty Backend field. Empty string means
	// fall through to the global session.llm_backend default.
	ExecutionBackend string `json:"execution_backend,omitempty"`
	// BL304: renamed from DecompositionEffort; JSON key updated to planning_effort.
	PlanningEffort     string `json:"planning_effort,omitempty"`
	VerificationEffort string `json:"verification_effort,omitempty"`
	// v5.26.16 — operator-pinned model overrides paired with the
	// backend fields above. Empty = backend default.
	// BL304: renamed from DecompositionModel; JSON key updated to planning_model.
	PlanningModel string `json:"planning_model,omitempty"`
	// PlanningTimeoutSeconds overrides the effort-scaled LLM call timeout
	// used during PRD decomposition (default: 120s quick/low, 300s normal,
	// 900s high/max). Set to 0 to use the effort-scaled defaults.
	// Exposed as autonomous.planning_timeout_seconds in config.
	PlanningTimeoutSeconds int `json:"planning_timeout_seconds,omitempty"`
	// PlanCoverageCheck sets how decomposition treats plan-requirement
	// coverage failures: "off" | "warn" | "block". Empty normalizes to
	// "block" (see config.NormalizePlanCoverageCheck / Manager
	// .EffectivePlanCoverage).
	// No `omitempty`, deliberately: the same round-trip gotcha as
	// config.AutonomousConfig.PlanCoverageCheck — an operator's explicit
	// "off" must survive the manager Config → config.AutonomousConfig
	// json round-trip in handleAutonomousConfig (internal/server/autonomous.go).
	PlanCoverageCheck string `json:"plan_coverage_check"`
	// PlanCoverageMinRatio is the minimum covered-requirement ratio a
	// plan must meet; zero/negative normalizes to 1.0 (full coverage) and
	// >1 clamps to 1.0 (see config.NormalizePlanCoverageRatio). No
	// omitempty for the same round-trip reason as PlanCoverageCheck.
	PlanCoverageMinRatio float64 `json:"plan_coverage_min_ratio"`
	// UsageBackoff and BackoffMaxWaitMinutes mirror the per-backend usage
	// back-off knobs (autonomous.usage_backoff, autonomous.backoff_max_wait_minutes)
	// so both autonomous config surfaces agree and handleAutonomousConfig's
	// round-trip into config.AutonomousConfig keeps them. The registry
	// itself is consulted through SetBackoff. omitempty, deliberately: an
	// unset mirror must not overwrite the value held in config.Config.
	UsageBackoff          string `json:"usage_backoff,omitempty"`
	BackoffMaxWaitMinutes int    `json:"backoff_max_wait_minutes,omitempty"`
	// PlanTierMaxInflight mirrors the tier -> max-inflight table
	// (autonomous.plan_tier_max_inflight) so the two autonomous config
	// surfaces (/api/config and /api/autonomous/config) agree. The manager
	// itself does not consume it: the daemon's capacity wiring
	// (cmd/datawatch/capacity_wiring.go) reads it from config.Config and
	// feeds EffectiveMaxInflight. The field exists so
	// handleAutonomousConfig's live round-trip (internal/server/
	// autonomous.go) does not drop the key — same role as
	// VerificationTimeoutSeconds. omitempty, deliberately: a nil table on
	// this mirror side must not marshal as null and clobber the table the
	// daemon holds in config.Config during that same round-trip.
	PlanTierMaxInflight map[string]int `json:"plan_tier_max_inflight,omitempty"`
	// VerificationTimeoutSeconds is the sibling of PlanningTimeoutSeconds.
	// Exposed as autonomous.verification_timeout_seconds in config. The
	// manager itself does not consume it: the daemon's verifier/guardrail
	// wiring in cmd/datawatch/main.go reads it from config.Config. The field
	// exists so the live manager config round-trip carried by
	// handleAutonomousConfig does not drop the key, the way the
	// VerificationBackends omitempty bug described in the config.go comment
	// dropped its value.
	VerificationTimeoutSeconds int `json:"verification_timeout_seconds,omitempty"`
	// JudgePolicy and JudgeTimeoutSeconds mirror autonomous.judge_policy and
	// autonomous.judge_timeout_seconds in config (see config.AutonomousConfig).
	// No omitempty on either, for the same round-trip reason as
	// VerificationBackends: handleAutonomousConfig marshals this struct and
	// unmarshals the result onto config.AutonomousConfig before persisting.
	JudgePolicy         []string `json:"judge_policy"`
	JudgeTimeoutSeconds int      `json:"judge_timeout_seconds"`
	// JudgeSizeThresholdLines mirrors autonomous.judge_size_threshold_lines in
	// config. No omitempty and no normalization: 0 means the size rule is off
	// and must round-trip as 0.
	JudgeSizeThresholdLines int `json:"judge_size_threshold_lines"`
	// SessionIdleTimeoutSeconds mirrors autonomous.session_idle_timeout_seconds
	// in config. The manager does not consume it: the daemon's autonomous
	// worker/verifier wait loops in cmd/datawatch/main.go and
	// cmd/datawatch/session_judgment.go read it from config.Config. The field
	// exists so the handleAutonomousConfig live round-trip
	// (internal/server/autonomous.go) does not drop the key.
	SessionIdleTimeoutSeconds int `json:"session_idle_timeout_seconds,omitempty"`
	// VerifierInfraEscalationThreshold and FallbackVerificationBackend mirror
	// the autonomous.* keys of the same names in config; the daemon reads them
	// from config.Config. They exist so the live config round-trip keeps them.
	VerifierInfraEscalationThreshold int    `json:"verifier_infra_escalation_threshold,omitempty"`
	FallbackVerificationBackend      string `json:"fallback_verification_backend,omitempty"`
	// VerifierNoProgressTimeoutSeconds mirrors
	// autonomous.verifier_no_progress_timeout_seconds in config; like
	// SessionIdleTimeoutSeconds it is read from config.Config by the daemon
	// and exists here so the live config round-trip does not drop the key.
	VerifierNoProgressTimeoutSeconds int `json:"verifier_no_progress_timeout_seconds,omitempty"`
	// WorkerIdleNudgeSeconds and WorkerIdleNudgeStallSeconds mirror
	// autonomous.worker_idle_nudge_seconds and
	// autonomous.worker_idle_nudge_stall_seconds in config. The manager does
	// not consume them: the worker wait loop in cmd/datawatch/main.go reads
	// them from the live Config() snapshot. Fields exist so the live
	// round-trip does not drop the keys. 0 or unset means the defaults
	// (config.NormalizeWorkerIdleNudgeSeconds /
	// config.NormalizeWorkerIdleNudgeStallSeconds).
	WorkerIdleNudgeSeconds      int `json:"worker_idle_nudge_seconds,omitempty"`
	WorkerIdleNudgeStallSeconds int `json:"worker_idle_nudge_stall_seconds,omitempty"`
	// SessionStartConfirmSeconds mirrors autonomous.session_start_confirm_seconds
	// in config. The manager does not consume it: the daemon's autonomous
	// worker start-confirmation loop reads it from config.Config. The
	// field exists so the handleAutonomousConfig live round-trip
	// (internal/server/autonomous.go) does not drop the key.
	SessionStartConfirmSeconds int `json:"session_start_confirm_seconds,omitempty"`
	// OrphanGraceSeconds mirrors autonomous.orphan_grace_seconds: how long a
	// live task worker session may stay unbound to any non-terminal task
	// before the orphan sweep ends it. 0 or unset => 300
	// (config.NormalizeOrphanGraceSeconds).
	OrphanGraceSeconds int    `json:"orphan_grace_seconds,omitempty"`
	VerificationModel  string `json:"verification_model,omitempty"`
	// GuardrailModel (v9.10.18) — pinned model for guardrail judge
	// sessions (rules-review, ci-watch, per-task/per-story guardrails,
	// rejudge). Tier 3 of ResolveGuardrailModel (guardrail_model.go):
	// PRD guardrail_model → task guardrail_model → GuardrailModel →
	// VerificationModel → PlanningModel. Empty = the tier falls through;
	// if no tier resolves a model the guardrail backend can serve, the
	// guardrail records a loud, non-retried infra-category block
	// (ErrNoGuardrailModel / NoGuardrailModelVerdict) instead of
	// launching a judge on the backend's built-in default model.
	GuardrailModel string `json:"guardrail_model,omitempty"`
	// VerificationBackends (v8.36.9) — see config.AutonomousConfig's doc
	// comment. Ordered list of LLM registry names to load-balance
	// verifier/guardrail capacity across; empty = single-backend
	// behavior via VerificationBackend/PRD backend/"ollama" fallback.
	//
	// B124 follow-up — no `omitempty`, deliberately: see
	// config.AutonomousConfig.VerificationBackends's doc comment.
	// internal/server/autonomous.go's handleAutonomousConfig marshals
	// THIS struct and unmarshals the result onto config.AutonomousConfig
	// before persisting — omitempty here would elide an explicit clear
	// from that marshal, silently reverting it on the next load.
	VerificationBackends []string `json:"verification_backends"`
	// VerifierPolicy mirrors autonomous.verifier_policy in config. The live
	// verifier selection reads it from the Config() snapshot. No omitempty,
	// same round-trip reason as VerificationBackends.
	VerifierPolicy config.VerifierPolicyConfig `json:"verifier_policy"`
	// TaskOverrideStatuses (v9.5.5) — PRD statuses that accept task-level and
	// story-level LLM overrides. Empty = default; see config.AutonomousConfig's doc comment.
	// No omitempty, same round-trip reason as VerificationBackends.
	TaskOverrideStatuses []string `json:"task_override_statuses"`
	StaleTaskSeconds     int      `json:"stale_task_seconds,omitempty"`
	// StuckRunningSeconds mirrors autonomous.stuck_running_seconds: how long a
	// PRD may sit in running with every task terminal, no live session and no
	// live executor before the stuck-running watchdog re-fires its rollup once,
	// then blocks it. 0 or unset => 900 (config.NormalizeStuckRunningSeconds).
	StuckRunningSeconds int `json:"stuck_running_seconds,omitempty"`
	AutoFixRetries      int `json:"auto_fix_retries,omitempty"`

	// BL410 Phase 1 — per-category retry budgets (daemon-wide default;
	// PRD.RetryPolicy > named RetryProfile > this). Keys: "infra",
	// "content", "scope", "policy" (see FailureReason's doc comment for
	// the taxonomy). "content"/"policy" fall back to AutoFixRetries
	// above when absent from this map — the backward-compat anchor, so
	// a PRD that never touches this feature gets byte-for-byte today's
	// retry count. "infra"'s real default (2) and "scope"'s (0) are
	// seeded here by DefaultConfig, not hardcoded in the resolver.
	RetryPolicy map[string]int `json:"retry_policy,omitempty"`
	// RetryCeilingTotal caps auto-retries summed across every category
	// and unit for one PRD's whole lifecycle (default 5 — per the
	// operator's own framing: a normal release PRD failing more than
	// ~5 times means the spec needs updating or datawatch itself is
	// broken, not a problem one more attempt fixes). PRD.RetryCeilingTotal
	// or a named RetryProfile can raise this for genuinely iterative
	// work (the operator's example: a CVE test-and-fix PRD).
	RetryCeilingTotal int `json:"retry_ceiling_total,omitempty"`
	// InfraRetryEscalateAfter — how many retries keep the EXACT
	// original backend/model config before an infra-category retry
	// falls back to the global default (default 2, per the operator's
	// "retry appropriately, more than 1x before falling back").
	InfraRetryEscalateAfter int `json:"infra_retry_escalate_after,omitempty"`
	// ContentAutoFixEnabled/AutoFixSkipApproval have no omitempty — see
	// AutoApproveChildren's comment below for why a bool here needs
	// the explicit-clear discipline (an operator writing false must
	// not silently no-op on the next config round-trip). Defaults:
	// both true (content auto-fix on, synthesized corrective work
	// skips PRD.PerStoryApproval) — the operator's own "work auto the
	// default" answer.
	ContentAutoFixEnabled bool `json:"content_auto_fix_enabled"`
	AutoFixSkipApproval   bool `json:"auto_fix_skip_approval"`
	// BlockOnFileDrift — strict foreign-file-drift enforcement: when
	// true, a task whose run modified tracked files outside its own
	// scope is blocked instead of merely flagged. No omitempty for the
	// same explicit-clear reason as ContentAutoFixEnabled above.
	// Default false (opt-in); overridable per GuardrailProfile and per
	// PRD (resolveBlockOnFileDrift).
	BlockOnFileDrift bool `json:"block_on_file_drift"`
	// DriftMode — pre-existing file-drift enforcement mode:
	// "block" (default) blocks the task when its commit swept in files
	// that were already dirty/untracked before the worker started and
	// were not in its planned files; "warn" only records a
	// file_drift_detected decision + warning. In-run findings (the
	// task's own commits) are never blocked regardless of mode. An
	// empty/unknown value resolves as "block" (resolveDriftMode), and
	// an explicit block_on_file_drift=false on the PRD or its
	// guardrail profile is honored as an opt-out even under "block"
	// (checkForeignFileDrift). No omitempty: keeping it explicit
	// mirrors the clear discipline of BlockOnFileDrift above.
	DriftMode string `json:"drift_mode"`
	// UndeclaredEditMode — off | warn | block: what to do with the
	// worker's own edits outside FilesPlanned ∪ FilesGenerated. Default
	// "warn"; empty/unknown resolves to "warn" (EffectiveUndeclaredEditMode).
	// Independent of DriftMode, which governs pre-existing drift only.
	UndeclaredEditMode string `json:"undeclared_edit_mode"`
	// ScopeLint — the resolved pre-spawn scope-lint mode: "off", "warn"
	// or "enforce" (config.AutonomousConfig.ScopeLintMode). The executor
	// and plan-time code read it; enforce is today's blocking behaviour.
	ScopeLint string `json:"scope_lint"`
	// VerifierDiffMaxBytes caps the git diff injected into the verifier
	// prompt for evidence grounding (BL366). 0 = 8192.
	VerifierDiffMaxBytes int  `json:"verifier_diff_max_bytes,omitempty"`
	SecurityScan         bool `json:"security_scan,omitempty"`

	// BL191 Q4 (v5.9.0) — recursive child-PRD knobs. MaxRecursionDepth
	// caps the parent→child PRD chain length (default 5; 0 disables
	// recursion entirely). AutoApproveChildren skips the
	// needs_review→approved gate for spawned children — needed for any
	// useful recursion, since otherwise every level hangs on operator
	// review. Defaults: depth=5, auto_approve=true.
	MaxRecursionDepth int `json:"max_recursion_depth,omitempty"`
	// AutoApproveChildren has no omitempty: SetConfig (internal/autonomous/api.go)
	// merges an incoming patch onto the CURRENT config via json.Unmarshal, which
	// only touches keys actually present in the JSON. omitempty on a bool means
	// an explicit "set this to false" is marshaled as an absent key and silently
	// never overwrites a previously-true value — found live 2026-10-05 via this
	// exact bug on PerStoryApproval below (SEC/A3 E2E investigation).
	AutoApproveChildren bool `json:"auto_approve_children"`

	// BL191 Q6 (v5.10.0) — guardrails at story + task level. Empty list =
	// disabled at that level (PRD-level guardrails are handled by the
	// BL117 orchestrator). When non-empty, the executor invokes each
	// named guardrail after the corresponding unit completes; a `block`
	// verdict halts the parent PRD with status PRDBlocked. The
	// well-known guardrail names match the orchestrator: rules,
	// security, release-readiness, docs-diagrams-architecture.
	PerTaskGuardrails  []string `json:"per_task_guardrails,omitempty"`
	PerStoryGuardrails []string `json:"per_story_guardrails,omitempty"`

	// BL418 — daemon-wide default for the "prd" level (PRD-completion
	// rules review). Same off-by-default, explicit-opt-in shape as the
	// two lists above: empty means runPRDCompletionRulesReview never
	// fires, even when a rules/context file exists. An operator adds
	// "rules-review" here (or to PRD.PerPRDGuardrails) to turn it on.
	// "ci-watch" is the other valid PRD-level name: it waits for the
	// CI runs of this automaton's own pushed commit (see
	// CIWatchTimeoutSeconds) and is equally off unless listed.
	PerPRDGuardrails []string `json:"per_prd_guardrails,omitempty"`

	// Phase 3 (v5.26.61) — per-story approval gate. When true, PRD-
	// level approval transitions every story to "awaiting_approval"
	// and the runner skips those until the operator approves each.
	// Default false preserves the v5.26.x behavior (PRD approval
	// implicitly approves every story).
	//
	// No omitempty — see AutoApproveChildren's comment above. A PUT
	// /api/config '{"autonomous.per_story_approval":false}' restore
	// call was silently dropped by SetConfig's merge-unmarshal while
	// this field had omitempty, leaving every subsequent PRD approval
	// wrongly gating its stories into awaiting_approval forever (no
	// code path ever calls approve_story for them) — found live
	// 2026-10-05 chasing a hung E2E run.
	PerStoryApproval bool `json:"per_story_approval"`

	// BL221 (v6.2.0) Phase 3 — scan framework config.
	Scan scan.Config `json:"scan,omitempty"`

	// BL406 — path to this project's prescriptive rules file (its
	// AGENT.md equivalent). Relative to a PRD's project_dir. Empty =
	// "AGENT.md" applied at read time, never stored as a default.
	RulesFile string `json:"rules_file,omitempty"`
	// ContextFile — path to this project's descriptive "state of the
	// project" doc. Explicitly optional. Empty = "CONTEXT.md" applied
	// at read time only when that file actually exists.
	ContextFile string `json:"context_file,omitempty"`
	// UpstreamRepos — named targets a project-rules file_upstream_issue
	// action can reference.
	UpstreamRepos []scan.UpstreamRepo `json:"upstream_repos,omitempty"`

	// BL407 Phase 1 — WorktreeBaseRepo is the git repo a PRD's
	// isolated worktree is created from (e.g. this checkout, for
	// self-build work). Empty = feature off, fully backward
	// compatible: a PRD with no explicit ProjectDir/ProjectProfile/
	// ClusterProfile runs the same way it always has. WorktreeDir is
	// where worktrees are created (default ~/.datawatch/prd-worktrees
	// when empty). See docs/plans/2026-10-10-prd-git-workflow.md.
	WorktreeBaseRepo string `json:"worktree_base_repo,omitempty"`
	WorktreeDir      string `json:"worktree_dir,omitempty"`
	// BL407 Phase 5 — daemon-wide default for a new PRD's Git.AutoPR.
	// Applied once, at CreatePRD time (not resolved at use-time the
	// way DefaultQualityGates/ContinueOnStoryFailure are) — Git.AutoPR
	// is a plain bool with no pointer/override indirection, so a
	// resolve-at-use-time default couldn't distinguish "operator
	// explicitly turned it back off on this PRD" from "never set"; a
	// one-time default at creation avoids that ambiguity entirely and
	// leaves the field as the PRD's own real, inspectable, freely
	// overridable value from then on (via the new set_git action).
	DefaultAutoPR bool `json:"default_auto_pr,omitempty"`

	// BL303 S2 — default skills automatically assigned to every new Automaton.
	DefaultSkills []string `json:"default_skills,omitempty"`

	// BL367 — default quality gate config for all PRDs. Per-PRD
	// QualityGates field overrides this when set. Disabled by default
	// (Enabled: false) to preserve v8.15.x behaviour.
	DefaultQualityGates pipeline.QualityGateConfig `json:"default_quality_gates,omitempty"`

	// BL369 — prompt injection guard. When true, user-supplied text
	// (PRD spec, task spec edits) is scanned for known injection phrases
	// at the API boundary. Findings are always logged; with BlockOnInjection
	// true the request is rejected with 400.
	// No omitempty on either bool — see PerStoryApproval's comment above.
	InjectionGuard   bool `json:"injection_guard"`
	BlockOnInjection bool `json:"block_on_injection"`

	// BL370 — task concurrency. MaxConcurrentTasks controls how many task
	// sessions the executor may have in-flight simultaneously. Default 1
	// preserves the existing sequential behaviour. Values > 1 fan out
	// independent (no-dependency) tasks up to the specified limit. Per-PRD
	// MaxConcurrentTasks overrides this global default.
	MaxConcurrentTasks int `json:"max_concurrent_tasks,omitempty"`

	// GateDisagreement — verifier/quality-gate disagreement policy. When
	// a task's verifier PASSES but the quality gate disagrees (a
	// valid-baseline regression, or an invalid gate), "warn" (default)
	// lets the task complete while recording the disagreement (task
	// gate_disagreement field + decision-log entry + warning); "block"
	// fails the task with a "gate_disagreement: …" error for a real
	// regression from a VALID baseline (an invalid gate never blocks
	// under any policy). Unset or unrecognized values read as "warn".
	GateDisagreement string `json:"gate_disagreement,omitempty"`
	// FailureEscalation — the global default failure escalation policy
	// (mirrors autonomous.failure_escalation). Automaton and task overrides
	// win per field; see resolveFailureEscalation. An unset MaxFailures reads
	// as 3 and a nil Enabled reads as true.
	FailureEscalation FailureEscalationPolicy `json:"failure_escalation"`
	// MaxEnginesPerNode is how many local-model engine sessions one
	// compute node may run at once. 1 = one per node (default). Mirrors
	// config.AutonomousConfig.MaxEnginesPerNode; the runtime copy is what
	// REST and MCP writes change.
	MaxEnginesPerNode int `json:"max_engines_per_node,omitempty"`
	// BL428 §3a ii — refuse to admit a task onto a project tree that
	// has uncommitted changes: the canonical tree's `git status
	// --porcelain` is non-empty. Hard failure at admission (before the
	// tree lock), never a retry. Default false; per-PRD override
	// PRD.RefuseDirtyTreeStart.
	RefuseDirtyTreeStart bool `json:"refuse_dirty_tree_start"`

	// ContinueOnStoryFailure controls whether the executor keeps running
	// later, independent stories after one story fails. Default false
	// (zero value) HALTS the PRD (status -> PRDBlocked) the moment a
	// story rolls up to StoryFailed, so a broken first story can't let
	// the run barrel ahead into stories 2-4 unattended. Set true to
	// restore the old "continue regardless" behavior. Per-PRD
	// PRD.ContinueOnStoryFailure overrides this when non-nil.
	// No omitempty — see PerStoryApproval's comment above.
	ContinueOnStoryFailure bool `json:"continue_on_story_failure"`

	// Capacity admission. nil = enabled (default on).
	CapacityEnabled *bool `json:"capacity_enabled,omitempty"`
	// CapacityWaitTimeoutSeconds bounds a capacity wait (default 14400).
	CapacityWaitTimeoutSeconds int `json:"capacity_wait_timeout_seconds,omitempty"`
	// CapacityRate* mirror config.AutonomousConfig: a per-llm: pool rolling
	// ceiling on lease grants and recorded tokens (0 = none; window 0 = 3600).
	CapacityRateRequests      int `json:"capacity_rate_requests,omitempty"`
	CapacityRateTokens        int `json:"capacity_rate_tokens,omitempty"`
	CapacityRateWindowSeconds int `json:"capacity_rate_window_seconds,omitempty"`
	// ResumeMaxAgeSeconds is how old (in seconds) a completed task may be
	// before it is no longer eligible for resume (default 3600). 0 is
	// meaningful: never resume.
	ResumeMaxAgeSeconds int `json:"resume_max_age_seconds"`
	// CIWatchTimeoutSeconds bounds how long the ci-watch completion guardrail waits for this automaton's own pushed commit's CI runs to finish (default 1500).
	// Default rationale: this repo's own tag Release workflow took ~18
	// minutes on the most recent real run (gh run list) and ordinary
	// push workflows 1-11 minutes, so 25 minutes covers the slowest real
	// run with headroom. 0 = use the default (see ciWatchTimeout).
	CIWatchTimeoutSeconds int `json:"ci_watch_timeout_seconds,omitempty"`

	// Plan review (2026-10-17 pre-approval gate; see plan_review.go).
	// Mode: "" or "off" = disabled (default); "warn" = record-only;
	// "block" = a negative verdict refuses Manager.Approve until the
	// verdict is replaced (review re-run or re-decompose with ok:true).
	// A malformed mode is a config error on the operator surface
	// (internal/config validates it); the manager normalizes anything
	// unrecognized to "off" rather than failing.
	PlanReviewBackend string `json:"plan_review_backend,omitempty"`
	PlanReviewModel   string `json:"plan_review_model,omitempty"`
	PlanReviewMode    string `json:"plan_review_mode,omitempty"`
	// Smoke verification backend/model pair — forward config surface for
	// the smoke-verification feature story (independent deliverable; it
	// consumes these, this task only defines the fields).
	SmokeVerificationBackend string `json:"smoke_verification_backend,omitempty"`
	SmokeVerificationModel   string `json:"smoke_verification_model,omitempty"`

	// Automation holds the autonomous.automation.* knobs.
	Automation AutomationConfig `json:"automation"`

	// LaneFill holds the autonomous.lane_fill.* knobs.
	LaneFill LaneFillConfig `json:"lane_fill"`

	// NotifyDefaultEvents — the most general (global) tier of notify
	// event-kind resolution (ResolveNotifyEvents): the Automaton's own
	// notify_events list wins, then the assigned guardrail profile's list,
	// then this list. Sourced from the autonomous.notify.default_events
	// config key: the daemon bridge (cmd/datawatch) copies it here at boot
	// and the live PUT /api/config path (API.SetConfig merge) lands it
	// here on change, so no restart is needed.
	//
	// Nil vs empty matters and is preserved on purpose:
	//   nil            = unset → ResolveNotifyEvents uses the built-in
	//                    DefaultNotifyEvents set (the five default kinds)
	//   non-nil, empty = operator explicitly turned notifications off;
	//                    ResolveNotifyEvents returns no events
	// In either case Source stays "global". Not omitempty: an explicit
	// empty list must survive the merge-based partial unmarshals
	// (API.SetConfig) and manager↔config round-trips without collapsing
	// back to nil.
	NotifyDefaultEvents []string `json:"notify_default_events"`
}

// AutomationConfig carries the Automation coordination knobs
// (autonomous.automation.*).
type AutomationConfig struct {
	// IdleAlertMinutes: alert once when an active Automation stays idle
	// this long. 0 = off.
	IdleAlertMinutes int `json:"idle_alert_minutes"`

	// TriageAlertMinutes: how long (whole minutes) an Automation may sit
	// waiting before a triage alert is raised. Pointer on purpose: an unset
	// value (nil) reads the default DefaultAutomationTriageAlertMinutes
	// (30), while an explicit 0 turns the triage alert off and survives a
	// merge round trip. The poll pass reads its threshold through
	// EffectiveTriageAlertMinutes.
	TriageAlertMinutes *int `json:"triage_alert_minutes,omitempty"`

	// AgentInsertPolicy: PolicyAllow (default, also the empty value) lets an
	// agent insert a member directly; PolicyRequireOperator holds an agent's
	// insert as a pending insert until an operator approves it.
	AgentInsertPolicy string `json:"agent_insert_policy"`
}

// DefaultAutomationTriageAlertMinutes — the autonomous.automation.
// triage_alert_minutes default: a triage alert is raised after the Automation
// has been waiting this many whole minutes. Only an explicit 0 turns the
// alert off.
const DefaultAutomationTriageAlertMinutes = 30

// EffectiveTriageAlertMinutes returns the triage alert threshold in effect:
// unset (nil) reads the default of DefaultAutomationTriageAlertMinutes (30),
// an explicit 0 turns the triage alert off. Negative values are rejected on
// the write paths, so this never has to guess for them.
func (c AutomationConfig) EffectiveTriageAlertMinutes() int {
	if c.TriageAlertMinutes == nil {
		return DefaultAutomationTriageAlertMinutes
	}
	return *c.TriageAlertMinutes
}

// DefaultConfig returns sane defaults — autonomous OFF until operator opts in.
func DefaultConfig() Config {
	return Config{
		Enabled:             false,
		PollIntervalSeconds: 30,
		MaxConcurrentTasks:  1,
		MaxEnginesPerNode:   1,
		GateDisagreement:    "warn",
		FailureEscalation:   defaultFailureEscalationPolicy(),
		AutoFixRetries:      1,
		SecurityScan:        true,
		MaxRecursionDepth:   5,
		AutoApproveChildren: true,
		Scan:                scan.DefaultConfig(),
		// BL410 Phase 1 — see Config.RetryPolicy's doc comment for why
		// "content"/"policy" are deliberately absent here (they fall
		// back to AutoFixRetries above). infra's budget (4) must stay
		// greater than InfraRetryEscalateAfter (2) below — otherwise
		// the per-category budget always exhausts at attempt 3, the
		// exact attempt escalation first becomes eligible at, and
		// escalation would be structurally unreachable under defaults
		// (caught by TestRunGuardrailWithInfraRetry_IncidentReproduction).
		RetryPolicy:             map[string]int{"infra": 4, "scope": 0},
		RetryCeilingTotal:       5,
		InfraRetryEscalateAfter: 2,
		ContentAutoFixEnabled:   true,
		BlockOnFileDrift:        false,   // legacy opt-in flag; DriftMode below is the enforcement knob
		DriftMode:               "block", // pre-existing drift blocks by default; "warn" for flag-only
		UndeclaredEditMode:      "warn",
		ScopeLint:               "enforce",
		AutoFixSkipApproval:     true,
		// B165 — boot-resume default limit: 1 h. 0 and -1 are meaningful
		// values (never resume / no limit), so an unset value must land
		// here, not at the zero value — mirrors the config-load default
		// (internal/config resume_max_age) that main.go's bridge picks up.
		ResumeMaxAgeSeconds: bootResumeDefaultMaxAgeSeconds,
		LaneFill:            DefaultLaneFillConfig(),
	}
}

// EffectiveUndeclaredEditMode resolves the undeclared-edit mode, preferring
// s over the configured default. Lowercased; only off|warn|block pass
// through, empty or unknown resolves to "warn".
func (m *Manager) EffectiveUndeclaredEditMode(s string) string {
	normalize := func(v string) string {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "off" || v == "warn" || v == "block" {
			return v
		}
		return "warn"
	}
	if s != "" {
		return normalize(s)
	}
	m.mu.Lock()
	v := m.cfg.UndeclaredEditMode
	m.mu.Unlock()
	return normalize(v)
}

// checkInjectionGuard (BL369) scans user-supplied text for prompt injection
// phrases. Always logs findings; returns an error only when BlockOnInjection
// is enabled — that lets callers reject the request at the API boundary.
func (m *Manager) checkInjectionGuard(label, text string) error {
	m.mu.Lock()
	guard := m.cfg.InjectionGuard
	block := m.cfg.BlockOnInjection
	m.mu.Unlock()
	if !guard {
		return nil
	}
	hits := ScanForInjection(text)
	if len(hits) == 0 {
		return nil
	}
	metrics.InjectionGuardHitsTotal.Inc()
	log.Printf("[BL369] injection-guard hit in %s: %s", label, strings.Join(hits, "; "))
	if block {
		return fmt.Errorf("injection-guard: potentially unsafe content detected in %s (%s)", label, hits[0])
	}
	return nil
}

// resolveQualityGates (BL367) returns the quality gate config to use for
// a PRD: per-PRD override takes precedence over the daemon-wide default.
func (m *Manager) resolveQualityGates(prd *PRD) pipeline.QualityGateConfig {
	if prd.QualityGates != nil {
		return *prd.QualityGates
	}
	m.mu.Lock()
	d := m.cfg.DefaultQualityGates
	m.mu.Unlock()
	return d
}

// resolveContinueOnStoryFailure returns whether the executor should keep
// running independent later stories after a story fails: per-PRD override
// takes precedence over the daemon-wide default (which itself defaults to
// false — halt — unless the operator opts in globally).
func (m *Manager) resolveContinueOnStoryFailure(prd *PRD) bool {
	if prd.ContinueOnStoryFailure != nil {
		return *prd.ContinueOnStoryFailure
	}
	m.mu.Lock()
	d := m.cfg.ContinueOnStoryFailure
	m.mu.Unlock()
	return d
}

// GuardrailFn (BL191 Q6, v5.10.0) is the indirection that runs one
// guardrail attestation at the story or task level. Wired in main.go
// to the same BL103 validator-agent path the BL117 orchestrator uses
// at the PRD level so guardrail behavior is uniform across levels.
// Tests inject a fake.
type GuardrailFn func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error)

// GuardrailInvocation is the input to a per-story / per-task guardrail
// call. Level is "task" or "story"; UnitID is the Task.ID or Story.ID.
type GuardrailInvocation struct {
	PRDID      string
	Level      string // "story" | "task"
	UnitID     string
	UnitTitle  string
	UnitSpec   string
	Guardrail  string // name from Config.PerTaskGuardrails / PerStoryGuardrails
	ProjectDir string
	// BL369 Layer 3 — federation trust boundary. Non-empty when the PRD
	// originated from a remote federation peer (PRD.OwnerPeer). The
	// guardrail prompt includes an untrusted-origin notice when set.
	OwnerPeer string
	// BL416 Phase 3 — tooled deterministic guardrails. ScriptRan is true
	// only when the unit's VerificationScript was actually attempted;
	// ScriptExitCode/ScriptOutput carry its real result (output is
	// combined stdout+stderr, capped by truncateScriptOutput) so the
	// judgment call is grounded in real output, not a guess.
	// GuardrailBackend/GuardrailModel are ALREADY-RESOLVED values from
	// resolveUnitGuardrailModel / ResolveGuardrailModel (v9.10.18,
	// guardrail_model.go) — callers never pass raw task/PRD override
	// fields here. GuardrailModel is always populated for pi-family
	// backends; when no tier resolves it is left empty and the caller
	// records NoGuardrailModelVerdict instead of launching a judge.
	ScriptRan        bool
	ScriptExitCode   int
	ScriptOutput     string
	GuardrailBackend string
	GuardrailModel   string
	// GuardrailPinned is true only when GuardrailBackend came from an
	// explicit task or PRD guardrail backend override. It is false when
	// GuardrailBackend is the global default fallback
	// (cfg.VerificationBackend) or nothing resolved.
	GuardrailPinned bool
	// Prompt, when set, is the whole judge prompt, used instead of the
	// generated guardrail prompt. The plan-review judge sets it: its reply
	// uses the same outcome/severity/summary/issues JSON, but it judges a
	// plan, not a unit.
	Prompt string
}

// AutomatonType (BL221 v6.2.0 Phase 4) is a registered automaton type.
// Well-known types are pre-registered; operators add custom types via the REST API.
type AutomatonType struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"` // CSS color for badges
}

// builtinTypes is the factory-default type registry.
var builtinTypes = []AutomatonType{
	{"software", "Software", "Feature development, bug fixes, refactoring", "#6366f1"},
	{"research", "Research", "Investigation, analysis, documentation", "#10b981"},
	{"operational", "Operational", "Deployments, infra changes, monitoring", "#f59e0b"},
	{"personal", "Personal", "Personal productivity and learning tasks", "#8b5cf6"},
}

// Manager is the public façade for the autonomous package.
type Manager struct {
	mu          sync.Mutex
	cfg         Config
	store       *Store
	automations *AutomationStore
	dataDir     string         // 2026-10-07 — root for ScratchDir (PRD scratch artifacts), same root Store itself was opened under
	templates   *TemplateStore // BL221 (v6.2.0) — dedicated template store
	decompose   DecomposeFn
	guardrail   GuardrailFn
	planReview  PlanReviewFn // pre-approval plan review (see plan_review.go); nil = degraded 'skipped' verdicts
	// sessionTags counts the sessions carrying a tag, for the delete-in-use
	// check (tags.go). Wired by the server; nil = Automata only.
	sessionTags SessionTagCounter
	// tagEventFn receives tag registry and assignment changes (tags_events.go).
	// Wired by the server; nil = no tag events. Guarded by mu.
	tagEventFn func(TagEvent)

	coordinator *Coordinator // set by Coordinator.Start; nil until then

	// pairingLogged records each pairing warning already written to the
	// daemon log, so a config set does not repeat a warning it logged before.
	pairingLogged map[string]bool

	// BL451 — per-PRD count of executor-owned saves (savePRDOwn), so the
	// completion rollup can discount its own Rev bumps. Guarded by mu.
	ownSaves map[string]int

	// savePRDHook is a test seam: when non-nil, savePRDOwn persists through
	// it instead of m.store.SavePRD, so a test can force a save failure.
	// Production never sets it.
	savePRDHook func(*PRD) error

	// BL427 — in-process lifecycle event dispatcher (lifecycle_event.go).
	// Has its own lock; never guarded by mu. lifecycleHost is
	// os.Hostname, cached once.
	lifecycle     *lifecycleDispatcher
	lifecycleHost string
	lifecycleOnce sync.Once
	// BL427 (phase 5) — receiving side of peer forwarding; nil until
	// SetForwardReceive. Read on every forwarded envelope (lifecycle_forward_receive.go).
	forwardRecv atomic.Pointer[ForwardReceiveConfig]

	// Holder-name resolver (capacity_titles.go) — lazily created, one shared
	// title snapshot so a whole capacity response resolves in one store read.
	holderResolverOnce sync.Once
	holderResolver     *TitleResolver

	// BL221 (v6.2.0) Phase 3 — scan framework
	graderFn     scan.GraderFn
	ruleEditorFn scan.RuleEditorFn
	scanResults  sync.Map // prdID → *scan.Result

	// BL406 Phase 4 — injectable so tests can fake the GitHub call
	// without shelling out to the real `gh` binary (that layer already
	// has its own direct unit tests in internal/git). nil means "use
	// the real git.Resolve" — see fireUpstreamIssueActions.
	gitProviderFn func(kind string) git.Provider

	// ci-watch completion guardrail seams (see ci_watch.go). Guarded by
	// mu like guardrail. nil/zero means "use the real implementation":
	// ciGitFn → defaultCIGit, ciGitSHAFn → defaultCIGitSHA, ciRunsFn →
	// defaultCIRuns, ciRerunFn →
	// defaultCIRerun, ciPollInterval → 30s, ciFollowUpDelay → the
	// ci-watch timeout (ciWatchTimeout). Injectable so tests never shell
	// out to the real git/gh binaries or wait real minutes.
	ciGitFn         func(ctx context.Context, projectDir string) (headSHA, originURL string, pushed bool, err error)
	ciGitSHAFn      func(ctx context.Context, projectDir, sha string) (originURL string, pushed bool, err error)
	ciRunsFn        func(ctx context.Context, ownerRepo, sha string) ([]CIWatchRun, error)
	ciRerunFn       func(ctx context.Context, ownerRepo string, runID int64) error
	ciPollInterval  time.Duration
	ciFollowUpDelay time.Duration

	// BL406 Phase 5 — nil until BL405 Phase 8 wires real CouncilProfile
	// support; resolveGuidedModeSource falls "council" back to
	// "operator" while this is nil, rather than silently skipping the
	// gate. Signature intentionally loose (consensus decision as a
	// bool + free-form note) since the real CouncilProfile type
	// doesn't exist yet — this is a placeholder seam, not a finished
	// contract.
	councilApprovalFn func(ctx context.Context, prd *PRD, t *Task) (approved bool, note string, err error)

	// B165 — boot-resume gate bookkeeping (boot_resume_gate.go).
	// bootResumeReport: append-only log of every boot-resume decision
	// (resumed or parked) since Manager construction; guarded by mu.
	// Surfaced on LoopStatus.BootResume + Manager.BootResumeReport.
	// alertFn: operator-alert hook fired when boot-resume parks a PRD;
	// wired in main.go to the daemon's alert store, nil = log-only.
	bootResumeReport []BootResumeEntry
	alertFn          func(title, body string)

	// BL221 (v6.2.0) Phase 4 — type registry
	typesMu sync.Mutex
	types   []AutomatonType

	// BL303 S2 — guardrail library + skill guardrail loader
	guardrailLibMu sync.RWMutex
	guardrailLib   []GuardrailEntry
	skillsDir      string // path to ~/.datawatch/skills (injected from main.go)

	// v5.26.19 — F10 profile resolver for PRD profile validation.
	// Injected from main.go so internal/autonomous stays free of
	// internal/profile dependencies. Nil = profile attachment skips
	// validation (operator gets an unchecked profile name).
	profileResolver ProfileResolver

	// BL244 — plugin session injection. When set, called with the PRD's
	// Type before each task spawn; non-empty result is forwarded in
	// SpawnRequest.ContextPrepend. Nil = no plugin context injected.
	contextFn func(prdType string) string

	// sessionKillerFn — when set, Cancel calls this for each task
	// session that is still running at the time of cancellation.
	// Injected from main.go to avoid a circular import on session.Manager.
	// caller names the code path that ends the session; reason says why, with
	// the numbers that path measured. The daemon records both on the session.
	sessionKillerFn func(sessionID, caller, reason string) error

	// sessionAliveFn — when set, reconcileStuckTasks uses it to skip
	// tasks whose session is still running (B90). Nil = conservative:
	// all stuck tasks are failed regardless of session state.
	sessionAliveFn func(sessionID string) bool
	// sessionListerFn — when set, reconcileStuckTasks looks for a live session
	// belonging to a task with an empty or dead session link before resetting it.
	sessionListerFn func() []SessionInfo
	// sessionLastOutputFn — when set, the boot-resume age gate counts a live
	// worker session's last output time as PRD activity. Nil = liveness only.
	sessionLastOutputFn func(sessionID string) (time.Time, bool)
	// sessionOutputFn — when set, boot-resume reads a dead session's recorded
	// output to find a completion marker written while the daemon was down.
	sessionOutputFn func(sessionID string) (output string, ok bool)
	// sessionAgentStartedFn — when set, boot-resume adopts a live session only
	// if its agent has started; a live bare shell is ended and its task reset.
	// Nil = adopt on alive (the pre-gate behaviour).
	sessionAgentStartedFn func(sessionID string) bool
	// sessionWaitFn — when set, executeOne waits on an adopted session through
	// it; nil falls back to polling sessionAliveFn.
	sessionWaitFn func(ctx context.Context, w AdoptedWait) error
	// sessionPaneFn — when set, the wait on an adopted session is held to the
	// start-confirm bound (sessionStartLimit): a session that never shows a
	// first assistant turn is ended and fails the attempt as a start failure.
	// Nil keeps the plain adopted wait.
	sessionPaneFn func(sessionID string) (string, error)
	// sessionStateFn — the session state the start-confirm bound reads; "" means
	// the bound does not apply to that session. Used only with sessionPaneFn.
	sessionStateFn func(sessionID string) string
	// sessionStartLimit — the start-confirm bound, the same value the worker
	// wait loop uses. <= 0 disables the bound.
	sessionStartLimit time.Duration
	// sessionLastLifeFn — the session's last evidence of life, which anchors the
	// adopted start-confirm window. Optional; nil measures from the task's launch.
	sessionLastLifeFn func(sessionID string) time.Time
	// decomposeSessionsFn lists live planning sessions linked to a PRD.
	decomposeSessionsFn func(prdID string) []string

	// Cost sources (see automation_cost.go): the spend ledger and the
	// per-session lifetime total used when a window predates the ledger.
	costLedger   *BudgetLedger
	sessionTotal func(sessionID string) (usd float64, llm string, ok bool)

	// autoBudget evaluates Automation and member budget policies (see
	// automation_budget.go); nil means no policy is enforced.
	autoBudget *AutomationBudget
	// autoBudgetPoll is how often a task held by an Automation budget
	// re-checks the hold (see automation_budget_admit.go); zero means the
	// default.
	autoBudgetPoll time.Duration

	// Capacity admission (see capacity.go).
	capacity     *capacity.Ledger
	capAudit     *CapacityAudit // admission wait/release audit recorder (nil drops)
	capacityKeys CapacityKeysFn
	// capacityChoices lists the candidate compute nodes of a backend+model
	// when its LLM entry pools two or more (nil = single-node behaviour).
	capacityChoices CapacityChoicesFn
	// sessionNode names the compute node an adopted session runs on.
	sessionNode SessionNodeFn
	// entryNodes counts the compute nodes an LLM entry lists (nil = unknown).
	entryNodes EntryNodesFn
	// sessionEndpoint reads the endpoint a session was placed on, for logging.
	sessionEndpoint SessionEndpointFn
	// piDefaultModel reports the provider-qualified model a pi-family launch
	// of a backend falls back to (nil = no provider known). See model_qualify.go.
	piDefaultModel PiDefaultModelFn
	// nodeLoad makes a worker's model resident on the node its lease was
	// granted on; nodeAlerts receives the error when that fails. Both are nil
	// until SetNodeLoad wires them (single-node entries never use them).
	nodeLoad   NodeLoader
	nodeAlerts NodeLoadAlerts
	// cooldownUntilFn reports when the BL30 global cooldown ends (zero when
	// none is active); waitClock is the time source for the spawn-wait loop
	// (nil = wall clock). Both are set via capacity.go.
	cooldownUntilFn func() time.Time
	waitClock       capacity.Clock

	// backoff is the per-backend usage back-off registry, shown in the
	// autonomous status (nil = none wired).
	backoff *Backoff
	// patternsFn reports the rate-limit pattern fingerprint and staleness
	// shown in the usage_backoff status (nil = not wired).
	patternsFn PatternsStatusFunc
	// budgetsFn reports the per-LLM dollar budget states shown in the
	// autonomous status (nil = not wired).
	budgetsFn BudgetsStatusFunc

	// sessionDeleteFn — when set, called after a task reaches a terminal
	// state (completed/failed/cancelled) to remove the ephemeral executor
	// session from the session list. Nil = sessions remain as killed entries.
	sessionDeleteFn func(sessionID string)

	// memorySeedFn (BL386 Phase 1) — when set, called after each spawn
	// when the PRD's MemorySeedConfig.Enabled=true. Seeds session-local
	// from applicable scopes. Nil = seeding silently skipped.
	memorySeedFn func(ctx context.Context, sessionID, prdID, storyID, projectDir string, cfg MemorySeedConfig) error

	// memoryHarvestFn (BL386 Phase 2) — when set, called after each task
	// reaches TaskCompleted when the PRD's MemoryHarvestConfig.Enabled=true.
	// Promotes session-local memories to story-shared or prd-shared.
	// Nil = harvest silently skipped.
	memoryHarvestFn func(ctx context.Context, sessionID, prdID, storyID, projectDir string, cfg MemoryHarvestConfig) error

	// auditFn — when set, called for audit-worthy autonomous events
	// (today: autonomous_file_drift_detected). Wired from main.go to the
	// operator audit log so the event lands in both JSON-lines and CEF.
	// Guarded by mu. Nil = audit silently skipped.
	auditFn func(action string, details map[string]any)
	// verifierHost — session/lease access for the startup verifier
	// reconcile. Guarded by mu. Nil = reconcile skipped.
	verifierHost VerifierHost
	// judgeHost — session/lease access for the startup guardrail-judge
	// reconcile. Guarded by mu. Nil = reconcile skipped.
	judgeHost JudgeHost
	// judgeAdopts records which guardrail-judge session was adopted for each
	// unit at startup, so a repeated reconcile adopts it as a no-op. Guarded
	// by mu.
	judgeAdopts map[string]string

	// orphanSeen records when the orphan sweep first saw each unbound worker
	// session; orphanNow is the sweep's clock (nil = time.Now). Guarded by mu.
	orphanSeen map[string]time.Time
	orphanNow  func() time.Time
	// orphanFormLogged records that the orphan sweep's session-id form
	// self-check has logged its one mismatch line. Guarded by mu.
	orphanFormLogged bool

	// stuckSeen records, per PRD, when the stuck-running watchdog first saw it
	// running with every task terminal and nothing live; stuckRefired marks the
	// PRDs whose rollup it has already re-fired once. stuckRefireFn re-launches
	// the rollup the way boot-resume does and stuckExecLiveFn reports a live
	// executor for a PRD; both are wired by API.SetExecutors. The watchdog
	// reads the orphan sweep's clock (orphanNow). Guarded by mu.
	stuckSeen       map[string]time.Time
	stuckRefired    map[string]bool
	stuckRefireFn   func(prdID string) (started bool, err error)
	stuckExecLiveFn func(prdID string) bool

	// sessionContinueFn — design §6.ii: sends a "resume" prompt to a parked
	// worker session after an await-external cycle re-admits it. Optional.
	sessionContinueFn func(sessionID string) error

	// treeLockBox — task ID → pointer to the executor's task-tree-lock
	// release closure, so an await-external cycle can release the lock while
	// the task waits (tree fully pushed) and re-claim it on re-admission.
	treeBoxMu   sync.Mutex
	treeLockBox map[string]*func()

	// alertSink — operator-facing alert callback (level, title, body).
	// Guarded by mu. Nil = alerts silently skipped. It returns the id of
	// the alert it recorded ("" when it cannot name one) so an emitter
	// raising a warning alongside the alert can link the event to it.
	alertSink func(level, title, body string) (id string)

	// attemptClock — the time source for failure escalation attempt records
	// and review holds (attemptNow). Nil reads the wall clock; tests inject a
	// fake so attempt durations are exact. Set before the Manager runs.
	attemptClock func() time.Time

	// driftGit — git runner for checkForeignFileDrift's committed-file
	// lookup. nil means defaultDriftGitRunner; tests inject a counting
	// fake.
	driftGit driftGitRunner

	// memoryVerifierFn (BL387 Phase 1a) — when set, called once per verification
	// issue after verify() returns !OK. Writes the issue to prd-shared scope so
	// retry tasks can seed from it. Nil = verifier writes silently skipped.
	memoryVerifierFn func(ctx context.Context, prdID, projectDir, content, role string) error

	// memoryScopeSeedFn (BL387 Phase 1b) — when set, called in recurseChildPRD
	// after the child PRD is created and before it is decomposed. Copies the
	// parent's prd-shared memories into the child's prd-shared scope.
	// Nil = child-PRD inheritance silently skipped.
	memoryScopeSeedFn func(ctx context.Context, fromPRDID, toPRDID, projectDir string, maxEntries int) error

	// memoryContextFn (BL387 Phase 2a) — when set, called before building the
	// decomposer prompt. Returns a formatted markdown block of relevant prior
	// context from project-shared memory. Empty return = no injection.
	// Nil = context enrichment silently skipped.
	memoryContextFn func(ctx context.Context, projectDir, query string, topK int) (string, error)

	// decisionModelFn and decisionTimeout back the decision assistant: the
	// model call (nil = unreachable, every decision escalates) and its per-call
	// timeout (zero = DefaultDecisionModelTimeout).
	decisionModelFn func(ctx context.Context, backend, model, prompt string) (string, error)
	decisionTimeout time.Duration

	// memoryCrossSeedFn (BL387 Phase 2b) — when set, called at PRD first-run
	// for each entry in MemorySeed.FromPRDs. Seeds the referenced PRD's
	// prd-shared into this PRD's prd-shared. Nil = cross-PRD seeding skipped.
	memoryCrossSeedFn func(ctx context.Context, fromPRDID, toPRDID, projectDir string, maxEntries int, roleFilter []string) error

	// memoryReportFn (BL387 Phase 3) — when set, called asynchronously when a
	// PRD reaches PRDCompleted. Returns a markdown summary of the prd-shared
	// memory scope; the result is stored on PRD.MemoryReport. Nil = no report.
	memoryReportFn func(ctx context.Context, prdID, projectDir string) (string, error)

	// memoryReportWG (BL387 Phase 3) tracks in-flight memory-report goroutines
	// so a caller (today: tests) can deterministically wait for the report to
	// be fully written — reportFn ran AND the PRD mutation + SavePRD completed —
	// before asserting or tearing down. A WaitGroup is self-synchronizing; it
	// is not additionally guarded by mu.
	memoryReportWG sync.WaitGroup

	// progressSnaps records the daily and milestone progress snapshots; nil
	// until EnableProgressSnapshots. progressSnapWG tracks in-flight milestone
	// goroutines so tests can wait for them. progressSnaps is guarded by mu,
	// the WaitGroup is self-synchronizing.
	progressSnaps  *ProgressSnapshotRunner
	progressSnapWG sync.WaitGroup

	// loop state
	ctx    context.Context
	cancel context.CancelFunc
	status LoopStatus

	// projectRoots returns the allowed project roots for request-supplied
	// directories (nil = none). projectRootsMu guards it on its own so a
	// check never waits on (or deadlocks with) mu.
	projectRoots   func() []string
	projectRootsMu sync.RWMutex
}

// ProfileResolver lets Manager validate that named F10 project +
// cluster profiles exist before attaching them to a PRD. Implemented
// by main.go which has access to internal/profile's stores.
type ProfileResolver interface {
	HasProjectProfile(name string) bool
	HasClusterProfile(name string) bool
}

// SetProfileResolver wires the profile-existence-check indirection
// (v5.26.19). Nil disables validation — operators can attach
// arbitrary profile names but the executor will fail at run time if
// the profile turns out not to exist.
func (m *Manager) SetProfileResolver(r ProfileResolver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.profileResolver = r
}

// SetContextFn (BL244) wires the plugin session-injection lookup. fn is called
// with the PRD's Type before each task spawn; a non-empty return is forwarded
// to SpawnRequest.ContextPrepend so the worker receives plugin-injected context.
func (m *Manager) SetContextFn(fn func(prdType string) string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contextFn = fn
}

// SetSessionKillerFn wires the session-kill callback used by Cancel to
// terminate any task sessions still running when a PRD is cancelled.
// Injected from main.go; nil disables the kill (sessions age out normally).
func (m *Manager) SetSessionKillerFn(fn func(sessionID, caller, reason string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionKillerFn = fn
}

// SetDecomposeSessionsFn wires the lookup of live planning (decompose)
// sessions for a PRD, used by boot-resume to clean up orphans.
func (m *Manager) SetDecomposeSessionsFn(fn func(prdID string) []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decomposeSessionsFn = fn
}

// killOrphanDecomposeSessions kills any live planning session for the PRD.
// After a daemon restart the decompose goroutine that would consume its output
// is gone, so the session is an orphan burning compute.
func (m *Manager) killOrphanDecomposeSessions(prdID string) int {
	m.mu.Lock()
	find, kill := m.decomposeSessionsFn, m.sessionKillerFn
	m.mu.Unlock()
	if find == nil || kill == nil {
		return 0
	}
	n := 0
	why := fmt.Sprintf("orphan planning session for PRD %s: the decompose goroutine that reads it did not survive the daemon restart", prdID)
	for _, id := range find(prdID) {
		if err := kill(id, "autonomous.boot-resume.orphan-decompose", why); err != nil {
			log.Printf("[autonomous] boot-resume: kill orphan decompose session %s (prd=%s): %v", id, prdID, err)
			continue
		}
		log.Printf("[autonomous] boot-resume: killed orphan decompose session %s (prd=%s)", id, prdID)
		n++
	}
	return n
}

// SetSessionAliveFn wires the liveness check used by reconcileStuckTasks (B90).
// fn returns true when sessionID still refers to a non-terminal session.
// Nil is safe: stuck tasks are conservatively marked failed on daemon start.
func (m *Manager) SetSessionAliveFn(fn func(sessionID string) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionAliveFn = fn
}

// LinkTaskSession records sessionID on the task before the worker receives its
// prompt, saves the task and binds the capacity ledger entry. It is the callback
// the session manager invokes once a new session record is persisted. Unknown
// PRD/task pairs are ignored; the post-spawn assignment in the executor remains
// as an idempotent fallback.
func (m *Manager) LinkTaskSession(prdID, taskID, sessionID string) {
	if m == nil || m.store == nil || taskID == "" || sessionID == "" {
		return
	}
	linked := m.store.MutateTask(taskID, func(t *Task) {
		if prdID == "" || t.PRDID == prdID {
			t.SessionID = sessionID
		}
	})
	if !linked {
		return
	}
	if led := m.Capacity(); led != nil {
		led.Bind(taskID, sessionID)
	}
}

// SetSessionOutputFn wires the recorded-output reader used at boot-resume to
// detect a worker that finished while the daemon was down. ok=false means the
// session record or its output is unavailable. Nil disables the check.
func (m *Manager) SetSessionOutputFn(fn func(sessionID string) (output string, ok bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionOutputFn = fn
}

// SetSessionAgentStartedFn wires the agent-started check used at boot-resume.
// fn returns true when the agent behind sessionID has started; a live session
// whose agent has not started (a bare shell from a failed launch) is not
// adopted. Nil keeps the old behaviour: a live session is adopted.
func (m *Manager) SetSessionAgentStartedFn(fn func(sessionID string) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionAgentStartedFn = fn
}

// SetSessionWaitFn wires the wait used for a session adopted at boot-resume.
// fn blocks until the session reaches its completion marker (nil error) or
// fails/stalls (non-nil error, wrapping ErrWorkerStalled for a stall). It gets
// the task identity and the persisted nudge time, so it can nudge and record
// like a fresh worker (see WaitAdopted). Nil falls back to polling the
// liveness hook.
func (m *Manager) SetSessionWaitFn(fn func(ctx context.Context, w AdoptedWait) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionWaitFn = fn
}

// SetSessionPaneFn wires the pane reader that holds a session adopted at
// boot-resume to the start-confirm bound. Nil keeps the plain adopted wait.
// The daemon wires the session manager's CapturePaneVisible.
func (m *Manager) SetSessionPaneFn(fn func(sessionID string) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionPaneFn = fn
}

// SetSessionStateFn wires the state reader for the adopted start-confirm bound.
// It returns "" for a session the bound does not apply to.
func (m *Manager) SetSessionStateFn(fn func(sessionID string) string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionStateFn = fn
}

// SetSessionStartLimit sets the start-confirm bound for adopted sessions, the
// same limit the daemon's worker wait loop applies. <= 0 disables it.
func (m *Manager) SetSessionStartLimit(limit time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionStartLimit = limit
}

// SetSessionLastLifeFn wires the last-evidence-of-life reader that anchors the
// adopted start-confirm window (see adoptedStartWatch.lastLifeFn).
func (m *Manager) SetSessionLastLifeFn(fn func(sessionID string) time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionLastLifeFn = fn
}

// adoptedStartWatchFor builds the start-confirm watch for a task's adopted
// session. It returns nil when no pane hook is wired, which keeps the plain
// adopted wait.
func (m *Manager) adoptedStartWatchFor(t *Task) *adoptedStartWatch {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessionPaneFn == nil {
		return nil
	}
	return &adoptedStartWatch{
		paneFn:     m.sessionPaneFn,
		stateFn:    m.sessionStateFn,
		killFn:     m.sessionKillerFn,
		limit:      m.sessionStartLimit,
		startedAt:  t.StartedAt,
		lastLifeFn: m.sessionLastLifeFn,
	}
}

// SetSessionDeleteFn wires the post-completion session cleanup callback.
// When set, it is called after each task reaches a terminal state so the
// ephemeral executor session is removed from the session list automatically.
// Nil = sessions remain visible as killed entries (pre-v8.33.42 behavior).
func (m *Manager) SetSessionDeleteFn(fn func(sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionDeleteFn = fn
}

// SetMemorySeedFn (BL386 Phase 1) wires the warm-start seeding callback.
// Called after each task spawn when the PRD's MemorySeedConfig.Enabled=true.
// Nil disables seeding (no-op; no error).
func (m *Manager) SetMemorySeedFn(fn func(ctx context.Context, sessionID, prdID, storyID, projectDir string, cfg MemorySeedConfig) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memorySeedFn = fn
}

// SetMemoryHarvestFn (BL386 Phase 2) wires the harvest-on-completion callback.
// Called after each task completes when the PRD's MemoryHarvestConfig.Enabled=true.
// Nil disables harvest (no-op; no error).
func (m *Manager) SetMemoryHarvestFn(fn func(ctx context.Context, sessionID, prdID, storyID, projectDir string, cfg MemoryHarvestConfig) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryHarvestFn = fn
}

// SetAuditFn wires the audit-event callback used for autonomous events
// that must reach the operator audit log (JSON-lines + CEF mirror),
// e.g. autonomous_file_drift_detected. Nil disables (no-op; no error).
func (m *Manager) SetAuditFn(fn func(action string, details map[string]any)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.auditFn = fn
}

// SetBackoff wires the per-backend usage back-off registry reported in
// the autonomous status (usage_backoff). Nil clears it.
func (m *Manager) SetBackoff(b *Backoff) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backoff = b
}

// SetPatternsStatus wires the rate-limit pattern fingerprint reported in the
// usage_backoff status (patterns_version, patterns_stale, claude_cli_version
// — the cached `claude --version` text mixed into the fingerprint).
// Nil clears it.
func (m *Manager) SetPatternsStatus(fn PatternsStatusFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.patternsFn = fn
}

// SetBudgetsStatus wires the source of the per-LLM budget states reported in
// the autonomous status (budgets). Nil clears it.
func (m *Manager) SetBudgetsStatus(fn BudgetsStatusFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.budgetsFn = fn
}

// budgetsSummary returns the budget states for the status, never nil. The
// function runs outside m.mu: it reads the LLM registry and the ledger.
func (m *Manager) budgetsSummary() []BudgetStatus {
	m.mu.Lock()
	fn := m.budgetsFn
	m.mu.Unlock()
	if fn != nil {
		if out := fn(); out != nil {
			return out
		}
	}
	return []BudgetStatus{}
}

// SetAlertSink wires the operator-facing alert callback. Nil disables.
// The sink returns the id of the alert it recorded ("" when it cannot
// name one).
func (m *Manager) SetAlertSink(fn func(level, title, body string) (id string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alertSink = fn
}

// AlertSink returns the configured alert callback, or nil when unset.
func (m *Manager) AlertSink() func(level, title, body string) (id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alertSink
}

// SetMemoryVerifierFn (BL387 Phase 1a) wires the verifier-finding write callback.
// Called once per issue after verify() fails on a task. Writes the issue to
// prd-shared so retry tasks can seed from it. Nil disables writes (no-op; no error).
func (m *Manager) SetMemoryVerifierFn(fn func(ctx context.Context, prdID, projectDir, content, role string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryVerifierFn = fn
}

// SetMemoryScopeSeedFn (BL387 Phase 1b) wires the child-PRD prd-shared inheritance
// callback. Called in recurseChildPRD after the child PRD is created and before
// Decompose. Copies parent's prd-shared into child's prd-shared. Nil = skipped.
func (m *Manager) SetMemoryScopeSeedFn(fn func(ctx context.Context, fromPRDID, toPRDID, projectDir string, maxEntries int) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryScopeSeedFn = fn
}

// SetMemoryContextFn (BL387 Phase 2a) wires the decomposer context-enrichment
// callback. Called before building the decomposer prompt; returns a formatted
// markdown block of relevant prior context from project-shared. Empty string =
// no injection. Nil = enrichment silently skipped.
func (m *Manager) SetMemoryContextFn(fn func(ctx context.Context, projectDir, query string, topK int) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryContextFn = fn
}

// SetMemoryCrossSeedFn (BL387 Phase 2b) wires the cross-PRD seeding callback.
// Called at PRD first-run for each entry in MemorySeed.FromPRDs. Copies the
// referenced PRD's prd-shared into the current PRD's prd-shared. Nil = skipped.
func (m *Manager) SetMemoryCrossSeedFn(fn func(ctx context.Context, fromPRDID, toPRDID, projectDir string, maxEntries int, roleFilter []string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryCrossSeedFn = fn
}

// SetMemoryReportFn (BL387 Phase 3) wires the auto-report callback. Called
// asynchronously when a PRD reaches PRDCompleted; result stored in PRD.MemoryReport.
// Nil = no report generated on completion.
func (m *Manager) SetMemoryReportFn(fn func(ctx context.Context, prdID, projectDir string) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryReportFn = fn
}

// WaitMemoryReports (BL387 Phase 3) blocks until every in-flight
// memory-report goroutine has finished — i.e. reportFn ran and the PRD write
// (in-memory mutation + SavePRD) completed — or until timeout elapses, in
// which case it returns an error. Tests that assert on PRD.MemoryReport should
// wait on this real goroutine-exit condition instead of polling
// PRD.MemoryReport within a fixed wall-clock deadline: the poll raced the
// report goroutine (a known flake), whereas waiting on the goroutine's own
// exit is deterministic.
func (m *Manager) WaitMemoryReports(timeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		m.memoryReportWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("autonomous: memory-report goroutine still running after %s", timeout)
	}
}

// decisionKindExists reports whether prd.Decisions already carries a decision
// of the given kind. Used to guard against appending a duplicate boot audit
// entry when the same PRD is saved more than once during the same boot pass
// (e.g. both the reconcile and the resume pass touch it).
func decisionKindExists(prd *PRD, kind string) bool {
	if prd == nil {
		return false
	}
	for i := range prd.Decisions {
		if prd.Decisions[i].Kind == kind {
			return true
		}
	}
	return false
}

// resetInProgressTasksForResume reconciles tasks left mid-flight by a prior
// daemon instance so the executor neither spawns a duplicate alongside a
// surviving session nor redoes work that is still running or already done.
// A TaskInProgress task with a live session is adopted: its capacity lease is
// re-taken, AdoptedSession is set and executeOne waits for the session instead
// of spawning. One whose session ended while the daemon was down, with the
// completion marker in its recorded output, moves to TaskVerifying. One with
// an empty or dead session (no marker) is killed if a session is recorded and
// reset to TaskPending. TaskVerifying/TaskRunningTests tasks with a dead or
// absent session resolve via ResolveVerifyingTask: a recorded ok verification
// verdict is applied (TaskCompleted) instead of blindly re-running the task;
// otherwise the task is reset to TaskPending. A genuinely live session is
// left alone (still TaskVerifying) so executeOne's own skip-spawn check picks
// it up and verifies it directly, matching reconcileStuckTasks' logic
// (B90/B93). This is the canonical boot-time task/session reconciliation;
// executeOne's kill-before-respawn guard (executor.go) is the second line of
// defense if this pass or its liveness check is wrong.
// Called from api.resumeRunningPRDs before re-launching the executor.
func (m *Manager) resetInProgressTasksForResume(prd *PRD) {
	m.mu.Lock()
	killerFn := m.sessionKillerFn
	aliveFn := m.sessionAliveFn
	outputFn := m.sessionOutputFn
	agentStartedFn := m.sessionAgentStartedFn
	m.mu.Unlock()
	if prd == nil {
		return
	}
	var reset []pendingTaskEvent
	completedCount, requeuedCount := 0, 0
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			prevStatus := t.Status
			switch t.Status {
			case TaskWaitingCapacity:
				// Waiter died with the prior daemon; it holds no session.
				//
				// Design §6.ii exception — a task parked on an EXTERNAL
				// condition (await_external sub-reason of waiting_capacity) may
				// still have its worker session alive and parked at the prompt, and
				// its capacity lease was released during the await cycle. Such a
				// task must be RE-ADMITTED on restart, not lost:
				// live session → re-take the lease (adopt, like the TaskInProgress
				// live branch above) and prompt the worker to finish; dead/absent
				// session → fall through to the plain re-queue below. Either way
				// the task resumes without having counted a retry.
				if IsAwaitExternalWaitReason(t.WaitReason) {
					if t.SessionID != "" && aliveFn != nil && aliveFn(t.SessionID) {
						if contFn := m.sessionContinueFnLocked(); contFn != nil {
							if cerr := contFn(t.SessionID); cerr != nil {
								log.Printf("[autonomous] boot-resume: await-external continue prompt to session %s: %v", t.SessionID, cerr)
							}
						}
						m.adoptCapacity(prd, t)
						t.Status = TaskInProgress
						t.AdoptedSession = true
						t.WaitReason = ""
						t.UpdatedAt = time.Now()
						reset = append(reset, pendingTaskEvent{t: t, oldStatus: prevStatus, oldSession: t.SessionID})
						log.Printf("[autonomous] boot-resume: re-admitted await-external task %s on live session %s for prd=%s (no retry counted)", t.ID, t.SessionID, prd.ID)
						continue
					}
					log.Printf("[autonomous] boot-resume: await-external task %s for prd=%s has no live session; re-queueing a fresh attempt", t.ID, prd.ID)
				}
			case TaskInProgress:
				switch decideInProgressResume(aliveFn, agentStartedFn, outputFn, t.SessionID) {
				case resumeAdopt:
					// The worker's session outlived the daemon: keep it. Re-take
					// its lease and mark it adopted so executeOne waits instead
					// of spawning.
					m.adoptCapacity(prd, t)
					t.AdoptedSession = true
					t.UpdatedAt = time.Now()
					reset = append(reset, pendingTaskEvent{t: t, oldStatus: prevStatus, oldSession: t.SessionID})
					log.Printf("[autonomous] boot-resume: adopted task %s (in_progress) on live session %s for prd=%s, capacity lease re-adopted", t.ID, t.SessionID, prd.ID)
					continue
				case resumeFinished:
					// Finished while the daemon was down: verify from the
					// recorded output; keep the session id, no kill.
					m.adoptCapacity(prd, t)
					t.Status = TaskVerifying
					t.UpdatedAt = time.Now()
					reset = append(reset, pendingTaskEvent{t: t, oldStatus: prevStatus, oldSession: t.SessionID})
					log.Printf("[autonomous] boot-resume: task %s (in_progress) finished while daemon was down (session %s), moved to verifying for prd=%s", t.ID, t.SessionID, prd.ID)
					continue
				case resumeStartFailed:
					// Live bare shell: the agent never launched, so the session is a
					// boot-time launch failure, not work. End it and reset the task
					// with the start-failure reason. No retry counter is touched.
					why := fmt.Sprintf("boot-resume: task %s was in_progress on session %s whose agent never started (boot-time launch failure: bare shell, no agent process); the session is ended and the task is requeued", t.ID, t.SessionID)
					oldSession := m.resetStartFailedAtBoot(t, killerFn, "autonomous.boot-resume.start-failed", why)
					reset = append(reset, pendingTaskEvent{t: t, oldStatus: prevStatus, oldSession: oldSession})
					log.Printf("[autonomous] boot-resume: task %s (was in_progress) reset with %q for prd=%s; session %s never started its agent", t.ID, SessionStartFailureReason, prd.ID, oldSession)
					continue
				}
				// Empty or dead session without a marker: orphaned, reset below.
			case TaskVerifying, TaskRunningTests:
				switch ResolveVerifyingResume(aliveFn, stableAgentStarted(agentStartedFn), outputFn, t) {
				case VerifyingResumeStartFailed:
					// Live bare shell: the worker's agent never launched, so the session is a
					// boot-time launch failure. End it and requeue the task with the
					// start-failure reason. No retry is counted.
					why := fmt.Sprintf("boot-resume: task %s was verifying on session %s whose agent never started (boot-time launch failure: bare shell, no agent process); the session is ended and the task is requeued", t.ID, t.SessionID)
					oldSession := m.resetStartFailedAtBoot(t, killerFn, "autonomous.boot-resume.start-failed", why)
					reset = append(reset, pendingTaskEvent{t: t, oldStatus: prevStatus, oldSession: oldSession})
					log.Printf("[autonomous] boot-resume: task %s (was %s) reset with %q for prd=%s; session %s never started its agent", t.ID, prevStatus, SessionStartFailureReason, prd.ID, oldSession)
					continue
				case VerifyingResumeReverify:
					// Worker finished, no verdict recorded: keep the task verifying
					// with its session, result and SHAs untouched. No kill, no
					// retry counted; executeOne verifies it once.
					m.adoptCapacity(prd, t)
					log.Printf("[autonomous] boot-resume: prd=%s task %s kept verifying (was %s): worker finished, no verdict recorded, session %s", prd.ID, t.ID, prevStatus, t.SessionID)
					continue
				case VerifyingResumeKeepLive:
					// Session still live; leave it for executeOne to resume. Re-take
					// its capacity lease now (BL462 F1) so admission counts it before
					// any other task on this PRD or another PRD is admitted.
					m.adoptCapacity(prd, t)
					log.Printf("[autonomous] boot-resume: prd=%s task %s kept (was %s) on live session %s, capacity lease re-adopted", prd.ID, t.ID, prevStatus, t.SessionID)
					continue
				case VerifyingResumeComplete:
					// A recorded ok verification verdict stands: complete the task
					// instead of resetting it for a blind re-run. Verification is
					// kept untouched.
					if killerFn != nil && t.SessionID != "" {
						why := fmt.Sprintf("boot-resume: task %s had a recorded ok verification; its dead session is ended before the task completes", t.ID)
						if err := killerFn(t.SessionID, "autonomous.boot-resume.complete-from-verdict", why); err != nil {
							log.Printf("[autonomous] boot-resume: kill orphaned session %s: %v", t.SessionID, err)
						}
					}
					now := time.Now()
					t.Status = TaskCompleted
					t.CompletedAt = &now
					t.Error = ""
					oldSession := t.SessionID
					t.SessionID = ""
					t.UpdatedAt = now
					reset = append(reset, pendingTaskEvent{t: t, oldStatus: prevStatus, oldSession: oldSession})
					completedCount++
					log.Printf("[autonomous] boot-resume: completed task %s (was %s) for prd=%s from recorded ok verification", t.ID, prevStatus, prd.ID)
					continue
				}
				// Reset: fall through to the reset-to-pending path below;
				// it was an orphaned verifying task re-queued for retry.
				requeuedCount++
			default:
				continue
			}
			if killerFn != nil && t.SessionID != "" {
				why := fmt.Sprintf("boot-resume: task %s was %s with no live session; the orphaned session is ended before the task is requeued", t.ID, prevStatus)
				if err := killerFn(t.SessionID, "autonomous.boot-resume.reset", why); err != nil {
					log.Printf("[autonomous] boot-resume: kill orphaned session %s: %v", t.SessionID, err)
				}
			}
			now := time.Now()
			oldSession := t.SessionID
			m.releaseCapacity(t.ID)
			t.SessionID = ""
			t.Status = TaskPending
			t.Error = ""
			t.UpdatedAt = now
			reset = append(reset, pendingTaskEvent{t: t, oldStatus: prevStatus, oldSession: oldSession})
			log.Printf("[autonomous] boot-resume: reset task %s (was %s) for prd=%s", t.ID, prevStatus, prd.ID)
		}
	}
	if len(reset) == 0 {
		return
	}
	now := time.Now()
	prd.UpdatedAt = now
	// Auditable verification of the orphaned-verifying resolutions: record how
	// many verifying tasks completed from a recorded ok verdict vs how many
	// were re-queued for retry. Guarded so a PRD saved twice in the same boot
	// pass never carries two verifying_resolution entries.
	if completedCount+requeuedCount > 0 && !decisionKindExists(prd, verifyingResolution) {
		prd.Decisions = append(prd.Decisions, Decision{
			At:    now,
			Kind:  verifyingResolution,
			Actor: "autonomous",
			Note:  fmt.Sprintf("boot-resume resolved %d orphaned verifying task(s) to completed (recorded ok verification) and re-queued %d for retry", completedCount, requeuedCount),
		})
	}
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] boot-resume: save prd=%s: %v", prd.ID, err)
		return
	}
	m.emitPendingTaskEvents(prd, reset, lastDecision(prd, verifyingResolution))
}

// resetKilledTasksForResume checks a PRDFailed PRD: if every failed task has
// either no SessionID or a dead session (no live session), it resets those
// tasks to pending so the executor can retry them after a daemon restart.
// Returns true if any task was reset (meaning the caller should re-run the PRD).
// Stories that are in a failed/blocked state are also reset to pending.
func (m *Manager) resetKilledTasksForResume(prd *PRD) bool {
	m.mu.Lock()
	aliveFn := m.sessionAliveFn
	killerFn := m.sessionKillerFn
	m.mu.Unlock()

	// Check whether any task is in an in-progress state with a live session.
	// A task that is already in `failed` state cannot have an actively running
	// session for THIS task (the session may have been reused for other work),
	// so we only guard on non-terminal statuses.
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if isTaskTerminal(t.Status) {
				continue // terminal tasks can't have live executor sessions
			}
			if aliveFn != nil && t.SessionID != "" && aliveFn(t.SessionID) {
				return false // a task is genuinely still running; leave it
			}
		}
	}

	// Check that at least one failed task exists and no task is completed.
	// A completed task means real work was done before the kill — partial runs
	// should not be auto-recovered; use reset_task per task instead.
	hasResettable := false
	for si := range prd.Story {
		for _, t := range prd.Story[si].Tasks {
			if t.Status == TaskCompleted {
				return false
			}
			if t.Status == TaskFailed {
				hasResettable = true
			}
		}
	}
	if !hasResettable {
		return false
	}

	// Reset all failed tasks to pending.
	now := time.Now()
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.Status != TaskFailed {
				continue
			}
			if killerFn != nil && t.SessionID != "" {
				why := fmt.Sprintf("boot-resume: failed task %s is reset to pending; its retained session is ended before the retry", t.ID)
				if err := killerFn(t.SessionID, "autonomous.boot-resume.failed-reset", why); err != nil {
					log.Printf("[autonomous] boot-resume: kill orphaned session %s: %v", t.SessionID, err)
				}
			}
			oldStatus, oldSession := t.Status, t.SessionID
			t.Status = TaskPending
			t.Error = ""
			t.SessionID = ""
			t.RetryCount = 0
			t.UpdatedAt = now
			if m.store.SaveTask(t) == nil {
				m.emitTaskStatus(prd, t, oldStatus, nil, nil)
				m.emitTaskSession(prd, t, oldSession)
			}
			log.Printf("[autonomous] boot-resume: reset killed task %s to pending for prd=%s", t.ID, prd.ID)
		}
		// Reset stories that are not completed back to pending.
		s := &prd.Story[si]
		if s.Status != StoryCompleted {
			s.Status = StoryPending
		}
	}
	old := prd.Status
	prd.Status = PRDRunning
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At:    now,
		Kind:  "boot_resume_recovery",
		Actor: "autonomous",
		Note:  "PRDFailed auto-recovered: all tasks reset to pending after daemon restart",
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] boot-resume: save prd=%s: %v", prd.ID, err)
		return false
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "boot_resume_recovery"), nil)
	return true
}

// killPRDSessions iterates every task in prd and calls sessionKillerFn for
// each non-empty SessionID. Best-effort — errors are logged but not returned.
func (m *Manager) killPRDSessions(prd *PRD) {
	m.mu.Lock()
	fn := m.sessionKillerFn
	m.mu.Unlock()
	if fn == nil || prd == nil {
		return
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			sid := prd.Story[si].Tasks[ti].SessionID
			if sid != "" {
				why := fmt.Sprintf("PRD %s cancelled: task %s session ends with the PRD", prd.ID, prd.Story[si].Tasks[ti].ID)
				if err := fn(sid, "autonomous.cancel.prd", why); err != nil {
					log.Printf("[autonomous] cancel: kill session %s: %v", sid, err)
				}
			}
		}
	}
}

// SetGuardrail wires the per-story / per-task guardrail indirection
// (BL191 Q6, v5.10.0). Nil = guardrails disabled even when
// PerTaskGuardrails / PerStoryGuardrails are configured (the executor
// short-circuits with a clear log line). main.go wires this to the
// BL103 validator path during daemon startup.
func (m *Manager) SetGuardrail(fn GuardrailFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guardrail = fn
}

// SetPlanReview wires the plan-review indirection (see plan_review.go).
// Nil = when a mode asks for a review, a degraded 'skipped' verdict is
// recorded instead (never a decompose / approve failure). Only called
// at daemon wiring time, like SetGuardrail.
func (m *Manager) SetPlanReview(fn PlanReviewFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.planReview = fn
}

// EmitPRDUpdate (v5.24.0) is the explicit broadcast trigger for
// callers that wrote through Store.SavePRD directly and want the WS
// broadcast to fire. The Manager's own mutator methods (Approve,
// Reject, Decompose, etc.) call this after a successful save; the
// `Store` layer is unaware of the WS path and stays decoupled.
//
// BL427 — publishes a KindPRDUpdated LifecycleEvent through the
// lifecycle dispatcher (formerly a single-slot callback). A PRD that
// no longer loads is reported with Deleted=true. main.go's WS adapter
// subscribes to KindPRDUpdated and maps the event back to the legacy
// frame with PRDUpdatePayload. Delivery is async per subscriber.
func (m *Manager) EmitPRDUpdate(prdID string) {
	if m == nil {
		return
	}
	ev := LifecycleEvent{Kind: KindPRDUpdated, Level: LevelPRD, PRDID: prdID}
	if prd, _ := m.store.GetPRD(prdID); prd != nil {
		ev.NewStatus = string(prd.Status)
		ev.ParentPRDID = prd.ParentPRDID
	} else {
		ev.Deleted = true
	}
	m.emit(ev)
}

// NewManager constructs the Manager and opens the store under dataDir.
func NewManager(dataDir string, cfg Config, decompose DecomposeFn) (*Manager, error) {
	st, err := NewStore(dataDir)
	if err != nil {
		return nil, err
	}
	automs, err := NewAutomationStore(st.dir, func(id string) (*PRD, bool) {
		return st.GetPRD(id)
	})
	if err != nil {
		return nil, err
	}
	types := make([]AutomatonType, len(builtinTypes))
	copy(types, builtinTypes)
	cfg.FailureEscalation = normalizeFailureEscalationPolicy(cfg.FailureEscalation)
	m := &Manager{
		cfg:         cfg,
		store:       st,
		automations: automs,
		dataDir:     dataDir,
		templates:   newTemplateStore(dataDir),
		decompose:   decompose,
		types:       types,
	}
	m.initGuardrailLibrary() // BL303 S2
	m.lifecycleBus()         // BL427 — create dispatcher + cache hostname
	return m, nil
}

// Templates exposes the TemplateStore for REST/API wiring.
func (m *Manager) Templates() *TemplateStore { return m.templates }

// SetConfig replaces the runtime config (hot-reload entry).
func (m *Manager) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// gate_disagreement: unset or unrecognized reads as "warn" (writes are
	// rejected upstream in API.SetConfig); persist the canonical value so
	// GET exposes it.
	if cfg.GateDisagreement != "warn" && cfg.GateDisagreement != "block" {
		cfg.GateDisagreement = "warn"
	}
	// failure_escalation: an unset max_failures reads as 3 and a nil enabled
	// reads as true.
	cfg.FailureEscalation = normalizeFailureEscalationPolicy(cfg.FailureEscalation)
	m.cfg = cfg
}

// Config returns a copy of the current config.
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// EffectivePlanCoverage resolves the plan-coverage settings in effect
// from the manager's current config (Config.PlanCoverageCheck /
// .PlanCoverageMinRatio) via the canonical config normalizers: an empty
// unset mode becomes "block" (the safe default), a zero/negative min
// ratio becomes 1.0 (require full coverage), and a ratio above 1 is
// clamped to 1.0. It returns an error if the stored mode is not one of
// off|warn|block — callers must surface that error rather than guess a
// mode.
func (m *Manager) EffectivePlanCoverage() (config.PlanCoverageSettings, error) {
	cfg := m.Config()
	return config.EffectivePlanCoverage(cfg.PlanCoverageCheck, cfg.PlanCoverageMinRatio)
}

// decomposeAttemptOutcome is the shared outcome validateAndStorePlan
// reports for one decompose attempt's parsed plan.
type decomposeAttemptOutcome int

const (
	// decomposeAttemptStored — every shared validation passed and the
	// stories are stored; the caller may finalize (see
	// finalizeDecomposedPlan).
	decomposeAttemptStored decomposeAttemptOutcome = iota
	// decomposeAttemptRetry — a retryable failure was recorded within
	// budget; the caller must loop back and retry the decompose.
	decomposeAttemptRetry
	// decomposeAttemptTerminal — the shared failure is terminal (budget
	// exhausted); the caller must surface the error (the helper has
	// already run failDecomposeAttempt).
	decomposeAttemptTerminal
)

// validateAndStorePlan is the SHARED validation + store tail that both
// decompose paths (Decompose and decomposeStreamingCore) ran as drifted
// copies of each other before this existed: the zero-story guard, the B124
// verification-script-coverage check, and SetStories (depends_on
// resolution + validation). Each step keeps the exact retry-with-budget
// (then draft-rollback) behavior the twins had, surfacing the retry/terminal
// distinction to the caller as decomposeAttemptOutcome.
func (m *Manager) validateAndStorePlan(prd *PRD, prdID string, s []Story) (decomposeAttemptOutcome, error) {
	// A valid-JSON plan with no usable stories must not be stored as a
	// finished decompose: a "content"-category failure, retryable within
	// the RetryPolicy budget and recorded as terminal when that budget is
	// exhausted (same path as below).
	if zerr := validateParsedPlan(s); zerr != nil {
		if m.retryDecomposeFailure(prd, "content", "decompose-zero-stories", zerr.Error()) {
			return decomposeAttemptRetry, nil
		}
		m.failDecomposeAttempt(prd, "decompose-zero-stories", zerr)
		return decomposeAttemptTerminal, zerr
	}
	// B124 — kick back a tooled plan that skipped verification scripts
	// entirely despite planning real file changes. Content, not scope:
	// the plan is fixable by adding scripts, so it retries from the
	// content budget (AutoFixRetries fallback in resolveRetryPolicy), and
	// retryDecomposeFailure's hint feeds the offending task names into
	// the next attempt.
	if verr := validateVerificationScriptCoverage(prd.ProjectDir, s); verr != nil {
		if m.retryDecomposeFailure(prd, "content", "verification-script-coverage", verr.Error()) {
			return decomposeAttemptRetry, nil
		}
		m.failDecomposeAttempt(prd, "verification-script-coverage", verr)
		return decomposeAttemptTerminal, verr
	}
	// Unscoped tmux kill-server / kill-session -a in a spec or verification
	// script: a "scope"-category failure, same retry path as B124.
	if terr := validateNoUnscopedTmux(prd.ProjectDir, s); terr != nil {
		if m.retryDecomposeFailure(prd, "scope", "unscoped-tmux", terr.Error()) {
			return decomposeAttemptRetry, nil
		}
		m.failDecomposeAttempt(prd, "unscoped-tmux", terr)
		return decomposeAttemptTerminal, terr
	}
	// Release-version gate and verification-script scope gate: restored
	// here from main (they were duplicated in both decompose twins, and the
	// shared tail must enforce them for both paths).
	if err := validateNoFixedReleaseVersion(s); err != nil {
		if m.retryDecomposeFailure(prd, "content", "release-version-fixed", err.Error()) {
			return decomposeAttemptRetry, nil
		}
		m.failDecomposeAttempt(prd, "release-version-fixed", err)
		return decomposeAttemptTerminal, err
	}
	if err := validateVerificationScriptScope(prd.ProjectDir, s); err != nil {
		if m.retryDecomposeFailure(prd, "scope", "verification-script-scope", err.Error()) {
			return decomposeAttemptRetry, nil
		}
		m.failDecomposeAttempt(prd, "verification-script-scope", err)
		return decomposeAttemptTerminal, err
	}
	// SetStories is where depends_on gets resolved + validated: it rejects
	// the whole plan (naming the exact bad entry or the cycle, storing
	// nothing) when a reference can't be resolved. A content rejection in
	// the same retry-then-terminal path — the only other error it can
	// return ("prd not found") cannot happen for a PRD loaded moments
	// ago and alive for the duration of this call.
	if err := m.store.SetStories(prdID, s); err != nil {
		if m.retryDecomposeFailure(prd, "content", "decompose-depends-on", err.Error()) {
			return decomposeAttemptRetry, nil
		}
		m.failDecomposeAttempt(prd, "decompose-depends-on", err)
		return decomposeAttemptTerminal, err
	}
	return decomposeAttemptStored, nil
}

// finalizeDecomposedPlan is the SHARED post-store tail of both decompose
// paths (formerly the copy-pasted block after each of their retry
// loops): clears the failure hint, attaches the deterministic
// plan-coverage report computed right at this moment (see
// recordPlanCoverage — stored in ALL modes, pinned to the effective
// plan-coverage settings at compute time so a later config flip
// can't re-interpret this stored report), transitions the PRD to
// needs_review with the decompose Decision + scope warnings, runs the
// BL268 persist-check, and removes the scratch decompose artifact.
func (m *Manager) finalizeDecomposedPlan(prd *PRD, prdID, backend, prompt, raw, title string, s []Story) (*PRD, error) {
	prd.LastDecomposeFailure = nil // success — clear the single-use hint
	prd.DecomposeSessionFailures = 0

	settings, serr := m.EffectivePlanCoverage()
	if serr != nil {
		// A plan-coverage mode that cannot be resolved is a config
		// error; do not leave the PRD half-finalized.
		m.failDecomposeAttempt(prd, "plan-coverage-config", serr)
		return nil, serr
	}
	// Compute + store the deterministic (non-LLM) coverage report BEFORE
	// saving the PRD so it lands atomically with the validated plan it
	// describes — in off/warn/block alike.
	m.recordPlanCoverage(prd, s, settings)

	if title != "" {
		prd.Title = title
	}
	// BL191 Q1: operator review/approve gate. Status lands in needs_review
	// rather than active/running. Approve / Reject / RequestRevision are
	// the explicit transitions out.
	// Smoke verification: apply the configured smoke-task verification
	// backend/model while the PRD is still needs_review (the state where
	// an operator can still see + correct it). Never clobbers explicit
	// per-task overrides; no-op unless smoke_verification_backend /
	// smoke_verification_model are set. See ApplySmokeVerificationOverrides.
	ApplySmokeVerificationOverrides(prd, m.cfg.SmokeVerificationBackend, m.cfg.SmokeVerificationModel)
	reviewOld := prd.Status
	prd.Status = PRDNeedsReview
	prd.Decisions = append(prd.Decisions, Decision{
		At:            time.Now(),
		Kind:          "decompose",
		Backend:       backend,
		PromptChars:   len(prompt),
		ResponseChars: len(raw),
		Actor:         "autonomous",
	})
	prd.ScopeWarnings = m.planScopeWarnings(prd, s)
	// A save failure here is a real decompose failure: route it through
	// the shared give-up tail (log, keep artifact, record
	// LastDecomposeFailure, roll back to PRDDraft) rather than returning
	// a bare error that strands the completed session's output.
	if err := m.store.SavePRD(prd); err != nil {
		m.failDecomposeAttempt(prd, "decompose-persist", err)
		return nil, err
	}
	// Only the persisted state counts: a decompose whose stories did not
	// land in the store is a failure, and its output artifact is kept.
	if perr := m.checkPersistedDecompose(prdID); perr != nil {
		m.failDecomposeAttempt(prd, "decompose-persist", perr)
		return nil, perr
	}
	// Plan review (2026-10-17 gate): pre-approval judgment over the
	// just-decomposed plan. Runs before the status emit so the verdict
	// decision row lands right after the decompose row. Never fails
	// Decompose — a reviewer outage is recorded as a verdict, not an
	// error (see plan_review.go).
	m.runPlanReview(prd, s, "")
	RemoveDecomposeOutputArtifact(m.dataDir, prdID)
	m.emitPRDStatus(prd, reviewOld, lastDecision(prd, "decompose"), nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// gateDisagreementPolicy returns the effective autonomous.gate_disagreement
// policy read by the executor: "block" when configured, "warn" otherwise
// (unset or unrecognized values read as "warn").
func (m *Manager) gateDisagreementPolicy() string {
	m.mu.Lock()
	p := m.cfg.GateDisagreement
	m.mu.Unlock()
	if p == "block" {
		return "block"
	}
	return "warn"
}

// Store exposes the underlying store (REST handlers and tests use it).
func (m *Manager) Store() *Store { return m.store }

// AutomationStore exposes the AutomationStore (REST handlers and tests).
func (m *Manager) AutomationStore() *AutomationStore { return m.automations }

// CreatePRD records a draft PRD without decomposing — call Decompose
// next or pass to Run() which decomposes lazily.
func (m *Manager) CreatePRD(spec, projectDir, backend, model string, effort Effort) (*PRD, error) {
	if err := m.checkInjectionGuard("prd spec", spec); err != nil {
		return nil, err
	}
	if err := validateModelForBackend(backend, model); err != nil {
		return nil, fmt.Errorf("create prd: %w", err)
	}
	prd, err := m.store.CreatePRD(spec, projectDir, backend, model, effort)
	if err != nil {
		return nil, err
	}
	// BL303 S2 T09 — apply default skills from config.
	m.mu.Lock()
	defaults := append([]string{}, m.cfg.DefaultSkills...)
	m.mu.Unlock()
	if len(defaults) > 0 {
		prd.Skills = defaults
		_ = m.store.SavePRD(prd)
	}
	// BL407 Phase 5 — apply the daemon-wide AutoPR default at creation
	// time, same shape as DefaultSkills above.
	m.mu.Lock()
	defaultAutoPR := m.cfg.DefaultAutoPR
	m.mu.Unlock()
	if defaultAutoPR && !prd.Git.AutoPR {
		prd.Git.AutoPR = true
		_ = m.store.SavePRD(prd)
	}
	return prd, nil
}

// SetSkillsDir wires the path to the skills directory (BL303 S2 T08).
// When set, Manager.loadSkillGuardrails reads skill manifests to register
// skill-contributed guardrails before PRD execution.
func (m *Manager) SetSkillsDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skillsDir = dir
}

// Decompose calls the LLM via the configured DecomposeFn and persists
// the resulting story tree. Returns the updated PRD.
//
// BL191 (v5.2.0) — status transitions: PRDDraft (or PRDRevisionsAsked)
// → PRDPlanning → PRDNeedsReview. The operator must explicitly
// Approve before Run is allowed; see Manager.Approve and Manager.Run.
// Planning records a Decision row on the PRD with the LLM call
// metadata (backend, prompt size, response size).
func (m *Manager) Decompose(prdID string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if m.decompose == nil {
		return nil, fmt.Errorf("decompose fn not configured")
	}
	if prd.IsTemplate {
		return nil, fmt.Errorf("prd %q is a template; instantiate it first", prdID)
	}
	// BL407 Phase 1 extension (found live 2026-10-11 creating BL404's
	// own PRD): session-based decompose needs a real project_dir, same
	// as Run() does, so a worktree-mode PRD gets one here too — not
	// just at Run() time, which decompose happens well before. See
	// ensureWorktreeIfConfigured's own doc comment.
	if created, werr := m.ensureWorktreeIfConfigured(prd); werr != nil {
		return nil, fmt.Errorf("prd %q worktree setup: %w", prdID, werr)
	} else if created {
		log.Printf("[autonomous] prd=%s worktree created at %s (branch=%s) for decompose", prdID, prd.ProjectDir, prd.Git.Branch)
	}
	// v8.20.0 — use the per-PRD planning backend (decomposition_profile) when
	// set; otherwise fall back to the global planning_backend config. The
	// execution backend (Backend) is reserved for task sessions only.
	// The Automation's planning fields sit between the PRD's own
	// decomposition fields and the global planning config. Twin in
	// decomposeStreamingCore(); keep both in sync.
	planBackend, planGlobalModel := effectivePlanning(m.AutomationForPRD(prd), m.cfg)
	backend := prd.DecompositionProfile
	if backend == "" {
		backend = planBackend
	}
	effort := prd.Effort
	if effort == "" {
		effort = Effort(m.cfg.PlanningEffort)
	}
	// Mark in-flight so the operator can see it from /api/autonomous/prds/{id}.
	// Also persists the worktree assignment above in the same write. The status
	// bump is a plain field write on the store's shared *PRD, so it must run under
	// the store lock to be ordered against a concurrent SnapshotPRD deep copy and
	// persist's JSON encoding (see Store.WithLock); the prior value is captured in
	// the same critical section. SavePRD below keeps its Rev bump, test hooks and
	// decision trims unchanged.
	var planOld PRDStatus
	m.store.WithLock(func() {
		planOld = prd.Status
		prd.Status = PRDPlanning
		prd.UpdatedAt = time.Now()
	})
	if m.store.SavePRD(prd) == nil {
		m.emitPRDStatus(prd, planOld, nil, nil)
	}

	// BL387 Phase 2a — enrich decomposer prompt with prior context from project-shared.
	prompt := fmt.Sprintf(PlanningPrompt, prd.Spec)
	m.mu.Lock()
	ctxFn := m.memoryContextFn
	m.mu.Unlock()
	if ctxFn != nil && prd.ProjectDir != "" {
		if priorCtx, cerr := ctxFn(context.Background(), prd.ProjectDir, prd.Spec, 15); cerr == nil && priorCtx != "" {
			prompt = fmt.Sprintf(PlanningPrompt, prd.Spec+"\n\n"+priorCtx)
		}
	}
	// BL406 — see rules_context.go. Found live 2026-10-11: this was
	// promised by the plan's own Context section and never implemented.
	prompt += m.rulesContextSuffix(prd)
	// BL416 Phase 2 — see verification_script_prompt.go. Twin call in decomposeStreamingCore(); keep both in sync.
	prompt += m.verificationScriptSuffix(prd)

	planModel := resolvePlannedModel(backend, prd.DecompositionModel, prd.Backend, prd.Model, planBackend, planGlobalModel, m.cfg.VerificationModel)
	// v9.10.19 — a pi-family decompose session must launch with an explicit
	// non-empty model — never its host's built-in default (on a federated
	// peer that would be the operator-local node model the session exists
	// to replace). A config failure retries nothing, so fail immediately
	// (no infra retry budget consumed) and record the reason loud.
	if serr := RequirePinnedModel(backend, planModel,
		"the PRD's decomposition_model", "autonomous.planning_model", "the PRD's model", "autonomous.verification_model"); serr != nil {
		m.failDecomposeAttempt(prd, "decompose-config", serr)
		return nil, serr
	}
	// BL410 remediation (2026-10-13) — bounded, hint-fed retry for an
	// infra/scope-classified decompose failure, instead of immediately
	// rolling back to PRDDraft on the very first failure. Parse failures
	// are NOT in this class: a parse failure means the session DID write
	// a plan (the session-based decompose fn only returns non-empty
	// content when the output file was written), so it fails immediately
	// below rather than consuming infra retry budget. See
	// retryDecomposeFailure's doc comment for the full rationale; see
	// decomposeStreamingCore's identical loop for the twin copy this
	// drifted-duplicate pair requires; both tails now route through the
	// shared validateAndStorePlan + finalizeDecomposedPlan helpers below.
	for {
		attemptPrompt := prompt
		if prd.LastDecomposeFailure != nil {
			attemptPrompt += fmt.Sprintf("\n\nYour previous attempt failed: %s\nFix this in your next response.", prd.LastDecomposeFailure.Detail)
		}
		r, derr := m.decomposeAdmitted(prd, backend, planModel, DecomposeRequest{Spec: attemptPrompt, Backend: backend, Effort: effort, Model: planModel, ProjectDir: prd.ProjectDir, ScopeBlock: ScopeBlock(prd), PRDID: prd.ID, TimeoutSeconds: m.cfg.PlanningTimeoutSeconds})
		if derr != nil {
			if errors.Is(derr, errDecomposeCancelled) {
				// Operator cancel/reject dropped the planner from the queue:
				// the PRD already holds a terminal status, so no retry budget
				// and no draft rollback (failDecomposeAttempt would clobber it).
				return nil, derr
			}
			if isOneShotNoOutput(derr) {
				if m.noteDecomposeSessionFailure(prd, backend, planModel, derr) {
					return nil, derr
				}
				continue
			}
			if m.retryDecomposeFailure(prd, "infra", "decompose-llm", derr.Error()) {
				continue
			}
			m.failDecomposeAttempt(prd, "decompose-llm", derr)
			return nil, fmt.Errorf("LLM plan: %w", derr)
		}
		t, s, perr := ParsePlanning(r)
		if perr != nil {
			// Written-verdict class (2026-10-19 incident; same class as
			// BL478's 28KB-discarded-plan bug): a parse failure means the
			// session wrote an output file that failed to parse — a verdict
			// was produced, so this is NOT an infra-category failure and
			// must not consume infra retry budget (retrying risks re-running
			// twice and discarding a good second plan). Fail immediately;
			// failDecomposeAttempt still records the parse reason on
			// LastDecomposeFailure.Detail so an operator sees WHY.
			m.failDecomposeAttempt(prd, "decompose-parse", perr)
			return nil, perr
		}
		// SHARED validation + store tail for the parsed plan (see
		// validateAndStorePlan) — the single source of truth both
		// decompose paths now call: retryable failures loop back into
		// the hint-fed retry above, terminal failures are already
		// recorded and surface to the caller, and success finalizes
		// through the shared tail (finalizeDecomposedPlan).
		switch outcome, oerr := m.validateAndStorePlan(prd, prdID, s); outcome {
		case decomposeAttemptRetry:
			continue
		case decomposeAttemptTerminal:
			return nil, oerr
		default: // decomposeAttemptStored — plan validated and stored.
			updated, ferr := m.finalizeDecomposedPlan(prd, prdID, backend, prompt, r, t, s)
			if ferr != nil {
				return nil, ferr
			}
			return updated, nil
		}
	}
}

// StoryCallback is called by decomposeStreamingCore as each story
// becomes available. index is 0-based; total is the total story count.
// The callback must not block.
type StoryCallback func(index int, total int, story Story)

// DecomposeStreaming satisfies the server.AutonomousAPI interface.
// cb receives each story as an any-typed Story value so the server
// package doesn't need to import internal/autonomous.
//
// BL328 — used by the async POST /decompose handler.
func (m *Manager) DecomposeStreaming(prdID string, cb func(index, total int, story any)) (any, error) {
	var wrapped StoryCallback
	if cb != nil {
		wrapped = func(index, total int, s Story) { cb(index, total, s) }
	}
	return m.decomposeStreamingCore(prdID, wrapped)
}

// decomposeStreamingCore is the typed implementation used internally and
// by tests. It runs the LLM planning phase and calls cb for each story.
func (m *Manager) decomposeStreamingCore(prdID string, cb StoryCallback) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if m.decompose == nil {
		return nil, fmt.Errorf("decompose fn not configured")
	}
	if prd.IsTemplate {
		return nil, fmt.Errorf("prd %q is a template; instantiate it first", prdID)
	}
	// BL407 Phase 1 extension — see Decompose()'s identical call for
	// the full explanation. decomposeStreamingCore is a drifted
	// duplicate of Decompose() by design history (see this function's
	// own "operator-reported" comment below); kept in sync here too
	// rather than risk a third hand-copied gate.
	if created, werr := m.ensureWorktreeIfConfigured(prd); werr != nil {
		return nil, fmt.Errorf("prd %q worktree setup: %w", prdID, werr)
	} else if created {
		log.Printf("[autonomous] prd=%s worktree created at %s (branch=%s) for decompose", prdID, prd.ProjectDir, prd.Git.Branch)
	}
	// The Automation's planning fields sit between the PRD's own
	// decomposition fields and the global planning config. Twin in
	// Decompose(); keep both in sync.
	planBackend, planGlobalModel := effectivePlanning(m.AutomationForPRD(prd), m.cfg)
	backend := prd.DecompositionProfile
	if backend == "" {
		backend = planBackend
	}
	effort := prd.Effort
	if effort == "" {
		effort = Effort(m.cfg.PlanningEffort)
	}
	// Mark in-flight. Also persists the worktree assignment above.
	// Same lock-ordered status bump as Decompose() (see Store.WithLock); the
	// prior value is captured in the same critical section.
	var planOld PRDStatus
	m.store.WithLock(func() {
		planOld = prd.Status
		prd.Status = PRDPlanning
		prd.UpdatedAt = time.Now()
	})
	if m.store.SavePRD(prd) == nil {
		m.emitPRDStatus(prd, planOld, nil, nil)
	}

	// BL387 Phase 2a — enrich decomposer prompt with prior context from project-shared.
	prompt := fmt.Sprintf(PlanningPrompt, prd.Spec)
	m.mu.Lock()
	ctxFn2 := m.memoryContextFn
	m.mu.Unlock()
	if ctxFn2 != nil && prd.ProjectDir != "" {
		if priorCtx, cerr := ctxFn2(context.Background(), prd.ProjectDir, prd.Spec, 15); cerr == nil && priorCtx != "" {
			prompt = fmt.Sprintf(PlanningPrompt, prd.Spec+"\n\n"+priorCtx)
		}
	}
	// BL406 — see rules_context.go and Decompose()'s identical call.
	prompt += m.rulesContextSuffix(prd)
	// BL416 Phase 2 — see verification_script_prompt.go. Twin call in Decompose(); keep both in sync.
	prompt += m.verificationScriptSuffix(prd)

	// Operator-reported: a PRD configured for claude-code kept running
	// decompose against the daemon's global qwen default instead. Root
	// cause: this streaming path is a drifted duplicate of Decompose()'s
	// setup and was missing the per-PRD DecompositionModel tier entirely,
	// jumping straight to the global default whenever one was configured
	// — backend resolution above was correct (so the claude CLI launched
	// fine), but the --model argument fed into it was always wrong.
	// Restored to the same tiered resolution Decompose() uses.
	planModel2 := resolvePlannedModel(backend, prd.DecompositionModel, prd.Backend, prd.Model, planBackend, planGlobalModel, m.cfg.VerificationModel)
	// v9.10.19 — pi-family decompose session: explicit non-empty model or
	// loud failure, never the host's built-in default (see the twin check
	// in Decompose above). Config failure: fail immediately, no retry budget.
	if serr := RequirePinnedModel(backend, planModel2,
		"the PRD's decomposition_model", "autonomous.planning_model", "the PRD's model", "autonomous.verification_model"); serr != nil {
		m.failDecomposeAttempt(prd, "decompose-config", serr)
		return nil, serr
	}
	// BL410 remediation (2026-10-13) — see Decompose()'s identical loop
	// for the full explanation; retryDecomposeFailure is the shared
	// helper both copies of this drifted-duplicate pair call, so the
	// retry logic itself exists in exactly one place; the retry loop and
	// both tails now share the validateAndStorePlan +
	// finalizeDecomposedPlan helpers (see Decompose above).
	for {
		attemptPrompt := prompt
		if prd.LastDecomposeFailure != nil {
			attemptPrompt += fmt.Sprintf("\n\nYour previous attempt failed: %s\nFix this in your next response.", prd.LastDecomposeFailure.Detail)
		}
		r, derr := m.decomposeAdmitted(prd, backend, planModel2, DecomposeRequest{Spec: attemptPrompt, Backend: backend, Effort: effort, Model: planModel2, ProjectDir: prd.ProjectDir, ScopeBlock: ScopeBlock(prd), PRDID: prd.ID, TimeoutSeconds: m.cfg.PlanningTimeoutSeconds})
		if derr != nil {
			if errors.Is(derr, errDecomposeCancelled) {
				// Same as Decompose() above: a cancelled/rejected PRD is not a
				// decompose failure — keep its terminal status, don't retry.
				return nil, derr
			}
			if isOneShotNoOutput(derr) {
				if m.noteDecomposeSessionFailure(prd, backend, planModel2, derr) {
					return nil, derr
				}
				continue
			}
			if m.retryDecomposeFailure(prd, "infra", "decompose-llm", derr.Error()) {
				continue
			}
			m.failDecomposeAttempt(prd, "decompose-llm", derr)
			return nil, fmt.Errorf("LLM plan: %w", derr)
		}
		t, s, perr := ParsePlanning(r)
		if perr != nil {
			// See Decompose()'s identical branch: a parse failure IS a
			// written verdict (non-empty content only returns when the
			// output file was written), so it fails immediately rather than
			// retrying as infra — 2026-10-19 incident class. The parse
			// reason still lands on LastDecomposeFailure.Detail.
			m.failDecomposeAttempt(prd, "decompose-parse", perr)
			return nil, perr
		}
		// Same SHARED validation + store tail as Decompose (see
		// validateAndStorePlan) — kept in one place rather than a second
		// drifted copy; retry/terminal outcomes are handled identically.
		switch outcome, oerr := m.validateAndStorePlan(prd, prdID, s); outcome {
		case decomposeAttemptRetry:
			continue
		case decomposeAttemptTerminal:
			return nil, oerr
		default: // decomposeAttemptStored — plan validated and stored.
			updated, ferr := m.finalizeDecomposedPlan(prd, prdID, backend, prompt, r, t, s)
			if ferr != nil {
				return nil, ferr
			}
			// Yield stories via callback after finalizing (s IS the
			// stored slice — IDs included, mutated in place by SetStories).
			if cb != nil {
				for i := range s {
					cb(i, len(s), s[i])
				}
			}
			return updated, nil
		}
	}
}

// Approve transitions a PRD from needs_review → approved so the loop /
// Manager.Run is allowed to start workers. BL191 Q1.
func (m *Manager) Approve(prdID, actor, note string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q is not approvable", prdID, prd.Status)
	}
	// Plan-coverage gate (block mode = the demonstrated default): a
	// stored, failing plan-coverage report blocks approval unless the
	// operator acknowledged THAT exact failing surface — an unmatched
	// requirement set or structural findings are never approved past
	// silently (see planCoverageApprovalGate and
	// AcknowledgePlanCoverage).
	if gerr := m.planCoverageApprovalGate(prdID, prd); gerr != nil {
		return nil, gerr
	}
	// Plan review gate (2026-10-17): in "block" mode a negative verdict
	// recorded by runPlanReview refuses approval until it is replaced
	// (review re-run or re-decompose with ok:true). A degraded verdict
	// (reviewer outage / unparseable / skipped) never blocks. A plan review the
	// judge held (it timed out on every backend) refuses approval in every mode,
	// since no judge checked the plan.
	if mode := normalizePlanReviewMode(m.cfg.PlanReviewMode); mode != PlanReviewModeOff {
		if v := prd.PlanReview; v != nil && !v.Ok && !v.ReviewerFailed && (mode == PlanReviewModeBlock || v.Held) {
			msg := v.Summary
			if cats := planReviewCategories(v.Findings); len(cats) > 0 {
				msg = strings.Join(cats, ", ") + ": " + msg
			}
			return nil, fmt.Errorf("plan review blocks approval of prd %q (mode %s): %s", prdID, mode, msg)
		}
	}
	now := time.Now()
	old := prd.Status
	prd.Status = PRDApproved
	// BL427 — fired after the save below.
	emitApproved := func() { m.emitPRDStatus(prd, old, lastDecision(prd, "approve"), nil) }
	prd.ApprovedBy = actor
	prd.ApprovedAt = &now
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "approve", Actor: actor, Note: note})
	// Phase 3 (v5.26.61) — when per_story_approval is on, transition
	// every pending/freshly-decomposed story to "awaiting_approval"
	// so the runner skips them until the operator approves each via
	// POST .../approve_story. Stories already in a non-pending state
	// (in_progress / completed / blocked / failed) are left alone.
	if m.cfg.PerStoryApproval {
		for si := range prd.Story {
			if prd.Story[si].Status == "" || prd.Story[si].Status == StoryPending {
				prd.Story[si].Status = StoryAwaitingApproval
				prd.Story[si].UpdatedAt = now
			}
		}
	}
	// BL406 Phase 5 — same shape, at task granularity: when
	// GuidedModeSource resolves to "operator" (the original BL384
	// intent, now actually built — or "council" falling back since
	// BL405 Phase 8 doesn't exist yet), transition every pending task
	// to awaiting_approval so the runner skips them until the operator
	// approves each via ApproveTask. Independent of PerStoryApproval —
	// a PRD can use either, both, or neither gate.
	var gated []pendingTaskEvent
	if m.resolveGuidedModeSource(prd) == GuidedModeOperator {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.Status == "" || t.Status == TaskPending {
					gated = append(gated, pendingTaskEvent{t: t, oldStatus: t.Status, oldSession: t.SessionID})
					t.Status = TaskAwaitingApproval
					t.UpdatedAt = now
				}
			}
		}
	}
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	emitApproved()
	m.emitPendingTaskEvents(prd, gated, nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// Reject marks a PRD as terminally rejected. The decomposition stays
// stored for inspection but the loop will never run it.
func (m *Manager) Reject(prdID, actor, reason string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status == PRDCompleted {
		return nil, fmt.Errorf("prd %q already completed; nothing to reject", prdID)
	}
	now := time.Now()
	old := prd.Status
	prd.Status = PRDRejected
	prd.RejectionReason = reason
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "reject", Actor: actor, Note: reason})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "reject"), nil)
	// A rejected PRD never ran (or, if it was running, the operator
	// has still declared it dead) — clean up any decompose-time
	// worktree now rather than leave it orphaned until someone
	// eventually archives this PRD. Non-fatal and best-effort; see
	// cleanupOrphanedWorktree's own doc comment for why it's safe.
	m.cleanupOrphanedWorktree(prd)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RequestRevision asks the operator (or a follow-up Decompose call)
// for a new decomposition. The PRD lands back in revisions_asked;
// calling Decompose again resets it to needs_review.
func (m *Manager) RequestRevision(prdID, actor, note string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDApproved && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q does not accept revisions", prdID, prd.Status)
	}
	now := time.Now()
	old := prd.Status
	prd.Status = PRDRevisionsAsked
	prd.RevisionsRequested++
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "request_revision", Actor: actor, Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "request_revision"), nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// ResetToDraft resets a PRD back to draft so the operator can reconfigure
// the backend and re-decompose. Stories and tasks are cleared so the next
// Decompose starts fresh. Blocked only while a run is in flight (running /
// planning) or when the PRD has been archived.
func (m *Manager) ResetToDraft(id, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	switch prd.Status {
	case PRDRunning, PRDActive, PRDPlanning:
		return nil, fmt.Errorf("prd %q is %s; stop it before resetting to draft", id, prd.Status)
	case PRDArchived:
		return nil, fmt.Errorf("prd %q is archived and cannot be reset", id)
	}
	now := time.Now()
	old := prd.Status
	prd.Status = PRDDraft
	prd.Story = nil
	prd.ApprovedBy = ""
	prd.ApprovedAt = nil
	prd.RejectionReason = ""
	// A reset starts a fresh decompose streak: the no-output counter and
	// its terminal hint must not carry into the next attempt.
	prd.DecomposeSessionFailures = 0
	if prd.LastDecomposeFailure != nil && prd.LastDecomposeFailure.Source == "decompose-session" {
		prd.LastDecomposeFailure = nil
	}
	prd.UpdatedAt = now
	prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
		At: now, Kind: "reset_to_draft", Actor: actor,
		Note: fmt.Sprintf("reset from %s by %s", prd.Status, actor),
	}))
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "reset_to_draft"), nil)
	updated, _ := m.store.GetPRD(id)
	return updated, nil
}

// Archive moves a terminal PRD (completed/rejected/cancelled) to
// PRDArchived status so it disappears from the active list without
// being permanently deleted.
func (m *Manager) Archive(id string) (*PRD, error) {
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	terminal := map[PRDStatus]bool{
		PRDCompleted: true, PRDRejected: true, PRDCancelled: true, PRDArchived: true,
	}
	if !terminal[prd.Status] {
		return nil, fmt.Errorf("prd %q status %q is not terminal; cancel or complete it first", id, prd.Status)
	}
	if prd.Status == PRDArchived {
		return prd, nil // idempotent
	}
	now := time.Now()
	old := prd.Status
	prd.Status = PRDArchived
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "archive", Actor: "operator"})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "archive"), nil)
	// Universal orphan-cleanup backstop: by the time a PRD reaches
	// Archive, the operator has said "done with this forever" —
	// whatever Decision 5's "keep for inspection" grace period on a
	// cancelled/failed/blocked PRD was for has ended. A no-op for a
	// completed PRD (Phase 2 already removed its worktree on success)
	// or one that never had a worktree.
	m.cleanupOrphanedWorktree(prd)
	updated, _ := m.store.GetPRD(id)
	return updated, nil
}

// DeletePRD (v5.19.0) hard-removes a PRD from the store, including
// any descendants spawned via SpawnPRD. The operator-facing surface
// for "I'm done with this PRD, remove it from the list". Distinct
// from Cancel which only flips Status to cancelled. Refuses while
// the PRD is running OR any descendant PRD is running — operator
// must Cancel the running ones first (v5.26.8 cascade-aware guard;
// previously only the top-level was checked, leaving spawned children
// dangling mid-execution after a hard-delete).
func (m *Manager) DeletePRD(id string) error {
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	if prd.Status == PRDRunning {
		return fmt.Errorf("prd %q is running; cancel before deleting", id)
	}
	// v5.26.8 — walk descendants and refuse if any are running. The
	// store-side cascade is happy to delete running children, but the
	// executor goroutine would keep writing to a now-deleted PRD;
	// catch it at the operator boundary instead.
	all := m.store.ListPRDs()
	for _, running := range descendantsOf(id, all) {
		if running.Status == PRDRunning {
			return fmt.Errorf("descendant prd %q is running; cancel it before deleting parent %q", running.ID, id)
		}
	}
	return m.store.DeletePRD(id)
}

// descendantsOf walks the SpawnPRD tree rooted at parentID and
// returns every descendant in the supplied snapshot. Helper for
// DeletePRD's cascade-aware check; does NOT include the root PRD.
func descendantsOf(parentID string, all []*PRD) []*PRD {
	var out []*PRD
	frontier := map[string]bool{parentID: true}
	for changed := true; changed; {
		changed = false
		for _, p := range all {
			if p.ParentPRDID != "" && frontier[p.ParentPRDID] && !frontier[p.ID] {
				frontier[p.ID] = true
				out = append(out, p)
				changed = true
			}
		}
	}
	return out
}

// SetPRDProfiles (v5.26.19) attaches F10 project + cluster profiles
// to a PRD. Profile names are validated against the wired
// ProfileResolver (when present); empty strings clear the field.
// Refuses while the PRD is running — operator must Cancel first.
// Records a Decision audit row.
func (m *Manager) SetPRDProfiles(id, projectProfile, clusterProfile string) error {
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	if prd.Status == PRDRunning {
		return fmt.Errorf("prd %q is running; cancel before attaching profiles", id)
	}
	m.mu.Lock()
	resolver := m.profileResolver
	m.mu.Unlock()
	if resolver != nil {
		if projectProfile != "" && !resolver.HasProjectProfile(projectProfile) {
			return fmt.Errorf("project profile %q not found", projectProfile)
		}
		if clusterProfile != "" && !resolver.HasClusterProfile(clusterProfile) {
			return fmt.Errorf("cluster profile %q not found", clusterProfile)
		}
	}
	prd.ProjectProfile = projectProfile
	prd.ClusterProfile = clusterProfile
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At:    time.Now(),
		Kind:  "set_profiles",
		Actor: "operator",
		Note:  fmt.Sprintf("project=%q cluster=%q", projectProfile, clusterProfile),
	})
	return m.store.SavePRD(prd)
}

// EditPRDFields (v5.19.0) edits PRD-level title + spec on a non-
// running PRD. Records a Decision so the timeline shows the edit.
// Use EditTaskSpec for per-task spec changes.
func (m *Manager) EditPRDFields(id, title, spec, actor string) (*PRD, error) {
	if spec != "" {
		if err := m.checkInjectionGuard("prd spec edit", spec); err != nil {
			return nil, err
		}
	}
	prd, err := m.store.UpdatePRDFields(id, title, spec)
	if err != nil {
		return nil, err
	}
	if actor == "" {
		actor = "operator"
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At:    time.Now(),
		Kind:  "edit",
		Actor: actor,
		Note:  fmt.Sprintf("title=%q spec_chars=%d", title, len(spec)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	return prd, nil
}

// EditTaskSpec lets the operator rewrite an LLM-decomposed task's spec
// before approving the PRD (needs_review / revisions_asked), after
// cancelling it (cancelled), or — BL421 — in place while the PRD is
// blocked, so a stuck task (e.g. one the pre-spawn scope lint rejected)
// can be corrected and then retried via ResetTask without
// cancel -> reset_to_draft -> re-decompose regenerating the whole tree.
// While blocked, only a failed or blocked task may be edited: the point is
// to patch the one stuck task, not to replan its pending siblings. A
// completed task's spec is immutable in every PRD status, since it is the
// record of work already done. A failed PRD accepts an edit only for a task
// that failed the pre-spawn scope lint (isScopeLintFailure), so that task can
// be corrected and retried without widening PRD dirs or recreating the PRD.
// Records a Decision so the timeline shows the edit.
func (m *Manager) EditTaskSpec(prdID, taskID, newSpec, actor string) (*PRD, error) {
	if err := m.checkInjectionGuard("task spec edit", newSpec); err != nil {
		return nil, err
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled && prd.Status != PRDBlocked && prd.Status != PRDFailed {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled / blocked / failed (scope-lint tasks only) accept task edits", prdID, prd.Status)
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				ts := prd.Story[si].Tasks[ti].Status
				if ts == TaskCompleted {
					return nil, fmt.Errorf("task %q is completed; completed task specs are immutable", taskID)
				}
				if prd.Status == PRDBlocked && ts != TaskFailed && ts != TaskBlocked {
					if ts == "" {
						ts = TaskPending
					}
					return nil, fmt.Errorf("task %q status %q cannot be edited while prd %q is blocked; only failed or blocked tasks can be patched in place", taskID, ts, prdID)
				}
				if prd.Status == PRDFailed && !isScopeLintFailure(&prd.Story[si].Tasks[ti]) {
					return nil, fmt.Errorf("prd %q status %q is locked for task %q; a failed prd only accepts edits to a task that failed the pre-spawn scope lint", prdID, prd.Status, taskID)
				}
				prd.Story[si].Tasks[ti].Spec = newSpec
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "edit_task", Actor: actor,
		Note: fmt.Sprintf("task=%s spec_chars=%d", taskID, len(newSpec)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RepairDependsOn (v8.36.6) — see Store.RepairDependsOn's doc comment.
func (m *Manager) RepairDependsOn(prdID, actor string) (*PRD, error) {
	prd, err := m.store.RepairDependsOn(prdID)
	if err != nil {
		return nil, err
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "repair_depends_on", Actor: actor,
		Note: "resolved DependsOn titles to real story/task IDs",
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// ResetTask (v8.23.0) resets a failed or blocked task back to pending so
// the autonomous loop will retry it. Only allowed while the PRD is running.
// Clears status, error, session_id, verification, and retry_count.
// ResetTask resets a failed/blocked task to pending for retry.
// BL382: force=true also requeues completed or cancelled tasks.
// recoverPendingTask reopens a terminal PRD whose target task is already
// pending (BL451). The task is left untouched; the containing story is
// reopened if terminal and the PRD returns to PRDRunning.
func (m *Manager) recoverPendingTask(prd *PRD, si int, taskID, actor string) (*PRD, error) {
	s := &prd.Story[si]
	if s.Status == StoryCompleted || s.Status == StoryFailed || s.Status == StoryCancelled {
		s.Status = StoryPending
	}
	// Same start-over guarantee as ResetTask: a manual reset of an
	// already-pending task must drop the per-task git anchors too.
	for ti := range s.Tasks {
		if s.Tasks[ti].ID == taskID {
			s.Tasks[ti].BaseSHA = ""
			s.Tasks[ti].AttemptCommitSHAs = nil
			break
		}
	}
	old := prd.Status
	now := time.Now()
	prd.Status = PRDRunning
	prd.UpdatedAt = now
	kind := "recover_pending_task"
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: kind, Actor: actor,
		Note: fmt.Sprintf("task=%s already pending; prd reopened from %s (force=true recovery)", taskID, old)})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, kind), nil)
	updated, _ := m.store.GetPRD(prd.ID)
	return updated, nil
}

func (m *Manager) ResetTask(prdID, taskID, actor string, force bool) (*PRD, error) {
	return m.ResetTaskWithOptions(prdID, taskID, actor, force, false)
}

// ResetTaskWithOptions is ResetTask with the clean-restart option. cleanRestart
// true makes the next attempt skip the restore of preserved attempt work
// (Task.CleanRestart); false keeps the restore on, as ResetTask always did.
func (m *Manager) ResetTaskWithOptions(prdID, taskID, actor string, force, cleanRestart bool) (*PRD, error) {
	return m.resetTask(prdID, taskID, actor, force, cleanRestart, nil)
}

// resetTask is ResetTaskWithOptions with an optional operator review. A nil
// review is a plain reset. A non-nil review (ResolveTaskReview's retry) also
// accepts a task held for review and applies the operator's changes in the
// same save as the requeue, so the changes and the requeue land together.
func (m *Manager) resetTask(prdID, taskID, actor string, force, cleanRestart bool, review *reviewRequeue) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	// PRDBlocked included: a guardrail block or (default) story-failure halt
	// both leave the PRD blocked with no other way to retry the offending
	// task — without this the operator has no path back except the
	// destructive reset_to_draft (which wipes all story/task state).
	// PRDCancelled included: operator-requested — cancelling a run to fix a
	// task's spec (e.g. adding a missing reference) shouldn't force a full
	// reset_to_draft + fresh decompose just to retry that one task.
	// PRDCompleted included (v8.36.6): a PRD can reach "completed" with a
	// cancelled story inside it (e.g. one story was cancelled-and-never-
	// restarted while a sibling that depends_on it still ran and completed —
	// see the depends_on title/ID resolution fix). The task-level check below
	// still only allows this via force=true on a completed/cancelled task, so
	// a non-force reset can never reach here (a truly-complete PRD has no
	// failed/blocked tasks to reset in the first place).
	if prd.Status != PRDRunning && prd.Status != PRDFailed && prd.Status != PRDBlocked && prd.Status != PRDCancelled && prd.Status != PRDCompleted {
		return nil, fmt.Errorf("prd %q status %q is not recoverable; reset_task requires PRDRunning, PRDFailed, PRDBlocked, PRDCancelled, or PRDCompleted", prdID, prd.Status)
	}
	// BL451 recovery: a terminal PRD holding an already-pending task (a
	// force reset flipped the task, then a stale rollup overwrote the PRD
	// status) has no other way back — approve/run refuse a completed PRD.
	// With force=true, leave the task pending and just reopen the PRD.
	if prd.Status != PRDRunning {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.ID != taskID || (t.Status != "" && t.Status != TaskPending) {
					continue
				}
				if !force {
					return nil, fmt.Errorf("task %q is already pending but prd %q is %q; use force=true to reopen the PRD (recover from a stale rollup)", taskID, prdID, prd.Status)
				}
				return m.recoverPendingTask(prd, si, taskID, actor)
			}
		}
	}
	found := false
	foundSI := -1
	emitReset := func(*Decision) {}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.ID == taskID {
				isRetryable := t.Status == TaskFailed || t.Status == TaskBlocked || (review != nil && t.Status == TaskReviewNeeded)
				isRequeue := force && (t.Status == TaskCompleted || t.Status == TaskCancelled)
				if !isRetryable && !isRequeue {
					if force {
						return nil, fmt.Errorf("task %q status %q cannot be requeued even with force=true; only completed or cancelled tasks qualify", taskID, t.Status)
					}
					return nil, fmt.Errorf("task %q status %q cannot be reset; only failed or blocked tasks can be retried (use force=true to requeue completed/cancelled tasks)", taskID, t.Status)
				}
				// v8.36.1 — capture why the previous attempt didn't land
				// before clearing it, so the retry's worker gets told what
				// happened instead of the bare original spec (see
				// PriorAttemptHint's doc comment on Task).
				hintParts := make([]string, 0, 2)
				if t.Error != "" {
					hintParts = append(hintParts, "Previous attempt error: "+t.Error)
				}
				if t.Verification != nil && !t.Verification.OK {
					vSummary := fmt.Sprintf("Previous verification failed (%s): %s", t.Verification.Severity, t.Verification.Summary)
					if len(t.Verification.Issues) > 0 {
						vSummary += " Specific issues: " + strings.Join(t.Verification.Issues, "; ")
					}
					hintParts = append(hintParts, vSummary)
				}
				if len(hintParts) > 0 {
					// v8.36.2 — found live retrying this exact hint on PRD
					// a2833a5e: the original wording ("verify/finish it
					// rather than starting over") was ambiguous enough that
					// the worker checked the files looked fine, wrote only a
					// CHECKPOINT.md note, and produced no diff touching the
					// actual required files — which the verifier (correctly
					// scoped to just this run's diff) then rejected as no
					// output at all. Now explicit: touching/recommitting
					// the real deliverables themselves is what's required,
					// not just a checkpoint note about them.
					t.PriorAttemptHint = "This is a retry of a previous attempt on this task. " +
						strings.Join(hintParts, " ") +
						" Check the project directory's current state (git log, existing files) before redoing work — the previous attempt may have already produced correct output that only failed for an unrelated reason (e.g. the verifier itself erroring). If so, fix the specific issues above (or reconfirm correctness) by editing the actual required deliverable files themselves — do not run git add or git commit; your work is committed automatically when you finish. Verification only sees this run's own diff, so a checkpoint note or other side file alone will not satisfy it, even if the underlying content is already right."
				}
				resetEv := pendingTaskEvent{t: t, oldStatus: t.Status, oldSession: t.SessionID}
				t.Status = ""
				t.Error = ""
				t.SessionID = ""
				// A human reset means starting over: drop the per-task git
				// anchors so the fresh attempt re-captures them at spawn.
				// Automatic verifier retries never go through here — BaseSHA
				// is written only on the first attempt (attempt 0) of the
				// executor's spawn path, so in-loop retries keep the first
				// attempt's anchor.
				t.BaseSHA = ""
				t.AttemptCommitSHAs = nil
				// BL427 — fired after the SavePRD below succeeds.
				emitReset = func(dec *Decision) { m.emitPendingTaskEvents(prd, []pendingTaskEvent{resetEv}, dec) }
				t.Verification = nil
				t.RetryCount = 0
				// A human reset starts a new count of failed attempts for the
				// failure escalation policy, so the ladder restarts at step 1.
				t.EscalationFailures = 0
				// v9.2.1 — a manual (operator) reset is the strongest "this is a
				// retry" signal: to reach this block the task already reached a
				// terminal-ish status (failed/blocked, or completed/cancelled
				// requeued via force), i.e. at least one prior attempt happened.
				// RetryCount is zeroed for the next attempt's auto-retry budget,
				// but PriorAttemptCount is cumulative and is what the verifier
				// consults to know a retry's empty diff is legitimate (the prior
				// attempt may have already landed correct work) rather than a
				// genuine no-output. Auto in-loop retries don't go through
				// ResetTask and leave this at 0 (see Task's field doc).
				t.PriorAttemptCount++
				// Operator's choice for this reset only: a plain reset leaves
				// the flag false, so an unconsumed clean restart never carries
				// over into a later reset.
				t.CleanRestart = cleanRestart
				// An operator review's changes go on last, so the hint they
				// set replaces the generic one built above.
				if review != nil {
					review.apply(t, time.Now())
				}
				t.StartedAt = nil
				t.CompletedAt = nil
				t.UpdatedAt = time.Now()
				found = true
				foundSI = si
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	// B99: if the containing story was completed/failed/cancelled, reopen it
	// to pending so the progress bar and status dot reflect that work is
	// still in flight. StoryCancelled added v8.36.6 — same recovery path as
	// PRDCancelled/PRDCompleted above; a cancelled story force-requeued via
	// one of its tasks needs to actually resume, not stay cancelled.
	if foundSI >= 0 {
		s := &prd.Story[foundSI]
		if s.Status == StoryCompleted || s.Status == StoryFailed || s.Status == StoryCancelled {
			s.Status = StoryPending
		}
	}
	now := time.Now()
	prd.UpdatedAt = now
	// B97: when called on a PRDFailed PRD, restore it to PRDRunning so the
	// executor picks it back up without the operator having to reset_to_draft
	// (which would discard all completed task work). Same for PRDBlocked
	// (guardrail block or story-failure halt) and PRDCancelled (operator
	// cancelled to fix a task's spec) — but the executor's Run loop has
	// already exited by the time a PRD reaches any of these, so restoring
	// the status alone isn't enough; the operator (or PWA) must call Run
	// again (boot-resume does this automatically on the next restart, but
	// that's not required for it to take effect).
	resetOld := prd.Status
	if prd.Status == PRDFailed || prd.Status == PRDBlocked || prd.Status == PRDCancelled || prd.Status == PRDCompleted {
		prd.Status = PRDRunning
	}
	kind := "reset_task"
	note := fmt.Sprintf("task=%s reset to pending for retry", taskID)
	decActor := actor
	if force {
		kind = "requeue_task"
		note = fmt.Sprintf("task=%s requeued (force=true)", taskID)
	}
	if review != nil {
		kind = reviewDecisionKind
		note = review.decisionNote(taskID, actor)
		decActor = reviewDecisionActor
		// The block reason named the held task, which is no longer held.
		prd.BlockedReason = ""
	}
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: kind, Actor: decActor, Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	if prd.Status != resetOld {
		m.emitPRDStatus(prd, resetOld, lastDecision(prd, kind), nil)
	}
	emitReset(lastDecision(prd, kind))
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RecordUnmodifiedRetry (v9.2.1) records the audit trail for a retry that
// passed verification via the unmodified-retry escape hatch — the retry
// itself produced no diff, but the task's own VerificationScript re-run
// against the already-committed deliverable passed, so the verifier
// completed the task rather than failing it. Like recordAutoRetryDecision it
// appends a single Decision to the PRD (the autonomous package's own
// already-universally-exposed audit trail, surfaced via REST/MCP/PWA)
// rather than inventing a new mechanism. Called from the cmd/datawatch
// verifier only on the exact rare path it exists for, and only AFTER the
// task has already been marked complete by the caller, so a lookup/save
// failure there is a lost audit note, never a lost outcome — best-effort
// callers log the returned error and move on. A prd that doesn't exist is
// a hard error so the caller doesn't silently claim a record it never made.
func (m *Manager) RecordUnmodifiedRetry(prdID, taskID, note string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	now := time.Now()
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At:    now,
		Kind:  "unmodified_retry_completed",
		Actor: "autonomous",
		Note:  fmt.Sprintf("task=%s unmodified retry completed verification script passed (%s)", taskID, note),
	})
	return m.store.SavePRD(prd)
}

// CancelStory (BL382) — operator cancels a single story without cancelling the whole PRD.
// The story is marked cancelled; any in-progress tasks for that story have their sessions killed.
// Returns 409 if the story is already in a terminal state.
func (m *Manager) CancelStory(prdID, storyID, actor, reason string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	terminalStory := map[StoryStatus]bool{
		StoryCompleted: true, StoryFailed: true, StoryCancelled: true,
	}
	now := time.Now()
	found := false
	var cancelled []pendingTaskEvent
	// BL427 — fired after the SavePRD below succeeds.
	emitCancelled := func() { m.emitPendingTaskEvents(prd, cancelled, lastDecision(prd, "cancel_story")) }
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			if terminalStory[prd.Story[si].Status] {
				return nil, fmt.Errorf("409: story %q is already in terminal state %q", storyID, prd.Story[si].Status)
			}
			// Kill any running task sessions for this story.
			m.mu.Lock()
			fn := m.sessionKillerFn
			m.mu.Unlock()
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.SessionID != "" && fn != nil {
					why := fmt.Sprintf("story %s cancelled: task %s session ends with the story", storyID, t.ID)
					if err := fn(t.SessionID, "autonomous.cancel.story", why); err != nil {
						log.Printf("[autonomous] cancel_story: kill session %s: %v", t.SessionID, err)
					}
				}
				if t.Status != TaskCompleted && t.Status != TaskCancelled {
					cancelled = append(cancelled, pendingTaskEvent{t: t, oldStatus: t.Status, oldSession: t.SessionID})
					t.Status = TaskCancelled
					t.UpdatedAt = now
				}
			}
			prd.Story[si].Status = StoryCancelled
			prd.Story[si].UpdatedAt = now
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	note := fmt.Sprintf("story=%s cancelled", storyID)
	if reason != "" {
		note += ": " + reason
	}
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "cancel_story", Actor: actor, Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	for _, ev := range cancelled {
		m.releaseCapacity(ev.t.ID)
	}
	emitCancelled()
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// CancelTask (BL382) — operator cancels a single task without cancelling its story or PRD.
// Returns 409 if the task is already in a terminal state.
func (m *Manager) CancelTask(prdID, taskID, actor, reason string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	terminalTask := map[TaskStatus]bool{
		TaskCompleted: true, TaskCancelled: true,
	}
	now := time.Now()
	found := false
	emitCancelled := func() {}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.ID == taskID {
				if terminalTask[t.Status] {
					return nil, fmt.Errorf("409: task %q is already in terminal state %q", taskID, t.Status)
				}
				// Kill the session if it's running.
				if t.SessionID != "" {
					m.mu.Lock()
					fn := m.sessionKillerFn
					m.mu.Unlock()
					if fn != nil {
						why := fmt.Sprintf("task %s cancelled: its session ends with the task", taskID)
						if err := fn(t.SessionID, "autonomous.cancel.task", why); err != nil {
							log.Printf("[autonomous] cancel_task: kill session %s: %v", t.SessionID, err)
						}
					}
				}
				oldStatus := t.Status
				m.store.WithLock(func() {
					t.Status = TaskCancelled
					t.UpdatedAt = now
				})
				// BL427 — fired after the SavePRD below succeeds.
				emitCancelled = func() { m.emitTaskStatus(prd, t, oldStatus, lastDecision(prd, "cancel_task"), nil) }
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	note := fmt.Sprintf("task=%s cancelled", taskID)
	if reason != "" {
		note += ": " + reason
	}
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "cancel_task", Actor: actor, Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.releaseCapacity(taskID)
	emitCancelled()
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// EditStory (v5.26.32) lets the operator rewrite an LLM-decomposed
// story's title + description before approving the PRD. Mirrors
// EditTaskSpec — only allowed in needs_review or revisions_asked,
// records a Decision so the timeline shows the edit. Operator-asked:
// "i don't see a story review or approval or story edit option."
// AddStory (operator-requested: structural edits without re-decomposing
// and handing the whole structure back to the LLM) appends a new, empty
// story to a PRD. Same review-phase gate as EditStory — the story/task
// structure is only mutable before the PRD is approved and workers start.
func (m *Manager) AddStory(prdID, title, description, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept structural edits", prdID, prd.Status)
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("title is required")
	}
	story, err := m.store.AddStory(prdID, title, description)
	if err != nil {
		return nil, err
	}
	prd, _ = m.store.GetPRD(prdID)
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "add_story", Actor: actor,
		Note: fmt.Sprintf("story=%s title=%q", story.ID, title),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RemoveStory deletes a story (and its tasks) from a PRD.
func (m *Manager) RemoveStory(prdID, storyID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept structural edits", prdID, prd.Status)
	}
	if err := m.store.RemoveStory(prdID, storyID); err != nil {
		return nil, err
	}
	prd, _ = m.store.GetPRD(prdID)
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "remove_story", Actor: actor,
		Note: fmt.Sprintf("story=%s", storyID),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// AddTask appends a new task to an existing story.
func (m *Manager) AddTask(prdID, storyID, title, spec, actor string, dependsOn []string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept structural edits", prdID, prd.Status)
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("title is required")
	}
	task, err := m.store.AddTask(prdID, storyID, title, spec, dependsOn)
	if err != nil {
		return nil, err
	}
	prd, _ = m.store.GetPRD(prdID)
	note := fmt.Sprintf("story=%s task=%s title=%q", storyID, task.ID, title)
	if len(dependsOn) > 0 {
		note += fmt.Sprintf(" depends_on=%v", dependsOn)
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "add_task", Actor: actor,
		Note: note,
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RemoveTask deletes one task from a story (structural edit — distinct
// from CancelTask, which stops a task that's already running/queued
// without removing it from the PRD).
func (m *Manager) RemoveTask(prdID, storyID, taskID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept structural edits", prdID, prd.Status)
	}
	if err := m.store.RemoveTask(prdID, storyID, taskID); err != nil {
		return nil, err
	}
	prd, _ = m.store.GetPRD(prdID)
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "remove_task", Actor: actor,
		Note: fmt.Sprintf("story=%s task=%s", storyID, taskID),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

func (m *Manager) EditStory(prdID, storyID, newTitle, newDescription, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept story edits", prdID, prd.Status)
	}
	found := false
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			if newTitle != "" {
				prd.Story[si].Title = newTitle
			}
			// Description is optional — empty newDescription clears it
			// only when the operator explicitly passes a sentinel; we
			// treat empty-string as "leave unchanged" so a title-only
			// edit doesn't accidentally drop the description.
			if newDescription != "" {
				prd.Story[si].Description = newDescription
			}
			prd.Story[si].UpdatedAt = time.Now()
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "edit_story", Actor: actor,
		Note: fmt.Sprintf("story=%s title_chars=%d desc_chars=%d", storyID, len(newTitle), len(newDescription)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetStoryProfile (Phase 3, v5.26.60) — operator overrides a single
// story's execution profile. Empty string clears the override
// (story falls back to PRD.ProjectProfile). Validated against the
// profile resolver if one is wired. Allowed in needs_review /
// revisions_asked only — same lock-after-approve gate as
// EditTaskSpec / EditStory.
func (m *Manager) SetStoryProfile(prdID, storyID, profile, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept story profile changes", prdID, prd.Status)
	}
	if profile != "" && m.profileResolver != nil {
		if !m.profileResolver.HasProjectProfile(profile) {
			return nil, fmt.Errorf("invalid execution profile %q: project profile not found", profile)
		}
	}
	found := false
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			prd.Story[si].ExecutionProfile = profile
			prd.Story[si].UpdatedAt = time.Now()
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_story_profile", Actor: actor,
		Note: fmt.Sprintf("story=%s profile=%q", storyID, profile),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// ApproveStory (Phase 3, v5.26.60) — operator approves an individual
// story for execution. Only effective when the per_story_approval
// config flag is on (otherwise PRD-level approval implicitly approves
// every story). Sets Story.Approved=true, ApprovedBy, ApprovedAt;
// records a kind=approve_story decision. Allowed when the PRD itself
// is approved or running (per-story gate runs *after* PRD-level gate).
func (m *Manager) ApproveStory(prdID, storyID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDApproved && prd.Status != PRDActive && prd.Status != PRDRunning {
		return nil, fmt.Errorf("prd %q status %q does not accept per-story approval; PRD must be approved or running first", prdID, prd.Status)
	}
	now := time.Now()
	found := false
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			prd.Story[si].Approved = true
			prd.Story[si].ApprovedBy = actor
			prd.Story[si].ApprovedAt = &now
			prd.Story[si].RejectedReason = ""
			// Transition awaiting_approval → pending so the runner
			// picks it up. Other states left alone.
			if prd.Story[si].Status == StoryAwaitingApproval {
				prd.Story[si].Status = StoryPending
			}
			prd.Story[si].UpdatedAt = now
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: "approve_story", Actor: actor,
		Note: fmt.Sprintf("story=%s", storyID),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// ApproveTask (BL406 Phase 5 — B111's real fix) — operator approves an
// individual task for execution. Only meaningful when
// GuidedModeSource resolves to "operator"; same shape as ApproveStory.
// Sets Task.Approved=true, ApprovedBy, ApprovedAt; records a
// kind=approve_task decision. Allowed when the PRD itself is approved
// or running (same precondition as ApproveStory — the task-level gate
// runs after the PRD-level gate, never before it).
func (m *Manager) ApproveTask(prdID, taskID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDApproved && prd.Status != PRDActive && prd.Status != PRDRunning && prd.Status != PRDBlocked {
		return nil, fmt.Errorf("prd %q status %q does not accept per-task approval; PRD must be approved, running, or blocked first", prdID, prd.Status)
	}
	now := time.Now()
	found := false
	emitPending := func() {}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.ID == taskID {
				t.Approved = true
				t.ApprovedBy = actor
				t.ApprovedAt = &now
				t.RejectedReason = ""
				// Transition awaiting_approval → pending so the runner
				// picks it up. Other states left alone.
				if t.Status == TaskAwaitingApproval {
					t.Status = TaskPending
					// BL427 — fired after the SavePRD below succeeds.
					emitPending = func() { m.emitTaskStatus(prd, t, TaskAwaitingApproval, lastDecision(prd, "approve_task"), nil) }
				}
				// Operator-approved a blocked task: release it back to
				// pending for re-run (matching checkForeignFileDrift's
				// blocked state). Clear the stale error from the blocked
				// attempt. The PRD itself is left in PRDBlocked — the
				// existing run path decides the PRD status transition; we
				// never auto-run anything from here.
				if t.Status == TaskBlocked {
					t.Status = TaskPending
					t.Error = ""
					emitPending = func() { m.emitTaskStatus(prd, t, TaskBlocked, lastDecision(prd, "approve_task"), nil) }
				}
				t.UpdatedAt = now
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: "approve_task", Actor: actor,
		Note: fmt.Sprintf("task=%s", taskID),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	emitPending()
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RejectStory (Phase 3, v5.26.60) — operator blocks a single story.
// Sets Status=blocked + records the reason. The runner skips blocked
// stories; the operator can later approve to resume.
func (m *Manager) RejectStory(prdID, storyID, actor, reason string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if reason == "" {
		return nil, fmt.Errorf("reason is required when rejecting a story")
	}
	now := time.Now()
	found := false
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			prd.Story[si].Status = StoryBlocked
			prd.Story[si].Approved = false
			prd.Story[si].RejectedReason = reason
			prd.Story[si].UpdatedAt = now
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: "reject_story", Actor: actor,
		Note: fmt.Sprintf("story=%s reason=%q", storyID, reason),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// FindTaskBySessionID (Phase 4 follow-up, v5.26.67) — given a
// datawatch session id (the SpawnResult.SessionID written into
// Task.SessionID by the executor), returns the (prd_id, task_id)
// pair so the post-session diff callback can route to
// RecordTaskFilesTouched. Returns ("","") when the session isn't
// linked to any autonomous task — the post-session hook treats
// that as a no-op. The argument may be the full or the short session id.
func (m *Manager) FindTaskBySessionID(sessionID string) (prdID, taskID string) {
	if sessionID == "" {
		return "", ""
	}
	for _, prd := range m.store.ListPRDs() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				if sameSessionID(prd.Story[si].Tasks[ti].SessionID, sessionID) {
					return prd.ID, prd.Story[si].Tasks[ti].ID
				}
			}
		}
	}
	return "", ""
}

// SetStoryFiles (Phase 4, v5.26.64) — operator overrides a story's
// FilesPlanned list. Allowed in needs_review / revisions_asked only
// (lock-after-approve); appends a `set_story_files` audit decision.
// Empty list clears the field. Capped at 50 paths to keep PRD
// records bounded (per the design doc's 5KB-per-story budget).
func (m *Manager) SetStoryFiles(prdID, storyID string, files []string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept story file edits", prdID, prd.Status)
	}
	if len(files) > 50 {
		files = files[:50]
	}
	found := false
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			prd.Story[si].FilesPlanned = files
			prd.Story[si].UpdatedAt = time.Now()
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_story_files", Actor: actor,
		Note: fmt.Sprintf("story=%s n=%d", storyID, len(files)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetTaskFiles (Phase 4, v5.26.64) — operator overrides a task's
// FilesPlanned. Same gates as SetStoryFiles.
func (m *Manager) SetTaskFiles(prdID, taskID string, files []string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept task file edits", prdID, prd.Status)
	}
	if len(files) > 50 {
		files = files[:50]
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				prd.Story[si].Tasks[ti].FilesPlanned = files
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_task_files", Actor: actor,
		Note: fmt.Sprintf("task=%s n=%d", taskID, len(files)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RecordTaskFilesTouched (Phase 4, v5.26.64) — daemon-internal hook
// fired by the post-session diff callback to record what the worker
// actually changed. Capped at 50 paths. No lock-after-approve gate
// — this fires after the worker session ends.
func (m *Manager) RecordTaskFilesTouched(prdID, taskID string, files []string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	if len(files) > 50 {
		files = files[:50]
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				prd.Story[si].Tasks[ti].FilesTouched = files
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// RecordTaskAttemptCommit (v5.26.64 sibling of RecordTaskFilesTouched) —
// daemon-internal hook fired by the autonomous verifier once the worker
// session's deterministic commit sha is known. Appends it to the task's
// ordered, de-duplicated AttemptCommitSHAs record (Task.RecordAttemptCommit
// already ignores empty values and duplicates). No lock-after-approve gate
// — this fires after the worker session ends.
func (m *Manager) RecordTaskAttemptCommit(prdID, taskID, sha string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				prd.Story[si].Tasks[ti].RecordAttemptCommit(sha)
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// RecordTaskNudge records one idle-worker nudge on the task: bumps NudgeCount
// and appends a NudgeEvent tagged with the session it was typed into. Like
// RecordTaskAttemptCommit it fires from the daemon while the worker runs, so
// there is no lock-after-approve gate.
func (m *Manager) RecordTaskNudge(prdID, taskID, sessionID string, at time.Time) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.ID == taskID {
				t.NudgeCount++
				t.NudgeEvents = append(t.NudgeEvents, NudgeEvent{At: at, Count: t.NudgeCount, SessionID: sessionID, Outcome: NudgeOutcomeSent})
				t.UpdatedAt = at
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = at
	return m.store.SavePRD(prd)
}

// TaskNudgeCount returns how many idle-worker nudges the task has recorded
// across all its sessions, or 0 when the task is unknown.
func (m *Manager) TaskNudgeCount(prdID, taskID string) int {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return 0
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if t := &prd.Story[si].Tasks[ti]; t.ID == taskID {
				return t.NudgeCount
			}
		}
	}
	return 0
}

// NudgeEventForSession returns the latest nudge the task recorded for the given
// session, with its outcome, and false when the task is unknown or the session
// has no nudge. Callers read it at the moment they judge the worker.
func (m *Manager) NudgeEventForSession(prdID, taskID, sessionID string) (NudgeEvent, bool) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return NudgeEvent{}, false
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if t := &prd.Story[si].Tasks[ti]; t.ID == taskID {
				return t.NudgeEventForSession(sessionID)
			}
		}
	}
	return NudgeEvent{}, false
}

// RecordTaskNudgeOutcome sets the outcome of the latest nudge sent into the
// given session on the task. It returns an error when the outcome is not one
// of the NudgeOutcome constants, the task is unknown, or the session has no
// recorded nudge.
func (m *Manager) RecordTaskNudgeOutcome(prdID, taskID, sessionID, outcome string) error {
	switch outcome {
	case NudgeOutcomeSent, NudgeOutcomeAnswered, NudgeOutcomeStalled:
	default:
		return fmt.Errorf("unknown nudge outcome %q", outcome)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.ID != taskID {
				continue
			}
			for ei := len(t.NudgeEvents) - 1; ei >= 0; ei-- {
				ev := &t.NudgeEvents[ei]
				if sessionID == "" || ev.SessionID != sessionID {
					continue
				}
				ev.Outcome = outcome
				now := time.Now()
				t.UpdatedAt = now
				prd.UpdatedAt = now
				return m.store.SavePRD(prd)
			}
			return fmt.Errorf("no nudge recorded for session %q on task %q", sessionID, taskID)
		}
	}
	return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
}

// RecordTaskDeliveryFailure records one task delivery failure on the task: it
// appends a DeliveryFailureEvent carrying the reason. Like RecordTaskNudge it
// fires from the daemon while the worker runs, so there is no lock-after-approve
// gate.
func (m *Manager) RecordTaskDeliveryFailure(prdID, taskID string, reason string, at time.Time) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.ID == taskID {
				t.DeliveryFailureEvents = append(t.DeliveryFailureEvents, DeliveryFailureEvent{At: at, Reason: reason})
				t.UpdatedAt = at
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = at
	return m.store.SavePRD(prd)
}

// SetStoryLLM (BL381) lets the operator override a story's worker LLM
// (backend / effort / model). All tasks in this story inherit these
// values unless overridden at the task level. Empty string clears the
// override (falls back to PRD-level then global). Accepted-while is
// governed by autonomous.task_override_statuses (default: needs_review /
// revisions_asked / cancelled / blocked).
func (m *Manager) SetStoryLLM(prdID, storyID, backend, effort, model, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	var story *Story
	for i := range prd.Story {
		if prd.Story[i].ID == storyID {
			story = &prd.Story[i]
			break
		}
	}
	if story == nil {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	// Refuse before any field changes: check the pair the story's tasks
	// will resolve to, using the story's new values over the record's.
	if err := checkResolvedLLMPair(prdID, &Task{}, &Story{Backend: backend, Model: model}, prd, m.AutomationForPRD(prd)); err != nil {
		return nil, err
	}
	story.Backend = backend
	story.Effort = Effort(effort)
	story.Model = model
	story.UpdatedAt = time.Now()
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_story_llm", Actor: actor,
		Note: fmt.Sprintf("story=%s backend=%s effort=%s model=%s", storyID, backend, effort, model),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetTaskLLM (BL203, v5.4.0) lets the operator override a task's
// worker LLM (backend / effort / model) before approval. Empty string
// clears the override (falls back to story-level then PRD-level then global).
// Accepted-while is governed by autonomous.task_override_statuses (default:
// needs_review / revisions_asked / cancelled / blocked); outside those
// statuses the worker is locked in.
func (m *Manager) SetTaskLLM(prdID, taskID, backend, effort, model, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	task, story := findTaskInPRD(prd, taskID)
	if task == nil {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	// Refuse before any field changes: check the pair the task will run
	// with, after the task's new values are applied over the story and
	// PRD the executor falls back to.
	if err := checkResolvedLLMPair(prdID, &Task{Backend: backend, Model: model}, story, prd, m.AutomationForPRD(prd)); err != nil {
		return nil, err
	}
	task.Backend = backend
	task.Effort = Effort(effort)
	task.Model = model
	task.UpdatedAt = time.Now()
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_task_llm", Actor: actor,
		Note: fmt.Sprintf("task=%s backend=%s effort=%s model=%s", taskID, backend, effort, model),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetTaskGuardrailLLM (BL416) overrides the backend/model used for a
// task's per-task guardrail judge. Presence-aware: a nil pointer leaves
// that field unchanged; a pointer to "" clears it so resolution falls
// back to the PRD-level then global guardrail_backend/guardrail_model.
// Same accepted-while rule as SetTaskLLM (autonomous.task_override_statuses).
func (m *Manager) SetTaskGuardrailLLM(prdID, taskID string, backend, model *string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	var task *Task
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				task = &prd.Story[si].Tasks[ti]
				break
			}
		}
		if task != nil {
			break
		}
	}
	if task == nil {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	if backend != nil || model != nil {
		// Refuse an unpairable replacement pair first — the same v8.37.2
		// rule the sibling setters enforce via checkLevelPair — so a bare
		// Claude alias on a pi-family backend (or any model the effective
		// backend cannot serve) is a pairing mismatch
		// (ErrModelBackendMismatch) regardless of what other config tiers
		// exist below it.
		if err := checkLevelPair(prdID, backend, model, task.GuardrailBackend, task.GuardrailModel); err != nil {
			return nil, err
		}
		// v9.10.18 — when the call pins/clears the MODEL, additionally
		// require the full unit pair to resolve to a servable model on the
		// copy the call would produce (v9.10.18 no-implicit-default
		// contract): a pin that leaves the unit with no resolvable model
		// is refused here instead of passing validation and failing at
		// judge time. Backend-only calls stay as accepted as before — the
		// unit's model may legitimately come from a lower tier, and the
		// resolver records a loud infra block at judge time if not.
		if model != nil {
			next := *task
			if backend != nil {
				next.GuardrailBackend = *backend
			}
			next.GuardrailModel = *model
			if _, _, _, gerr := m.resolveUnitGuardrailModel(&next, prd); gerr != nil {
				return nil, fmt.Errorf("prd %q: guardrail model: %w", prdID, gerr)
			}
		}
	}
	if backend != nil {
		task.GuardrailBackend = *backend
	}
	if model != nil {
		task.GuardrailModel = *model
	}
	task.UpdatedAt = time.Now()
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_task_guardrail_llm", Actor: actor,
		Note: fmt.Sprintf("task=%s guardrail_backend=%s guardrail_model=%s", taskID, task.GuardrailBackend, task.GuardrailModel),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetPRDLLM (BL203, v5.4.0) overrides the PRD-level worker LLM defaults
// (backend / effort / model / decomposition LLM). Presence-aware: a nil
// pointer leaves that field unchanged; a pointer to "" clears it so
// resolution falls back to the global default. Only allowed pre-Run.
// Tasks without per-task overrides will inherit these.
func (m *Manager) SetMemorySeed(prdID string, cfg MemorySeedConfig, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.MemorySeed = cfg
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_memory_seed", Actor: actor,
		Note: fmt.Sprintf("enabled=%v max_per_scope=%d", cfg.Enabled, cfg.MaxPerScope),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// validPermissionModes mirrors the claude-code permission-mode set the
// start_session MCP tool already validates against (internal/mcp/server.go).
var validPermissionModes = map[string]bool{
	"default": true, "plan": true, "acceptEdits": true,
	"auto": true, "bypassPermissions": true, "dontAsk": true,
}

// SetPermissionMode (GH#172 D75) updates the PRD's permission_mode, which
// tasks spawned under it inherit as their claude-code --permission-mode
// default (see Task.PermissionMode's own doc comment). Empty clears it
// back to the session/config default.
func (m *Manager) SetPermissionMode(prdID, mode, actor string) (*PRD, error) {
	if mode != "" && !validPermissionModes[mode] {
		return nil, fmt.Errorf("invalid permission_mode %q — valid values: default, plan, acceptEdits, auto, bypassPermissions, dontAsk", mode)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.PermissionMode = mode
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_permission_mode", Actor: actor,
		Note: fmt.Sprintf("permission_mode=%s", mode),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// Pause (GH#172 D52) marks a running PRD paused. The already-live
// executor goroutine (if any) notices this on its next result-loop
// iteration (see executor.go's Run) and drains in-flight work without
// starting anything new — this method only flips the stored status;
// it does not itself touch any goroutine.
func (m *Manager) Pause(prdID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDRunning {
		return nil, fmt.Errorf("prd %q status %q is not running; only a running PRD can be paused", prdID, prd.Status)
	}
	old := prd.Status
	prd.Status = PRDPaused
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "pause", Actor: actor})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "pause"), nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// Resume (GH#172 D52) flips a paused PRD back to running. It does not
// itself relaunch the executor goroutine — the caller (API.Resume)
// does that the same way API.Run always does, since only the API
// layer holds the spawn/verify callbacks Manager.Run needs.
func (m *Manager) Resume(prdID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDPaused {
		return nil, fmt.Errorf("prd %q status %q is not paused; nothing to resume", prdID, prd.Status)
	}
	old := prd.Status
	prd.Status = PRDRunning
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "resume", Actor: actor})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "resume"), nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// ResumeParked resumes a PRD that the boot-resume gate parked. It accepts
// ONLY a blocked PRD whose newest park/resume decision is
// "boot_resume_parked" (the Decision parkPRDForBootResume persists, so the
// marker survives a restart); any other state is an error naming the
// allowed state. It clears BlockedReason, records Decision{Kind:"resume"},
// sets the status back to running, persists, emits the status event and
// records a "resumed_by_operator" boot-resume entry. It does not start the
// executor; API.ResumeParked does that through runLocked.
func (m *Manager) ResumeParked(prdID, actor, note string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	m.mu.Lock()
	status := prd.Status
	parked := status == PRDBlocked && isBootParked(prd)
	m.mu.Unlock()
	if !parked {
		return nil, fmt.Errorf("prd %q status %q cannot be resumed this way; only a blocked PRD parked by the boot-resume gate can (status %q)", prdID, status, PRDBlocked)
	}
	h := m.bootResumeHooks()
	now := time.Now()
	ageSec := int64(0)
	if newest := newestActivity(prd, h); !newest.IsZero() {
		if d := int64(now.Sub(newest).Seconds()); d > 0 {
			ageSec = d
		}
	}
	live := liveWorkerSessions(prd, h)
	m.mu.Lock()
	prd.Status = PRDRunning
	prd.BlockedReason = ""
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "resume", Actor: actor, Note: note})
	m.mu.Unlock()
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, PRDBlocked, lastDecision(prd, "resume"), nil)
	reason := "operator resumed the parked PRD"
	if note != "" {
		reason += ": " + note
	}
	m.recordBootResume(BootResumeEntry{PRDID: prdID, Outcome: "resumed_by_operator", AgeSeconds: ageSec, Reason: reason, At: now, LiveSessions: live})
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// isBootParked reports whether the newest park/resume decision on prd is
// the boot-resume park marker.
func isBootParked(prd *PRD) bool {
	for i := len(prd.Decisions) - 1; i >= 0; i-- {
		switch prd.Decisions[i].Kind {
		case bootResumeParkedKind:
			return true
		case "resume":
			return false
		}
	}
	return false
}

// SetMemoryHarvest (BL386 Phase 2) updates the PRD's harvest-on-completion config.
func (m *Manager) SetMemoryHarvest(prdID string, cfg MemoryHarvestConfig, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.MemoryHarvest = cfg
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_memory_harvest", Actor: actor,
		Note: fmt.Sprintf("enabled=%v promote_to=%s max=%d", cfg.Enabled, cfg.PromoteTo, cfg.Max),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// prdLLMOverrideAllowed reports whether the worker backend and model of an
// Automaton in the given status may still be changed: the overrides only apply
// before a run, so a running or completed Automaton is locked. SetPRDLLM
// enforces it and lane fill reads it to know whether a backend can move.
func prdLLMOverrideAllowed(status PRDStatus) bool {
	return status != PRDRunning && status != PRDCompleted
}

func (m *Manager) SetPRDLLM(prdID string, backend, effort, model, decompositionProfile, decompositionModel *string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if !prdLLMOverrideAllowed(prd.Status) {
		return nil, fmt.Errorf("prd %q status %q is locked; PRD LLM overrides only apply pre-Run", prdID, prd.Status)
	}
	// The decomposition model is not checked: its backend is the planning
	// backend, which resolvePlannedModel resolves separately.
	if err := checkLevelPair(prdID, backend, model, prd.Backend, prd.Model); err != nil {
		return nil, err
	}
	if backend != nil {
		prd.Backend = *backend
	}
	if effort != nil {
		prd.Effort = Effort(*effort)
	}
	if model != nil {
		prd.Model = *model
	}
	if decompositionProfile != nil {
		prd.DecompositionProfile = *decompositionProfile
	}
	if decompositionModel != nil {
		prd.DecompositionModel = *decompositionModel
	}
	prd.UpdatedAt = time.Now()
	// Decision-log note lists ONLY the fields included in this call:
	// a pointer to "" shows the field as cleared (empty value), a nil
	// field is not mentioned at all.
	var parts []string
	for _, p := range []struct {
		name string
		v    *string
	}{
		{"backend", backend},
		{"effort", effort},
		{"model", model},
		{"decomposition_profile", decompositionProfile},
		{"decomposition_model", decompositionModel},
	} {
		if p.v != nil {
			parts = append(parts, p.name+"="+*p.v)
		}
	}
	note := strings.Join(parts, " ")
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_prd_llm", Actor: actor,
		Note: note,
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetPRDGuardrailLLM (BL416) overrides the PRD-level backend/model used
// by the per-task / per-story guardrail judges. Presence-aware: a nil
// pointer leaves that field unchanged; a pointer to "" clears it so
// resolution falls back to the global guardrail_backend/guardrail_model.
// Same lock rule as SetPRDLLM.
func (m *Manager) SetPRDGuardrailLLM(prdID string, backend, model *string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status == PRDRunning || prd.Status == PRDCompleted {
		return nil, fmt.Errorf("prd %q status %q is locked; PRD LLM overrides only apply pre-Run", prdID, prd.Status)
	}
	if err := checkLevelPair(prdID, backend, model, prd.GuardrailBackend, prd.GuardrailModel); err != nil {
		return nil, err
	}
	if backend != nil {
		prd.GuardrailBackend = *backend
	}
	if model != nil {
		prd.GuardrailModel = *model
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_prd_guardrail_llm", Actor: actor,
		Note: fmt.Sprintf("guardrail_backend=%s guardrail_model=%s", prd.GuardrailBackend, prd.GuardrailModel),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// validVerificationEffort reports whether e is empty (inherit) or one
// of the Effort constants.
func validVerificationEffort(e string) bool {
	switch Effort(e) {
	case "", EffortLow, EffortMedium, EffortHigh, EffortMax, EffortQuick, EffortNormal, EffortThorough:
		return true
	}
	return false
}

// verificationLLMLocked applies SetTaskLLM's lock rule: verifier LLM
// overrides are accepted only in needs_review / revisions_asked /
// cancelled.
func verificationLLMLocked(prd *PRD) error {
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept LLM overrides", prd.ID, prd.Status)
	}
	return nil
}

// taskOverrideStatusesKnown is the PRD status set a
// task_override_statuses list may name (the models.go PRDStatus constants).
var taskOverrideStatusesKnown = []string{
	string(PRDDraft), string(PRDPlanning), string(PRDNeedsReview), string(PRDApproved),
	string(PRDRevisionsAsked), string(PRDActive), string(PRDRunning), string(PRDCompleted), string(PRDRejected),
	string(PRDCancelled), string(PRDArchived), string(PRDBlocked), string(PRDFailed),
	string(PRDPaused),
}

// TaskOverrideStatusesKnown returns the 14 PRD status names a
// task_override_statuses list may name (the models.go status constants).
func TaskOverrideStatusesKnown() []string {
	return append([]string(nil), taskOverrideStatusesKnown...)
}

// TaskOverrideStatusValid reports whether s is a known PRD status name.
func TaskOverrideStatusValid(s string) bool {
	for _, k := range taskOverrideStatusesKnown {
		if s == k {
			return true
		}
	}
	return false
}

// TaskOverrideStatusesDefault — v9.5.5 default accepted-while list: the
// pre-existing pre-Run trio plus blocked. A blocked PRD is
// operator-held and unblocking it usually starts with re-pointing the
// next retry's LLM, which used to be refused with
// `status "blocked" is locked`.
func TaskOverrideStatusesDefault() []string {
	return []string{string(PRDNeedsReview), string(PRDRevisionsAsked), string(PRDCancelled), string(PRDBlocked)}
}

// EffectiveTaskOverrideStatuses resolves the empty-list-means-default
// convention: non-empty lists are returned verbatim (copy), empty/nil
// lists return the default 4.
func EffectiveTaskOverrideStatuses(list []string) []string {
	if len(list) == 0 {
		return TaskOverrideStatusesDefault()
	}
	return append([]string(nil), list...)
}

// ValidateTaskOverrideStatuses reports the first unknown name and the
// known set. Empty list is VALID (= revert to defaults). Error must be
// exactly the shape (so every surface returns the same message).
func ValidateTaskOverrideStatuses(list []string) error {
	for _, s := range list {
		if !TaskOverrideStatusValid(s) {
			return fmt.Errorf("task_override_statuses: unknown PRD status %q (known: %s)",
				s, strings.Join(taskOverrideStatusesKnown, ", "))
		}
	}
	return nil
}

// taskOverrideLocked enforces the configurable accepted-while rule for
// task-level and story-level LLM override calls (v9.5.5). PRD-level
// overrides keep using verificationLLMLocked / their inline checks.
func (m *Manager) taskOverrideLocked(prd *PRD, prdID string) error {
	eff := EffectiveTaskOverrideStatuses(m.cfg.TaskOverrideStatuses)
	for _, s := range eff {
		if s == string(prd.Status) {
			return nil
		}
	}
	return fmt.Errorf("prd %q status %q is locked; only %s accept LLM overrides (autonomous.task_override_statuses)",
		prdID, prd.Status, strings.Join(eff, " / "))
}

// applyVerificationLLM writes the presence-aware overrides onto the
// three fields: a nil pointer leaves the field unchanged; a pointer to
// "" clears it so resolution falls back to the next level.
// findTaskInPRD returns the task with taskID and the story that holds it,
// or nil, nil when no task matches.
func findTaskInPRD(prd *PRD, taskID string) (*Task, *Story) {
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				return &prd.Story[si].Tasks[ti], &prd.Story[si]
			}
		}
	}
	return nil, nil
}

// pickString returns the value a presence-aware setter field carries: the
// new value when the call sent the field (a nil pointer means it did not),
// otherwise the record's current value.
func pickString(next *string, current string) string {
	if next == nil {
		return current
	}
	return *next
}

// checkLevelPair refuses a backend/model override at one level (PRD, story
// or task) when the call sets either field. The pair checked is the one the
// level will hold after the call: a field the call leaves out keeps its
// current value. A call that touches neither field is not checked, so an
// unrelated edit cannot be refused over a pair it never changed.
func checkLevelPair(prdID string, backend, model *string, curBackend, curModel string) error {
	if backend == nil && model == nil {
		return nil
	}
	if err := validateModelForBackend(pickString(backend, curBackend), pickString(model, curModel)); err != nil {
		return fmt.Errorf("prd %q: %w", prdID, err)
	}
	return nil
}

// checkResolvedLLMPair refuses a story- or task-level worker pair that the
// executor would run with. It resolves the effective backend and model
// through resolveTaskBackendModel, so an empty backend inherits the story
// or PRD backend exactly as a run does.
func checkResolvedLLMPair(prdID string, task *Task, story *Story, prd *PRD, auto *Automation) error {
	backend, model := resolveTaskBackendModel(task, story, prd, auto, "")
	if err := validateModelForBackend(backend, model); err != nil {
		return fmt.Errorf("prd %q: %w", prdID, err)
	}
	return nil
}

func applyVerificationLLM(dstBackend, dstModel *string, dstEffort *Effort, backend, model, effort *string) {
	if backend != nil {
		*dstBackend = *backend
	}
	if model != nil {
		*dstModel = *model
	}
	if effort != nil {
		*dstEffort = Effort(*effort)
	}
}

// SetTaskVerificationLLM overrides the verifier backend/model/effort
// for one task (most specific level; see ResolveVerificationLLM).
// Presence-aware: a nil pointer leaves that field unchanged; a pointer
// to "" clears it so resolution falls back to the story, then the
// Automaton, then the global verification_* config. Same accepted-while
// rule as SetTaskLLM (autonomous.task_override_statuses).
func (m *Manager) SetTaskVerificationLLM(prdID, taskID string, backend, model, effort *string, actor string) (*PRD, error) {
	if effort != nil && !validVerificationEffort(*effort) {
		return nil, fmt.Errorf("invalid verification effort %q", *effort)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	var task *Task
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				task = &prd.Story[si].Tasks[ti]
				break
			}
		}
		if task != nil {
			break
		}
	}
	if task == nil {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	if err := checkLevelPair(prdID, backend, model, task.VerificationBackend, task.VerificationModel); err != nil {
		return nil, err
	}
	applyVerificationLLM(&task.VerificationBackend, &task.VerificationModel, &task.VerificationEffort, backend, model, effort)
	task.UpdatedAt = time.Now()
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_task_verification_llm", Actor: actor,
		Note: fmt.Sprintf("task=%s verification_backend=%s verification_model=%s verification_effort=%s",
			taskID, task.VerificationBackend, task.VerificationModel, task.VerificationEffort),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetStoryVerificationLLM overrides the verifier backend/model/effort
// for every task in one story that has no task-level override.
// Presence-aware like SetTaskVerificationLLM; "" falls back to the
// Automaton then the global verification_* config. Same accepted-while
// rule as SetTaskLLM (autonomous.task_override_statuses).
func (m *Manager) SetStoryVerificationLLM(prdID, storyID string, backend, model, effort *string, actor string) (*PRD, error) {
	if effort != nil && !validVerificationEffort(*effort) {
		return nil, fmt.Errorf("invalid verification effort %q", *effort)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	var story *Story
	for i := range prd.Story {
		if prd.Story[i].ID == storyID {
			story = &prd.Story[i]
			break
		}
	}
	if story == nil {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	if err := checkLevelPair(prdID, backend, model, story.VerificationBackend, story.VerificationModel); err != nil {
		return nil, err
	}
	applyVerificationLLM(&story.VerificationBackend, &story.VerificationModel, &story.VerificationEffort, backend, model, effort)
	story.UpdatedAt = time.Now()
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_story_verification_llm", Actor: actor,
		Note: fmt.Sprintf("story=%s verification_backend=%s verification_model=%s verification_effort=%s",
			storyID, story.VerificationBackend, story.VerificationModel, story.VerificationEffort),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetPRDVerificationLLM overrides the Automaton-level verifier
// backend/model/effort. Presence-aware like SetTaskVerificationLLM; ""
// falls back to the global verification_* config. Same lock rule as
// SetPRDLLM.
func (m *Manager) SetPRDVerificationLLM(prdID string, backend, model, effort *string, actor string) (*PRD, error) {
	if effort != nil && !validVerificationEffort(*effort) {
		return nil, fmt.Errorf("invalid verification effort %q", *effort)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := verificationLLMLocked(prd); err != nil {
		return nil, err
	}
	if err := checkLevelPair(prdID, backend, model, prd.VerificationBackend, prd.VerificationModel); err != nil {
		return nil, err
	}
	applyVerificationLLM(&prd.VerificationBackend, &prd.VerificationModel, &prd.VerificationEffort, backend, model, effort)
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_prd_verification_llm", Actor: actor,
		Note: fmt.Sprintf("verification_backend=%s verification_model=%s verification_effort=%s",
			prd.VerificationBackend, prd.VerificationModel, prd.VerificationEffort),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// InstantiateTemplate (BL191 Q2) takes a template PRD + caller-supplied
// vars and stores a fresh executable PRD with substitutions applied to
// spec, title, and per-task spec strings. Required vars without a value
// are an error; defaults fill missing optionals.
func (m *Manager) InstantiateTemplate(templateID string, vars map[string]string, actor string) (*PRD, error) {
	tmpl, ok := m.store.GetPRD(templateID)
	if !ok {
		return nil, fmt.Errorf("template %q not found", templateID)
	}
	if !tmpl.IsTemplate {
		return nil, fmt.Errorf("prd %q is not a template", templateID)
	}
	resolved := make(map[string]string, len(tmpl.TemplateVars))
	for _, v := range tmpl.TemplateVars {
		if val, ok := vars[v.Name]; ok && val != "" {
			resolved[v.Name] = val
			continue
		}
		if v.Default != "" {
			resolved[v.Name] = v.Default
			continue
		}
		if v.Required {
			return nil, fmt.Errorf("template var %q is required but missing", v.Name)
		}
	}
	subst := func(s string) string {
		out := s
		for name, val := range resolved {
			out = replaceAll(out, "{{"+name+"}}", val)
		}
		return out
	}
	newPRD, err := m.store.CreatePRD(subst(tmpl.Spec), tmpl.ProjectDir, tmpl.Backend, tmpl.Model, tmpl.Effort)
	if err != nil {
		return nil, err
	}
	newPRD.Title = subst(tmpl.Title)
	newPRD.TemplateOf = tmpl.ID
	newPRD.LanePreference = tmpl.LanePreference
	newPRD.VerificationBackend = tmpl.VerificationBackend
	newPRD.VerificationModel = tmpl.VerificationModel
	newPRD.VerificationEffort = tmpl.VerificationEffort
	// Copy stories with substitutions applied to per-task specs.
	for _, st := range tmpl.Story {
		st2 := st
		tasks2 := make([]Task, 0, len(st.Tasks))
		for _, t := range st.Tasks {
			t2 := t
			t2.Spec = subst(t.Spec)
			t2.Title = subst(t.Title)
			tasks2 = append(tasks2, t2)
		}
		st2.Tasks = tasks2
		newPRD.Story = append(newPRD.Story, st2)
	}
	if len(newPRD.Story) > 0 {
		// Pre-decomposed from template — land in needs_review so operator
		// can confirm before Run.
		newPRD.Status = PRDNeedsReview
	}
	newPRD.Decisions = append(newPRD.Decisions, Decision{
		At: time.Now(), Kind: "template_instantiate", Actor: actor,
		Note: fmt.Sprintf("from=%s vars=%d", tmpl.ID, len(resolved)),
	})
	if err := m.store.SavePRD(newPRD); err != nil {
		return nil, err
	}
	if newPRD.Status == PRDNeedsReview {
		// Freshly created PRD: no prior status, so OldStatus is empty.
		m.emitPRDStatus(newPRD, "", lastDecision(newPRD, "template_instantiate"), nil)
	}
	updated, _ := m.store.GetPRD(newPRD.ID)
	return updated, nil
}

// replaceAll is a small allocation-friendly substring replace; bypasses
// strings.ReplaceAll only because we want zero-package-import in this
// hot path.
func replaceAll(s, old, new string) string {
	for {
		i := indexOf(s, old)
		if i < 0 {
			return s
		}
		s = s[:i] + new + s[i+len(old):]
	}
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Status returns a snapshot of the loop state.
//
// v5.22.0 — observability fill-in for BL191 Q4 + Q6. Counts
// ChildPRDs (parent_prd_id != ""), MaxDepthSeen, BlockedPRDs
// (status == PRDBlocked), and VerdictCounts (outcome rollup across
// every Story.Verdicts + Task.Verdicts) so operators querying
// /api/autonomous/status get visible signal for the recursion + per-
// task/story guardrail features.
func (m *Manager) Status() LoopStatus {
	// Built before m.mu is taken: the summaries read the Automation store
	// and the PRD store, and neither may be entered while holding m.mu.
	automations := m.automationStatusSummaries(time.Now())
	usageBackoff := m.usageBackoffSummary()
	budgets := m.budgetsSummary()
	// Same rule for the task views: they read the PRD store and the ledger.
	queue, holdings := m.buildTaskVisibility(budgets)
	// Held tasks are read under the store lock (the lock is not re-entrant, so
	// this runs before m.mu is taken, as the PRD reads above do).
	var held []HeldTaskStatus
	m.store.WithLock(func() { held = heldTasksAcrossPRDs(m.store.prds) })
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.status
	st.ReviewNeeded = held
	st.Budgets = budgets
	st.Queue, st.Holdings = queue, holdings
	st.BootResume = append([]BootResumeEntry(nil), m.bootResumeReport...)
	st.UsageBackoff = usageBackoff
	st.Running = m.ctx != nil && m.ctx.Err() == nil
	verdictCounts := map[string]int{}
	// Notify block (notify-events): the effective global default plus the
	// non-finished Automata whose own (or profile) list overrides it.
	// defNotify is captured under m.mu like every other Config read; the
	// tier resolution itself is the lock-free core (store calls may run
	// under m.mu — same nesting as the ListPRDs loop below).
	defNotify := m.cfg.NotifyDefaultEvents
	if defNotify == nil {
		defNotify = DefaultNotifyEvents()
	}
	notifyDefault := copyStrings(defNotify)
	if notifyDefault == nil {
		notifyDefault = []string{}
	}
	var notifyOverrides []NotifyOverride
	for _, p := range m.store.ListPRDs() {
		if p.Status == PRDActive || p.Status == PRDRunning {
			st.ActivePRDs++
		}
		if p.ParentPRDID != "" {
			st.ChildPRDsTotal++
		}
		if p.Depth > st.MaxDepthSeen {
			st.MaxDepthSeen = p.Depth
		}
		if p.Status == PRDBlocked {
			st.BlockedPRDs++
		}
		// The Automaton overrides the global notify tier when its resolved
		// set comes from the automaton or profile tier; finished Automata
		// no longer running and never count as overrides.
		if !finishedPRDStatus(p.Status) {
			if set := m.resolveNotifyTier(p, defNotify); set.Source != NotifySourceGlobal {
				notifyOverrides = append(notifyOverrides, NotifyOverride{
					ID:     p.ID,
					Source: set.Source,
					Events: set.Events,
				})
			}
		}
		for _, s := range p.Story {
			for _, v := range s.Verdicts {
				if v.Outcome != "" {
					verdictCounts[v.Outcome]++
				}
			}
			for _, t := range s.Tasks {
				switch t.Status {
				case TaskQueued:
					st.QueuedTasks++
				case TaskInProgress, TaskRunningTests, TaskVerifying:
					st.RunningTasks++
				}
				for _, v := range t.Verdicts {
					if v.Outcome != "" {
						verdictCounts[v.Outcome]++
					}
				}
				// BL367 — quality gate counters.
				if t.QualityGateResult != nil {
					st.QualityGateRuns++
					if t.QualityGateResult.Invalid {
						st.QualityGateInvalid++ // invalid baseline: neither pass nor regression
					} else if t.QualityGateResult.Regression {
						st.QualityGateFail++
						st.QualityGateRegressions++
					} else {
						st.QualityGatePass++
					}
				}
				if t.NudgeCount > 0 {
					backend, _ := resolveTaskBackendModel(&t, &s, p, m.AutomationForPRD(p), m.cfg.ExecutionBackend)
					if backend == "" {
						backend = "unknown"
					}
					st.NudgesTotal += t.NudgeCount
					st.NudgeTasks = append(st.NudgeTasks, TaskNudge{PRDID: p.ID, StoryID: s.ID, TaskID: t.ID, Backend: backend, Count: t.NudgeCount})
					if st.NudgesByBackend == nil {
						st.NudgesByBackend = map[string]int{}
					}
					st.NudgesByBackend[backend] += t.NudgeCount
				}
				// BL382 — cancelled task count.
				if t.Status == TaskCancelled {
					st.TasksCancelled++
				}
			}
			// BL382 — cancelled story count.
			if s.Status == StoryCancelled {
				st.StoriesCancelled++
			}
		}
		// BL382 — requeued task count from decision log.
		for _, d := range p.Decisions {
			if d.Kind == "requeue_task" {
				st.TasksRequeued++
			}
		}
	}
	if len(verdictCounts) > 0 {
		st.VerdictCounts = verdictCounts
	}
	// Notify block — the resolved global tier and the per-Automaton
	// overrides, sorted by ID and capped. Both lists are never nil, so
	// the block always emits both keys as lists (empty, never null).
	if len(notifyOverrides) > 0 {
		sort.Slice(notifyOverrides, func(i, j int) bool {
			return notifyOverrides[i].ID < notifyOverrides[j].ID
		})
		if len(notifyOverrides) > NotifyStatusMaxOverrides {
			notifyOverrides = notifyOverrides[:NotifyStatusMaxOverrides]
		}
	} else {
		notifyOverrides = []NotifyOverride{}
	}
	st.Notify = &NotifyStatus{
		DefaultEvents: notifyDefault,
		Overrides:     notifyOverrides,
	}
	st.Warnings = PairingWarnings(m.cfg, m.pairingDecomposeRefs(m.cfg))
	st.Automations = automations
	return st
}

// Start kicks the background loop. Idempotent.
func (m *Manager) Start(parent context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx != nil && m.ctx.Err() == nil {
		return // already running
	}
	if !m.cfg.Enabled {
		return
	}
	m.ctx, m.cancel = context.WithCancel(parent)
	go m.run(m.ctx)
	if r := m.progressSnaps; r != nil {
		go r.Run(m.ctx)
	}
}

// Stop signals the loop to exit (does not block).
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
		m.ctx = nil
	}
}

// reconcileStuckTasks scans all PRDs on daemon start and resolves any stuck
// TaskInProgress/TaskVerifying/TaskRunningTests whose session is gone (B90).
// TaskVerifying/TaskRunningTests tasks resolve via ResolveVerifyingTask: a
// recorded ok verification verdict is applied (TaskCompleted) instead of a
// blind reset; everything else — TaskInProgress, or a verifying task with a
// failed or missing verdict — resets to TaskPending (not failed) so the
// executor restart picks it up naturally, avoiding the bug where tasks end up
// stuck in failed state because resetInProgressTasksForResume only looks for
// TaskInProgress (which reconcileStuckTasks would have already converted).
func (m *Manager) reconcileStuckTasks() {
	m.mu.Lock()
	aliveFn := m.sessionAliveFn
	killerFn := m.sessionKillerFn
	listFn := m.sessionListerFn
	agentStartedFn := m.sessionAgentStartedFn
	outputFn := m.sessionOutputFn
	m.mu.Unlock()

	prds := m.store.ListPRDs()
	for _, prd := range prds {
		if prd.Status != PRDRunning && prd.Status != PRDActive {
			continue
		}
		changed := false
		adoptedAny := false
		verdictCompleted, verdictRequeued := 0, 0
		var reset []pendingTaskEvent
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.Status != TaskInProgress && t.Status != TaskVerifying && t.Status != TaskRunningTests {
					continue
				}
				completing := false
				startFailed := false
				if t.Status == TaskVerifying || t.Status == TaskRunningTests {
					switch ResolveVerifyingResume(aliveFn, stableAgentStarted(agentStartedFn), outputFn, t) {
					case VerifyingResumeStartFailed:
						// Live bare shell whose agent never started: never adopted. The reset
						// below ends the session and requeues the task.
						startFailed = true
					case VerifyingResumeKeepLive:
						continue // session still live; leave it
					case VerifyingResumeReverify:
						// The worker finished and no verdict is recorded: keep the
						// task verifying with its session, result and SHAs
						// untouched, so executeOne verifies it once instead of
						// running the worker again. No kill, no retry counted.
						m.adoptCapacity(prd, t)
						log.Printf("[autonomous] boot-reconcile: prd=%s task=%s kept verifying (worker finished, no verdict recorded, session %s)",
							prd.ID, t.ID, t.SessionID)
						continue
					case VerifyingResumeComplete:
						// A recorded ok verification verdict stands: complete the
						// task instead of blindly re-running it. Verification is
						// kept untouched.
						completing = true
					}
				} else if aliveFn != nil && t.SessionID != "" && aliveFn(t.SessionID) {
					continue // session still live; leave it (TaskInProgress)
				}
				// A live session may belong to this task even though its link is
				// empty or dead (created just before the restart): adopt it
				// instead of resetting. Extra live matches are left to the
				// reset-cleanup path.
				if !completing && !startFailed {
					if id, _ := m.adoptUnlinkedSession(prd, t, listFn); id != "" {
						// Older live matches would run beside the adopted session.
						recordEndedSessions(prd, t, m.endLiveSessionsOfTask(prd, t, id, "autonomous.boot-reconcile.duplicate", "older duplicate of the session adopted on daemon restart"), "older duplicate of the session adopted on daemon restart")
						adoptedAny = true
						continue
					}
				}
				// Kill any surviving tmux session so the new executor doesn't
				// spawn alongside it.
				if killerFn != nil && t.SessionID != "" {
					why := fmt.Sprintf("boot-reconcile: task %s was %s with no live session; the surviving session is ended so the new executor does not spawn beside it", t.ID, t.Status)
					if err := killerFn(t.SessionID, "autonomous.boot-reconcile.stuck", why); err != nil {
						log.Printf("[autonomous] boot-reconcile: kill orphaned session %s: %v", t.SessionID, err)
					}
				}
				// Count the orphaned verifying resolutions for the audit entry.
				if t.Status == TaskVerifying || t.Status == TaskRunningTests {
					if completing {
						verdictCompleted++
					} else {
						verdictRequeued++
					}
				}
				// Every other live session of this task or its worktree must end
				// before the replacement launches.
				recordEndedSessions(prd, t, m.endLiveSessionsOfTask(prd, t, "", "autonomous.boot-reconcile.reset", taskResetEndReason(prd.ID, t.ID)), "task reset on daemon restart")
				now := time.Now()
				if completing {
					log.Printf("[autonomous] boot-reconcile: prd=%s task=%s status=%s session=%s → TaskCompleted (recorded ok verification, dead session)",
						prd.ID, t.ID, t.Status, t.SessionID)
					// The session is gone, so the task's node lease is freed here
					// rather than left for the periodic reaper.
					m.releaseCapacity(t.ID)
					reset = append(reset, pendingTaskEvent{t: t, oldStatus: t.Status, oldSession: t.SessionID})
					t.Status = TaskCompleted
					t.CompletedAt = &now
					t.Error = ""
					t.SessionID = ""
					t.UpdatedAt = now
				} else {
					log.Printf("[autonomous] boot-reconcile: prd=%s task=%s status=%s session=%s → TaskPending (session gone on daemon restart)",
						prd.ID, t.ID, t.Status, t.SessionID)
					m.releaseCapacity(t.ID)
					reset = append(reset, pendingTaskEvent{t: t, oldStatus: t.Status, oldSession: t.SessionID})
					t.Status = TaskPending
					t.Error = ""
					t.SessionID = ""
					t.UpdatedAt = now
				}
				changed = true
			}
		}
		if !changed && !adoptedAny {
			continue
		}
		now := time.Now()
		prd.UpdatedAt = now
		if changed {
			prd.Decisions = append(prd.Decisions, Decision{
				At:    now,
				Kind:  "boot_reconcile",
				Actor: "autonomous",
				Note:  "orphaned task(s) resolved to completed (recorded ok verification) or pending on daemon restart",
			})
		}
		// Auditable verification of the orphaned-verifying resolutions: record
		// how many verifying tasks completed from a recorded ok verdict vs how
		// many were re-queued for retry. Guarded so a PRD saved twice in the
		// same boot pass never carries two verifying_resolution entries.
		if verdictCompleted+verdictRequeued > 0 && !decisionKindExists(prd, verifyingResolution) {
			prd.Decisions = append(prd.Decisions, Decision{
				At:    now,
				Kind:  verifyingResolution,
				Actor: "autonomous",
				Note:  fmt.Sprintf("boot pass resolved %d orphaned verifying task(s) to completed (recorded ok verification) and re-queued %d for retry", verdictCompleted, verdictRequeued),
			})
		}
		if err := m.store.SavePRD(prd); err != nil {
			log.Printf("[autonomous] boot-reconcile: save prd=%s: %v", prd.ID, err)
		} else {
			m.emitPendingTaskEvents(prd, reset, lastDecision(prd, "boot_reconcile"))
		}
	}
}

// run is the loop body. v1 is intentionally minimal: marks newly-
// active PRDs so external schedulers (e.g. main daemon's
// pipeline.Executor or operator-driven `datawatch autonomous run`)
// can pick them up. The actual Task → Session dispatch is done by
// the executor in executor.go (called from REST handler).
//
// ctx is the context Start created. The loop must not read m.ctx: Stop
// clears it, and a loop that had not yet reached its select would then call
// Done on a nil context.
func (m *Manager) run(ctx context.Context) {
	m.reconcileStuckTasks()

	interval := time.Duration(m.cfg.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.mu.Lock()
			m.status.LastTickAt = time.Now()
			m.mu.Unlock()
			m.sweepOrphanSessions()
			m.sweepStuckRunning()
		}
	}
}

// CloneToTemplate creates a new Template from a completed (or any terminal)
// PRD's spec + title. Strips execution state; use_count starts at 0.
func (m *Manager) CloneToTemplate(prdID, description, actor string) (*Template, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	return m.templates.Create(prd.Title, description, prd.Spec, "", nil)
}

// InstantiateFromTemplateStore (BL221 v6.2.0) — substitutes vars in a
// TemplateStore entry and creates a new draft PRD with the resulting spec.
func (m *Manager) InstantiateFromTemplateStore(templateID string, vars map[string]string, projectDir, backend string, effort Effort) (*PRD, error) {
	spec, _, err := m.templates.Instantiate(templateID, vars)
	if err != nil {
		return nil, err
	}
	return m.store.CreatePRD(spec, projectDir, backend, "", effort)
}

// ── BL221 (v6.2.0) Phase 3 — scan framework ──────────────────────────────

// SetScanGrader wires the LLM rules-check grader (Option C: inline LLM via
// POST /api/ask loopback). Nil disables LLM grading even when
// cfg.Scan.RulesGraderEnabled is true.
func (m *Manager) SetScanGrader(fn scan.GraderFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.graderFn = fn
}

// SetRuleEditorFn wires the LLM rule-editor (Phase 3b): proposes AGENT.md
// changes for scan violations. Nil disables rule editing.
func (m *Manager) SetRuleEditorFn(fn scan.RuleEditorFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ruleEditorFn = fn
}

// ScanConfig returns a copy of the current scan sub-config.
func (m *Manager) ScanConfig() scan.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Scan
}

// SetScanConfig replaces the scan sub-config at runtime.
func (m *Manager) SetScanConfig(sc scan.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Scan = sc
}

// RunScan executes the configured scanners against the PRD's project_dir.
// Results are cached in memory (last result per PRD); re-run to refresh.
func (m *Manager) RunScan(prdID string) (*scan.Result, error) {
	m.mu.Lock()
	cfg := m.cfg
	grader := m.graderFn
	m.mu.Unlock()

	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.ProjectDir == "" {
		return nil, fmt.Errorf("prd %q has no project_dir set", prdID)
	}

	sc := cfg.Scan
	var scanners []scan.Scanner
	if sc.SASTEnabled {
		scanners = append(scanners, scan.NewSASTScanner())
	}
	if sc.SecretsEnabled {
		scanners = append(scanners, scan.NewSecretsScanner())
	}
	if sc.DepsEnabled {
		scanners = append(scanners, scan.NewDepsScanner())
	}
	// BL406 Phase 1 — the fourth, data-driven scan category. Runs
	// whenever there are rules configured, independent of the three
	// boolean toggles above (project rules aren't security scanners).
	if len(sc.ProjectRules) > 0 {
		scanners = append(scanners, scan.NewProjectRulesScanner(sc.ProjectRules, buildPRDParityContext(prd)))
	}

	gradeFn := grader
	if !sc.RulesGraderEnabled {
		gradeFn = nil
	}

	r := scan.Run(prd.ProjectDir, sc, scanners, gradeFn)
	r.PRDID = prdID
	m.scanResults.Store(prdID, &r)
	m.fireUpstreamIssueActions(sc.ProjectRules, r.Findings)
	return &r, nil
}

// GetScanResult returns the latest cached scan result for a PRD.
func (m *Manager) GetScanResult(prdID string) (*scan.Result, bool) {
	v, ok := m.scanResults.Load(prdID)
	if !ok {
		return nil, false
	}
	r, ok := v.(*scan.Result)
	return r, ok
}

// CreateFixPRD creates a child PRD whose spec targets the violations in the
// last cached scan result for prdID. Returns the new child PRD.
func (m *Manager) CreateFixPRD(prdID string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	v, hasScan := m.scanResults.Load(prdID)
	if !hasScan {
		return nil, fmt.Errorf("no scan result for prd %q; run scan first", prdID)
	}
	result, _ := v.(*scan.Result)
	if result == nil || len(result.Findings) == 0 {
		return nil, fmt.Errorf("no findings to fix in prd %q", prdID)
	}
	spec := scan.BuildFixSpec(result.Findings)
	child, err := m.store.CreatePRD(spec, prd.ProjectDir, prd.Backend, prd.Model, prd.Effort)
	if err != nil {
		return nil, err
	}
	child.ParentPRDID = prdID
	if err := m.store.SavePRD(child); err != nil {
		return nil, err
	}
	return child, nil
}

// ProposeRuleEdits calls the rule-editor LLM to suggest AGENT.md changes
// based on the latest scan findings for prdID.
func (m *Manager) ProposeRuleEdits(prdID string) (string, error) {
	m.mu.Lock()
	fn := m.ruleEditorFn
	m.mu.Unlock()
	if fn == nil {
		return "", fmt.Errorf("rule editor not configured (set rules_grader_enabled + ask-compatible backend)")
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return "", fmt.Errorf("prd %q not found", prdID)
	}
	v, hasScan := m.scanResults.Load(prdID)
	if !hasScan {
		return "", fmt.Errorf("no scan result for prd %q; run scan first", prdID)
	}
	result, _ := v.(*scan.Result)
	if result == nil || len(result.Findings) == 0 {
		return "", fmt.Errorf("no findings to propose rules for in prd %q", prdID)
	}
	return fn(result.Findings, prd.ProjectDir)
}

// ── BL221 (v6.2.0) Phase 4 — type registry, Guided Mode, skills ──────────

// ListTypes returns the current type registry (builtin + operator-registered).
func (m *Manager) ListTypes() []AutomatonType {
	m.typesMu.Lock()
	defer m.typesMu.Unlock()
	out := make([]AutomatonType, len(m.types))
	copy(out, m.types)
	return out
}

// RegisterType adds or replaces a type in the registry. ID must be non-empty.
func (m *Manager) RegisterType(t AutomatonType) error {
	if t.ID == "" {
		return fmt.Errorf("type id required")
	}
	m.typesMu.Lock()
	defer m.typesMu.Unlock()
	for i, existing := range m.types {
		if existing.ID == t.ID {
			m.types[i] = t
			return nil
		}
	}
	m.types = append(m.types, t)
	return nil
}

// SetPRDType updates the Type field on a PRD (hot-path, no executor needed).
func (m *Manager) SetPRDType(prdID, typ string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	prd.Type = typ
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetPRDGuidedMode toggles Guided Mode on a PRD.
func (m *Manager) SetPRDGuidedMode(prdID string, guided bool) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	prd.GuidedMode = guided
	// BL406 Phase 5 — B111's real fix. migrateGuidedMode applies this same
	// rule at store-load time; applying it live here too means toggling
	// the checkbox actually gates tasks immediately, not only after the
	// next daemon restart reloads the PRD from disk.
	if guided {
		if prd.GuidedModeSource == "" {
			prd.GuidedModeSource = GuidedModeOperator
		}
	} else {
		prd.GuidedModeSource = ""
	}
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetPRDSkills replaces the skills list on a PRD.
func (m *Manager) SetPRDSkills(prdID string, skills []string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	prd.Skills = skills
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetPRDConcurrency (BL370) sets the per-PRD max_concurrent_tasks override.
// n <= 0 clears the override (falls back to global config).
func (m *Manager) SetPRDConcurrency(prdID string, n int) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	if n < 0 {
		n = 0
	}
	prd.MaxConcurrentTasks = n
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetPRDContinueOnStoryFailure sets the per-PRD override for whether the
// executor continues into later, independent stories after one story
// fails. nil would mean "inherit the daemon default" but a REST/MCP call
// always supplies an explicit true/false, so this always sets an override.
func (m *Manager) SetPRDContinueOnStoryFailure(prdID string, continueOnFailure bool) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.ContinueOnStoryFailure = &continueOnFailure
	prd.UpdatedAt = time.Now()
	return prd, m.store.SavePRD(prd)
}

// SetPRDNotifyEvents sets the per-Automaton notify_events list — the
// lifecycle event kinds that produce notifications for this Automaton.
// A nil or empty list clears the field (inherit from the next tier:
// the assigned guardrail profile, then the global default set).
// An invalid entry returns ValidateNotifyEvents' error unchanged so
// callers can show the operator the valid kinds. A stored value that
// actually changed writes one autonomous_notify_events_changed audit
// entry (the single record for this setting); a no-change write writes
// none.
func (m *Manager) SetPRDNotifyEvents(prdID string, kinds []string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := ValidateNotifyEvents(kinds); err != nil {
		return nil, err
	}
	oldList := prd.NotifyEvents
	next := NormalizeNotifyEvents(kinds)
	changed := strings.Join(oldList, ",") != strings.Join(next, ",")
	prd.NotifyEvents = next
	prd.UpdatedAt = time.Now()
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	if changed && m.auditFn != nil {
		sourceAfter := m.ResolveNotifyEvents(prd).Source
		m.auditFn(audit.ActionNotifyEventsChanged, map[string]any{
			"scope":        "automaton",
			"prd_id":       prd.ID,
			"old":          strings.Join(oldList, ","),
			"new":          strings.Join(next, ","),
			"source_after": sourceAfter,
		})
	}
	return prd, nil
}

// SetPRDGit (BL407 Phase 5) sets the per-PRD Git.AutoPR/BaseBranch
// overrides. Both fields are set unconditionally on every call (same
// shape as SetPRDGuardrails' multi-field replace) — a caller wanting
// to change only one of the two reads the current PRD first, same as
// every other multi-field setter in this file. Branch/URL/Provider/
// PRURL are read-only here: they're runtime state the executor and
// git-completion hooks own, not operator-settable config.
func (m *Manager) SetPRDGit(prdID string, autoPR bool, baseBranch string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.Git.AutoPR = autoPR
	prd.Git.BaseBranch = baseBranch
	prd.UpdatedAt = time.Now()
	return prd, m.store.SavePRD(prd)
}

// SetPRDPriority sets the PRD's capacity-queue priority.
func (m *Manager) SetPRDPriority(prdID string, n int) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	prd.Priority = n
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// kindSetAttempts bounds how often SetPRDKind retries after another writer
// changed the record between its read of the revision and its commit.
const kindSetAttempts = 3

// SetPRDKind sets the explicit kind of an Automaton. An empty kind clears it,
// so the kind is derived again at read time. An unknown kind is refused. The
// change is committed only if the record kept the revision it was read at, and
// is logged on the decision timeline.
func (m *Manager) SetPRDKind(prdID, kind string) error {
	k, ok := NormalizePRDKind(kind)
	if !ok {
		return fmt.Errorf("unknown Automaton kind %q: use feature, bug or other", strings.TrimSpace(kind))
	}
	for attempt := 0; attempt < kindSetAttempts; attempt++ {
		rev, found := m.store.PRDRev(prdID)
		if !found {
			return fmt.Errorf("prd %q not found", prdID)
		}
		err := m.store.CommitPRDIfRev(prdID, rev, func(p *PRD) bool {
			from := p.Kind
			if from == "" {
				from = "derived"
			}
			to := k
			if to == "" {
				to = "derived"
			}
			p.Kind = k
			p.Decisions = append(p.Decisions, Decision{
				At: time.Now(), Kind: "set_kind", Actor: "operator",
				Note: fmt.Sprintf("kind %s -> %s", from, to),
			})
			return true
		})
		if !errors.Is(err, ErrPRDStale) {
			return err
		}
	}
	return ErrPRDStale
}

// SetPRDDirs sets the per-PRD read-only and writable directory scope. Nil
// leaves a list unchanged; an empty non-nil slice clears it.
func (m *Manager) SetPRDDirs(prdID string, readDirs, writeDirs []string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	if err := ValidateScopeDirs(readDirs); err != nil {
		return err
	}
	if err := ValidateScopeDirs(writeDirs); err != nil {
		return err
	}
	if readDirs != nil {
		prd.ReadDirs = cleanDirs(readDirs)
	}
	if writeDirs != nil {
		prd.WriteDirs = cleanDirs(writeDirs)
	}
	prd.ScopeWarnings = m.planScopeWarnings(prd, prd.Story)
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetPRDQualityGates (BL367) sets per-PRD quality gate config.
func (m *Manager) SetPRDQualityGates(prdID string, enabled bool, testCommand string, timeout int, blockOnRegression bool) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.QualityGates = &pipeline.QualityGateConfig{
		Enabled:           enabled,
		TestCommand:       testCommand,
		Timeout:           timeout,
		BlockOnRegression: blockOnRegression,
	}
	// Re-validate whenever the gate command changes: run it once as a
	// baseline in the PRD's project dir (skipped when the PRD has no dir)
	// and record the verdict. An invalid baseline is stored with its plain
	// reason and logs a warning; it never fails this call or blocks work.
	if status, reason := m.checkGateBaseline(prd.ProjectDir, *prd.QualityGates); status == "invalid" {
		prd.QualityGateStatus = "invalid"
		prd.QualityGateInvalidReason = reason
		prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
			At:    time.Now(),
			Kind:  "quality_gate_invalid",
			Actor: "operator",
			Note:  reason,
		}))
		log.Printf("[autonomous] prd=%s WARNING: quality-gate baseline invalid (%s) — gate will warn only, never block a task", prdID, reason)
	} else {
		// "valid" (or nothing to check / no dir): drop any stale verdict.
		prd.QualityGateStatus = ""
		prd.QualityGateInvalidReason = ""
	}
	prd.UpdatedAt = time.Now()
	return prd, m.store.SavePRD(prd)
}

// SetPRDGuardrails (BL303 S2 T06; BL418 adds perPRD) sets per-Automaton
// guardrail overrides. blockOnFileDrift is assigned as-is like the
// other overrides here (nil = inherit profile/global).
func (m *Manager) SetPRDGuardrails(prdID, profile string, perTask, perStory, perPRD []string, blockOnFileDrift *bool) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.GuardrailProfile = profile
	prd.PerTaskGuardrails = perTask
	prd.PerStoryGuardrails = perStory
	prd.PerPRDGuardrails = perPRD
	prd.BlockOnFileDrift = blockOnFileDrift
	prd.UpdatedAt = time.Now()
	return prd, m.store.SavePRD(prd)
}

// CreateGuardrailProfile (BL303 S2 T05) creates a new guardrail profile.
// notifyEvents is validated (an invalid list returns the validation error
// and creates nothing) and normalized; a nil or empty list clears the
// field (inherit from the global default set).
func (m *Manager) CreateGuardrailProfile(name, description string, guardrails []string, blockOnFileDrift *bool, notifyEvents []string) (*GuardrailProfile, error) {
	if name == "" {
		return nil, fmt.Errorf("profile name is required")
	}
	if err := ValidateNotifyEvents(notifyEvents); err != nil {
		return nil, err
	}
	p := &GuardrailProfile{
		ID:               newProfileID(),
		Name:             name,
		Description:      description,
		Guardrails:       guardrails,
		BlockOnFileDrift: blockOnFileDrift,
		NotifyEvents:     NormalizeNotifyEvents(notifyEvents),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	return p, m.store.SaveGuardrailProfile(p)
}

// UpdateGuardrailProfile (BL303 S2 T05) updates an existing profile.
// A nil blockOnFileDrift leaves the profile's current value unchanged.
// notifyEvents nil means "leave unchanged"; any non-nil slice (even
// empty) replaces the stored list, where empty clears it to inherit.
// An invalid list returns the validation error and changes nothing.
// A change to the stored list writes one autonomous_notify_events_changed
// audit entry with scope "profile"; a no-change write writes none.
func (m *Manager) UpdateGuardrailProfile(id, name, description string, guardrails []string, blockOnFileDrift *bool, notifyEvents []string) (*GuardrailProfile, error) {
	p, ok := m.store.GetGuardrailProfile(id)
	if !ok {
		return nil, fmt.Errorf("guardrail profile %q not found", id)
	}
	eventsChanged := false
	oldEvents := p.NotifyEvents
	newEvents := p.NotifyEvents
	if notifyEvents != nil {
		if err := ValidateNotifyEvents(notifyEvents); err != nil {
			return nil, err
		}
		newEvents = NormalizeNotifyEvents(notifyEvents)
		eventsChanged = strings.Join(oldEvents, ",") != strings.Join(newEvents, ",")
		p.NotifyEvents = newEvents
	}
	if name != "" {
		p.Name = name
	}
	p.Description = description
	p.Guardrails = guardrails
	if blockOnFileDrift != nil {
		p.BlockOnFileDrift = blockOnFileDrift
	}
	p.UpdatedAt = time.Now()
	if err := m.store.SaveGuardrailProfile(p); err != nil {
		return nil, err
	}
	if eventsChanged && m.auditFn != nil {
		m.auditFn(audit.ActionNotifyEventsChanged, map[string]any{
			"scope":      "profile",
			"profile_id": id,
			"old":        strings.Join(oldEvents, ","),
			"new":        strings.Join(newEvents, ","),
		})
	}
	return p, nil
}

// DeleteGuardrailProfile (BL303 S2 T05) removes a profile.
func (m *Manager) DeleteGuardrailProfile(id string) error {
	return m.store.DeleteGuardrailProfile(id)
}

// ListGuardrailProfiles (BL303 S2 T05) returns all profiles.
func (m *Manager) ListGuardrailProfiles() []*GuardrailProfile {
	return m.store.ListGuardrailProfiles()
}

// CreateRetryProfile (BL410 Phase 1) creates a new retry profile.
func (m *Manager) CreateRetryProfile(name, description string, budgets map[string]int, ceilingTotal, infraRetryEscalateAfter int, contentAutoFixEnabled, autoFixSkipApproval *bool) (*RetryProfile, error) {
	if name == "" {
		return nil, fmt.Errorf("profile name is required")
	}
	p := &RetryProfile{
		ID:                      newProfileID(),
		Name:                    name,
		Description:             description,
		Budgets:                 budgets,
		CeilingTotal:            ceilingTotal,
		InfraRetryEscalateAfter: infraRetryEscalateAfter,
		ContentAutoFixEnabled:   contentAutoFixEnabled,
		AutoFixSkipApproval:     autoFixSkipApproval,
		CreatedAt:               time.Now(),
		UpdatedAt:               time.Now(),
	}
	return p, m.store.SaveRetryProfile(p)
}

// UpdateRetryProfile (BL410 Phase 1) updates an existing retry profile.
func (m *Manager) UpdateRetryProfile(id, name, description string, budgets map[string]int, ceilingTotal, infraRetryEscalateAfter int, contentAutoFixEnabled, autoFixSkipApproval *bool) (*RetryProfile, error) {
	p, ok := m.store.GetRetryProfile(id)
	if !ok {
		return nil, fmt.Errorf("retry profile %q not found", id)
	}
	if name != "" {
		p.Name = name
	}
	p.Description = description
	p.Budgets = budgets
	p.CeilingTotal = ceilingTotal
	p.InfraRetryEscalateAfter = infraRetryEscalateAfter
	p.ContentAutoFixEnabled = contentAutoFixEnabled
	p.AutoFixSkipApproval = autoFixSkipApproval
	p.UpdatedAt = time.Now()
	return p, m.store.SaveRetryProfile(p)
}

// DeleteRetryProfile (BL410 Phase 1) removes a retry profile.
func (m *Manager) DeleteRetryProfile(id string) error {
	return m.store.DeleteRetryProfile(id)
}

// ListRetryProfiles (BL410 Phase 1) returns all retry profiles.
func (m *Manager) ListRetryProfiles() []*RetryProfile {
	return m.store.ListRetryProfiles()
}

// GetRetryProfile (BL410 Phase 1) returns one retry profile by id.
func (m *Manager) GetRetryProfile(id string) (*RetryProfile, bool) {
	return m.store.GetRetryProfile(id)
}

// SetPRDRetryPolicy (BL410 Phase 1) sets per-Automaton retry-policy
// overrides, mirroring SetPRDGuardrails' shape exactly.
func (m *Manager) SetPRDRetryPolicy(prdID, profile string, policy map[string]int, ceilingTotal int, contentAutoFixEnabled, autoFixSkipApproval *bool) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.RetryProfile = profile
	prd.RetryPolicy = policy
	prd.RetryCeilingTotal = ceilingTotal
	prd.ContentAutoFixEnabled = contentAutoFixEnabled
	prd.AutoFixSkipApproval = autoFixSkipApproval
	prd.UpdatedAt = time.Now()
	return prd, m.store.SavePRD(prd)
}

// GetGuardrailProfile (BL303 S2 T05) returns one profile by ID.
func (m *Manager) GetGuardrailProfile(id string) (*GuardrailProfile, bool) {
	return m.store.GetGuardrailProfile(id)
}
