package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/genartifacts"
	"github.com/dmz006/datawatch/internal/pipeline"
)

// daemonTemplateFixture builds an Automaton with one story holding a finished
// worker task and one daemon-run template task. projectDir is set on the
// Automaton as given.
func daemonTemplateFixture(t *testing.T, spec, projectDir string, daemon bool) (*Manager, *PRD, *Task) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "pi-prompt", "qwen3", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{
		{Title: "worker", Spec: "do work", Status: TaskCompleted},
		{Title: "template", Spec: spec, DaemonRun: daemon},
	}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	prd.ProjectDir = projectDir
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd, &prd.Story[0].Tasks[1]
}

// failIfSpawned is the spawn function for daemon-run tests: a daemon-run task
// must never reach it.
func failIfSpawned(t *testing.T) SpawnFn {
	t.Helper()
	return func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		t.Fatal("spawn was called for a daemon-run template task")
		return SpawnResult{}, nil
	}
}

// reloadedTask returns the task's saved state and the Automaton's last decision note.
func reloadedTask(t *testing.T, m *Manager, prdID, taskID string) (Task, string) {
	t.Helper()
	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("GetPRD(%s): not found", prdID)
	}
	for _, st := range prd.Story {
		for _, tk := range st.Tasks {
			if tk.ID == taskID {
				note := ""
				if n := len(prd.Decisions); n > 0 {
					note = prd.Decisions[n-1].Note
				}
				return tk, note
			}
		}
	}
	t.Fatalf("task %s not found", taskID)
	return Task{}, ""
}

func TestDaemonTemplateTaskRunsChangelogEntry(t *testing.T) {
	dir := t.TempDir()
	spec := "Write the changelog entry. (" + TaskTemplateIDMarker("changelog_entry") + ")"
	m, prd, task := daemonTemplateFixture(t, spec, dir, true)
	// Give the completed worker task a saved change record so the merge has input.
	if err := changerecord.Save(m.dataDir, prd.Story[0].Tasks[0].ID, &changerecord.ChangeRecord{TaskID: prd.Story[0].Tasks[0].ID}); err != nil {
		t.Fatalf("Save change record: %v", err)
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		t.Fatal("verify must not run for a daemon-run template task")
		return VerificationResult{}, nil
	}
	if err := m.executeOne(context.Background(), prd, task, failIfSpawned(t), verify, 0, pipeline.QualityGateConfig{}, nil); err != nil {
		t.Fatalf("executeOne: %v", err)
	}
	got, note := reloadedTask(t, m, prd.ID, task.ID)
	if got.Status != TaskCompleted {
		t.Fatalf("status = %q, want %q", got.Status, TaskCompleted)
	}
	draft := genartifacts.TrackedPath(genartifacts.ChangelogDraft, &changerecord.ChangeRecord{AutomatonID: prd.ID}, genartifacts.Options{Root: dir})
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(draft))); err != nil {
		t.Fatalf("changelog draft not written at %s: %v", draft, err)
	}
	if !strings.Contains(note, "daemon completed task") || !strings.Contains(note, draft) {
		t.Fatalf("decision note = %q, want it to name %s", note, draft)
	}
}

func TestDaemonTemplateTaskRunsParityIssueDraft(t *testing.T) {
	dir := t.TempDir()
	spec := "Draft the parity issue. (" + TaskTemplateIDMarker("parity_issue_draft") + ")"
	m, prd, task := daemonTemplateFixture(t, spec, dir, true)
	if err := m.executeOne(context.Background(), prd, task, failIfSpawned(t), nil, 0, pipeline.QualityGateConfig{}, nil); err != nil {
		t.Fatalf("executeOne: %v", err)
	}
	got, note := reloadedTask(t, m, prd.ID, task.ID)
	if got.Status != TaskCompleted {
		t.Fatalf("status = %q, want %q", got.Status, TaskCompleted)
	}
	draft := genartifacts.TrackedPath(genartifacts.ParityDraft, &changerecord.ChangeRecord{AutomatonID: prd.ID}, genartifacts.Options{Root: dir})
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(draft))); err != nil {
		t.Fatalf("parity draft not written at %s: %v", draft, err)
	}
	if !strings.Contains(note, draft) {
		t.Fatalf("decision note = %q, want it to name %s", note, draft)
	}
}

func TestDaemonTemplateTaskRunsNoteTemplates(t *testing.T) {
	for _, id := range []string{"bounded_load_step", "version_bump_left_to_merge"} {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			spec := "Record the posture note. (" + TaskTemplateIDMarker(id) + ")"
			m, prd, task := daemonTemplateFixture(t, spec, dir, true)
			if err := m.executeOne(context.Background(), prd, task, failIfSpawned(t), nil, 0, pipeline.QualityGateConfig{}, nil); err != nil {
				t.Fatalf("executeOne: %v", err)
			}
			got, note := reloadedTask(t, m, prd.ID, task.ID)
			if got.Status != TaskCompleted {
				t.Fatalf("status = %q, want %q", got.Status, TaskCompleted)
			}
			if !strings.Contains(note, "note recorded, no file written") || !strings.Contains(note, "Record the posture note.") {
				t.Fatalf("decision note = %q, want the rendered template spec", note)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("ReadDir: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("note template wrote files into the project dir: %v", entries)
			}
		})
	}
}

func TestDaemonTemplateTaskRunsFailsOnGeneratorError(t *testing.T) {
	// An Automaton without a project directory cannot receive a generated artefact.
	spec := "Write the changelog entry. (" + TaskTemplateIDMarker("changelog_entry") + ")"
	m, prd, task := daemonTemplateFixture(t, spec, "", true)
	err := m.executeOne(context.Background(), prd, task, failIfSpawned(t), nil, 0, pipeline.QualityGateConfig{}, nil)
	if err == nil {
		t.Fatal("executeOne returned nil, want the generator error")
	}
	got, _ := reloadedTask(t, m, prd.ID, task.ID)
	if got.Status != TaskFailed {
		t.Fatalf("status = %q, want %q", got.Status, TaskFailed)
	}
	if !strings.Contains(got.Error, "project directory") {
		t.Fatalf("task error = %q, want the generator error text", got.Error)
	}
}

func TestDaemonTemplateTaskRunsNotHandledWithoutDaemonRun(t *testing.T) {
	dir := t.TempDir()
	spec := "Write the changelog entry. (" + TaskTemplateIDMarker("changelog_entry") + ")"
	m, prd, task := daemonTemplateFixture(t, spec, dir, false)
	handled, err := m.runDaemonTemplateTask(context.Background(), prd, task)
	if handled || err != nil {
		t.Fatalf("runDaemonTemplateTask = (%v, %v), want (false, nil)", handled, err)
	}
	if task.Status == TaskCompleted {
		t.Fatal("task without DaemonRun was completed by the daemon")
	}
	if _, err := os.Stat(filepath.Join(dir, "docs")); err == nil {
		t.Fatal("task without DaemonRun wrote a generated artefact")
	}
}
