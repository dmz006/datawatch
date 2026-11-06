package autonomous

import "testing"

// TestTaskOverrideAdmittedWorkerStale reproduces the stale admitted-worker
// bug: a task is admitted with its own worker (the admitted pair is recorded
// on the task), and only then the operator sets a different task-level
// Backend/Model override (an escalation-review requeue, or set_task_llm on
// the queued task). admittedWorker must follow the task's new override;
// today it unconditionally returns the recorded admitted pair whenever the
// task has a backend and a record exists, so the override is ignored and the
// task keeps running on the old pool.
func TestTaskOverrideAdmittedWorkerStale(t *testing.T) {
	prd := &PRD{Backend: "ollama", Model: "qwen"}
	task := &Task{Backend: "pi-prompt", Model: "qwen", OperatorWorkerSet: true}

	// The first attempt is admitted with the task's own worker; the
	// admission is recorded on the task.
	recordAdmittedWorker(task, ExecutorSourceTask, "pi-prompt", "local/qwen")
	if task.AdmittedBackend != "pi-prompt" || task.AdmittedModel != "local/qwen" {
		t.Fatalf("setup: admission not recorded: %+v", task)
	}

	// The operator then switches the task's own worker after the admission
	// record exists.
	task.Backend, task.Model = "claude-code", "sonnet"

	b, m := admittedWorker(task, nil, prd, nil, "ollama")
	if b != "claude-code" || m != "sonnet" {
		t.Fatalf("admittedWorker got %s/%s, want the task's new override claude-code/sonnet (stale admitted pair: %s/%s)", b, m, task.AdmittedBackend, task.AdmittedModel)
	}
}
