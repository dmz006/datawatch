// daemon_template_task.go — runs the task templates whose mode is "daemon".
//
// A template task with DaemonRun set is performed by the daemon itself, so no
// worker session is ever spawned for it. The four daemon-capable templates are:
//
//   - changelog_entry: merges the change records of the Automaton's completed
//     tasks in the same story and writes the changelog draft.
//   - parity_issue_draft: the same merge, writing the parity-issue draft.
//   - bounded_load_step and version_bump_left_to_merge: a note only. No file is
//     written; the rendered template spec is recorded as the decision note.
//
// Every such task completes through the same path as a worker task (status,
// save, lifecycle emit), so the story roll-up counts it.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// daemonTemplateKinds maps the artefact-writing daemon templates to their
// generated artefact kind. Templates absent from the map are note-only.
var daemonTemplateKinds = map[string]genartifacts.Kind{
	TemplateChangelogEntry:   genartifacts.ChangelogDraft,
	TemplateParityIssueDraft: genartifacts.ParityDraft,
}

// daemonNoteTemplates are the daemon templates that write no file.
var daemonNoteTemplates = map[string]bool{
	TemplateBoundedLoadStep:        true,
	TemplateVersionBumpLeftToMerge: true,
}

// taskTemplateIDOfSpec reads the template id from the marker that
// taskForTemplate embeds in a template task's spec. Empty when absent.
func taskTemplateIDOfSpec(spec string) string {
	i := strings.Index(spec, TaskTemplateMarkerPrefix)
	if i < 0 {
		return ""
	}
	rest := spec[i+len(TaskTemplateMarkerPrefix):]
	if j := strings.IndexAny(rest, ") \t\r\n"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// runDaemonTemplateTask performs a daemon-run template task without a session.
// handled is false unless t.DaemonRun is set. A generator error marks the task
// failed and is returned; a successful run completes the task and records a
// decision naming the artefact path or note.
func (m *Manager) runDaemonTemplateTask(ctx context.Context, prd *PRD, t *Task) (handled bool, err error) {
	if t == nil || !t.DaemonRun {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	id := taskTemplateIDOfSpec(t.Spec)
	var note string
	if kind, ok := daemonTemplateKinds[id]; ok {
		res, gerr := m.generateDaemonArtefact(prd, t, kind)
		if gerr != nil {
			m.failDaemonTemplateTask(prd, t, gerr)
			return true, gerr
		}
		note = fmt.Sprintf("daemon completed task %s: %s %s", id, kind, res.Path)
		if res.Reason != "" {
			note += " (" + res.Reason + ")"
		}
	} else if daemonNoteTemplates[id] {
		note = fmt.Sprintf("daemon completed task %s: note recorded, no file written: %s", id, strings.TrimSpace(t.Spec))
	} else {
		gerr := fmt.Errorf("daemon-run task %q has no daemon handler for template %q", t.ID, id)
		m.failDaemonTemplateTask(prd, t, gerr)
		return true, gerr
	}
	m.completeDaemonTemplateTask(prd, t, note)
	return true, nil
}

// generateDaemonArtefact merges the change records of the completed tasks in
// the task's story and writes one generated artefact of the given kind.
func (m *Manager) generateDaemonArtefact(prd *PRD, t *Task, kind genartifacts.Kind) (genartifacts.Result, error) {
	if prd == nil || strings.TrimSpace(prd.ProjectDir) == "" {
		return genartifacts.Result{}, fmt.Errorf("daemon-run %s: the Automaton has no project directory", kind)
	}
	merged := m.daemonStoryRecord(prd, t)
	opts := genartifacts.Options{Root: prd.ProjectDir}
	if s, ok := m.generatedArtefactSettings(); ok {
		opts = m.generatedArtefactOptions(prd, s)
	}
	res, err := genartifacts.Generate(kind, merged, opts)
	if err != nil {
		return res, fmt.Errorf("daemon-run %s: %w", kind, err)
	}
	return res, nil
}

// daemonStoryRecord merges the saved change records of the completed tasks
// that share the task's story. A task with no saved record is skipped.
func (m *Manager) daemonStoryRecord(prd *PRD, t *Task) *changerecord.ChangeRecord {
	var recs []*changerecord.ChangeRecord
	for _, st := range prd.Story {
		owns := false
		for i := range st.Tasks {
			if st.Tasks[i].ID == t.ID {
				owns = true
				break
			}
		}
		if !owns {
			continue
		}
		for i := range st.Tasks {
			s := &st.Tasks[i]
			if s.ID == t.ID || s.Status != TaskCompleted {
				continue
			}
			r, err := changerecord.Load(m.dataDir, s.ID)
			if err != nil {
				continue // no record for this task: not an error
			}
			recs = append(recs, r)
		}
	}
	merged := changerecord.Merge(recs...)
	merged.AutomatonID = prd.ID
	return merged
}

// completeDaemonTemplateTask marks the task completed, records the decision,
// saves the task and emits its status exactly as the worker completion path does.
func (m *Manager) completeDaemonTemplateTask(prd *PRD, t *Task, note string) {
	old := t.Status
	now := m.genNow()
	m.store.WithLock(func() {
		t.Status = TaskCompleted
		t.CompletedAt = &now
		t.Error = ""
	})
	dec := Decision{At: now, Kind: generatedArtefactDecisionKind, Actor: "autonomous", Note: note}
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, dec)
	})
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] %s/%s save daemon-run task: %v", prd.ID, t.ID, err)
		return
	}
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s save daemon-run decision: %v", prd.ID, err)
	}
	m.emitTaskStatus(prd, t, old, nil, nil)
}

// failDaemonTemplateTask marks the task failed with the error text and saves it.
func (m *Manager) failDaemonTemplateTask(prd *PRD, t *Task, cause error) {
	old := t.Status
	m.store.WithLock(func() {
		t.Status = TaskFailed
		t.Error = cause.Error()
	})
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] %s/%s save failed daemon-run task: %v", prd.ID, t.ID, err)
		return
	}
	m.emitTaskStatus(prd, t, old, nil, nil)
}
