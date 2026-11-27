// The order work is taken up after a restart or a resume: a running worker
// already uses its slot, so it is counted and walked before any task that
// still has to wait for one. Fake sessions, temp dirs, no daemon.
package autonomous

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// One slot, and a run order that puts waiting tasks ahead of the running one.
// If they take every lane they wait for the slot of a task that never gets a
// lane, so all of them must wait out the capacity limit.
func TestCapacityRestart_RunningTaskGetsALaneFirst(t *testing.T) {
	// The tasks share one tree. A tree wait looks again well inside the short
	// capacity wait set below.
	old := capRetryPause
	capRetryPause = 10 * time.Millisecond
	t.Cleanup(func() { capRetryPause = old })
	for _, c := range []struct {
		name  string
		lanes int
		order []string
	}{
		{"two lanes: parked, never started, running", 2, []string{"parked", "new", "running"}},
		{"one lane: parked, running", 1, []string{"parked", "running"}},
		{"one lane: never started, running", 1, []string{"new", "running"}},
		{"two lanes: parked, parked, running verification", 2, []string{"parked", "parked", "verifying"}},
		{"one lane: never started, parked", 1, []string{"new", "parked"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, api, led := capFixture(t, 1)
			m.mu.Lock()
			m.cfg.CapacityWaitTimeoutSeconds = 3
			m.mu.Unlock()
			prd := newPRDWithTasks(t, m, len(c.order))
			cur, _ := m.Store().GetPRD(prd.ID)
			cur.Status, cur.ProjectDir, cur.MaxConcurrentTasks = PRDRunning, t.TempDir(), c.lanes
			_ = m.Store().SavePRD(cur)
			ids, err := topoSort(flattenTasks(cur))
			if err != nil {
				t.Fatal(err)
			}
			parked := int32(0)
			for i, role := range c.order {
				tk := lookupTask(cur, ids[i])
				switch role {
				case "parked":
					parked++
					tk.Status, tk.SessionID = TaskWaitingCapacity, "parked-"+tk.ID
					tk.WaitReason = "await_external: CI run #7"
				case "running":
					tk.Status, tk.SessionID = TaskInProgress, "running-"+tk.ID
				case "verifying":
					tk.Status, tk.SessionID = TaskVerifying, "running-"+tk.ID
				}
				_ = m.Store().SaveTask(tk)
			}
			newGapSessions(m)
			var prompts, spawns, early int32
			m.SetSessionContinueFn(func(string) error { atomic.AddInt32(&prompts, 1); return nil })
			m.SetSessionWaitFn(func(context.Context, AdoptedWait) error {
				time.Sleep(20 * time.Millisecond)
				return nil
			})
			led.BeginAdoption()
			api.SetExecutors(func(context.Context, SpawnRequest) (SpawnResult, error) {
				if atomic.LoadInt32(&prompts) < parked {
					atomic.AddInt32(&early, 1)
				}
				n := atomic.AddInt32(&spawns, 1)
				return SpawnResult{SessionID: fmt.Sprintf("new-%d", n)}, nil
			}, func(context.Context, *PRD, *Task) (VerificationResult, error) {
				return VerificationResult{OK: true}, nil
			})
			pollPRDTerminal(t, m, cur.ID, 15*time.Second)
			if err := api.WaitIdle(10 * time.Second); err != nil {
				t.Fatal(err)
			}
			snap, _ := m.Store().SnapshotPRD(cur.ID)
			for _, tk := range snap.Story[0].Tasks {
				if tk.Status != TaskCompleted {
					t.Errorf("task %s = %s (%s), want completed", tk.ID, tk.Status, tk.Error)
				}
			}
			if got := atomic.LoadInt32(&prompts); got != parked {
				t.Errorf("prompts = %d, want %d (one for each parked task)", got, parked)
			}
			// A parked task had a lane before the restart: it gets one back
			// before a task that never started, which may need what it holds.
			if n := atomic.LoadInt32(&early); n != 0 {
				t.Errorf("%d new session(s) started ahead of a parked task", n)
			}
			if n := len(led.Leases()); n != 0 {
				t.Errorf("%d lease(s) left", n)
			}
		})
	}
}

// A resume with no restart hold (an operator resume, or the hold off or timed
// out) counts the running worker before it looks for room for a parked one,
// whatever order the tasks are listed in.
func TestCapacityRestart_RunningWorkersAreCountedBeforeParkedOnes(t *testing.T) {
	for _, status := range []TaskStatus{TaskInProgress, TaskVerifying} {
		t.Run(string(status), func(t *testing.T) {
			m, _, led := capFixture(t, 1)
			prd := newPRDWithTasks(t, m, 2)
			cur, _ := m.Store().GetPRD(prd.ID)
			cur.Status = PRDRunning
			_ = m.Store().SavePRD(cur)
			parked, running := &cur.Story[0].Tasks[0], &cur.Story[0].Tasks[1]
			parked.Status, parked.SessionID = TaskWaitingCapacity, "parked-sess"
			parked.WaitReason = "await_external: CI run #7"
			running.Status, running.SessionID = status, "running-sess"
			_ = m.Store().SaveTask(parked)
			_ = m.Store().SaveTask(running)
			newGapSessions(m)
			var prompts int32
			m.SetSessionContinueFn(func(string) error { atomic.AddInt32(&prompts, 1); return nil })

			m.resetInProgressTasksForResume(cur)

			if n := len(led.Leases()); n != 1 || !led.Held(running.ID) {
				t.Fatalf("%d lease(s) on 1 slot, running task holds one: %v", n, led.Held(running.ID))
			}
			if n := atomic.LoadInt32(&prompts); n != 0 {
				t.Fatalf("the parked session was prompted %d time(s) with the slot in use", n)
			}
			got := testTaskAt(t, m, prd.ID, 0)
			if got.Status != TaskWaitingCapacity || !IsAwaitExternalWaitReason(got.WaitReason) {
				t.Fatalf("parked task = %s %q, want it still waiting", got.Status, got.WaitReason)
			}
		})
	}
}
