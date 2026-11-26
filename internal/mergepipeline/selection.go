// Selection and landing. A run asks the daemon's merge selection rule what it
// may merge, holds each branch to the exact commit selected, and records the
// landing only after the remote is confirmed to contain the pushed commit.
package mergepipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// Selection is the merge selection rule (autonomous.Manager.SelectMergeRun).
type Selection interface {
	SelectMergeRun(ctx context.Context, branches []string, automatonID string) (autonomous.MergeSelectionEntry, error)
}

// Landing records a confirmed landing (autonomous.Manager.RecordMergeLanding).
// It must record nothing when the remote does not contain commit.
type Landing interface {
	RecordMergeLanding(ctx context.Context, automatonID, release, commit string, at time.Time) (*autonomous.PRD, error)
}

// applySelection asks the selection rule about the run. A refused run does
// nothing; a selected one belongs to the one Automaton the rule names.
func (s *runState) applySelection(ctx context.Context) error {
	if s.r.Selection == nil {
		return nil
	}
	e, err := s.r.Selection.SelectMergeRun(ctx, s.req.Branches, s.req.AutomatonID)
	if err != nil {
		return fmt.Errorf("merge refused: %w", err)
	}
	s.req.AutomatonID = e.AutomatonID
	s.pins = map[string]string{s.req.Branches[0]: e.Tip}
	return nil
}

// checkPin fails when a selected branch no longer points at the commit the
// run holds it to.
func (s *runState) checkPin(branch, at string) error {
	if want, ok := s.pins[branch]; ok && want != at {
		return fmt.Errorf("branch %q moved since it was selected: it was %s and is now %s", branch, want, at)
	}
	return nil
}

func (s *runState) checkPinnedBranch(ctx context.Context, branch string) error {
	if _, ok := s.pins[branch]; !ok {
		return nil
	}
	at, err := s.m.revParse(ctx, s.r.Dir, "refs/heads/"+branch)
	if err != nil {
		return fmt.Errorf("read selected branch %q: %w", branch, err)
	}
	return s.checkPin(branch, at)
}

func (s *runState) repin(branch, at string) {
	if _, ok := s.pins[branch]; ok {
		s.pins[branch] = at
	}
}

// releasedCommit is the commit the run pushed.
func (s *runState) releasedCommit(ctx context.Context) (string, error) {
	if s.pushedCommit != "" {
		return s.pushedCommit, nil
	}
	return s.mainHead(ctx)
}

// lastStepOK says the newest recorded step ran and succeeded.
func (s *runState) lastStepOK() bool {
	n := len(s.run.Steps)
	return n > 0 && s.run.Steps[n-1].Result == autonomous.MergeStepOK
}

// landingStepName is the step that records the Automaton landed.
const landingStepName = "record-landing"

// recordLanding records the landing after the push step, as its own step. A
// push step the release gate held (or that was skipped) records nothing; the
// recorder itself refuses unless the remote contains the commit. A failure is
// shown on the step and leaves the landing pending.
func (s *runState) recordLanding(ctx context.Context, push Step) {
	if s.r.Landing == nil || s.req.AutomatonID == "" {
		return
	}
	step := Step{Name: landingStepName, Kind: push.Kind, OnFailure: OnFailureContinue}
	_, _, _ = s.recordStep(ctx, "landing", step, DefaultStepTimeout, func(c context.Context) (string, string, error) {
		commit, err := s.releasedCommit(c)
		if err != nil {
			return hookOutcome("", err)
		}
		version, err := s.releaseVersionOf()
		if err != nil || s.noRelease {
			version = "" // no version: the landing is still a fact
		}
		if _, err := s.r.Landing.RecordMergeLanding(c, s.req.AutomatonID, version, commit, s.r.now()); err != nil {
			return hookOutcome("", err)
		}
		return autonomous.MergeStepOK, "recorded landed, completed and shipped at commit " + commit, nil
	})
}
