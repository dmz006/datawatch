package autonomous

import (
	"path/filepath"
	"time"
)

// NewPlanningRecordFromLadder builds the record for a routed decompose that
// ran the planning ladder. The planner is the one that produced the final
// plan (or the last one tried); FindingCount is the number of findings left
// on the returned plan.
func NewPlanningRecordFromLadder(d RouteDecision, lr LadderResult, now time.Time) *PlanningRecord {
	rec := &PlanningRecord{
		RuleName:       d.RuleName,
		Classes:        append([]string(nil), d.Classes...),
		PlannerBackend: lr.Planner.Backend,
		PlannerModel:   lr.Planner.Model,
		Shadow:         d.Shadow,
		Fallback:       d.Fallback,
		FindingCount:   len(lr.Findings),
		RecordedAt:     now,
	}
	attempts := lr.Attempts
	if len(attempts) > PlanningRecordMaxAttempts {
		attempts = attempts[len(attempts)-PlanningRecordMaxAttempts:]
	}
	rec.Attempts = append([]AttemptRecord(nil), attempts...)
	return rec
}

// NewPlanningRecordFromDecision builds the record for a decompose that was
// planned without the ladder (shadow mode, or a fallback route): it holds the
// route decision and the planner that was actually used.
func NewPlanningRecordFromDecision(d RouteDecision, backend, model string, now time.Time) *PlanningRecord {
	return &PlanningRecord{
		RuleName:       d.RuleName,
		Classes:        append([]string(nil), d.Classes...),
		PlannerBackend: backend,
		PlannerModel:   model,
		Shadow:         d.Shadow,
		Fallback:       d.Fallback,
		RecordedAt:     now,
	}
}

// routeInputForPRD is the routing input derived from a PRD and its Automaton.
func routeInputForPRD(prd *PRD) RouteInput {
	in := RouteInput{
		Kind:      EffectiveKind(prd, nil),
		Tags:      append([]string(nil), prd.TagIDs...),
		SpecBytes: len(prd.Spec),
	}
	if prd.ProjectDir != "" {
		in.Project = filepath.Base(prd.ProjectDir)
	}
	return in
}

// storePlanningRecord sets the PRD's record under the store lock and saves.
func (m *Manager) storePlanningRecord(prd *PRD, rec *PlanningRecord) {
	if rec == nil {
		return
	}
	m.store.WithLock(func() { prd.PlanningRecord = rec })
	_ = m.store.SavePRD(prd)
}

// recordPlanningOnFailure is deferred by both decompose twins: when the
// decompose returns an error and a record exists, it is stored on the PRD
// (the failure path of the record).
func (m *Manager) recordPlanningOnFailure(prd *PRD, rec **PlanningRecord, errp *error) {
	if errp != nil && *errp != nil && rec != nil && *rec != nil {
		m.storePlanningRecord(prd, *rec)
	}
}

// NotePlanningEdit increments SupervisorEdits on a PRD that already has a
// planning record. It is a no-op for a PRD without one. The API edit paths
// (edit, add and remove of a story or task) call it after a successful edit.
func (m *Manager) NotePlanningEdit(prdID string) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	var bumped bool
	m.store.WithLock(func() {
		if prd.PlanningRecord != nil {
			prd.PlanningRecord.SupervisorEdits++
			bumped = true
		}
	})
	if bumped {
		_ = m.store.SavePRD(prd)
	}
}
