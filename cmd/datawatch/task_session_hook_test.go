package main

import (
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// Every role session of a task carries the task id. Only the worker session is
// the task's session: a verifier or a task-level judge that starts later must
// not replace the worker's session id on the task.
func TestTaskSessionCreatedHookLinksOnlyTheWorkerSession(t *testing.T) {
	amgr, err := autonomouspkg.NewManager(t.TempDir(), autonomouspkg.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := amgr.CreatePRD("spec", "/projlink", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := amgr.Store().SetStories(prd.ID, []autonomouspkg.Story{
		{Title: "story", Tasks: []autonomouspkg.Task{{Title: "task", Status: autonomouspkg.TaskInProgress}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = amgr.Store().GetPRD(prd.ID)
	taskID := prd.Story[0].Tasks[0].ID
	hook := taskSessionCreatedHook(amgr)
	taskSession := func() string {
		got, ok := amgr.Store().GetTask(taskID)
		if !ok {
			t.Fatalf("task %s missing", taskID)
		}
		return got.SessionID
	}

	const worker = "host-aaaa"
	hook(&session.Session{FullID: worker, Name: autonomousTaskSessionName("Verify the thing"), PRDID: prd.ID, TaskID: taskID})
	if got := taskSession(); got != worker {
		t.Fatalf("worker session not linked: task session = %q, want %q", got, worker)
	}
	for _, name := range []string{
		"autonomous:verify:" + taskID,
		"autonomous:guardrail:" + taskID,
		"autonomous:decompose",
	} {
		hook(&session.Session{FullID: "host-bbbb", Name: name, PRDID: prd.ID, TaskID: taskID})
		if got := taskSession(); got != worker {
			t.Fatalf("after session %q was created the task session is %q, want the worker %q", name, got, worker)
		}
	}
}
