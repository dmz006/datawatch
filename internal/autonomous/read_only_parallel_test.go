package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// roTaskSpec names a task and whether it is flagged read-only.
type roTaskSpec struct {
	title    string
	readOnly bool
}

// makeRoPRD creates an approved PRD with the given tasks in one story and
// returns it with the task IDs in the same order.
func makeRoPRD(t *testing.T, mgr *Manager, dir string, specs []roTaskSpec) (*PRD, []string) {
	t.Helper()
	st := mgr.Store()
	prd, err := st.CreatePRD("read only parallel", dir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	var tasks []Task
	for _, s := range specs {
		tasks = append(tasks, Task{Title: s.title, Spec: "do " + s.title, ReadOnly: s.readOnly})
	}
	if err := st.SetStories(prd.ID, []Story{{Title: "S1", Tasks: tasks}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = st.GetPRD(prd.ID)
	prd.Status = PRDApproved
	prd.MaxConcurrentTasks = len(specs)
	if err := st.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	var ids []string
	for _, tk := range prd.Story[0].Tasks {
		ids = append(ids, tk.ID)
	}
	return prd, ids
}

func newRoManager(t *testing.T, readOnlyTasks *bool) *Manager {
	t.Helper()
	return newTreeLockExecutorManager(t, Config{Parallel: config.AutonomousParallelConfig{ReadOnlyTasks: readOnlyTasks}})
}

// roSpawnGate records the order tasks reach spawn and lets a test hold a
// chosen task inside spawn (the session "running") until released.
type roSpawnGate struct {
	arrived map[string]chan struct{}
	hold    map[string]chan struct{}
}

func newRoSpawnGate(ids []string, holdIDs ...string) *roSpawnGate {
	r := &roSpawnGate{arrived: map[string]chan struct{}{}, hold: map[string]chan struct{}{}}
	for _, id := range ids {
		r.arrived[id] = make(chan struct{})
	}
	for _, id := range holdIDs {
		r.hold[id] = make(chan struct{})
	}
	return r
}

func (r *roSpawnGate) spawn(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
	close(r.arrived[req.TaskID])
	if h, ok := r.hold[req.TaskID]; ok {
		select {
		case <-h:
		case <-ctx.Done():
			return SpawnResult{}, ctx.Err()
		}
	}
	return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
}

func (r *roSpawnGate) release(id string) { close(r.hold[id]) }

func waitArrived(t *testing.T, r *roSpawnGate, id string, within time.Duration) bool {
	t.Helper()
	select {
	case <-r.arrived[id]:
		return true
	case <-time.After(within):
		return false
	}
}

func runAsync(mgr *Manager, prdID string, spawn SpawnFn) (chan error, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	ran := make(chan error, 1)
	go func() { ran <- mgr.Run(ctx, prdID, spawn, tlVerify) }()
	return ran, cancel
}

func finishRun(t *testing.T, ran chan error) {
	t.Helper()
	select {
	case err := <-ran:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not finish")
	}
}

func TestReadOnlyTasksRunConcurrently(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil) // absent key reads on
	prd, ids := makeRoPRD(t, mgr, repo, []roTaskSpec{{"a", true}, {"b", true}})
	rec := newRoSpawnGate(ids, ids[0], ids[1])

	ran, cancel := runAsync(mgr, prd.ID, rec.spawn)
	defer cancel()
	if !waitArrived(t, rec, ids[0], 10*time.Second) || !waitArrived(t, rec, ids[1], 5*time.Second) {
		t.Fatal("two read-only tasks did not run at the same time")
	}
	rec.release(ids[0])
	rec.release(ids[1])
	finishRun(t, ran)
}

func TestWriterSerialisesBehindWriter(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	prd, ids := makeRoPRD(t, mgr, repo, []roTaskSpec{{"a", false}, {"b", false}})
	rec := newRoSpawnGate(ids, ids[0], ids[1])

	ran, cancel := runAsync(mgr, prd.ID, rec.spawn)
	defer cancel()
	// Whichever writer spawns first holds the tree; the other must wait.
	var first, second string
	select {
	case <-rec.arrived[ids[0]]:
		first, second = ids[0], ids[1]
	case <-rec.arrived[ids[1]]:
		first, second = ids[1], ids[0]
	case <-time.After(10 * time.Second):
		t.Fatal("no writer spawned")
	}
	if waitArrived(t, rec, second, time.Second) {
		t.Fatal("second writer spawned while the first still held the tree")
	}
	rec.release(first)
	if !waitArrived(t, rec, second, 10*time.Second) {
		t.Fatal("second writer never spawned after the first finished")
	}
	rec.release(second)
	finishRun(t, ran)
}

func TestReadOnlyTaskDoesNotBlockWriter(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	prd, ids := makeRoPRD(t, mgr, repo, []roTaskSpec{{"reader", true}, {"writer", false}})
	rec := newRoSpawnGate(ids, ids[0]) // hold only the read-only task

	ran, cancel := runAsync(mgr, prd.ID, rec.spawn)
	defer cancel()
	if !waitArrived(t, rec, ids[0], 10*time.Second) {
		t.Fatal("read-only task did not spawn")
	}
	if !waitArrived(t, rec, ids[1], 5*time.Second) {
		t.Fatal("writer was blocked by a running read-only task")
	}
	rec.release(ids[0])
	finishRun(t, ran)
}

func TestReadOnlySettingOffKeepsOldBehaviour(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	off := false
	mgr := newRoManager(t, &off)
	prd, ids := makeRoPRD(t, mgr, repo, []roTaskSpec{{"a", true}, {"b", true}})
	rec := newRoSpawnGate(ids, ids[0], ids[1])

	ran, cancel := runAsync(mgr, prd.ID, rec.spawn)
	defer cancel()
	var first, second string
	select {
	case <-rec.arrived[ids[0]]:
		first, second = ids[0], ids[1]
	case <-rec.arrived[ids[1]]:
		first, second = ids[1], ids[0]
	case <-time.After(10 * time.Second):
		t.Fatal("no task spawned")
	}
	if waitArrived(t, rec, second, time.Second) {
		t.Fatal("with the setting off, read-only tasks must serialise like writers")
	}
	rec.release(first)
	if !waitArrived(t, rec, second, 10*time.Second) {
		t.Fatal("second task never spawned")
	}
	rec.release(second)
	finishRun(t, ran)
}

func TestReadOnlyTaskThatDirtiesTreeFails(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	prd, ids := makeRoPRD(t, mgr, repo, []roTaskSpec{{"reader", true}})

	var sawCommitStep bool
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		sawCommitStep = req.Git || len(req.WriteScope) > 0
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	// spawn only starts the session; the worker runs until verify returns. The
	// stray file is written there, after spawn, to model a worker that dirties
	// the tree while it runs.
	verify := func(ctx context.Context, p *PRD, tk *Task) (VerificationResult, error) {
		if err := os.WriteFile(filepath.Join(repo, "stray.txt"), []byte("x"), 0o644); err != nil {
			t.Errorf("write: %v", err)
		}
		return tlVerify(ctx, p, tk)
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- mgr.Run(runCtx, prd.ID, spawn, verify) }()
	finishRun(t, ran)

	if sawCommitStep {
		t.Error("a read-only task must be spawned with no commit step")
	}
	cur, _ := mgr.Store().GetTask(ids[0])
	if cur.Status != TaskFailed {
		t.Fatalf("status = %q; want failed", cur.Status)
	}
	if !strings.Contains(cur.Error, "read-only") || !strings.Contains(cur.Error, "stray.txt") {
		t.Errorf("error %q should name the read-only violation and the file", cur.Error)
	}
}
