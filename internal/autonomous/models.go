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
	PRDDecomposing     PRDStatus = "decomposing"      // BL191 (v5.2.0) — Decompose call in flight
	PRDNeedsReview     PRDStatus = "needs_review"     // BL191 — decomposed; awaiting operator review/edit
	PRDApproved        PRDStatus = "approved"         // BL191 — operator approved; Run is allowed
	PRDRevisionsAsked  PRDStatus = "revisions_asked"  // BL191 — operator requested re-decomposition
	PRDActive          PRDStatus = "active"           // legacy alias kept for back-compat with v5.1.x stores; new code uses PRDRunning
	PRDRunning         PRDStatus = "running"          // BL191 — Run is in flight
	PRDCompleted       PRDStatus = "completed"
	PRDRejected        PRDStatus = "rejected"         // BL191 — operator rejected the decomposition; PRD is dead
	PRDCancelled       PRDStatus = "cancelled"        // BL191 — operator cancelled an in-flight Run
	PRDArchived        PRDStatus = "archived"
)

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
	StoryPending    StoryStatus = "pending"
	StoryInProgress StoryStatus = "in_progress"
	StoryCompleted  StoryStatus = "completed"
	StoryBlocked    StoryStatus = "blocked"
	StoryFailed     StoryStatus = "failed"
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
	Backend    string    `json:"backend,omitempty"`     // PRD-level worker LLM (default for tasks; tasks override per-task)
	Effort     Effort    `json:"effort,omitempty"`
	Model      string    `json:"model,omitempty"`       // BL203 (v5.4.0) — PRD-level model name (e.g., "claude-3-5-sonnet"); tasks may override
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
}
