package main

import (
	"context"
	"fmt"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// judgeSizeRoute orders a judge backend chain for a measured diff size. It is
// the configured-strong-backend form of config.JudgeSizeStrongFirst: the
// strong backend (autonomous.judge_strong_backend, empty = the last entry of
// the judge policy) moves to the front when the changed lines exceed the
// threshold; below or at the threshold, and with threshold 0 (size routing
// off), the order is unchanged. It returns a fresh slice with each backend
// once (first occurrence wins) and the decision the log shows; chain is never
// mutated. The decision is made once per run and reported on every pick.
func judgeSizeRoute(chain []string, lines, threshold int, strong string) (ordered []string, rule string) {
	return config.JudgeSizeStrongFirst(chain, lines, threshold, strong)
}

// judgeSizeMeasureTimeout bounds the git work that counts one Automaton's
// changed lines before a judge call.
const judgeSizeMeasureTimeout = 30 * time.Second

// judgeDiffLines counts an Automaton branch's changed lines; a variable so
// tests can fake the git work.
var judgeDiffLines = autonomouspkg.JudgeDiffLines

// judgeSize is the size input of one judge call. The zero value (threshold 0)
// is size routing off. Strong is autonomous.judge_strong_backend ("" = the
// last entry of the judge policy). A non-nil Err means the line count is
// unknown, and the judge keeps the default order.
type judgeSize struct {
	Threshold int
	Lines     int
	Strong    string
	Err       error
}

// measureJudgeSize counts the changed lines of req's Automaton for one judge
// call. With threshold 0 it measures nothing. A failed measurement is never an
// error for the judge: it is recorded on the result and the judge routes in
// default order. find looks the Automaton up in the manager store. strong is
// the configured strong backend ("" = the judge policy's last entry).
func measureJudgeSize(ctx context.Context, threshold int, strong string, req autonomouspkg.GuardrailInvocation, find func(id string) (*autonomouspkg.PRD, bool)) judgeSize {
	if threshold <= 0 {
		return judgeSize{}
	}
	size := judgeSize{Threshold: threshold, Strong: strong}
	var prd *autonomouspkg.PRD
	if find != nil {
		prd, _ = find(req.PRDID)
	}
	if prd == nil {
		size.Err = fmt.Errorf("automaton %s not found in the store", req.PRDID)
		return size
	}
	mctx, cancel := context.WithTimeout(ctx, judgeSizeMeasureTimeout)
	defer cancel()
	size.Lines, size.Err = judgeDiffLines(mctx, prd, req.ProjectDir)
	return size
}

// judgeSizeFor is the size input for one judge call. A pinned invocation keeps
// its own backend and is not measured, so it gets the zero judgeSize (size
// routing off); every other invocation is measured. find looks the Automaton
// up in the manager store. strong is autonomous.judge_strong_backend.
func judgeSizeFor(ctx context.Context, threshold int, strong string, req autonomouspkg.GuardrailInvocation, find func(id string) (*autonomouspkg.PRD, bool)) judgeSize {
	if req.GuardrailPinned {
		return judgeSize{}
	}
	return measureJudgeSize(ctx, threshold, strong, req, find)
}

// route returns the rule text for the decision log and the function that
// orders a backend list for this call (a fresh slice, each backend once). read
// is the list the selector reads, which the rule text is built from. balance
// says the selector picks by load, so the order only breaks load ties and the
// rule text says so, whether or not the rule moved a backend.
func (s judgeSize) route(read []string, balance bool) (rule string, order func([]string) []string) {
	const balanceNote = "; balance mode picks by load, so the order only breaks load ties"
	if s.Err != nil {
		rule = fmt.Sprintf("default order: changed line count unknown (%v), size routing skipped", s.Err)
		if balance {
			rule += balanceNote
		}
		return rule, func(l []string) []string { return l }
	}
	_, rule = judgeSizeRoute(read, s.Lines, s.Threshold, s.Strong)
	order = func(l []string) []string {
		o, _ := judgeSizeRoute(l, s.Lines, s.Threshold, s.Strong)
		return o
	}
	if balance && s.Threshold > 0 {
		rule += balanceNote
	}
	return rule, order
}
