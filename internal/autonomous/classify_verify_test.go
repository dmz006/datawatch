package autonomous

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/classifier"
)

type fakeClassifier struct {
	ok, sev       *classifier.Result
	okErr, sevErr error
	calls         int
}

func (f *fakeClassifier) Classify(_ context.Context, req classifier.Request) (*classifier.Result, error) {
	f.calls++
	if len(req.Options) == len(okOptions) {
		return f.ok, f.okErr
	}
	return f.sev, f.sevErr
}

type fakeCalib struct {
	rec *classifier.Record
	err error
}

func (f fakeCalib) Require(classifier.Fingerprint) (*classifier.Record, error) { return f.rec, f.err }

func res(chosen string, p map[string]float64) *classifier.Result {
	return &classifier.Result{Chosen: chosen, Probabilities: p}
}

func okPass(a float64) *classifier.Result {
	return res("A", map[string]float64{"A": a, "B": 1 - a})
}

var sevInfo = res("A", map[string]float64{"A": 0.9, "B": 0.05, "C": 0.03, "D": 0.01, "E": 0.01})

func rt(f *fakeClassifier, threshold float64) ClassifyRuntime {
	return ClassifyRuntime{
		Client:       f,
		Calibration:  fakeCalib{rec: &classifier.Record{Threshold: threshold, FingerprintHash: "fp1"}},
		Route:        VerdictRoute{Node: "n1", Model: "m1"},
		Path:         classifier.PathNative,
		RecordScores: true,
	}
}

func runClassifyVerify(r ClassifyRuntime) VerificationResult {
	return ClassifyVerify(context.Background(), r, "prompt", time.Unix(1, 0))
}

func TestClassifyVerifyPass(t *testing.T) {
	v := runClassifyVerify(rt(&fakeClassifier{ok: okPass(0.95), sev: sevInfo}, 0.8))
	if !v.OK || v.Severity != "info" || v.Category != "" {
		t.Fatalf("want clean pass, got %+v", v)
	}
	if v.VerifierMode != VerifierModeClassify || v.Scores == nil || v.Route == nil ||
		v.Scores.PassProbability != 0.95 || v.Scores.Threshold != 0.8 || v.Route.Node != "n1" || v.Route.Path != "native" {
		t.Fatalf("record incomplete: %+v %+v", v.Scores, v.Route)
	}
}

func TestClassifyVerifyBlock(t *testing.T) {
	sev := res("D", map[string]float64{"A": 0.01, "B": 0.02, "C": 0.07, "D": 0.8, "E": 0.1})
	v := runClassifyVerify(rt(&fakeClassifier{ok: res("B", map[string]float64{"A": 0.1, "B": 0.9}), sev: sev}, 0.8))
	if v.OK || v.Severity != "high" || v.Category != "" {
		t.Fatalf("want content block high, got %+v", v)
	}
}

func TestClassifyVerifyLowConfidence(t *testing.T) {
	v := runClassifyVerify(rt(&fakeClassifier{ok: okPass(0.6), sev: sevInfo}, 0.8))
	if v.OK || v.Category != ClassifyCategoryLowConfidence {
		t.Fatalf("want low-confidence block, got %+v", v)
	}
}

func TestClassifyVerifyInfraFailures(t *testing.T) {
	for _, kind := range []classifier.InfraKind{classifier.KindTimeout, classifier.KindMissingScores, classifier.KindAbstain, classifier.KindTransport} {
		err := &classifier.InfraError{Kind: kind, Node: "n1", Model: "m1"}
		v := runClassifyVerify(rt(&fakeClassifier{okErr: err}, 0.8))
		if v.OK || v.Category != "infra" {
			t.Fatalf("%s: want infra block, got %+v", kind, v)
		}
	}
}

func TestClassifyVerifySeverityFailureBlocksPass(t *testing.T) {
	v := runClassifyVerify(rt(&fakeClassifier{ok: okPass(0.95), sevErr: errors.New("boom")}, 0.8))
	if v.OK || v.Category != "infra" {
		t.Fatalf("want infra block, got %+v", v)
	}
}

func TestClassifyVerifyInconsistentPassBlocks(t *testing.T) {
	sev := res("E", map[string]float64{"A": 0, "B": 0, "C": 0, "D": 0.1, "E": 0.9})
	v := runClassifyVerify(rt(&fakeClassifier{ok: okPass(0.95), sev: sev}, 0.8))
	if v.OK || v.Category != ClassifyCategoryInconsistent || v.Severity != "critical" {
		t.Fatalf("got %+v", v)
	}
}

