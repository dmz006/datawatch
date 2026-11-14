// Fork branch chains: a fork branch is a chain of members, not one member.
// The member a fork lists in fork_members is the chain head; following its
// on_complete edges forms the chain; the last member (empty on_complete) is the
// branch tail, and the paired join's join_members names the tails. A branch of
// one member is the trivial chain, where head and tail are the same member, so
// a fork over single members behaves as it always has.
//
// A chain member carries on_complete only. When a chain member ends blocked,
// failed or cancelled there is no edge to take: the branch ends as failed, the
// members behind it are skipped, and the join rule decides what that means.
//
// The helpers here are pure reads of the Automation. They tolerate a graph that
// is not valid yet (an unknown member, a loop) by stopping the walk, so callers
// on a draft or a half-edited record never loop or panic; validation reports the
// defect.

package autonomous

import (
	"fmt"
	"sort"
)

// BranchChain is one fork branch as the status and timeline views report it:
// the chain head named by the fork, every chain member in order, and the tail
// the join collects.
type BranchChain struct {
	Fork    string   `json:"fork"`
	Join    string   `json:"join,omitempty"`
	Head    string   `json:"head"`
	Tail    string   `json:"tail"`
	Members []string `json:"members"`
	// Status is the chain outcome the join reads: running (some member still
	// to end), completed (every member completed) or failed (a member ended
	// without success, or the branch was cancelled or skipped).
	Status string `json:"status"`
	// FailedMember names the member that ended the branch as failed, when it
	// did.
	FailedMember string `json:"failed_member,omitempty"`
}

// Branch chain statuses reported by BranchChain.Status.
const (
	BranchChainRunning   = "running"
	BranchChainCompleted = "completed"
	BranchChainFailed    = "failed"
)

// walkBranchChain follows on_complete edges from head and returns the chain in
// order. The walk stops at the first member that is unknown or already on the
// chain, so a loop or a dangling edge never repeats a member.
func walkBranchChain(a *Automation, head string) []string {
	var chain []string
	seen := make(map[string]struct{})
	for cur := head; cur != ""; {
		if _, dup := seen[cur]; dup {
			break
		}
		m := automationMember(a, cur)
		if m == nil {
			break
		}
		seen[cur] = struct{}{}
		chain = append(chain, cur)
		cur = m.OnComplete
	}
	return chain
}

// forkChains returns fork's branch chains in sorted head order.
func forkChains(a *Automation, fork *AutomationMember) [][]string {
	chains := make([][]string, 0, len(fork.ForkMembers))
	for _, head := range sortedCopy(fork.ForkMembers) {
		if c := walkBranchChain(a, head); len(c) > 0 {
			chains = append(chains, c)
		}
	}
	return chains
}

// forkTails returns the tail of each of fork's chains, in sorted head order.
func forkTails(a *Automation, fork *AutomationMember) []string {
	chains := forkChains(a, fork)
	tails := make([]string, 0, len(chains))
	for _, c := range chains {
		tails = append(tails, c[len(c)-1])
	}
	return tails
}

// forkForJoin returns the fork paired with join: the one whose chain tails are
// the join's members. Validation guarantees exactly one.
func forkForJoin(a *Automation, join *AutomationMember) *AutomationMember {
	for i := range a.Members {
		m := &a.Members[i]
		if m.kind() == MemberKindFork && sameMemberSet(forkTails(a, m), join.JoinMembers) {
			return m
		}
	}
	return nil
}

// branchChainOf returns the fork, paired join and chain that member id belongs
// to, or nils when id is not on any fork branch.
func branchChainOf(a *Automation, id string) (fork, join *AutomationMember, chain []string) {
	for i := range a.Members {
		f := &a.Members[i]
		if f.kind() != MemberKindFork {
			continue
		}
		for _, c := range forkChains(a, f) {
			for _, cid := range c {
				if cid == id {
					return f, joinFor(a, f), c
				}
			}
		}
	}
	return nil, nil, nil
}

// isBranchTail reports whether id is the last member of its chain.
func isBranchTail(chain []string, id string) bool {
	return len(chain) > 0 && chain[len(chain)-1] == id
}

// chainOutcome reads one chain's outcome from member statuses alone, so it is
// the same on a replay. The first member that has not completed decides: still
// running or not started means the branch is running; anything else (failed,
// blocked, cancelled, skipped) means it failed, and that member is named.
// A chain whose members all completed is completed.
func chainOutcome(a *Automation, chain []string) (status, failedMember string) {
	for _, id := range chain {
		m := automationMember(a, id)
		switch {
		case m == nil:
			return BranchChainFailed, id
		case m.Status == MemberCompleted:
			continue
		case m.Status == MemberRunning || m.Status == MemberPending:
			return BranchChainRunning, ""
		default:
			return BranchChainFailed, id
		}
	}
	return BranchChainCompleted, ""
}

