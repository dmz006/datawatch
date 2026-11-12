package autonomous

// Gate scope at the executor: turns a gate scope plan (from
// internal/pipeline) into which test command actually runs for one task,
// keeps the plan on the gate result for later tracing, and folds vet
// regressions from non-full runs into the comparison.

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// gateScopeRunner supplies the facts a scope plan needs (changed files,
// package listings, import graph). It is a var, like runQualityGateTests,
// so tests can install a deterministic fake.
var gateScopeRunner pipeline.GateScopeRunner = pipeline.NewGateScopeListRunner()

// isFinaleGateTask reports whether t's gate runs as the PRD's final gate:
// t is tagged TaskKindFinalGate, or t is its PRD' last non-terminal task
// (every other task is already completed/failed/cancelled). That is the
// point at which the full suite is checked — a scoped run would miss
// breakage accumulated across the PRD here for the first time.
func (m *Manager) isFinaleGateTask(prd *PRD, t *Task) bool {
	if t == nil || prd == nil {
		return false
	}
	if t.Kind == TaskKindFinalGate {
		return true
	}
	isFinale := false
	m.store.WithLock(func() {
		if isTaskTerminal(t.Status) {
			return // already decided; keep the scoped treatment
		}
		isFinale = true
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				o := &prd.Story[si].Tasks[ti]
				if o == t {
					continue
				}
				if !isTaskTerminal(o.Status) {
					isFinale = false
					return
				}
			}
		}
	})
	return isFinale
}

// gateScopeSuffix renders a gate scope plan for the quality-gate log line
// and the Summary suffix, e.g.
//
//	scope touched-plus-smoke: 7 package(s) (3 touched, 2 importers, 2 smoke)
//	scope full: full test command run (final gate task always runs the full suite)
//
// The reason strings ("changed", "imports <pkg>", "smoke set") are the
// ones PlanGateScope writes into GateScopePlan.Reasons.
func gateScopeSuffix(plan *pipeline.GateScopePlan) string {
	if plan == nil {
		return ""
	}
	scope := plan.Scope
	if scope == "" {
		scope = pipeline.GateScopeFull
	}
	if scope == pipeline.GateScopeFull {
		reason := plan.FullReason
		if reason == "" {
			reason = "configured scope is full"
		}
		if fb := plan.FallbackReason; fb != "" {
			reason = fb
		}
		return "scope full: full test command run (" + reason + ")"
	}
	if plan.NothingToRun {
		return fmt.Sprintf("scope %s: nothing to run (no changed file maps to a Go package)", scope)
	}
	touched, importers, smoke := 0, 0, 0
	for _, r := range plan.Reasons {
		switch {
		case r.Reason == "changed":
			touched++
		case strings.HasPrefix(r.Reason, "imports "):
			importers++
		case r.Reason == "smoke set":
			smoke++
		}
	}
	parts := []string{fmt.Sprintf("%d touched", touched)}
	if importers > 0 {
		parts = append(parts, fmt.Sprintf("%d importers", importers))
	}
	if scope == pipeline.GateScopeTouchedPlusSmoke {
		parts = append(parts, fmt.Sprintf("%d smoke", smoke))
	}
	out := fmt.Sprintf("scope %s: %d package(s) (%s)", scope, len(plan.Packages), strings.Join(parts, ", "))
	if fb := plan.FallbackReason; fb != "" {
		out += "; scoped command not rewritable, full command was run: " + fb
	}
	return out
}

