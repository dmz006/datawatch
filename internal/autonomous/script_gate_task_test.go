package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// scriptGateFixture builds an Automaton whose story holds a finished worker
// task and a final_gate task that names script (a file under the project's
// verify folder) as its gate. body is the script text; empty writes none.
func scriptGateFixture(t *testing.T, script, body string, timeoutSeconds int) (*Manager, *PRD, *Task, string) {
	t.Helper()
	dir := t.TempDir()
	if body != "" {
		if err := os.MkdirAll(filepath.Join(dir, ".datawatch", "verify"), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, script), []byte(body), 0o755); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	cfg := DefaultConfig()
	cfg.ScriptGateTimeoutSeconds = timeoutSeconds
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "pi-prompt", "qwen3", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	gate := Task{Title: "gate", Spec: "run the gate", Kind: TaskKindFinalGate, VerificationScript: script}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{
		{Title: "worker", Spec: "do work", Status: TaskCompleted},
		gate,
	}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	prd.ProjectDir = dir
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd, &prd.Story[0].Tasks[1], dir
}

func runScriptGate(t *testing.T, m *Manager, prd *PRD, task *Task) error {
	t.Helper()
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		t.Fatal("spawn was called for a daemon-run gate")
		return SpawnResult{}, nil
	}
	return m.executeOne(context.Background(), prd, task, spawn, nil, 0, pipeline.QualityGateConfig{}, nil)
}

func readGateReport(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".datawatch", "reports", "final-gate.txt"))
	if err != nil {
		t.Fatalf("gate report not written: %v", err)
	}
	return string(b)
}

func TestScriptGateTaskPassingGateCompletes(t *testing.T) {
	m, prd, task, dir := scriptGateFixture(t, ".datawatch/verify/gate.sh", "echo all good\nexit 0\n", 30)
	if err := runScriptGate(t, m, prd, task); err != nil {
		t.Fatalf("executeOne: %v", err)
	}
	got, note := reloadedTask(t, m, prd.ID, task.ID)
	if got.Status != TaskCompleted {
		t.Fatalf("status = %q, want %q", got.Status, TaskCompleted)
	}
	if rep := readGateReport(t, dir); !strings.Contains(rep, "all good") || !strings.Contains(rep, "passed") {
		t.Fatalf("report = %q, want output and a passed verdict", rep)
	}
	if !strings.Contains(note, "the daemon ran the gate") {
		t.Fatalf("decision note = %q", note)
	}
}

func TestScriptGateTaskFailingGateFails(t *testing.T) {
	m, prd, task, dir := scriptGateFixture(t, ".datawatch/verify/gate.sh", "echo broken thing\nexit 3\n", 30)
	if err := runScriptGate(t, m, prd, task); err == nil {
		t.Fatal("executeOne returned nil, want the gate failure")
	}
	got, note := reloadedTask(t, m, prd.ID, task.ID)
	if got.Status != TaskFailed {
		t.Fatalf("status = %q, want %q", got.Status, TaskFailed)
	}
	if !strings.Contains(got.Error, "3") {
		t.Fatalf("error = %q, want the exit code", got.Error)
	}
	if rep := readGateReport(t, dir); !strings.Contains(rep, "broken thing") || !strings.Contains(rep, "failed") {
		t.Fatalf("report = %q, want the output and a failed verdict", rep)
	}
	if !strings.Contains(note, "exit status 3") {
		t.Fatalf("decision note = %q, want the exit status", note)
	}
}

func TestScriptGateTaskTimedOutGateNamesLimit(t *testing.T) {
	m, prd, task, dir := scriptGateFixture(t, ".datawatch/verify/gate.sh", "sleep 30\n", 1)
	if err := runScriptGate(t, m, prd, task); err == nil {
		t.Fatal("executeOne returned nil, want the timeout failure")
	}
	got, note := reloadedTask(t, m, prd.ID, task.ID)
	if got.Status != TaskFailed {
		t.Fatalf("status = %q, want %q", got.Status, TaskFailed)
	}
	if !strings.Contains(got.Error, "1-second limit") {
		t.Fatalf("error = %q, want it to name the limit", got.Error)
	}
	if !strings.Contains(note, "1-second limit") {
		t.Fatalf("decision note = %q, want it to name the limit", note)
	}
	if rep := readGateReport(t, dir); !strings.Contains(rep, "timeout") {
		t.Fatalf("report = %q, want a timeout verdict", rep)
	}
}

func TestScriptGateTaskNothingRunnableFailsWithReason(t *testing.T) {
	m, prd, task, _ := scriptGateFixture(t, "", "", 30)
	if err := runScriptGate(t, m, prd, task); err == nil {
		t.Fatal("executeOne returned nil, want the reason")
	}
	got, _ := reloadedTask(t, m, prd.ID, task.ID)
	if got.Status != TaskFailed {
		t.Fatalf("status = %q, want %q", got.Status, TaskFailed)
	}
	if !strings.Contains(got.Error, "nothing runnable") {
		t.Fatalf("error = %q, want the reason", got.Error)
	}
}

func TestScriptGateTaskNotHandledForOrdinaryTask(t *testing.T) {
	m, prd, task, _ := scriptGateFixture(t, ".datawatch/verify/gate.sh", "exit 0\n", 30)
	task.Kind = ""
	handled, err := m.runScriptGateTask(context.Background(), prd, task)
	if handled || err != nil {
		t.Fatalf("runScriptGateTask = (%v, %v), want (false, nil)", handled, err)
	}
}

func TestScriptGateTaskStoryCompletesWhenGatePasses(t *testing.T) {
	m, prd, task, _ := scriptGateFixture(t, ".datawatch/verify/gate.sh", "exit 0\n", 30)
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		t.Fatal("spawn was called for a daemon-run gate")
		return SpawnResult{}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if st := got.Story[0].Status; st != StoryCompleted {
		t.Fatalf("story status = %q, want %q", st, StoryCompleted)
	}
	if tk, _ := reloadedTask(t, m, prd.ID, task.ID); tk.Status != TaskCompleted {
		t.Fatalf("gate task status = %q, want %q", tk.Status, TaskCompleted)
	}
}
