package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// runCapErr is the no-output error a verifier session returns when the run cap
// ended it.
func runCapErr() error {
	return &autonomouspkg.OneShotNoOutputError{Msg: "verifier session ended at its run cap", RunCap: true}
}

// fakeVerifierRuns scripts one verifier session per backend: errs[backend] is
// returned for that backend, and content[backend] is the content it writes.
type fakeVerifierRuns struct {
	errs    map[string]error
	content map[string]string
	calls   []string
}

func (f *fakeVerifierRuns) run(backend, model string) (string, error) {
	f.calls = append(f.calls, backend+"|"+model)
	return f.content[backend], f.errs[backend]
}

func retryModelRegistryDefault(string) (string, bool) { return "", true }

func TestVerifierRunCapRetryExactlyOneRetryOnNextBackend(t *testing.T) {
	f := &fakeVerifierRuns{
		errs:    map[string]error{"ollama-a": runCapErr()},
		content: map[string]string{"ollama-b": `{"ok":true}`},
	}
	content, err := runVerifierSessionWithRunCapRetry([]string{"ollama-a", "ollama-b", "ollama-c"},
		"ollama-a", "m1", f.run, retryModelRegistryDefault)
	if err != nil || content != `{"ok":true}` {
		t.Fatalf("got content=%q err=%v, want the retry backend's output", content, err)
	}
	if want := []string{"ollama-a|m1", "ollama-b|"}; strings.Join(f.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v (one retry on the next backend)", f.calls, want)
	}
}

func TestVerifierRunCapRetrySecondCapEndDoesNotRetryAgain(t *testing.T) {
	f := &fakeVerifierRuns{
		errs: map[string]error{"ollama-a": runCapErr(), "ollama-b": runCapErr()},
	}
	_, err := runVerifierSessionWithRunCapRetry([]string{"ollama-a", "ollama-b", "ollama-c"},
		"ollama-a", "", f.run, retryModelRegistryDefault)
	if !autonomouspkg.IsVerifierRunCapEnd(err) {
		t.Fatalf("err = %v, want the second run-cap end returned unchanged", err)
	}
	if want := []string{"ollama-a|", "ollama-b|"}; strings.Join(f.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v: the second cap end must not retry on ollama-c", f.calls, want)
	}
}

func TestVerifierRunCapRetrySingleBackendIsInfraNotFailed(t *testing.T) {
	f := &fakeVerifierRuns{errs: map[string]error{"ollama-a": runCapErr()}}
	_, err := runVerifierSessionWithRunCapRetry([]string{"ollama-a"}, "ollama-a", "", f.run, retryModelRegistryDefault)
	if !autonomouspkg.IsVerifierRunCapEnd(err) {
		t.Fatalf("err = %v, want the run-cap end returned", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %v, want a single run with no retry", f.calls)
	}
	res := verifierSessionInfraResult(err)
	if res.Category != "infra" || res.OK {
		t.Fatalf("verdict = %+v, want an infra result that is not a content failure", res)
	}
}

func TestVerifierRunCapRetryNoModelForRetryKeepsCutError(t *testing.T) {
	f := &fakeVerifierRuns{errs: map[string]error{"ollama-a": runCapErr()}}
	_, err := runVerifierSessionWithRunCapRetry([]string{"ollama-a", "ollama-b"}, "ollama-a", "",
		f.run, func(string) (string, bool) { return "", false })
	if !autonomouspkg.IsVerifierRunCapEnd(err) || len(f.calls) != 1 {
		t.Fatalf("err=%v calls=%v, want the cap end with no retry when no model can be set", err, f.calls)
	}
}

func TestVerifierRunCapRetryOtherErrorsAreNotRetried(t *testing.T) {
	boom := errors.New("session failed")
	f := &fakeVerifierRuns{errs: map[string]error{"ollama-a": boom}}
	_, err := runVerifierSessionWithRunCapRetry([]string{"ollama-a", "ollama-b"}, "ollama-a", "",
		f.run, retryModelRegistryDefault)
	if !errors.Is(err, boom) || len(f.calls) != 1 {
		t.Fatalf("err=%v calls=%v, want the non-cap error returned with no retry", err, f.calls)
	}
}

// readVerifierRunCapRetrySource returns the helper file's source, for the
// wiring tests that pin the verdict text.
func readVerifierRunCapRetrySource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("verifier_run_cap_retry.go")
	if err != nil {
		t.Fatalf("read verifier_run_cap_retry.go: %v", err)
	}
	return string(b)
}
