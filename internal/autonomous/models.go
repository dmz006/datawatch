// Package autonomous (BL24 + BL25, Sprint S6 → v3.10.0) implements
// LLM-driven PRD → Stories → Tasks decomposition with independent
// verification (BL25). Models are deliberately close to nightwire's
// Pydantic schema (HackingDave/nightwire/nightwire/autonomous/models.py)
// so cross-tool memory + SIEM ingestion can interoperate.

package autonomous

import (
	"time"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// Status enums — kept in sync with nightwire for cross-tool compat.

type PRDStatus string

const (
	PRDDraft          PRDStatus = "draft"
	PRDPlanning       PRDStatus = "planning"        // BL221 (v6.2.0) — Decompose call in flight; renamed from PRDDecomposing
	PRDDecomposing              = PRDPlanning       // back-compat alias; old stored value "decomposing" normalized on load
	PRDNeedsReview    PRDStatus = "needs_review"    // BL191 — decomposed; awaiting operator review/edit
	PRDApproved       PRDStatus = "approved"        // BL191 — operator approved; Run is allowed
	PRDRevisionsAsked PRDStatus = "revisions_asked" // BL191 — operator requested re-decomposition
	PRDActive         PRDStatus = "active"          // legacy alias kept for back-compat with v5.1.x stores; new code uses PRDRunning
	PRDRunning        PRDStatus = "running"         // BL191 — Run is in flight
	PRDCompleted      PRDStatus = "completed"
	PRDRejected       PRDStatus = "rejected"  // BL191 — operator rejected the decomposition; PRD is dead
	PRDCancelled      PRDStatus = "cancelled" // BL191 — operator cancelled an in-flight Run
	PRDArchived       PRDStatus = "archived"
	PRDBlocked        PRDStatus = "blocked" // BL191 Q6 — a guardrail returned `block`; awaits operator action
	PRDFailed         PRDStatus = "failed"  // one or more tasks failed after all retries; PRD did not complete
	PRDPaused         PRDStatus = "paused"  // GH#172 D52 — operator paused an in-flight Run; in-progress tasks finish, no new ones dispatch
)

// NormalizePRDStatus maps legacy stored status values to current constants.
// Call on every PRD loaded from disk; safe to call on already-current values.
func NormalizePRDStatus(s PRDStatus) PRDStatus {
	if s == "decomposing" {
		return PRDPlanning
	}
	return s
}

// Decision (BL191 Q3, v5.2.0) is one entry in the per-PRD audit log
// of LLM calls + verifier verdicts. Append-only. Surfaced via
// GET /api/autonomous/prds/{id}/decisions for the operator timeline,
// and mirrored into the security-grade audit log for compliance.
type Decision struct {
	At             time.Time `json:"at"`
	Kind           string    `json:"kind"` // decompose | run | verify | approve | reject | request_revision | edit_task | template_instantiate | ci_watch | boot_reconcile | verifying_resolution | boot_resume_parked | resume | failure_escalation | review_needed | operator_decision | operator_answer | decision_gate_hold | decision_gate_shadow
	Backend        string    `json:"backend,omitempty"`
	Model          string    `json:"model,omitempty"`
	PromptChars    int       `json:"prompt_chars,omitempty"`
	ResponseChars  int       `json:"response_chars,omitempty"`
	CostUSD        float64   `json:"cost_usd,omitempty"`
	VerdictOutcome string    `json:"verdict_outcome,omitempty"` // pass | warn | block | n/a
	Actor          string    `json:"actor,omitempty"`           // operator | autonomous | verifier
	Note           string    `json:"note,omitempty"`            // free-form: what was edited, why rejected, etc.
	// JudgeSessionID is the id of the one-shot session that produced a
	// guardrail verdict. Empty when the judgment was a text-only call or
	// a deterministic check, so the audit entry links back to the judge
	// run only when one existed.
	JudgeSessionID string `json:"judge_session_id,omitempty"`
	// FailureCategory (BL410 Phase 1) mirrors FailureReason.Category —
	// visible inline in this append-only log without opening a nested
	// Verdict/Verification struct.
	FailureCategory string `json:"failure_category,omitempty"`
	// QuestionID, AnswerID and DecisionState link an operator_decision or
	// operator_answer entry to the operator question it records: the
	// question id, the answer id (answers only) and the question state the
	// entry stands for (open | answered | withdrawn | expired).
	QuestionID    string `json:"question_id,omitempty"`
	AnswerID      string `json:"answer_id,omitempty"`
	DecisionState string `json:"decision_state,omitempty"`
	// MergedMember carries the structured proof of a merged_member entry
	// written by the Automation coordinator; nil on every other kind.
	MergedMember *MergedMemberDecision `json:"merged_member,omitempty"`
}

// GateDisagreement is the record of a verifier/quality-gate disagreement
// (verifier passed, quality gate disagrees), captured on the task by the
// executor under the autonomous.gate_disagreement policy:
//
//   - VerifierOK is always true when this field is set — a disagreement
//     only exists because the verifier passed.
//   - GateRegression is true for a real regression from a VALID baseline;
//     gate_disagreement=block fails the task in this case.
//   - GateInvalid is true when the gate has no usable baseline; an invalid
//     gate NEVER blocks under any policy.
//   - Policy is the effective gate_disagreement policy in force ("warn"|
//     "block"); Summary is the gate's own one-line summary/regression reason.
//
// Nil (absent in JSON) when no disagreement was recorded or quality gates
// were not running. Mutually exclusive with FailureReason in intent: this
// can be present on a completed task (warn policy) and must not read as a
// failure.
type GateDisagreement struct {
	VerifierOK     bool   `json:"verifier_ok"`
	GateRegression bool   `json:"gate_regression"`
	GateInvalid    bool   `json:"gate_invalid"`
	Policy         string `json:"policy"`            // "warn" or "block" as in force
	Summary        string `json:"summary,omitempty"` // gate summary / invalid reason
}

// verifyingResolution is the Decision.Kind for the boot-time audit entry
// appended to prd.Decisions whenever an orphaned TaskVerifying /
// TaskRunningTests task is resolved on daemon restart — either completed from
// a recorded ok verification verdict or re-queued for executor retry. The
// Note names how many of each occurred for that PRD. Unexported: the
// decision kind is internal to the autonomous manager; the string literal is
// the wire value persisted in the decision log.
const verifyingResolution = "verifying_resolution"

type StoryStatus string

const (
	StoryPending          StoryStatus = "pending"
	StoryAwaitingApproval StoryStatus = "awaiting_approval" // Phase 3 (v5.26.60)
	StoryInProgress       StoryStatus = "in_progress"
	StoryCompleted        StoryStatus = "completed"
	StoryBlocked          StoryStatus = "blocked"
	StoryFailed           StoryStatus = "failed"
	StoryCancelled        StoryStatus = "cancelled" // BL382 — operator-cancelled via cancel_story
)

// Task-kind constants (2026-10-17, smoke-verification story): "smoke" marks a task that IS the smoke
// verification (its verification script is the plan's smoke suite), "final_gate" the terminal task; empty = ordinary.
const (
	TaskKindSmoke     = "smoke"
	TaskKindFinalGate = "final_gate"
)

type TaskStatus string

const (
	TaskPending      TaskStatus = "pending"
	TaskQueued       TaskStatus = "queued"
	TaskInProgress   TaskStatus = "in_progress"
	TaskRunningTests TaskStatus = "running_tests"
	TaskVerifying    TaskStatus = "verifying"
	TaskCompleted    TaskStatus = "completed"
	TaskFailed       TaskStatus = "failed"
	TaskBlocked      TaskStatus = "blocked"
	TaskCancelled    TaskStatus = "cancelled"
	// TaskWaitingCapacity: admitted by the executor but waiting for a capacity
	// lease (host / node / LLM slot). Not a failure and not a retry.
	TaskWaitingCapacity TaskStatus = "waiting_capacity"
	// TaskWaitingDecision: the task's spec names an operator decision that is
	// not answered (the decision gate, decision_gate_hold.go). It has not
	// started. It is neither a failure nor a retry, and it returns to pending
	// by itself once every decision the spec names is answered.
	TaskWaitingDecision TaskStatus = "waiting_decision"
	// TaskAwaitingApproval (BL406 Phase 5 — B111's real fix): set when
	// PRD.GuidedModeSource is "operator" and the task would otherwise
	// be ready to launch. The executor's flattenTasks skips tasks in
	// this state (same mechanism StoryAwaitingApproval already uses at
	// story granularity); ApproveTask transitions it back to pending.
	TaskAwaitingApproval TaskStatus = "awaiting_approval"
	// TaskReviewNeeded: the task failed enough attempts (the failure
	// escalation policy's max_failures) that it is held for the operator to
	// retry or cancel it. It is not terminal and not a failure; see
	// failure_escalation.go and ResolveTaskReview.
	TaskReviewNeeded TaskStatus = "review_needed"
)

// Effort mirrors session.EffortLevels (BL41) but kept separate so
// nightwire's "max" tier is available for autonomous mode without
// polluting the per-session table.
type Effort string

const (
	EffortLow      Effort = "low"
	EffortMedium   Effort = "medium"
	EffortHigh     Effort = "high"
	EffortMax      Effort = "max"
	EffortQuick    Effort = "quick"    // session.EffortLevels alias
	EffortNormal   Effort = "normal"   // session.EffortLevels alias
	EffortThorough Effort = "thorough" // session.EffortLevels alias
)

// UncommittedWorkHold records why a task or an Automaton is held on files git
// still sees as uncommitted in the Automaton worktree. Kind is "task" or
// "worktree". Policy is the uncommitted-work policy in force when the hold was
// raised. Files lists the held paths and OutsideScope the subset the task did
// not declare. QuestionID names the operator question raised for the hold; it
// is empty when no question store was wired.
type UncommittedWorkHold struct {
	Kind         string    `json:"kind"`
	Policy       string    `json:"policy,omitempty"`
	Files        []string  `json:"files,omitempty"`
	OutsideScope []string  `json:"outside_scope,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	QuestionID   string    `json:"question_id,omitempty"`
	HeldAt       time.Time `json:"held_at"`
}

// PRD is the operator-supplied feature description.
type PRD struct {
	// Truncated is set only on the capped copy the default detail GET
	// returns (see CapPRDText). It is never stored.
	Truncated  []TruncatedField `json:"truncated,omitempty"`
	ID         string           `json:"id"`   // 8-hex
	Spec       string           `json:"spec"` // raw operator input
	Title      string           `json:"title,omitempty"`
	ProjectDir string           `json:"project_dir"`
	// v5.26.19 — operator-reported: "Prd should be based on directory or
	// profile, should be able to check out repo and do work" + "Prd
	// should also support using cluster profiles". When ProjectProfile
	// is set, the executor resolves the F10 project profile to a git
	// URL + branch and clones into a worker-side workspace; cluster
	// profile dispatches the worker to /api/agents (cluster spawn)
	// instead of /api/sessions/start (local tmux). Either or both can
	// be set; ProjectDir is still honored when profiles are empty.
	ProjectProfile string `json:"project_profile,omitempty"`
	ClusterProfile string `json:"cluster_profile,omitempty"`
	// Git (BL407 Phase 1) — branch-per-PRD + auto-PR workflow state.
	// When ProjectDir/ProjectProfile/ClusterProfile are all empty and
	// the daemon has autonomous.Config.WorktreeBaseRepo configured,
	// Run() creates a git worktree for this PRD and populates
	// ProjectDir + Git.Branch automatically — the PRD never has to run
	// directly in the base repo's own checkout. See
	// docs/plans/2026-10-10-prd-git-workflow.md.
	Git GitWorkflow `json:"git,omitempty"`
	// Phase 3 (v5.26.60) — explicit planning profile distinct
	// from the default execution profile. Empty falls back to the
	// global autonomous.planning_backend config knob (BL304). The
	// existing ProjectProfile field is re-purposed as the *default
	// execution profile* for stories that don't override.
	DecompositionProfile string `json:"decomposition_profile,omitempty"`
	DecompositionModel   string `json:"decomposition_model,omitempty"` // model override for the planning/decompose backend
	Backend              string `json:"backend,omitempty"`             // PRD-level worker LLM (default for tasks; tasks override per-task)
	Effort               Effort `json:"effort,omitempty"`
	Model                string `json:"model,omitempty"` // BL203 (v5.4.0) — PRD-level model name (e.g., "claude-3-5-sonnet"); tasks may override
	// BL416 Phase 1 — PRD-level guardrail LLM override. Empty = inherit
	// the global autonomous verification_backend/verification_model
	// (existing config; no new global key). Tasks may override via
	// Task.GuardrailBackend/GuardrailModel. Schema-only today: not yet
	// used for guardrail routing.
	GuardrailBackend string `json:"guardrail_backend,omitempty"`
	GuardrailModel   string `json:"guardrail_model,omitempty"`
	// Automaton-level verifier LLM override. Precedence:
	// task → story → Automaton (here) → global autonomous
	// verification_backend/verification_model/verification_effort.
	// Backend and model resolve from the same level (see
	// ResolveVerificationLLM); effort resolves independently. Empty =
	// inherit the global config. Independent of the worker
	// Backend/Model and of GuardrailBackend/GuardrailModel.
	VerificationBackend string `json:"verification_backend,omitempty"`
	VerificationModel   string `json:"verification_model,omitempty"`
	VerificationEffort  Effort `json:"verification_effort,omitempty"`
	// VerifierPolicy overrides the global verifier policy mode and backend
	// list for this Automaton. Precedence: task → Automaton (here) → global
	// autonomous.verifier_policy. Nil = inherit. See ResolveVerifierPolicy.
	VerifierPolicy *VerifierPolicyOverride `json:"verifier_policy,omitempty"`
	// FailureEscalation overrides the global failure escalation policy for
	// every task of this Automaton. Precedence per field: task → Automaton
	// (here) → global autonomous.failure_escalation. Nil = inherit. See
	// resolveFailureEscalation.
	FailureEscalation *FailureEscalationPolicy `json:"failure_escalation,omitempty"`
	// LSPLanguage (v8.7.1) — language server preset for OpenCode-backed workers.
	// Applies to all tasks unless overridden at the task level.
	LSPLanguage string `json:"lsp_language,omitempty"`
	// PermissionMode (v5.27.5) — claude-code permission-mode flag
	// applied to every task in this PRD that doesn't override it.
	// Set to "plan" to make the whole PRD a design-only walk
	// (claude won't write files); leave empty to use the session
	// default. Per-task value takes precedence.
	PermissionMode string `json:"permission_mode,omitempty"`
	// ThinkingLevel — pi --thinking level override (off|minimal|low|
	// medium|high) applied to every task whose Task.ThinkingLevel is
	// empty. Empty = fall through to the global pi.thinking_level
	// config. Resolution order: task -> story -> PRD -> config.
	ThinkingLevel string    `json:"thinking_level,omitempty"`
	Status        PRDStatus `json:"status"`
	// BlockedReason is a plain sentence saying why the automaton is blocked:
	// which guardrail verdict blocked it and which judge produced it, or (B165)
	// why the boot-resume gate parked it (stale activity, removed project
	// directory, resume disabled). Empty unless Status is blocked.
	BlockedReason string `json:"blocked_reason,omitempty"`
	// WaitReason is a plain sentence saying why the PRD's planner is queued
	// on capacity (which pool is full) while a decompose waits for admission.
	// Same convention as Task.WaitReason for waiting tasks; empty unless the
	// planner is (or was) waiting. Set and cleared by
	// setPRDWaitReason/clearPRDWaitReason so it never outlives the wait.
	WaitReason string `json:"wait_reason,omitempty"`
	// UncommittedWork — set while the Automaton's worktree holds uncommitted
	// files that no single task owns (see holdAutomatonForUncommittedWork).
	UncommittedWork *UncommittedWorkHold `json:"uncommitted_work,omitempty"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
	// BL451 — optimistic-concurrency revision, bumped by Store.SavePRD
	// (and Store.CommitPRDIfRev) on every save. 0 for PRDs persisted
	// before BL451; load tolerates the missing field.
	Rev   int     `json:"rev,omitempty"`
	Story []Story `json:"stories,omitempty"` // populated after Decompose

	// BL191 Q1 (v5.2.0) — review/approve gate. Manager.Run refuses
	// unless Status == PRDApproved (or the legacy PRDActive for back-
	// compat with pre-v5.2.0 stores). Operators transition via
	// POST /api/autonomous/prds/{id}/{approve|reject|request_revision}.
	ApprovedBy         string     `json:"approved_by,omitempty"`
	ApprovedAt         *time.Time `json:"approved_at,omitempty"`
	RejectionReason    string     `json:"rejection_reason,omitempty"`
	RevisionsRequested int        `json:"revisions_requested,omitempty"` // count of request_revision calls

	// BL191 Q3 — append-only per-PRD audit timeline.
	Decisions []Decision `json:"decisions,omitempty"`

	// PlanReview (2026-10-17 plan-review gate) — the latest pre-approval
	// plan-review verdict for this PRD (see plan_review.go). Nil when the
	// configured mode is off (default) or no review has been recorded; the
	// latest verdict wins — re-run (ReviewPlan) or re-decompose replaces it.
	PlanReview *PlanReviewVerdict `json:"plan_review,omitempty"`

	// PRD-completion rules review (B124 extension, 2026-10-12) —
	// judgment verdicts at the whole-PRD level, same shape Story/Task
	// already carry for their own guardrails (see Story.Verdicts' doc
	// comment). Populated by runPRDCompletionRulesReview: a tool-using
	// session reads this project's own configured rules file
	// (default AGENT.md/CONTEXT.md) and checks the PRD's real changes
	// against it — not a fixed checklist of specific items, since
	// every project's rules file is different; the LLM decides what's
	// relevant to what actually changed. Deliberately NOT scripted
	// (operator decision, 2026-10-12): a deterministic check would
	// have to hardcode this project's own rule content, which breaks
	// for any other operator's repo and rule set. The existing
	// deterministic prd_complete project-rules scan
	// (runPRDCompletionProjectRulesCheck) stays for the few items that
	// ARE safely pattern-matchable (e.g. version-string equality);
	// this is the judgment-based complement for everything else.
	Verdicts []GuardrailVerdict `json:"verdicts,omitempty"`

	// JudgeTries holds the timed-out judge attempts of judge calls that are
	// still running for this PRD (see judge_tries.go). A daemon restart reads
	// them back, so a judge never retries a backend that already timed out
	// for the same call. Cleared when the call finishes; the summary then
	// lives on the verdict.
	JudgeTries []JudgeTry `json:"judge_tries,omitempty"`

	// BL191 Q2 — template flag + variable schema. When IsTemplate is
	// true the PRD is a reusable shape rather than an executable plan;
	// instantiating with POST /api/autonomous/prds/{id}/instantiate
	// substitutes {{var}} markers in spec/title/task specs and stores
	// a fresh executable PRD.
	IsTemplate   bool          `json:"is_template,omitempty"`
	TemplateVars []TemplateVar `json:"template_vars,omitempty"`
	TemplateOf   string        `json:"template_of,omitempty"` // ID of the source template; only set on instantiated PRDs

	// BL191 Q4 (v5.9.0) — recursion. ParentPRDID is the PRD whose task
	// spawned this one. ParentTaskID is that specific task. Depth is
	// the recursion depth (root PRDs are 0). Manager.recurse refuses to
	// spawn a child when Parent.Depth + 1 > Config.MaxRecursionDepth so
	// a runaway decomposition can't tank the daemon.
	ParentPRDID  string `json:"parent_prd_id,omitempty"`
	ParentTaskID string `json:"parent_task_id,omitempty"`
	Depth        int    `json:"depth,omitempty"`

	// AutomationID is the Automation this Automaton is currently a member of (empty = standalone). Maintained by the Automation store; nothing reads it for execution yet.
	AutomationID string `json:"automation_id,omitempty"`

	// BL221 (v6.2.0) Phase 4 — type extensibility, Guided Mode, skills.
	// Type is a well-known or operator-registered automaton type identifier
	// (e.g., "software", "research", "operational", "personal").
	// Skills is the list of skill IDs assigned to this automaton; passed to
	// spawn requests so workers can load the appropriate skill context.
	Type string `json:"type,omitempty"`
	// Kind classifies the work as "feature", "bug" or "other"; empty means
	// derive at read time (old records keep working). See EffectiveKind.
	Kind   string   `json:"kind,omitempty"`
	Skills []string `json:"skills,omitempty"`
	// LanePreference says which kind of lane suits this Automaton: "gpu" or
	// "claude". Empty means any lane. See NormalizeLanePreference.
	LanePreference string `json:"lane_preference,omitempty"`
	// TagIDs lists the managed Tag IDs assigned to this automaton.
	TagIDs []string `json:"tag_ids,omitempty"`
	// PrimaryTagID is an optional tag from TagIDs. A tag browser uses it only
	// to sort the Automaton; it never hides a membership. Empty = none.
	PrimaryTagID string `json:"primary_tag_id,omitempty"`

	// GuidedMode (BL221 v6.2.0) was originally documented as pausing
	// before each task — confirmed never implemented (B111, found
	// 2026-10-10: full set-side REST/MCP/CLI/comm plumbing existed, but
	// nothing anywhere ever read the flag). Kept for JSON backward
	// compatibility with existing persisted PRDs; no longer the real
	// field. GuidedModeSource (BL406 Phase 5) is the real one: ""
	// (guided mode off), "operator" (pause before each task, waits for
	// ApproveTask — the original intent, now actually built),
	// "council" (pause and dispatch to a CouncilProfile for a
	// consensus go/no-go — not yet implemented, BL405 Phase 8 is the
	// dependency; falls back to "operator" with a logged reason rather
	// than silently skipping the gate), "guardrail_auto" (continue
	// automatically whenever quality gates + project rules already
	// pass — behaviorally identical to leaving this empty; declared
	// explicitly for operators who want to say so on purpose, not new
	// executor behavior). Migrated on read: an existing PRD with
	// guided_mode:true and an empty GuidedModeSource is treated as
	// "operator" (see migrateGuidedMode) — never silently drops an
	// operator's prior intent, even though it never actually worked.
	GuidedMode       bool   `json:"guided_mode,omitempty"`
	GuidedModeSource string `json:"guided_mode_source,omitempty"`

	// BL303 S2 — per-Automaton guardrail overrides.
	// GuardrailProfile names a profile; PerTask/PerStoryGuardrails are explicit
	// overrides. Priority: explicit > profile > global Config.
	GuardrailProfile   string   `json:"guardrail_profile,omitempty"`
	PerTaskGuardrails  []string `json:"per_task_guardrails,omitempty"`
	PerStoryGuardrails []string `json:"per_story_guardrails,omitempty"`
	// BL418 — per-Automaton override for the "prd" level (PRD-completion
	// rules review). Same explicit > profile > global Config precedence
	// as the task/story lists above.
	PerPRDGuardrails []string `json:"per_prd_guardrails,omitempty"`
	// NotifyEvents lists the lifecycle event kinds (see
	// ValidNotifyEventKinds) that produce notifications for this
	// Automaton. Empty = inherit from the next tier (profile, then
	// global default set).
	NotifyEvents []string `json:"notify_events,omitempty"`
	// NotifyEventsEffective is a computed, never-persisted read-time
	// view: the effective notification event kinds for this Automaton
	// (Manager.ResolveNotifyEvents) and the tier that supplied them.
	// The API fills it on the copy it returns for reads (GetPRD,
	// ListPRDs and the notify-events setter response) — never on the
	// stored object — and the store clears it before persisting, so a
	// saved row never carries it. Nil on stored objects.
	NotifyEventsEffective *NotifyEventSet `json:"notify_events_effective,omitempty"`

	// BL410 Phase 1 — per-Automaton retry-policy overrides. Priority:
	// explicit RetryPolicy > named RetryProfile > global Config,
	// mirroring the guardrail precedence above exactly (resolveRetryPolicy).
	RetryProfile      string         `json:"retry_profile,omitempty"`
	RetryPolicy       map[string]int `json:"retry_policy,omitempty"`
	RetryCeilingTotal int            `json:"retry_ceiling_total,omitempty"`
	// RetryCountsByCategory is the running consumption this PRD has
	// already spent, checked against resolveRetryCeiling before any
	// further auto-retry anywhere in the PRD's lifecycle.
	RetryCountsByCategory map[string]int `json:"retry_counts_by_category,omitempty"`
	// ContentAutoFixEnabled/AutoFixSkipApproval override the global
	// Config/RetryProfile default for just this PRD. nil = inherit.
	ContentAutoFixEnabled *bool `json:"content_auto_fix_enabled,omitempty"`
	AutoFixSkipApproval   *bool `json:"auto_fix_skip_approval,omitempty"`
	// BlockOnFileDrift opts this Automaton into strict foreign-file-drift
	// enforcement, overriding the GuardrailProfile/global Config default
	// (resolveBlockOnFileDrift). nil = inherit.
	BlockOnFileDrift *bool `json:"block_on_file_drift,omitempty"`
	// LastDecomposeFailure is a single-use hint seeded into the next
	// Decompose retry's prompt, mirroring Task.PriorAttemptHint's exact
	// lifecycle (set before a retry, consumed-and-cleared on use).
	LastDecomposeFailure *FailureReason `json:"last_decompose_failure,omitempty"`
	// DecomposeSessionFailures counts consecutive decompose-attempt
	// sessions that ended without writing the plan output file (see
	// OneShotNoOutputError). Reaching decomposeSessionStopThreshold stops
	// the PRD instead of spawning another session. Cleared on a successful
	// decompose and on ResetToDraft. Persisted by SavePRD, so it survives
	// a daemon restart.
	DecomposeSessionFailures int `json:"decompose_session_failures,omitempty"`
	// CIWatch is this automaton's own record of whether CI actually
	// passed for the commit it pushed, written by the ci-watch
	// completion guardrail. nil = ci-watch never ran for this PRD.
	CIWatch *CIWatchResult `json:"ci_watch,omitempty"`

	// BL331 — federation peer that originated this PRD via channel routing.
	OwnerPeer string `json:"owner_peer,omitempty"`

	// BL367 — per-PRD quality gate config. When QualityGates.Enabled is
	// true the executor captures a test baseline before the first task,
	// then re-runs tests after each task completes. New failures block the
	// task and trigger auto-fix retries (same as verifier failure). Empty
	// value inherits autonomous.default_quality_gates from daemon config.
	QualityGates *pipeline.QualityGateConfig `json:"quality_gates,omitempty"`
	// Verdict of the latest quality-gate baseline validation:
	// "valid" | "invalid" ("", with omitempty, when never checked or the
	// gates are off). An invalid baseline (command/build/environment
	// failure) is logged as a warning and NEVER blocks or retries tasks;
	// QualityGateInvalidReason carries the plain-language why.
	QualityGateStatus        string `json:"quality_gate_status,omitempty"`
	QualityGateInvalidReason string `json:"quality_gate_invalid_reason,omitempty"`

	// BL370 — per-PRD task concurrency override. When > 0 overrides
	// autonomous.max_concurrent_tasks for this specific PRD. 0 = use
	// the global default (effectively 1 = sequential).
	MaxConcurrentTasks int `json:"max_concurrent_tasks,omitempty"`

	// ContinueOnStoryFailure overrides the daemon-wide
	// autonomous.continue_on_story_failure default for this PRD. nil
	// inherits the global default (itself false unless the operator
	// opts in), which HALTS the PRD (-> PRDBlocked) as soon as a story
	// fails rather than continuing into later, independent stories.
	ContinueOnStoryFailure *bool `json:"continue_on_story_failure,omitempty"`

	// BL428 §3a ii — refuse-dirty-tree override for this PRD's tasks.
	// nil inherits the daemon-wide config.Autonomous.refuse_dirty_tree_start
	// default (resolveRefuseDirtyTree).
	RefuseDirtyTreeStart *bool `json:"refuse_dirty_tree_start,omitempty"`

	// Priority orders this PRD's tasks in the capacity wait queue (higher
	// first; default 0).
	Priority int `json:"priority,omitempty"`

	// BL386 Phase 1 — warm-start seeding. When Enabled, the executor
	// seeds each spawned task's session-local scope from project-shared,
	// prd-shared, and story-shared before the task begins work.
	MemorySeed MemorySeedConfig `json:"memory_seed,omitempty"`

	// BL386 Phase 2 — harvest on task completion. When Enabled, the executor
	// promotes session-local memories to story-shared (or prd-shared) after
	// each task reaches TaskCompleted.
	MemoryHarvest MemoryHarvestConfig `json:"memory_harvest,omitempty"`

	// BL387 Phase 3 — auto-report generated when the PRD reaches PRDCompleted.
	// MemoryReport holds the generated text (populated asynchronously).
	// MemoryReportAt is the UTC timestamp when the report was generated.
	MemoryReport   string     `json:"memory_report,omitempty"`
	MemoryReportAt *time.Time `json:"memory_report_at,omitempty"`

	// Scope boundary. WriteDirs (default: ProjectDir) are the directories
	// workers may modify; ReadDirs are extra read-only directories.
	// ScopeWarnings lists plan entries that reference paths outside them.
	ReadDirs      []string `json:"read_dirs,omitempty"`
	WriteDirs     []string `json:"write_dirs,omitempty"`
	ScopeWarnings []string `json:"scope_warnings,omitempty"`

	// PlanCoverage is the stored plan-requirement coverage state from
	// the last decomposition: the check mode and min ratio in effect
	// when the report was computed, the covered/missing/weakened
	// counts and the unmatched requirements, plus the operator
	// acknowledgement state (nil until acknowledged). Nil = no coverage
	// check has been stored for this PRD yet.
	PlanCoverage *PlanCoverage `json:"plan_coverage,omitempty"`

	// Triage is the single record a failed or blocked Automaton carries
	// while it waits for operator attention (see triage_record.go): the
	// Automaton's identity, the failed/blocked task and its error, the
	// end reason for any session that was ended, the resolved retry
	// budget, and the append-only notes later steps write to it. Present
	// only while the Automaton is stuck — written when it lands in
	// failed/blocked, refreshed (never duplicated) while it stays there,
	// cleared when it leaves. Absent (nil) in JSON otherwise, so an
	// healthy Automaton's stored row is byte-for-byte what it was before.
	Triage *TriageRecord `json:"triage,omitempty"`
}

// PlanCoverage is the plan-coverage state stored on the PRD
// (PRD.PlanCoverage). It pins the effective settings from the moment
// the report was computed (so later config changes don't silently
// re-interpret an old report) and carries the acknowledgement state.
type PlanCoverage struct {
	// Mode is the plan-coverage check mode in effect when the report
	// was computed: "off" | "warn" | "block".
	Mode string `json:"mode"`
	// MinRatio is the minimum covered/extractable requirement ratio
	// in effect when the report was computed (0,1]; 1.0 = full coverage.
	MinRatio float64 `json:"min_ratio"`
	// Report is the coverage report from CheckPlanCoverage for the
	// latest decomposition attempt.
	Report PlanCoverageReport `json:"report"`
	// Ack is the operator acknowledgement of a failing report, if one
	// exists. Nil = the report is (if failing) unacknowledged.
	Ack *PlanCoverageAck `json:"ack,omitempty"`
	// Findings is the structural-findings half of the same
	// deterministic (non-LLM) coverage check computed alongside the
	// requirements report at decompose time: any present finding
	// counts as a failing stored report in the Approve gate, and the
	// finding set participates in the Ack fingerprint (see
	// PlanCoverage.Fingerprint). Nil/empty means the stored plan had
	// no structural findings.
	Findings []StructuralFinding `json:"findings,omitempty"`
}

// PlanCoverageAck is the operator acknowledgement of one specific
// failing plan-coverage report. Fingerprint ties it to that report's
// content: a later decomposition producing a report with a different
// fingerprint no longer counts as acknowledged and must be re-acked.
type PlanCoverageAck struct {
	// By is the operator identity that acknowledged (user id or caller
	// kind, e.g. "api").
	By string `json:"by"`
	// At is the acknowledgement time (UTC).
	At *time.Time `json:"at"`
	// Note is the operator's optional free-text justification.
	Note string `json:"note,omitempty"`
	// Fingerprint is the fingerprint of the failing report this
	// acknowledgement covers (the consuming code computes it from the
	// report's failing content).
	Fingerprint string `json:"fingerprint"`
}

// GitWorkflow (BL407) is a PRD's branch-per-PRD + auto-PR workflow
// state. Provider/URL resolve from PRD.ProjectProfile's GitSpec when
// set, else from the worktree's own `origin` remote (the self-build/
// dogfooding case, where a PRD has no ProjectProfile at all).
// BaseBranch is an optional operator override; empty means "the base
// repo's current default branch" (resolved at worktree-creation
// time, not stored until then). Branch and PRURL are populated by the
// executor/completion hook — not operator-set fields.
type GitWorkflow struct {
	Provider   string `json:"provider,omitempty"`
	URL        string `json:"url,omitempty"`
	BaseBranch string `json:"base_branch,omitempty"`
	Branch     string `json:"branch,omitempty"`
	AutoPR     bool   `json:"auto_pr,omitempty"`
	PRURL      string `json:"pr_url,omitempty"`
}

// MemorySeedConfig (BL386 Phase 1 + BL387 Phase 2b) controls warm-start seeding at task spawn.
// Zero value = disabled; no behavior change from pre-BL386 code.
type MemorySeedConfig struct {
	Enabled     bool     `json:"enabled,omitempty"`
	MaxPerScope int      `json:"max_per_scope,omitempty"` // default 20 when Enabled
	RoleFilter  []string `json:"role_filter,omitempty"`   // empty = all roles
	// FromPRDs (BL387 Phase 2b) — list of PRD IDs whose prd-shared memories are
	// seeded into this PRD's prd-shared at first-task spawn. Enables cross-PRD
	// knowledge transfer without manual operator intervention.
	FromPRDs []string `json:"from_prds,omitempty"`
}

// MemoryHarvestConfig (BL386 Phase 2) controls harvest-on-completion.
// Zero value = disabled; no behavior change from pre-BL386 code.
type MemoryHarvestConfig struct {
	Enabled    bool     `json:"enabled,omitempty"`
	PromoteTo  string   `json:"promote_to,omitempty"`  // "story-shared" | "prd-shared" | "project-shared"; default "story-shared"
	RoleFilter []string `json:"role_filter,omitempty"` // empty = all roles
	Max        int      `json:"max,omitempty"`         // default 50 when Enabled
}

// TemplateVar (BL191 Q2) declares one substitutable variable for a
// PRD-as-template. {{name}} occurrences in spec, title, and per-task
// specs are replaced at instantiate time.
type TemplateVar struct {
	Name     string `json:"name"`
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
	Help     string `json:"help,omitempty"`
}

// Template (BL221 v6.2.0) is a reusable spec blueprint with variable
// placeholders. Distinct from is_template PRDs (which are executable
// clones); Templates are first-class entities with their own store.
type Template struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Spec        string        `json:"spec"`
	Type        string        `json:"type,omitempty"` // software | research | operational | personal
	Tags        []string      `json:"tags,omitempty"`
	Vars        []TemplateVar `json:"vars,omitempty"`
	IsBuiltin   bool          `json:"is_builtin,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	LastUsedAt  *time.Time    `json:"last_used_at,omitempty"`
	UseCount    int           `json:"use_count,omitempty"`
}

// ExtractVars scans spec for {{var_name}} placeholders and returns
// TemplateVar entries for any new names not already in existing.
func ExtractVars(spec string, existing []TemplateVar) []TemplateVar {
	seen := map[string]bool{}
	for _, v := range existing {
		seen[v.Name] = true
	}
	out := append([]TemplateVar{}, existing...)
	i := 0
	for i < len(spec) {
		start := findSubstr(spec, "{{", i)
		if start < 0 {
			break
		}
		end := findSubstr(spec, "}}", start+2)
		if end < 0 {
			break
		}
		name := spec[start+2 : end]
		if name != "" && isVarName(name) && !seen[name] {
			seen[name] = true
			out = append(out, TemplateVar{Name: name})
		}
		i = end + 2
	}
	return out
}

func findSubstr(s, sub string, from int) int {
	idx := 0
	for j := from; j <= len(s)-len(sub); j++ {
		if s[j:j+len(sub)] == sub {
			return j
		}
		idx++
	}
	return -1
}

func isVarName(s string) bool {
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return len(s) > 0
}

// Story is a meaningful slice of work; multiple Stories per PRD,
// each with its own task DAG.
type Story struct {
	ID          string      `json:"id"`
	PRDID       string      `json:"prd_id"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Status      StoryStatus `json:"status"`
	DependsOn   []string    `json:"depends_on,omitempty"` // other Story IDs
	Tasks       []Task      `json:"tasks,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`

	// BL191 Q6 (v5.10.0) — guardrail verdicts at the story level.
	// Populated when Config.PerStoryGuardrails is non-empty; one entry
	// per guardrail named in that list. Block on any block outcome.
	Verdicts []GuardrailVerdict `json:"verdicts,omitempty"`

	// FailureReason (BL410 Phase 1) — structured, categorized detail
	// for the story's last failed/blocked guardrail outcome. Story has
	// no Error-equivalent bare-string field today; this is genuinely
	// new, not additive-alongside-an-existing-field like Task's.
	FailureReason *FailureReason `json:"failure_reason,omitempty"`

	// Phase 3 (v5.26.60) — per-story execution profile override +
	// per-story approval gate. ExecutionProfile empty = inherit
	// PRD.ProjectProfile (the new "default execution profile" role).
	// Approved is the per-story gate; only relevant when the global
	// autonomous.per_story_approval flag is on. Without it, stories
	// auto-approve when the PRD itself is approved (current behavior).
	ExecutionProfile string     `json:"execution_profile,omitempty"`
	Approved         bool       `json:"approved,omitempty"`
	ApprovedBy       string     `json:"approved_by,omitempty"`
	ApprovedAt       *time.Time `json:"approved_at,omitempty"`
	RejectedReason   string     `json:"rejected_reason,omitempty"`

	// BL381 — per-story LLM overrides. Resolution order:
	//   per-task → per-story → per-PRD → global.
	// Empty = inherit from PRD then global config.
	Backend string `json:"backend,omitempty"`
	Effort  Effort `json:"effort,omitempty"`
	Model   string `json:"model,omitempty"`
	// Story-level pi --thinking override (off|minimal|low|medium|high).
	// Inherited by tasks in this story whose own ThinkingLevel is empty.
	// Empty = inherit from the PRD-level value then the global
	// pi.thinking_level config.
	ThinkingLevel string `json:"thinking_level,omitempty"`
	// Story-level verifier LLM override. Precedence: task → story
	// (here) → Automaton → global autonomous verification_* config.
	// Applies to every task in this story that has no task-level
	// override. Empty = inherit. See ResolveVerificationLLM.
	VerificationBackend string `json:"verification_backend,omitempty"`
	VerificationModel   string `json:"verification_model,omitempty"`
	VerificationEffort  Effort `json:"verification_effort,omitempty"`

	// Phase 4 (v5.26.64) — file association. FilesPlanned is
	// LLM-extracted at decompose time (the decomposer prompt asks
	// for `files: [...]` per story); operator can edit via the
	// story-edit modal. Empty when the decomposer omitted them.
	//
	// BL416 Phase 1 (Decision 9) — file provenance. A story carries two
	// of the three categories (real-diff files live on its Tasks):
	//   (a) FilesPlanned (json "files") — planning intent from
	//       decompose; operator-editable; may not match what was
	//       actually changed.
	//   (b) FilesTouched — Task-only; see Task's doc comment. The
	//       story-level view is the union of its tasks' FilesTouched.
	//   (c) FilesGenerated — files produced by the verification step
	//       itself (e.g. the story's VerificationScript under
	//       .datawatch/verify/), recorded separately so they are never
	//       confused with the story's real work.
	// The shape is parallel arrays rather than a tagged struct so the
	// existing `files` / `files_touched` JSON wire shape stays unchanged
	// for existing clients. A future writer of VerificationScript must
	// append the script path to FilesGenerated.
	FilesPlanned   []string `json:"files,omitempty"`
	FilesGenerated []string `json:"files_generated,omitempty"`

	// BL416 Phase 1 (Decision 4) — story-level verification script:
	// relative path under PRD.ProjectDir to an executable script
	// (planned location `.datawatch/verify/<story-id>.sh`) that a
	// future decompose pass writes for session-based backends. Empty =
	// none; the legacy judgment-only guardrail behavior is the
	// fallback. Inert today: nothing writes or reads it yet.
	VerificationScript string `json:"verification_script,omitempty"`

	// Truncated is set only on the capped copy the default detail GET
	// returns (see CapPRDText). It is never stored.
	Truncated []TruncatedField `json:"truncated,omitempty"`
}

