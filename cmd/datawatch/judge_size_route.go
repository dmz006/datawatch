package main

import (
	"context"
	"fmt"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// judgeSizeRouteBackend is the backend a large diff is routed to first.
const judgeSizeRouteBackend = "claude-code"

// judgeSizeRoute orders a judge backend chain for a measured diff size.
// threshold 0 turns size routing off. At or below the threshold the chain
// keeps its configured order; above it claude-code moves to the front when
// it is in the chain and the rest keep their relative order. It returns a
// fresh slice with each backend once (first occurrence wins) and the
// sentence the decision log shows; chain is never mutated.
func judgeSizeRoute(chain []string, lines, threshold int) (ordered []string, rule string) {
	ordered = make([]string, 0, len(chain))
	seen := make(map[string]struct{}, len(chain))
	for _, b := range chain {
		if _, dup := seen[b]; dup {
			continue
		}
		seen[b] = struct{}{}
		ordered = append(ordered, b)
	}

	switch {
	case threshold == 0:
		return ordered, "default order: size routing is off (threshold 0)"
	case lines <= threshold:
		return ordered, fmt.Sprintf("default order: %d changed lines is at or below the threshold of %d", lines, threshold)
	}

	if _, ok := seen[judgeSizeRouteBackend]; !ok {
		return ordered, fmt.Sprintf("default order: %d changed lines is above the threshold of %d but %s is not in the chain",
			lines, threshold, judgeSizeRouteBackend)
	}
	front := make([]string, 0, len(ordered))
	front = append(front, judgeSizeRouteBackend)
	for _, b := range ordered {
		if b != judgeSizeRouteBackend {
			front = append(front, b)
		}
	}
	return front, fmt.Sprintf("size rule: %d changed lines is above the threshold of %d, %s tried first",
		lines, threshold, judgeSizeRouteBackend)
}

// judgeSizeMeasureTimeout bounds the git work that counts one Automaton's
// changed lines before a judge call.
const judgeSizeMeasureTimeout = 30 * time.Second

// judgeDiffLines counts an Automaton branch's changed lines; a variable so
// tests can fake the git work.
var judgeDiffLines = autonomouspkg.JudgeDiffLines

// judgeSize is the size input of one judge call. The zero value (threshold 0)
// is size routing off. A non-nil Err means the line count is unknown, and the
// judge keeps the default order.
type judgeSize struct {
	Threshold int
	Lines     int
	Err       error
}

// measureJudgeSize counts the changed lines of req's Automaton for one judge
// call. With threshold 0 it measures nothing. A failed measurement is never an
// error for the judge: it is recorded on the result and the judge routes in
// default order. find looks the Automaton up in the manager store.
func measureJudgeSize(ctx context.Context, threshold int, req autonomouspkg.GuardrailInvocation, find func(id string) (*autonomouspkg.PRD, bool)) judgeSize {
	if threshold <= 0 {
		return judgeSize{}
	}
	size := judgeSize{Threshold: threshold}
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
	_, rule = judgeSizeRoute(read, s.Lines, s.Threshold)
	order = func(l []string) []string {
		o, _ := judgeSizeRoute(l, s.Lines, s.Threshold)
		return o
	}
	if balance && s.Threshold > 0 {
		rule += balanceNote
	}
	return rule, order
}
