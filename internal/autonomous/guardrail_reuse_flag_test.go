package autonomous

import (
	"context"
	"errors"
	"testing"
	"time"
)

// reusingJudge returns a judge fake that counts its calls and returns v, err.
func reusingJudge(calls *int, v GuardrailVerdict, err error) GuardrailFn {
	return func(ctx context.Context, i GuardrailInvocation) (GuardrailVerdict, error) {
		*calls++
		return v, err
	}
}

func TestGuardrailReusingHitReportsReused(t *testing.T) {
	m, prd, _, inv := newJudgeVerdictCacheTestManager(t, t.TempDir(), nil)
	judged := GuardrailVerdict{Guardrail: "rules", Outcome: "block", Category: "content", Summary: "stored", VerdictAt: time.Now()}
	m.judgeVerdictCacheRecord(prd, inv, judged, nil)

	calls := 0
	v, reused, err := m.runGuardrailReusing(context.Background(), prd, inv, reusingJudge(&calls, GuardrailVerdict{Outcome: "pass"}, nil))
	if err != nil {
		t.Fatalf("runGuardrailReusing: %v", err)
	}
	if !reused {
		t.Fatal("reused=false on a cache hit")
	}
	if calls != 0 {
		t.Fatalf("judge called %d times on a cache hit, want 0", calls)
	}
	if v.Summary != "stored" || v.Outcome != "block" {
		t.Fatalf("hit returned %+v, want the stored verdict", v)
	}
}

func TestGuardrailReusingMissRunsJudgeAndStores(t *testing.T) {
	m, prd, _, inv := newJudgeVerdictCacheTestManager(t, t.TempDir(), nil)
	judged := GuardrailVerdict{Guardrail: "rules", Outcome: "pass", Summary: "fresh", VerdictAt: time.Now()}

	calls := 0
	v, reused, err := m.runGuardrailReusing(context.Background(), prd, inv, reusingJudge(&calls, judged, nil))
	if err != nil {
		t.Fatalf("runGuardrailReusing: %v", err)
	}
	if reused {
		t.Fatal("reused=true on a cache miss")
	}
	if calls != 1 {
		t.Fatalf("judge called %d times on a miss, want 1", calls)
	}
	if v.Summary != "fresh" {
		t.Fatalf("miss returned %+v, want the judged verdict", v)
	}
	stored, ok := m.judgeVerdictCacheHit(prd, inv)
	if !ok || stored.Summary != "fresh" {
		t.Fatalf("verdict not stored after a miss: %+v ok=%v", stored, ok)
	}
}

func TestGuardrailReusingBypassedContextRunsJudge(t *testing.T) {
	m, prd, _, inv := newJudgeVerdictCacheTestManager(t, t.TempDir(), nil)
	m.judgeVerdictCacheRecord(prd, inv, GuardrailVerdict{Guardrail: "rules", Outcome: "pass", Summary: "stored", VerdictAt: time.Now()}, nil)

	calls := 0
	ctx := withJudgeVerdictCacheLookupBypassed(context.Background())
	v, reused, err := m.runGuardrailReusing(ctx, prd, inv, reusingJudge(&calls, GuardrailVerdict{Guardrail: "rules", Outcome: "block", Category: "content", Summary: "live", VerdictAt: time.Now()}, nil))
	if err != nil {
		t.Fatalf("runGuardrailReusing: %v", err)
	}
	if reused {
		t.Fatal("reused=true with the lookup bypassed")
	}
	if calls != 1 {
		t.Fatalf("judge called %d times with the lookup bypassed, want 1", calls)
	}
	if v.Summary != "live" {
		t.Fatalf("bypassed run returned %+v, want the live verdict", v)
	}
}

func TestGuardrailReusingDisabledCacheReportsNotReused(t *testing.T) {
	off := false
	m, prd, _, inv := newJudgeVerdictCacheTestManager(t, t.TempDir(), &off)

	calls := 0
	judged := GuardrailVerdict{Guardrail: "rules", Outcome: "pass", Summary: "live", VerdictAt: time.Now()}
	for i := 1; i <= 2; i++ {
		_, reused, err := m.runGuardrailReusing(context.Background(), prd, inv, reusingJudge(&calls, judged, nil))
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if reused {
			t.Fatalf("run %d: reused=true with the cache disabled", i)
		}
		if calls != i {
			t.Fatalf("run %d: judge calls=%d want %d", i, calls, i)
		}
	}
}

func TestGuardrailReusingErrorReportsNotReused(t *testing.T) {
	m, prd, _, inv := newJudgeVerdictCacheTestManager(t, t.TempDir(), nil)

	calls := 0
	boom := errors.New("judge crashed")
	_, reused, err := m.runGuardrailReusing(context.Background(), prd, inv, reusingJudge(&calls, GuardrailVerdict{}, boom))
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v want %v", err, boom)
	}
	if reused {
		t.Fatal("reused=true on an error")
	}
	if _, ok := m.judgeVerdictCacheHit(prd, inv); ok {
		t.Fatal("an error result was stored in the cache")
	}
}

func TestGuardrailReusingTimedOutVerdictNotReusedNextCall(t *testing.T) {
	m, prd, _, inv := newJudgeVerdictCacheTestManager(t, t.TempDir(), nil)
	timedOut := GuardrailVerdict{Guardrail: "rules", Outcome: "block", Category: JudgeTimeoutCategory, Summary: "judge timed out", VerdictAt: time.Now()}

	calls := 0
	for i := 1; i <= 2; i++ {
		v, reused, err := m.runGuardrailReusing(context.Background(), prd, inv, reusingJudge(&calls, timedOut, nil))
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if reused {
			t.Fatalf("run %d: a timed-out verdict was reused", i)
		}
		if v.Category != JudgeTimeoutCategory {
			t.Fatalf("run %d returned %+v", i, v)
		}
	}
	if calls < 2 {
		t.Fatalf("judge calls=%d, want the judge to run again on the second call", calls)
	}
}