// VerifierPolicyChoice records which verifier backend the verifier policy
// picked for one verification and why.
type VerifierPolicyChoice struct {
	Backend string    `json:"backend"`
	Model   string    `json:"model,omitempty"`
	Reason  string    `json:"reason,omitempty"`
	At      time.Time `json:"at"`
}

// Nudge outcomes recorded on a NudgeEvent. sent: the nudge was typed into the
// session; answered: the worker printed the completion marker or fresh output
// followed the nudge; stalled: the worker stayed silent past the stall window.
const (
	NudgeOutcomeSent     = "sent"
	NudgeOutcomeAnswered = "answered"
	NudgeOutcomeStalled  = "stalled"
)

// NudgeEvent records one idle-worker nudge sent to a task's session. SessionID
// names the session the nudge was typed into: a retry runs in a fresh session,
// so the nudge state of a new session is read only from events that match it.
// Events written before SessionID existed decode with it empty.
type NudgeEvent struct {
	At        time.Time `json:"at"`
	Count     int       `json:"count"`
	SessionID string    `json:"session_id,omitempty"`
	Outcome   string    `json:"outcome,omitempty"`
}

// DeliveryFailureEvent records one task delivery that failed because the
// worker session's main prompt never appeared within every attempt.
type DeliveryFailureEvent struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

