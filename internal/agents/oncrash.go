// BL106 — runtime enforcement of ProjectProfile.OnCrash.
//
// S8.7 shipped the field + validation. BL106 wires the response: when
// a worker transitions to Failed without RecordResult the Manager
// consults profile.OnCrash and applies one of:
//
//   "" / "fail_parent" — default; do nothing (caller surfaces failure)
//   "respawn_once"     — single retry with the same SpawnRequest;
//                        further failures fall back to fail_parent
//   "respawn_with_backoff" — exponential delay (1m, 2m, 4m, …, capped
//                        at 30m) until manual operator intervention
//
// Per-(project, branch, parentAgentID) retry book-keeping lives on
// Manager.crashRetries so a single profile racing in lockstep can't
// short-circuit the limit on respawn_once.

package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/profile"
)

const (
	OnCrashFailParent          = "fail_parent"
	OnCrashRespawnOnce         = "respawn_once"
	OnCrashRespawnWithBackoff  = "respawn_with_backoff"

	respawnBackoffStart = time.Minute
	respawnBackoffMax   = 30 * time.Minute
)

// HandleCrash is called when an agent transitions to Failed without
// RecordResult. It consults profile.OnCrash and either issues a
// replacement spawn (respawn_once / respawn_with_backoff) or returns
// false to let the caller surface the failure as before
// (fail_parent / unknown / nil profile).
//
// Returns (replacementAgent, didRespawn, err). When didRespawn is
// false the caller behaves as if BL106 didn't exist; when true the
// failure is treated as recovered and the caller should *not* also
// alert the parent session.
func (m *Manager) HandleCrash(ctx context.Context, original *Agent) (*Agent, bool, error) {
	if original == nil || original.project == nil {
		return nil, false, nil
	}
	policy := original.project.OnCrash
	switch policy {
	case "", OnCrashFailParent:
		return nil, false, nil
	case OnCrashRespawnOnce:
		return m.respawnOnce(ctx, original)
	case OnCrashRespawnWithBackoff:
		return m.respawnWithBackoff(ctx, original)
	default:
		// Unknown policy — log but don't crash on top of crash; treat
		// as fail_parent so operator gets a chance to fix the profile.
		emit(m.Auditor, "crash_policy_unknown", original.ID,
			original.ProjectProfile, original.ClusterProfile,
			string(original.State),
			fmt.Sprintf("on_crash=%q falls through to fail_parent", policy),
			nil)
		return nil, false, nil
	}
}

func crashKey(p *profile.ProjectProfile, branch, parentID string) string {
	return p.Name + "|" + branch + "|" + parentID
}

func (m *Manager) respawnOnce(ctx context.Context, original *Agent) (*Agent, bool, error) {
	key := crashKey(original.project, original.Branch, original.ParentAgentID)
	m.mu.Lock()
	st, ok := m.crashRetries[key]
	if !ok {
		st = &crashState{}
		m.crashRetries[key] = st
	}
	if st.count >= 1 {
		// Already used the single retry — fall back to fail_parent.
		m.mu.Unlock()
		emit(m.Auditor, "crash_respawn_exhausted", original.ID,
			original.ProjectProfile, original.ClusterProfile,
			string(original.State),
			"respawn_once budget exhausted; falling back to fail_parent",
			nil)
		return nil, false, nil
	}
	st.count++
	st.lastAt = time.Now().UTC()
	m.mu.Unlock()

	emit(m.Auditor, "crash_respawn", original.ID,
		original.ProjectProfile, original.ClusterProfile,
		string(original.State),
		"on_crash=respawn_once retry #1",
		nil)

	req := SpawnRequest{
		ProjectProfile: original.ProjectProfile,
		ClusterProfile: original.ClusterProfile,
		Task:           original.Task,
		Branch:         original.Branch,
		ParentAgentID:  original.ParentAgentID,
	}
	a, err := m.Spawn(ctx, req)
	return a, true, err
}

func (m *Manager) respawnWithBackoff(ctx context.Context, original *Agent) (*Agent, bool, error) {
	key := crashKey(original.project, original.Branch, original.ParentAgentID)
	m.mu.Lock()
	st, ok := m.crashRetries[key]
	if !ok {
		st = &crashState{}
		m.crashRetries[key] = st
	}
	delay := respawnBackoffFor(st.count)
	since := time.Since(st.lastAt)
	st.count++
	st.lastAt = time.Now().UTC()
	m.mu.Unlock()

	// First crash for this key fires immediately; subsequent crashes
	// only respawn if enough time has elapsed since the last attempt.
	wait := time.Duration(0)
	if st.count > 1 && since < delay {
		wait = delay - since
	}

	emit(m.Auditor, "crash_respawn_backoff", original.ID,
		original.ProjectProfile, original.ClusterProfile,
		string(original.State),
		fmt.Sprintf("on_crash=respawn_with_backoff attempt %d wait=%s",
			st.count, wait.Round(time.Second)),
		nil)

	if wait <= 0 {
		req := SpawnRequest{
			ProjectProfile: original.ProjectProfile,
			ClusterProfile: original.ClusterProfile,
			Task:           original.Task,
			Branch:         original.Branch,
			ParentAgentID:  original.ParentAgentID,
		}
		a, err := m.Spawn(ctx, req)
		return a, true, err
	}

	// Respawn is acknowledged but deferred — fire it from a
	// goroutine so the caller doesn't block waiting on the backoff.
	// Treat this as a successful "didRespawn" because the parent
	// shouldn't also surface the failure synchronously.
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		req := SpawnRequest{
			ProjectProfile: original.ProjectProfile,
			ClusterProfile: original.ClusterProfile,
			Task:           original.Task,
			Branch:         original.Branch,
			ParentAgentID:  original.ParentAgentID,
		}
		_, _ = m.Spawn(ctx, req)
	}()
	return nil, true, nil
}

// respawnBackoffFor returns the delay used after the n-th retry.
// 0 → 1m, 1 → 2m, 2 → 4m, …, capped at 30m. Exported via test
// to keep the curve visible.
func respawnBackoffFor(n int) time.Duration {
	if n < 0 {
		n = 0
	}
	d := respawnBackoffStart << n
	if d > respawnBackoffMax || d <= 0 {
		return respawnBackoffMax
	}
	return d
}

// ResetCrashRetries clears the per-key retry book-keeping for the
// supplied (project, branch, parentID) tuple. Useful when an
// operator manually intervenes ("you can stop respawning now") and
// for tests.
func (m *Manager) ResetCrashRetries(projectName, branch, parentID string) {
	key := projectName + "|" + branch + "|" + parentID
	m.mu.Lock()
	delete(m.crashRetries, key)
	m.mu.Unlock()
}
