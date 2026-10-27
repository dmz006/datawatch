package main

import (
	"context"
	"fmt"
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

// verifierCapacityFits is a dry run of the verifier's admission: it reports
// whether a verifier lease for the task on backend and model would be granted
// right now, without queueing, taking a lease or loading a model. Set by
// wireCapacity; nil until then, and everything then fits.
var verifierCapacityFits func(backend, model, taskID, prdID string) bool

// verifierCapacityChanged returns the capacity ledger's change channel (see
// capacity.Ledger.Changed). Set by wireCapacity; nil until then.
var verifierCapacityChanged func() <-chan struct{}

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

// policySelection turns the backend and model the selector named into a
// selection, through the same routing safety logic the global path uses. It
// takes no lease.
func policySelection(
	backend, model string,
	in verifierPolicyInput,
	resolveAskBackend func(raw string) (kind, model string, err error),
	askCompatible func(kind string) bool,
	isSessionKind func(kind string) bool,
) verifierSelection {
	var sel verifierSelection
	sel.Backend, sel.Kind, sel.Model, sel.UseSession = resolveVerifierBackendModelWithSessionModel(
		backend, model, in.PRDBackend, in.PRDModel, in.ProjectDir, model,
		resolveAskBackend, askCompatible, isSessionKind)
	if model == "" && sel.UseSession && isPiFamilyKind(sel.Kind) {
		// Leave the model to the pi-family pinned-model guard in the caller.
		sel.Model = ""
	}
	sel.Source = autonomouspkg.VerifierSourcePolicy
	return sel
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
	sel := policySelection(backend, model, in, resolveAskBackend, askCompatible, isSessionKind)
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
	flags = withVerifierClaudeLimits(mgr, policy, candidates, flags)

	in := verifierPolicyInput{
		Policy: policy, Candidates: candidates, Flags: flags,
		PRDBackend: prd.Backend, PRDModel: prd.Model, ProjectDir: projectDir,
	}
	pick, ok := pickVerifierByPolicy(in, tryAdmit, resolveAskBackend, askCompatible, isSessionKind)
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
	sel := pick.Selection
	if pick.ModelChosen == "" {
		// The policy chose this backend from the load, so it may choose again
		// while the request waits. The stronger verifier (it names a model)
		// stays where it is.
		sel.Retarget = newVerifierRetarget(mgr, in, task.ID, prd.ID, resolveAskBackend, askCompatible, isSessionKind)
	}
	return sel, true
}

// withVerifierClaudeLimits fills in the holds on the policy's backends and the
// Claude session count for the cap window.
func withVerifierClaudeLimits(mgr *autonomouspkg.Manager, policy config.VerifierPolicyConfig, candidates []string, flags autonomouspkg.VerifierSelectFlags) autonomouspkg.VerifierSelectFlags {
	if mgr != nil {
		flags.ClaudeHold = mgr.HeldBackends(verifierPolicyBackends(policy, candidates))
	}
	window := time.Duration(config.NormalizeVerifierPolicyClaudeCapWindow(policy.ClaudeCapWindowMinutes)) * time.Minute
	flags.ClaudeSessionsInWindow = verifierClaudeWindow.Count(window)
	return flags
}

// verifierRetargetLoad is the autonomouspkg.VerifierLoad a waiting request
// reads. The backend it waits on and every backend it left read as full; any
// other backend fits when the ledger dry run says so. It reserves nothing.
type verifierRetargetLoad struct {
	skip    map[string]bool
	fits    func(backend, model string) bool
	modelOf func(backend string) string

	results map[string]bool
}

func (l *verifierRetargetLoad) TryAdmit(backend string) bool {
	if l.skip[backend] {
		return false
	}
	if ok, seen := l.results[backend]; seen {
		return ok
	}
	ok := l.fits == nil || l.fits(backend, l.modelOf(backend))
	l.results[backend] = ok
	return ok
}

func (l *verifierRetargetLoad) Free(string) int { return -1 }

func (l *verifierRetargetLoad) RelativeLoad(backend string) float64 {
	if verifierBackendLoad == nil {
		return 0
	}
	return verifierBackendLoad(backend, l.modelOf(backend))
}

// newVerifierRetarget builds verifierSelection.Retarget for a pick the policy
// made from the load. Each call re-runs the selector with the same policy and
// candidates and fresh holds and Claude window count. The retry and final-gate
// triggers are left off: they decide the first pick only, and this one was
// not made by them. The waiting backend and those already left read as full
// but stay in the eligible list, so fixed, gpu-only and claude-only keep their
// rules and never move, prefer-free moves to the first fitting backend and
// balance to the lowest load among the fitting ones. It takes no lease and
// writes no decision log; the caller records a move it acts on.
func newVerifierRetarget(
	mgr *autonomouspkg.Manager,
	in verifierPolicyInput,
	taskID, prdID string,
	resolveAskBackend func(raw string) (kind, model string, err error),
	askCompatible func(kind string) bool,
	isSessionKind func(kind string) bool,
) func(waiting string, left []string) (verifierSelection, string, bool) {
	return func(waiting string, left []string) (verifierSelection, string, bool) {
		skip := map[string]bool{waiting: true}
		for _, b := range left {
			skip[b] = true
		}
		load := &verifierRetargetLoad{
			skip: skip,
			fits: func(backend, model string) bool {
				fits := verifierCapacityFits
				return fits == nil || fits(backend, model, taskID, prdID)
			},
			modelOf: func(backend string) string {
				_, m, _ := resolveAskBackend(backend)
				return m
			},
			results: map[string]bool{},
		}
		flags := withVerifierClaudeLimits(mgr, in.Policy, in.Candidates, autonomouspkg.VerifierSelectFlags{})
		backend, model, reason := autonomouspkg.SelectVerifierBackend(in.Policy, in.Candidates, load, flags)
		if backend == "" || model != "" || !load.TryAdmit(backend) {
			return verifierSelection{}, "", false
		}
		sel := policySelection(backend, model, in, resolveAskBackend, askCompatible, isSessionKind)
		if sel.Backend != backend {
			// The routing safety logic downgraded the pick to a backend the
			// dry run did not check.
			return verifierSelection{}, "", false
		}
		return sel, reason, true
	}
}

// verifierLeaseWait carries what awaitVerifierLease needs besides the
// selection.
type verifierLeaseWait struct {
	// Admit is the blocking admission (verifierCapacityAdmit for the task).
	Admit func(ctx context.Context, backend, model string) (release func(), node string, err error)
	// Take is the non-blocking admission (verifierCapacityTryAdmit); nil makes
	// a move report the new backend and admit on it with Admit.
	Take func(backend, model string) (release func(), node string, ok bool)
	// Changed returns the capacity change channel (may be nil).
	Changed func() <-chan struct{}
	// Complete finishes a move target (for a pi-family session backend, the
	// pinned-model guard). It returns false when the target cannot run, and
	// the request then stays where it is.
	Complete func(sel *verifierSelection) bool
	// Mgr, PRDID and TaskID say where a move is recorded; Mgr may be nil.
	Mgr           *autonomouspkg.Manager
	PRDID, TaskID string
}

// verifierLease is where a verifier ended up.
type verifierLease struct {
	// Sel is the selection the verifier runs under: the one passed in, or the
	// move target when Moved.
	Sel     verifierSelection
	Release func()
	Node    string
	Moved   bool
}

// awaitVerifierLease waits for the verifier's lease on sel.Backend with sel's
// model. While it waits, a selection that carries a Retarget re-runs the
// policy on every tick and every capacity change and takes the lease on
// another backend as soon as one has room. A move is written to the Automaton's
// decision log and the task's policy choice, and counts toward the Claude
// session cap when it lands on a Claude backend. A selection with no Retarget
// waits on its backend alone.
func awaitVerifierLease(ctx context.Context, sel verifierSelection, w verifierLeaseWait) (verifierLease, error) {
	targets := map[string]verifierSelection{}
	in := retargetWaitIn{Backend: sel.Backend, Model: sel.Model, Wait: w.Admit, Take: w.Take, Changed: w.Changed}
	if sel.Retarget != nil {
		in.Next = func(waiting string, left []string) (string, string, string, bool) {
			ns, reason, ok := sel.Retarget(waiting, left)
			if !ok || (w.Complete != nil && !w.Complete(&ns)) {
				return "", "", "", false
			}
			targets[ns.Backend] = ns
			return ns.Backend, ns.Model, reason, true
		}
		in.OnMove = func(from, to, _, reason string) {
			ns := targets[to]
			why := fmt.Sprintf("moved from %s: %s", from, reason)
			log.Printf("[autonomous] verifier: %s -> %s (%s/%s): %s", from, to, w.PRDID, w.TaskID, reason)
			if w.Mgr != nil {
				if err := w.Mgr.RecordVerifierPolicyChoice(w.PRDID, w.TaskID, to, ns.Model, why); err != nil {
					log.Printf("[autonomous] verifier: RecordVerifierPolicyChoice(%s/%s): %v", w.PRDID, w.TaskID, err)
				}
			}
			if config.IsClaudeVerifierBackend(to) {
				verifierClaudeWindow.Record()
			}
		}
	}
	out, err := retargetWait(ctx, in)
	if err != nil {
		return verifierLease{}, err
	}
	lease := verifierLease{Sel: sel, Release: out.Release, Node: out.Node}
	if !out.Moved {
		return lease, nil
	}
	lease.Sel, lease.Moved = targets[out.Backend], true
	if lease.Release == nil {
		// No non-blocking admission is wired: admit on the new backend now.
		lease.Release, lease.Node, err = w.Admit(ctx, lease.Sel.Backend, lease.Sel.Model)
		if err != nil {
			return verifierLease{}, err
		}
	}
	return lease, nil
}
