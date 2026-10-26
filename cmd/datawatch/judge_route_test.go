package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// nopPick discards the decision log writes of a chooser under test.
var nopPick judgePickFn = func(string, string, string) {}

// judgeRouteConfig is a live autonomous config whose verifier policy uses mode
// over the given backend list. The judge policy list is left empty.
func judgeRouteConfig(mode string, backends ...string) autonomouspkg.Config {
	return autonomouspkg.Config{VerifierPolicy: config.VerifierPolicyConfig{Mode: mode, Backends: backends}}
}

// pickRecorder collects the decision log writes of one judge call.
type pickRecorder struct{ picks []string }

func (r *pickRecorder) record(backend, model, reason string) {
	r.picks = append(r.picks, backend+"|"+model+"|"+reason)
}

// grantingLease is a judge lease admission fake: it grants at once on node and
// counts how many times the lease was released.
func grantingLease(releases *atomic.Int32, node string) judgeLeaseAdmit {
	return func(context.Context, string, string, string, string) (func(), string, error) {
		return func() { releases.Add(1) }, node, nil
	}
}

func TestJudgeWaitsForFreeSlotBeforeTheJudgeRuns(t *testing.T) {
	// Every backend's pool is full, so prefer-free waits on the first one. The
	// judge body must not start until the lease is granted.
	choose := judgeVerifierChooser(nil, judgeRouteConfig("prefer-free", "qwen", "claude-code"), "ollama",
		func(string, string) bool { return false }, nopPick)
	backend, ok := choose(nil)
	if !ok || backend != "qwen" {
		t.Fatalf("pool full: picked %q %v, want to wait on qwen", backend, ok)
	}

	free := make(chan struct{})
	admit := func(ctx context.Context, _, _, _, _ string) (func(), string, error) {
		select {
		case <-free:
			return func() {}, "alpha", nil
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	ran := make(chan string, 1)
	type result struct {
		v   autonomouspkg.GuardrailVerdict
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, err := runJudgeLeased(context.Background(), admit, &judgeAttempt{Backend: backend}, "", "u1", "p1",
			func(node string) (autonomouspkg.GuardrailVerdict, error) {
				ran <- node
				return autonomouspkg.GuardrailVerdict{Outcome: "pass"}, nil
			})
		done <- result{v, err}
	}()

	select {
	case <-ran:
		t.Fatal("judge body ran while the pool was full")
	case <-time.After(50 * time.Millisecond):
	}
	close(free)
	select {
	case node := <-ran:
		if node != "alpha" {
			t.Fatalf("judge ran on node %q, want the leased node alpha", node)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("judge body did not run after the slot freed")
	}
	if r := <-done; r.err != nil || r.v.Outcome != "pass" {
		t.Fatalf("result = %+v, want the judge's pass verdict", r)
	}
}

func TestJudgeFallsBackToTheNextBackendWhenPreferredPoolIsFull(t *testing.T) {
	// qwen's pool is full, claude-code has a free slot: the judge takes the
	// next backend instead of waiting on the preferred one.
	probe := func(backend, _ string) bool { return backend == "claude-code" }
	choose := judgeVerifierChooser(nil, judgeRouteConfig("prefer-free", "qwen", "claude-code"), "ollama", probe, nopPick)
	if b, ok := choose(nil); !ok || b != "claude-code" {
		t.Fatalf("fallback picked %q %v, want claude-code", b, ok)
	}
}

func TestJudgeLeaseIsReleasedWhenTheJudgeFails(t *testing.T) {
	var releases atomic.Int32
	boom := errors.New("judge session crashed")
	_, err := runJudgeLeased(context.Background(), grantingLease(&releases, "alpha"), &judgeAttempt{Backend: "qwen"}, "", "u1", "p1",
		func(string) (autonomouspkg.GuardrailVerdict, error) {
			return autonomouspkg.GuardrailVerdict{}, boom
		})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the judge's failure", err)
	}
	if n := releases.Load(); n != 1 {
		t.Fatalf("lease released %d times after a failed judge, want 1", n)
	}
}

func TestJudgeLeaseIsReleasedOnSuccessAndOnCancel(t *testing.T) {
	var releases atomic.Int32
	if _, err := runJudgeLeased(context.Background(), grantingLease(&releases, ""), &judgeAttempt{Backend: "qwen"}, "", "u1", "p1",
		func(string) (autonomouspkg.GuardrailVerdict, error) {
			return autonomouspkg.GuardrailVerdict{Outcome: "pass"}, nil
		}); err != nil {
		t.Fatalf("success: %v", err)
	}
	if n := releases.Load(); n != 1 {
		t.Fatalf("lease released %d times after success, want 1", n)
	}

	ctx, cancel := context.WithCancel(context.Background())
	_, err := runJudgeLeased(ctx, grantingLease(&releases, ""), &judgeAttempt{Backend: "qwen"}, "", "u1", "p1",
		func(string) (autonomouspkg.GuardrailVerdict, error) {
			cancel()
			return autonomouspkg.GuardrailVerdict{}, ctx.Err()
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err = %v, want context.Canceled", err)
	}
	if n := releases.Load(); n != 2 {
		t.Fatalf("lease released %d times after two runs, want 2", n)
	}
}

func TestJudgeBodyNeverRunsWhenTheLeaseIsNotGranted(t *testing.T) {
	admit := func(context.Context, string, string, string, string) (func(), string, error) {
		return nil, "", errors.New("wait expired")
	}
	ran := false
	v, err := runJudgeLeased(context.Background(), admit, &judgeAttempt{Backend: "qwen"}, "", "u1", "p1",
		func(string) (autonomouspkg.GuardrailVerdict, error) {
			ran = true
			return autonomouspkg.GuardrailVerdict{Outcome: "pass"}, nil
		})
	if err != nil || ran {
		t.Fatalf("err = %v ran = %v, want no error and the judge never run", err, ran)
	}
	if v.Category != "infra" || v.Outcome != "block" {
		t.Fatalf("verdict = %+v, want an infra block", v)
	}
}

func TestJudgeChooserRecordsEveryPickToTheDecisionLog(t *testing.T) {
	var rec pickRecorder
	probe := func(backend, _ string) bool { return backend == "claude-code" }
	choose := judgeVerifierChooser(nil, judgeRouteConfig("prefer-free", "qwen", "claude-code"), "ollama", probe, rec.record)
	if b, _ := choose(nil); b != "claude-code" {
		t.Fatalf("picked %q, want claude-code", b)
	}
	if len(rec.picks) != 1 || !strings.HasPrefix(rec.picks[0], "claude-code|") || !strings.Contains(rec.picks[0], "free slot") {
		t.Fatalf("decision log = %v, want one claude-code pick with its reason", rec.picks)
	}

	// Nothing eligible on the first attempt falls back to the unit backend,
	// and that fallback is written too.
	var fallback pickRecorder
	empty := judgeVerifierChooser(nil, judgeRouteConfig("fixed"), "ollama", nil, fallback.record)
	if b, ok := empty(nil); !ok || b != "ollama" {
		t.Fatalf("fallback = %q %v, want the unit backend ollama", b, ok)
	}
	if len(fallback.picks) != 1 || !strings.HasPrefix(fallback.picks[0], "ollama|") {
		t.Fatalf("fallback decision log = %v, want the unit backend pick", fallback.picks)
	}
}

func TestJudgePolicyListReplacesVerifierBackendsForJudges(t *testing.T) {
	// autonomous.judge_policy replaces the verifier policy's own backend list
	// for judges; the verifier's mode still decides among the judge list.
	cfg := judgeRouteConfig("fixed", "claude-code")
	cfg.JudgePolicy = []string{"qwen"}
	choose := judgeVerifierChooser(nil, cfg, "ollama", nil, nopPick)
	if b, ok := choose(nil); !ok || b != "qwen" {
		t.Fatalf("judge list picked %q %v, want qwen", b, ok)
	}
}
