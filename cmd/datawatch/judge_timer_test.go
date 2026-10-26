package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// judgeTestClock is a clock a test moves by hand.
type judgeTestClock struct{ t time.Time }

func (c *judgeTestClock) now() time.Time          { return c.t }
func (c *judgeTestClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestJudgeTimeLimitDefaultIsThirtyMinutes(t *testing.T) {
	if config.JudgeTimeoutDefaultSeconds != 1800 {
		t.Fatalf("default = %d seconds, want 1800", config.JudgeTimeoutDefaultSeconds)
	}
	for _, v := range []int{0, -5} {
		if got := config.NormalizeJudgeTimeoutSeconds(v); got != 1800 {
			t.Fatalf("Normalize(%d) = %d, want the 30 minute default", v, got)
		}
	}
	if got := judgeSessionTimeout(autonomouspkg.Config{}); got != 30*time.Minute {
		t.Fatalf("unset limit = %s, want 30m", got)
	}
}

func TestJudgeTimerDoesNotCountTheWaitForACapacitySlot(t *testing.T) {
	// The slot is granted only after three times the limit has passed. The body
	// then runs well inside the limit, so there is no timeout.
	const limit = 100 * time.Millisecond
	free := make(chan struct{})
	var releases atomic.Int32
	admit := func(ctx context.Context, _, _, _, _ string) (func(), string, error) {
		select {
		case <-free:
			return func() { releases.Add(1) }, "alpha", nil
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	go func() {
		time.Sleep(3 * limit)
		close(free)
	}()
	att := &judgeAttempt{Backend: "qwen"}
	v, to, err := runJudgeTimed(context.Background(), admit, att, "", "u1", "p1", limit, nil,
		func(runCtx context.Context, node string) (autonomouspkg.GuardrailVerdict, error) {
			if node != "alpha" {
				t.Errorf("node = %q, want alpha", node)
			}
			if runCtx.Err() != nil {
				t.Error("run context already ended when the body started")
			}
			return autonomouspkg.GuardrailVerdict{Outcome: "pass"}, nil
		})
	if err != nil || to != nil || v.Outcome != "pass" || att.TimedOut {
		t.Fatalf("v=%+v timeout=%+v err=%v timedOut=%v, want a pass with no timeout", v, to, err, att.TimedOut)
	}
	if n := releases.Load(); n != 1 {
		t.Fatalf("lease released %d times, want 1", n)
	}
}

func TestJudgeRunUnderTheLimitIsUnaffected(t *testing.T) {
	var releases atomic.Int32
	clock := &judgeTestClock{t: time.Unix(1000, 0)}
	att := &judgeAttempt{Backend: "qwen"}
	v, to, err := runJudgeTimed(context.Background(), grantingLease(&releases, ""), att, "", "u1", "p1", time.Minute, clock.now,
		func(context.Context, string) (autonomouspkg.GuardrailVerdict, error) {
			clock.advance(10 * time.Second)
			return autonomouspkg.GuardrailVerdict{Outcome: "block", Category: "content", Summary: "real finding"}, nil
		})
	if err != nil || to != nil {
		t.Fatalf("timeout=%+v err=%v, want neither", to, err)
	}
	if v.Outcome != "block" || v.Summary != "real finding" || att.TimedOut {
		t.Fatalf("verdict=%+v timedOut=%v, want the judge's own verdict untouched", v, att.TimedOut)
	}
	if n := releases.Load(); n != 1 {
		t.Fatalf("lease released %d times, want 1", n)
	}
}

func TestJudgeRunOverTheLimitReturnsTimeoutResultAndReleasesLease(t *testing.T) {
	var releases atomic.Int32
	clock := &judgeTestClock{t: time.Unix(1000, 0)}
	var sessionStopped atomic.Bool
	att := &judgeAttempt{Backend: "claude-code"}
	v, to, err := runJudgeTimed(context.Background(), grantingLease(&releases, "alpha"), att, "", "u1", "p1", 20*time.Millisecond, clock.now,
		func(runCtx context.Context, _ string) (autonomouspkg.GuardrailVerdict, error) {
			<-runCtx.Done() // the session runs until the limit ends it
			sessionStopped.Store(true)
			clock.advance(20 * time.Millisecond)
			return autonomouspkg.GuardrailVerdict{}, runCtx.Err()
		})
	if err != nil {
		t.Fatalf("err = %v, want the timeout as a result, not an error", err)
	}
	if to == nil || to.Backend != "claude-code" || to.Elapsed != 20*time.Millisecond || to.Limit != 20*time.Millisecond {
		t.Fatalf("timeout = %+v, want backend claude-code, elapsed 20ms", to)
	}
	if !att.TimedOut || att.Elapsed != to.Elapsed {
		t.Fatalf("attempt = %+v, want TimedOut with the same elapsed time", att)
	}
	if v.Category != "infra" || v.Outcome != "block" {
		t.Fatalf("verdict = %+v, want the infra no-answer verdict, never a pass or content block", v)
	}
	if !sessionStopped.Load() {
		t.Fatal("judge session was not told to stop")
	}
	if n := releases.Load(); n != 1 {
		t.Fatalf("lease released %d times after a timeout, want 1", n)
	}
}

func TestJudgeCancelledByItsCallerIsNotATimeout(t *testing.T) {
	var releases atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	att := &judgeAttempt{Backend: "qwen"}
	_, to, err := runJudgeTimed(ctx, grantingLease(&releases, ""), att, "", "u1", "p1", time.Minute, nil,
		func(runCtx context.Context, _ string) (autonomouspkg.GuardrailVerdict, error) {
			cancel()
			<-runCtx.Done()
			return autonomouspkg.GuardrailVerdict{}, runCtx.Err()
		})
	if to != nil || att.TimedOut {
		t.Fatalf("timeout = %+v timedOut = %v, want none for a cancelled judge", to, att.TimedOut)
	}
	if err == nil {
		t.Fatal("a cancelled judge must return the context error")
	}
	if n := releases.Load(); n != 1 {
		t.Fatalf("lease released %d times, want 1", n)
	}
}
