package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// noSlotLease is a judge lease admission whose wait expires: no slot is granted.
func noSlotLease(context.Context, string, string, string, string) (func(), string, error) {
	return nil, "", errors.New("wait expired")
}

// freeLease is a judge lease admission that grants at once on no node.
func freeLease(context.Context, string, string, string, string) (func(), string, error) {
	return func() {}, "", nil
}

func TestJudgeMovesOnWhenItsLeaseWaitExpires(t *testing.T) {
	// qwen has no free slot within the wait, so the judge never runs there and
	// moves on to claude-code. A lease wait is not a time-limit timeout: no
	// alert is raised.
	var alerts []alertRecord
	var tried []string
	admit := func(ctx context.Context, backend, model, unitID, prdID string) (func(), string, error) {
		if backend == "qwen" {
			return noSlotLease(ctx, backend, model, unitID, prdID)
		}
		return freeLease(ctx, backend, model, unitID, prdID)
	}
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-9", Guardrail: "rules"}
	verdict, err := runJudgeAttempts(req, judgeChooserFromList([]string{"qwen", "claude-code"}), time.Minute, recordAlerts(&alerts),
		func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
			tried = append(tried, backend)
			return runJudgeLeased(context.Background(), admit, att, "", "u1", "prd-9",
				func(string) (autonomouspkg.GuardrailVerdict, error) {
					return autonomouspkg.GuardrailVerdict{Outcome: "pass"}, nil
				})
		})
	if err != nil || verdict.Outcome != "pass" {
		t.Fatalf("verdict = %+v err = %v, want the second backend's pass", verdict, err)
	}
	if !reflect.DeepEqual(tried, []string{"qwen", "claude-code"}) {
		t.Fatalf("attempts = %v, want qwen then claude-code", tried)
	}
	if len(alerts) != 0 {
		t.Fatalf("alerts = %+v, want none for a lease wait that expired", alerts)
	}
}

func TestJudgeWithNoSlotOnTheLastBackendIsInfraNotHold(t *testing.T) {
	// The only backend has no free slot in time. The judge did not time out, so
	// the PRD is not held: the infra verdict is returned and no error alert fires.
	var alerts []alertRecord
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-10", Guardrail: "rules"}
	verdict, err := runJudgeAttempts(req, judgeChooserFromList([]string{"qwen"}), time.Minute, recordAlerts(&alerts),
		func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
			return runJudgeLeased(context.Background(), noSlotLease, att, "", "u1", "prd-10",
				func(string) (autonomouspkg.GuardrailVerdict, error) {
					t.Fatal("judge body ran without a lease")
					return autonomouspkg.GuardrailVerdict{}, nil
				})
		})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if verdict.Category != "infra" || verdict.Outcome != "block" {
		t.Fatalf("verdict = %+v, want the infra block", verdict)
	}
	if len(alerts) != 0 {
		t.Fatalf("alerts = %+v, want none: no judge timed out", alerts)
	}
}

func TestJudgeClaudeWindowCountsOnlyGrantedLeases(t *testing.T) {
	const window = time.Hour
	body := func(string) (autonomouspkg.GuardrailVerdict, error) {
		return autonomouspkg.GuardrailVerdict{Outcome: "pass"}, nil
	}
	before := judgeClaudeWindow.Count(window)

	// A Claude pick whose lease is not granted does not use up the window cap.
	if _, err := runJudgeLeased(context.Background(), noSlotLease, &judgeAttempt{Backend: "claude-code"}, "", "u1", "p1", body); err != nil {
		t.Fatalf("denied lease: %v", err)
	}
	if n := judgeClaudeWindow.Count(window) - before; n != 0 {
		t.Fatalf("denied Claude lease counted %d times, want 0", n)
	}

	// A granted Claude lease counts once.
	if _, err := runJudgeLeased(context.Background(), freeLease, &judgeAttempt{Backend: "claude-code"}, "", "u1", "p1", body); err != nil {
		t.Fatalf("granted lease: %v", err)
	}
	if n := judgeClaudeWindow.Count(window) - before; n != 1 {
		t.Fatalf("granted Claude lease counted %d times, want 1", n)
	}

	// A non-Claude backend never counts against the Claude window.
	if _, err := runJudgeLeased(context.Background(), freeLease, &judgeAttempt{Backend: "qwen"}, "", "u1", "p1", body); err != nil {
		t.Fatalf("qwen lease: %v", err)
	}
	if n := judgeClaudeWindow.Count(window) - before; n != 1 {
		t.Fatalf("non-Claude lease changed the Claude count to %d, want 1", n)
	}
}

func TestJudgePinnedUnitBackendIsProbedBySelector(t *testing.T) {
	// A pinned unit's judge goes through the selector too: its one backend is
	// probed for a slot, and the judge never moves on to another backend.
	var probed []string
	prev := judgeCapacityTryAdmit
	judgeCapacityTryAdmit = func(_ context.Context, backend, _, _, _ string) bool {
		probed = append(probed, backend)
		return false
	}
	t.Cleanup(func() { judgeCapacityTryAdmit = prev })

	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-1", UnitID: "u-1", GuardrailBackend: "qwen"}
	gcfg := autonomouspkg.Config{
		VerifierPolicy: config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"claude-code"}},
		JudgePolicy:    []string{"claude-code"},
	}
	choose := judgeBackendChooserFor(context.Background(), nil, req, gcfg, "ollama")
	backend, ok := choose(nil)
	if !ok || backend != "qwen" {
		t.Fatalf("pinned pick = %q %v, want qwen", backend, ok)
	}
	if len(probed) == 0 {
		t.Fatal("pinned judge was not probed through the selector")
	}
	for _, b := range probed {
		if b != "qwen" {
			t.Fatalf("probed %q, want only the pinned qwen", b)
		}
	}
	if _, ok := choose([]string{"qwen"}); ok {
		t.Fatal("pinned judge must not move on to claude-code")
	}
}

func TestJudgeAskRunStopsAtTheTimeLimit(t *testing.T) {
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hold:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(hold)

	_, _, timedOut, err := judgeAskRun(context.Background(), 50*time.Millisecond, srv.URL, "", []byte("{}"))
	if err == nil || !timedOut {
		t.Fatalf("err = %v timedOut = %v, want a timeout at the limit", err, timedOut)
	}
}

func TestJudgeAskRunAnswerWithinTheLimitIsNotATimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			http.Error(w, "bad auth "+got, http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"answer":"ok"}`))
	}))
	defer srv.Close()

	status, rb, timedOut, err := judgeAskRun(context.Background(), time.Minute, srv.URL, "tok", []byte("{}"))
	if err != nil || timedOut {
		t.Fatalf("err = %v timedOut = %v, want a clean answer", err, timedOut)
	}
	if status != http.StatusOK || string(rb) != `{"answer":"ok"}` {
		t.Fatalf("status = %d body = %q, want 200 with the answer", status, rb)
	}
}

func TestJudgeAskRunCancelledJudgeIsNotATimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, timedOut, err := judgeAskRun(ctx, time.Minute, srv.URL, "", []byte("{}"))
	if err == nil || timedOut {
		t.Fatalf("err = %v timedOut = %v, want a cancellation that is not a timeout", err, timedOut)
	}
}
