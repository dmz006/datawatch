package autonomous

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// VerifierPolicyOverride is a per-Automaton or per-task override of the
// global verifier policy. Only the mode and the backend list can be
// overridden. An empty field inherits from the next level up.
type VerifierPolicyOverride struct {
	Mode     string   `json:"mode,omitempty"`
	Backends []string `json:"backends,omitempty"`
}

// ResolveVerifierPolicy returns the verifier policy in force for one task.
// Precedence is task, then Automaton, then global; the mode and the backend
// list each resolve independently, so a task that sets only a mode still
// inherits the Automaton's backend list. Every other field comes from the
// global policy. Either of prd and task may be nil.
func ResolveVerifierPolicy(prd *PRD, task *Task, global config.VerifierPolicyConfig) config.VerifierPolicyConfig {
	out := global
	var levels []*VerifierPolicyOverride
	if task != nil {
		levels = append(levels, task.VerifierPolicy)
	}
	if prd != nil {
		levels = append(levels, prd.VerifierPolicy)
	}
	for _, o := range levels {
		if o != nil && strings.TrimSpace(o.Mode) != "" {
			out.Mode = o.Mode
			break
		}
	}
	for _, o := range levels {
		if o != nil && len(config.NormalizeVerifierPolicyBackends(o.Backends)) > 0 {
			out.Backends = append([]string(nil), o.Backends...)
			break
		}
	}
	return out
}

// VerifierLoad is the view of backend capacity the selector reads. The
// capacity ledger satisfies it in the daemon; tests pass a fake. None of the
// methods may reserve a slot: the selector only probes.
type VerifierLoad interface {
	// TryAdmit reports whether a new verifier session on the backend would
	// be admitted right now.
	TryAdmit(backend string) bool
	// Free returns the backend's free slots; a negative value means
	// unlimited.
	Free(backend string) int
	// RelativeLoad returns in-use slots over capacity, 0 when idle, 1 when
	// full. Backends without a known capacity report 0.
	RelativeLoad(backend string) float64
}

// VerifierSelectFlags carries the per-call facts the selector needs besides
// the policy and the capacity view.
type VerifierSelectFlags struct {
	// Retry marks a verification that retries an earlier failed one.
	Retry bool
	// FinalGate marks the final verification gate of a PRD.
	FinalGate bool
	// ClaudeHold is the set of backends currently held (usage back-off). A
	// held Claude backend holds every Claude backend, since they share one
	// usage limit.
	ClaudeHold map[string]bool
	// ClaudeSessionsInWindow is the count of Claude verifier sessions started
	// in the current cap window.
	ClaudeSessionsInWindow int
}

// SelectVerifierBackend picks the verifier backend for one verification. The
// policy is the resolved one (see ResolveVerifierPolicy); candidates is the
// fallback backend list used when the policy names none (the
// verification_backends pool, or the single configured backend).
//
// Rules, in order:
//  1. The eligible list is the policy list, else candidates, filtered by the
//     gpu-only / claude-only presets. Held backends are removed, and so are
//     all Claude backends when a Claude backend is held or the Claude session
//     cap for the window is reached.
//  2. A retry (force_stronger_on_retry) or the final gate
//     (final_gate_stronger) selects the stronger verifier, unless it is held,
//     capped or barred by the preset, in which case normal selection applies.
//  3. fixed takes the first eligible entry; prefer-free the first that would
//     admit a session, else the first; balance the lowest relative load
//     among admitting backends (all, if none admit), ties to list order.
//
// The model is non-empty only for the stronger verifier; otherwise the
// backend's registry default applies. An empty backend means nothing is
// eligible. The reason is a single line for the decision log.
func SelectVerifierBackend(policy config.VerifierPolicyConfig, candidates []string, load VerifierLoad, flags VerifierSelectFlags) (backend, model, reason string) {
	mode, err := config.NormalizeVerifierPolicyMode(policy.Mode)
	if err != nil {
		mode = config.VerifierPolicyFixed
	}
	claudeBlocked := claudeBlockedReason(policy, flags)
	allowed := func(b string) bool {
		if flags.ClaudeHold[b] {
			return false
		}
		return claudeBlocked == "" || !config.IsClaudeVerifierBackend(b)
	}

	if (flags.Retry && policy.ForceStrongerOnRetry) || (flags.FinalGate && policy.FinalGateStronger) {
		sb := config.NormalizeVerifierPolicyStrongerBackend(policy.StrongerBackend)
		presetOK := (mode != config.VerifierPolicyGPUOnly || !config.IsClaudeVerifierBackend(sb)) &&
			(mode != config.VerifierPolicyClaudeOnly || config.IsClaudeVerifierBackend(sb))
		if presetOK && allowed(sb) {
			why := "retry"
			if !flags.Retry || !policy.ForceStrongerOnRetry {
				why = "final gate"
			}
			return sb, config.NormalizeVerifierPolicyStrongerModel(policy.StrongerModel),
				fmt.Sprintf("stronger verifier for %s", why)
		}
	}

	var eligible []string
	for _, b := range policy.EffectiveBackends(candidates, "") {
		if allowed(b) {
			eligible = append(eligible, b)
		}
	}
	if len(eligible) == 0 {
		msg := "no eligible verifier backend"
		if claudeBlocked != "" {
			msg += " (" + claudeBlocked + ")"
		}
		return "", "", msg
	}

	switch mode {
	case config.VerifierPolicyPreferFree:
		for _, b := range eligible {
			if admits(load, b) {
				return b, "", fmt.Sprintf("prefer-free: %s has a free slot", b)
			}
		}
		return eligible[0], "", fmt.Sprintf("prefer-free: all full, waiting on %s", eligible[0])
	case config.VerifierPolicyBalance:
		pool := make([]string, 0, len(eligible))
		for _, b := range eligible {
			if admits(load, b) {
				pool = append(pool, b)
			}
		}
		if len(pool) == 0 {
			pool = eligible
		}
		best, bestLoad := pool[0], relativeLoad(load, pool[0])
		for _, b := range pool[1:] {
			if l := relativeLoad(load, b); l < bestLoad {
				best, bestLoad = b, l
			}
		}
		return best, "", fmt.Sprintf("balance: %s has the lowest load (%.2f)", best, bestLoad)
	default: // fixed, gpu-only, claude-only
		if mode == config.VerifierPolicyFixed {
			return eligible[0], "", fmt.Sprintf("fixed: %s", eligible[0])
		}
		return eligible[0], "", fmt.Sprintf("%s: %s", mode, eligible[0])
	}
}

// claudeBlockedReason says why Claude backends are out of the eligible list,
// or returns "" when they are not.
func claudeBlockedReason(policy config.VerifierPolicyConfig, flags VerifierSelectFlags) string {
	for b, held := range flags.ClaudeHold {
		if held && config.IsClaudeVerifierBackend(b) {
			return "claude backend held"
		}
	}
	if policy.ClaudeSessionCap > 0 && flags.ClaudeSessionsInWindow >= policy.ClaudeSessionCap {
		return fmt.Sprintf("claude session cap %d reached", policy.ClaudeSessionCap)
	}
	return ""
}

func admits(load VerifierLoad, backend string) bool {
	return load == nil || (load.TryAdmit(backend) && load.Free(backend) != 0)
}

func relativeLoad(load VerifierLoad, backend string) float64 {
	if load == nil {
		return 0
	}
	return load.RelativeLoad(backend)
}
