package autonomous

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// routedPlan is the planner resolution both decompose twins share (see
// routedPlanForDecompose in manager.go).
type routedPlan struct {
	// Backend and Model are the planBackend / planGlobalModel the twin feeds
	// into its backend and resolvePlannedModel steps (below the PRD's own
	// decomposition fields).
	Backend string
	Model   string
	// Decision is the route decision; nil when routing was not consulted
	// (routing off, or an explicit PRD / Automaton planner setting won).
	Decision *RouteDecision
	// UseLadder is true when the decision is enabled, not shadow and not a
	// fallback: the twin runs the planning ladder instead of one attempt.
	UseLadder bool
	Planners  []config.PlannerRef
	Ladder    config.PlanningLadder
}

// plainRecord is the record stored when the decision was computed but the
// planner was left unchanged (shadow mode, or a fallback route). nil when the
// ladder runs or routing was not consulted.
func (rp routedPlan) plainRecord(now time.Time) *PlanningRecord {
	if rp.Decision == nil || rp.UseLadder {
		return nil
	}
	return NewPlanningRecordFromDecision(*rp.Decision, rp.Backend, rp.Model, now)
}

// decomposeViaLadder runs the planning ladder with the existing decompose
// call as the PlannerFn, then validates, stores and finalizes the result like
// the single-attempt loop. Each planner call keeps the single-attempt
// behaviour: admission (decomposeAdmitted), cancel, and the bounded infra
// retry. It returns the finalized PRD and the stored stories.
func (m *Manager) decomposeViaLadder(prd *PRD, prdID string, rp routedPlan, prompt string, effort Effort) (*PRD, []Story, error) {
	for {
		ctx, cancel := context.WithCancel(context.Background())
		var (
			fatal     error  // cancel or session-stop: end the whole decompose, PRD already holds its status
			giveUp    error  // an infra give-up on the last planner call
			lastRaw   string // raw output of the last successful planner call
			lastTitle string
		)
		plan := func(ctx context.Context, backend, model, p string) (string, error) {
			for {
				if err := ctx.Err(); err != nil {
					return "", err
				}
				// The ladder judges coverage against the bare spec; the first
				// move sends the full decompose prompt, a touch-up or replan
				// keeps that prompt's format and rules ahead of its own text.
				attemptPrompt := prompt
				if p != prd.Spec {
					attemptPrompt = prompt + "\n\n---\n" + p
				}
				if prd.LastDecomposeFailure != nil {
					attemptPrompt += fmt.Sprintf("\n\nYour previous attempt failed: %s\nFix this in your next response.", prd.LastDecomposeFailure.Detail)
				}
				r, derr := m.decomposeAdmitted(prd, backend, model, DecomposeRequest{Spec: attemptPrompt, Backend: backend, Effort: effort, Model: model, ProjectDir: prd.ProjectDir, ScopeBlock: ScopeBlock(prd), PRDID: prd.ID, TimeoutSeconds: m.cfg.PlanningTimeoutSeconds})
				if derr != nil {
					if errors.Is(derr, errDecomposeCancelled) {
						fatal = derr
						cancel()
						return "", derr
					}
					if isOneShotNoOutput(derr) {
						if m.noteDecomposeSessionFailure(prd, backend, model, derr) {
							fatal = derr
							cancel()
							return "", derr
						}
						continue
					}
					if m.retryDecomposeFailure(prd, "infra", "decompose-llm", derr.Error()) {
						continue
					}
					// Budget spent for this planner: let the ladder escalate.
					giveUp = derr
					return "", derr
				}
				giveUp = nil
				lastRaw = r
				if t, _, perr := ParsePlanning(r); perr == nil {
					lastTitle = t
				}
				return r, nil
			}
		}
		lr := RunPlanningLadder(ctx, rp.Ladder, rp.Planners, prd.Spec, plan, time.Now)
		cancel()
		if fatal != nil {
			return nil, nil, fatal
		}
		rec := NewPlanningRecordFromLadder(*rp.Decision, lr, time.Now())
		if len(lr.FinalStories) == 0 {
			var cause error
			source := "decompose-parse"
			if giveUp != nil {
				cause, source = giveUp, "decompose-llm"
			} else if n := len(lr.Attempts); n > 0 && lr.Attempts[n-1].Error != "" {
				cause = errors.New(lr.Attempts[n-1].Error)
			} else {
				cause = errors.New("planning ladder produced no plan")
			}
			m.failDecomposeAttempt(prd, source, cause)
			m.storePlanningRecord(prd, rec)
			if giveUp != nil {
				return nil, nil, fmt.Errorf("LLM plan: %w", cause)
			}
			return nil, nil, cause
		}
		s := lr.FinalStories
		switch outcome, oerr := m.validateAndStorePlan(prd, prdID, s); outcome {
		case decomposeAttemptRetry:
			continue
		case decomposeAttemptTerminal:
			m.storePlanningRecord(prd, rec)
			return nil, nil, oerr
		default:
			backend := lr.Planner.Backend
			updated, ferr := m.finalizeDecomposedPlan(prd, prdID, backend, prompt, lastRaw, lastTitle, s, rec)
			if ferr != nil {
				return nil, nil, ferr
			}
			return updated, s, nil
		}
	}
}
