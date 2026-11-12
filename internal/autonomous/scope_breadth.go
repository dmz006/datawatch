package autonomous

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/classifier"
)

// ScopeBreadthQuestion is the closed question put to the classifier. The
// option set is fixed.
const ScopeBreadthQuestion = "Question: is the change this task asks for one coherent change, " +
	"or does it bundle several separable units of work?"

var scopeBreadthOptions = []string{
	"One coherent change.",
	"Bundles multiple separable units.",
}

// ScopeBreadthStem builds the stem from the task text (spec and, when known,
// the failing verification summary).
func ScopeBreadthStem(taskText string) string {
	return taskText + "\n\n" + ScopeBreadthQuestion
}

// ScopeBreadthFingerprint derives the calibration fingerprint of the scope
// question from the verifier fingerprint of the same model and node. The prompt
// hash covers the scope question, so the scope classifier has its own
// calibration set and a verifier calibration is never reused for it.
func ScopeBreadthFingerprint(base classifier.Fingerprint) classifier.Fingerprint {
	fp := classifier.NewFingerprint(base.Model, base.Quantization, base.Node, ScopeBreadthQuestion, "")
	fp.TemplateHash = base.TemplateHash
	return fp
}

// ScopeBreadthResult is the outcome of one scope-breadth read. TooBroad is only
// ever true on a clean, calibrated B read. Any failure leaves it false and
// fills InfraNote; a scope-breadth failure never blocks anything.
type ScopeBreadthResult struct {
	TooBroad  bool
	InfraNote string
	Scores    map[string]float64
}

// ScopeBreadthRun runs one read for a failed task's text.
type ScopeBreadthRun func(ctx context.Context, taskText string) ScopeBreadthResult

// ClassifyScopeBreadth asks the closed question through rt's classifier. The
// runtime's Fingerprint must already be the scope fingerprint.
func ClassifyScopeBreadth(ctx context.Context, rt ClassifyRuntime, taskText string) ScopeBreadthResult {
	note := func(format string, a ...any) ScopeBreadthResult {
		return ScopeBreadthResult{InfraNote: "scope-breadth: " + fmt.Sprintf(format, a...)}
	}
	if rt.Client == nil || rt.Calibration == nil {
		return note("no classifier runtime")
	}
	rec, err := rt.Calibration.Require(rt.Fingerprint)
	if err != nil {
		return note("calibration: %v", err)
	}
	r, err := rt.Client.Classify(ctx, classifier.Request{
		Node: rt.Route.Node, Model: rt.Route.Model, Mode: rt.Path,
		Stem: ScopeBreadthStem(taskText), Options: scopeBreadthOptions})
	if err != nil {
		return note("read failed: %v", err)
	}
	if r == nil {
		return note("read returned no result")
	}
	pA, ok := r.Probabilities["A"]
	if !ok {
		return note("read carried no score for the coherent option")
	}
	c := strings.ToUpper(strings.TrimSpace(r.Chosen))
	if c != "A" && c != "B" {
		return note("read abstained")
	}
	out := ScopeBreadthResult{Scores: copyProbs(r.Probabilities)}
	out.TooBroad = c == "B" && !rec.Pass(pA)
	return out
}

// scopeBreadth is the manager's wiring for the scope-breadth call site.
type scopeBreadth struct {
	enabled func() bool
	run     ScopeBreadthRun
}

// SetScopeBreadth wires the scope-breadth classifier. enabled reads the
// autonomous.scope_breadth_enabled key live (default off). A nil enabled or run
// disables the call site.
func (m *Manager) SetScopeBreadth(enabled func() bool, run ScopeBreadthRun) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if enabled == nil || run == nil {
		m.scopeBreadth = nil
		return
	}
	m.scopeBreadth = &scopeBreadth{enabled: enabled, run: run}
}

// scopeBreadthFor runs the scope-breadth read for a failed verification. It
// applies only to a content-category failure, only when the key is on, and only
// when the classifier is wired. It never blocks and never returns an error: a
// classifier failure yields (false, note) and the note is recorded on the PRD.
func (m *Manager) scopeBreadthFor(ctx context.Context, prd *PRD, t *Task, vr VerificationResult) (tooBroad bool, note string) {
	if vr.OK || orContentCategory(vr.Category) != FailureCategoryContent {
		return false, ""
	}
	m.mu.Lock()
	sb := m.scopeBreadth
	m.mu.Unlock()
	if sb == nil || !sb.enabled() {
		return false, ""
	}
	text := strings.TrimSpace(t.Spec)
	if vr.Summary != "" {
		text += "\n\nVerification finding: " + vr.Summary
	}
	res := sb.run(ctx, text)
	if res.InfraNote != "" {
		prd.Decisions = append(prd.Decisions, Decision{
			At: time.Now(), Kind: "scope_breadth_infra", Actor: "autonomous",
			Note: fmt.Sprintf("unit=%s %s", t.ID, res.InfraNote), FailureCategory: "infra",
		})
		return false, res.InfraNote
	}
	return res.TooBroad, ""
}