// Task is a single unit of work — runs as a session under
// session.Manager. Maps 1:1 to a pipeline.Task once enqueued.
//
// BL203 (v5.4.0) — flexible LLM selection at the task level. Backend /
// Effort / Model fields override story-level defaults when set. Most-
// specific wins:
//
//	per-task → per-story (BL381) → per-PRD → per-stage (autonomous.{planning,verification}_backend) → global session.llm_backend
//
// TaskHistoryEntry is one note in a task's history.
type TaskHistoryEntry struct {
	At   time.Time `json:"at"`
	Note string    `json:"note"`
}

type Task struct {
	ID         string     `json:"id"`
	StoryID    string     `json:"story_id"`
	PRDID      string     `json:"prd_id"`
	Title      string     `json:"title"`
	Spec       string     `json:"spec"` // detailed task description for the worker
	Status     TaskStatus `json:"status"`
	DependsOn  []string   `json:"depends_on,omitempty"`  // other Task IDs
	SessionID  string     `json:"session_id,omitempty"`  // datawatch session ID once running
	WaitReason string     `json:"wait_reason,omitempty"` // why the task is waiting_capacity or waiting_decision
	// ComputeNode is the compute node the task's worker was started on, as the
	// start handler reports it. A daemon restart adopts the surviving worker
	// onto this node before it looks at anything else.
	ComputeNode string `json:"compute_node,omitempty"`
	// VerifierSessionID is the live verifier session adopted at boot, so the
	// resumed verification waits for it instead of spawning a second one.
	VerifierSessionID string `json:"verifier_session_id,omitempty"`
	// VerifierInfraFailures counts consecutive infra verifier failures on this
	// task; reset by any non-infra result and after an escalated run.
	VerifierInfraFailures int `json:"verifier_infra_failures,omitempty"`
	// VerifierPolicyChoice is the backend the verifier policy picked for this
	// task's latest verification, with the model and the reason. Nil when the
	// policy did not decide (mode fixed, no override).
	VerifierPolicyChoice *VerifierPolicyChoice `json:"verifier_policy_choice,omitempty"`
	// Kind classifies the task for the smoke-verification feature story
	// (independent deliverable; this task only adds the field): "smoke" |
	// "final_gate" | "" (ordinary). Empty = ordinary task; readers must
	// tolerate unknown values.
	Kind         string              `json:"kind,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
	StartedAt    *time.Time          `json:"started_at,omitempty"`
	CompletedAt  *time.Time          `json:"completed_at,omitempty"`
	Error        string              `json:"error,omitempty"`
	Verification *VerificationResult `json:"verification,omitempty"`
	RetryCount   int                 `json:"retry_count,omitempty"`
	// PriorAttemptCount (v9.2.1) — how many times this task has been reset
	// and re-queued (ResetTask) for a fresh attempt. Unlike RetryCount
	// (which ResetTask zeroes to start the next attempt's auto-retry budget
	// at 0), this is a cumulative, monotonically-increasing count that is
	// NEVER reset: ResetTask increments it and only ever resets RetryCount.
	// The verifier uses it to tell a genuine retry ("this is a re-run of a
	// task whose prior attempt may have already landed correct work") from a
	// first attempt, so it can safely fall back to the task's own
	// verification script when the retry produced no diff. JSON omitempty so
	// the first attempt (0) is invisible on the wire; no existing client
	// relied on its absence to mean something.
	PriorAttemptCount int `json:"prior_attempt_count,omitempty"`
	// NudgeCount is how many idle-worker nudges were sent for this task, and
	// NudgeEvents records each one.
	NudgeCount  int          `json:"nudge_count,omitempty"`
	NudgeEvents []NudgeEvent `json:"nudge_events,omitempty"`
	// DeliveryFailureEvents records each time the task's worker session never
	// reached its main prompt, so the task was never given its work. Each
	// event carries the reason logged at the time.
	DeliveryFailureEvents []DeliveryFailureEvent `json:"delivery_failure_events,omitempty"`
	// History is a short audit trail of task-level events that have no field of
	// their own, e.g. live sessions ended before a replacement launched.
	History []TaskHistoryEntry `json:"history,omitempty"`
	// CleanRestart — set by an operator reset (ResetTaskWithOptions with
	// cleanRestart=true) to ask that the next attempt start from the pre-task
	// tree instead of restoring the last preserved attempt work (AttemptRefs).
	// The spawn path skips the restore for that one attempt and clears the flag
	// once the attempt has run, so automatic retries after it restore again.
	// Default false: restore stays on.
	CleanRestart bool `json:"clean_restart,omitempty"`
	// AdoptedSession — set at boot-resume when this in_progress task's recorded
	// session survived the daemon restart and was adopted instead of killed.
	// It tells executeOne not to spawn: wait for the session's completion
	// marker, then verify. Cleared once executeOne consumes it.
	AdoptedSession bool `json:"adopted_session,omitempty"`
	// FailureReason (BL410 Phase 1) — structured, categorized detail
	// alongside Error (a bare string, left untouched for existing
	// readers). Set on the task's last failed/blocked attempt.
	FailureReason *FailureReason `json:"failure_reason,omitempty"`
	// SessionEndReason records why this task's live sessions were ended —
	// set structurally by reset_ends_sessions.go whenever they are ended,
	// so the reason is a stable field rather than only recoverable from a
	// decision note. Blank while the task holds no ended-session record.
	SessionEndReason string `json:"session_end_reason,omitempty"`
	// StartFailureTail — the last 20 non-empty lines of the visible tmux
	// pane captured at failure time, set only when a spawned session never
	// started (sat in waiting_input without any assistant output past the
	// start-confirm bound). Retained on the task for post-mortem diagnosis
	// (e.g. a startup dialog waiting on an operator action that no one
	// dismissed); empty omitted on the wire for normal attempts.
	StartFailureTail []string `json:"start_failure_tail,omitempty"`
	// StartFailures — how many of this task's attempts failed the
	// start-confirm bound (ErrSessionNeverStarted) and were re-queued. Each
	// such failure re-runs the SAME attempt without consuming the PRD's
	// normal auto-retry budget (RetryPolicy categories, RetryCeilingTotal
	// and AutoFixRetries all untouched); once StartFailures exceeds
	// SessionStartRetryBudget the task fails with the "session never
	// started" reason. Persisted through the same task save path the
	// executor uses for every attempt, so a daemon restart mid-streak keeps
	// the count. JSON omitempty: 0 is invisible on the wire.
	StartFailures int `json:"start_failures,omitempty"`
	// Per-task LLM overrides — empty = inherit from PRD then global.
	Backend string `json:"backend,omitempty"`
	Effort  Effort `json:"effort,omitempty"`
	Model   string `json:"model,omitempty"`
	// ThinkingLevel — per-task pi --thinking override (off|minimal|low|
	// medium|high, closed set from config.PiThinkingLevels). Most
	// specific wins: this value beats the story- and PRD-level values
	// and the global pi.thinking_level config. Empty = fall through
	// the chain. Validated at the set_thinking_level write edge.
	ThinkingLevel string `json:"thinking_level,omitempty"`
	// ProjectDir is an optional per-task working/verification directory.
	// Empty inherits PRD.ProjectDir. Same task > PRD precedence as the
	// per-task LLM overrides (Backend/Effort/Model above); resolved by
	// ResolveTaskProjectDir. BL447.
	ProjectDir string `json:"project_dir,omitempty"`
	// BL416 Phase 1 — per-task guardrail LLM override — empty = inherit
	// PRD.GuardrailBackend/GuardrailModel, then the global autonomous
	// verification_backend/verification_model. Resolved by
	// resolveGuardrailBackendModel (executor.go; mirrors
	// resolveTaskBackendModel's chain); not yet used for routing.
	GuardrailBackend string `json:"guardrail_backend,omitempty"`
	GuardrailModel   string `json:"guardrail_model,omitempty"`
	// Per-task verifier LLM override — the most specific level.
	// Precedence: task (here) → story → Automaton → global autonomous
	// verification_backend/verification_model/verification_effort.
	// Empty = inherit. See ResolveVerificationLLM.
	VerificationBackend string `json:"verification_backend,omitempty"`
	VerificationModel   string `json:"verification_model,omitempty"`
	VerificationEffort  Effort `json:"verification_effort,omitempty"`
	// VerifierPolicy overrides the Automaton and global verifier policy mode
	// and backend list for this task — the most specific level. Nil =
	// inherit. See ResolveVerifierPolicy.
	VerifierPolicy *VerifierPolicyOverride `json:"verifier_policy,omitempty"`
	// FailureEscalation overrides the Automaton and global failure
	// escalation policy for this task, per field. Nil = inherit. See
	// resolveFailureEscalation.
	FailureEscalation *FailureEscalationPolicy `json:"failure_escalation,omitempty"`
	// Attempts is the per-attempt history the failure escalation policy
	// records for this task (one entry per failed, stalled or cancelled
	// attempt).
	Attempts []TaskAttempt `json:"attempts,omitempty"`
	// RunnerEvidence is set by the worker wait loop just before a pi-prompt
	// stall and cleared once recordFailedAttempt has copied it into the
	// attempt record.
	RunnerEvidence *RunnerEvidence `json:"runner_evidence,omitempty"`
	// DecisionResume records the worker question the coordinator tick last
	// resumed this task for (question_tick.go), so a later tick never resumes
	// it twice for the same answer. Nil = never resumed for a question.
	DecisionResume *DecisionResume `json:"decision_resume,omitempty"`
	// EscalationFailures counts failed attempts since the last operator
	// action; it indexes the policy's steps and is compared with
	// max_failures.
	EscalationFailures int `json:"escalation_failures,omitempty"`
	// ReviewSummary is the plain-text report written when the task is held
	// in TaskReviewNeeded.
	ReviewSummary *ReviewSummary `json:"review_summary,omitempty"`
	// OperatorWorkerSet is true when the operator's last review retry chose
	// the task's worker. The failure escalation ladder then does not replace
	// that worker for the attempts that follow; a retry without a worker
	// change clears it, and the ladder applies again.
	OperatorWorkerSet bool `json:"operator_worker_set,omitempty"`
	// ExecutorSource is the level that supplied the execution backend for
	// the most recent run attempt: "task", "story", "prd", "automation",
	// "global" (no level set one, so the fallback backend was used) or
	// "escalation" (a failure escalation step chose the worker). Set by
	// the dispatch loop before the session spawns and overwritten by each
	// later attempt.
	ExecutorSource string `json:"executor_source,omitempty"`
	// LSPLanguage (v8.7.1) — per-task override; inherits PRD.LSPLanguage when empty.
	LSPLanguage string `json:"lsp_language,omitempty"`
	// PermissionMode (v5.27.5) — claude-code --permission-mode for
	// just this task. Inherits PRD.PermissionMode → session default
	// when empty. Operators set "plan" on a single task to keep
	// that step design-only inside an otherwise execute-the-plan PRD.
	PermissionMode string `json:"permission_mode,omitempty"`

	// BL191 Q4 (v5.9.0) — recursive child-PRD shortcut. When SpawnPRD
	// is true, the task spec is treated as a *child PRD spec* rather
	// than a direct worker prompt. The executor creates a child PRD,
	// decomposes it, runs it (auto-approving when Config.AutoApproveChildren),
	// and completes this parent task only when the child PRD reaches
	// PRDCompleted. ChildPRDID records the spawned PRD's ID for
	// traceability across the genealogy tree.
	SpawnPRD   bool   `json:"spawn_prd,omitempty"`
	ChildPRDID string `json:"child_prd_id,omitempty"`

	// BL366 — git SHA captured before the worker session ran. Used by the
	// verifier to produce a git diff of the worker's actual changes.
	// Empty when the project has no git repo or the task is cluster-dispatched.
	// Per-attempt: overwritten with the current attempt's HEAD on every
	// (re)attempt; the per-task value lives in BaseSHA.
	PreTaskSHA string `json:"pre_task_sha,omitempty"`
	// BaseSHA — the git HEAD captured at the start of the task's FIRST
	// attempt. Unlike PreTaskSHA (per-attempt, rewritten on every retry),
	// BaseSHA is per-task and is never overwritten by retries: it always
	// anchors the diff of the task's entire work against the state before
	// it ever started.
	BaseSHA string `json:"base_sha,omitempty"`
	// PostVerifySHA — the git HEAD (this task's own commit; BL465 fires
	// the deterministic commit at DATAWATCH_COMPLETE, before verify) captured
	// right where the executor sets TaskCompleted. The ci-watch
	// PRD-completion guardrail watches this pin instead of whatever live HEAD
	// is at roll-up time — a sibling PRD or the operator may have moved the
	// same tree in the meantime (2026-10-13 incident, root cause iii).
	// Empty when there is no usable git repo (or the capture failed); the
	// guardrail then falls back to live HEAD.
	PostVerifySHA string `json:"post_verify_sha,omitempty"`
	// UncommittedWork — set while the task is held because its worktree
	// still carries uncommitted files after the task finished (see
	// holdForUncommittedWork). Nil when no hold was ever recorded.
	UncommittedWork *UncommittedWorkHold `json:"uncommitted_work,omitempty"`
	// AttemptCommitSHAs — the deterministic commit shas recorded for this
	// task's finished attempts, in order (RecordAttemptCommit; duplicates
	// and empty values are ignored).
	AttemptCommitSHAs []string `json:"attempt_commit_shas,omitempty"`
	// AttemptRefs — the git refs (refs/datawatch/attempts/<task>/<n>) that
	// preserve the working state of this task's killed or stalled attempts,
	// in order. Appended by recordAttemptSnapshot; the next snapshot uses
	// len(AttemptRefs)+1 as its attempt number so refs never collide.
	AttemptRefs []string `json:"attempt_refs,omitempty"`
	// v8.25.12 — untracked file list (git ls-files --others --exclude-standard)
	// captured alongside PreTaskSHA. Verifier uses this to detect newly created
	// files that are not tracked by git (e.g. new documentation, new test fixtures).
	PreTaskUntrackedFiles []string `json:"pre_task_untracked_files,omitempty"`
	// Foreign-file drift — tracked files with unstaged OR staged-but-uncommitted
	// modifications (git diff --name-only HEAD ∪ git diff --name-only --cached HEAD)
	// captured alongside PreTaskSHA. Used by the foreign-file-drift check
	// (DetectForeignFileDrift). Empty in worktree mode (nothing pre-dirty to protect).
	PreTaskDirtyFiles []string `json:"pre_task_dirty_files,omitempty"`
	// ForeignFileDrift — the sorted union (across retries) of the exact files this task's own commit(s) swept
	// in that were already dirty or untracked before its worker session
	// started and are NOT in FilesPlanned (see DetectForeignFileDrift).
	// Deliberately separate from FailureReason: this can fire on an
	// otherwise-successful TaskCompleted task and must not make a clean
	// completion read as a failure.
	ForeignFileDrift []string `json:"foreign_file_drift,omitempty"`

	// UndeclaredEdits — the worker's own committed edits to files that are
	// in neither FilesPlanned nor FilesGenerated (see checkUndeclaredEdits).
	// Accumulated across retries; LinesChanged is summed per path. Disjoint
	// from ForeignFileDrift by construction.
	UndeclaredEdits []UndeclaredEdit `json:"undeclared_edits,omitempty"`

	// BL367 — quality gate result recorded after each task completes.
	// Nil when quality gates are disabled for this PRD.
	QualityGateResult *pipeline.QualityGateResult `json:"quality_gate_result,omitempty"`

	// GateDisagreement — set when the verifier PASSED but the quality gate
	// disagreed (valid-baseline regression, or invalid gate). Recorded
	// under every policy (warn and block); under "warn" the task completes
	// and this is the only trace of the disagreement on the task, under
	// "block" (regression only) it rides along on the failed task. See
	// GateDisagreement for the field semantics.
	GateDisagreement *GateDisagreement `json:"gate_disagreement,omitempty"`

	// PriorAttemptHint (v8.36.1) — set by ResetTask from the outgoing
	// Error/Verification of the attempt it's about to clear, so a fresh
	// Run() after an operator-triggered retry seeds executeOne's RetryHint
	// with why the last attempt failed, instead of redelivering the
	// identical original spec with no context. Without this, a retry of a
	// task whose prior attempt genuinely succeeded but only failed
	// *verification* looks to the worker like brand-new, already-done work
	// — capable models notice the discrepancy and stop to ask what to do,
	// which hangs forever in a one-shot session with no human present.
	// Cleared once consumed (single-use, like the in-loop retry hint it
	// mirrors).
	PriorAttemptHint string `json:"prior_attempt_hint,omitempty"`

	// BL191 Q6 (v5.10.0) — guardrail verdicts at the task level.
	// Populated when Config.PerTaskGuardrails is non-empty; one entry
	// per guardrail named in that list. Block on any block outcome.
	Verdicts []GuardrailVerdict `json:"verdicts,omitempty"`

	// BL406 Phase 5 — per-task operator approval gate, same shape as
	// Story's Approved/ApprovedBy/ApprovedAt/RejectedReason. Only
	// meaningful when PRD.GuidedModeSource is "operator".
	Approved       bool       `json:"approved,omitempty"`
	ApprovedBy     string     `json:"approved_by,omitempty"`
	ApprovedAt     *time.Time `json:"approved_at,omitempty"`
	RejectedReason string     `json:"rejected_reason,omitempty"`

	// Phase 4 (v5.26.64) — file association. FilesPlanned is
	// LLM-extracted at decompose time (operator-editable). FilesTouched
	// is populated post-spawn from the worker session's git diff
	// --name-only. Both are advisory — empty doesn't block execution.
	//
	// BL416 Phase 1 (Decision 9) — file provenance. Three distinct
	// categories, never merged into one list:
	//   (a) FilesPlanned (json "files") — planning intent from
	//       decompose; operator-editable; may not match what was
	//       actually changed.
	//   (b) FilesTouched — the real, git-diff-derived files the task's
	//       work modified, populated by Manager.RecordTaskFilesTouched.
	//       Semantics unchanged; must NEVER include verification-script
	//       files.
	//   (c) FilesGenerated — files produced by the verification step
	//       itself (e.g. the VerificationScript file under
	//       .datawatch/verify/), recorded separately so they are never
	//       confused with the task's real work.
	// The shape is parallel arrays rather than a tagged struct so the
	// existing `files` / `files_touched` JSON wire shape stays unchanged
	// for existing clients. A future writer of VerificationScript must
	// append the script path to FilesGenerated, and a future
	// FilesTouched population must exclude any path present in
	// FilesGenerated.
	FilesPlanned   []string `json:"files,omitempty"`
	FilesTouched   []string `json:"files_touched,omitempty"`
	FilesGenerated []string `json:"files_generated,omitempty"`

	// BL416 Phase 1 (Decision 4) — relative path under PRD.ProjectDir
	// to an executable verification script (planned location
	// `.datawatch/verify/<task-id>.sh`) that a future decompose pass
	// writes for session-based backends. Empty = none; the legacy
	// judgment-only guardrail behavior is the fallback. Inert today:
	// nothing writes or reads it yet.
	VerificationScript string `json:"verification_script,omitempty"`
	// VerificationScriptReason — B124 exemption: a task may legitimately
	// have no verification script (e.g. a pure-config or documentation
	// change where a script would only re-assert trivially true things).
	// When such a task exists in a plan, the decomposer / editor sets this
	// field to state explicitly WHY no VerificationScript is attached
	// (e.g. "config-only change; no executable surface to verify"). The
	// structural plan check (CheckPlanStructure) flags any task that has
	// neither a VerificationScript nor a VerificationScriptReason, so an
	// unexplained absence is a finding while an explained one passes.
	// Optional (omitempty); setting it does not change any existing
	// VerificationScript behavior — it is purely metadata consumed by the
	// structural check and surfaced in reports.
	VerificationScriptReason string `json:"verification_script_reason,omitempty"`

	// Progress — a live snapshot of the worker session's own log (read via
	// internal/pilog) for THIS attempt: how many assistant turns it has taken,
	// which tools it has called and how often, its most recent action, and the
	// cumulative token count. Mirrors pilog.Progress; LastEventAt is RFC3339
	// so it renders correctly in JSON without extra plumbing.
	//
	// Populated ONLY while the task is in progress: the executor's periodic
	// session poll (waitLoop, cmd/datawatch/main.go) refreshes it every few
	// seconds from the session's live log. Once the task leaves in-progress
	// (completed / failed / pending / cancelled / blocked) it is cleared and
	// must stay nil — Store enforces the invariant at persist time so the
	// guarantee holds no matter which save path ran. prd-get surfaces it
	// under each running task so an operator (or an MCP client) can see what
	// the worker is actually doing right now, not just that it's in progress.
	Progress *TaskProgress `json:"progress,omitempty"`

	// LoopingWarning — set by the same poll loop that refreshes Progress when
	// the worker's most recent action has been identical across several
	// consecutive polls with no progress between them: a strong signal the
	// agent is spinning on the same tool call. Free-form, human-readable, and
	// explicitly a warning not a verdict: it never fails the task on its own,
	// it tells a human or an LLM-supervisor where to look first. Empty
	// normally; a still-running healthy task has no warning set.
	LoopingWarning string `json:"looping_warning,omitempty"`

	// Truncated is set only on the capped copy the default detail GET
	// returns (see CapPRDText). It is never stored.
	Truncated []TruncatedField `json:"truncated,omitempty"`
}

// UndeclaredEdit records one file a task's worker edited without declaring
// it (neither planned nor generated) and the lines changed in it.
type UndeclaredEdit struct {
	Path         string `json:"path"`
	LinesChanged int    `json:"lines_changed"`
}

// NudgeEventForSession returns the latest nudge recorded for the given session
// and whether that session has one. Nudges from other sessions, including
// earlier attempts of this task, are ignored, and an empty sessionID matches
// nothing.
func (t *Task) NudgeEventForSession(sessionID string) (NudgeEvent, bool) {
	if sessionID == "" {
		return NudgeEvent{}, false
	}
	for i := len(t.NudgeEvents) - 1; i >= 0; i-- {
		if t.NudgeEvents[i].SessionID == sessionID {
			return t.NudgeEvents[i], true
		}
	}
	return NudgeEvent{}, false
}

// NudgeTimeForSession returns the time of the latest nudge recorded for the
// given session, or the zero time when that session has none. Nudges from
// other sessions, including earlier attempts of this task, are ignored, and an
// empty sessionID matches nothing.
func (t *Task) NudgeTimeForSession(sessionID string) time.Time {
	ev, ok := t.NudgeEventForSession(sessionID)
	if !ok {
		return time.Time{}
	}
	return ev.At
}

// RecordAttemptCommit appends the deterministic commit sha recorded for a
// finished attempt of this task, in order. An empty sha is ignored, and a
// sha already present is not appended again, so the list stays an
// ordered, de-duplicated record of the task's finished attempts.
func (t *Task) RecordAttemptCommit(sha string) {
	if t == nil || sha == "" {
		return
	}
	for _, existing := range t.AttemptCommitSHAs {
		if existing == sha {
			return
		}
	}
	t.AttemptCommitSHAs = append(t.AttemptCommitSHAs, sha)
}

// GuardrailVerdict (BL191 Q6, v5.10.0) is one guardrail's pass/warn/block
// judgment at the story or task level. Mirrors the orchestrator's
// Verdict shape so downstream UI can render uniformly.
type GuardrailVerdict struct {
	Guardrail string    `json:"guardrail"` // "rules" | "security" | "release-readiness" | "docs-diagrams-architecture" | …
	Outcome   string    `json:"outcome"`   // "pass" | "warn" | "block"
	Severity  string    `json:"severity,omitempty"`
	Summary   string    `json:"summary"`
	Issues    []string  `json:"issues,omitempty"`
	VerdictAt time.Time `json:"verdict_at"`
	// Category (BL410 Phase 0) classifies WHY a non-pass outcome happened:
	// "infra" (the judging mechanism itself failed with NO output —
	// session timeout/crash/no-output failure — never reached a real
	// judgment) vs "invalid-response" (the judge DID write a response,
	// but it could not be parsed as a verdict; terminal — never a basis
	// for an infra retry, which would re-run the whole judge against
	// the same already-relocated output file) vs
	// "content" (a real judgment ran and found a real issue). Empty
	// means "content" — the pre-BL410 implicit behavior, since every
	// verdict before this field existed came from a completed judgment.
	// See FailureReason's doc comment for the full taxonomy. Found live
	// 2026-10-12: a session timeout and a genuine AGENT.md violation both
	// produced an indistinguishable block outcome, so a timeout required
	// the same manual operator intervention as a real compliance finding.
	Category string `json:"category,omitempty"`
	// JudgeSessionID is the id of the one-shot session that produced this
	// verdict. Empty when the judgment was a text-only call or a
	// deterministic check.
	JudgeSessionID string `json:"judge_session_id,omitempty"`
	// JudgeOutputPath is the full path of the judge output file that
	// backed this verdict. Empty when there was no file.
	JudgeOutputPath string `json:"judge_output_path,omitempty"`
	// Backend is the name of the backend that ran a text-only judgment.
	// Empty when the verdict carries no backend (the usual case) or when a
	// judge session already identifies the run.
	Backend string `json:"backend,omitempty"`
	// RawJudge is the raw judge text backing this verdict, truncated to
	// 16 KiB, persisted on the PRD for auditing and debugging. Empty when
	// no raw text is available (deterministic checks, or the judge text
	// was not captured).
	RawJudge string `json:"raw_judge,omitempty"`
	// JudgeAttempts is the summary of every judge attempt that led to this
	// verdict, one per backend tried, in the order tried. Set on a judge-
	// timeout hold; empty for a judgment that finished on a backend.
	JudgeAttempts []JudgeAttemptRecord `json:"judge_attempts,omitempty"`
}

// VerificationResult is BL25 output. Severity levels match
// nightwire's verifier.py contract for cross-tool ingestion.
type VerificationResult struct {
	OK         bool      `json:"ok"`
	Severity   string    `json:"severity,omitempty"` // info | low | medium | high | critical
	Summary    string    `json:"summary"`
	Issues     []string  `json:"issues,omitempty"`
	DiffSHA    string    `json:"diff_sha,omitempty"` // git rev-parse HEAD post-task
	VerifiedAt time.Time `json:"verified_at"`
	VerifierID string    `json:"verifier_id,omitempty"` // session ID of the verifier worker
	// What the verifier ACTUALLY ran with, recorded per verification.
	// VerifierSource names where the backend came from: "task",
	// "story", "prd" (the Automaton level), "automation" (the Automation
	// the Automaton belongs to) or "global" per ResolveVerificationLLM, or "pool" when the backend was picked
	// from the global verification_backends capacity pool.
	VerifierBackend string `json:"verifier_backend,omitempty"`
	VerifierModel   string `json:"verifier_model,omitempty"`
	VerifierEffort  string `json:"verifier_effort,omitempty"`
	VerifierSource  string `json:"verifier_source,omitempty"`
	// Category (BL410 Phase 0) — see GuardrailVerdict.Category's doc
	// comment; same taxonomy, same empty-means-"content" default.
	Category string `json:"category,omitempty"`
	// UnmodifiedRetry (v9.2.1) — true when this verification passed via the
	// unmodified-retry escape hatch: the retry itself produced no diff or
	// new files (taskProducedNoOutput), but the task is a retry and its own
	// VerificationScript re-run against the already-committed deliverable
	// passed. Records that the "empty diff" verdict was overridden by a
	// deterministic script check rather than by the LLM judge, so the
	// decision is auditable in the result itself. Absent (false) for
	// every other verification path; omitempty keeps the wire shape
	// unchanged for existing consumers.
	UnmodifiedRetry bool `json:"unmodified_retry,omitempty"`
	// RawJudge is the raw verifier text backing this result, truncated to
	// 16 KiB, persisted on the PRD for auditing and debugging. Empty when
	// no raw text is available (deterministic path, or the verification
	// text was not captured).
	RawJudge string `json:"raw_judge,omitempty"`
}

// Learning is a post-task insight extracted by the loop and saved
// into datawatch memory (BL57 KG + BL23 episodic). Kept here purely
// for the autonomous-internal record; the source of truth is memory.
// GuardrailEntry (BL303 S2) is one registered guardrail in the library.
// Built-in scan guardrails (sast-scan, secrets-scan, deps-scan) are Type=="scan";
// skill-contributed and custom guardrails are Type=="skill" or "custom".
type GuardrailEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"`                // "scan" | "custom" | "skill"
	ScanType    string `json:"scan_type,omitempty"` // "sast" | "secrets" | "deps" (scan entries only)
}

