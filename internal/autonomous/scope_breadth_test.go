package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/classifier"
)

type scopeFake struct {
	r   *classifier.Result
	err error
	req classifier.Request
}

func (f *scopeFake) Classify(_ context.Context, req classifier.Request) (*classifier.Result, error) {
	f.req = req
	return f.r, f.err
}

func scopeRT(f *scopeFake, cal fakeCalib) ClassifyRuntime {
	return ClassifyRuntime{Client: f, Calibration: cal, Route: VerdictRoute{Node: "n", Model: "m"}, Path: classifier.PathNative}
}

var scopeCal = fakeCalib{rec: &classifier.Record{Threshold: 0.7}}

func TestScopeBreadthTooBroad(t *testing.T) {
	f := &scopeFake{r: res("B", map[string]float64{"A": 0.1, "B": 0.9})}
	r := ClassifyScopeBreadth(context.Background(), scopeRT(f, scopeCal), "spec")
	if !r.TooBroad || r.InfraNote != "" {
		t.Fatalf("got %+v", r)
	}
	if len(f.req.Options) != 2 || !strings.Contains(f.req.Stem, ScopeBreadthQuestion) {
		t.Fatalf("request %+v", f.req)
	}
}

func TestScopeBreadthCoherent(t *testing.T) {
	f := &scopeFake{r: res("A", map[string]float64{"A": 0.9, "B": 0.1})}
	if r := ClassifyScopeBreadth(context.Background(), scopeRT(f, scopeCal), "s"); r.TooBroad || r.InfraNote != "" {
		t.Fatalf("got %+v", r)
	}
}

func TestScopeBreadthFailuresLeaveFalseWithNote(t *testing.T) {
	cases := map[string]ClassifyRuntime{
		"error":     scopeRT(&scopeFake{err: errors.New("boom")}, scopeCal),
		"nil":       scopeRT(&scopeFake{}, scopeCal),
		"noscore":   scopeRT(&scopeFake{r: res("B", map[string]float64{"B": 1})}, scopeCal),
		"abstain":   scopeRT(&scopeFake{r: res("", map[string]float64{"A": 0.5, "B": 0.5})}, scopeCal),
		"stale":     scopeRT(&scopeFake{}, fakeCalib{err: &classifier.StaleError{Reason: "x"}}),
		"noruntime": {},
	}
	for name, rt := range cases {
		r := ClassifyScopeBreadth(context.Background(), rt, "s")
		if r.TooBroad || r.InfraNote == "" {
			t.Errorf("%s: got %+v", name, r)
		}
	}
}

func TestScopeBreadthFingerprintDiffers(t *testing.T) {
	base := classifier.NewFingerprint("m", "q", "n", "verifier prompt", "tmpl")
	if ScopeBreadthFingerprint(base).Hash() == base.Hash() {
		t.Fatal("scope fingerprint must differ from the verifier fingerprint")
	}
}

func TestScopeBreadthForGating(t *testing.T) {
	calls := 0
	on := true
	m := &Manager{}
	m.SetScopeBreadth(func() bool { return on }, func(context.Context, string) ScopeBreadthResult {
		calls++
		return ScopeBreadthResult{TooBroad: true}
	})
	prd, task := &PRD{}, &Task{ID: "t1", Spec: "do things"}
	fail := VerificationResult{OK: false, Summary: "bad"}
	if tb, _ := m.scopeBreadthFor(context.Background(), prd, task, fail); !tb || calls != 1 {
		t.Fatal("content failure should be classified")
	}
	if tb, _ := m.scopeBreadthFor(context.Background(), prd, task, VerificationResult{OK: false, Category: "infra"}); tb || calls != 1 {
		t.Fatal("non-content failure must not call the classifier")
	}
	if tb, _ := m.scopeBreadthFor(context.Background(), prd, task, VerificationResult{OK: true}); tb || calls != 1 {
		t.Fatal("pass must not call the classifier")
	}
	on = false
	if tb, _ := m.scopeBreadthFor(context.Background(), prd, task, fail); tb || calls != 1 {
		t.Fatal("key off must not call the classifier")
	}
}

func TestScopeBreadthForInfraNoteRecorded(t *testing.T) {
	m := &Manager{}
	m.SetScopeBreadth(func() bool { return true }, func(context.Context, string) ScopeBreadthResult {
		return ScopeBreadthResult{InfraNote: "scope-breadth: read failed: boom"}
	})
	prd := &PRD{}
	tb, note := m.scopeBreadthFor(context.Background(), prd, &Task{ID: "t1"}, VerificationResult{})
	if tb || note == "" || len(prd.Decisions) != 1 || prd.Decisions[0].Kind != "scope_breadth_infra" {
		t.Fatalf("tb=%v note=%q decisions=%+v", tb, note, prd.Decisions)
	}
}

func TestScopeBreadthUnwiredIsNoop(t *testing.T) {
	m := &Manager{}
	if tb, n := m.scopeBreadthFor(context.Background(), &PRD{}, &Task{}, VerificationResult{}); tb || n != "" {
		t.Fatal("unwired must be a no-op")
	}
}
