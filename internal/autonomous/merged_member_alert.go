// Merged-member alert: the comm-channel alert the coordinator raises when it
// advances an Automaton's chain over a cancelled member whose branch is already
// merged. The unmerged and the manual-policy paths block the Automaton and
// raise the blocked alert instead, so they raise nothing here.
package autonomous

import "fmt"

// mergedMemberAlert is what the alert names: the member, its branch, the
// default branch it was proved merged into and the proving commit.
type mergedMemberAlert struct {
	member, branch, base, commit string
}

// MergedMemberAlertTitle is the title of the merged-member advance alert.
const MergedMemberAlertTitle = "Automaton advanced on a merged member"

// MergedMemberAlertBody is the alert text: the Automaton, the member, the
// branch and the proving commit.
func MergedMemberAlertBody(automation, id, member, branch, base, commit string) string {
	return fmt.Sprintf(
		"Automaton %q moved on past member %q: its branch %s is already merged on %s (proving commit %s), so the cancelled member counts as completed. Read the decisions with: autonomous merged-members %s.",
		automation, member, branch, base, commit, id)
}

// afterMergedMember raises the info alert for a chain advanced on a merged
// member. A nil n (any other step) raises nothing.
func (c *Coordinator) afterMergedMember(a *Automation, n *mergedMemberAlert) {
	if n == nil {
		return
	}
	name := a.Name
	if name == "" {
		name = a.ID
	}
	c.alert("info", MergedMemberAlertTitle, MergedMemberAlertBody(name, a.ID, n.member, n.branch, n.base, n.commit))
}
