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

// Member kinds (AutomationMember.Kind). The empty kind means the same as
// MemberKindPRD: a single PRD member.
const (
	MemberKindPRD        = "prd"
	MemberKindAutomation = "automation"
	MemberKindFork       = "fork"
	MemberKindJoin       = "join"
)

// Branch failure rules for a join (AutomationMember.OnBranchFailure). The
// empty value means the same as BranchAllMustSucceed.
const (
	BranchAllMustSucceed = "all_must_succeed"
	BranchAnyOneIsEnough = "any_one_is_enough"
	BranchContinueAnyway = "continue_anyway"
)

// Automation is a graph of member Automations (PRDs) advanced by a single
// cursor, with per-outcome edges and an optional budget policy.
type Automation struct {
	ID              string             `json:"id"` // 8-hex, like PRD.ID
	Name            string             `json:"name"`
	Description     string             `json:"description,omitempty"`
	Status          AutomationStatus   `json:"status"`
	EntryMemberID   string             `json:"entry_member_id"`
	CurrentMemberID string             `json:"current_member_id,omitempty"` // deprecated: the single cursor of a linear Automation
	ActiveCursors   []string           `json:"active_cursors,omitempty"`    // member IDs in flight, sorted and unique
	Members         []AutomationMember `json:"members"`
	Version         int64              `json:"version"` // bumped on every write; optimistic concurrency

	BudgetPolicy *BudgetPolicy `json:"budget_policy,omitempty"`

	// Execution defaults — new tier between PRD and global.
	Backend string `json:"backend,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  Effort `json:"effort,omitempty"`
	// Planning default for member decomposition, separate from the run and
	// verify defaults; unset means the global planning backend.
	PlanningBackend string `json:"planning_backend,omitempty"`
	PlanningModel   string `json:"planning_model,omitempty"`
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

	// PendingInserts are member inserts held for an operator's approval (agent
	// inserts under agent_insert_policy=require_operator, and any insert into a
	// non-draft Automation). They are not members yet and do not affect the graph.
	PendingInserts []PendingInsert `json:"pending_inserts,omitempty"`
}

// AutomationMember is one member Automation (PRD) of an Automation, with
// per-outcome edges instead of a flat ordinal: a graph, not a list.
// Each outcome edge names at most one next member, so a linear Automation has
// exactly one cursor at a time; a linear Automation is the trivial case.
type AutomationMember struct {
	ID    string `json:"id"`     // 8-hex, stable across edits
	PRDID string `json:"prd_id"` // the member Automaton

	// Per-outcome edges: another member ID, or "" = stop (see edge semantics).
	OnComplete string `json:"on_complete,omitempty"`
	OnBlocked  string `json:"on_blocked,omitempty"`
	OnFailed   string `json:"on_failed,omitempty"`

	// Member kind and the fields each kind allows (Phase 3a fork/join and
	// nesting). Only the kind's own fields may be set; see validateMemberKind.
	Kind            string   `json:"kind,omitempty"`
	AutomationID    string   `json:"automation_id,omitempty"`     // kind=automation: the child Automation ID
	ForkMembers     []string `json:"fork_members,omitempty"`      // kind=fork: branch member IDs (at least 2)
	JoinMembers     []string `json:"join_members,omitempty"`      // kind=join: branch member IDs, equal to the paired fork's
	OnBranchFailure string   `json:"on_branch_failure,omitempty"` // kind=join: all_must_succeed (default) | any_one_is_enough | continue_anyway

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

// kind returns the member's kind, treating the empty kind as MemberKindPRD.
func (m AutomationMember) kind() string {
	if m.Kind == "" {
		return MemberKindPRD
	}
	return m.Kind
}

// automationIsLinear reports whether a has no fork, join or nested member:
// the single-cursor shape that CurrentMemberID describes.
func automationIsLinear(a *Automation) bool {
	for _, m := range a.Members {
		switch m.kind() {
		case MemberKindFork, MemberKindJoin, MemberKindAutomation:
			return false
		}
	}
	return true
}

// sortedUniqueStrings returns a sorted copy of in with duplicates removed, or
// nil when nothing remains (so omitempty drops the field).
func sortedUniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	n := 0
	for _, v := range out {
		if n == 0 || v != out[n-1] {
			out[n] = v
			n++
		}
	}
	return out[:n]
}

// normalizeActiveCursors brings a's cursor set to its canonical form before it
// is validated and persisted. A linear Automation keeps its cursor in
// CurrentMemberID (the single-cursor advance still reads and writes that
// field), so its set is derived from it and never edited on its own. A
// multi-cursor Automation keeps ActiveCursors sorted and unique.
func normalizeActiveCursors(a *Automation) {
	if automationIsLinear(a) {
		a.ActiveCursors = nil
		if a.CurrentMemberID != "" {
			a.ActiveCursors = []string{a.CurrentMemberID}
		}
		return
	}
	a.ActiveCursors = sortedUniqueStrings(a.ActiveCursors)
}

// automationSuccessors maps each member ID to the member IDs it leads to: its
// non-empty outcome edges, a fork's branches, and a branch's paired join. The
// fork/join pairing is walked here so the cycle and reachability checks see the
// same graph the advance rule follows (design doc "Validation addenda" rule 8).
// Edge order is fixed, so results are deterministic.
func automationSuccessors(a *Automation) map[string][]string {
	pairedJoin := make(map[string]string)
	for _, m := range a.Members {
		if m.kind() != MemberKindJoin {
			continue
		}
		for _, b := range m.JoinMembers {
			pairedJoin[b] = m.ID
		}
	}
	out := make(map[string][]string, len(a.Members))
	for _, m := range a.Members {
		var next []string
		for _, t := range []string{m.OnComplete, m.OnBlocked, m.OnFailed} {
			if t != "" {
				next = append(next, t)
			}
		}
		if m.kind() == MemberKindFork {
			next = append(next, m.ForkMembers...)
		}
		if j, ok := pairedJoin[m.ID]; ok {
			next = append(next, j)
		}
		out[m.ID] = next
	}
	return out
}

// automationHasCycle reports whether the member graph reachable from the
// entry member contains a cycle, following every successor edge (outcome
// edges, fork branches and branch-to-join pairings; see automationSuccessors).
// It is a three-colour DFS keyed by member ID (white = unseen, grey = on the
// current path, black = explored); a back-edge to a grey node is a cycle, and
// the member IDs on the cycle path are returned for diagnostics. Edge order is
// fixed, so the result is deterministic.
func automationHasCycle(a *Automation) (bool, []string) {
	succ := automationSuccessors(a)

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
		for _, target := range succ[id] {
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
// entry member following any successor edge (see automationSuccessors), sorted
// for determinism. Drafts may have unreachable members; validateAutomation
// rejects them only on non-draft Automations, and callers surface them as
// warnings on drafts.
func automationUnreachable(a *Automation) []string {
	if a.EntryMemberID == "" {
		return nil
	}
	succ := automationSuccessors(a)

	reached := map[string]bool{a.EntryMemberID: true}
	stack := []string{a.EntryMemberID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, next := range succ[id] {
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
// including the fork/join addenda, the in-Automation cycle and reachability
// checks (design doc "Validation"); PRD ownership is enforced by the store, and
// nested-Automation ownership and cross-level cycles by validateAutomationNesting.
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
	byID := make(map[string]AutomationMember, len(a.Members))
	for _, m := range a.Members {
		if strings.TrimSpace(m.ID) == "" {
			return fmt.Errorf("automation invalid: member with empty ID")
		}
		if err := validateMemberKind(a, m); err != nil {
			return err
		}
		if m.kind() == MemberKindPRD && strings.TrimSpace(m.PRDID) == "" {
			return fmt.Errorf("automation invalid: member %s has empty PRDID", m.ID)
		}
		if _, dup := memberIDs[m.ID]; dup {
			return fmt.Errorf("automation invalid: duplicate member ID %s", m.ID)
		}
		memberIDs[m.ID] = struct{}{}
		byID[m.ID] = m
		if m.kind() == MemberKindPRD {
			if _, dup := prdIDs[m.PRDID]; dup {
				return fmt.Errorf("automation invalid: PRD %s is a member more than once", m.PRDID)
			}
			prdIDs[m.PRDID] = struct{}{}
		}
	}
	if len(a.Members) > 0 && a.EntryMemberID == "" {
		return fmt.Errorf("automation invalid: has members but empty entry member ID")
	}
	if a.EntryMemberID != "" {
		entry, found := byID[a.EntryMemberID]
		if !found {
			return fmt.Errorf("automation invalid: entry member %s is not a member", a.EntryMemberID)
		}
		if entry.kind() == MemberKindJoin {
			return fmt.Errorf("automation invalid: entry member %s is a join; a join is reached only through its fork", a.EntryMemberID)
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
			if byID[target].kind() == MemberKindJoin {
				return fmt.Errorf("automation invalid: member %s %s edge names join %s; a join is reached only through its fork", m.ID, name, target)
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
	if !automationIsLinear(a) {
		seen := make(map[string]struct{}, len(a.ActiveCursors))
		for _, c := range a.ActiveCursors {
			if _, found := memberIDs[c]; !found {
				return fmt.Errorf("automation invalid: active cursor %s is not a member", c)
			}
			if _, dup := seen[c]; dup {
				return fmt.Errorf("automation invalid: active cursor %s listed more than once", c)
			}
			seen[c] = struct{}{}
		}
	}
	if len(a.Members) > 0 {
		if err := validateForkJoinPairing(a, byID); err != nil {
			return err
		}
		if _, cycle := automationHasCycle(a); cycle != nil {
			return fmt.Errorf("automation invalid: cycle detected via members [%s]", strings.Join(cycle, " "))
		}
		if unreachable := automationUnreachable(a); len(unreachable) > 0 && a.Status != AutomationDraft {
			return fmt.Errorf("automation invalid: members unreachable from entry [%s]", strings.Join(unreachable, " "))
		}
	}
	return nil
}

// validateMemberKind checks that m's kind is known and that m carries only the
// fields its kind allows (design doc "Validation addenda" rules 5-7). It does
// not check PRD existence or edges; those belong to validateAutomation.
func validateMemberKind(a *Automation, m AutomationMember) error {
	switch m.Kind {
	case "", MemberKindPRD, MemberKindAutomation, MemberKindFork, MemberKindJoin:
	default:
		return fmt.Errorf("automation invalid: member %s has unknown kind %q", m.ID, m.Kind)
	}
	k := m.kind()
	if k != MemberKindFork && len(m.ForkMembers) > 0 {
		return fmt.Errorf("automation invalid: member %s: fork_members is only valid on kind fork", m.ID)
	}
	if k != MemberKindJoin && (len(m.JoinMembers) > 0 || m.OnBranchFailure != "") {
		return fmt.Errorf("automation invalid: member %s: join_members and on_branch_failure are only valid on kind join", m.ID)
	}
	if k != MemberKindAutomation && m.AutomationID != "" {
		return fmt.Errorf("automation invalid: member %s: automation_id is only valid on kind automation", m.ID)
	}
	if k != MemberKindPRD && m.PRDID != "" {
		return fmt.Errorf("automation invalid: member %s: kind %s must not carry prd_id", m.ID, k)
	}
	if k == MemberKindAutomation {
		if strings.TrimSpace(m.AutomationID) == "" {
			return fmt.Errorf("automation invalid: member %s: kind automation needs automation_id", m.ID)
		}
		if a != nil && m.AutomationID == a.ID {
			return fmt.Errorf("automation invalid: member %s nests its own automation %s", m.ID, a.ID)
		}
	}
	if k == MemberKindJoin {
		switch m.OnBranchFailure {
		case "", BranchAllMustSucceed, BranchAnyOneIsEnough, BranchContinueAnyway:
		default:
			return fmt.Errorf("automation invalid: member %s: unknown on_branch_failure %q", m.ID, m.OnBranchFailure)
		}
	}
	return nil
}

// validateForkJoinPairing enforces the fork/join addenda (design doc
// "Validation addenda" rules 1-4 and the branch rules in "Fork/join
// semantics"): branch sets name prd or automation members that carry no
// outcome edges and belong to one fork and one join at most, a fork and its
// join name the same branch set, and a fork carries no outcome edges and a
// join carries only on_complete. Errors name the member IDs involved.
func validateForkJoinPairing(a *Automation, byID map[string]AutomationMember) error {
	forkClaims := make(map[string]string)
	joinClaims := make(map[string]string)
	for _, m := range a.Members {
		switch m.kind() {
		case MemberKindFork:
			if len(m.ForkMembers) < 2 {
				return fmt.Errorf("automation invalid: fork %s needs at least 2 branch members", m.ID)
			}
			if m.OnComplete != "" || m.OnBlocked != "" || m.OnFailed != "" {
				return fmt.Errorf("automation invalid: fork %s must not carry outcome edges; its forward path is its paired join", m.ID)
			}
			if err := checkBranchSet(m, m.ForkMembers, byID, forkClaims); err != nil {
				return err
			}
		case MemberKindJoin:
			if len(m.JoinMembers) < 2 {
				return fmt.Errorf("automation invalid: join %s needs at least 2 branch members", m.ID)
			}
			if m.OnBlocked != "" || m.OnFailed != "" {
				return fmt.Errorf("automation invalid: join %s may carry only on_complete; a join failure blocks the automation", m.ID)
			}
			if err := checkBranchSet(m, m.JoinMembers, byID, joinClaims); err != nil {
				return err
			}
		}
	}
	for _, f := range a.Members {
		if f.kind() != MemberKindFork {
			continue
		}
		var joins []string
		for _, j := range a.Members {
			if j.kind() == MemberKindJoin && sameMemberSet(j.JoinMembers, f.ForkMembers) {
				joins = append(joins, j.ID)
			}
		}
		if len(joins) != 1 {
			return fmt.Errorf("automation invalid: fork %s has %d paired joins %v; exactly one join must list its branch set", f.ID, len(joins), joins)
		}
	}
	for _, j := range a.Members {
		if j.kind() != MemberKindJoin {
			continue
		}
		var forks []string
		for _, f := range a.Members {
			if f.kind() == MemberKindFork && sameMemberSet(f.ForkMembers, j.JoinMembers) {
				forks = append(forks, f.ID)
			}
		}
		if len(forks) != 1 {
			return fmt.Errorf("automation invalid: join %s has %d paired forks %v; exactly one fork must list its branch set", j.ID, len(forks), forks)
		}
	}
	return nil
}

// checkBranchSet checks one fork's or join's branch list for owner m. Each
// branch must be a known prd or automation member with no outcome edges, must
// not repeat, and must not already be claimed by another owner of the same
// kind (claims maps branch ID to owner ID).
func checkBranchSet(m AutomationMember, branches []string, byID map[string]AutomationMember, claims map[string]string) error {
	seen := make(map[string]struct{}, len(branches))
	for _, b := range branches {
		if _, dup := seen[b]; dup {
			return fmt.Errorf("automation invalid: %s %s lists branch member %s more than once", m.kind(), m.ID, b)
		}
		seen[b] = struct{}{}
		target, found := byID[b]
		if !found {
			return fmt.Errorf("automation invalid: %s %s names unknown branch member %s", m.kind(), m.ID, b)
		}
		if k := target.kind(); k != MemberKindPRD && k != MemberKindAutomation {
			return fmt.Errorf("automation invalid: %s %s branch member %s is a %s member; branches must be prd or automation members", m.kind(), m.ID, b, k)
		}
		if target.OnComplete != "" || target.OnBlocked != "" || target.OnFailed != "" {
			return fmt.Errorf("automation invalid: branch member %s of %s %s must not carry outcome edges", b, m.kind(), m.ID)
		}
		if prev, taken := claims[b]; taken {
			return fmt.Errorf("automation invalid: branch member %s is already in %s %s", b, m.kind(), prev)
		}
		claims[b] = m.ID
	}
	return nil
}

// sameMemberSet reports whether x and y hold the same member IDs. Both lists
// are expected to be duplicate-free, which checkBranchSet guarantees.
func sameMemberSet(x, y []string) bool {
	if len(x) != len(y) {
		return false
	}
	in := make(map[string]struct{}, len(y))
	for _, v := range y {
		in[v] = struct{}{}
	}
	for _, v := range x {
		if _, ok := in[v]; !ok {
			return false
		}
	}
	return true
}

// validateAutomationOwnership enforces the cross-Automation rules for members
// (design doc "Validation", "One active Automation per PRD"): a member PRD
// must exist, must not be a template or a child Automaton (children are run
// by their parent task), and must not already belong to another non-terminal
// Automation. Only prd members are checked; nested and control members own no
// PRD. Terminal Automations (completed/cancelled) own nothing, and a nil
// Automation is validated by the caller. lookupPRD reports whether the PRD with
// that ID exists; ownerOf returns the ID of the non-terminal Automation that
// currently owns the PRD, or "" if none does.
func validateAutomationOwnership(a *Automation, lookupPRD func(id string) (*PRD, bool), ownerOf func(prdID string) string) error {
	if a == nil || a.Status == AutomationCompleted || a.Status == AutomationCancelled {
		return nil
	}
	for _, m := range a.Members {
		if m.kind() != MemberKindPRD {
			continue
		}
		prd, ok := lookupPRD(m.PRDID)
		if !ok {
			return fmt.Errorf("automation invalid: member prd %s not found", m.PRDID)
		}
		if prd.IsTemplate {
			return fmt.Errorf("automation invalid: prd %s is a template and cannot be a member", m.PRDID)
		}
		if prd.ParentPRDID != "" {
			return fmt.Errorf("automation invalid: prd %s is a child automaton (run by its parent task) and cannot be a member", m.PRDID)
		}
		if owner := ownerOf(m.PRDID); owner != "" && owner != a.ID {
			return fmt.Errorf("automation invalid: prd %s already belongs to automation %s", m.PRDID, owner)
		}
	}
	return nil
}

// validateAutomationNesting enforces the nested-Automation rules (design doc
// "Validation addenda" rule 5 and "Nested Automation semantics"): each
// kind=automation member names an existing child Automation that no other
// non-terminal Automation nests, and the parent cannot reach itself through any
// chain of nested children. lookupAutomation returns a stored Automation;
// ownerOfAutomation returns the ID of the non-terminal Automation that currently
// nests the child, or "". A nil or terminal Automation nests nothing that is
// checked, as with validateAutomationOwnership.
func validateAutomationNesting(a *Automation, lookupAutomation func(id string) (*Automation, bool), ownerOfAutomation func(childID string) string) error {
	if a == nil || a.Status == AutomationCompleted || a.Status == AutomationCancelled {
		return nil
	}
	for _, m := range a.Members {
		if m.kind() != MemberKindAutomation {
			continue
		}
		child, ok := lookupAutomation(m.AutomationID)
		if !ok {
			return fmt.Errorf("automation invalid: member %s: child automation %s not found", m.ID, m.AutomationID)
		}
		if owner := ownerOfAutomation(m.AutomationID); owner != "" && owner != a.ID {
			return fmt.Errorf("automation invalid: member %s: child automation %s already belongs to automation %s", m.ID, m.AutomationID, owner)
		}
		if path := nestedPathToSelf(a.ID, child, lookupAutomation); path != nil {
			return fmt.Errorf("automation invalid: member %s: nesting cycle via automations [%s]", m.ID, strings.Join(path, " "))
		}
	}
	return nil
}

// nestedPathToSelf walks the kind=automation members of child, transitively,
// and returns the chain of Automation IDs from child to selfID when selfID is
// reachable, or nil when it is not. A visited set stops the walk on any cycle
// that does not pass through selfID.
func nestedPathToSelf(selfID string, child *Automation, lookupAutomation func(id string) (*Automation, bool)) []string {
	seen := make(map[string]bool)
	var walk func(cur *Automation, path []string) []string
	walk = func(cur *Automation, path []string) []string {
		path = append(append([]string{}, path...), cur.ID)
		if cur.ID == selfID {
			return path
		}
		if seen[cur.ID] {
			return nil
		}
		seen[cur.ID] = true
		for _, m := range cur.Members {
			if m.kind() != MemberKindAutomation {
				continue
			}
			// The parent under validation is matched by ID, not looked up: its
			// stored copy may be an older version than the one being checked.
			if m.AutomationID == selfID {
				return append(append([]string{}, path...), selfID)
			}
			next, ok := lookupAutomation(m.AutomationID)
			if !ok {
				continue
			}
			if found := walk(next, path); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(child, nil)
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
