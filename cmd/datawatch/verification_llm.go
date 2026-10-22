package main

import autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"

// verifierSelectInput is everything selectVerifierLLM needs to decide
// which LLM the autonomous verifier runs on.
type verifierSelectInput struct {
	// Resolved is autonomouspkg.ResolveVerificationLLM(prd, task, automation, cfg).
	Resolved autonomouspkg.VerificationLLM
	// ExplicitEffort is the first effort set at task → story → Automaton →
	// Automation level, without the global verification_effort fallback (resolve
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
	// task/story/Automaton/Automation override, never from the global
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
//   - An explicit task/story/Automaton/Automation override (Resolved.Source !=
//     global) skips the verification_backends capacity pool entirely —
//     an explicit per-unit choice beats load-balancing — and is passed
//     through resolveVerifierBackendModel's normal safety logic
//     (ask-compatible check, session routing, ollama downgrade without a
//     project_dir) as the configured backend/model. When it ends up
//     session-routed, the override's own model is used.
//   - Otherwise (global) the behavior is unchanged: try each pool
//     candidate via tryAdmit (nil = no pool), then fall back to the
//     global verification_backend/model, then the Automaton's own
//     backend. A pi-family session kind (pi, pi-prompt) on this path
//     takes the configured verification_model as its session model; an
//     empty one stays empty — the caller's pi-family pinned-model guard
//     (v9.10.19) then applies the global pin from the verification
//     chain (autonomous.planning_model) or fails loudly, naming the
//     configuration keys.
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
	// The effective backend is verification_backend, else the PRD's own,
	// else ollama — the same order resolveVerifierBackendModel uses.
	effBackend := in.GlobalBackend
	usesPRDBackend := effBackend == "" && in.PRDBackend != ""
	if effBackend == "" {
		effBackend = in.PRDBackend
	}
	if effBackend == "" {
		effBackend = "ollama"
	}
	effKind, _, _ := resolveAskBackend(effBackend)
	piFamily := isPiFamilyKind(effKind)

	// A pi-family session uses exactly the configured verification_model
	// (a pi 'provider/model' string). Other session kinds never get it —
	// the ask-compatible model would be rejected by e.g. a claude-code
	// session (see resolveVerifierBackendModelWithSessionModel).
	var sessionModel string
	if piFamily {
		sessionModel = in.GlobalModel
	}
	var sel verifierSelection
	sel.Backend, sel.Kind, sel.Model, sel.UseSession = resolveVerifierBackendModelWithSessionModel(
		in.GlobalBackend, in.GlobalModel, in.PRDBackend, in.PRDModel, in.ProjectDir, sessionModel,
		resolveAskBackend, askCompatible, isSessionKind)
	// Pi family with no configured verification_model: do not let the
	// function fall back to resolveAskBackend's default model for the
	// named backend (a model that belongs to some other backend). Leave
	// the model empty so the caller's pi-family pinned-model guard
	// (v9.10.19) applies the global pin from the verification chain or
	// fails loudly. The PRD's own model still wins when the PRD's
	// backend is the one in use.
	if piFamily && sel.UseSession && in.GlobalModel == "" && (!usesPRDBackend || in.PRDModel == "") {
		sel.Model = ""
	}
	sel.Source = autonomouspkg.VerifierSourceGlobal
	return sel
}

// isPiFamilyKind reports whether kind is a pi.dev harness kind: the
// interactive "pi" or the one-shot "pi-prompt" backend.
func isPiFamilyKind(kind string) bool {
	return kind == "pi" || kind == "pi-prompt"
}

// stamp records the selection on a verification result.
func (s verifierSelection) stamp(vr *autonomouspkg.VerificationResult) {
	vr.VerifierBackend = s.Backend
	vr.VerifierModel = s.Model
	vr.VerifierEffort = s.Effort
	vr.VerifierSource = s.Source
}