// Tag is a managed label that can be nested as a tree and assigned to
// several Automata via PRD.TagIDs.
type Tag struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ParentID    string    `json:"parent_id,omitempty"` // "" = root tag
	Color       string    `json:"color,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GuardrailProfile (BL303 S2) is a named collection of guardrails that can
// be assigned to an Automaton or used as a global default.
type GuardrailProfile struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Guardrails  []string `json:"guardrails"`
	// BlockOnFileDrift uses a pointer so a profile can explicitly
	// express "disabled" rather than merely "not set" (nil = fall
	// through to global Config.BlockOnFileDrift).
	BlockOnFileDrift *bool `json:"block_on_file_drift,omitempty"`
	// NotifyEvents lists the lifecycle event kinds (see
	// ValidNotifyEventKinds) that produce notifications for Automata on
	// this profile. Empty = inherit from the next tier (global default
	// set).
	NotifyEvents []string  `json:"notify_events,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// RetryProfile (BL410 Phase 1) is a named, reusable retry policy — the
// resolveRetryPolicy precedence's middle tier (explicit PRD.RetryPolicy
// > named RetryProfile > global Config), mirroring GuardrailProfile's
// shape and CRUD. Per the operator's explicit decision, a profile
// controls both the per-category budget numbers AND behavior (whether
// content auto-fix is enabled at all, the infra escalation threshold,
// the ceiling) — not just counts.
type RetryProfile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Budgets maps category ("infra"|"content"|"scope"|"policy") to
	// its retry count. A category absent here falls through to global
	// Config (same content/policy→AutoFixRetries fallback applies).
	Budgets                 map[string]int `json:"budgets,omitempty"`
	CeilingTotal            int            `json:"ceiling_total,omitempty"`
	InfraRetryEscalateAfter int            `json:"infra_retry_escalate_after,omitempty"`
	// ContentAutoFixEnabled/AutoFixSkipApproval use pointers so a
	// profile can explicitly express "disabled" rather than merely
	// "not set" — the same explicit-clear discipline the Config-level
	// bools next to these need (see Config.ContentAutoFixEnabled).
	ContentAutoFixEnabled *bool     `json:"content_auto_fix_enabled,omitempty"`
	AutoFixSkipApproval   *bool     `json:"auto_fix_skip_approval,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type Learning struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	PRDID     string    `json:"prd_id"`
	Category  string    `json:"category,omitempty"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// LoopStatus is the manager-loop snapshot.
