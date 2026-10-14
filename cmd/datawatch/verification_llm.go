package main

import autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"

// verifierSelectInput is everything selectVerifierLLM needs to decide
// which LLM the autonomous verifier runs on.
type verifierSelectInput struct {
	// Resolved is autonomouspkg.ResolveVerificationLLM(prd, task, cfg).
	Resolved autonomouspkg.VerificationLLM
	// ExplicitEffort is the first effort set at task → story → Automaton
	// level, without the global verification_effort fallback (resolve
	// with a cfg whose VerificationEffort is cleared).
	ExplicitEffort string
	// Pool is autonomous.verification_backends; GlobalBackend /
	// GlobalModel are verification_backend / verification_model.
	Pool                       []string
	GlobalBackend, GlobalModel string
	PRDBackend, PRDModel       string
	ProjectDir                 string
}

// verifierSelection is the decision selectVerifierLLM returns.
type verifierSelection struct {
	Backend, Kind, Model string
	UseSession           bool
	// Effort is the session effort to apply — set only for an explicit
	// task/story/Automaton override, never from the global
	// verification_effort.
	Effort string
	// Source is the VerifierSource* value recorded on the result.
	Source string
	// Release is non-nil only when a pool candidate was admitted
	// (Source == pool); the caller must call it when the call ends.
	Release func()
}

// selectVerifierLLM decides the verifier backend/kind/model, whether the
// call is session-routed, and where the choice came from.
//
//   - An explicit task/story/Automaton override (Resolved.Source !=
//     global) skips the verification_backends capacity pool entirely —
//     an explicit per-unit choice beats load-balancing — and is passed
//     through resolveVerifierBackendModel's normal safety logic
//     (ask-compatible check, session routing, ollama downgrade without a
//     project_dir) as the configured backend/model. When it ends up
//     session-routed, the override's own model is used.
//   - Otherwise (global) the behavior is unchanged: try each pool
//     candidate via tryAdmit (nil = no pool), then fall back to the
//     global verification_backend/model, then the Automaton's own
//     backend.
//
// The global verification_effort is still not consumed by the verify
// path (a pre-existing gap, deliberately left as-is here so the global
// path stays unchanged).
//
// tryAdmit, resolveAskBackend, askCompatible and isSessionKind are passed
// in so this stays independently testable.
func selectVerifierLLM(
	in verifierSelectInput,
	tryAdmit func(backend, model string) (release func(), ok bool),
	resolveAskBackend func(raw string) (kind, model string, err error),
	askCompatible func(kind string) bool,
	isSessionKind func(kind string) bool,
) verifierSelection {
	if in.Resolved.Source != "" && in.Resolved.Source != autonomouspkg.VerifierSourceGlobal {
		var sel verifierSelection
		sel.Backend, sel.Kind, sel.Model, sel.UseSession = resolveVerifierBackendModelWithSessionModel(
			in.Resolved.Backend, in.Resolved.Model, in.PRDBackend, in.PRDModel, in.ProjectDir, in.Resolved.Model,
			resolveAskBackend, askCompatible, isSessionKind)
		sel.Effort = in.ExplicitEffort
		sel.Source = in.Resolved.Source
		return sel
	}
	if len(in.Pool) > 0 && tryAdmit != nil {
		for _, c := range resolveVerifierCandidates(in.Pool, resolveAskBackend, askCompatible) {
			if r, ok := tryAdmit(c.backend, c.model); ok {
				return verifierSelection{
					Backend: c.backend, Kind: c.kind, Model: c.model,
					Source: autonomouspkg.VerifierSourcePool, Release: r,
				}
			}
		}
	}
	var sel verifierSelection
	sel.Backend, sel.Kind, sel.Model, sel.UseSession = resolveVerifierBackendModel(
		in.GlobalBackend, in.GlobalModel, in.PRDBackend, in.PRDModel, in.ProjectDir,
		resolveAskBackend, askCompatible, isSessionKind)
	sel.Source = autonomouspkg.VerifierSourceGlobal
	return sel
}

// stamp records the selection on a verification result.
func (s verifierSelection) stamp(vr *autonomouspkg.VerificationResult) {
	vr.VerifierBackend = s.Backend
	vr.VerifierModel = s.Model
	vr.VerifierEffort = s.Effort
	vr.VerifierSource = s.Source
}
