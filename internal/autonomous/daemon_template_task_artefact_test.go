package autonomous

// End-to-end checks for daemon-run artefact templates. An Automaton is built in
// a temporary project directory with two completed worker tasks that carry saved
// change records, plus template tasks produced by ApplyTaskTemplates. The real
// executor (Manager.Run) runs it with a spawn function that fails the test if it
// is ever called, so every daemon-mode template task must complete without a
// session.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

var artefactTestNow = time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)

// artefactTemplateConfig enables the template pack. Every built-in id not named
// in modes is set to off, so only the ids under test can produce tasks.
func artefactTemplateConfig(modes map[string]string) *config.Config {
	cfg := &config.Config{}
	cfg.Autonomous.TaskTemplates.Enabled = true
	all := map[string]string{
		TemplateDocsSet:                "off",
		TemplateBoundedLoadStep:        "off",
		TemplateFinalGate:              "off",
		TemplateVersionBumpLeftToMerge: "off",
	}
	for id, mode := range modes {
		all[id] = mode
	}
	cfg.Autonomous.TaskTemplates.Mode = all
	return cfg
}

// artefactSaveRecords gives each worker task a distinct saved change record, so
// the merge that the artefacts are built from has more than one input.
func artefactSaveRecords(t *testing.T, m *Manager, workers []Task) {
	t.Helper()
	recs := []*changerecord.ChangeRecord{
		{
			Timestamp:     artefactTestNow,
			Files:         []changerecord.FileChange{{Path: "internal/alpha/alpha.go", Status: changerecord.StatusAdded}},
			TestsAdded:    []string{"TestAlpha"},
			Commands:      []changerecord.CommandRun{{Command: "go test ./internal/alpha", ExitCode: 0, Pass: 2}},
			SettingsAdded: nil,
		},
		{
			Timestamp:  artefactTestNow.Add(time.Minute),
			Files:      []changerecord.FileChange{{Path: "internal/beta/beta.go", Status: changerecord.StatusModified}},
			TestsAdded: []string{"TestBeta"},
			Commands:   []changerecord.CommandRun{{Command: "go test ./internal/beta", ExitCode: 0, Pass: 3}},
		},
	}
	for i := range workers {
		recs[i].TaskID = workers[i].ID
		recs[i].AutomatonID = ""
		if err := changerecord.Save(m.dataDir, workers[i].ID, recs[i]); err != nil {
			t.Fatalf("changerecord.Save(%s): %v", workers[i].ID, err)
		}
	}
}

