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
	Kind           string    `json:"kind"` // decompose | run | verify | approve | reject | request_revision | edit_task | template_instantiate | ci_watch | boot_reconcile | verifying_resolution
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
	// TaskAwaitingApproval (BL406 Phase 5 — B111's real fix): set when
	// PRD.GuidedModeSource is "operator" and the task would otherwise
	// be ready to launch. The executor's flattenTasks skips tasks in
	// this state (same mechanism StoryAwaitingApproval already uses at
	// story granularity); ApproveTask transitions it back to pending.
	TaskAwaitingApproval TaskStatus = "awaiting_approval"
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

// PRD is the operator-supplied feature description.
type PRD struct {
	ID         string `json:"id"`   // 8-hex
	Spec       string `json:"spec"` // raw operator input
	Title      string `json:"title,omitempty"`
	ProjectDir string `json:"project_dir"`
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
	// LSPLanguage (v8.7.1) — language server preset for OpenCode-backed workers.
	// Applies to all tasks unless overridden at the task level.
	LSPLanguage string `json:"lsp_language,omitempty"`
	// PermissionMode (v5.27.5) — claude-code permission-mode flag
	// applied to every task in this PRD that doesn't override it.
	// Set to "plan" to make the whole PRD a design-only walk
	// (claude won't write files); leave empty to use the session
	// default. Per-task value takes precedence.
	PermissionMode string    `json:"permission_mode,omitempty"`
	Status         PRDStatus `json:"status"`
	// BlockedReason is a plain sentence saying which guardrail verdict
	// blocked the automaton and which judge session produced it. Empty
	// unless Status is blocked by a guardrail.
	BlockedReason string    `json:"blocked_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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
	Type   string   `json:"type,omitempty"`
	Skills []string `json:"skills,omitempty"`

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
}

// GitWorkflow (BL407) is a PRD's branch-per-PRD + auto-PR workflow
// state. Provider/URL resolve from PRD.ProjectProfile's GitSpec when
// set, else from the worktree's own `origin` remote (the self-build/
// dogfooding case, where a PRD has no ProjectProfile at all).
// BaseBranch is an optional operator override; empty means "the base
// repo's current default branch" (resolved at worktree-creation
// time, not stored until then). Branch and PRURL are populated by the
// executor/completion hook — not operator-set fields.
// PushStatePushed / PushStateNotPushed — the two values a PRD's
// GitWorkflow.PushState can take. "" (zero value) means "no push outcome
// recorded yet". Old stored PRDs that predate push-on-complete load
// with PushState=="" unchanged.
const (
	PushStatePushed    = "pushed"
	PushStateNotPushed = "not_pushed"
)

type GitWorkflow struct {
	Provider   string `json:"provider,omitempty"`
	URL        string `json:"url,omitempty"`
	BaseBranch string `json:"base_branch,omitempty"`
	Branch     string `json:"branch,omitempty"`
	AutoPR     bool   `json:"auto_pr,omitempty"`
	PRURL      string `json:"pr_url,omitempty"`
	// PushOnComplete is the per-PRD override of the daemon's git.push_on_complete
	// policy ("", "never", "branch", "default_branch"). "" = use the daemon
	// setting (see Manager.effectivePushPolicy).
	PushOnComplete string     `json:"push_on_complete,omitempty"`
	PushState      string     `json:"push_state,omitempty"` // "" | pushed(PushStatePushed) | not_pushed(PushStateNotPushed)
	PushedRef      string     `json:"pushed_ref,omitempty"` // ref last successfully pushed
	PushedSHA      string     `json:"pushed_sha,omitempty"` // HEAD SHA last successfully pushed
	PushedAt       *time.Time `json:"pushed_at,omitempty"`  // when the last successful push landed
	// PushWarning is the human reason shown when PushState is not_pushed;
	// cleared on a later successful push.
	PushWarning string `json:"push_warning,omitempty"`
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
}

// Task is a single unit of work — runs as a session under
// session.Manager. Maps 1:1 to a pipeline.Task once enqueued.
//
// BL203 (v5.4.0) — flexible LLM selection at the task level. Backend /
// Effort / Model fields override story-level defaults when set. Most-
// specific wins:
//
//	per-task → per-story (BL381) → per-PRD → per-stage (autonomous.{planning,verification}_backend) → global session.llm_backend
type Task struct {
	ID           string              `json:"id"`
	StoryID      string              `json:"story_id"`
	PRDID        string              `json:"prd_id"`
	Title        string              `json:"title"`
	Spec         string              `json:"spec"` // detailed task description for the worker
	Status       TaskStatus          `json:"status"`
	DependsOn    []string            `json:"depends_on,omitempty"`  // other Task IDs
	SessionID    string              `json:"session_id,omitempty"`  // datawatch session ID once running
	WaitReason   string              `json:"wait_reason,omitempty"` // why the task is waiting_capacity
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
	// CleanRestart — set by an operator reset (ResetTaskWithOptions with
	// cleanRestart=true) to ask that the next attempt start from the pre-task
	// tree instead of restoring the last preserved attempt work (AttemptRefs).
	// The spawn path skips the restore for that one attempt and clears the flag
	// once the attempt has run, so automatic retries after it restore again.
	// Default false: restore stays on.
	CleanRestart bool `json:"clean_restart,omitempty"`
	// FailureReason (BL410 Phase 1) — structured, categorized detail
	// alongside Error (a bare string, left untouched for existing
	// readers). Set on the task's last failed/blocked attempt.
	FailureReason *FailureReason `json:"failure_reason,omitempty"`
	// Per-task LLM overrides — empty = inherit from PRD then global.
	Backend string `json:"backend,omitempty"`
	Effort  Effort `json:"effort,omitempty"`
	Model   string `json:"model,omitempty"`
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

	// BL367 — quality gate result recorded after each task completes.
	// Nil when quality gates are disabled for this PRD.
	QualityGateResult *pipeline.QualityGateResult `json:"quality_gate_result,omitempty"`

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
	// "story", "prd" (the Automaton level) or "global" per
	// ResolveVerificationLLM, or "pool" when the backend was picked
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
	BlockOnFileDrift *bool     `json:"block_on_file_drift,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
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

	// BL382 — cancel/requeue lifecycle operation totals (across all stored PRDs).
	StoriesCancelled int64 `json:"stories_cancelled,omitempty"`
	TasksCancelled   int64 `json:"tasks_cancelled,omitempty"`
	TasksRequeued    int64 `json:"tasks_requeued,omitempty"`

	// Warnings are plain-language notices that a planning or verification
	// model does not pair with the backend it is used with, so it will not
	// reach its session. See PairingWarnings.
	Warnings []string `json:"warnings,omitempty"`
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
