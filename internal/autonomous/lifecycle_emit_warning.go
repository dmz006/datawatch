// Emission helpers for the two opt-in warning event kinds:
// scope_warning and guardrail_warn (design:
// docs/plans/2026-10-13-prd-lifecycle-events.md, open question 4 and the
// 2026-10-23 operator decisions). Both kinds are off by default — they
// only reach a notification sink when listed in an Automaton's
// notify_events, its guardrail profile's list, or the global
// default_events (see notify_events_resolve.go). They are purely
// informational: emitting them never blocks or alters any task, story or
// Automaton behavior.

package autonomous

// emitScopeWarning publishes a scope_warning event for t, which just
// touched files outside its declared scope. Call it only after the PRD
// and task saves that persisted the finding succeeded. files is the
// finding list and dec the Decision the same code path appended (nil
// when none); both are copied so later mutation cannot leak into a
// subscriber.
//
// alertID is the id of the alert the same code path raised alongside
// the warning ("" when the raise recorded no alert, e.g. the
// foreign-drift check); it renders into ev.Link as the PWA deep link
// "?alert=<id>" so the warning points at its alert.
func (m *Manager) emitScopeWarning(prd *PRD, t *Task, files []string, dec *Decision, alertID string) {
	if m == nil || prd == nil || t == nil {
		return
	}
	ev := LifecycleEvent{
		Kind:        KindScopeWarning,
		Level:       LevelTask,
		PRDID:       prd.ID,
		ParentPRDID: prd.ParentPRDID,
		StoryID:     t.StoryID,
		TaskID:      t.ID,
		Files:       append([]string(nil), files...),
		Link:        alertLink(alertID),
	}
	if dec != nil {
		d := *dec
		ev.Decision = &d
	}
	m.emit(ev)
}

// alertLink renders the PWA deep link for a recorded alert id
// ("?alert=<id>"); an empty id yields an empty link, which
// DescribeLifecycleEvent omits.
func alertLink(alertID string) string {
	if alertID == "" {
		return ""
	}
	return "?alert=" + alertID
}

// emitGuardrailWarn publishes a guardrail_warn event for the warn
// outcome v recorded on unitID in the PRD named by prd (prd must be
// non-nil). Level and the ID fields come from unitID: unitID == prd.ID
// is Automaton-level; otherwise unitID is looked up among the PRD's
// tasks (task-level event, StoryID from the task) and then among its
// stories (a story is reported at Automaton level with StoryID set —
// the levels are only prd, task and automation). Call it only after the
// PRD save that persisted the verdict succeeded. v is deep-copied (Issues
// included) so later mutation cannot leak into a subscriber; dec is the
// guardrail_verdict Decision the same code path appended (nil when
// none) and is copied like everywhere else.
func (m *Manager) emitGuardrailWarn(prd *PRD, unitID string, v *GuardrailVerdict, dec *Decision) {
	if m == nil || prd == nil || v == nil {
		return
	}
	ev := LifecycleEvent{
		Kind:        KindGuardrailWarn,
		Level:       LevelPRD,
		PRDID:       prd.ID,
		ParentPRDID: prd.ParentPRDID,
		Guardrail:   v.Guardrail,
	}
	if unitID == prd.ID {
		// Automaton-level guardrail (CI watch, rules review, rejudge).
	} else if task, _ := findTaskInPRD(prd, unitID); task != nil {
		ev.Level = LevelTask
		ev.TaskID = task.ID
		ev.StoryID = task.StoryID
	} else {
		// A story ID: reported at Automaton level, named via StoryID.
		for si := range prd.Story {
			if prd.Story[si].ID == unitID {
				ev.StoryID = unitID
				break
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