type LoopStatus struct {
	Running      bool      `json:"running"`
	ActivePRDs   int       `json:"active_prds"`
	QueuedTasks  int       `json:"queued_tasks"`
	RunningTasks int       `json:"running_tasks"`
	LastTickAt   time.Time `json:"last_tick_at,omitempty"`
	StaleTasks   int       `json:"stale_tasks,omitempty"`

	// v5.22.0 — observability fill-in for BL191 Q4 + Q6 (audit
	// follow-up). Pre-v5.22.0 the LoopStatus carried no signal for
	// the recursion/guardrail features shipped in v5.9.0/v5.10.0,
	// breaking the AGENT.md § Monitoring & Observability rule
	// ("every new feature MUST include monitoring support").
	ChildPRDsTotal int            `json:"child_prds_total,omitempty"` // BL191 Q4: count of PRDs with non-empty ParentPRDID
	MaxDepthSeen   int            `json:"max_depth_seen,omitempty"`   // BL191 Q4: max(PRD.Depth) across the store
	VerdictCounts  map[string]int `json:"verdict_counts,omitempty"`   // BL191 Q6: outcome → count rollup ("pass" / "warn" / "block") across every Story.Verdicts + Task.Verdicts
	BlockedPRDs    int            `json:"blocked_prds,omitempty"`     // BL191 Q6: count of PRDs in PRDBlocked status (guardrail block)

	// BL367 — quality gate observability (surfaced via GET /api/autonomous/status + autonomous_status MCP).
	QualityGateRuns        int `json:"quality_gate_runs,omitempty"`        // tasks with a non-nil QualityGateResult
	QualityGatePass        int `json:"quality_gate_pass,omitempty"`        // gate ran and no regression
	QualityGateFail        int `json:"quality_gate_fail,omitempty"`        // gate ran and regression detected
	QualityGateRegressions int `json:"quality_gate_regressions,omitempty"` // alias for fail for clarity
	QualityGateInvalid     int `json:"quality_gate_invalid,omitempty"`     // invalid baseline; gate warned, never blocked

	// BL382 — cancel/requeue lifecycle operation totals (across all stored PRDs).
	StoriesCancelled int64 `json:"stories_cancelled,omitempty"`
	TasksCancelled   int64 `json:"tasks_cancelled,omitempty"`
	TasksRequeued    int64 `json:"tasks_requeued,omitempty"`

	// Idle-worker nudge observability. NudgesTotal is the sum of every
	// task's NudgeCount; NudgeTasks lists each task that was nudged;
	// NudgesByBackend sums the nudges per backend (the backend the task
	// resolves to: task, story, PRD, then autonomous.execution_backend).
	NudgesTotal     int            `json:"nudges_total,omitempty"`
	NudgeTasks      []TaskNudge    `json:"nudge_tasks,omitempty"`
	NudgesByBackend map[string]int `json:"nudges_by_backend,omitempty"`

	// UsageBackoff reports the live per-backend usage back-off flags and
	// the effective back-off config. Always present in a Manager.Status().
	UsageBackoff *UsageBackoffStatus `json:"usage_backoff,omitempty"`

	// Budgets lists the dollar budget state of every LLM that has a budget.
	// Always an array, empty when none is wired or set.
	Budgets []BudgetStatus `json:"budgets"`

	// Queue lists every queued or capacity-waiting task with the plain-words
	// reason it is waiting (and since when, when it resets, who holds what it
	// needs). Holdings lists every running task with the pools, node and tree
	// lock it holds. Both are omitted when empty.
	Queue    []TaskWaitView `json:"queue,omitempty"`
	Holdings []TaskHoldView `json:"holdings,omitempty"`

	// Warnings are plain-language notices that a planning or verification
	// model does not pair with the backend it is used with, so it will not
	// reach its session. See PairingWarnings.
	Warnings []string `json:"warnings,omitempty"`

	// B165 — boot-resume gate decisions (resumed and parked), most recent
	// boot-resume scan at the end. Empty when boot-resume has never
	// decided anything for a PRDRunning PRD. See BootResumeEntry.
	BootResume []BootResumeEntry `json:"boot_resume,omitempty"`

	// Automations summarises each non-terminal Automation (draft, active,
	// paused or blocked): where its cursor is and how long it has sat idle.
	Automations []AutomationStatusSummary `json:"automations,omitempty"`

	// ReviewNeeded lists every task held for operator review across all
	// Automata, oldest hold first. Omitted when none is held.
	ReviewNeeded []HeldTaskStatus `json:"review_needed,omitempty"`
	// Notify reports the notify-events resolution state: the effective
	// global default tier and which non-finished Automata override it
	// with an automaton- or profile-tier event list. Always present, with
	// both keys as lists (empty, never null).
	Notify *NotifyStatus `json:"notify"`
}