// artefactFixture builds the Automaton and returns it with its project directory.
// The worker tasks are completed and have saved records; the template tasks are
// whatever ApplyTaskTemplates produced for the given modes.
func artefactFixture(t *testing.T, modes map[string]string) (*Manager, *PRD, string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetGeneratedArtefacts(func() config.EffectiveGeneratedArtefactsSettings {
		return config.GeneratedArtefacts{}.Effective()
	})
	m.genArt.now = func() time.Time { return artefactTestNow }

	projectDir := t.TempDir()
	prd, err := m.CreatePRD("spec", "/p", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	workers := []Task{
		{Title: "first", Spec: "build the alpha part", Status: TaskCompleted},
		{Title: "second", Spec: "build the beta part", Status: TaskCompleted},
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "Story", Tasks: workers}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	artefactSaveRecords(t, m, prd.Story[0].Tasks)

	stories, _ := ApplyTaskTemplates(prd.Story, "feature", projectDir, artefactTemplateConfig(modes))
	prd.Story = stories
	prd.ProjectDir = projectDir
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd, projectDir
}

// artefactRun runs the Automaton with a spawn function that fails the test if
// it is called, and a verify function that fails the test if it is called.
func artefactRun(t *testing.T, m *Manager, prdID string) {
	t.Helper()
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		t.Fatalf("spawn was called for task %q; daemon-run templates must not reach a session", req.Title)
		return SpawnResult{}, nil
	}
	verify := func(_ context.Context, _ *PRD, task *Task) (VerificationResult, error) {
		t.Fatalf("verify was called for task %q", task.Title)
		return VerificationResult{}, nil
	}
	if err := m.Run(context.Background(), prdID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// artefactTaskByTemplate returns the template task of the given id, or nil.
func artefactTaskByTemplate(prd *PRD, id string) *Task {
	for _, st := range prd.Story {
		for i := range st.Tasks {
			if taskTemplateIDOfSpec(st.Tasks[i].Spec) == id {
				return &st.Tasks[i]
			}
		}
	}
	return nil
}

// artefactDecisionNotes returns every decision note the Automaton recorded.
func artefactDecisionNotes(prd *PRD) []string {
	var notes []string
	for _, d := range prd.Decisions {
		notes = append(notes, d.Note)
	}
	return notes
}

func TestDaemonTemplateArtefactMatchesCompletionWrite(t *testing.T) {
	modes := map[string]string{
		TemplateChangelogEntry:   "daemon",
		TemplateParityIssueDraft: "daemon",
	}
	m, prd, projectDir := artefactFixture(t, modes)
	artefactRun(t, m, prd.ID)

	done, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("Automaton disappeared after Run")
	}
	// Reference: the completion-time write, run against a separate directory with
	// the same Automaton identity and the same saved records.
	refDir := t.TempDir()
	ref := *done
	ref.ProjectDir = refDir
	m.writeGeneratedArtefacts(&ref)

	s := config.GeneratedArtefacts{}.Effective()
	opts := m.generatedArtefactOptions(done, s)
	rec := &changerecord.ChangeRecord{AutomatonID: done.ID}
	for _, k := range []genartifacts.Kind{genartifacts.ChangelogDraft, genartifacts.ParityDraft} {
		rel := genartifacts.TrackedPath(k, rec, opts)
		got, err := os.ReadFile(filepath.Join(projectDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: daemon artefact not written at %s: %v", k, rel, err)
		}
		want, err := os.ReadFile(filepath.Join(refDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: reference artefact not written at %s: %v", k, rel, err)
		}
		if len(got) == 0 {
			t.Fatalf("%s: daemon artefact is empty", k)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: daemon artefact differs from completion write\n--- daemon ---\n%s\n--- completion ---\n%s", k, got, want)
		}
	}
}

func TestDaemonTemplateArtefactTasksCompleteWithDaemonDecision(t *testing.T) {
	modes := map[string]string{
		TemplateChangelogEntry:   "daemon",
		TemplateParityIssueDraft: "daemon",
	}
	m, prd, _ := artefactFixture(t, modes)
	artefactRun(t, m, prd.ID)

	done, _ := m.Store().GetPRD(prd.ID)
	notes := artefactDecisionNotes(done)
	for _, id := range []string{TemplateChangelogEntry, TemplateParityIssueDraft} {
		task := artefactTaskByTemplate(done, id)
		if task == nil {
			t.Fatalf("no task for template %s", id)
		}
		if !task.DaemonRun {
			t.Errorf("template %s task is not marked DaemonRun", id)
		}
		if task.Status != TaskCompleted {
			t.Errorf("template %s status = %q, want %q (error %q)", id, task.Status, TaskCompleted, task.Error)
		}
		want := "daemon completed task " + id + ":"
		found := false
		for _, n := range notes {
			if strings.Contains(n, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no decision says the daemon did template %s; notes: %q", id, notes)
		}
	}
}

func TestDaemonTemplateArtefactStoryCompletesAndRollsUp(t *testing.T) {
	modes := map[string]string{
		TemplateChangelogEntry:   "daemon",
		TemplateParityIssueDraft: "daemon",
	}
	m, prd, _ := artefactFixture(t, modes)
	artefactRun(t, m, prd.ID)

	done, _ := m.Store().GetPRD(prd.ID)
	if len(done.Story) != 1 {
		t.Fatalf("stories = %d, want 1", len(done.Story))
	}
	if got := done.Story[0].Status; got != StoryCompleted {
		t.Errorf("story status = %q, want %q", got, StoryCompleted)
	}
	if want := artefactBaselineStatus(t); done.Status != want {
		t.Errorf("Automaton status = %q, want the baseline roll-up %q", done.Status, want)
	}
}

// artefactBaselineStatus runs the same Automaton with both artefact templates
// off (so no daemon task exists) and returns the Automaton status it rolls up
// to. The daemon-run variants must roll up to the same status.
func artefactBaselineStatus(t *testing.T) PRDStatus {
	t.Helper()
	m, prd, _ := artefactFixture(t, map[string]string{
		TemplateChangelogEntry:   "off",
		TemplateParityIssueDraft: "off",
	})
	artefactRun(t, m, prd.ID)
	base, _ := m.Store().GetPRD(prd.ID)
	return base.Status
}

func TestDaemonTemplateArtefactOffTemplateAddsNoTask(t *testing.T) {
	// Changelog is off, parity is daemon-run. Only the parity task may appear.
	modes := map[string]string{
		TemplateChangelogEntry:   "off",
		TemplateParityIssueDraft: "daemon",
	}
	m, prd, _ := artefactFixture(t, modes)
	if artefactTaskByTemplate(prd, TemplateChangelogEntry) != nil {
		t.Fatal("a task was created for a template set to off")
	}
	artefactRun(t, m, prd.ID)

	done, _ := m.Store().GetPRD(prd.ID)
	if artefactTaskByTemplate(done, TemplateChangelogEntry) != nil {
		t.Fatal("a changelog task appeared after Run for a template set to off")
	}
	if artefactTaskByTemplate(done, TemplateParityIssueDraft) == nil {
		t.Fatal("the daemon-run parity task is missing")
	}
	if got := done.Story[0].Status; got != StoryCompleted {
		t.Errorf("story status = %q, want %q", got, StoryCompleted)
	}
	if want := artefactBaselineStatus(t); done.Status != want {
		t.Errorf("Automaton status = %q, want the baseline roll-up %q", done.Status, want)
	}
}
