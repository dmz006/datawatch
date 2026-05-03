// Package autonomous (BL24 + BL25, Sprint S6 → v3.10.0) implements
// LLM-driven PRD → Stories → Tasks decomposition with independent
// verification (BL25). Models are deliberately close to nightwire's
// Pydantic schema (HackingDave/nightwire/nightwire/autonomous/models.py)
// so cross-tool memory + SIEM ingestion can interoperate.

package autonomous

import "time"

// Status enums — kept in sync with nightwire for cross-tool compat.

type PRDStatus string

const (
	PRDDraft           PRDStatus = "draft"
	PRDPlanning        PRDStatus = "planning"         // BL221 (v6.2.0) — Decompose call in flight; renamed from PRDDecomposing
	PRDDecomposing               = PRDPlanning        // back-compat alias; old stored value "decomposing" normalized on load
	PRDNeedsReview     PRDStatus = "needs_review"     // BL191 — decomposed; awaiting operator review/edit
	PRDApproved        PRDStatus = "approved"         // BL191 — operator approved; Run is allowed
	PRDRevisionsAsked  PRDStatus = "revisions_asked"  // BL191 — operator requested re-decomposition
	PRDActive          PRDStatus = "active"           // legacy alias kept for back-compat with v5.1.x stores; new code uses PRDRunning
	PRDRunning         PRDStatus = "running"          // BL191 — Run is in flight
	PRDCompleted       PRDStatus = "completed"
	PRDRejected        PRDStatus = "rejected"         // BL191 — operator rejected the decomposition; PRD is dead
	PRDCancelled       PRDStatus = "cancelled"        // BL191 — operator cancelled an in-flight Run
	PRDArchived        PRDStatus = "archived"
	PRDBlocked         PRDStatus = "blocked"          // BL191 Q6 — a guardrail returned `block`; awaits operator action
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
	At            time.Time `json:"at"`
	Kind          string    `json:"kind"`            // decompose | run | verify | approve | reject | request_revision | edit_task | template_instantiate
	Backend       string    `json:"backend,omitempty"`
	Model         string    `json:"model,omitempty"`
	PromptChars   int       `json:"prompt_chars,omitempty"`
	ResponseChars int       `json:"response_chars,omitempty"`
	CostUSD       float64   `json:"cost_usd,omitempty"`
	VerdictOutcome string   `json:"verdict_outcome,omitempty"` // pass | warn | block | n/a
	Actor         string    `json:"actor,omitempty"`           // operator | autonomous | verifier
	Note          string    `json:"note,omitempty"`            // free-form: what was edited, why rejected, etc.
}

type StoryStatus string