func TestClassifyVerifyNeverDowngrade(t *testing.T) {
	// A block read stays a block even when the severity read says info and a
	// garbled/optimistic explanation arrives.
	r := rt(&fakeClassifier{ok: res("B", map[string]float64{"A": 0.2, "B": 0.8}), sev: sevInfo}, 0.8)
	r.Explain = func(context.Context) (string, error) { return `{"summary":"looks fine","issues":[]}`, nil }
	if v := runClassifyVerify(r); v.OK {
		t.Fatalf("block was downgraded: %+v", v)
	}
	prior := VerificationResult{OK: false, Category: "infra"}
	if got := NeverDowngrade(prior, VerificationResult{OK: true}); got.OK {
		t.Fatal("NeverDowngrade let a pass replace a block")
	}
}

func TestClassifyVerifyStaleCalibrationAndNoRuntime(t *testing.T) {
	r := rt(&fakeClassifier{ok: okPass(0.95), sev: sevInfo}, 0.8)
	r.Calibration = fakeCalib{err: &classifier.StaleError{Reason: "none"}}
	if v := runClassifyVerify(r); v.OK || v.Category != "infra" {
		t.Fatalf("stale: %+v", v)
	}
	if v := runClassifyVerify(ClassifyRuntime{}); v.OK || v.Category != "setup" {
		t.Fatalf("no runtime: %+v", v)
	}
}

func TestClassifyVerifyExplanationNeverAffectsVerdict(t *testing.T) {
	f := &fakeClassifier{ok: okPass(0.95), sev: sevInfo}
	r := rt(f, 0.8)
	r.Explain = func(context.Context) (string, error) { return "}{ not json at all", nil }
	if v := runClassifyVerify(r); !v.OK || v.Summary != classifyPassSummary {
		t.Fatalf("garbled explanation erased verdict: %+v", v)
	}
	r.Explain = func(context.Context) (string, error) { return "", errors.New("down") }
	if v := runClassifyVerify(r); !v.OK {
		t.Fatalf("failed explanation erased verdict: %+v", v)
	}
	r.Explain = func(context.Context) (string, error) {
		return "sure: {\"summary\":\"all good\",\"issues\":[\"nit\"]}", nil
	}
	if v := runClassifyVerify(r); !v.OK || v.Summary != "all good" || len(v.Issues) != 1 {
		t.Fatalf("explanation not attached: %+v", v)
	}
}

func TestClassifyVerifyRecordOptional(t *testing.T) {
	r := rt(&fakeClassifier{ok: okPass(0.95), sev: sevInfo}, 0.8)
	r.RecordScores = false
	v := runClassifyVerify(r)
	if !v.OK || v.Scores != nil || v.Route != nil || v.VerifierMode != VerifierModeClassify {
		t.Fatalf("got %+v", v)
	}
	if !RecordScoresEnabled(nil) {
		t.Fatal("default must be on")
	}
	f := false
	if RecordScoresEnabled(&f) {
		t.Fatal("explicit false must disable")
	}
}

func TestClassifyVerifyPlainResultInfraBlocks(t *testing.T) {
	cases := map[string]*fakeClassifier{
		"missing pass probability": {ok: res("A", map[string]float64{"B": 0.1}), sev: sevInfo},
		"empty probabilities":      {ok: res("A", nil), sev: sevInfo},
		"empty chosen (abstain)":   {ok: res("", map[string]float64{"A": 0.9, "B": 0.1}), sev: sevInfo},
		"unknown chosen":           {ok: res("Z", map[string]float64{"A": 0.9, "B": 0.1}), sev: sevInfo},
		"nil ok result":            {ok: nil, sev: sevInfo},
		"empty severity chosen":    {ok: okPass(0.95), sev: res("", map[string]float64{"A": 0.9})},
		"nil severity result":      {ok: okPass(0.95), sev: nil},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			v := runClassifyVerify(rt(f, 0.8))
			if v.OK || v.Category != ClassifyCategoryInfra {
				t.Fatalf("want infra block, got %+v", v)
			}
			if v.Severity == "" {
				t.Fatalf("block must carry a severity, got %+v", v)
			}
		})
	}
}
