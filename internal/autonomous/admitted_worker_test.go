package autonomous

import "testing"

func TestAdmittedWorkerPrefersRecordedPair(t *testing.T) {
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
}
