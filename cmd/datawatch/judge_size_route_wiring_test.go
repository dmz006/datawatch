package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// sizeRouteConfig is a live config whose judge policy lists chain, with the
// verifier policy in mode.
func sizeRouteConfig(mode string, chain ...string) autonomouspkg.Config {
	return autonomouspkg.Config{
		JudgePolicy:    chain,
		VerifierPolicy: config.VerifierPolicyConfig{Mode: mode},
	}
}

// sizeRouteFirstPick runs the first choice of one judge call over cfg with the
// given measured size and returns the backend and the recorded reason.
func sizeRouteFirstPick(t *testing.T, cfg autonomouspkg.Config, size judgeSize) (string, string) {
	t.Helper()
	rec := &pickRecorder{}
	probe := func(string, string) bool { return true }
	choose := judgeVerifierChooserSized(nil, cfg, "ollama", probe, rec.record, size)
	backend, ok := choose(nil)
	if !ok {
		t.Fatal("no backend chosen")
	}
	if len(rec.picks) != 1 {
		t.Fatalf("decision log has %d picks, want 1: %v", len(rec.picks), rec.picks)
	}
	parts := strings.SplitN(rec.picks[0], "|", 3)
	if parts[0] != backend {
		t.Fatalf("logged backend %q, chose %q", parts[0], backend)
	}
	return backend, parts[2]
}

func TestJudgeSizeRouteAboveThresholdPicksClaudeCodeFirst(t *testing.T) {
	cfg := sizeRouteConfig("fixed", "pi", "opencode", "claude-code")
	backend, reason := sizeRouteFirstPick(t, cfg, judgeSize{Threshold: 400, Lines: 900})
	if backend != "claude-code" {
		t.Fatalf("first pick = %q, want claude-code", backend)
	}
	for _, want := range []string{"900 changed lines", "threshold of 400", "size rule"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("reason %q lacks %q", reason, want)
		}
	}
}

func TestJudgeSizeRouteAboveThresholdWorksForPreferFreeAndPool(t *testing.T) {
	// prefer-free reads the same order; with no judge or verifier list the
	// candidate pool is the list read, and it is reordered too.
	cfg := autonomouspkg.Config{
		VerificationBackends: []string{"pi", "claude-code"},
		VerifierPolicy:       config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"pi", "claude-code"}},
	}
	if b, _ := sizeRouteFirstPick(t, cfg, judgeSize{Threshold: 400, Lines: 900}); b != "claude-code" {
		t.Fatalf("prefer-free first pick = %q, want claude-code", b)
	}
	cfg = autonomouspkg.Config{
		VerificationBackends: []string{"pi", "claude-code"},
		VerifierPolicy:       config.VerifierPolicyConfig{Mode: "prefer-free"},
	}
	b, reason := sizeRouteFirstPick(t, cfg, judgeSize{Threshold: 400, Lines: 900})
	if b != "claude-code" || !strings.Contains(reason, "900 changed lines") {
		t.Fatalf("pool: pick %q reason %q, want claude-code and the count", b, reason)
	}
}

func TestJudgeSizeRouteAtAndBelowThresholdKeepsDefaultOrder(t *testing.T) {
	cfg := sizeRouteConfig("fixed", "pi", "claude-code")
	for _, lines := range []int{400, 12, 0} {
		backend, reason := sizeRouteFirstPick(t, cfg, judgeSize{Threshold: 400, Lines: lines})
		if backend != "pi" {
			t.Fatalf("%d lines: first pick = %q, want pi", lines, backend)
		}
		if !strings.Contains(reason, "default order") {
			t.Fatalf("%d lines: reason %q lacks default order", lines, reason)
		}
	}
}

func TestJudgeSizeRouteThresholdZeroIsDefaultOrder(t *testing.T) {
	cfg := sizeRouteConfig("fixed", "pi", "claude-code")
	// Off is the zero judgeSize: the count in it is never read.
	backend, reason := sizeRouteFirstPick(t, cfg, judgeSize{Lines: 100000})
	if backend != "pi" {
		t.Fatalf("first pick = %q, want pi", backend)
	}
	if strings.Contains(reason, "size rule") || strings.Contains(reason, "100000") {
		t.Fatalf("reason %q mentions the size rule while it is off", reason)
	}
}

func TestJudgeSizeRouteThresholdZeroSkipsMeasurement(t *testing.T) {
	called := false
	defer func(f func(context.Context, *autonomouspkg.PRD, string) (int, error)) { judgeDiffLines = f }(judgeDiffLines)
	judgeDiffLines = func(context.Context, *autonomouspkg.PRD, string) (int, error) {
		called = true
		return 1, nil
	}
	size := measureJudgeSize(context.Background(), 0, autonomouspkg.GuardrailInvocation{PRDID: "p1"},
		func(string) (*autonomouspkg.PRD, bool) { called = true; return &autonomouspkg.PRD{}, true })
	if called || size.Threshold != 0 || size.Err != nil {
		t.Fatalf("threshold 0 measured anyway: called=%v size=%+v", called, size)
	}
}

