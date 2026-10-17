package autonomous

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

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

// automationHasCycle reports whether the member graph reachable from the
// entry member contains a cycle, following all three edge kinds
// (OnComplete, OnBlocked, OnFailed). It is a three-colour DFS keyed by
// member ID (white = unseen, grey = on the current path, black = explored);
// a back-edge to a grey node is a cycle, and the member IDs on the cycle
// path are returned for diagnostics. Edge order is fixed, so the result is
// deterministic.
func automationHasCycle(a *Automation) (bool, []string) {
	byID := make(map[string]AutomationMember, len(a.Members))
	for _, m := range a.Members {
		byID[m.ID] = m
	}

	const (
		white = iota // unseen
		grey         // on the current DFS path
		black        // fully explored
	)
	color := make(map[string]int, len(a.Members))
	var path []string

	var dfs func(id string) (bool, []string)
	dfs = func(id string) (bool, []string) {
		color[id] = grey
		path = append(path, id)
		m := byID[id]
		for _, target := range []string{m.OnComplete, m.OnBlocked, m.OnFailed} {
			if target == "" {
				continue
			}
			switch color[target] {
			case grey: // back-edge: the cycle is the path from target to id
				i := 0
				for i < len(path) && path[i] != target {
					i++
				}
				return true, append([]string{}, path[i:]...)
			case white:
				if found, cycle := dfs(target); found {
					return true, cycle
				}
			}
		}
		color[id] = black
		path = path[:len(path)-1]
		return false, nil
	}

	return dfs(a.EntryMemberID)
}

// automationUnreachable returns the IDs of members not reachable from the
// entry member following any non-empty edge (OnComplete, OnBlocked or
// OnFailed), sorted for determinism. Drafts may have unreachable members;
// validateAutomation rejects them only on non-draft Automations, and callers
// surface them as warnings on drafts.
func automationUnreachable(a *Automation) []string {
	if a.EntryMemberID == "" {
		return nil
	}
	byID := make(map[string][]string, len(a.Members))
	for _, m := range a.Members {
		var targets []string
		for _, t := range []string{m.OnComplete, m.OnBlocked, m.OnFailed} {
			if t != "" {
				targets = append(targets, t)
			}
		}
		byID[m.ID] = targets
	}

	reached := map[string]bool{a.EntryMemberID: true}
	stack := []string{a.EntryMemberID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, next := range byID[id] {
			if !reached[next] {
				reached[next] = true
				stack = append(stack, next)
			}
		}
	}

	var unreachable []string
	for _, m := range a.Members {
		if !reached[m.ID] {
			unreachable = append(unreachable, m.ID)
		}
	}
	sort.Strings(unreachable)
	return unreachable
}

// validateAutomation checks the structural invariants of an Automation,
// including the cycle and reachability checks (design doc "Validation"); PRD
// ownership is enforced by the store.
func validateAutomation(a *Automation) error {
	if a == nil || strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("automation invalid: nil Automation or blank name")
	}
	switch a.Status {
	case AutomationDraft, AutomationActive, AutomationPaused,
		AutomationCompleted, AutomationBlocked, AutomationCancelled:
	default:
		return fmt.Errorf("automation invalid: unknown status %q", a.Status)
	}
	memberIDs := make(map[string]struct{}, len(a.Members))
	prdIDs := make(map[string]struct{}, len(a.Members))
	for _, m := range a.Members {
		if strings.TrimSpace(m.ID) == "" {
			return fmt.Errorf("automation invalid: member with empty ID")
		}
		if strings.TrimSpace(m.PRDID) == "" {
			return fmt.Errorf("automation invalid: member %s has empty PRDID", m.ID)
		}
		if _, dup := memberIDs[m.ID]; dup {
			return fmt.Errorf("automation invalid: duplicate member ID %s", m.ID)
		}
		if _, dup := prdIDs[m.PRDID]; dup {
			return fmt.Errorf("automation invalid: PRD %s is a member more than once", m.PRDID)
		}
		memberIDs[m.ID] = struct{}{}
		prdIDs[m.PRDID] = struct{}{}
	}
	if len(a.Members) > 0 && a.EntryMemberID == "" {
		return fmt.Errorf("automation invalid: has members but empty entry member ID")
	}
	if a.EntryMemberID != "" {
		if _, found := memberIDs[a.EntryMemberID]; !found {
			return fmt.Errorf("automation invalid: entry member %s is not a member", a.EntryMemberID)
		}
	}
	for _, m := range a.Members {
		for name, target := range map[string]string{
			"on_complete": m.OnComplete,
			"on_blocked":  m.OnBlocked,
			"on_failed":   m.OnFailed,
		} {
			if target == "" {
				continue
			}
			if target == m.ID {
				return fmt.Errorf("automation invalid: member %s %s edge points to itself", m.ID, name)
			}
			if _, found := memberIDs[target]; !found {
				return fmt.Errorf("automation invalid: member %s %s edge names unknown member %s", m.ID, name, target)
			}
		}
	}
	for _, m := range a.Members {
		if err := validateBudgetPolicy(m.BudgetPolicy); err != nil {
			return fmt.Errorf("automation invalid: member %s: %w", m.ID, err)
		}
	}
	if err := validateBudgetPolicy(a.BudgetPolicy); err != nil {
		return err
	}
	if len(a.Members) > 0 {
		if _, cycle := automationHasCycle(a); cycle != nil {
			return fmt.Errorf("automation invalid: cycle detected via members [%s]", strings.Join(cycle, " "))
		}
		if unreachable := automationUnreachable(a); len(unreachable) > 0 && a.Status != AutomationDraft {
			return fmt.Errorf("automation invalid: members unreachable from entry [%s]", strings.Join(unreachable, " "))
		}
	}
	return nil
}

// validateBudgetPolicy checks one budget policy's fields; a nil policy is
// valid (no override).
func validateBudgetPolicy(p *BudgetPolicy) error {
	if p == nil {
		return nil
	}
	switch p.CeilingType {
	case "dollar", "allocation_pct":
	default:
		return fmt.Errorf("automation invalid: budget ceiling type %q", p.CeilingType)
	}
	if p.CeilingValue <= 0 {
		return fmt.Errorf("automation invalid: budget ceiling %v must be positive", p.CeilingValue)
	}
	switch p.Window {
	case "automation", "day", "week", "month":
	default:
		return fmt.Errorf("automation invalid: budget window %q", p.Window)
	}
	switch p.Pacing {
	case "", "none", "even":
	default:
		return fmt.Errorf("automation invalid: budget pacing %q", p.Pacing)
	}
	switch p.OnBreach {
	case "", "pause", "block":
	default:
		return fmt.Errorf("automation invalid: budget on_breach %q", p.OnBreach)
	}
	return nil
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
