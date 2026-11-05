package supervisorloop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// evalFake answers per model: a queue of replies, or a fixed error.
type evalFake struct {
	mu      sync.Mutex
	replies map[string][]string
	errs    map[string]error
	calls   map[string]int
}

func (f *evalFake) Ask(_ context.Context, r Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[r.Model]++
	if err := f.errs[r.Model]; err != nil {
		return "", err
	}
	q := f.replies[r.Model]
	if len(q) == 0 {
		return "", errors.New("no reply")
	}
	f.replies[r.Model] = q[1:]
	return q[0], nil
}

func evalCasesDir(t *testing.T, job string, cases map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, job), 0o755); err != nil {
		t.Fatal(err)
	}
	for n, body := range cases {
		if err := os.WriteFile(filepath.Join(dir, job, n+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func steppingClock() func() time.Time {
	t := time.Unix(0, 0)
	return func() time.Time { t = t.Add(100 * time.Millisecond); return t }
}

func TestSupervisorLoopEvalPassRateAndRetries(t *testing.T) {
	dir := evalCasesDir(t, "classify_ci_failure", map[string]string{
		"a": `{"input":"lint failure","expected_validator":"pass","expected_value":"lint"}`,
		"b": `{"input":"say banana","expected_validator":"fail"}`,
	})
	f := &evalFake{replies: map[string][]string{
		// case a: first reply invalid (retry), second right; case b: invalid twice -> escalated
		"m1": {"nonsense", "lint", "banana", "banana"},
	}}
	ev := &Evaluator{Client: f, Settings: DefaultSettings(), Now: steppingClock()}
	rep, err := ev.EvalRun(context.Background(), "classify_ci_failure", []string{"m1"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	m := rep.Models[0]
	if m.Cases != 2 || m.Passed != 2 {
		t.Errorf("cases/passed = %d/%d, want 2/2", m.Cases, m.Passed)
	}
	if m.Retries != 2 {
		t.Errorf("retries = %d, want 2", m.Retries)
	}
	if m.ValidatorFailures != 3 {
		t.Errorf("validator failures = %d, want 3", m.ValidatorFailures)
	}
	if m.Escalations != 1 {
		t.Errorf("escalations = %d, want 1", m.Escalations)
	}
	if m.MaxDurationMS < m.MedianDurationMS || m.MaxDurationMS == 0 {
		t.Errorf("durations median=%d max=%d", m.MedianDurationMS, m.MaxDurationMS)
	}
}

func TestSupervisorLoopEvalWrongValueNotPassed(t *testing.T) {
	dir := evalCasesDir(t, "classify_ci_failure", map[string]string{
		"a": `{"input":"x","expected_validator":"pass","expected_value":"lint"}`,
	})
	f := &evalFake{replies: map[string][]string{"m1": {"infra"}}}
	rep, err := (&Evaluator{Client: f, Settings: DefaultSettings()}).EvalRun(context.Background(), "classify_ci_failure", []string{"m1"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Models[0].Passed != 0 {
		t.Errorf("passed = %d, want 0 (valid class but not the expected one)", rep.Models[0].Passed)
	}
}

func TestSupervisorLoopEvalUnreachableModelError(t *testing.T) {
	dir := evalCasesDir(t, "pick_backend", map[string]string{
		"a": `{"input":"cheap","candidates":["x","y"],"expected_validator":"pass"}`,
	})
	f := &evalFake{errs: map[string]error{"down1": errors.New("connection refused"), "down2": errors.New("no such model")}}
	_, err := (&Evaluator{Client: f, Settings: DefaultSettings()}).EvalRun(context.Background(), "pick_backend", []string{"down1", "down2"}, dir)
	if err == nil {
		t.Fatal("want an error when no model is reachable")
	}
	for _, want := range []string{"down1", "down2", "connection refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestSupervisorLoopEvalPartialReachable(t *testing.T) {
	dir := evalCasesDir(t, "pick_backend", map[string]string{
		"a": `{"input":"cheap","candidates":["x","y"],"expected_validator":"pass","expected_value":"x"}`,
	})
	f := &evalFake{replies: map[string][]string{"up": {"x"}}, errs: map[string]error{"down": errors.New("refused")}}
	rep, err := (&Evaluator{Client: f, Settings: DefaultSettings()}).EvalRun(context.Background(), "pick_backend", []string{"up", "down"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Models[0].Passed != 1 || rep.Models[0].Unreachable {
		t.Errorf("up = %+v", rep.Models[0])
	}
	if !rep.Models[1].Unreachable || rep.Models[1].Passed != 0 {
		t.Errorf("down = %+v", rep.Models[1])
	}
}

func TestSupervisorLoopEvalBundledFixtures(t *testing.T) {
	for _, job := range ModelJobNames {
		cases, err := LoadEvalCases(job, "testdata/eval")
		if err != nil {
			t.Fatalf("%s: %v", job, err)
		}
		var pass, fail int
		for _, c := range cases {
			if c.Input == "" {
				t.Errorf("%s/%s has no input", job, c.Name)
			}
			if c.ExpectedValidator == EvalExpectPass {
				pass++
			} else {
				fail++
			}
		}
		if pass != 1 || fail != 1 {
			t.Errorf("%s: %d passing and %d failing cases, want 1 and 1", job, pass, fail)
		}
	}
}

func TestSupervisorLoopEvalRejectsBadInput(t *testing.T) {
	ev := &Evaluator{Client: &evalFake{}, Settings: DefaultSettings()}
	if _, err := ev.EvalRun(context.Background(), "nope", []string{"m"}, "testdata/eval"); err == nil {
		t.Error("unknown job accepted")
	}
	if _, err := ev.EvalRun(context.Background(), "draft_spec", nil, "testdata/eval"); err == nil {
		t.Error("empty model list accepted")
	}
}
