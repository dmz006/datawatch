// Planning repair for the housekeeping framework (group planning).
//
// A draft Automaton can sit with zero stories because planning failed for a
// reason retrying will not change. The repair here lists those drafts. When
// every spec lint finding on the draft is one the daemon can reword, Apply
// re-plans it through the same path the planning question's "fix the wording"
// answer uses (reword the spec, apply it through revise, plan again). A draft
// whose failure is not correctable is an unsafe finding: the framework reports
// it with its one alert per subject and never repairs it.
//
// Everything a repair can do goes through PlanningRepairOps: read the drafts
// and re-plan one. There is no way to kill, end or signal a session, merge,
// ship, delete, or change a model or backend.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// PlanningDraftView is one draft Automaton with a recorded planning failure as
// the repair sees it.
type PlanningDraftView struct {
	// ID and Title name the Automaton.
	ID, Title string
	// Draft is true while the Automaton is still a draft.
	Draft bool
	// Stories is the number of stories it holds.
	Stories int
	// Failed is true when a planning failure is recorded.
	Failed bool
	// Retryable is true when the recorded failure may clear by retrying.
	Retryable bool
	// Source and Detail are the failed rule and the reason.
	Source, Detail string
	// Spec is the spec text.
	Spec string
}

// PlanningRepairOps is everything the planning repair may do or read.
type PlanningRepairOps interface {
	// Drafts lists the Automata with their planning state.
	Drafts(ctx context.Context) []PlanningDraftView
	// Replan rewords the spec with the lint suggestions and plans again, the
	// way the planning question's fix answer does.
	Replan(ctx context.Context, id string) error
}

// NewPlanningRepairs returns the planning repair over ops. Nil ops gives none.
func NewPlanningRepairs(ops PlanningRepairOps) []Repair {
	if ops == nil {
		return nil
	}
	return []Repair{&planningRepair{ops: ops}}
}

type planningRepair struct{ ops PlanningRepairOps }

func (*planningRepair) Group() string { return RepairGroupPlanning }

func planningSubject(id string) string { return "draft " + id }

// planningStuck reports whether d is a draft with a non-retryable recorded
// planning failure and no stories.
func planningStuck(d PlanningDraftView) bool {
	return d.Draft && d.Stories == 0 && d.Failed && !d.Retryable
}

func (r *planningRepair) Detect(ctx context.Context) []RepairFinding {
	var out []RepairFinding
	for _, d := range r.ops.Drafts(ctx) {
		if !planningStuck(d) {
			continue
		}
		name := d.Title
		if name == "" {
			name = d.ID
		}
		f := RepairFinding{
			Subject: planningSubject(d.ID),
			Summary: fmt.Sprintf("the draft Automaton %q has no stories; planning failed on rule %s and retrying will not help: %s", name, d.Source, d.Detail),
		}
		if !specAllCorrectable(LintSpecText(d.Spec)) {
			f.Unsafe = true
			f.UnsafeReason = "the daemon cannot reword the failure by itself; the spec needs a person to change it"
		}
		out = append(out, f)
	}
	return out
}

func (r *planningRepair) Apply(ctx context.Context, f RepairFinding) (string, error) {
	id := strings.TrimPrefix(f.Subject, "draft ")
	for _, d := range r.ops.Drafts(ctx) {
		if d.ID != id {
			continue
		}
		if !planningStuck(d) {
			return "", errors.New("the draft is no longer stuck on a planning failure; nothing was changed")
		}
		if !specAllCorrectable(LintSpecText(d.Spec)) {
			return "", errors.New("the failure is not one the daemon can reword; nothing was changed")
		}
		if err := r.ops.Replan(ctx, id); err != nil {
			return "", err
		}
		return fmt.Sprintf("reworded the spec of the draft Automaton %s with the lint suggestions and planned it again; no session was touched", id), nil
	}
	return "", errors.New("the draft is gone; nothing was changed")
}

func (*planningRepair) UndoHint(RepairFinding) string {
	return "the spec change is on the Automaton's decision log; edit the spec back from the Automaton's detail view and plan again"
}

// ManagerPlanningOps is the production PlanningRepairOps over the Manager.
type ManagerPlanningOps struct{ API *API }

// Drafts lists every Automaton with its planning state.
func (o ManagerPlanningOps) Drafts(_ context.Context) []PlanningDraftView {
	if o.API == nil || o.API.M == nil {
		return nil
	}
	st := o.API.M.Store()
	var out []PlanningDraftView
	for _, p := range st.ListPRDs() {
		prd, ok := st.SnapshotPRD(p.ID)
		if !ok {
			continue
		}
		if prd.Status == PRDHeld {
			continue // a held Automaton is never replanned or reworded
		}
		d := PlanningDraftView{ID: prd.ID, Title: prd.Title, Draft: prd.Status == PRDDraft, Stories: len(prd.Story), Spec: prd.Spec}
		if fr := prd.LastDecomposeFailure; fr != nil {
			d.Failed, d.Retryable, d.Source, d.Detail = true, fr.Retryable, fr.Source, fr.Detail
		}
		out = append(out, d)
	}
	return out
}

// Replan applies the planning question's fix option.
func (o ManagerPlanningOps) Replan(_ context.Context, id string) error {
	if o.API == nil || o.API.M == nil {
		return ErrRepairNotConfigured
	}
	return o.API.M.ApplyPlanningFailureAnswer(id, PlanningFailureOptionFix)
}