// NotifyStatus is the notify-events block of LoopStatus. DefaultEvents is
// the resolved global tier — the events every Automaton without an
// override falls back to. Overrides lists each non-finished Automaton
// whose effective set came from its own list or its guardrail profile
// (anything sourced from the global tier is not an override and is
// omitted), sorted by Automaton ID and capped at NotifyStatusMaxOverrides
// entries. Both lists are never nil, so they marshal as [] rather than null.
type NotifyStatus struct {
	DefaultEvents []string         `json:"default_events"`
	Overrides     []NotifyOverride `json:"overrides"`
}

// NotifyOverride is one Automaton in NotifyStatus.Overrides whose effective
// notification event kinds came from the automaton or profile tier.
type NotifyOverride struct {
	ID     string   `json:"id"`
	Source string   `json:"source"` // NotifySourceAutomaton or NotifySourceProfile
	Events []string `json:"events"` // the effective event kinds
}

// NotifyStatusMaxOverrides caps the number of per-Automaton overrides the
// status loop reports, so a store with a large fleet cannot bloat it.
const NotifyStatusMaxOverrides = 100

// AutomationStatusSummary is one Automation's row in LoopStatus. SpentUSD,
// CeilingDisplay and Budget are filled when the Automation has a budget policy.
type AutomationStatusSummary struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	CurrentMemberID string `json:"current_member_id,omitempty"`
	CurrentPRDID    string `json:"current_prd_id,omitempty"`
	CurrentPRDTitle string `json:"current_prd_title,omitempty"`
	// PendingMembers counts members not started plus inserts held for the
	// operator's approval.
	PendingMembers int `json:"pending_members"`
	// PendingInserts lists the inserts held for the operator's approval.
	PendingInserts []PendingInsert `json:"pending_inserts,omitempty"`
	IdleSince      *time.Time      `json:"idle_since,omitempty"`
	IdleSeconds    int64           `json:"idle_seconds,omitempty"`
	BlockedReason  string          `json:"blocked_reason,omitempty"`
	SpentUSD       float64         `json:"spent_usd,omitempty"`
	CeilingDisplay string          `json:"ceiling_display,omitempty"`
	// Budget is the computed budget view: spend, window, reset, hold, the
	// stored breach state and the estimated allocation figure.
	Budget *BudgetView `json:"budget,omitempty"`

	// DecisionAssistant is the Automation-level decision assistant setting;
	// absent when the assistant is off.
	DecisionAssistant *DecisionAssistant `json:"decision_assistant,omitempty"`

	// ActiveCursors lists the Automation's live cursors, sorted by member ID;
	// never nil, so an idle Automation reports []. JoinWait is set only while
	// the Automation waits on a join.
	ActiveCursors []ActiveCursorSummary `json:"active_cursors"`
	JoinWait      *JoinWaitSummary      `json:"join_wait,omitempty"`

	// ReviewNeededCount and ReviewNeededTaskIDs report the tasks of the current
	// member Automaton held for operator review. Both are omitted when none is
	// held.
	ReviewNeededCount   int      `json:"review_needed_count,omitempty"`
	ReviewNeededTaskIDs []string `json:"review_needed_task_ids,omitempty"`
}

