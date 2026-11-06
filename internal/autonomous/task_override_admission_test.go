package autonomous

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

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

// TestTaskOverrideAdmissionKeepsRunningPair is the other half of the
// staleness rule: a task whose worker was admitted and has not been
// re-overridden keeps exactly the pair it was admitted with — a running
// attempt must not lose the admitted (qualified) model to a re-derivation —
// including an escalated attempt whose admitted worker differs from the
// task's own.
func TestTaskOverrideAdmissionKeepsRunningPair(t *testing.T) {
	prd := &PRD{Backend: "ollama", Model: "qwen"}

	// Task-source admission: the admitted pair was qualified to local/qwen
	// and the task's own fields have not changed since.
	task := &Task{Backend: "pi-prompt", Model: "qwen", OperatorWorkerSet: true}
	recordAdmittedWorker(task, ExecutorSourceTask, "pi-prompt", "local/qwen")
	b, m := admittedWorker(task, nil, prd, nil, "ollama")
	if b != "pi-prompt" || m != "local/qwen" {
		t.Fatalf("running attempt got %s/%s, want the admitted pair pi-prompt/local/qwen", b, m)
	}

	// Escalation admission: the admitted worker differs from the task's own
	// and the task's fields are unchanged — the recorded pair must stand.
	esc := &Task{Backend: "pi-prompt", Model: "qwen"}
	recordAdmittedWorker(esc, ExecutorSourceEscalation, "claude-code", "opus")
	b, m = admittedWorker(esc, nil, prd, nil, "ollama")
	if b != "claude-code" || m != "opus" {
		t.Fatalf("escalated running attempt got %s/%s, want the recorded pair claude-code/opus", b, m)
	}
}

// TestTaskOverrideAdmissionWaiterMovesToIdleLane pins the admission side of
// the override rule: a task still queued on its old backend's pool — behind
// the pool's occupant, holding a stale admitted record from its last
// admission — is retargeted by the override to the new backend's pool. That
// pool is idle, so the retargeted wait resolves to a single lease on it,
// admitted with the overridden worker, without any further polling.
func TestTaskOverrideAdmissionWaiterMovesToIdleLane(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	for _, n := range []string{"solo", "fastb"} {
		led.SetLimit("node:"+n, 1)
	}
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		if backend == "ollama" {
			return []string{"host", "llm:pool", "node:fastb"}, "fastb"
		}
		return []string{"host", "llm:pool", "node:solo"}, "solo"
	})
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	taskID := cur.Story[0].Tasks[0].ID
	// The task runs on its own worker (the old backend, whose pool is full
	// below) and was admitted with it last time it ran: the record is stale
	// for whatever the operator sets next.
	cur.Story[0].Tasks[0].Backend = "pi-prompt"
	cur.Story[0].Tasks[0].Model = "qwen"
	cur.Story[0].Tasks[0].OperatorWorkerSet = true
	cur.Story[0].Tasks[0].AdmittedBackend, cur.Story[0].Tasks[0].AdmittedModel = "pi-prompt", "local/qwen"
	cur.Story[0].Tasks[0].AdmittedTaskBackend, cur.Story[0].Tasks[0].AdmittedTaskModel = "pi-prompt", "qwen"
	cur.Story[0].Tasks[0].Status = TaskWaitingCapacity
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	queueWaiter(t, led, taskID, prd.ID) // fills node:solo and queues the task behind it
	if countTaskWaiters(led, taskID) != 1 {
		t.Fatalf("waiters = %d, want 1 on the full old pool before the override", countTaskWaiters(led, taskID))
	}
	if ls := led.LeaseOf(taskID); ls != nil {
		t.Fatalf("lease = %+v, want the task still queued on the full pool", ls)
	}

	_, res, err := m.SetTaskLLM(prd.ID, taskID, "ollama", "", "m2", "op")
	if err != nil {
		t.Fatalf("SetTaskLLM: %v", err)
	}
	if res == nil || len(res.Changed) != 1 || !res.Changed[0].CapacityMoved {
		t.Fatalf("res = %+v, want the task changed and its queued request retargeted", res)
	}

	// fastb is the idle lane: the retargeted wait resolves there at once,
	// with the lease admitted for the overridden worker.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if ls := led.LeaseOf(taskID); ls != nil {
			if ls.Node != "fastb" {
				t.Fatalf("lease node = %q, want the idle lane fastb", ls.Node)
			}
			if ls.Model != "m2" {
				t.Fatalf("lease model = %q, want the overridden model m2", ls.Model)
			}
			break
		}
		if time.Now().After(deadline) {
			snap := led.Snapshot()
			t.Fatalf("task never granted on the idle lane after retarget; waiting = %+v", snap.Waiting)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The record now follows the override: admission of this task again
	// resolves the new worker, not the stale admitted pair.
	got, _ := m.Store().GetPRD(prd.ID)
	task, _ := findTaskInPRD(got, taskID)
	if task.AdmittedBackend != "" || task.AdmittedModel != "" {
		t.Fatalf("stale admitted pair survived the override: %s/%s", task.AdmittedBackend, task.AdmittedModel)
	}
	b, mdl := admittedWorker(task, &got.Story[0], got, nil, m.cfg.ExecutionBackend)
	if b != "ollama" || mdl != "m2" {
		t.Fatalf("admittedWorker got %s/%s, want the overridden pair ollama/m2", b, mdl)
	}
}
