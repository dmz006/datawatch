package autonomous

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Stable blocker codes of the priority stall detector. Each names the one
// thing that keeps a waiting, high-priority Automaton from starting.
const (
	StallBlockerLaneFull             = "lane_full"
	StallBlockerPlanCoverage         = "plan_coverage_blocked"
	StallBlockerHeld                 = "held"
	StallBlockerStale                = "stale"
	StallBlockerProposalNotConfirmed = "proposal_not_confirmed"
	StallBlockerJudgeTimedOut        = "judge_timed_out"
	StallBlockerMergePending         = "merge_pending"
	// StallBlockerOther covers a lane fill refusal that has no dedicated
	// mapping; the refusal text is passed through.
	StallBlockerOther = "other_refusal"
)

// LaneFillDecisionSource supplies, for one Automaton, the facts the stall
// detector reads. Tests fake it. Every method is read only.
type LaneFillDecisionSource interface {
	// Refusal returns the code and text from EvaluateLaneFillEligibility
	// for the Automaton, or two empty strings when lane fill would take it.
	Refusal(p *PRD) (code, text string)
	// LaneFull reports whether no lane is free for the Automaton.
	LaneFull(p *PRD) bool
	// MergePending reports whether every task is done and the branch waits
	// for a merge.
	MergePending(p *PRD) bool
	// JudgeTimedOut reports whether the judge timed out on every backend.
	JudgeTimedOut(p *PRD) bool
}

// WakeSink receives one event per Automaton per blocker.
type WakeSink interface {
	Wake(PriorityStall)
}

// PriorityStall is one stalled Automaton and the one action that clears it.
type PriorityStall struct {
	AutomatonID    string `json:"automaton_id"`
	Priority       int    `json:"priority"`
	WaitingMinutes int    `json:"waiting_minutes"`
	BlockerCode    string `json:"blocker_code"`
	BlockerText    string `json:"blocker_text"`
	Action         string `json:"action"`
}

// stallBlocker maps a blocker code to its plain text and its one action.
func stallBlocker(code, refusalText string) (outCode, text, action string) {
	switch code {
	case StallBlockerLaneFull:
		return code, "every lane is busy", "wait for a lane or free one"
	case LaneFillReasonPlanCoverageBlocked:
		return StallBlockerPlanCoverage, "the plan coverage check failed and nobody has acknowledged it",
			"acknowledge the coverage failure or re-decompose the Automaton"
	case LaneFillReasonHeld, LaneFillReasonOperatorHold:
		return StallBlockerHeld, "the Automaton is on hold", "release the hold"
	case LaneFillReasonStale:
		return StallBlockerStale, "the Automaton has not been touched for too long",
			"refresh the Automaton or re-decompose it"
	case LaneFillReasonProposalNotConfirmed:
		return StallBlockerProposalNotConfirmed, "its proposal is not confirmed", "confirm the proposal"
	case StallBlockerJudgeTimedOut:
		return code, "the judge timed out on every backend", "re-run the judge"
	case StallBlockerMergePending:
		return code, "every task is done and the branch is waiting for a merge", "merge the branch"
	default:
		if refusalText == "" {
			refusalText = "lane fill refuses this Automaton"
		}
		return StallBlockerOther, refusalText, "clear the reason named in the text"
	}
}

// StallDetector finds waiting Automata of high priority that have waited
// too long, names what blocks each, and wakes the sink once per Automaton
// per blocker. It only reads: it never approves, runs or acknowledges.
type StallDetector struct {
	src  LaneFillDecisionSource
	sink WakeSink

	mu    sync.Mutex
	woken map[string]string // Automaton id -> blocker code last woken
}

// NewStallDetector builds a detector over a decision source and a wake sink.
func NewStallDetector(src LaneFillDecisionSource, sink WakeSink) *StallDetector {
	return &StallDetector{src: src, sink: sink, woken: map[string]string{}}
}

// blockerFor returns the code, text and action for the Automaton, or an
// empty code when nothing blocks it.
func (d *StallDetector) blockerFor(p *PRD) (code, text, action string) {
	if d.src.JudgeTimedOut(p) {
		return stallBlocker(StallBlockerJudgeTimedOut, "")
	}
	if rc, rt := d.src.Refusal(p); rc != "" {
		return stallBlocker(rc, rt)
	}
	if d.src.MergePending(p) {
		return stallBlocker(StallBlockerMergePending, "")
	}
	if d.src.LaneFull(p) {
		return stallBlocker(StallBlockerLaneFull, "")
	}
	return "", "", ""
}

// Scan returns the current stalls, ordered by priority (high first) then
// id. It wakes the sink for each stall whose blocker differs from the one
// last woken for that Automaton, and clears the state of every Automaton
// that no longer stalls.
func (d *StallDetector) Scan(prds []*PRD, cfg SupervisorWatchConfig, now time.Time) []PriorityStall {
	floor := cfg.EffectivePriorityFloor()
	threshold := cfg.EffectiveStallThresholdMinutes()

	var stalls []PriorityStall
	for _, p := range prds {
		age, waiting := supervisorWatchWaitingAge(p, now)
		if !waiting || p.Priority < floor || age <= threshold {
			continue
		}
		code, text, action := d.blockerFor(p)
		if code == "" {
			continue
		}
		stalls = append(stalls, PriorityStall{
			AutomatonID: p.ID, Priority: p.Priority, WaitingMinutes: age,
			BlockerCode: code, BlockerText: text, Action: action,
		})
	}
	sort.SliceStable(stalls, func(i, j int) bool {
		if stalls[i].Priority != stalls[j].Priority {
			return stalls[i].Priority > stalls[j].Priority
		}
		return stalls[i].AutomatonID < stalls[j].AutomatonID
	})

	d.mu.Lock()
	var wake []PriorityStall
	live := make(map[string]bool, len(stalls))
	for _, s := range stalls {
		live[s.AutomatonID] = true
		if d.woken[s.AutomatonID] != s.BlockerCode {
			d.woken[s.AutomatonID] = s.BlockerCode
			wake = append(wake, s)
		}
	}
	for id := range d.woken {
		if !live[id] {
			delete(d.woken, id)
		}
	}
	d.mu.Unlock()

	if d.sink != nil && cfg.EffectiveStallWake() {
		for _, s := range wake {
			d.sink.Wake(s)
		}
	}
	return stalls
}

// LifecycleWakeSink publishes each stall as a priority_stall lifecycle event,
// which the supervisor wake set delivers to the supervisor session.
type LifecycleWakeSink struct {
	Emit func(LifecycleEvent)
}

// Wake emits the stall: NewStatus is the blocker code, Details the JSON payload.
func (s LifecycleWakeSink) Wake(st PriorityStall) {
	if s.Emit == nil {
		return
	}
	b, _ := json.Marshal(st)
	s.Emit(LifecycleEvent{
		Kind: KindPriorityStall, Level: LevelPRD, PRDID: st.AutomatonID,
		NewStatus: st.BlockerCode, Details: string(b),
	})
}

// Text renders the stall as one plain sentence.
func (s PriorityStall) Text() string {
	return fmt.Sprintf("Automaton %s (priority %d) has waited %d minutes: %s. To clear it: %s.",
		s.AutomatonID, s.Priority, s.WaitingMinutes, s.BlockerText, s.Action)
}
