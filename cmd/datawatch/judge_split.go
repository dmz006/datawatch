package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// judgeDiffFiles lists the changed files of an Automaton branch; a variable so
// tests can fake the git work.
var judgeDiffFiles = autonomouspkg.JudgeDiffFiles

// judgeRunLimit is the time limit a judge attempt runs under: the limit its
// attempt was given (scaled with the change size), or the base limit when the
// attempt carries none.
func judgeRunLimit(att *judgeAttempt, gcfg autonomouspkg.Config) time.Duration {
	if att != nil && att.Limit > 0 {
		return att.Limit
	}
	return judgeSessionTimeout(gcfg)
}

// judgeSplitGroupsFor returns the file groups a rules review is split into, or
// nil when the review runs whole: another guardrail, a change within the size
// threshold, an unknown size, or a file list that cannot be read.
func judgeSplitGroupsFor(ctx context.Context, size judgeSize, req autonomouspkg.GuardrailInvocation, find func(id string) (*autonomouspkg.PRD, bool)) []autonomouspkg.JudgeGroup {
	if req.Guardrail != "rules-review" || size.Err != nil || size.Threshold <= 0 || size.Lines <= size.Threshold || find == nil {
		return nil
	}
	prd, _ := find(req.PRDID)
	if prd == nil {
		return nil
	}
	mctx, cancel := context.WithTimeout(ctx, judgeSizeMeasureTimeout)
	defer cancel()
	files, err := judgeDiffFiles(mctx, prd, req.ProjectDir)
	if err != nil {
		return nil
	}
	return autonomouspkg.SplitJudgeFiles(files, size.Threshold)
}

// judgeGroupTryLog keeps the timed-out attempts of one group apart from the
// whole-judge record and from the other groups. A nil base (no manager or
// PRD) stays nil.
func judgeGroupTryLog(base judgeTryLog, mgr *autonomouspkg.Manager, req autonomouspkg.GuardrailInvocation, group string) judgeTryLog {
	if base == nil {
		return nil
	}
	return prdJudgeTryLog{mgr: mgr, prdID: req.PRDID, key: req.UnitID + "|" + req.Guardrail + "|group:" + group}
}

// judgeGroupRequest narrows req to one group: its prompt names only the
// group's files.
func judgeGroupRequest(req autonomouspkg.GuardrailInvocation, g autonomouspkg.JudgeGroup, idx, total int) autonomouspkg.GuardrailInvocation {
	var b strings.Builder
	fmt.Fprintf(&b, "\n\nScope of this review: part %d of %d of a large change, group %q. Review only these changed files and ignore the rest of the change:\n", idx, total, g.Name)
	for _, f := range g.Files {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	req.UnitSpec += b.String()
	if req.Prompt != "" {
		req.Prompt += b.String()
	}
	return req
}

// runJudgeSplit runs the judge once per group, one after the other, each with
// its own time limit scaled by the group's changed lines, and merges the
// verdicts (autonomouspkg.MergeJudgeVerdicts). run judges one group; a group
// whose judge held on a timeout is reported as timed out. Every group is
// judged even when an earlier one found a problem or timed out, so a finding
// is never hidden by a timeout on another group. A cancelled judge stops at
// once and returns the infra verdict of the group it was in.
func runJudgeSplit(req autonomouspkg.GuardrailInvocation, groups []autonomouspkg.JudgeGroup, gcfg autonomouspkg.Config,
	run func(gr autonomouspkg.GuardrailInvocation, g autonomouspkg.JudgeGroup, limit time.Duration) (autonomouspkg.GuardrailVerdict, error)) (autonomouspkg.GuardrailVerdict, error) {
	results := make([]autonomouspkg.JudgeGroupResult, 0, len(groups))
	for i, g := range groups {
		v, err := run(judgeGroupRequest(req, g, i+1, len(groups)), g, judgeAttemptLimit(gcfg, g.Lines))
		if err != nil {
			return v, err
		}
		if v.Category == "infra" {
			// The judge did not judge this group and was not held: hand the
			// infra verdict back so the infra retry runs the judge again.
			v.Summary = fmt.Sprintf("[%s] %s", g.Name, v.Summary)
			return v, nil
		}
		results = append(results, autonomouspkg.JudgeGroupResult{Group: g.Name, Verdict: v, TimedOut: autonomouspkg.IsJudgeTimeoutHold(v)})
	}
	return autonomouspkg.MergeJudgeVerdicts(req.Guardrail, results, time.Now()), nil
}
