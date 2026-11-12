// A launch refused by a draining daemon is a wait, never a failed attempt.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func TestIsLaunchRefusedDraining(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"typed", session.ErrDaemonDraining, true},
		{"typed wrapped", fmt.Errorf("start: %w", session.ErrDaemonDraining), true},
		{"sentinel", ErrLaunchRefusedDraining, true},
		{"http 500 body text", errors.New("POST /api/sessions/start: 500: daemon is shutting down; a new launch is refused"), true},
		{"unrelated", errors.New("tmux: server exited unexpectedly"), false},
	}
	for _, c := range cases {
		if got := IsLaunchRefusedDraining(c.err); got != c.want {
			t.Errorf("%s: IsLaunchRefusedDraining = %v, want %v", c.name, got, c.want)
		}
		if c.want && !isSpawnWaitError(c.err) {
			t.Errorf("%s: isSpawnWaitError = false, want true", c.name)
		}
	}
}

func TestLaunchRefusedDraining_IsWaitNotAttempt(t *testing.T) {
	for _, refusal := range []error{
		session.ErrDaemonDraining,
		errors.New("500 Internal Server Error: daemon is shutting down; a new launch is refused"),
	} {
		m, api, _, verify := apiFixture(t)
		prd := newPRDWithTasks(t, m, 1)
		old := capRetryPause
		capRetryPause = 10 * time.Millisecond
		var mu sync.Mutex
		calls := 0
		spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if calls < 3 {
				return SpawnResult{}, refusal
			}
			return SpawnResult{SessionID: "s"}, nil
		}
		got := runToTerminal(t, m, api, prd.ID, spawn, verify)
		capRetryPause = old
		tk := got.Story[0].Tasks[0]
		if got.Status != PRDCompleted || calls != 3 {
			t.Fatalf("%v: status=%s calls=%d, want completed after 3 spawns", refusal, got.Status, calls)
		}
		if tk.RetryCount != 0 || tk.InfraRetryCount != 0 || len(tk.InfraRetries) != 0 ||
			tk.ClaudeRetries != 0 || tk.StartFailures != 0 {
			t.Errorf("%v: a counter moved: %+v", refusal, tk)
		}
		for _, a := range tk.Attempts {
			if a.Outcome != "" && a.Outcome != "completed" && a.Outcome != "passed" {
				t.Errorf("%v: refusal left a failed attempt entry: %+v", refusal, a)
			}
		}
		if tk.WaitReason != "" {
			t.Errorf("%v: wait reason not cleared: %q", refusal, tk.WaitReason)
		}
	}
}

// A daemon going down cancels the executor context; the wait ends with it and
// records no failure.
func TestLaunchRefusedDraining_DaemonRestartLeavesTaskParked(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	tk := &prd.Story[0].Tasks[0]
	old := capRetryPause
	capRetryPause = 10 * time.Millisecond
	defer func() { capRetryPause = old }()

	ctx, cancel := context.WithCancelCause(context.Background())
	calls := 0
	spawn := m.capacityRetrySpawn(tk, func(context.Context, SpawnRequest) (SpawnResult, error) {
		calls++
		if calls == 2 {
			cancel(ErrDaemonRestart)
		}
		return SpawnResult{}, session.ErrDaemonDraining
	})
	_, err := spawn(ctx, SpawnRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context cancellation", err)
	}
	if tk.RetryCount != 0 || tk.InfraRetryCount != 0 || len(tk.InfraRetries) != 0 || len(tk.Attempts) != 0 || tk.StartFailures != 0 {
		t.Errorf("shutdown wait touched counters: %+v", tk)
	}
}
