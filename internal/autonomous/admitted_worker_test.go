package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/pipeline"
)

func TestAdmittedWorkerIgnoresRecordForTaskWithoutBackend(t *testing.T) {
	prd := &PRD{Backend: "ollama", Model: "qwen"}
	task := &Task{AdmittedBackend: "claude-code", AdmittedModel: "sonnet"}
	b, m := admittedWorker(task, nil, prd, nil, "global")
	if b != "ollama" || m != "qwen" {
		t.Fatalf("got %s/%s, want the PRD worker for a task with no backend", b, m)
	}
}

func TestAdmittedWorkerUsesRecordForExplicitBackend(t *testing.T) {
	prd := &PRD{Backend: "ollama", Model: "qwen"}
	task := &Task{Backend: "claude-code", Model: "sonnet", OperatorWorkerSet: true}
	b, m := admittedWorker(task, nil, prd, nil, "ollama")
	if b != "claude-code" || m != "sonnet" {
		t.Fatalf("unrecorded: got %s/%s, want the task's explicit worker", b, m)
	}
	task.AdmittedBackend, task.AdmittedModel = "claude-code", "anthropic/sonnet"
	b, m = admittedWorker(task, nil, prd, nil, "ollama")
	if b != "claude-code" || m != "anthropic/sonnet" {
		t.Fatalf("recorded: got %s/%s, want the admitted pair", b, m)
	}
}

func TestRecordAdmittedWorkerKeepsTaskRecordInSync(t *testing.T) {
	task := &Task{Backend: "pi-prompt", Model: "qwen"}
	recordAdmittedWorker(task, ExecutorSourceTask, "pi-prompt", "local/qwen")
	if task.Model != "local/qwen" || task.AdmittedModel != "local/qwen" {
		t.Fatalf("task model %q admitted %q, want both local/qwen", task.Model, task.AdmittedModel)
	}
	// An escalated attempt is admitted with another worker; the task's own
	// worker stays as the operator set it.
	task2 := &Task{Backend: "pi-prompt", Model: "qwen"}
	recordAdmittedWorker(task2, ExecutorSourceEscalation, "claude-code", "opus")
	if task2.Backend != "pi-prompt" || task2.Model != "qwen" || task2.AdmittedBackend != "claude-code" {
		t.Fatalf("escalated attempt rewrote the task worker: %+v", task2)
	}
	// A task with no backend of its own records no admitted pair.
	task3 := &Task{AdmittedBackend: "stale", AdmittedModel: "stale"}
	recordAdmittedWorker(task3, ExecutorSourceGlobal, "ollama", "qwen")
	if task3.AdmittedBackend != "" || task3.AdmittedModel != "" {
		t.Fatalf("task without backend kept an admitted pair: %+v", task3)
	}
}

func TestReviewRequeueClearsAdmittedWorker(t *testing.T) {
	task := &Task{Backend: "pi-prompt", AdmittedBackend: "pi-prompt", AdmittedModel: "old"}
	r := &reviewRequeue{workerChanged: true}
	r.req.Backend, r.req.Model = "claude-code", "sonnet"
	r.apply(task, time.Now())
	if task.Backend != "claude-code" || task.Model != "sonnet" || !task.OperatorWorkerSet {
		t.Fatalf("worker not applied: %+v", task)
	}
	if task.AdmittedBackend != "" || task.AdmittedModel != "" {
		t.Fatalf("admitted pair survived the requeue: %+v", task)
	}
}

// TestExecuteOneAdmitsAndRecordsExplicitWorker drives the dispatch path: a task
// carrying an explicit backend and model is admitted with exactly that pair,
// spawned with it, and its persisted record matches.
func TestExecuteOneAdmitsAndRecordsExplicitWorker(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	led := capacity.New(capacity.Options{PollInterval: time.Second, Clock: newFakeCapClock()})
	led.SetLimit("host", 4)
	led.SetLimit("llm:claude-code", 2)
	var mu sync.Mutex
	var seen [][2]string
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		mu.Lock()
		seen = append(seen, [2]string{backend, model})
		mu.Unlock()
		if backend != "claude-code" {
			return nil, ""
		}
		return []string{"host", "llm:claude-code"}, ""
	})
	prd, _ := m.CreatePRD("spec", "", "pi-prompt", "qwen3", "")
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{
		{Title: "t", Spec: "do it", Backend: "claude-code", Model: "sonnet", OperatorWorkerSet: true},
	}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	task := &prd.Story[0].Tasks[0]

	var spawned SpawnRequest
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned = req
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	_ = m.executeOne(context.Background(), prd, task, spawn, verify, 0, pipeline.QualityGateConfig{}, nil)

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("capacity was never consulted")
	}
	for _, s := range seen {
		if s[0] != "claude-code" {
			t.Fatalf("admission keyed on %v, want the task's claude-code worker", s)
		}
	}
	if spawned.Backend != "claude-code" {
		t.Errorf("spawned backend %q, want claude-code", spawned.Backend)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	gt := got.Story[0].Tasks[0]
	if gt.Backend != "claude-code" || gt.AdmittedBackend != "claude-code" || gt.Model != gt.AdmittedModel {
		t.Errorf("persisted record %s/%s admitted %s/%s does not match admission", gt.Backend, gt.Model, gt.AdmittedBackend, gt.AdmittedModel)
	}
}
