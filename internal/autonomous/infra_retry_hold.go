// Infrastructure retry exhaustion: what happens when a task has used up its
// infrastructure retries.
//
// The policy is config.InfraRetryConfig.ExhaustedAction. "count" books the
// failure as one normal failed attempt, so the task takes the same path any
// failed attempt takes.
package autonomous

import (
	"context"

	"github.com/dmz006/datawatch/internal/config"
)

// isInfraText reports whether the text of a failed attempt names an
// infrastructure failure.
func isInfraText(text string) bool {
	_, ok := ClassifyInfraText(text)
	return ok
}

// infraRetriesUsedUp reports whether err is a transient infrastructure failure
// that the policy retries and the task has already used all its tries on: the
// feature is on, the class is listed, and the task's consecutive retry count
// has reached max_tries.
func (m *Manager) infraRetriesUsedUp(t *Task, err error) bool {
	cfg := m.infraRetryConfig()
	if !cfg.EffectiveEnabled() {
		return false
	}
	class, ok := ClassifyInfraFailure(err)
	if !ok || !cfg.HasCategory(class) {
		return false
	}
	var used int
	m.store.WithLock(func() { used = t.InfraRetryCount })
	return used >= cfg.EffectiveMaxTries()
}

// infraRetriesExhausted applies the exhausted action to a task whose
// infrastructure retries are used up. With the "count" action it records one
// normal failed attempt, categorised by the failure class, and returns
// handled=false so the caller's existing failure path continues. Any other
// action leaves the failure to that path as well.
func (m *Manager) infraRetriesExhausted(ctx context.Context, prd *PRD, t *Task, run attemptRun, err error) (handled bool, rerr error) {
	cfg := m.infraRetryConfig()
	if cfg.EffectiveExhaustedAction() != config.ExhaustedActionCount {
		return false, nil
	}
	class, _ := ClassifyInfraFailure(err)
	m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{
		Outcome:  attemptOutcomeForError(err),
		Category: class,
		Summary:  boundInfraReason(err.Error()),
	})
	return false, nil
}
