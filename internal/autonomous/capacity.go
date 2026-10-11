package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// CapacityKeysFn maps a task's backend and model to the capacity pools it
// needs plus its primary compute node (for model affinity).
type CapacityKeysFn func(backend, model string) (pools []string, node string)

var errCancelledWhileWaiting = errors.New("task cancelled while waiting for capacity")

// ErrWorkerStalled marks a verify() error as a worker-stall detection (SSE
// stall, or the log-mtime staleWorkerCheck) rather than a hard fault. The
// executor's retry budget applies to it exactly like a normal verification
// failure — the previous behavior (a bare error return) exited executeOne
// immediately on the FIRST stall, silently ignoring auto_fix_retries and
// contradicting the "retrying task" wording in the error message itself.
var ErrWorkerStalled = errors.New("worker stalled")

const defaultCapacityWait = 4 * time.Hour

// capRetryPause is the pause between session-cap retries (var for tests).
var capRetryPause = 15 * time.Second

// SetCapacity wires the admission ledger and pool resolver.
func (m *Manager) SetCapacity(l *capacity.Ledger, keys CapacityKeysFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capacity = l
	m.capacityKeys = keys
}

// Capacity returns the ledger (nil when not wired).
func (m *Manager) Capacity() *capacity.Ledger {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capacity
}

// CapacityKeys returns the pool-resolution function wired via SetCapacity
// (nil when not wired). Exposed so callers outside the executor loop (the
// verifier's own /api/ask call, wired in cmd/datawatch/main.go) can acquire
// the same node:/llm: pools a task on that backend+model would use, even
// when the verifier's resolved backend differs from the task's own worker
// backend (BL25's "cross-backend independence" design).
func (m *Manager) CapacityKeys() CapacityKeysFn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capacityKeys
}

// CapacityWaitDuration returns the configured capacity-wait bound (mirrors
// the private capacityWait() used inside the executor loop).
func (m *Manager) CapacityWaitDuration() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.CapacityWaitTimeoutSeconds > 0 {
		return time.Duration(m.cfg.CapacityWaitTimeoutSeconds) * time.Second
	}
	return defaultCapacityWait
}

func (m *Manager) capacityOn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capacity != nil && m.capacityKeys != nil && (m.cfg.CapacityEnabled == nil || *m.cfg.CapacityEnabled)
}

func (m *Manager) capacityWait() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.CapacityWaitTimeoutSeconds > 0 {
		return time.Duration(m.cfg.CapacityWaitTimeoutSeconds) * time.Second
	}
	return defaultCapacityWait
}

func (m *Manager) taskCancelled(id string) bool {
	if cur, ok := m.store.GetTask(id); ok {
		return cur.Status == TaskCancelled
	}
	return false
}

func (m *Manager) setWaiting(t *Task, reason string) {
	t.Status = TaskWaitingCapacity
	t.WaitReason = reason
	t.UpdatedAt = time.Now()
	_ = m.store.SaveTask(t)
	m.EmitPRDUpdate(t.PRDID)
}

func (m *Manager) clearWaiting(t *Task) {
	if t.Status == TaskWaitingCapacity {
		t.Status = TaskInProgress
	}
	t.WaitReason = ""
	_ = m.store.SaveTask(t)
	m.EmitPRDUpdate(t.PRDID)
}

// admit blocks until the task holds a capacity lease. Waiting is visible as
// TaskWaitingCapacity and never counts as a retry or failure.
func (m *Manager) admit(ctx context.Context, prd *PRD, t *Task, backend, model string) error {
	if !m.capacityOn() {
		return nil
	}
	m.mu.Lock()
	led, keysFn := m.capacity, m.capacityKeys
	m.mu.Unlock()
	pools, node := keysFn(backend, model)
	if len(pools) == 0 {
		return nil
	}
	waited := false
	err := led.Acquire(ctx, capacity.Request{
		Holder: t.ID, PRDID: prd.ID, Priority: prd.Priority, Pools: pools, Node: node, Model: model,
	}, m.capacityWait(), func() bool { return m.taskCancelled(t.ID) }, func(reason string) {
		waited = true
		m.setWaiting(t, reason)
	})
	switch {
	case err == nil:
		if waited {
			m.clearWaiting(t)
		}
		return nil
	case errors.Is(err, context.Canceled) && m.taskCancelled(t.ID):
		return errCancelledWhileWaiting
	case errors.Is(err, capacity.ErrWaitTimeout):
		return fmt.Errorf("capacity: %w", err)
	default:
		return err
	}
}

// releaseCapacity frees the task's lease (safe when none is held).
func (m *Manager) releaseCapacity(taskID string) {
	m.mu.Lock()
	led := m.capacity
	m.mu.Unlock()
	if led != nil {
		led.Release(taskID)
	}
}

// IsCapacityError reports whether a spawn error means "no free session slot".
func IsCapacityError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "max sessions")
}

// IsWorkspaceLockError (BL407 Phase 4) reports whether a spawn error is
// agents.Manager.checkWorkspaceLock's "another active agent already
// owns this (project_profile, branch) tuple" refusal. Every
// cluster-dispatched task of one PRD now requests the SAME
// Phase-4-assigned branch (one PR per PRD, Decision 8) — the
// executor's own concurrent task pool (BL370) can easily try to
// dispatch a second one before the first's agent reaches a terminal
// state, which the workspace lock (designed for two *unrelated*
// agents colliding by coincidence) would otherwise reject outright.
// Treating it as retryable, exactly like a capacity-full wait, is the
// correct fix: two containers racing pushes to the SAME branch is a
// real hazard this lock exists to prevent, so the right behavior is
// to wait for the sibling task's agent to finish, not to bypass the
// lock.
func IsWorkspaceLockError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "workspace lock:")
}

// capacityRetrySpawn wraps a SpawnFn so a full session cap, or (BL407
// Phase 4) a workspace-lock collision between two of the same PRD's
// own cluster-dispatched tasks, is a wait rather than a hard failure:
// it retries with a pause, showing the task as waiting_capacity.
func (m *Manager) capacityRetrySpawn(t *Task, spawn SpawnFn) SpawnFn {
	return func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		start := time.Now()
		waited := false
		for {
			sr, err := spawn(ctx, req)
			if !IsCapacityError(err) && !IsWorkspaceLockError(err) {
				if waited {
					m.clearWaiting(t)
				}
				return sr, err
			}
			if time.Since(start) >= m.capacityWait() {
				if IsWorkspaceLockError(err) {
					return sr, fmt.Errorf("workspace lock: %w: %v", capacity.ErrWaitTimeout, err)
				}
				return sr, fmt.Errorf("capacity: %w: %v", capacity.ErrWaitTimeout, err)
			}
			waited = true
			m.setWaiting(t, err.Error())
			select {
			case <-ctx.Done():
				return sr, ctx.Err()
			case <-time.After(capRetryPause):
			}
			if m.taskCancelled(t.ID) {
				return sr, errCancelledWhileWaiting
			}
		}
	}
}
