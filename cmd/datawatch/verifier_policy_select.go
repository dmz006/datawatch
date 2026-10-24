package main

import (
	"log"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// verifierClaudeWindow counts the Claude verifier sessions the policy has
// started, for the per-window session cap.
var verifierClaudeWindow = autonomouspkg.NewVerifierPolicyWindow(nil)

// verifierBackendLoad reports a backend's relative load (0 idle, 1 full) from
// the capacity ledger. Set by wireCapacity; nil until then, and the selector
// then reads every backend as idle.
var verifierBackendLoad func(backend, model string) float64

// verifierPolicyInput is everything pickVerifierByPolicy needs.
type verifierPolicyInput struct {
	// Policy is the resolved policy (autonomouspkg.ResolveVerifierPolicy).
	Policy config.VerifierPolicyConfig
	// Candidates is the list used when the policy names no backends.
	Candidates []string
	// Flags carries retry, final gate, holds and the Claude window count.
	Flags autonomouspkg.VerifierSelectFlags
	// PRDBackend, PRDModel and ProjectDir feed the same routing safety logic
	// the global path uses.
	PRDBackend, PRDModel, ProjectDir string
}

// verifierPolicyApplies reports whether the policy decides the backend. A
// fixed policy with no backend list, no Claude cap and no stronger-verifier
// trigger for this call leaves today's selection untouched.
func verifierPolicyApplies(p config.VerifierPolicyConfig, f autonomouspkg.VerifierSelectFlags) bool {
	mode, err := config.NormalizeVerifierPolicyMode(p.Mode)
	if err != nil {
		return false
	}
	if mode != config.VerifierPolicyFixed {
		return true
	}
	return len(config.NormalizeVerifierPolicyBackends(p.Backends)) > 0 ||
		p.ClaudeSessionCap > 0 ||
		(f.Retry && p.ForceStrongerOnRetry) ||
		(f.FinalGate && p.FinalGateStronger)
}

// verifierPolicyBackends lists every backend the selector may consider for
// the policy, so the caller can read their hold state once.
func verifierPolicyBackends(p config.VerifierPolicyConfig, candidates []string) []string {
	out := p.EffectiveBackends(candidates, "")
	return append(out, config.NormalizeVerifierPolicyStrongerBackend(p.StrongerBackend))
}

// verifierProbeLoad is the autonomouspkg.VerifierLoad the selector reads. It
// probes with the non-blocking tryAdmit. The ledger holds one verifier lease
// per task, so at most one probe lease is kept at a time: probing the next
// backend first lets go of the previous one.
type verifierProbeLoad struct {
	tryAdmit func(backend, model string) (func(), string, bool)
	modelOf  func(backend string) string

	results map[string]bool
	loads   map[string]float64
	held    string
	release func()
	// node is the compute node the held probe lease landed on ("" for one-node).
	node string
}

func (p *verifierProbeLoad) TryAdmit(backend string) bool {
	if ok, seen := p.results[backend]; seen {
		return ok
	}
	if p.tryAdmit == nil {
		return true
	}
	model := p.modelOf(backend)
	if verifierBackendLoad != nil {
		p.loads[backend] = verifierBackendLoad(backend, model)
	}
	p.dropHeld()
	rel, node, ok := p.tryAdmit(backend, model)
	p.results[backend] = ok
	if ok {
		p.held, p.release, p.node = backend, rel, node
	}
	return ok
}

func (p *verifierProbeLoad) Free(backend string) int {
	if p.tryAdmit == nil || p.results[backend] {
		return -1
	}
	return 0
}

func (p *verifierProbeLoad) RelativeLoad(backend string) float64 { return p.loads[backend] }

func (p *verifierProbeLoad) dropHeld() {
	if p.release != nil {
		p.release()
	}
	p.held, p.release, p.node = "", nil, ""
}

// take returns the admission lease for the chosen backend and the node it
// holds, or a nil lease when the caller must admit it itself. Any other probe
// lease is let go.
func (p *verifierProbeLoad) take(chosen, model string) (func(), string) {
	if p.tryAdmit == nil {
		return nil, ""
	}
	if p.held == chosen && p.release != nil {
		rel, node := p.release, p.node
		p.held, p.release, p.node = "", nil, ""
		return rel, node
	}
	p.dropHeld()
	if rel, node, ok := p.tryAdmit(chosen, model); ok {
		return rel, node
	}
	return nil, ""
}

// verifierPolicyPick is the policy's decision, ready for the verify path.
type verifierPolicyPick struct {
	Selection verifierSelection
	// Reason is the one-line explanation kept on the task and in the audit.
	Reason string
	// Claude is true when the chosen backend is a Claude backend.
	Claude bool
	// ModelChosen is the model the selector named (the stronger verifier's),
	// empty when the backend's own default applies.
	ModelChosen string
}

// pickVerifierByPolicy runs the verifier policy and turns its choice into a
// selection. ok is false when no backend is eligible; the caller then keeps
// today's selection. A non-nil Selection.Release means the backend was
// admitted without waiting; nil means the caller admits it in the normal way
// (the stronger verifier is never probed).
func pickVerifierByPolicy(
	in verifierPolicyInput,
	tryAdmit func(backend, model string) (release func(), node string, ok bool),
	resolveAskBackend func(raw string) (kind, model string, err error),
	askCompatible func(kind string) bool,
	isSessionKind func(kind string) bool,
) (verifierPolicyPick, bool) {
	probe := &verifierProbeLoad{
		tryAdmit: tryAdmit,
		modelOf: func(backend string) string {
			_, m, _ := resolveAskBackend(backend)
			return m
		},
		results: map[string]bool{},
		loads:   map[string]float64{},
	}
	backend, model, reason := autonomouspkg.SelectVerifierBackend(in.Policy, in.Candidates, probe, in.Flags)
	if backend == "" {
		probe.dropHeld()
		return verifierPolicyPick{Reason: reason}, false
	}
	var sel verifierSelection
	sel.Backend, sel.Kind, sel.Model, sel.UseSession = resolveVerifierBackendModelWithSessionModel(
		backend, model, in.PRDBackend, in.PRDModel, in.ProjectDir, model,
		resolveAskBackend, askCompatible, isSessionKind)
	if model == "" && sel.UseSession && isPiFamilyKind(sel.Kind) {
		// Leave the model to the pi-family pinned-model guard in the caller.
		sel.Model = ""
	}
	sel.Source = autonomouspkg.VerifierSourcePolicy
	if sel.Backend == backend {
		sel.Release, sel.Node = probe.take(backend, sel.Model)
	} else {
		// The backend was downgraded by the routing safety logic; the probe
		// lease belongs to the backend that was not used.
		probe.dropHeld()
	}
	return verifierPolicyPick{
		Selection: sel, Reason: reason,
		Claude: config.IsClaudeVerifierBackend(backend), ModelChosen: model,
	}, true
}

// verifierRetryOf reports whether the task already has a failed verification
// on record: an infra failure counted, or a stored result that did not pass.
func verifierRetryOf(task *autonomouspkg.Task) bool {
	return task.VerifierInfraFailures > 0 || (task.Verification != nil && !task.Verification.OK)
}

// policyVerifierSelection applies the verifier policy to one verification.
// ok is false when the policy leaves the choice to today's selection: an
// explicit per-unit verification LLM (or an infra escalation, which sets the
// source), a fixed policy with nothing to apply, or no eligible backend.
func policyVerifierSelection(
	mgr *autonomouspkg.Manager,
	prd *autonomouspkg.PRD, task *autonomouspkg.Task,
	resolved autonomouspkg.VerificationLLM, liveCfg autonomouspkg.Config,
	projectDir string,
	tryAdmit func(backend, model string) (func(), string, bool),
	resolveAskBackend func(raw string) (kind, model string, err error),
	askCompatible func(kind string) bool,
	isSessionKind func(kind string) bool,
) (verifierSelection, bool) {
	if resolved.Source != "" && resolved.Source != autonomouspkg.VerifierSourceGlobal {
		return verifierSelection{}, false
	}
	policy := autonomouspkg.ResolveVerifierPolicy(prd, task, effectiveVerifierPolicy(liveCfg))
	flags := autonomouspkg.VerifierSelectFlags{
		Retry:     verifierRetryOf(task),
		FinalGate: task.Kind == autonomouspkg.TaskKindFinalGate,
	}
	if !verifierPolicyApplies(policy, flags) {
		return verifierSelection{}, false
	}
	pool, global, _ := effectiveVerifierSelection(liveCfg)
	candidates := pool
	if len(candidates) == 0 {
		switch {
		case global != "":
			candidates = []string{global}
		case prd.Backend != "":
			candidates = []string{prd.Backend}
		default:
			candidates = []string{"ollama"}
		}
	}
	if mgr != nil {
		flags.ClaudeHold = mgr.HeldBackends(verifierPolicyBackends(policy, candidates))
	}
	window := time.Duration(config.NormalizeVerifierPolicyClaudeCapWindow(policy.ClaudeCapWindowMinutes)) * time.Minute
	flags.ClaudeSessionsInWindow = verifierClaudeWindow.Count(window)

	pick, ok := pickVerifierByPolicy(verifierPolicyInput{
		Policy: policy, Candidates: candidates, Flags: flags,
		PRDBackend: prd.Backend, PRDModel: prd.Model, ProjectDir: projectDir,
	}, tryAdmit, resolveAskBackend, askCompatible, isSessionKind)
	if !ok {
		log.Printf("[autonomous] verifier: policy: %s (%s/%s); using the configured selection", pick.Reason, prd.ID, task.ID)
		return verifierSelection{}, false
	}
	if pick.Claude {
		verifierClaudeWindow.Record()
	}
	if mgr != nil {
		if err := mgr.RecordVerifierPolicyChoice(prd.ID, task.ID, pick.Selection.Backend, pick.Selection.Model, pick.Reason); err != nil {
			log.Printf("[autonomous] verifier: RecordVerifierPolicyChoice(%s/%s): %v", prd.ID, task.ID, err)
		}
	}
	return pick.Selection, true
}