func TestJudgeSizeRouteAboveThresholdWithoutClaudeCodeKeepsOrder(t *testing.T) {
	cfg := sizeRouteConfig("fixed", "pi", "ollama")
	backend, reason := sizeRouteFirstPick(t, cfg, judgeSize{Threshold: 400, Lines: 900})
	if backend != "pi" {
		t.Fatalf("first pick = %q, want pi", backend)
	}
	if !strings.Contains(reason, "not in the chain") {
		t.Fatalf("reason %q should say claude-code is not in the chain", reason)
	}
}

func TestJudgeSizeRouteBalanceModeSaysOrderOnlyBreaksTies(t *testing.T) {
	cfg := sizeRouteConfig("balance", "pi", "claude-code")
	// Equal load: the reordered list breaks the tie in claude-code's favour.
	backend, reason := sizeRouteFirstPick(t, cfg, judgeSize{Threshold: 400, Lines: 900})
	if backend != "claude-code" {
		t.Fatalf("first pick = %q, want claude-code on a load tie", backend)
	}
	if !strings.Contains(reason, "balance mode") || !strings.Contains(reason, "load ties") {
		t.Fatalf("reason %q lacks the balance-mode note", reason)
	}
}

func TestJudgeSizeRouteMeasurementFailureGivesDefaultOrder(t *testing.T) {
	defer func(f func(context.Context, *autonomouspkg.PRD, string) (int, error)) { judgeDiffLines = f }(judgeDiffLines)
	judgeDiffLines = func(context.Context, *autonomouspkg.PRD, string) (int, error) {
		return 0, errors.New("git exploded")
	}
	req := autonomouspkg.GuardrailInvocation{PRDID: "p1", ProjectDir: "/x"}
	size := measureJudgeSize(context.Background(), 400, req,
		func(string) (*autonomouspkg.PRD, bool) { return &autonomouspkg.PRD{ID: "p1"}, true })
	if size.Err == nil {
		t.Fatal("measurement error was dropped")
	}
	backend, reason := sizeRouteFirstPick(t, sizeRouteConfig("fixed", "pi", "claude-code"), size)
	if backend != "pi" {
		t.Fatalf("first pick = %q, want pi", backend)
	}
	for _, want := range []string{"default order", "unknown", "git exploded"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("reason %q lacks %q", reason, want)
		}
	}
}

func TestJudgeSizeRouteMissingAutomatonGivesUnknownCount(t *testing.T) {
	size := measureJudgeSize(context.Background(), 400, autonomouspkg.GuardrailInvocation{PRDID: "gone"},
		func(string) (*autonomouspkg.PRD, bool) { return nil, false })
	if size.Err == nil {
		t.Fatal("missing automaton should leave the count unknown")
	}
	if b, reason := sizeRouteFirstPick(t, sizeRouteConfig("fixed", "pi", "claude-code"), size); b != "pi" || !strings.Contains(reason, "unknown") {
		t.Fatalf("pick %q reason %q, want pi and an unknown count", b, reason)
	}
}

func TestJudgeSizeRouteMeasurementIsBoundedAndUsesProjectDir(t *testing.T) {
	defer func(f func(context.Context, *autonomouspkg.PRD, string) (int, error)) { judgeDiffLines = f }(judgeDiffLines)
	var gotDir string
	var gotDeadline time.Time
	judgeDiffLines = func(ctx context.Context, _ *autonomouspkg.PRD, dir string) (int, error) {
		gotDir = dir
		gotDeadline, _ = ctx.Deadline()
		return 777, nil
	}
	size := measureJudgeSize(context.Background(), 400, autonomouspkg.GuardrailInvocation{PRDID: "p1", ProjectDir: "/work/p1"},
		func(string) (*autonomouspkg.PRD, bool) { return &autonomouspkg.PRD{ID: "p1"}, true })
	if size.Lines != 777 || size.Err != nil || size.Threshold != 400 {
		t.Fatalf("size = %+v", size)
	}
	if gotDir != "/work/p1" {
		t.Fatalf("measured in %q, want the request project dir", gotDir)
	}
	if left := time.Until(gotDeadline); left <= 0 || left > judgeSizeMeasureTimeout {
		t.Fatalf("measurement deadline in %s, want within %s", left, judgeSizeMeasureTimeout)
	}
}