// forkBranchChains describes every branch chain of each fork in a, in member
// order then head order, for the status and timeline views.
func forkBranchChains(a *Automation) []BranchChain {
	var out []BranchChain
	for i := range a.Members {
		f := &a.Members[i]
		if f.kind() != MemberKindFork {
			continue
		}
		joinID := ""
		if j := joinFor(a, f); j != nil {
			joinID = j.ID
		}
		for _, c := range forkChains(a, f) {
			status, failed := chainOutcome(a, c)
			out = append(out, BranchChain{
				Fork: f.ID, Join: joinID, Head: c[0], Tail: c[len(c)-1],
				Members: append([]string(nil), c...), Status: status, FailedMember: failed,
			})
		}
	}
	return out
}

// skipChainAfter marks every member of chain behind member id that has not
// started as skipped: the branch already failed, so they never run.
func (st *stepper) skipChainAfter(chain []string, id string) {
	t := st.now
	after := false
	for _, cid := range chain {
		if cid == id {
			after = true
			continue
		}
		if !after {
			continue
		}
		m := automationMember(st.a, cid)
		if m == nil || (m.Status != MemberPending && m.Status != "") {
			continue
		}
		dropCursor(st.a, cid)
		m.Status = MemberSkipped
		m.Outcome = "branch-failed"
		m.FinishedAt = &t
		m.EdgeTaken = ""
	}
}

// checkBranchChains validates the chain behind every head of fork m. claims
// maps each chain member to the fork that owns it, so a member on two chains is
// reported. A chain stays inside its fork/join pair: every member is a prd or
// automation member carrying on_complete only, the walk reaches a tail without
// a loop, and nothing outside the chain enters it part-way.
func checkBranchChains(a *Automation, m AutomationMember, byID map[string]AutomationMember, claims map[string]string) error {
	seen := make(map[string]struct{}, len(m.ForkMembers))
	for _, head := range m.ForkMembers {
		if _, dup := seen[head]; dup {
			return fmt.Errorf("automation invalid: fork %s lists branch member %s more than once", m.ID, head)
		}
		seen[head] = struct{}{}
		if _, found := byID[head]; !found {
			return fmt.Errorf("automation invalid: fork %s names unknown branch member %s", m.ID, head)
		}
		onChain := make(map[string]struct{})
		for cur := head; cur != ""; {
			target, found := byID[cur]
			if !found {
				return fmt.Errorf("automation invalid: fork %s branch chain from %s names unknown member %s", m.ID, head, cur)
			}
			if k := target.kind(); k != MemberKindPRD && k != MemberKindAutomation {
				return fmt.Errorf("automation invalid: fork %s branch chain member %s is a %s member; branches must be prd or automation members", m.ID, cur, k)
			}
			if target.OnBlocked != "" || target.OnFailed != "" {
				return fmt.Errorf("automation invalid: branch member %s of fork %s must not carry outcome edges other than on_complete; a blocked or failed chain member ends its branch as failed", cur, m.ID)
			}
			if _, loop := onChain[cur]; loop {
				return fmt.Errorf("automation invalid: fork %s branch chain from %s loops back to member %s", m.ID, head, cur)
			}
			if prev, taken := claims[cur]; taken {
				if prev == m.ID {
					return fmt.Errorf("automation invalid: branch member %s is in two chains of fork %s", cur, m.ID)
				}
				return fmt.Errorf("automation invalid: branch member %s is already in a chain of fork %s", cur, prev)
			}
			claims[cur] = m.ID
			onChain[cur] = struct{}{}
			cur = target.OnComplete
			if cur != "" {
				if next, ok := byID[cur]; ok && next.kind() == MemberKindJoin {
					return fmt.Errorf("automation invalid: fork %s branch chain from %s leaves its fork/join pair: %s on_complete names join %s; the tail has an empty on_complete and the join names the tail", m.ID, head, target.ID, cur)
				}
			}
		}
	}
	return nil
}

// checkChainEntries rejects an edge from outside a chain that enters it
// part-way: a chain is entered only through its head, by its fork. claims maps
// every chain member to its fork; heads names the chain heads.
func checkChainEntries(a *Automation, claims map[string]string, heads map[string]struct{}) error {
	pos := make(map[string]string, len(claims)) // chain member -> its predecessor on the chain
	for _, m := range a.Members {
		if _, ok := claims[m.ID]; !ok || m.OnComplete == "" {
			continue
		}
		pos[m.OnComplete] = m.ID
	}
	ids := make([]string, 0, len(a.Members))
	for _, m := range a.Members {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	byID := make(map[string]AutomationMember, len(a.Members))
	for _, m := range a.Members {
		byID[m.ID] = m
	}
	for _, id := range ids {
		m := byID[id]
		for _, t := range []string{m.OnComplete, m.OnBlocked, m.OnFailed} {
			if t == "" {
				continue
			}
			if _, inChain := claims[t]; !inChain {
				continue
			}
			if _, isHead := heads[t]; isHead {
				continue
			}
			if pos[t] == id {
				continue // the chain's own link
			}
			return fmt.Errorf("automation invalid: member %s edge names %s, which is inside a fork branch chain; a chain is entered only through its head by its fork", id, t)
		}
	}
	return nil
}
