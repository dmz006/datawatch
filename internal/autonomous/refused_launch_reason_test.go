package autonomous

// A launch the daemon refuses (an opencode.json it cannot read) must leave the
// task saying why. This records the existing spawn-error path.

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRefusedLaunchEndsWithTheReasonOnTheTask(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	// The shape of the loopback spawn's error for a refused start.
	refusal := errors.New("session start: 500 Internal Server Error — launch refused: " +
		"datawatch cannot safely add its settings to opencode.json, so this session would run on a model nobody chose " +
		"(make the file plain JSON with no comments, or move it away): opencode config not written: " +
		"/srv/projects/a-rather-long-project-name/opencode.json cannot be parsed")
	_, task := r.run(t, func(context.Context, SpawnRequest, int) (SpawnResult, error) {
		return SpawnResult{}, refusal
	}, passingVerify)

	if task.Status == TaskCompleted {
		t.Fatalf("a task whose worker never started is completed")
	}
	if task.FailureReason == nil || !strings.Contains(task.FailureReason.Detail, "model nobody chose") {
		t.Errorf("status %q, failure reason %+v, want one saying why the launch was refused", task.Status, task.FailureReason)
	}
	if r.spawned() != 4 {
		t.Errorf("spawns = %d, want 4 (the launch and the default 3 retries)", r.spawned())
	}
	t.Logf("status=%s reason=%+v", task.Status, task.FailureReason)
}
