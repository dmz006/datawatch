// Read-only view of the merged-member decisions. The Automation coordinator
// records one merged_member Decision whenever the merged-member proof took part
// in a member's end (see automation_merged_member_coordinator.go); this file
// turns that log into rows every surface (REST, MCP, PWA, CLI) can show. It
// reads only: nothing here writes an Automation or runs git.
package autonomous

import (
	"fmt"
	"sort"
	"time"
)

// Outcomes of a merged-member decision: the chain moved on, or the Automation
// stayed blocked for the operator.
const (
	MergedMemberAdvanced = "advanced"
	MergedMemberBlocked  = "blocked"
)

// MergedMemberDecision is the structured record a merged_member Decision
// carries: which member, the branch and the default branch it was proved
// merged into, the proving commit, the policy in force and the outcome.
type MergedMemberDecision struct {
	Member        string `json:"member"`
	PRDID         string `json:"prd_id,omitempty"`
	Branch        string `json:"branch"`
	DefaultBranch string `json:"default_branch"`
	Commit        string `json:"commit"`
	Policy        string `json:"policy"`
	Outcome       string `json:"outcome"` // advanced | blocked
}

// MergedMemberRow is one merged-member decision of one Automation.
type MergedMemberRow struct {
	AutomationID   string    `json:"automation_id"`
	AutomationName string    `json:"automation_name,omitempty"`
	Member         string    `json:"member"`
	PRDID          string    `json:"prd_id,omitempty"`
	Branch         string    `json:"branch"`
	DefaultBranch  string    `json:"default_branch"`
	ProvingCommit  string    `json:"proving_commit"`
	Policy         string    `json:"policy"`
	Outcome        string    `json:"outcome"`
	At             time.Time `json:"at"`
	Note           string    `json:"note,omitempty"`
}

// MergedMemberRows lists the merged-member decisions in a's decision log,
// oldest first. An entry written without the structured record (none exist
// today) still yields a row carrying its time and note.
func MergedMemberRows(a *Automation) []MergedMemberRow {
	rows := []MergedMemberRow{}
	if a == nil {
		return rows
	}
	for _, d := range a.Decisions {
		if d.Kind != decisionMergedMember {
			continue
		}
		row := MergedMemberRow{AutomationID: a.ID, AutomationName: a.Name, At: d.At, Note: d.Note}
		if r := d.MergedMember; r != nil {
			row.Member, row.PRDID, row.Branch, row.DefaultBranch = r.Member, r.PRDID, r.Branch, r.DefaultBranch
			row.ProvingCommit, row.Policy, row.Outcome = r.Commit, r.Policy, r.Outcome
		}
		rows = append(rows, row)
	}
	return rows
}

// MergedMembers returns the merged-member decisions for the Automation id, or
// for every Automation when id is empty, oldest first either way. An unknown
// id is a "404: " error, the form the REST layer maps to not found.
func (m *Manager) MergedMembers(id string) ([]MergedMemberRow, error) {
	if m.automations == nil {
		return nil, fmt.Errorf("404: Automaton %s not found", id)
	}
	if id != "" {
		a, ok := m.automations.Get(id)
		if !ok {
			return nil, fmt.Errorf("404: Automaton %s not found", id)
		}
		return MergedMemberRows(a), nil
	}
	rows := []MergedMemberRow{}
	for _, a := range m.automations.List() {
		rows = append(rows, MergedMemberRows(a)...)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].At.Before(rows[j].At) })
	return rows, nil
}
