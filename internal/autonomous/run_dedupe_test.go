// Regression tests: the "is an executor live for this PRD, and if not register
// one" step in API.Run / boot-resume must be atomic, so concurrent starts for
// one PRD launch exactly one executor and never cancel a live one (B98).

package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"
)

// spawnProbe counts executor spawns per PRD and blocks each one until released.
type spawnProbe struct {
	mu      sync.Mutex
	starts  map[string]int
	entered chan string
	release chan struct{}
}

func newSpawnProbe() *spawnProbe {
	return &spawnProbe{
		starts:  map[string]int{},
		entered: make(chan string, 64),
		release: make(chan struct{}),
	}
}

func (p *spawnProbe) spawn(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
	p.mu.Lock()
	p.starts[req.PRDID]++
	p.mu.Unlock()
	p.entered <- req.PRDID
	select {
	case <-p.release:
	case <-ctx.Done():
		return SpawnResult{}, ctx.Err()
	}
	return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
}

func (p *spawnProbe) count(id string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.starts[id]
}

func (p *spawnProbe) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-p.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("executor never reached spawn")
	}
}

func prdInApprovedState(t *testing.T, m *Manager) *PRD {
	t.Helper()
	prd := prdInRunningState(t, m)
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD approved: %v", err)
	}
	return prd
}

// (a) boot-resume of a PRDRunning PRD racing an explicit Run starts one executor.
func TestRunDedupe_BootResumeVsRun(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	prd := prdInRunningState(t, m)
	probe := newSpawnProbe()

	ready := make(chan struct{})
	api.SetServerReadyCh(ready)
	api.SetExecutors(probe.spawn, verify) // resume scan parked on ready

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	go func() { defer wg.Done(); <-start; close(ready) }()
	go func() {
		defer wg.Done()
		<-start
		if err := api.Run(prd.ID); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()
	close(start)
	wg.Wait()

	probe.waitEntered(t)
	// Drain: cancels the blocked executor and waits out the resume scan too.
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if n := probe.count(prd.ID); n != 1 {
		t.Fatalf("executor spawned %d times for PRD, want 1", n)
	}
}

// (b) two concurrent Run calls for one approved PRD start one executor.
func TestRunDedupe_ConcurrentRun(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	prd := prdInApprovedState(t, m)
	probe := newSpawnProbe()
	api.spawnFn, api.verify = probe.spawn, verify

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := api.Run(prd.ID); err != nil {
				t.Errorf("Run: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	probe.waitEntered(t)
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if n := probe.count(prd.ID); n != 1 {
		t.Fatalf("executor spawned %d times for PRD, want 1", n)
	}
}

// (c) once the executor has ended, Run can start the PRD again.
func TestRunDedupe_RestartAfterExecutorEnds(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	prd := prdInApprovedState(t, m)
	probe := newSpawnProbe()
	api.spawnFn, api.verify = probe.spawn, verify
	close(probe.release) // spawns complete immediately

	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	probe.waitEntered(t)
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}

	// Rewind the PRD so it is runnable again.
	fresh, _ := m.Store().GetPRD(prd.ID)
	fresh.Status = PRDApproved
	for si := range fresh.Story {
		for ti := range fresh.Story[si].Tasks {
			fresh.Story[si].Tasks[ti].Status = ""
			fresh.Story[si].Tasks[ti].SessionID = ""
		}
	}
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD rewind: %v", err)
	}

	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	probe.waitEntered(t)
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if n := probe.count(prd.ID); n != 2 {
		t.Fatalf("executor spawned %d times across two runs, want 2", n)
	}
}
