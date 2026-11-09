package mergepipeline

// The release gate wired into a run. DecideRelease (release_gate.go) is the
// pure decision; this is where a run applies it, before the first step that
// leaves the machine, and records the outcome in the run's own plain wording.

import (
	"fmt"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// gateVerdict is what the release gate decided for one run. It is applied to
// every step that leaves the machine, but the first such step decides for
// the whole run.
type gateVerdict struct {
	// holds stops every leaving step of the run.
	holds bool
	// skipNote is what the skipped steps say, including the mode and the
	// size when it is known, the reason, and the question id when one was
	// raised or found.
	skipNote string
	// approvedNote is returned at most once: it is what the first leaving
	// step that runs prepends to its detail when the operator already
	// approved the release by answering its question.
	approvedNote string
}

// gateForLeaving applies the release gate before a leaving step runs. It can
// be called for several steps (tag, push, deploy, post-ship hooks): the
// verdict is decided at most once and cached, so the run cannot answer
// differently for later steps. The step itself runs only when the gate is
// not wired for this run, or said auto.
func (s *runState) gateForLeaving() (gateVerdict, error) {
	if s.r.ReleaseApproval == "" {
		return gateVerdict{}, nil // the gate is not wired for this run; ship as today
	}
	if s.gateSet {
		v := s.gate
		v.approvedNote = "" // spent on the first step that ran
		return v, nil
	}
	v, err := s.decideGate()
	if err != nil {
		return gateVerdict{}, err
	}
	s.gate, s.gateSet = v, true
	return v, nil
}

// decideGate asks the release gate how this run may proceed, at the first
// step that leaves the machine. A failed check never ships in any mode,
// and never asks: a "yes" against failed checks could only be honoured by
// shipping them, so the run holds for a person to look. With the checks
// passed the mode decides: auto ships as today, manual holds for a person,
// and ask raises — or finds — its one decision question and ships only
// when it was already answered yes; otherwise it holds, recording the
// question.
func (s *runState) decideGate() (gateVerdict, error) {
	r := s.r
	if s.failedSteps > 0 {
		return gateVerdict{holds: true, skipNote: releaseFailHoldNote(r.ReleaseApproval, r.ReleaseSize)}, nil
	}
	out := DecideRelease(r.ReleaseApproval, r.ReleaseSize, true)
	switch out.Action {
	case ReleaseGateShip:
		return gateVerdict{}, nil // auto: ship as today, the run is unchanged
	case ReleaseGateHoldForPerson:
		return gateVerdict{holds: true, skipNote: releaseGateNote(out, "")}, nil
	case ReleaseGateHoldForDecision:
		qid, _, err := s.raiseReleaseDecision()
		if err != nil {
			return gateVerdict{}, err
		}
		if yes, answered := ReleaseDecisionAnswer(r.ReleaseAnswers, qid); yes && answered {
			return gateVerdict{approvedNote: releaseApprovedNote(qid)}, nil
		}
		return gateVerdict{holds: true, skipNote: releaseGateNote(out, "(operator question "+qid+")")}, nil
	}
	// DecideRelease has no other outcome; this cannot be reached.
	return gateVerdict{}, fmt.Errorf("the release gate returned a decision the run does not know what to do with: %s", out.Action)
}

// raiseReleaseDecision raises the release's decision question, or finds the
// open question an earlier run of this release already raised: the question
// is keyed by project, Automaton and size, so a re-run finds it rather than
// raising a second one. A failed raise means asking failed, and asking
// failed means the run must not ship guessing: the step fails and the run
// stops short of the ship.
func (s *runState) raiseReleaseDecision() (string, bool, error) {
	return RaiseReleaseDecision(s.r.Questions, s.releasePlan())
}

// releasePlan describes, for the question, the release that is prepared and
// the checks that already ran, from what the run already knows.
func (s *runState) releasePlan() ReleasePlan {
	r := s.r
	plan := ReleasePlan{
		Project:     r.Project,
		AutomatonID: s.req.AutomatonID,
		Size:        r.ReleaseSize,
		Branches:    append([]string(nil), s.req.Branches...),
	}
	// The version the release-version step reads, when it can read one.
	if v, err := r.currentVersion(); err == nil {
		plan.Version = v
	}
	if s.m != nil {
		for _, st := range s.m.Pipeline.Steps {
			if leavesTheMachine(st.Kind) {
				plan.Steps = append(plan.Steps, st.Name)
			}
		}
	}
	// The checks are the completed steps so far, with their outcome.
	for _, step := range s.run.Steps {
		switch step.Result {
		case autonomous.MergeStepOK, autonomous.MergeStepFailed:
			plan.Checks = append(plan.Checks, ReleaseCheck{
				Name:   step.Step,
				Passed: step.Result == autonomous.MergeStepOK,
				Detail: step.Detail,
			})
		}
	}
	return plan
}

// releaseFailHoldNote is the run-record wording for the steps the gate
// holds back because a check failed: the mode, the size (or "not read"),
// and why a person is needed.
func releaseFailHoldNote(mode, size string) string {
	if size == "" {
		size = "not read"
	}
	return fmt.Sprintf("release gate (mode %s, size %s): the checks failed; a person needs to look before this ships", mode, size)
}

// releaseGateNote is the run-record wording for a step the gate holds: mode,
// size (or "not read"), what the gate decided, and the question when one
// was raised or found.
func releaseGateNote(out ReleaseGateOutcome, extra string) string {
	size := out.Size
	if size == "" {
		size = "not read"
	}
	note := fmt.Sprintf("release gate (mode %s, size %s): %s", out.Mode, size, out.Reason)
	if extra != "" {
		note += " " + extra
	}
	return note
}

// releaseApprovedNote is the one-time prefix on the first leaving step that
// runs after the operator answered the release decision question.
func releaseApprovedNote(questionID string) string {
	return "approved by the operator (operator question " + questionID + ")"
}
