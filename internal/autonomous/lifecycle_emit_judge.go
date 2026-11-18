// Judge verdict and final gate events: one event when a judge verdict with
// the block, warn or pass outcome is recorded, and one when a final gate task
// fails. All four kinds are opt-in, so delivery is gated by the notify-event
// resolver exactly like the other opt-in kinds and existing subscriptions are
// untouched. Emitting never alters the verdict or the gate outcome.

package autonomous

import "fmt"

// judgeVerdictKind maps a verdict outcome to its event kind; other outcomes
// have none.
func judgeVerdictKind(outcome string) string {
	switch outcome {
	case "block":
		return KindJudgeVerdictBlock
	case "warn":
		return KindJudgeVerdictWarn
	case "pass":
		return KindJudgeVerdictPass
	}
	return ""
}

// emitJudgeVerdict publishes the verdict event for v, recorded on unitID of
// prd. Call it once, after the save that persisted the verdict succeeded.
// Level and ID fields follow emitGuardrailWarn: the Automaton for its own id,
// a task for a task id, and the Automaton with StoryID set for a story.
func (m *Manager) emitJudgeVerdict(prd *PRD, unitID string, v *GuardrailVerdict, dec *Decision) {
	if m == nil || prd == nil || v == nil {
		return
	}
	kind := judgeVerdictKind(v.Outcome)
	if kind == "" {
		return
	}
	ev := LifecycleEvent{
		Kind:        kind,
		Level:       LevelPRD,
		PRDID:       prd.ID,
		ParentPRDID: prd.ParentPRDID,
		Guardrail:   v.Guardrail,
	}
	if unitID != prd.ID {
		if task, _ := findTaskInPRD(prd, unitID); task != nil {
			ev.Level = LevelTask
			ev.TaskID = task.ID
			ev.StoryID = task.StoryID
		} else {
			for si := range prd.Story {
				if prd.Story[si].ID == unitID {
					ev.StoryID = unitID
					break
				}
			}
		}
	}
	if dec != nil {
		d := *dec
		ev.Decision = &d
	}
	vc := *v
	vc.Issues = append([]string(nil), v.Issues...)
	ev.Verdict = &vc
	m.emit(ev)
}

// emitFinalGateFailed publishes the final-gate-failed event for task t of
// prd. Call it once, after the failure was recorded and saved.
func (m *Manager) emitFinalGateFailed(prd *PRD, t *Task, reason string) {
	if m == nil || prd == nil || t == nil {
		return
	}
	m.emit(LifecycleEvent{
		Kind:        KindFinalGateFailed,
		Level:       LevelTask,
		PRDID:       prd.ID,
		ParentPRDID: prd.ParentPRDID,
		StoryID:     t.StoryID,
		TaskID:      t.ID,
		Details:     fmt.Sprintf("final gate failed: %s", reason),
	})
}
