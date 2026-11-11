package costreport

import (
	"fmt"
	"time"
)

// AttributionCheck counts ledger sessions launched by Automaton machinery
// (a task id, or a worker / decompose / rules-review / verification /
// guardrail role) that carry no automaton id in the ledger. Such spend
// falls under "unattributed" in the automaton grouping though an Automaton
// caused it. Sessions with no task and role "other" (or blank) are
// genuinely non-Automaton and never counted.
type AttributionCheck struct {
	// AutomatonLaunched is the ledger sessions launched by Automaton machinery.
	AutomatonLaunched int `json:"automaton_launched"`
	// Unattributed is the subset of AutomatonLaunched with no automaton id.
	Unattributed int `json:"unattributed"`
	// SessionIDs lists the unattributed sessions, in record order.
	SessionIDs []string `json:"session_ids"`
	// OK is true when Unattributed is zero.
	OK   bool   `json:"ok"`
	Note string `json:"note"`
}

// machineryRoles are the spend roles only Automaton machinery assigns.
var machineryRoles = map[string]bool{
	"worker": true, "decompose": true, "rules_review": true,
	"verification": true, "guardrail": true,
}

// LaunchedByAutomaton reports whether the record's task or role shows the
// session was launched by Automaton machinery.
func LaunchedByAutomaton(r Record) bool {
	return r.TaskID != "" || machineryRoles[r.Role]
}

// CheckAttribution counts the unattributed Automaton-launched sessions
// among the ledger records inside the window. Non-ledger records are
// ignored; peer records only count when includePeer is set.
func CheckAttribution(recs []Record, since, until time.Time, includePeer bool) AttributionCheck {
	c := AttributionCheck{SessionIDs: []string{}}
	for _, r := range recs {
		if !r.FromLedger || (r.Peer && !includePeer) {
			continue
		}
		if !r.At.IsZero() && ((!since.IsZero() && r.At.Before(since)) || (!until.IsZero() && r.At.After(until))) {
			continue
		}
		if !LaunchedByAutomaton(r) {
			continue
		}
		c.AutomatonLaunched++
		if r.LedgerAutomatonID == "" {
			c.Unattributed++
			c.SessionIDs = append(c.SessionIDs, r.SessionID)
		}
	}
	c.OK = c.Unattributed == 0
	if c.OK {
		c.Note = fmt.Sprintf("attribution ok: all %d Automaton-launched session(s) carry an automaton id", c.AutomatonLaunched)
	} else {
		c.Note = fmt.Sprintf("attribution: %d of %d Automaton-launched session(s) have no automaton id in the ledger and show as unattributed", c.Unattributed, c.AutomatonLaunched)
	}
	return c
}