const (
	StoryPending           StoryStatus = "pending"
	StoryAwaitingApproval  StoryStatus = "awaiting_approval" // Phase 3 (v5.26.60)
	StoryInProgress        StoryStatus = "in_progress"
	StoryCompleted         StoryStatus = "completed"
	StoryBlocked           StoryStatus = "blocked"
	StoryFailed            StoryStatus = "failed"
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
	ID         string    `json:"id"`           // 8-hex
	Spec       string    `json:"spec"`         // raw operator input
	Title      string    `json:"title,omitempty"`
	ProjectDir string    `json:"project_dir"`
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
	// Phase 3 (v5.26.60) — explicit decomposition profile distinct
	// from the default execution profile. Empty falls back to the
	// global autonomous.decomposition_backend config knob. The
	// existing ProjectProfile field is re-purposed as the *default
	// execution profile* for stories that don't override.
	DecompositionProfile string `json:"decomposition_profile,omitempty"`
	Backend    string    `json:"backend,omitempty"`     // PRD-level worker LLM (default for tasks; tasks override per-task)
	Effort     Effort    `json:"effort,omitempty"`
	Model      string    `json:"model,omitempty"`       // BL203 (v5.4.0) — PRD-level model name (e.g., "claude-3-5-sonnet"); tasks may override
	// PermissionMode (v5.27.5) — claude-code permission-mode flag
	// applied to every task in this PRD that doesn't override it.
	// Set to "plan" to make the whole PRD a design-only walk
	// (claude won't write files); leave empty to use the session
	// default. Per-task value takes precedence.
	PermissionMode string `json:"permission_mode,omitempty"`
	Status     PRDStatus `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Story      []Story   `json:"stories,omitempty"` // populated after Decompose

	// BL191 Q1 (v5.2.0) — review/approve gate. Manager.Run refuses
	// unless Status == PRDApproved (or the legacy PRDActive for back-
	// compat with pre-v5.2.0 stores). Operators transition via
	// POST /api/autonomous/prds/{id}/{approve|reject|request_revision}.
	ApprovedBy        string    `json:"approved_by,omitempty"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
	RejectionReason   string    `json:"rejection_reason,omitempty"`
	RevisionsRequested int       `json:"revisions_requested,omitempty"` // count of request_revision calls

	// BL191 Q3 — append-only per-PRD audit timeline.
	Decisions []Decision `json:"decisions,omitempty"`

	// BL191 Q2 — template flag + variable schema. When IsTemplate is
	// true the PRD is a reusable shape rather than an executable plan;
	// instantiating with POST /api/autonomous/prds/{id}/instantiate
	// substitutes {{var}} markers in spec/title/task specs and stores
	// a fresh executable PRD.
	IsTemplate   bool                `json:"is_template,omitempty"`
	TemplateVars []TemplateVar       `json:"template_vars,omitempty"`
	TemplateOf   string              `json:"template_of,omitempty"` // ID of the source template; only set on instantiated PRDs

	// BL191 Q4 (v5.9.0) — recursion. ParentPRDID is the PRD whose task
	// spawned this one. ParentTaskID is that specific task. Depth is
	// the recursion depth (root PRDs are 0). Manager.recurse refuses to
	// spawn a child when Parent.Depth + 1 > Config.MaxRecursionDepth so
	// a runaway decomposition can't tank the daemon.
	ParentPRDID  string `json:"parent_prd_id,omitempty"`
	ParentTaskID string `json:"parent_task_id,omitempty"`
	Depth        int    `json:"depth,omitempty"`

	// BL221 (v6.2.0) Phase 4 — type extensibility, Guided Mode, skills.
	// Type is a well-known or operator-registered automaton type identifier
	// (e.g., "software", "research", "operational", "personal").
	// GuidedMode enables step-by-step operator checkpoints during decomposition.
	// Skills is the list of skill IDs assigned to this automaton; passed to
	// spawn requests so workers can load the appropriate skill context.
	Type        string   `json:"type,omitempty"`
	GuidedMode  bool     `json:"guided_mode,omitempty"`
	Skills      []string `json:"skills,omitempty"`
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
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Spec        string       `json:"spec"`
	Type        string       `json:"type,omitempty"` // software | research | operational | personal
	Tags        []string     `json:"tags,omitempty"`
	Vars        []TemplateVar `json:"vars,omitempty"`
	IsBuiltin   bool         `json:"is_builtin,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	LastUsedAt  *time.Time   `json:"last_used_at,omitempty"`
	UseCount    int          `json:"use_count,omitempty"`
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
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
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

	// Phase 4 (v5.26.64) — file association. FilesPlanned is
	// LLM-extracted at decompose time (the decomposer prompt asks
	// for `files: [...]` per story); operator can edit via the
	// story-edit modal. Empty when the decomposer omitted them.
	FilesPlanned []string `json:"files,omitempty"`
}

// Task is a single unit of work — runs as a session under
// session.Manager. Maps 1:1 to a pipeline.Task once enqueued.
//
// BL203 (v5.4.0) — flexible LLM selection at the task level. Backend /
// Effort / Model fields override PRD-level defaults when set. Most-
// specific wins:
//   per-task → per-PRD → per-stage (autonomous.{decomposition,verification}_backend) → global session.llm_backend
type Task struct {
	ID            string     `json:"id"`
	StoryID       string     `json:"story_id"`
	PRDID         string     `json:"prd_id"`
	Title         string     `json:"title"`
	Spec          string     `json:"spec"` // detailed task description for the worker
	Status        TaskStatus `json:"status"`
	DependsOn     []string   `json:"depends_on,omitempty"` // other Task IDs
	SessionID     string     `json:"session_id,omitempty"` // datawatch session ID once running
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Error         string     `json:"error,omitempty"`
	Verification  *VerificationResult `json:"verification,omitempty"`
	RetryCount    int        `json:"retry_count,omitempty"`
	// Per-task LLM overrides — empty = inherit from PRD then global.
	Backend       string     `json:"backend,omitempty"`
	Effort        Effort     `json:"effort,omitempty"`
	Model         string     `json:"model,omitempty"`
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

	// BL191 Q6 (v5.10.0) — guardrail verdicts at the task level.
	// Populated when Config.PerTaskGuardrails is non-empty; one entry
	// per guardrail named in that list. Block on any block outcome.
	Verdicts []GuardrailVerdict `json:"verdicts,omitempty"`

	// Phase 4 (v5.26.64) — file association. FilesPlanned is
	// LLM-extracted at decompose time (operator-editable). FilesTouched
	// is populated post-spawn from the worker session's git diff
	// --name-only. Both are advisory — empty doesn't block execution.
	FilesPlanned []string `json:"files,omitempty"`
	FilesTouched []string `json:"files_touched,omitempty"`
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
}

// Learning is a post-task insight extracted by the loop and saved
// into datawatch memory (BL57 KG + BL23 episodic). Kept here purely
// for the autonomous-internal record; the source of truth is memory.
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
	Running       bool      `json:"running"`
	ActivePRDs    int       `json:"active_prds"`
	QueuedTasks   int       `json:"queued_tasks"`
	RunningTasks  int       `json:"running_tasks"`
	LastTickAt    time.Time `json:"last_tick_at,omitempty"`
	StaleTasks    int       `json:"stale_tasks,omitempty"`

	// v5.22.0 — observability fill-in for BL191 Q4 + Q6 (audit
	// follow-up). Pre-v5.22.0 the LoopStatus carried no signal for
	// the recursion/guardrail features shipped in v5.9.0/v5.10.0,
	// breaking the AGENT.md § Monitoring & Observability rule
	// ("every new feature MUST include monitoring support").
	ChildPRDsTotal      int            `json:"child_prds_total,omitempty"`           // BL191 Q4: count of PRDs with non-empty ParentPRDID
	MaxDepthSeen        int            `json:"max_depth_seen,omitempty"`             // BL191 Q4: max(PRD.Depth) across the store
	VerdictCounts       map[string]int `json:"verdict_counts,omitempty"`             // BL191 Q6: outcome → count rollup ("pass" / "warn" / "block") across every Story.Verdicts + Task.Verdicts
	BlockedPRDs         int            `json:"blocked_prds,omitempty"`               // BL191 Q6: count of PRDs in PRDBlocked status (guardrail block)
}
