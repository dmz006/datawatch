package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/classifier"
)

// Verification modes recorded on VerificationResult.VerifierMode.
const (
	VerifierModeClassify = "classify"
	VerifierModeGenerate = "generate"
)

// Failure categories a classify-mode verification can carry. "infra" is the
// shared infrastructure category; the others are specific to classify mode.
const (
	// ClassifyCategoryInfra: the classifier produced no usable scores (missing
	// scores, timeout, transport error, abstain, capability, gate, stale
	// calibration). Always a block.
	ClassifyCategoryInfra = "infra"
	// ClassifyCategoryLowConfidence: the pass option won but its probability is
	// below the calibrated threshold. A block.
	ClassifyCategoryLowConfidence = "low-confidence"
	// ClassifyCategoryInconsistent: the ok read said pass but the severity read
	// said high or critical. A block; the two reads disagree.
	ClassifyCategoryInconsistent = "inconsistent-verdict"
)

// SeverityLabels are the five severity classes, in order. The classifier is
// given them as lettered options A..E.
var SeverityLabels = []string{"info", "low", "medium", "high", "critical"}

// VerdictRoute records where the classify calls ran.
type VerdictRoute struct {
	Node  string `json:"node"`
	Model string `json:"model"`
	// Path is the read path: "native" or "logprobs".
	Path string `json:"path,omitempty"`
}

// VerdictScores is the score vector behind a classify-mode verdict. With the
// route and the mode it makes a verdict reproducible.
type VerdictScores struct {
	// OK maps the ok option letters to probabilities: A = pass, B = block.
	OK map[string]float64 `json:"ok,omitempty"`
	// Severity maps the severity option letters (A..E, see SeverityLabels) to
	// probabilities.
	Severity map[string]float64 `json:"severity,omitempty"`
	// PassProbability is OK["A"], the value compared with Threshold.
	PassProbability float64 `json:"pass_probability"`
	// Threshold is the calibrated pass threshold applied.
	Threshold float64 `json:"threshold"`
	// Calibration is the fingerprint hash of the calibration record applied.
	Calibration string `json:"calibration,omitempty"`
}

// ClassifierClient is the slice of classifier.Client the verifier uses.
// Tests pass a fake.
type ClassifierClient interface {
	Classify(ctx context.Context, req classifier.Request) (*classifier.Result, error)
}

// CalibrationSource returns the calibration record fresh for a fingerprint, or
// an error (a *classifier.StaleError when none exists or it is stale).
type CalibrationSource interface {
	Require(fp classifier.Fingerprint) (*classifier.Record, error)
}

// ExplainFn is the optional generative call that produces the summary and
// issues. Its result never affects the verdict.
type ExplainFn func(ctx context.Context) (raw string, err error)

// ClassifyRuntime is everything classify mode needs besides the task prompt.
type ClassifyRuntime struct {
	Client      ClassifierClient
	Calibration CalibrationSource
	Route       VerdictRoute
	Path        classifier.Path
	Fingerprint classifier.Fingerprint
	// Explain is optional. Nil, an error or a garbled answer leaves the verdict
	// untouched and falls back to a fixed summary.
	Explain ExplainFn
	// RecordScores controls whether the full record (scores and route) is kept
	// on the returned result. The mode is always recorded.
	RecordScores bool
}

// RecordScoresEnabled resolves autonomous.verification_record_scores: unset
// means true (the full record is returned by default).
func RecordScoresEnabled(v *bool) bool { return v == nil || *v }

// Option texts for the two reads. The sets are fixed.
var okOptions = []string{
	"The diff plausibly implements the task spec.",
	"The diff does not implement the task spec.",
}

// ClassifyStems returns the stems for the ok and severity reads, built from the
// verifier prompt body (which already carries the spec and diff in data tags).
func ClassifyStems(promptBody string) (okStem, sevStem string) {
	okStem = promptBody + "\n\nQuestion: does the diff implement the task spec?"
	sevStem = promptBody + "\n\nQuestion: how severe are the problems with the diff, if any? " +
		"Choose the severity of the worst problem; choose the first option when there is none."
	return okStem, sevStem
}

