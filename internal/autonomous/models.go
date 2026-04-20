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
	PRDDraft     PRDStatus = "draft"
	PRDActive    PRDStatus = "active"
	PRDCompleted PRDStatus = "completed"
	PRDArchived  PRDStatus = "archived"
)

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
	Backend    string    `json:"backend,omitempty"`     // override
	Effort     Effort    `json:"effort,omitempty"`
	Status     PRDStatus `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Story      []Story   `json:"stories,omitempty"` // populated after Decompose
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
