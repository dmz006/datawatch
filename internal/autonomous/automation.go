package autonomous

import "time"

// AutomationStatus is the lifecycle state of an Automation (see the
// orchestration design doc, "Status lifecycle").
type AutomationStatus string

const (
	AutomationDraft     AutomationStatus = "draft"
	AutomationActive    AutomationStatus = "active"
	AutomationPaused    AutomationStatus = "paused"
	AutomationCompleted AutomationStatus = "completed"
	AutomationBlocked   AutomationStatus = "blocked"
	AutomationCancelled AutomationStatus = "cancelled"
)

// MemberStatus is the lifecycle state of one member Automaton within an
// Automation.
type MemberStatus string

const (
	MemberPending   MemberStatus = "pending"   // not started (UI derives "approved" from the PRD)
	MemberRunning   MemberStatus = "running"   // the cursor is here; PRD running/paused
	MemberCompleted MemberStatus = "completed" // PRD completed; OnComplete followed
	MemberBlocked   MemberStatus = "blocked"   // PRD blocked (or cancelled/reverted, see edge mapping)
	MemberFailed    MemberStatus = "failed"
	MemberSkipped   MemberStatus = "skipped"   // operator skip_member
	MemberCancelled MemberStatus = "cancelled" // Automation cancelled before it ran
)

// Automation is a graph of member Automations (PRDs) advanced by a single
// cursor, with per-outcome edges and an optional budget policy.
type Automation struct {
	ID              string             `json:"id"` // 8-hex, like PRD.ID
	Name            string             `json:"name"`
	Description     string             `json:"description,omitempty"`
	Status          AutomationStatus   `json:"status"`
	EntryMemberID   string             `json:"entry_member_id"`
	CurrentMemberID string             `json:"current_member_id,omitempty"` // the single cursor
	Members         []AutomationMember `json:"members"`
	Version         int64              `json:"version"` // bumped on every write; optimistic concurrency

	BudgetPolicy *BudgetPolicy `json:"budget_policy,omitempty"`

	// Execution defaults — new tier between PRD and global.
	Backend string `json:"backend,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  Effort `json:"effort,omitempty"`
	// Verification defaults — new tier between Automaton (PRD) and global.
	VerificationBackend string `json:"verification_backend,omitempty"`
	VerificationModel   string `json:"verification_model,omitempty"`
	VerificationEffort  Effort `json:"verification_effort,omitempty"`

	ProjectDir string `json:"project_dir,omitempty"` // informational; BL428 3a keys on each member's own tree

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	IdleSince  *time.Time `json:"idle_since,omitempty"` // set when a member finishes and no next member started
	Decisions  []Decision `json:"decisions,omitempty"`  // same type as PRD.Decisions; same 200 trim
}

// AutomationMember is one member Automaton (PRD) of an Automation, with
// per-outcome edges instead of a flat ordinal: a graph, not a list.
// Each outcome edge names at most one next member, so the Automation has
// exactly one cursor at a time; a linear Automation is the trivial case.
type AutomationMember struct {
	ID    string `json:"id"`     // 8-hex, stable across edits
	PRDID string `json:"prd_id"` // the member Automaton

	// Per-outcome edges: another member ID, or "" = stop (see edge semantics).
	OnComplete string `json:"on_complete,omitempty"`
	OnBlocked  string `json:"on_blocked,omitempty"`
	OnFailed   string `json:"on_failed,omitempty"`

	AddedAt     time.Time `json:"added_at"`
	AddedBy     string    `json:"added_by"`               // "operator" | "session:<id>" | "agent:<id>" | "import:bl117:<graph>"
	AddedReason string    `json:"added_reason,omitempty"` // required for inserts into a non-draft Automation

	Status     MemberStatus `json:"status"`
	StartedAt  *time.Time   `json:"started_at,omitempty"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
	Outcome    string       `json:"outcome,omitempty"`    // PRD status observed at finish
	EdgeTaken  string       `json:"edge_taken,omitempty"` // on_complete|on_blocked|on_failed|skip

	BudgetPolicy *BudgetPolicy `json:"budget_policy,omitempty"` // explicit member override (narrows further)
}

// BudgetPolicy is a narrower cost ceiling scoped to an Automation or one of
// its members; it never widens the global ceiling, only narrows it.
type BudgetPolicy struct {
	CeilingType  string  `json:"ceiling_type"`  // "dollar" | "allocation_pct"
	CeilingValue float64 `json:"ceiling_value"` // USD, or percent (0-100) of the window's allocation
	Window       string  `json:"window"`        // "automation" (lifetime) | "day" | "week" | "month" — BL428 llm.<name>.budget windows + lifetime
	Pacing       string  `json:"pacing"`        // "none" | "even"
	OnBreach     string  `json:"on_breach"`     // "pause" (default) | "block"
	Backend      string  `json:"backend,omitempty"`
}
