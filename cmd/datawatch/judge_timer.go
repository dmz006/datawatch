package main

import (
	"context"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// judgeTimeoutResult says that one judge run hit its time limit: which backend
// it ran on, how long the run lasted and the limit it was given. It is not a
// verdict. The caller decides what a timeout means (try the next backend, hold
// the PRD); it is never read as a pass or a block.
type judgeTimeoutResult struct {
	Backend string
	Elapsed time.Duration
	Limit   time.Duration
}

// runJudgeTimed runs one judge body under the judge's capacity lease and the
// judge's time limit. The limit starts when the body starts, which is after the
// lease has been granted, so the wait for a slot is never counted against it.
// body gets a context that ends at the limit; it must stop its session when
// that context ends. A limit of zero or less means no bound. now is the clock
// the elapsed time is read from (time.Now in production).
//
// When the limit elapses while the judge's own context is still live, the
// result carries a *judgeTimeoutResult and an infra verdict, att.TimedOut and
// att.Elapsed are set, and the lease is released as runJudgeLeased releases it,
// on return. A judge that is cancelled, or that finishes within the limit, has
// no timeout result.
func runJudgeTimed(ctx context.Context, admit judgeLeaseAdmit, att *judgeAttempt, model, unitID, prdID string,
	limit time.Duration, now func() time.Time,
	body func(runCtx context.Context, node string) (autonomouspkg.GuardrailVerdict, error)) (autonomouspkg.GuardrailVerdict, *judgeTimeoutResult, error) {
	if now == nil {
		now = time.Now
	}
	var timeout *judgeTimeoutResult
	verdict, err := runJudgeLeased(ctx, admit, att, model, unitID, prdID,
		func(node string) (autonomouspkg.GuardrailVerdict, error) {
			runCtx, cancel := judgeRunContext(ctx, limit)
			defer cancel()
			started := now()
			v, berr := body(runCtx, node)
			elapsed := now().Sub(started)
			if !judgeRunTimedOut(ctx, runCtx) {
				return v, berr
			}
			if berr == nil && v.Category != "infra" {
				// The judge answered as the limit passed: a real verdict.
				return v, nil
			}
			att.TimedOut = true
			att.Elapsed = elapsed
			timeout = &judgeTimeoutResult{Backend: att.Backend, Elapsed: elapsed, Limit: limit}
			return judgeAskTimeoutVerdict(limit), nil
		})
	return verdict, timeout, err
}
