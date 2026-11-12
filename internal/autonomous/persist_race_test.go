package autonomous

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// runConcurrentPRDWithPersistDriver runs a PRD of independent tasks on the
// concurrent executor path (MaxConcurrentTasks=4) while a second goroutine
// continuously drives Store.persist through MutateTask and a deep-copy
// snapshot. Under -race this fails if executeOne (or the coordinator)
// mutates PRD/Task fields without the lock persist holds.
func runConcurrentPRDWithPersistDriver(t *testing.T, dir string) (*Manager, string) {
	t.Helper()
	m, err := NewManager(dir, DefaultConfig(), func(DecomposeRequest) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("persist race", "/w/proj", "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	var tasks []Task
	for i := 0; i < 8; i++ {
		tasks = append(tasks, Task{Title: fmt.Sprintf("T%d", i), Spec: "do it"})
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: tasks}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	m.Store().WithLock(func() {
		p.Status = PRDApproved
		p.MaxConcurrentTasks = 4
	})
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-" + req.TaskID, PreTaskSHA: "abc"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}

	var taskIDs []string
	if snap, ok := m.Store().SnapshotPRD(prd.ID); ok {
		for _, tk := range snap.Story[0].Tasks {
			taskIDs = append(taskIDs, tk.ID)
		}
	}

	done := make(chan error, 1)
	go func() { done <- m.Run(context.Background(), prd.ID, spawn, verify) }()

	driverStopped := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		defer close(driverStopped)
		for {
			select {
			case <-stop:
				return
			default:
			}
			// MutateTask persists the whole store without bumping the PRD's
			// Rev, so it cannot make the executor's roll-up look stale.
			for _, id := range taskIDs {
				m.Store().MutateTask(id, func(*Task) {})
			}
			_, _ = m.Store().SnapshotPRD(prd.ID)
		}
	}()

	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("Run did not return")
	}
	close(stop)
	<-driverStopped
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	return m, prd.ID
}

func TestPersistRace_ConcurrentExecutorVsPersist(t *testing.T) {
	m, id := runConcurrentPRDWithPersistDriver(t, t.TempDir())
	got, ok := m.Store().SnapshotPRD(id)
	if !ok {
		t.Fatal("PRD missing")
	}
	if got.Status != PRDCompleted {
		t.Fatalf("PRD status = %s, want completed", got.Status)
	}
	for _, tk := range got.Story[0].Tasks {
		if tk.Status != TaskCompleted {
			t.Errorf("task %s status = %s, want completed", tk.ID, tk.Status)
		}
	}
}

func TestPersistRace_OnDiskReloadsWithAllTasksCompleted(t *testing.T) {
	dir := t.TempDir()
	_, id := runConcurrentPRDWithPersistDriver(t, dir)

	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore on same dir: %v", err)
	}
	got, ok := reloaded.GetPRD(id)
	if !ok {
		t.Fatal("PRD missing after reload")
	}
	if len(got.Story) != 1 || len(got.Story[0].Tasks) != 8 {
		t.Fatalf("reloaded shape = %d stories", len(got.Story))
	}
	for _, tk := range got.Story[0].Tasks {
		if tk.Status != TaskCompleted {
			t.Errorf("reloaded task %s status = %s, want completed", tk.ID, tk.Status)
		}
		if tk.Verification == nil || !tk.Verification.OK {
			t.Errorf("reloaded task %s lost its verification result", tk.ID)
		}
	}
}
