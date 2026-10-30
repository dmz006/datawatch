// Merged-member handling in the Automation coordinator. A member PRD that
// ends cancelled (or completed) is checked against the project's default
// branch: when its work is already merged, the autonomous.automation.
// merged_member_policy decides what the Automation does. Policy completed
// treats the member as completed so the chain follows its on_complete edge;
// manual keeps today's blocked outcome and adds the proof to the block reason
// so the operator can resolve it with resume or skip; block is exactly the
// behaviour before the policy existed and runs no git at all. A branch that
// is not merged, or whose state is unknown, always keeps today's outcome.
// The git work runs through the Coordinator's MergeGitRunner with no manager
// lock held.
package autonomous

import (
	"context"
	"fmt"

	"github.com/dmz006/datawatch/internal/config"
)

// decisionMergedMember is the Automation Decision kind recorded when the
// merged-member proof took part in a member's end.
const decisionMergedMember = "merged_member"

// mergedMemberNote is what a merged-member consultation leaves on the
// Automation: one Decision and one audit entry, written by the step.
type mergedMemberNote struct {
	prdID, branch, base, commit, policy string
	merged                              bool
	// cancelled is set when the policy treated a cancelled member as
	// completed, the one case that moves the chain on and raises an alert.
	cancelled bool
	note      string
}

// mergedMemberEnd refines end for a member PRD that ended cancelled or
// completed. It returns end unchanged unless the proof says the member's
// branch is merged and the policy asks for a different outcome or a record.
func (c *Coordinator) mergedMemberEnd(prd *PRD, status PRDStatus, end endOutcome) endOutcome {
	cfg := c.m.Config()
	policy := cfg.Automation.EffectiveMergedMemberPolicy()
	if policy == config.MergedMemberPolicyBlock {
		return end
	}

	var prdID, branch, name string
	c.m.readLocked(func() { prdID, branch, name = prd.ID, prd.Git.Branch, prd.Title })
	if branch == "" {
		return end
	}
	if name == "" {
		name = prdID
	}

	ctx, cancel := context.WithTimeout(context.Background(), mergePendingGitTimeout)
	defer cancel()
	proof := MergedMemberProof(ctx, c.mp.runner(), cfg.WorktreeBaseRepo, branch)
	if !proof.Merged {
		return end
	}

	n := &mergedMemberNote{
		prdID: prdID, branch: proof.Branch, base: proof.Base, commit: proof.Commit,
		policy: policy, merged: true,
	}
	evidence := fmt.Sprintf("member %s: branch %s is merged on %s (commit %s)", name, proof.Branch, proof.Base, proof.Commit)
	switch {
	case status == PRDCompleted:
		n.note = evidence + "; the member completed"
	case policy == config.MergedMemberPolicyManual:
		n.note = evidence + "; policy manual keeps the Automation blocked for the operator to resume or skip the member"
		end.direct = fmt.Sprintf("%s; %s", end.direct, evidence)
	default:
		n.note = evidence + "; policy completed treats the cancelled member as completed"
		done, _ := prdEnd(PRDCompleted)
		end = done
		n.cancelled = true
	}
	end.merged = n
	return end
}