// ClassifyVerify produces a verdict from classifier reads. promptBody is the
// verifier prompt text. Every failure is a block; none is a pass, and a block
// is never turned into a pass by a later step. Generate mode does not call it.
func ClassifyVerify(ctx context.Context, rt ClassifyRuntime, promptBody string, now time.Time) VerificationResult {
	res := VerificationResult{VerifiedAt: now, VerifierMode: VerifierModeClassify}
	route := rt.Route
	route.Path = string(rt.Path)
	if rt.RecordScores {
		r := route
		res.Route = &r
	}
	block := func(sev, cat, summary string, issues ...string) VerificationResult {
		res.OK, res.Severity, res.Category, res.Summary, res.Issues = false, sev, cat, summary, issues
		return res
	}
	if rt.Client == nil || rt.Calibration == nil {
		return block("high", "setup", "verifier: classify mode has no classifier runtime — judge never ran",
			"configuration failure, not a finding about the task")
	}
	rec, err := rt.Calibration.Require(rt.Fingerprint)
	if err != nil {
		return block("medium", ClassifyCategoryInfra, "verifier: classify calibration: "+err.Error())
	}
	scores := &VerdictScores{Threshold: rec.Threshold, Calibration: rec.FingerprintHash}
	keep := func() {
		if rt.RecordScores {
			res.Scores = scores
		}
	}
	okStem, sevStem := ClassifyStems(promptBody)

	okRes, err := rt.Client.Classify(ctx, classifier.Request{
		Node: route.Node, Model: route.Model, Mode: rt.Path, Stem: okStem, Options: okOptions})
	if err != nil {
		return block("medium", ClassifyCategoryInfra, "verifier: classify ok read failed: "+err.Error())
	}
	if okRes == nil {
		return block("medium", ClassifyCategoryInfra, "verifier: classify ok read returned no result")
	}
	scores.OK = copyProbs(okRes.Probabilities)
	keep()
	passP, havePass := okRes.Probabilities["A"]
	if !havePass {
		return block("medium", ClassifyCategoryInfra, "verifier: classify ok read carried no score for the pass option")
	}
	scores.PassProbability = passP
	if c := strings.ToUpper(strings.TrimSpace(okRes.Chosen)); c != "A" && c != "B" {
		// Empty or unknown choice: the classifier abstained.
		return block("medium", ClassifyCategoryInfra, "verifier: classify ok read abstained (no usable choice)")
	}

	sevRes, sevErr := rt.Client.Classify(ctx, classifier.Request{
		Node: route.Node, Model: route.Model, Mode: rt.Path, Stem: sevStem, Options: SeverityLabels})
	sev := ""
	if sevErr == nil && sevRes == nil {
		sevErr = fmt.Errorf("no result")
	}
	if sevErr == nil {
		scores.Severity = copyProbs(sevRes.Probabilities)
		sev = severityFromLetter(sevRes.Chosen)
		if sev == "" {
			sevErr = fmt.Errorf("severity read abstained (no usable choice)")
		}
	}

	// A block read stands whatever the severity read says.
	if strings.ToUpper(strings.TrimSpace(okRes.Chosen)) != "A" {
		if sev == "" {
			sev = "medium"
		}
		res = withExplanation(ctx, rt, res, false)
		res.OK, res.Severity, res.Category = false, sev, ""
		return res
	}
	// The pass option won: it must clear the calibrated threshold.
	if !rec.Pass(scores.PassProbability) {
		if sev == "" {
			sev = "medium"
		}
		return block(sev, ClassifyCategoryLowConfidence,
			fmt.Sprintf("verifier: pass probability %.3f is below the calibrated threshold %.3f", scores.PassProbability, rec.Threshold))
	}
	// A pass needs a usable severity read.
	if sevErr != nil {
		return block("medium", ClassifyCategoryInfra, "verifier: classify severity read failed: "+sevErr.Error())
	}
	if sev == "high" || sev == "critical" {
		return block(sev, ClassifyCategoryInconsistent,
			fmt.Sprintf("verifier: ok read passed but severity read says %s", sev))
	}
	res.OK, res.Severity = true, sev
	res = withExplanation(ctx, rt, res, true)
	return res
}

func copyProbs(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func severityFromLetter(l string) string {
	if len(l) != 1 {
		return ""
	}
	i := int(strings.ToUpper(l)[0]) - 'A'
	if i < 0 || i >= len(SeverityLabels) {
		return ""
	}
	return SeverityLabels[i]
}

// fixed summaries used when there is no usable explanation.
const (
	classifyPassSummary  = "verifier (classify): the diff implements the spec"
	classifyBlockSummary = "verifier (classify): the diff does not implement the spec"
)

// withExplanation attaches the summary and issues from the optional generative
// call. It never changes OK, Severity or Category.
func withExplanation(ctx context.Context, rt ClassifyRuntime, res VerificationResult, ok bool) VerificationResult {
	res.Summary = classifyBlockSummary
	if ok {
		res.Summary = classifyPassSummary
	}
	if rt.Explain == nil {
		return res
	}
	raw, err := rt.Explain(ctx)
	if err != nil {
		return res
	}
	if s, issues, perr := ParseExplanation(raw); perr == nil {
		if s != "" {
			res.Summary = s
		}
		res.Issues = issues
	}
	return res
}

// ParseExplanation reads {"summary": "...", "issues": ["..."]} from a
// generative answer. A garbled answer is an error and carries no weight.
func ParseExplanation(raw string) (summary string, issues []string, err error) {
	raw = strings.TrimSpace(raw)
	if i, j := strings.Index(raw, "{"), strings.LastIndex(raw, "}"); i >= 0 && j > i {
		raw = raw[i : j+1]
	}
	var v struct {
		Summary string   `json:"summary"`
		Issues  []string `json:"issues"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(v.Summary), v.Issues, nil
}

// NeverDowngrade returns the verdict a later step may report: a prior block is
// kept whatever next says. It is the single place the rule is stated.
func NeverDowngrade(prior, next VerificationResult) VerificationResult {
	if !prior.OK {
		return prior
	}
	return next
}
