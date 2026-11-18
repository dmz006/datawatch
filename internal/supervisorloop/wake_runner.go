package supervisorloop

import "sync"

// Runner runs the supervisor pass for a wake batch with single-flight
// behavior: at most one run is active, and batches that arrive meanwhile are
// merged into exactly one queued follow-up that runs once the active run ends.
type Runner struct {
	run func(WakeBatch)

	mu      sync.Mutex
	running bool
	queued  *WakeBatch
	queuedK map[string]bool
	wg      sync.WaitGroup
}

// NewRunner builds a Runner that calls run for each batch it starts.
func NewRunner(run func(WakeBatch)) *Runner {
	return &Runner{run: run}
}

// Submit starts a run for the batch, or merges it into the queued follow-up
// when a run is already in progress. It never blocks on the run and never
// starts a second concurrent run. It reports whether a run started now.
func (r *Runner) Submit(b WakeBatch) bool {
	if len(b.Events) == 0 {
		return false
	}
	r.mu.Lock()
	if r.running {
		r.mergeLocked(b)
		r.mu.Unlock()
		return false
	}
	r.running = true
	r.wg.Add(1)
	r.mu.Unlock()
	go r.loop(b)
	return true
}

// Wait blocks until the active run and any queued follow-up have finished.
func (r *Runner) Wait() { r.wg.Wait() }

// Running reports whether a run is in progress.
func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

func (r *Runner) loop(b WakeBatch) {
	defer r.wg.Done()
	for {
		r.run(b)
		r.mu.Lock()
		if r.queued == nil {
			r.running = false
			r.mu.Unlock()
			return
		}
		b = *r.queued
		r.queued, r.queuedK = nil, nil
		r.mu.Unlock()
	}
}

// mergeLocked folds b into the single queued follow-up, dropping conditions
// already waiting in it.
func (r *Runner) mergeLocked(b WakeBatch) {
	if r.queued == nil {
		r.queued = &WakeBatch{}
		r.queuedK = map[string]bool{}
	}
	for _, ev := range b.Events {
		k := ev.Key()
		if r.queuedK[k] {
			continue
		}
		r.queuedK[k] = true
		r.queued.Events = append(r.queued.Events, ev)
	}
}
