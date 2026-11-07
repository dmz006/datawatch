package autonomous

import (
	"context"
	"fmt"
	"strings"
)

// GuardrailClassifyRun runs one classify-mode verification over prompt on the
// backend/model that judges a guardrail. Every failure it reports is a block.
type GuardrailClassifyRun func(ctx context.Context, backend, model, prompt string) VerificationResult

// guardrailClassify is the classify-mode gate for guardrail outcomes.
type guardrailClassify struct {
	lookup VerificationModeLookup
	run    GuardrailClassifyRun
}

// SetGuardrailClassify wires the classify-mode gate. lookup resolves the
// verification mode of the guardrail judge's backend/model; run executes the
// classifier. A nil lookup or run leaves every guardrail in generate mode.
func (m *Manager) SetGuardrailClassify(lookup VerificationModeLookup, run GuardrailClassifyRun) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lookup == nil || run == nil {
		m.guardrailClassify = nil
		return
	}
	m.guardrailClassify = &guardrailClassify{lookup: lookup, run: run}
}

// classifyGate is the final gate over a generative guardrail verdict (the
// findings of rules-review, ci-watch, and the per-task and per-story judges).
// When the judge's resolved mode is classify, the outcome becomes pass|warn|
// block from the classifier; the generative prose stays as the explanation.
//
// Rules: a prior block is never downgraded, and then no classifier call is
// made; a prior warn is never raised to pass; a classifier error, missing
// scores or an abstain is a block with the infra category (assigned here, by
// the harness, never by the model). There is no fallback to generate mode.
func (m *Manager) classifyGate(ctx context.Context, inv GuardrailInvocation, v GuardrailVerdict) GuardrailVerdict {
	m.mu.Lock()
	gc := m.guardrailClassify
	m.mu.Unlock()
	if gc == nil || inv.GuardrailBackend == "" {
		return v
	}
	if string(gc.lookup(inv.GuardrailBackend, inv.GuardrailModel)) != VerifierModeClassify {
		return v
	}
	v.Mode = VerifierModeClassify
	if v.Outcome == "block" {
		return v
	}
	body := classifyGateBody(inv, v)
	launch := m.planGuardrailLaunch(inv)
	ctx = WithGuardrailLaunch(ctx, launch)
	if launch != nil {
		v.CeilingPath, v.CeilingMB = launch.Path, launch.CeilingMB
	}
	res := gc.run(ctx, inv.GuardrailBackend, inv.GuardrailModel, body)
	v.Scores, v.Route = res.Scores, res.Route
	if !res.OK {
		prose := v.Summary
		v.Outcome, v.Category = "block", res.Category
		v.Severity = res.Severity
		v.Summary = res.Summary
		if prose != "" && prose != res.Summary {
			v.Issues = append([]string{"judge prose: " + prose}, v.Issues...)
		}
		v.Issues = append(v.Issues, res.Issues...)
		return v
	}
	if v.Outcome == "" || v.Outcome == "pass" {
		v.Outcome = "pass"
		if res.Severity == "medium" || res.Severity == "low" {
			v.Outcome = "warn"
		}
	}
	if v.Severity == "" {
		v.Severity = res.Severity
	}
	return v
}

type guardrailLaunchKey struct{}

// WithGuardrailLaunch carries the ceiling launch for a one-shot guardrail run
// to the runner. A nil launch leaves ctx unchanged.
func WithGuardrailLaunch(ctx context.Context, l *verifyLaunch) context.Context {
	if l == nil {
		return ctx
	}
	return context.WithValue(ctx, guardrailLaunchKey{}, l)
}

// planGuardrailLaunch resolves the ceiling and path for a guardrail run from
// the unit it judges, with the same resolver the verification runs use.
func (m *Manager) planGuardrailLaunch(inv GuardrailInvocation) *verifyLaunch {
	prd, t := &PRD{}, &Task{ID: inv.UnitID}
	if m.store != nil {
		if p, ok := m.store.GetPRD(inv.PRDID); ok {
			prd = p
		}
		if tk, ok := m.store.GetTask(inv.UnitID); ok {
			t = tk
		}
	}
	return m.planVerifyLaunch(prd, t)
}

// GuardrailRunArgv wraps the one-shot guardrail command carried in ctx's
// launch with the shared scope argument builder. It returns the command to
// run and the ceiling path ("" when no launch is carried).
func GuardrailRunArgv(ctx context.Context, base []string) ([]string, string) {
	l, _ := ctx.Value(guardrailLaunchKey{}).(*verifyLaunch)
	argv, path, _ := scopedArgv(base, "guardrail", l)
	return argv, path
}

// classifyGateBody builds the data the classifier reads: the unit spec and the
// generative findings.
func classifyGateBody(inv GuardrailInvocation, v GuardrailVerdict) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Guardrail: %s (%s %s)\n", inv.Guardrail, inv.Level, inv.UnitID)
	fmt.Fprintf(&b, "<spec>\n%s\n</spec>\n", inv.UnitSpec)
	fmt.Fprintf(&b, "<findings outcome=%q>\n%s\n", v.Outcome, v.Summary)
	for _, is := range v.Issues {
		fmt.Fprintf(&b, "- %s\n", is)
	}
	b.WriteString("</findings>")
	return b.String()
}