func TestJudgeSizeRoutePinnedUnitBackendKeepsItsBehaviour(t *testing.T) {
	req := autonomouspkg.GuardrailInvocation{PRDID: "p1", UnitID: "u1", GuardrailBackend: "pi"}
	cfg := sizeRouteConfig("fixed", "pi", "claude-code")
	choose := judgeBackendChooserForSize(context.Background(), nil, req, cfg, "pi", judgeSize{Threshold: 400, Lines: 900})
	if b, ok := choose(nil); !ok || b != "pi" {
		t.Fatalf("pinned pick = %q %v, want pi", b, ok)
	}
	if _, ok := choose([]string{"pi"}); ok {
		t.Fatal("a pinned judge must never move to another backend")
	}
}

func TestJudgeSizeRouteEachBackendTriedOnceAndHoldStaysUnchanged(t *testing.T) {
	cfg := sizeRouteConfig("fixed", "pi", "opencode", "claude-code")
	rec := &pickRecorder{}
	choose := judgeVerifierChooserSized(nil, cfg, "ollama", func(string, string) bool { return true }, rec.record,
		judgeSize{Threshold: 400, Lines: 900})
	var ran []string
	var alerts []string
	req := autonomouspkg.GuardrailInvocation{PRDID: "p1", UnitID: "u1", Guardrail: "g"}
	verdict, err := runJudgeAttempts(req, choose, time.Minute,
		func(level, title, body string) { alerts = append(alerts, level) }, nil,
		func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
			ran = append(ran, backend)
			att.TimedOut = true
			return autonomouspkg.GuardrailVerdict{}, nil
		})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if want := "claude-code,pi,opencode"; strings.Join(ran, ",") != want {
		t.Fatalf("attempt order = %v, want %s", ran, want)
	}
	// The hold verdict is what moves the Automaton to needs_review with the
	// judge_timeout_hold reason (internal/autonomous/judge_hold.go).
	if !autonomouspkg.IsJudgeTimeoutHold(verdict) {
		t.Fatalf("verdict = %+v, want the judge-timeout hold (judge_timeout_hold)", verdict)
	}
	if len(verdict.JudgeAttempts) != 3 {
		t.Fatalf("hold lists %d attempts, want 3", len(verdict.JudgeAttempts))
	}
	if len(alerts) == 0 || alerts[len(alerts)-1] != "error" {
		t.Fatalf("alerts = %v, want a final error alert", alerts)
	}
	for _, p := range rec.picks {
		if !strings.Contains(p, "900 changed lines") {
			t.Fatalf("pick %q lacks the line count", p)
		}
	}
}

func TestJudgeSizeRouteUnitBackendFallbackCarriesRule(t *testing.T) {
	cases := map[string]struct {
		cfg   autonomouspkg.Config
		probe func(string, string) bool
		size  judgeSize
		want  string
	}{
		"policy does not apply": {
			cfg:   autonomouspkg.Config{VerifierPolicy: config.VerifierPolicyConfig{Mode: "fixed"}},
			probe: func(string, string) bool { return true },
			size:  judgeSize{Threshold: 400, Lines: 900},
			want:  "900 changed lines",
		},
		"no eligible backend": {
			cfg:   sizeRouteConfig("gpu-only", "claude-code"),
			probe: func(string, string) bool { return false },
			size:  judgeSize{Threshold: 400, Lines: 900},
			want:  "900 changed lines",
		},
		"count unknown": {
			cfg:   sizeRouteConfig("gpu-only", "claude-code"),
			probe: func(string, string) bool { return false },
			size:  judgeSize{Threshold: 400, Err: errors.New("git failed")},
			want:  "changed line count unknown",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := &pickRecorder{}
			choose := judgeVerifierChooserSized(nil, c.cfg, "ollama", c.probe, rec.record, c.size)
			backend, ok := choose(nil)
			if !ok || backend != "ollama" {
				t.Fatalf("pick = %q %v, want the unit backend ollama", backend, ok)
			}
			if len(rec.picks) != 1 {
				t.Fatalf("decision log has %d picks, want 1: %v", len(rec.picks), rec.picks)
			}
			reason := strings.SplitN(rec.picks[0], "|", 3)[2]
			if !strings.Contains(reason, c.want) || !strings.Contains(reason, "using the unit backend ollama") {
				t.Fatalf("fallback reason %q lacks %q or the unit backend note", reason, c.want)
			}
		})
	}
}

func TestJudgeSizeRouteBalanceDefaultOrderSaysOrderOnlyBreaksTies(t *testing.T) {
	cfg := sizeRouteConfig("balance", "pi", "claude-code")
	_, reason := sizeRouteFirstPick(t, cfg, judgeSize{Threshold: 400, Lines: 12})
	if !strings.Contains(reason, "default order") || !strings.Contains(reason, "load ties") {
		t.Fatalf("reason %q lacks default order or the balance-mode note", reason)
	}
}