// runScopedQualityGate runs one task's quality gate in qgDir against base
// (the usable baseline for that directory): it plans the scope — final
// gate tasks and "full" scopes get the configured command with the full
// baseline, a scoped plan gets the scoped command against the restricted
// baseline — evaluates it, and for non-full scopes runs vet in its own
// run and folds new vet failures into the comparison as regressions.
// The plan is attached to the result (QualityGateResult.ScopePlan) for
// persistence and later tracing.
func (m *Manager) runScopedQualityGate(prd *PRD, t *Task, qgDir string, qgCfg pipeline.QualityGateConfig, baselines *qualityGateBaselines, base pipeline.TestResult) pipeline.QualityGateResult {
	scope, source, smokePatterns := m.resolveGateScopeSettings(prd)
	plan := pipeline.PlanGateScope(gateScopeRunner, pipeline.GateScopeRequest{
		Dir:           qgDir,
		BaseSHA:       t.BaseSHA,
		Scope:         scope,
		ScopeSource:   source,
		SmokePatterns: smokePatterns,
		IsFinal:       m.isFinaleGateTask(prd, t),
		TestCommand:   qgCfg.TestCommand,
	})

	gateBase := base
	cmd := qgCfg.TestCommand
	if !plan.NothingToRun && !plan.Full && len(plan.Packages) > 0 {
		scoped, rewritable, reason := pipeline.ScopedTestCommand(qgCfg.TestCommand, plan.Packages)
		if !rewritable {
			// The configured command cannot be safely rewritten to a
			// package list: run the full command and keep the reason on
			// the plan as the fallback.
			plan.Full = true
			plan.FullReason = reason
			plan.FallbackReason = reason
			plan.Packages = nil
			plan.Reasons = nil
		} else {
			cmd = scoped
			gateBase = pipeline.RestrictBaseline(base, plan.Packages)
		}
	}

	var cmp pipeline.QualityGateResult
	if plan.NothingToRun {
		// Nothing in scope to run, so nothing for the gate to catch:
		// record a STABLE result without executing a thing, and let the
		// persisted plan show why the gate was green.
		cmp = pipeline.QualityGateResult{
			Baseline: base,
			Current: pipeline.TestResult{
				Command:  qgCfg.TestCommand,
				ExitCode: 0,
				RunAt:    time.Now(),
				Output:   "not run: scoped gate had nothing to run in scope",
			},
			Summary: "STABLE: scoped gate had nothing to run (no changed file maps to a Go package)",
		}
	} else {
		current := runQualityGateTests(cmd, qgDir, qgCfg.EffectiveTimeout(), m.allowedProjectRoots())
		cmp = pipeline.EvaluateGate(gateBase, current, qgDir, qgCfg.EffectiveTimeout(), m.allowedProjectRoots())
		if scope != pipeline.GateScopeFull {
			mergeGateVet(qgDir, qgCfg, baselines, &cmp, &plan)
		}
	}

	cmp.ScopePlan = &plan
	if suffix := gateScopeSuffix(&plan); suffix != "" {
		cmp.Summary = cmp.Summary + " " + suffix
	}
	return cmp
}

// mergeGateVet runs vet for the scope's packages in qgDir as its own run
// (separate from the test run), and folds any NEW vet failures — packages
// that were not failing at the vet baseline — into the comparison as
// regressions named "vet:<pkg>", so block_on_regression and the
// gate_disagreement policy treat vet breakage like a test regression.
// A vet run that cannot produce a failure set (baseline or current) never
// counts as a regression: it is logged as a warning and the vet
// comparison is skipped (unusable is warn-only, same as tests).
func mergeGateVet(qgDir string, qgCfg pipeline.QualityGateConfig, baselines *qualityGateBaselines, cmp *pipeline.QualityGateResult, plan *pipeline.GateScopePlan) {
	if cmp.Invalid {
		// An invalid gate is warning-only under every policy; a vet
		// finding does not change that verdict.
		return
	}
	vetBase, baseUsable := baselines.vetForDir(qgDir)
	vetResult := runQualityGateTests(pipeline.ScopedVetCommand(plan.Packages), qgDir, qgCfg.EffectiveTimeout(), baselines.roots)
	if vetResult.ExitCode == -1 {
		log.Printf("[autonomous] WARNING: quality-gate vet run unusable in %s - vet comparison skipped (warn-only)", qgDir)
		return
	}
	failing := pipeline.VetFailuresFromOutput(vetResult.Output)
	if plan != nil {
		plan.VetFailures = failing
	}
	if !baseUsable {
		log.Printf("[autonomous] WARNING: quality-gate vet comparison skipped in %s (baseline unusable); vet failing packages this run: %v", qgDir, failing)
		return
	}
	regressions := pipeline.VetRegressions(vetBase, failing)
	if len(regressions) == 0 {
		return
	}
	cmp.Regression = true
	cmp.Improved = false
	cmp.NewFailures += len(regressions)
	cmp.NewFailureNames = append(cmp.NewFailureNames, regressions...)
	cmp.Summary = cmp.Summary + "; new vet failures not in baseline: " + strings.Join(regressions, ", ")
	log.Printf("[autonomous] quality-gate: vet regression(s) in %s: %s", qgDir, strings.Join(regressions, ", "))
}