// ActiveCursorSummary is one live cursor of an Automation in LoopStatus. PRDID
// is set for a PRD member and AutomationID for a nested child Automation.
type ActiveCursorSummary struct {
	MemberID     string `json:"member_id"`
	PRDID        string `json:"prd_id,omitempty"`
	AutomationID string `json:"automation_id,omitempty"`
	Status       string `json:"status"`
}

// JoinWaitSummary describes the join an Automation waits on: the rule it applies,
// the branches still live, and the branches that ended without success.
type JoinWaitSummary struct {
	JoinMemberID    string   `json:"join_member_id"`
	Rule            string   `json:"rule"`
	PendingBranches []string `json:"pending_branches"`
	FailedBranches  []string `json:"failed_branches"`
}

// TaskNudge is one task's idle-worker nudge count in LoopStatus.
type TaskNudge struct {
	PRDID   string `json:"prd_id"`
	StoryID string `json:"story_id,omitempty"`
	TaskID  string `json:"task_id"`
	Backend string `json:"backend,omitempty"`
	Count   int    `json:"count"`
}

// CIWatchResult is an automaton's own record of whether the CI runs for
// the commit it pushed actually passed — so "done" means the remote
// build agreed, not just that the local work finished. Written by the
// ci-watch completion guardrail (off unless "ci-watch" is listed as a
// PRD-level guardrail).
type CIWatchResult struct {
	// Status: skipped | pass | warn | pending | failed_policy | failed_content | failed_infra | error
	Status string `json:"status"`
	// SHA is the pushed commit whose CI runs were watched.
	SHA string `json:"sha,omitempty"`
	// Repo is the remote repository (owner/name) the runs belong to.
	Repo string `json:"repo,omitempty"`
	// Runs are the individual CI runs observed for SHA.
	Runs []CIWatchRun `json:"runs,omitempty"`
	// Summary is a short operator-readable outcome.
	Summary string `json:"summary,omitempty"`
	// Category is the failure category judged against the project's own
	// rules/context file when a run failed.
	Category string `json:"category,omitempty"`
	// Issues lists the specific problems found in the failed runs.
	Issues []string `json:"issues,omitempty"`
	// CheckedAt is when this result was recorded.
	CheckedAt time.Time `json:"checked_at"`
}

// CIWatchRun is one CI run seen for the commit an automaton pushed —
// part of the automaton's own record of whether its CI actually passed.
type CIWatchRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
	URL        string `json:"url,omitempty"`
}
